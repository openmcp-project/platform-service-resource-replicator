package v1alpha1

import (
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"

	clustersv1alpha1 "github.com/openmcp-project/openmcp-operator/api/clusters/v1alpha1"
	commonapi "github.com/openmcp-project/openmcp-operator/api/common"
)

// RawTemplate is a string that can be unmarshaled from either a JSON string or a JSON object/array (stored as raw JSON).
type RawTemplate string

func (t *RawTemplate) UnmarshalJSON(data []byte) error {
	// Try a plain JSON string first.
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*t = RawTemplate(s)
		return nil
	}
	// It's a JSON object or array, convert it to YAML and store it as a string.
	// Converting to YAML has two advantages:
	// 1. The template looks more similar to full-string-templates, which are likely specified in YAML format.
	// 2. YAML is less likely to contain stacked curly braces, which would interfere with the templating.
	yamlData, err := yaml.JSONToYAML(data)
	if err != nil {
		return err
	}
	*t = RawTemplate(yamlData)
	return nil
}

func (t RawTemplate) MarshalJSON() ([]byte, error) {
	return json.Marshal(string(t))
}

// +kubebuilder:validation:XValidation:rule="has(self.template) || size(self.sources) == 1",message="template is required when sources has more than one entry"
type ReplicaSpec struct {
	// Sources is a list of references to the resources which should be combined into the replica.
	// +kubebuilder:validation:MinItems=1
	// +listType=map
	// +listMapKey=id
	Sources []ReferenceWithID `json:"sources"`

	// Template is a template for the target resource's manifest.
	// It can be omitted, if sources contains exactly one entry, and the target resource should be an exact copy of that source resource.
	// 'Exact copy' excludes finalizers, owner references, status, and other fields where copying does not make sense (e.g. UID).
	//
	// Note: If the target specifies a namespace, the 'metadata.namespace' field of the template will be ignored and the target namespace will be used instead.
	// Otherwise, the template must specify a namespace, unless the resource is cluster-scoped, or the template is omitted (in which case the source's namespace will be used).
	//
	// Template is expected to be a go template.
	// It may be specified as an inline YAML mapping (with only some field values being templated, if any) or as a plain string.
	// If it is specified as yaml, it will be converted to a string before being passed to the template engine.
	//
	// Available bindings in the template:
	// - sources.<id>: Manifest of the resource referenced by the source with the given ID.
	// - target: Information about the target resource which is currently being rendered. It contains the following fields:
	//   - cluster: Information about the cluster the target resource is being rendered for. Can be empty, in which case the platform cluster is targeted. It contains the following fields:
	//     - name: Name of the Cluster resource.
	//     - namespace: Namespace of the Cluster resource.
	//     - purpose: spec.purpose of the Cluster resource.
	//   - namespace: Namespace of the target resource. Only available if a namespace selector is specified in the target definition; not set if the namespace is computed by the template itself.
	// - replica: Metadata information about this Replica resource. Contains fields for name, namespace, labels, and annotations.
	//
	// +optional
	Template *RawTemplate `json:"template,omitempty"`

	// Targets is a list of targets where replicas should be created.
	// Note that a single target can also resolve to multiple actual targets, e.g. if a cluster selector matches multiple clusters.
	// Targets should be disjunct, multiple target definitions causing the same target resource to be created will result in an error.
	// +kubebuilder:validation:MinItems=1
	Targets []TargetDefinition `json:"targets"`
}

type ReferenceWithID struct {
	commonapi.TypedObjectReference `json:",inline"`

	// ID is a unique identifier for the reference.
	// It is used to refer to the resource in the template.
	// +kubebuilder:validation:MinLength=1
	ID string `json:"id"`
}

// +kubebuilder:validation:XValidation:rule="has(self.cluster) || has(self.namespace)",message="at least one of cluster or namespace must be set"
// +kubebuilder:validation:XValidation:rule="!has(self.cluster) || !has(self.cluster.location) || self.cluster.location != 'NextTo' || !has(self.namespace)",message="namespace selector cannot be set when cluster location is 'NextTo'"
type TargetDefinition struct {
	// Cluster specifies Cluster resource(s) to target for the replica.
	// Can be omitted, in which case the platform cluster (where the operator runs) is targeted.
	// +optional
	Cluster *ClusterTargetDefinition `json:"cluster,omitempty"`

	// Namespace specifies namespace(s) to target for the replica.
	// If this resolves to multiple namespaces, the replica will be created in each of them.
	// If omitted, the namespace must be specified in the template (except for the 'only one source, no template' case).
	// +optional
	Namespace *NamespaceTargetDefinition `json:"namespace,omitempty"`

	// TargetConflictPolicy specifies what to do if the target resource already exists.
	// Valid values are:
	// - Overwrite
	//     The existing target resource will be 'taken over' by the replica.
	//     It will be overwritten and the replica will be responsible for it from then on.
	//     Note that this policy still returns an error if the existing target resource is managed by another (Cluster)Replica, which still exists.
	// - Skip
	//     The existing resource will remain unchanged and the replica will not be created.
	//     The skip will be visible in the conditions, but the Replica will still be considered healthy.
	// - Fail (default)
	//     The existing resource will remain unchanged and the replica will not be created.
	//     The reconciliation returns an error, which is also reflected in the conditions.
	//     This causes the Replica to not become healthy unless the conflicting resource is removed.
	// +kubebuilder:validation:Enum=Overwrite;Skip;Fail
	// +optional
	TargetConflictPolicy *TargetConflictPolicy `json:"targetConflictPolicy,omitempty"`

	// NamespacePolicy specifies what to do if the target namespace does not exist.
	// Has no effect if the rendered template results in a cluster-scoped resource.
	// Valid values are:
	// - Create (default)
	//     The target namespace will be created if it does not exist.
	// - Skip
	//     The target namespace will be skipped and the replica will not be created.
	//     The skip will be visible in the conditions, but the Replica will still be considered healthy.
	// - Fail
	//     The reconciliation returns an error, which is also reflected in the conditions.
	//     This causes the Replica to not become healthy unless the target namespace is created.
	// +kubebuilder:validation:Enum=Create;Skip;Fail
	// +optional
	NamespacePolicy *NamespacePolicy `json:"namespacePolicy,omitempty"`
}

