package replica

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	gotmpl "text/template"

	"github.com/Masterminds/sprig/v3"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	"sigs.k8s.io/yaml"

	"github.com/openmcp-project/controller-utils/pkg/conditions"
	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	errutils "github.com/openmcp-project/controller-utils/pkg/errors"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/multicluster-provider/pkg/provider"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	cconst "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1/constants"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
)

const (
	ControllerName = "Replica"

	HostingPlatformClusterNameForLogging = "<hosting-platform-cluster>"
)

type ReplicaController struct {
	platformClient client.Client
	provider       multicluster.Provider
	providerName   string
	eventRecorder  events.EventRecorder
}

var _ reconcile.Reconciler = &ReplicaController{}

type ReconcileResult = ctrlutils.ReconcileResult[repv1alpha1.ReplicaEquivalent]

// Reconcile implements [reconcile.TypedReconciler].
func (c *ReplicaController) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName)
	ctx = logging.NewContext(ctx, log)
	log.Info("Starting reconcile")

	rr := c.reconcile(ctx, req)

	res, err := ctrlutils.NewOpenMCPStatusUpdaterBuilder[repv1alpha1.ReplicaEquivalent]().
		WithNestedStruct("Status").
		WithConditionUpdater(false).
		WithConditionEvents(c.eventRecorder, conditions.EventPerNewStatus).
		WithPhaseUpdateFunc(func(obj repv1alpha1.ReplicaEquivalent, rr ReconcileResult) (string, error) {
			if !rr.Object.GetDeletionTimestamp().IsZero() {
				return commonapi.StatusPhaseTerminating, nil
			}
			for _, con := range rr.Object.GetStatus().Conditions {
				if con.Status != metav1.ConditionTrue {
					return commonapi.StatusPhaseProgressing, nil
				}
			}
			return commonapi.StatusPhaseReady, nil
		}).
		Build().
		UpdateStatus(ctx, c.platformClient, rr)

	return res, err
}

