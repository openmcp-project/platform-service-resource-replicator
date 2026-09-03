package shared

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/sets"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/openmcp-project/controller-utils/pkg/logging"
)

// DeduplicateReplicaEnqueues controls whether duplicate enqueue requests for the same (Cluster)Replica are suppressed.
// When enabled, a replica that is already pending reconciliation will not be enqueued a second time until its current reconcile starts.
// Set to false to disable deduplication (e.g. for debugging).
const DeduplicateReplicaEnqueues = true

var (
	sharedInstance *sharedInformation
	sharedOnce     sync.Once
)

// sharedInformation contains information which is shared between multiple controllers.
// All access to it should happen via its methods, which need to be thread-safe.
type sharedInformation struct {
	lock sync.RWMutex

	replicaNotificationChannel chan event.TypedGenericEvent[client.Object]
	pendingReplicas            sets.Set[string]
}

func SharedInformation() *sharedInformation {
	sharedOnce.Do(func() {
		sharedInstance = &sharedInformation{}
		sharedInstance.Reset() // initialize all fields to their default values
	})
	return sharedInstance
}

// Reset resets the state of the shared information to its initial state.
// THIS IS ONLY FOR TESTING PURPOSES AND SHOULD NOT BE CALLED IN PRODUCTION CODE.
func (si *sharedInformation) Reset() {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.replicaNotificationChannel = make(chan event.TypedGenericEvent[client.Object], 1000)
	si.pendingReplicas = sets.New[string]()
}

func replicaKeyFromObject(obj client.Object) string {
	return replicaKeyFromNamespacedName(types.NamespacedName{Namespace: obj.GetNamespace(), Name: obj.GetName()})
}

func replicaKeyFromNamespacedName(nn types.NamespacedName) string {
	return nn.Namespace + "/" + nn.Name
}

// EnqueueReplica enqueues the given (Cluster)Replica for reconciliation.
// When DeduplicateReplicaEnqueues is true, duplicate requests for a replica that is
// already pending are silently dropped.
func (si *sharedInformation) EnqueueReplica(log logging.Logger, obj client.Object) {
	if DeduplicateReplicaEnqueues {
		key := replicaKeyFromObject(obj)
		si.lock.Lock()
		alreadyPending := si.pendingReplicas.Has(key)
		if !alreadyPending {
			si.pendingReplicas.Insert(key)
		}
		si.lock.Unlock()
		if alreadyPending {
			return
		}
	}

	select {
	case si.replicaNotificationChannel <- event.TypedGenericEvent[client.Object]{Object: obj}:
	default:
		if DeduplicateReplicaEnqueues {
			si.lock.Lock()
			defer si.lock.Unlock()
			si.pendingReplicas.Delete(replicaKeyFromObject(obj))
		}
		log.Error(nil, "Replica notification channel is full, dropping enqueue request", "name", obj.GetName(), "namespace", obj.GetNamespace())
	}
}

// ClearPending removes the given (Cluster)Replica from the set of pending replicas, allowing it to be enqueued again.
// Should be called at the beginning of Reconcile for reconciliations triggered via the shared notification channel.
// This is a no-op when DeduplicateReplicaEnqueues is false.
func (si *sharedInformation) ClearPending(nn types.NamespacedName) {
	if !DeduplicateReplicaEnqueues {
		return
	}
	si.lock.Lock()
	defer si.lock.Unlock()
	si.pendingReplicas.Delete(replicaKeyFromNamespacedName(nn))
}

func (si *sharedInformation) GetReplicaNotificationChannel() <-chan event.TypedGenericEvent[client.Object] {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.replicaNotificationChannel
}
