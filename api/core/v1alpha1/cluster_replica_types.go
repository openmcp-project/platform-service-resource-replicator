package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:scope=Cluster,shortName=clusterrep;crep
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type ClusterReplica struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReplicaSpec   `json:"spec,omitempty"`
	Status ReplicaStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true
type ClusterReplicaList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClusterReplica `json:"items"`
}

func init() {
	RegisterToSchemeBuilder(&ClusterReplica{}, &ClusterReplicaList{})
}

var _ ReplicaEquivalent = &ClusterReplica{}

func (cr *ClusterReplica) GetSpec() *ReplicaSpec {
	return &cr.Spec
}

func (cr *ClusterReplica) GetStatus() *ReplicaStatus {
	return &cr.Status
}

func (cr *ClusterReplica) ReplicaKind() string {
	return KindClusterReplica
}

func (cr *ClusterReplica) DeepCopyReplicaEquivalent() ReplicaEquivalent {
	return cr.DeepCopy()
}

func (cr *ClusterReplica) NamespacedName() string {
	return cr.Name
}
