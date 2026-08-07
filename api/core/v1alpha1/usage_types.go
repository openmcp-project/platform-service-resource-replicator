package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

type ReplicaSpec struct {
	// TODO
}

type ResourceReference struct {
	metav1.GroupVersionKind   `json:",inline"`
	commonapi.ObjectReference `json:",inline"`
}

type ReplicaStatus struct {
	// TODO
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=rep
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:subresource:status
type Replica struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicaSpec   `json:"spec,omitempty"`
	Status ReplicaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ReplicaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Replica `json:"items"`
}

func init() {
	RegisterToSchemeBuilder(&Replica{}, &ReplicaList{})
}