func (td *TargetDefinition) GetTargetConflictPolicy() TargetConflictPolicy {
	if td.TargetConflictPolicy == nil {
		return TargetConflictPolicyFail
	}
	return *td.TargetConflictPolicy
}

func (td *TargetDefinition) GetNamespacePolicy() NamespacePolicy {
	if td.NamespacePolicy == nil {
		return NamespacePolicyCreate
	}
	return *td.NamespacePolicy
}

type ClusterTargetDefinition struct {
	// Selector is a cluster selector for identifying the target Cluster resources.
	// If empty, all Cluster resources match.
	// +optional
	Selector *clustersv1alpha1.IdentityLabelPurposeSelector `json:"selector,omitempty"`

	// Location specifies where the target resource should be created.
	// It must be either 'Onto' or 'NextTo'.
	// If omitted, 'Onto' is assumed.
	// If it is 'Onto', the target resource is created on the cluster represented by the Cluster resource.
	// If it is 'NextTo', the target resource is created on the platform cluster, in the same namespace as the Cluster resource. This is incompatible with any namespace selectors.
	// Whether only one or multiple target resources are created for multiple matching Cluster resources in the same namespace when using 'NextTo' depends on whether the template generates different names for the different clusters' target resources.
	// +kubebuilder:validation:Enum=Onto;NextTo
	// +optional
	Location *ClusterRelativeLocation `json:"location,omitempty"`
}

type ClusterRelativeLocation string

const (
	OntoCluster   ClusterRelativeLocation = "Onto"
	NextToCluster ClusterRelativeLocation = "NextTo"
)

type NamespaceTargetDefinition struct {
	// Selector is a namespace selector for identifying the target namespaces.
	// Note that there is a difference between this being nil and this being an empty struct:
	// - If nil, no namespaces will be selected and the target resource's namespace must be specified in the template (except for the 'only one source, no template' case).
	// - If empty, this results in a namespace selector which matches all namespaces, and the target resource will be created in all of them.
	// +optional
	// +nullable
	Selector *NamespaceSelector `json:"selector,omitempty"`
}

type NamespaceSelector struct {
	// MatchIdentities specifies a list of namespace names to match.
	// If this is nil, the label selector applies.
	// If this is not nil, but empty, no namespaces match. This is rarely useful.
	// Otherwise, exactly the namespaces with the given names match.
	// Note that the label selector will be ignored if this is not nil.
	// +optional
	// +listType=atomic
	// +nullable
	MatchIdentities []commonapi.LocalObjectReference `json:"matchIdentities,omitempty"`

	metav1.LabelSelector `json:",inline"`
}

type CreatedResourcesWithTypeList []CreatedResourcesWithType

type CreatedResourcesWithType struct {
	// Type specifies the type of the created resources.
	Type metav1.GroupVersionKind `json:"type"`

	// Resources is a list of references to the created resources of the given type.
	Resources []CreatedResource `json:"resources"`
}

type CreatedResource struct {
	// Cluster specifies the cluster where the replica was created.
	// If nil, the replica was created on the platform cluster (where the operator runs).
	// +optional
	Cluster *commonapi.ObjectReference `json:"cluster,omitempty"`

	commonapi.ObjectReferenceWithOptionalNamespace `json:",inline"`
}

type ReplicaStatus struct {
	commonapi.Status `json:",inline"`

	// Replicas is a list of references to the resources which were created as replicas.
	// It is grouped by type, so that the operator can easily find all replicas of a given type.
	// +optional
	Replicas CreatedResourcesWithTypeList `json:"replicas,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:shortName=rep
// +kubebuilder:metadata:labels="openmcp.cloud/cluster=platform"
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:JSONPath=`.status.phase`,name="Phase",type=string
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
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

var _ ReplicaEquivalent = &Replica{}

func (r *Replica) GetSpec() *ReplicaSpec {
	return &r.Spec
}

func (r *Replica) GetStatus() *ReplicaStatus {
	return &r.Status
}

func (r *Replica) ReplicaKind() string {
	return KindReplica
}

func (r *Replica) DeepCopyReplicaEquivalent() ReplicaEquivalent {
	return r.DeepCopy()
}

func (r *Replica) NamespacedName() string {
	return r.Namespace + "/" + r.Name
}
