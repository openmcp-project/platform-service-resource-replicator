package cluster

import (
	"context"
	"fmt"
	"reflect"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	mcreconcile "sigs.k8s.io/multicluster-runtime/pkg/reconcile"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	"github.com/openmcp-project/controller-utils/pkg/logging"
	clusterctrl "github.com/openmcp-project/multicluster-provider/pkg/cluster"
	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
	openmcpconst "github.com/openmcp-project/openmcp-operator/api/constants"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
	"github.com/openmcp-project/platform-service-resource-replicator/internal/shared"
)

// New creates a new ClusterHandler.
// It basically enqueues all Replica and ClusterReplica resources which have an association with the cluster whenever a cluster event is triggered.
// If the given selector is nil, all clusters are considered to be relevant, only the matching ones otherwise.
func New(selector clustersv1alpha1.Selector) *ClusterHandler {
	return &ClusterHandler{
		selector:      selector,
		clusterStates: map[commonapi.ObjectReference]clusterState{},
	}
}

type ClusterHandler struct {
	selector      clustersv1alpha1.Selector
	clusterStates map[commonapi.ObjectReference]clusterState
}

var _ clusterctrl.ClusterHandler = &ClusterHandler{}

// clusterState holds the relevant parts of a Cluster resource's state.
// This is used to avoid unnecessary re-enqueues of Replica resources when a cluster is reconciled but its relevant state has not changed.
type clusterState struct {
	annotations map[string]string // required because it can be used in templating
	labels      map[string]string // required because it can be used in templating
	inDeletion  bool
	generation  int64
}

func (c *ClusterHandler) clusterStateChanged(cluster *clustersv1alpha1.Cluster) bool {
	if cluster == nil {
		return true
	}
	ref := commonapi.ReferenceFromObject(cluster)
	old, ok := c.clusterStates[ref]
	if !ok {
		return true
	}
	newState := clusterState{
		annotations: cluster.Annotations,
		labels:      cluster.Labels,
		inDeletion:  !cluster.DeletionTimestamp.IsZero(),
		generation:  cluster.Generation,
	}
	if !reflect.DeepEqual(old, newState) {
		c.clusterStates[ref] = newState
		return true
	}
	return false
}

// IsResponsibleFor implements [cluster.ClusterHandler].
func (c *ClusterHandler) IsResponsibleFor(ctx context.Context, req mcreconcile.Request, platformClient client.Client, cluster *clustersv1alpha1.Cluster) bool {
	if cluster == nil || c.selector == nil {
		return true
	}
	return c.selector.Matches(cluster)
}

// HandleCreateOrUpdate implements [cluster.ClusterHandler].
func (c *ClusterHandler) HandleCreateOrUpdate(ctx context.Context, req mcreconcile.Request, platformClient client.Client, cluster *clustersv1alpha1.Cluster, access cluster.Cluster) (reconcile.Result, error) {
	var err error
	if c.clusterStateChanged(cluster) {
		err = c.enqueueAllReplicasForCluster(ctx, platformClient, cluster)
	}
	return reconcile.Result{}, err
}

// HandleDelete implements [cluster.ClusterHandler].
func (c *ClusterHandler) HandleDelete(ctx context.Context, req mcreconcile.Request, platformClient client.Client, cluster *clustersv1alpha1.Cluster, access cluster.Cluster) (reconcile.Result, error) {
	return reconcile.Result{}, c.enqueueAllReplicasForCluster(ctx, platformClient, cluster)
}

// AfterDeletion implements [cluster.ClusterHandler].
func (c *ClusterHandler) AfterDeletion(ctx context.Context, req mcreconcile.Request, platformClient client.Client) (reconcile.Result, error) {
	delete(c.clusterStates, commonapi.ObjectReference{Namespace: req.Namespace, Name: req.Name})
	return reconcile.Result{}, nil
}

// enqueueAllReplicasForCluster enqueues all Replica and ClusterReplica resources that are associated with the given cluster.
// 'associated' means that either the spec has a target selector that matches the cluster, or the status has a copy entry for the cluster.
func (c *ClusterHandler) enqueueAllReplicasForCluster(ctx context.Context, platformClient client.Client, cluster *clustersv1alpha1.Cluster) error {
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
	for _, replica := range namespacedReplicas.Items {
		if !ctrlutils.HasAnnotationWithValue(&replica, openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore) {
			replicas = append(replicas, &replica)
		}
	}
	for _, clusterReplica := range clusterReplicas.Items {
		if !ctrlutils.HasAnnotationWithValue(&clusterReplica, openmcpconst.OperationAnnotation, openmcpconst.OperationAnnotationValueIgnore) {
			replicas = append(replicas, &clusterReplica)
		}
	}

	for _, rep := range replicas {
		// Check if the replica is associated with the given cluster:
		// either via a status entry referencing the cluster, or via a spec target selector matching it.
		enqueued := false
		repStatus := rep.GetStatus()
		for _, copyWithType := range repStatus.Replicas {
			for _, copy := range copyWithType.Resources {
				// clusterMatch checks if there is a resource copy on the given cluster
				clusterMatch := copy.Cluster != nil && copy.Cluster.Namespace == cluster.Namespace && copy.Cluster.Name == cluster.Name
				// nextToMatch checks if the resource copy lives on the hosting platform cluster next to the given Cluster resource
				nextToMatch := copy.Cluster == nil && copy.Namespace == cluster.Namespace
				// isClusterScoped checks if the resource copy is cluster-scoped (i.e. has no namespace)
				isClusterScoped := copy.Namespace == ""
				if isClusterScoped && clusterMatch || nextToMatch {
					if rep.GetNamespace() == "" {
						log.Debug("Enqueuing ClusterReplica", "replica", rep.GetName(), "causingCopy", fmt.Sprintf("[%s]%s/%s", copyWithType.Type.String(), copy.Namespace, copy.Name))
					} else {
						log.Debug("Enqueuing Replica", "replica", fmt.Sprintf("%s/%s", rep.GetNamespace(), rep.GetName()), "causingCopy", fmt.Sprintf("[%s]%s/%s", copyWithType.Type.String(), copy.Namespace, copy.Name))
					}
					shared.SharedInformation().EnqueueReplica(log, rep)
					enqueued = true
					break
				}
			}
			if enqueued {
				break
			}
		}
		if !enqueued {
			for _, targetDef := range rep.GetSpec().Targets {
				if targetDef.Cluster != nil && (targetDef.Cluster.Selector == nil || targetDef.Cluster.Selector.Matches(cluster)) {
					if rep.GetNamespace() == "" {
						log.Debug("Enqueuing ClusterReplica due to matching target selector", "replica", rep.GetName())
					} else {
						log.Debug("Enqueuing Replica due to matching target selector", "replica", fmt.Sprintf("%s/%s", rep.GetNamespace(), rep.GetName()))
					}
					shared.SharedInformation().EnqueueReplica(log, rep)
					break
				}
			}
		}
	}

	return nil
}
