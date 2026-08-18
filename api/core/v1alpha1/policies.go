package v1alpha1

type TargetConflictPolicy string

const (
	// TargetConflictPolicyOverwrite indicates that the target resource should be overwritten if it already exists.
	TargetConflictPolicyOverwrite TargetConflictPolicy = "Overwrite"
	// TargetConflictPolicySkip indicates that the target resource should be skipped if it already exists.
	TargetConflictPolicySkip TargetConflictPolicy = "Skip"
	// TargetConflictPolicyFail indicates that the reconciliation should fail if the target resource already exists.
	TargetConflictPolicyFail TargetConflictPolicy = "Fail"
)

type NamespacePolicy string

const (
	// NamespacePolicyCreate indicates that the target namespace should be created if it does not exist.
	NamespacePolicyCreate NamespacePolicy = "Create"
	// NamespacePolicySkip indicates that the target namespace should be skipped if it does not exist.
	NamespacePolicySkip NamespacePolicy = "Skip"
	// NamespacePolicyFail indicates that the reconciliation should fail if the target namespace does not exist.
	NamespacePolicyFail NamespacePolicy = "Fail"
)
