# Cross-Cluster, Fixed Namespace

The following example copies a resource to multiple clusters, into a specific namespace.

## System State

How the clusters look before anything is copied.

### Cluster Setup

| Namespace | Name | Purposes | Comment |
| --- | --- | --- | --- |
| openmcp-system | onboarding | onboarding | |
| openmcp-system | platform | platform | Also the hosting cluster of the resource replicator. |
| workloads | wl-01 | workload | |
| workloads | wl-02 | workload | |
| user-01 | cp-01 | controlplane | |
| user-02 | cp-01 | controlplane | |
| user-02 | cp-02 | controlplane | |

### Replica

<!-- inject: fixed_namespace_before/replica_fixed.yaml -->
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
        matchIdentities:
        - name: bar
```
<!-- end inject -->

### Source Secret

<!-- inject: fixed_namespace_before/source.yaml -->
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

### Target Secret

The same target resource is created in multiple clusters.

<!-- inject: fixed_namespace_after/target_fixed.yaml -->
```yaml
# Clusters:
# - workloads/wl-01
# - workloads/wl-02
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: bar
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

<!-- inject: fixed_namespace_after/replica_fixed_status.yaml -->
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
        matchIdentities:
        - name: bar
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
      namespace: bar
    - cluster:
        name: wl-02
        namespace: workloads
      name: cryptic
      namespace: bar
      
```
<!-- end inject -->
