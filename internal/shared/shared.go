package shared

import (
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

var sharedInstance *sharedInformation

// sharedInformation contains information which is shared between multiple controllers.
// All access to it should happen via its methods, which need to be thread-safe.
type sharedInformation struct {
	lock sync.RWMutex

	replicaNotificationChannel chan event.TypedGenericEvent[client.Object]
}

func SharedInformation() *sharedInformation {
	if sharedInstance == nil {
		sharedInstance = &sharedInformation{
			lock: sync.RWMutex{},
		}
		sharedInstance.Reset() // initialize all fields to their default values
	}
	return sharedInstance
}

// Reset resets the state of the shared information to its initial state.
// THIS IS ONLY FOR TESTING PURPOSES AND SHOULD NOT BE CALLED IN PRODUCTION CODE.
func (si *sharedInformation) Reset() {
	si.lock.Lock()
	defer si.lock.Unlock()

	si.replicaNotificationChannel = make(chan event.TypedGenericEvent[client.Object], 1000)
}

// EnqueueReplica enqueues the referenced Replica for reconciliation.
// If the given namespace is empty, the reference is assumed to point to a ClusterReplica instead of a Replica.
func (si *sharedInformation) EnqueueReplica(obj client.Object) {
	si.replicaNotificationChannel <- event.TypedGenericEvent[client.Object]{Object: obj}
}

func (si *sharedInformation) GetReplicaNotificationChannel() <-chan event.TypedGenericEvent[client.Object] {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.replicaNotificationChannel
}
