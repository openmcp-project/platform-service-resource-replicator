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
