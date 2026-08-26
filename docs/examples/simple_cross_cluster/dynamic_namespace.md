# Cross-Cluster, Dynamic Namespace (via Label Selector)

The following example copies a resource to multiple clusters.
In each cluster, the resource is copied into each namespace with a specific label.

## System State

How the clusters look before anything is copied.

### Cluster Setup

| Namespace | Name | Purposes | Comment |
| --- | --- | --- | --- |
| openmcp-system | onboarding | onboarding | |
| openmcp-system | platform | platform | Also the hosting cluster of the resource replicator. |
| workloads | wl-01 | workload | Namespaces `abc` and `xyz` have a label matching the selector. |
| workloads | wl-02 | workload | Namespace `test` has a label matching the selector. |
| user-01 | cp-01 | controlplane | |
| user-02 | cp-01 | controlplane | |
| user-02 | cp-02 | controlplane | |

### Replica

The specified source is copied onto all workload clusters, into every namespace with a `foo.bar.baz/target` label (independent of its value).

<!-- inject: dynamic_namespace_before/replica_dynamic.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: simple
  namespace: test-01
spec:
  sources:
  - id: source
    group: ""
    version: v1
    kind: Secret
    name: cryptic
    namespace: foo
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
    namespace:
      selector:
        matchExpressions:
        - key: foo.bar.baz/target
          operator: Exists
```
<!-- end inject -->

### Source Secret

<!-- inject: dynamic_namespace_before/source.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: foo
  finalizers:
  - foo.bar.baz/fin
  labels:
    app: foo
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

## Replication Results

Results of the resource replication.

### Target Secrets

The same target resource is created in multiple clusters, and in multiple namespaces.

<!-- inject: dynamic_namespace_after/target_dynamic1.yaml -->
```yaml
# Clusters:
# - workloads/wl-01
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: abc
  labels:
    app: foo
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: simple
    replica.core.open-control-plane.io/namespace: test-01
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

<!-- inject: dynamic_namespace_after/target_dynamic2.yaml -->
```yaml
# Clusters:
# - workloads/wl-01
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: xyz
  labels:
    app: foo
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: simple
    replica.core.open-control-plane.io/namespace: test-01
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

<!-- inject: dynamic_namespace_after/target_dynamic3.yaml -->
```yaml
# Clusters:
# - workloads/wl-02
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: test
  labels:
    app: foo
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: simple
    replica.core.open-control-plane.io/namespace: test-01
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

### Replica Status

<!-- inject: dynamic_namespace_after/replica_dynamic_status.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: simple
  namespace: test-01
  finalizers:
  - replica.core.open-control-plane.io/finalizer
spec:
  sources:
  - id: source
    group: ""
    version: v1
    kind: Secret
    name: cryptic
    namespace: foo
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
    namespace:
      selector:
        matchExpressions:
        - key: foo.bar.baz/target
          operator: Exists
status:
  phase: Ready
  observedGeneration: 0
  conditions:
  - message: "Successfully accessed cluster 'workloads/wl-01'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_workloads.wl-01
  - message: "Successfully accessed cluster 'workloads/wl-02'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_workloads.wl-02
  - message: ""
    reason: Meta_True
    status: "True"
    type: Meta
  - message: "Source resource 'foo/cryptic' (id: source) successfully read"
    reason: SourceRead
    status: "True"
    type: Source_foo.cryptic_Secret.v1
  - message: "Target resource 'abc/cryptic' (/v1, Kind=Secret) successfully synced to cluster 'workloads/wl-01'"
    reason: TargetSynced
    status: "True"
    type: Target_workloads.wl-01_abc.cryptic_Secret.v1
  - message: "Target resource 'xyz/cryptic' (/v1, Kind=Secret) successfully synced to cluster 'workloads/wl-01'"
    reason: TargetSynced
    status: "True"
    type: Target_workloads.wl-01_xyz.cryptic_Secret.v1
  - message: "Target resource 'test/cryptic' (/v1, Kind=Secret) successfully synced to cluster 'workloads/wl-02'"
    reason: TargetSynced
    status: "True"
    type: Target_workloads.wl-02_test.cryptic_Secret.v1
  replicas:
  - type:
      group: ""
      version: v1
      kind: Secret
    resources:
    - cluster:
        name: wl-01
        namespace: workloads
      name: cryptic
      namespace: abc
    - cluster:
        name: wl-01
        namespace: workloads
      name: cryptic
      namespace: xyz
    - cluster:
        name: wl-02
        namespace: workloads
      name: cryptic
      namespace: test
      
```
<!-- end inject -->
