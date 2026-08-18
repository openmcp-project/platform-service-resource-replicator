package v1alpha1

const (
	ReplicaGroupPrefix = "replica." + GroupName
	KindReplica        = "Replica"
	KindClusterReplica = "ClusterReplica"

	// ReplicaFinalizer is the finalizer used by the Replica controller.
	ReplicaFinalizer = ReplicaGroupPrefix + "/finalizer"

	// ReplicaSourceKindLabel is a label on a resource copy that indicates whether it was created by a Replica or a ClusterReplica.
	// Its value is the lowercase variant of either "Replica" or "ClusterReplica".
	ReplicaSourceKindLabel = ReplicaGroupPrefix + "/kind"
	// ReplicaSourceNameLabel is the label used to indicate the name of the Replica or ClusterReplica that created the resource copy.
	ReplicaSourceNameLabel = ReplicaGroupPrefix + "/name"
	// ReplicaSourceNamespaceLabel is the label used to indicate the namespace of the Replica that created the resource copy.
	// This label is not set for copies created by ClusterReplicas, as they are cluster-scoped.
	ReplicaSourceNamespaceLabel = ReplicaGroupPrefix + "/namespace"
	// ReplicaSourceGenerationLabel is the label used to indicate the generation of the Replica or ClusterReplica that created the resource copy.
	// This is used to determine whether the copy is up-to-date with the source.
	ReplicaSourceGenerationLabel = ReplicaGroupPrefix + "/generation"

	// ReasonTargetClusterInteractionProblem indicates that the reconciliation of a Replica or ClusterReplica failed due to an error when interacting with the target cluster.
	ReasonTargetClusterInteractionProblem = "TargetClusterInteractionProblem"
	// ReasonTargetConflict indicates that the controller tried to create a target resource that already exists and is not owned by the Replica or ClusterReplica.
	ReasonTargetConflict = "TargetConflict"
	// ReasonMissingNamespace indicates that the controller tried to create a target resource in a namespace that does not exist and cannot be created.
	ReasonMissingNamespace = "MissingNamespace"
	// ReasonNamespaceInDeletion indicates that a namespace where resources are to be created in is being deleted.
	ReasonNamespaceInDeletion = "NamespaceInDeletion"

	// ConditionTypeSourcePrefix is the prefix for source-specific conditions
	ConditionTypeSourcePrefix = "Source_"
	// ConditionTypeTargetPrefix is the prefix for target-specific conditions
	ConditionTypeTargetPrefix = "Target_"
	// ConditionTypeClusterPrefix is the prefix for cluster-specific conditions
	ConditionTypeClusterPrefix = "Cluster_"
	// ConditionTypeMeta is the condition type used for conditions which don't fit anywhere else, e.g. for conditions that are not specific to a source or target.
	ConditionTypeMeta = "Meta"
	// ConditionReasonSourceRead indicates that the corresponding source was successfully read.
	ConditionReasonSourceRead = "SourceRead"
	// ConditionReasonTargetCreated indicates that the corresponding target cluster was successfully accessed.
	ConditionReasonTargetClusterAccess = "TargetClusterAccess"
	// ConditionReasonTargetSkipped indicates that the corresponding target resource was skipped due to a conflict.
	ConditionReasonTargetSkipped = "TargetSkipped"
	// ConditionReasonTargetSynced indicates that the corresponding target resource was successfully created or updated.
	ConditionReasonTargetSynced = "TargetSynced"
	// ConditionReasonWaitingForManagedReplicasDeletion indicates that the controller is waiting for managed replicas to be deleted before it can proceed with deletion of the (Cluster)Replica.
	ConditionReasonWaitingForManagedReplicasDeletion = "WaitingForManagedReplicasDeletion"
)
