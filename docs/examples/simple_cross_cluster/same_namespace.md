# Cross-Cluster, Identical Resource

The following example copies a resource to multiple clusters without any modification.

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

<!-- inject: same_namespace_before/replica_same.yaml -->
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
```
<!-- end inject -->

### Source Secret

<!-- inject: same_namespace_before/source.yaml -->
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

<!-- inject: same_namespace_after/target_same.yaml -->
```yaml
# Clusters:
# - workloads/wl-01
# - workloads/wl-02
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: foo
  labels:
    app: foo
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

### Replica Status

<!-- inject: same_namespace_after/replica_same_status.yaml -->
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
status:
  phase: Ready
  observedGeneration: 1
  conditions: [] # redacted
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
      namespace: foo
    - cluster:
        name: wl-02
        namespace: workloads
      name: cryptic
      namespace: foo
      
```
<!-- end inject -->
