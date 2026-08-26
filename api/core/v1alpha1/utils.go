package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// Matches returns true if the given namespace matches the selector.
// If the selector is nil or empty, all namespaces match.
func (s *NamespaceSelector) Matches(ns *corev1.Namespace) (bool, error) {
	if s.Empty() {
		return true, nil
	}
	if s.MatchIdentities != nil {
		for _, id := range s.MatchIdentities {
			if id.Name == ns.Name {
				return true, nil
			}
		}
		return false, nil
	} else {
		sel, err := metav1.LabelSelectorAsSelector(&s.LabelSelector)
		if err != nil {
			return false, err
		}
		return sel.Matches(labels.Set(ns.Labels)), nil
	}
}

// Empty returns true if the selector is nil or empty (matches all namespaces).
func (s *NamespaceSelector) Empty() bool {
	if s == nil {
		return true
	}
	return s.MatchIdentities == nil && s.MatchLabels == nil && len(s.MatchExpressions) == 0
}

func clusterRefEqual(a, b *commonapi.ObjectReference) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return a.Name == b.Name && a.Namespace == b.Namespace
}

// AddRaw adds a new created resource to the list, if it is not already present.
// An empty cluster reference is treated as nil, as it is the same as the hosting platform cluster.
func (cr *CreatedResourcesWithType) AddRaw(namespace, name string, cluster *commonapi.ObjectReference) {
	if cluster != nil && cluster.Namespace == "" && cluster.Name == "" {
		cluster = nil
	}
	cr.Add(CreatedResource{
		Cluster: cluster,
		ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// Add adds a new created resource to the list, if it is not already present.
func (cr *CreatedResourcesWithType) Add(res CreatedResource) {
	for _, existing := range cr.Resources {
		if existing.Name == res.Name && existing.Namespace == res.Namespace && clusterRefEqual(existing.Cluster, res.Cluster) {
			// already present
			return
		}
	}
	cr.Resources = append(cr.Resources, res)
}

// RemoveRaw removes a created resource from the list, if it is present.
// An empty cluster reference is treated as nil, as it is the same as the hosting platform cluster.
func (cr *CreatedResourcesWithType) RemoveRaw(namespace, name string, cluster *commonapi.ObjectReference) {
	if cluster != nil && cluster.Namespace == "" && cluster.Name == "" {
		cluster = nil
	}
	cr.Remove(CreatedResource{
		Cluster: cluster,
		ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// Remove removes a created resource from the list, if it is present.
func (cr *CreatedResourcesWithType) Remove(c CreatedResource) {
	if cr == nil {
		return
	}
	for i, existing := range cr.Resources {
		if existing.Name == c.Name && existing.Namespace == c.Namespace && clusterRefEqual(existing.Cluster, c.Cluster) {
			// found, remove it
			cr.Resources = append(cr.Resources[:i], cr.Resources[i+1:]...)
			return
		}
	}
}

// AddRaw adds a created resource of the given type to the list, if it is not already present.
// An empty cluster reference is treated as nil, as it is the same as the hosting platform cluster.
func (l *CreatedResourcesWithTypeList) AddRaw(gvk metav1.GroupVersionKind, namespace, name string, cluster *commonapi.ObjectReference) {
	if cluster != nil && cluster.Namespace == "" && cluster.Name == "" {
		cluster = nil
	}
	l.Add(gvk, CreatedResource{
		Cluster: cluster,
		ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// Add adds a created resource of the given type to the list, if it is not already present.
func (l *CreatedResourcesWithTypeList) Add(gvk metav1.GroupVersionKind, res CreatedResource) {
	for i := range *l {
		if (*l)[i].Type == gvk {
			(*l)[i].Add(res)
			return
		}
	}
	*l = append(*l, CreatedResourcesWithType{
		Type:      gvk,
		Resources: []CreatedResource{res},
	})
}

// RemoveRaw removes a created resource of the given type from the list, if it is present.
// An empty cluster reference is treated as nil, as it is the same as the hosting platform cluster.
func (l *CreatedResourcesWithTypeList) RemoveRaw(gvk metav1.GroupVersionKind, namespace, name string, cluster *commonapi.ObjectReference) {
	if cluster != nil && cluster.Namespace == "" && cluster.Name == "" {
		cluster = nil
	}
	l.Remove(gvk, CreatedResource{
		Cluster: cluster,
		ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// Remove removes a created resource of the given type from the list, if it is present.
func (l *CreatedResourcesWithTypeList) Remove(gvk metav1.GroupVersionKind, res CreatedResource) {
	for i := range *l {
		if (*l)[i].Type == gvk {
			(*l)[i].Remove(res)
			return
		}
	}
}

// ContainsRaw returns true if the list contains the given created resource of the given type.
// An empty cluster reference is treated as nil, as it is the same as the hosting platform cluster.
func (l CreatedResourcesWithTypeList) ContainsRaw(gvk metav1.GroupVersionKind, namespace, name string, cluster *commonapi.ObjectReference) bool {
	if cluster != nil && cluster.Namespace == "" && cluster.Name == "" {
		cluster = nil
	}
	return l.Contains(gvk, CreatedResource{
		Cluster: cluster,
		ObjectReferenceWithOptionalNamespace: commonapi.ObjectReferenceWithOptionalNamespace{
			Name:      name,
			Namespace: namespace,
		},
	})
}

// Contains returns true if the list contains the given created resource of the given type.
func (l CreatedResourcesWithTypeList) Contains(gvk metav1.GroupVersionKind, res CreatedResource) bool {
	for i := range l {
		if l[i].Type == gvk {
			for _, existing := range l[i].Resources {
				if existing.Name == res.Name && existing.Namespace == res.Namespace && clusterRefEqual(existing.Cluster, res.Cluster) {
					return true
				}
			}
			return false
		}
	}
	return false
}

// +kubebuilder:object:generate=false
type ReplicaEquivalent interface {
	client.Object

	// GetSpec returns a pointer to the replica's spec.
	GetSpec() *ReplicaSpec
	// GetStatus returns a pointer to the replica's status.
	GetStatus() *ReplicaStatus
	// ReplicaKind returns the kind of the replica (either "Replica" or "ClusterReplica").
	ReplicaKind() string
	// DeepCopyReplicaEquivalent returns a deep copy of the replica.
	DeepCopyReplicaEquivalent() ReplicaEquivalent
	// NamespacedName returns a string containing namespace (in case of Replica) and name of the replica.
	// Format is '<namespace/name>' for Replica and '<name>' for ClusterReplica.
	NamespacedName() string
}
