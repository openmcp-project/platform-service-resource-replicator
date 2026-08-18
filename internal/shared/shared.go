package shared

import (
	"sync"
)

var sharedInstance *sharedInformation

// sharedInformation contains information which is shared between multiple controllers.
// All access to it should happen via its methods, which need to be thread-safe.
type sharedInformation struct {
	lock sync.RWMutex
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

}

// EnqueueReplica enqueues the referenced Replica for reconciliation.
// If the given namespace is empty, the reference is assumed to point to a ClusterReplica instead of a Replica.
func (si *sharedInformation) EnqueueReplica(namespace, name string) {
	// TODO
}
