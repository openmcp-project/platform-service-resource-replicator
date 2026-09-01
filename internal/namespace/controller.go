package namespace

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcbuilder "sigs.k8s.io/multicluster-runtime/pkg/builder"
	mcmanager "sigs.k8s.io/multicluster-runtime/pkg/manager"
	"sigs.k8s.io/multicluster-runtime/pkg/multicluster"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	"github.com/openmcp-project/multicluster-provider/pkg/provider"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/shared"
)

const ControllerName = "Namespace"

// NamespaceController watches Namespace resources on the platform cluster and all provider clusters.
// When a namespace is created or its labels change, it enqueues all Replica and ClusterReplica resources
// whose spec has a TargetDefinition with a namespace selector that matches the affected namespace,
// as well as any that have resources already created in that namespace (on that cluster) in their status.
type NamespaceController struct {
	provider multicluster.Provider
}

func New(prov multicluster.Provider) *NamespaceController {
	return &NamespaceController{provider: prov}
}

var _ mcreconcile.Reconciler = &NamespaceController{}

// Reconcile implements [mcreconcile.Reconciler].
func (c *NamespaceController) Reconcile(ctx context.Context, req mcreconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx).WithName(ControllerName).WithValues("targetNamespace", req.Name, "cluster", req.ClusterName)
	ctx = logging.NewContext(ctx, log)
	log.Debug("Starting reconcile")
	return c.reconcile(ctx, req)
}

func (c *NamespaceController) reconcile(ctx context.Context, req mcreconcile.Request) (reconcile.Result, error) {
	log := logging.FromContextOrPanic(ctx)

	// getting cluster access
	clusterAccess, err := c.provider.Get(ctx, req.ClusterName)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to get access to cluster '%s': %w", req.ClusterName, err)
	}

	ns := &corev1.Namespace{}
	if err := clusterAccess.GetClient().Get(ctx, client.ObjectKey{Name: req.Name}, ns); err != nil {
		if apierrors.IsNotFound(err) {
			log.Debug("Namespace not found, skipping")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("unable to get Namespace '%s': %w", req.Name, err)
	}

	platformCluster, err := c.provider.Get(ctx, provider.HostingPlatformCluster)
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("unable to get access to platform cluster: %w", err)
	}

	return reconcile.Result{}, c.enqueueMatchingReplicas(ctx, ns, req.ClusterName, platformCluster.GetClient())
}

func (c *NamespaceController) enqueueMatchingReplicas(ctx context.Context, ns *corev1.Namespace, clusterName multicluster.ClusterName, platformClient client.Client) error {
	log := logging.FromContextOrPanic(ctx)

	log.Debug("Listing all Replica resources")
	namespacedReplicas := &repv1alpha1.ReplicaList{}
	if err := platformClient.List(ctx, namespacedReplicas); err != nil {
		return fmt.Errorf("failed to list Replica resources: %w", err)
	}

	log.Debug("Listing all ClusterReplica resources")
	clusterReplicas := &repv1alpha1.ClusterReplicaList{}
	if err := platformClient.List(ctx, clusterReplicas); err != nil {
		return fmt.Errorf("failed to list ClusterReplica resources: %w", err)
	}

	replicas := make([]repv1alpha1.ReplicaEquivalent, 0, len(namespacedReplicas.Items)+len(clusterReplicas.Items))
	for i := range namespacedReplicas.Items {
		if !ctrlutils.HasAnnotationWithValue(&namespacedReplicas.Items[i], openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore) {
			replicas = append(replicas, &namespacedReplicas.Items[i])
		}
	}
	for i := range clusterReplicas.Items {
		if !ctrlutils.HasAnnotationWithValue(&clusterReplicas.Items[i], openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore) {
			replicas = append(replicas, &clusterReplicas.Items[i])
		}
	}

	for _, rep := range replicas {
		if c.replicaIsRelevantForNamespaceEvent(ctx, rep, ns, clusterName) {
			log.Debug("Enqueuing replica due to namespace event", "replica", rep.NamespacedName(), "replicaKind", rep.ReplicaKind())
			shared.SharedInformation().EnqueueReplica(log, rep)
		}
	}

	return nil
}

// replicaIsRelevantForNamespaceEvent returns true if the replica should be re-queued due to a namespace create/label-change event.
// This is the case if:
// - any target definition's namespace selector matches the affected namespace, or
// - any resource in the replica's status was created in the affected namespace on the affected cluster.
func (c *NamespaceController) replicaIsRelevantForNamespaceEvent(ctx context.Context, rep repv1alpha1.ReplicaEquivalent, ns *corev1.Namespace, clusterName multicluster.ClusterName) bool {
	log := logging.FromContextOrPanic(ctx)

	for _, targetDef := range rep.GetSpec().Targets {
		if targetDef.Namespace == nil || targetDef.Namespace.Selector == nil {
			continue
		}
		matches, err := targetDef.Namespace.Selector.Matches(ns)
		if err != nil {
			log.Error(err, "Failed to evaluate namespace selector", "replica", rep.NamespacedName())
			continue
		}
		if matches {
			return true
		}
	}

	for _, copyWithType := range rep.GetStatus().Replicas {
		for _, copy := range copyWithType.Resources {
			if copy.Namespace != ns.Name {
				continue
			}
			if clusterName == provider.HostingPlatformCluster && copy.Cluster == nil {
				return true
			} else if copy.Cluster != nil && provider.ClusterName(copy.Cluster.Namespace, copy.Cluster.Name) == clusterName {
				return true
			}
		}
	}

	return false
}

func (c *NamespaceController) SetupWithMulticlusterManager(mgr mcmanager.Manager) error {
	startTime := time.Now()
	return mcbuilder.ControllerManagedBy(mgr).
		For(&corev1.Namespace{},
			mcbuilder.WithEngageWithLocalCluster(true),
			mcbuilder.WithEngageWithProviderClusters(true),
			mcbuilder.WithPredicates(predicate.Or(
				// only react to namespaces that were created after this controller started up,
				// to avoid triggering a reconcile storm for all pre-existing namespaces on startup
				predicate.Funcs{CreateFunc: func(e event.CreateEvent) bool {
					return e.Object.GetCreationTimestamp().After(startTime)
				}},
				predicate.And(
					ctrlutils.OnUpdatePredicate(),
					predicate.LabelChangedPredicate{},
				),
			)),
		).
		Complete(c)
}