func (c *ReplicaController) reconcile(ctx context.Context, req reconcile.Request) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)

	rr := ReconcileResult{}

	// fetch the replica resource
	var rep repv1alpha1.ReplicaEquivalent
	if req.Namespace != "" {
		rep = &repv1alpha1.Replica{}
	} else {
		rep = &repv1alpha1.ClusterReplica{}
	}
	if err := c.platformClient.Get(ctx, req.NamespacedName, rep); err != nil {
		if !apierrors.IsNotFound(err) {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("error fetching resource: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
			return rr
		}
		log.Debug("Resource not found")
		return rr
	}
	log = log.WithValues("replicaKind", rep.ReplicaKind())
	ctx = logging.NewContext(ctx, log)

	// handle operation annotation
	if rep.GetAnnotations() != nil {
		op, ok := rep.GetAnnotations()[openmcpconst.OperationAnnotation]
		if ok {
			switch op {
			case openmcpconst.OperationAnnotationValueIgnore:
				log.Info("Ignoring resource due to ignore operation annotation")
				return rr
			case openmcpconst.OperationAnnotationValueReconcile:
				log.Debug("Removing reconcile operation annotation from resource")
				if err := ctrlutils.EnsureAnnotation(ctx, c.platformClient, rep, openmcpconst.OperationAnnotation, "", true, ctrlutils.DELETE); err != nil {
					rr.ReconcileError = errutils.WithReason(fmt.Errorf("error removing operation annotation: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
					return rr
				}
			}
		}
	}

	rr.Object = rep
	rr.OldObject = rep.DeepCopyReplicaEquivalent()
	rr.Conditions = []metav1.Condition{}
	if rr.Object.GetStatus().Replicas == nil {
		rr.Object.GetStatus().Replicas = repv1alpha1.CreatedResourcesWithTypeList{}
	}

	var managedResources map[commonapi.ObjectReference][]client.Object
	if rep.GetDeletionTimestamp().IsZero() {
		rr, managedResources = c.handleCreateOrUpdate(ctx, rr)
	} else {
		rr = c.handleDelete(ctx, rr)
		if rr.ReconcileError == nil {
			// assign an empty map to indicate that no resources are managed anymore, so that the deletion of all managed resources is triggered
			managedResources = map[commonapi.ObjectReference][]client.Object{}
		}
	}

	// remove obsolete resources
	rr = c.deleteObsoleteResources(ctx, rr, managedResources)

	// ensure that all managed resources are in the (Cluster)Replica's status
	for clusterRef, resources := range managedResources {
		for _, res := range resources {
			rr.Object.GetStatus().Replicas.AddRaw(metav1.GroupVersionKind(res.GetObjectKind().GroupVersionKind()), res.GetNamespace(), res.GetName(), &clusterRef)
		}
	}

	return rr
}

func (c *ReplicaController) handleCreateOrUpdate(ctx context.Context, rr ReconcileResult) (ReconcileResult, map[commonapi.ObjectReference][]client.Object) {
	log := logging.FromContextOrPanic(ctx)
	log.Debug("Handling creation/update")

	createCon := ctrlutils.GenerateCreateConditionFunc(&rr)

	// ensure finalizer on (Cluster)Replica
	old := rr.Object.DeepCopyReplicaEquivalent()
	if controllerutil.AddFinalizer(rr.Object, repv1alpha1.ReplicaFinalizer) {
		log.Info("Adding finalizer to (Cluster)Replica")
		if err := c.platformClient.Patch(ctx, rr.Object, client.MergeFrom(old)); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to add finalizer to (Cluster)Replica '%s': %w", rr.Object.NamespacedName(), err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(repv1alpha1.ConditionTypeMeta, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr, nil
		}
	}

	// parse template
	rawTemplate := string(*rr.Object.GetSpec().Template)
	tmpl, err := gotmpl.New("").Funcs(sprig.FuncMap()).Parse(rawTemplate)
	if err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to parse template: %w", wrapTemplateError(err, rawTemplate, nil, "")), cconst.ReasonConfigurationProblem)
		createCon(repv1alpha1.ConditionTypeMeta, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr, nil
	}

	// fetch source resources
	sources := map[string]*unstructured.Unstructured{}
	for _, src := range rr.Object.GetSpec().Sources {
		obj := &unstructured.Unstructured{}
		obj.SetGroupVersionKind(schema.GroupVersionKind{
			Group:   src.Group,
			Version: src.Version,
			Kind:    src.Kind,
		})
		obj.SetName(src.Name)
		obj.SetNamespace(src.Namespace)
		if err := c.platformClient.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to fetch source resource '%s' (id: %s): %w", namespacedName(obj), src.ID, err), cconst.ReasonPlatformClusterInteractionProblem)
			createCon(SourceCondition(src.TypedObjectReference), metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
			return rr, nil
		}
		sources[src.ID] = obj
		createCon(SourceCondition(src.TypedObjectReference), metav1.ConditionTrue, repv1alpha1.ConditionReasonSourceRead, fmt.Sprintf("Source resource '%s' (id: %s) successfully read", namespacedName(obj), src.ID))
	}

	// list Cluster resources
	clusterList := &clustersv1alpha1.ClusterList{}
	if err := c.platformClient.List(ctx, clusterList); err != nil {
		rr.ReconcileError = errutils.WithReason(fmt.Errorf("unable to list Cluster resources: %w", err), cconst.ReasonPlatformClusterInteractionProblem)
		createCon(repv1alpha1.ConditionTypeMeta, metav1.ConditionFalse, rr.ReconcileError.Reason(), rr.ReconcileError.Error())
		return rr, nil
	}
	createCon(repv1alpha1.ConditionTypeMeta, metav1.ConditionTrue, "", "")

	managedResources := map[commonapi.ObjectReference][]client.Object{}
	errs := errutils.NewReasonableErrorList()
	for _, targetDef := range rr.Object.GetSpec().Targets {
		// filter Cluster resources with selector
		var matchedClusters []*clustersv1alpha1.Cluster
		if targetDef.Cluster != nil {
			for i := range clusterList.Items {
				cluster := &clusterList.Items[i]
				if targetDef.Cluster.Selector == nil || targetDef.Cluster.Selector.Matches(cluster) {
					matchedClusters = append(matchedClusters, cluster)
				}
			}
		} else {
			// a nil cluster points to the hosting cluster
			matchedClusters = []*clustersv1alpha1.Cluster{nil}
		}

		for _, targetCluster := range matchedClusters {
			// get cluster access from provider
			clusterName := provider.ClusterNameFromCluster(targetCluster)
			logClusterName := string(clusterName)
			if logClusterName == string(provider.HostingPlatformCluster) {
				logClusterName = HostingPlatformClusterNameForLogging
			}
			clog := log.WithValues("cluster", logClusterName)

			if targetCluster != nil && !targetCluster.GetDeletionTimestamp().IsZero() {
				clog.Debug("Cluster is in deletion, triggering deletion of resource replicas on it")
				continue
			}

			access, err := c.provider.Get(ctx, clusterName)
			if err != nil {
				rerr := errutils.WithReason(fmt.Errorf("unable to get access to target cluster '%s': %w", logClusterName, err), provider.ReasonClusterAccessError)
				errs.Append(rerr)
				createCon(ClusterCondition(commonapi.ReferenceFromObject(targetCluster)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
				continue
			}

			// resolve target namespaces
			var targetNamespaces []string // empty string means "no namespace selector, template must specify it"
			if targetDef.Namespace != nil {
				nsList := &corev1.NamespaceList{}
				if err := access.GetClient().List(ctx, nsList); err != nil {
					rerr := errutils.WithReason(fmt.Errorf("unable to list namespaces in cluster '%s': %w", logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
					errs.Append(rerr)
					createCon(ClusterCondition(commonapi.ReferenceFromObject(targetCluster)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
					continue
				}
				for _, ns := range nsList.Items {
					if targetDef.Namespace.Selector == nil {
						targetNamespaces = append(targetNamespaces, ns.Name)
					} else {
						ok, err := targetDef.Namespace.Selector.Matches(&ns)
						if err != nil {
							errs.Append(errutils.WithReason(fmt.Errorf("unable to match namespace selector against namespace '%s' in cluster '%s': %w", ns.Name, logClusterName, err), cconst.ReasonConfigurationProblem))
							// no condition in this case, but this should happen rarely enough so that this is acceptable
							continue
						}
						if ok {
							targetNamespaces = append(targetNamespaces, ns.Name)
						}
					}
				}
			} else {
				targetNamespaces = []string{""}
			}

			clusterConUsedForTemplateError := false
			for _, targetNamespace := range targetNamespaces {
				// render template
				var rendered *unstructured.Unstructured
				if rr.Object.GetSpec().Template == nil {
					// no template: exact copy of the single source
					src := sources[rr.Object.GetSpec().Sources[0].ID]
					rendered = src.DeepCopy()
					rendered.SetUID("")
					rendered.SetResourceVersion("")
					rendered.SetGeneration(0)
					rendered.SetCreationTimestamp(metav1.Time{})
					rendered.SetDeletionTimestamp(nil)
					rendered.SetFinalizers(nil)
					rendered.SetOwnerReferences(nil)
					delete(rendered.Object, "status")
					if targetNamespace != "" {
						rendered.SetNamespace(targetNamespace)
					}
				} else {
					// build template data
					sourcesData := map[string]any{}
					for id, src := range sources {
						sourcesData[id] = src.Object
					}
					targetData := map[string]any{}
					if targetCluster != nil {
						targetData["cluster"] = map[string]any{
							"name":      targetCluster.Name,
							"namespace": targetCluster.Namespace,
							"purpose":   targetCluster.Spec.Purposes,
						}
					} else {
						targetData["cluster"] = map[string]any{}
					}
					if targetNamespace != "" {
						targetData["namespace"] = targetNamespace
					}
					tmplData := map[string]any{
						"sources": sourcesData,
						"target":  targetData,
						"replica": map[string]any{
							"name":        rr.Object.GetName(),
							"namespace":   rr.Object.GetNamespace(),
							"labels":      rr.Object.GetLabels(),
							"annotations": rr.Object.GetAnnotations(),
						},
					}

					var buf bytes.Buffer
					if err := tmpl.Execute(&buf, tmplData); err != nil {
						rerr := errutils.WithReason(fmt.Errorf("unable to execute template: %w", wrapTemplateError(err, rawTemplate, tmplData, "")), cconst.ReasonConfigurationProblem)
						errs.Append(rerr)
						if !clusterConUsedForTemplateError {
							createCon(ClusterCondition(commonapi.ReferenceFromObject(targetCluster)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
							clusterConUsedForTemplateError = true
						}
						continue
					}
					renderedRaw := buf.Bytes()
					rendered = &unstructured.Unstructured{}
					if err := yaml.Unmarshal(renderedRaw, rendered); err != nil {
						rerr := errutils.WithReason(fmt.Errorf("unable to unmarshal rendered template: %w", wrapTemplateError(err, rawTemplate, tmplData, string(renderedRaw))), cconst.ReasonConfigurationProblem)
						errs.Append(rerr)
						if !clusterConUsedForTemplateError {
							createCon(ClusterCondition(commonapi.ReferenceFromObject(targetCluster)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
							clusterConUsedForTemplateError = true
						}
						continue
					}
					if targetNamespace != "" {
						rendered.SetNamespace(targetNamespace)
					}
				}

				renderedGVK := rendered.GroupVersionKind()
				tlog := clog.WithValues("targetName", rendered.GetName(), "targetNamespace", rendered.GetNamespace(), "targetGroup", renderedGVK.Group, "targetVersion", renderedGVK.Version, "targetKind", renderedGVK.Kind)

				// set management labels/annotations
				labels := rendered.GetLabels()
				if labels == nil {
					labels = map[string]string{}
				}
				labels[openmcpconst.ManagedByLabel] = c.providerName
				labels[repv1alpha1.ReplicaSourceKindLabel] = rr.Object.ReplicaKind()
				labels[repv1alpha1.ReplicaSourceGenerationLabel] = fmt.Sprintf("%d", rr.Object.GetGeneration())
				labels[repv1alpha1.ReplicaSourceNameLabel] = rr.Object.GetName()
				if rr.Object.ReplicaKind() == repv1alpha1.KindReplica {
					labels[repv1alpha1.ReplicaSourceNamespaceLabel] = rr.Object.GetNamespace()
				}

				// check if target resource already exists
				existing := &unstructured.Unstructured{}
				existing.SetGroupVersionKind(rendered.GroupVersionKind())
				existing.SetName(rendered.GetName())
				existing.SetNamespace(rendered.GetNamespace())
				if err := access.GetClient().Get(ctx, client.ObjectKeyFromObject(existing), existing); err != nil {
					if !apierrors.IsNotFound(err) {
						rerr := errutils.WithReason(fmt.Errorf("unable to check existence of target resource '%s' (%s) in cluster '%s': %w", client.ObjectKeyFromObject(existing).String(), existing.GetObjectKind().GroupVersionKind().String(), logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
						errs.Append(rerr)
						createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
						continue
					}
					existing = nil
				}
				if existing != nil {
					// check if the existing resource is owned by the (Cluster)Replica
					// handle according to the target definition's target conflict policy if not
					ownedByThis := existing.GetLabels()[repv1alpha1.ReplicaSourceKindLabel] == rr.Object.ReplicaKind() &&
						existing.GetLabels()[repv1alpha1.ReplicaSourceNameLabel] == rr.Object.GetName() &&
						existing.GetLabels()[repv1alpha1.ReplicaSourceNamespaceLabel] == rr.Object.GetNamespace()
					if !ownedByThis {
						switch targetDef.GetTargetConflictPolicy() {
						case repv1alpha1.TargetConflictPolicyOverwrite:
							// return an error if the existing resource is in deletion, as we cannot overwrite it in that case
							if !existing.GetDeletionTimestamp().IsZero() {
								rerr := errutils.WithReason(fmt.Errorf("target resource '%s' (%s) in cluster '%s' already exists, is not owned by this (Cluster)Replica, and is in deletion", client.ObjectKeyFromObject(existing).String(), existing.GetObjectKind().GroupVersionKind().String(), logClusterName), repv1alpha1.ReasonTargetConflict)
								errs.Append(rerr)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
								continue
							}
							// fall through to create/update
							tlog.Info("Overwriting existing target resource which is currently not owned by this (Cluster)Replica")
						case repv1alpha1.TargetConflictPolicySkip:
							tlog.Info("Skipping target resource that already exists and is not owned by this (Cluster)Replica")
							createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionTrue, repv1alpha1.ConditionReasonTargetSkipped, fmt.Sprintf("Target resource '%s' (%s) in cluster '%s' already exists and is not owned by this (Cluster)Replica, skipping", client.ObjectKeyFromObject(existing).String(), existing.GetObjectKind().GroupVersionKind().String(), logClusterName))
							continue
						case repv1alpha1.TargetConflictPolicyFail:
							rerr := errutils.WithReason(fmt.Errorf("target resource '%s' (%s) in cluster '%s' already exists and is not owned by this (Cluster)Replica", client.ObjectKeyFromObject(existing).String(), existing.GetObjectKind().GroupVersionKind().String(), logClusterName), cconst.ReasonConfigurationProblem)
							errs.Append(rerr)
							createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
							continue
						}
					} else {
						createdForThisCluster := managedResources[commonapi.ReferenceFromObject(targetCluster)]
						for _, obj := range createdForThisCluster {
							if obj.GetName() == rendered.GetName() &&
								obj.GetNamespace() == rendered.GetNamespace() &&
								obj.GetObjectKind().GroupVersionKind().Group == renderedGVK.Group &&
								obj.GetObjectKind().GroupVersionKind().Version == renderedGVK.Version &&
								obj.GetObjectKind().GroupVersionKind().Kind == renderedGVK.Kind {
								// This happens if multiple target definitions in the same (Cluster)Replica result in the same target resource.
								// As the generated resources can differ, this must always result in an error.
								rerr := errutils.WithReason(fmt.Errorf("target resource '%s' (%s) in cluster '%s' is created by this (Cluster)Replica multiple times", client.ObjectKeyFromObject(existing).String(), existing.GetObjectKind().GroupVersionKind().String(), logClusterName), repv1alpha1.ReasonTargetConflict)
								errs.Append(rerr)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
								continue
							}
						}
					}
				} else {
					// if the rendered resource is namespaced (= has a non-empty namespace), check if the target namespace exists in the target cluster
					// handle according to the target definition's namespace policy if not
					if rendered.GetNamespace() != "" {
						ns := &corev1.Namespace{}
						ns.Name = rendered.GetNamespace()
						if err := access.GetClient().Get(ctx, client.ObjectKeyFromObject(ns), ns); err != nil {
							if !apierrors.IsNotFound(err) {
								rerr := errutils.WithReason(fmt.Errorf("unable to check existence of namespace '%s' in cluster '%s': %w", ns.Name, logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
								errs.Append(rerr)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
								continue
							}
							// namespace does not exist
							switch targetDef.GetNamespacePolicy() {
							case repv1alpha1.NamespacePolicyCreate:
								if err := access.GetClient().Create(ctx, ns); err != nil {
									rerr := errutils.WithReason(fmt.Errorf("unable to create namespace '%s' in cluster '%s': %w", ns.Name, logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
									errs.Append(rerr)
									createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
									continue
								}
								tlog.Info("Created namespace in target cluster", "namespace", ns.Name)
							case repv1alpha1.NamespacePolicySkip:
								tlog.Info("Skipping target resource because target namespace does not exist", "namespace", ns.Name)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionTrue, repv1alpha1.ConditionReasonTargetSkipped, fmt.Sprintf("Target namespace '%s' in cluster '%s' does not exist, skipping", ns.Name, logClusterName))
								continue
							case repv1alpha1.NamespacePolicyFail:
								rerr := errutils.WithReason(fmt.Errorf("target namespace '%s' in cluster '%s' does not exist", ns.Name, logClusterName), repv1alpha1.ReasonMissingNamespace)
								errs.Append(rerr)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
								continue
							}
						} else if !ns.DeletionTimestamp.IsZero() {
							switch targetDef.GetNamespacePolicy() {
							case repv1alpha1.NamespacePolicyCreate, repv1alpha1.NamespacePolicyFail:
								rerr := errutils.WithReason(fmt.Errorf("target namespace '%s' in cluster '%s' is being deleted", ns.Name, logClusterName), repv1alpha1.ReasonNamespaceInDeletion)
								errs.Append(rerr)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
								continue
							case repv1alpha1.NamespacePolicySkip:
								tlog.Info("Skipping target resource because target namespace is being deleted", "namespace", ns.Name)
								createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionTrue, repv1alpha1.ConditionReasonTargetSkipped, fmt.Sprintf("Target namespace '%s' in cluster '%s' is being deleted, skipping", ns.Name, logClusterName))
								continue
							}
						}
					}
				}

				// create or update the target resource in the target cluster
				var clusterStatusRef *commonapi.ObjectReference
				if targetCluster != nil {
					ref := commonapi.ReferenceFromObject(targetCluster)
					clusterStatusRef = &ref
				}
				trackedInStatus := rr.Object.GetStatus().Replicas.ContainsRaw(metav1.GroupVersionKind(renderedGVK), rendered.GetNamespace(), rendered.GetName(), clusterStatusRef)

				if !trackedInStatus {
					// add to status before creating, so we don't lose track if something goes wrong afterwards
					rr.Object.GetStatus().Replicas.AddRaw(metav1.GroupVersionKind(renderedGVK), rendered.GetNamespace(), rendered.GetName(), clusterStatusRef)
					if err := c.platformClient.Status().Patch(ctx, rr.Object, client.MergeFrom(rr.OldObject)); err != nil {
						rerr := errutils.WithReason(fmt.Errorf("unable to update status of (Cluster)Replica '%s' before creating target resource: %w", rr.Object.NamespacedName(), err), cconst.ReasonPlatformClusterInteractionProblem)
						errs.Append(rerr)
						createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
						continue
					}
					rr.OldObject = rr.Object.DeepCopyReplicaEquivalent()
				}

				target := rendered.DeepCopy()
				opResult, err := controllerutil.CreateOrUpdate(ctx, access.GetClient(), target, func() error {
					for k, v := range rendered.Object {
						if k == "metadata" || k == "status" || k == "apiVersion" || k == "kind" {
							continue
						}
						target.Object[k] = v
					}
					for k := range target.Object {
						if k == "metadata" || k == "status" || k == "apiVersion" || k == "kind" {
							continue
						}
						if _, ok := rendered.Object[k]; !ok {
							delete(target.Object, k)
						}
					}
					target.SetLabels(rendered.GetLabels())
					target.SetAnnotations(rendered.GetAnnotations())
					return nil
				})
				if err != nil {
					rerr := errutils.WithReason(fmt.Errorf("unable to create/update target resource '%s' (%s) in cluster '%s': %w", namespacedName(rendered), renderedGVK.String(), logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
					errs.Append(rerr)
					createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
					continue
				}
				tlog.Info("Synced target resource", "operation", opResult)

				clusterRef := commonapi.ReferenceFromObject(targetCluster)
				managedResources[clusterRef] = append(managedResources[clusterRef], target)
				createCon(TargetCondition(commonapi.ReferenceFromObject(targetCluster), commonapi.TypedReferenceFromObject(rendered)), metav1.ConditionTrue, repv1alpha1.ConditionReasonTargetSynced, fmt.Sprintf("Target resource '%s' (%s) successfully synced to cluster '%s'", namespacedName(rendered), renderedGVK.String(), logClusterName))
			}

			if !clusterConUsedForTemplateError {
				createCon(ClusterCondition(commonapi.ReferenceFromObject(targetCluster)), metav1.ConditionTrue, repv1alpha1.ConditionReasonTargetClusterAccess, fmt.Sprintf("Successfully accessed cluster '%s'", logClusterName))
			}
		}
	}

	rr.ReconcileError = errs.Aggregate()
	return rr, managedResources
}

func (c *ReplicaController) handleDelete(ctx context.Context, rr ReconcileResult) ReconcileResult {

	return rr
}

// deleteObsoleteResources deletes resources that are no longer managed by the (Cluster)Replica.
// No-op if the managedResources map is nil, which indicates that an error occurred before the managed resources could be determined.
func (c *ReplicaController) deleteObsoleteResources(ctx context.Context, rr ReconcileResult, managedResources map[commonapi.ObjectReference][]client.Object) ReconcileResult {
	log := logging.FromContextOrPanic(ctx)

	if managedResources == nil {
		log.Debug("Skipping obsolete resource deletion because the currently managed resources could not be determined")
		return rr
	}
	log.Debug("Deleting obsolete resources")

	createCon := ctrlutils.GenerateCreateConditionFunc(&rr)

	// first, get a list of managed resources
	// The list in the status is the source of truth for this.
	hostingPlatformClusterRef := commonapi.ObjectReference{}
	resourcesToDelete := map[commonapi.ObjectReference][]commonapi.TypedObjectReference{}
	for _, copyWithType := range rr.Object.GetStatus().Replicas {
		for _, copy := range copyWithType.Resources {
			clusterRef := hostingPlatformClusterRef
			if copy.Cluster != nil {
				clusterRef = *copy.Cluster
			}
			resourcesToDelete[clusterRef] = append(resourcesToDelete[clusterRef], commonapi.TypedObjectReference{
				ObjectReferenceWithOptionalNamespace: copy.ObjectReferenceWithOptionalNamespace,
				GroupVersionKind:                     copyWithType.Type,
			})
		}
	}

	// ensure that there are no duplicates in the resourcesToDelete map
	// (should not happen, but will cause problems if it does)
	for clusterRef, resources := range resourcesToDelete {
		seen := map[commonapi.TypedObjectReference]bool{}
		uniqueResources := []commonapi.TypedObjectReference{}
		for _, res := range resources {
			if !seen[res] {
				seen[res] = true
				uniqueResources = append(uniqueResources, res)
			}
		}
		resourcesToDelete[clusterRef] = uniqueResources
	}

	// remove all resources for which there is a corresponding entry in the managedResources map
	for clusterRef, resources := range managedResources {
		for _, obj := range resources {
			// find the corresponding entry in resourcesToDelete, if any, and remove it
			resourcesForCluster := resourcesToDelete[clusterRef]
			for i, res := range resourcesForCluster {
				if res.Name == obj.GetName() &&
					res.Namespace == obj.GetNamespace() &&
					res.Group == obj.GetObjectKind().GroupVersionKind().Group &&
					res.Version == obj.GetObjectKind().GroupVersionKind().Version &&
					res.Kind == obj.GetObjectKind().GroupVersionKind().Kind {
					// remove from slice
					resourcesToDelete[clusterRef] = append(resourcesForCluster[:i], resourcesForCluster[i+1:]...)
					break
				}
			}
		}
	}

	// also remove all resources for which there is a condition to be updated
	// (this is to avoid deleting resources that failed to be created/updated)
	seenClusterConditions := map[string]metav1.ConditionStatus{}
	seenTargetConditions := map[string]metav1.ConditionStatus{}
	for _, conToBe := range rr.Conditions {
		if strings.HasPrefix(conToBe.Type, repv1alpha1.ConditionTypeClusterPrefix) {
			seenClusterConditions[conToBe.Type] = conToBe.Status
		} else if strings.HasPrefix(conToBe.Type, repv1alpha1.ConditionTypeTargetPrefix) {
			seenTargetConditions[conToBe.Type] = conToBe.Status
		}
	}
	// first, let's remove all clusters completely which have an unhealthy cluster condition
	for clusterRef := range resourcesToDelete {
		if status, ok := seenClusterConditions[ClusterCondition(clusterRef)]; ok && status == metav1.ConditionFalse {
			log.Debug("Skipping deletion of resources in cluster because there is an unhealthy cluster condition update", "cluster", clusterRef.NamespacedName().String())
			delete(resourcesToDelete, clusterRef)
		}
	}
	// then, all targets resources for which there is any condition update
	for clusterRef, resources := range resourcesToDelete {
		remainingResources := []commonapi.TypedObjectReference{}
		for _, res := range resources {
			if _, ok := seenTargetConditions[TargetCondition(clusterRef, res)]; ok {
				log.Debug("Skipping deletion of target resource because there is a condition update", "cluster", clusterRef.NamespacedName().String(), "targetName", res.Name, "targetNamespace", res.Namespace, "targetGroup", res.Group, "targetVersion", res.Version, "targetKind", res.Kind)
				continue
			}
			remainingResources = append(remainingResources, res)
		}
		resourcesToDelete[clusterRef] = remainingResources
	}

	// handle deletion of the identified obsolete resources
	errs := errutils.NewReasonableErrorList()
	for clusterRef, resources := range resourcesToDelete {
		clusterName := provider.ClusterNameFromReference(&clusterRef)
		logClusterName := string(clusterName)
		if logClusterName == string(provider.HostingPlatformCluster) {
			logClusterName = HostingPlatformClusterNameForLogging
		}
		access, err := c.provider.Get(ctx, provider.ClusterNameFromReference(&clusterRef))
		if err != nil {
			rerr := errutils.WithReason(fmt.Errorf("unable to get access to target cluster '%s' to delete resources: %w", logClusterName, err), provider.ReasonClusterAccessError)
			errs.Append(rerr)
			// create a condition for every resource we wanted to delete in this cluster, so that the user knows that we couldn't delete them
			for _, res := range resources {
				createCon(TargetCondition(clusterRef, res), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
			}
			continue
		}
		for _, res := range resources {
			rtd := &unstructured.Unstructured{}
			rtd.SetGroupVersionKind(schema.GroupVersionKind(res.GroupVersionKind))
			rtd.SetName(res.Name)
			rtd.SetNamespace(res.Namespace)
			if err := access.GetClient().Delete(ctx, rtd); err != nil {
				if !apierrors.IsNotFound(err) {
					rerr := errutils.WithReason(fmt.Errorf("unable to delete target resource '%s' (%s) in cluster '%s': %w", namespacedName(rtd), rtd.GetObjectKind().GroupVersionKind().String(), logClusterName, err), repv1alpha1.ReasonTargetClusterInteractionProblem)
					errs.Append(rerr)
					createCon(TargetCondition(clusterRef, res), metav1.ConditionFalse, rerr.Reason(), rerr.Error())
					continue
				}
				log.Debug("Obsolete target resource already deleted", "cluster", logClusterName, "targetName", res.Name, "targetNamespace", res.Namespace, "targetGroup", res.Group, "targetVersion", res.Version, "targetKind", res.Kind)
			} else {
				log.Info("Deleted target resource", "cluster", logClusterName, "targetName", res.Name, "targetNamespace", res.Namespace, "targetGroup", res.Group, "targetVersion", res.Version, "targetKind", res.Kind)
			}
			rr.ConditionsToRemove = append(rr.ConditionsToRemove, TargetCondition(clusterRef, res))

			// remove the resource from the status, if it is still there
			rr.Object.GetStatus().Replicas.RemoveRaw(res.GroupVersionKind, res.Namespace, res.Name, &clusterRef)
		}
	}

	errs.Append(rr.ReconcileError)
	rr.ReconcileError = errs.Aggregate()
	return rr
}

func wrapTemplateError(err error, template string, input map[string]any, output string) error {
	bld := errutils.TemplateErrorBuilder(err)
	if template != "" {
		bld.WithSource(&template)
	}
	bld.WithInput(input, errutils.NewTemplateInputFormatter(true))
	if output != "" {
		bld.WithFormattedOutput(&output)
	}
	return bld.Build()
}
