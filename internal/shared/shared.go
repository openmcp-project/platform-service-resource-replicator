package shared

import (
	"sync"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"

	"github.com/openmcp-project/controller-utils/pkg/logging"
)

var (
	sharedInstance *sharedInformation
	sharedOnce     sync.Once
)

// sharedInformation contains information which is shared between multiple controllers.
// All access to it should happen via its methods, which need to be thread-safe.
type sharedInformation struct {
	lock sync.RWMutex

	replicaNotificationChannel chan event.TypedGenericEvent[client.Object]
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
}

// EnqueueReplica enqueues the given (Cluster)Replica for reconciliation.
func (si *sharedInformation) EnqueueReplica(log logging.Logger, obj client.Object) {
	select {
	case si.replicaNotificationChannel <- event.TypedGenericEvent[client.Object]{Object: obj}:
	default:
		log.Error(nil, "Replica notification channel is full, dropping enqueue request", "name", obj.GetName(), "namespace", obj.GetNamespace())
	}
}

func (si *sharedInformation) GetReplicaNotificationChannel() <-chan event.TypedGenericEvent[client.Object] {
	si.lock.RLock()
	defer si.lock.RUnlock()

	return si.replicaNotificationChannel
}
