# Templating: Resource Conversion

The following example converts a namespaced `Role` on the platform cluster into a `ClusterRole` on every workload cluster.
The `rules` are copied as-is; only the API version, kind, and namespace change.

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

The template is specified as an inline string.
It sets the target kind to `ClusterRole` and copies the `rules` from the source `Role` using the `toYaml` function.

<!-- inject: conversion_before/replica.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: pod-reader-clusterrole
  namespace: test-01
spec:
  sources:
  - id: source
    group: rbac.authorization.k8s.io
    version: v1
    kind: Role
    name: pod-reader
    namespace: infra
  template: |
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: {{ .sources.source.metadata.name }}
    rules:
    {{- .sources.source.rules | toYaml | nindent 4 }}
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
```
<!-- end inject -->

### Source Role

<!-- inject: conversion_before/source.yaml -->
```yaml
# Cluster: platform
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-reader
  namespace: infra
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get"]
```
<!-- end inject -->

## Replication Results

### Target ClusterRole

The same `ClusterRole` is created on each workload cluster. Since `ClusterRole` is cluster-scoped, no namespace is set.

<!-- inject: conversion_after/target.yaml -->
```yaml
# Clusters:
# - workloads/wl-01
# - workloads/wl-02
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: pod-reader
rules:
- apiGroups: [""]
  resources: ["pods", "pods/log"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["get"]
```
<!-- end inject -->

### Replica Status

<!-- inject: conversion_after/replica_status.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: pod-reader-clusterrole
  namespace: test-01
spec:
  sources:
  - id: source
    group: rbac.authorization.k8s.io
    version: v1
    kind: Role
    name: pod-reader
    namespace: infra
  template: |
    apiVersion: rbac.authorization.k8s.io/v1
    kind: ClusterRole
    metadata:
      name: {{ .sources.source.metadata.name }}
    rules:
    {{- .sources.source.rules | toYaml | nindent 4 }}
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
      group: rbac.authorization.k8s.io
      version: v1
      kind: ClusterRole
    resources:
    - cluster:
        name: wl-01
        namespace: workloads
      name: pod-reader
    - cluster:
        name: wl-02
        namespace: workloads
      name: pod-reader
```
<!-- end inject -->
