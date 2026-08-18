package replica

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ctrlutils "github.com/openmcp-project/controller-utils/pkg/controller"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"

	repv1alpha1 "github.com/openmcp-project/platform-service-resource-replicator/api/core/v1alpha1"
)

// SourceCondition generates a condition type for a specific source.
// To make this unique, it must contain GVK and namespace + name of the source.
func SourceCondition(ref commonapi.TypedObjectReference) string {
	// the limit for condition types seems to be a little bit more than 300
	return ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s%s_%s", repv1alpha1.ConditionTypeSourcePrefix, namespacedNameForConType(ref.NamespacedName()), gvkForConType(ref.GroupVersionKind)), 300)
}

// TargetCondition generates a condition type for a created target.
// For uniqueness, the type must contain GVK and namespace + name of the target resource, as well as the Cluster the target lives on.
func TargetCondition(clusterRef commonapi.ObjectReference, ref commonapi.TypedObjectReference) string {
	// the limit for condition types seems to be a little bit more than 300
	return ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s%s_%s_%s", repv1alpha1.ConditionTypeTargetPrefix, namespacedNameForConType(clusterRef.NamespacedName()), namespacedNameForConType(ref.NamespacedName()), gvkForConType(ref.GroupVersionKind)), 300)
}

// ClusterCondition generates a condition type for a specific cluster.
// For uniqueness, the type must contain the name and namespace of the cluster.
func ClusterCondition(clusterRef commonapi.ObjectReference) string {
	// the limit for condition types seems to be a little bit more than 300
	return ctrlutils.ShortenToXCharactersUnsafe(fmt.Sprintf("%s%s", repv1alpha1.ConditionTypeClusterPrefix, namespacedNameForConType(clusterRef.NamespacedName())), 300)
}

func gvkForConType(gvk metav1.GroupVersionKind) string {
	return fmt.Sprintf("%s.%s.%s", gvk.Kind, gvk.Version, gvk.Group)
}

func namespacedNameForConType(nn types.NamespacedName) string {
	return strings.TrimPrefix(fmt.Sprintf("%s/%s", nn.Namespace, nn.Name), ".")
}

func namespacedName(obj client.Object) string {
	return strings.TrimPrefix(fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName()), "/")
}
