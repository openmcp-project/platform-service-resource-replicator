# Templating: Copying Resources 'next to' Cluster

The following example copies an image pull secret onto the platform cluster, next to every ControlPlane cluster resource.
The secret's name is dynamically set to `imagepull-<cluster-name>` using a template.

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

The template is specified as a YAML structure.
Non-templated fields are plain YAML; fields requiring dynamic values use `{{ }}` expressions.

<!-- inject: next_to_before/replica.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: imagepull-next-to
  namespace: test-01
spec:
  sources:
  - id: source
    group: ""
    version: v1
    kind: Secret
    name: imagepull
    namespace: registry
  template:
    apiVersion: v1
    kind: Secret
    metadata:
      name: "imagepull-{{ .target.cluster.name }}"
    type: "{{ .sources.source.type }}"
    data:
      .dockerconfigjson: "{{ index .sources.source.data \".dockerconfigjson\" }}"
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - controlplane
      location: NextTo
```
<!-- end inject -->

### Source Secret

<!-- inject: next_to_before/source.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: imagepull
  namespace: registry
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: eyJhdXRocyI6eyJnaGNyLmlvIjp7ImF1dGgiOiJzb21lYXV0aHRva2VuIn19fQ==
```
<!-- end inject -->

## Replication Results

The secret is created on the platform cluster, in the same namespace as each ControlPlane Cluster resource.
The name is derived from the cluster's name.

### Target Secrets

<!-- inject: next_to_after/target_user-01_cp-01.yaml -->
```yaml
# Cluster: platform
# Namespace: user-01 (same namespace as Cluster resource 'user-01/cp-01')
apiVersion: v1
kind: Secret
metadata:
  name: imagepull-cp-01
  namespace: user-01
  labels:
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: imagepull-next-to
    replica.core.open-control-plane.io/namespace: test-01
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: eyJhdXRocyI6eyJnaGNyLmlvIjp7ImF1dGgiOiJzb21lYXV0aHRva2VuIn19fQ==
```
<!-- end inject -->

<!-- inject: next_to_after/target_user-02_cp-01.yaml -->
```yaml
# Cluster: platform
# Namespace: user-02 (same namespace as Cluster resource 'user-02/cp-01')
apiVersion: v1
kind: Secret
metadata:
  name: imagepull-cp-01
  namespace: user-02
  labels:
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: imagepull-next-to
    replica.core.open-control-plane.io/namespace: test-01
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: eyJhdXRocyI6eyJnaGNyLmlvIjp7ImF1dGgiOiJzb21lYXV0aHRva2VuIn19fQ==
```
<!-- end inject -->

<!-- inject: next_to_after/target_user-02_cp-02.yaml -->
```yaml
# Cluster: platform
# Namespace: user-02 (same namespace as Cluster resource 'user-02/cp-02')
apiVersion: v1
kind: Secret
metadata:
  name: imagepull-cp-02
  namespace: user-02
  labels:
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: imagepull-next-to
    replica.core.open-control-plane.io/namespace: test-01
type: kubernetes.io/dockerconfigjson
data:
  .dockerconfigjson: eyJhdXRocyI6eyJnaGNyLmlvIjp7ImF1dGgiOiJzb21lYXV0aHRva2VuIn19fQ==
```
<!-- end inject -->

### Replica Status

<!-- inject: next_to_after/replica_status.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: imagepull-next-to
  namespace: test-01
  finalizers:
  - replica.core.open-control-plane.io/finalizer
spec:
  sources:
  - id: source
    group: ""
    version: v1
    kind: Secret
    name: imagepull
    namespace: registry
  template:
    apiVersion: v1
    kind: Secret
    metadata:
      name: "imagepull-{{ .target.cluster.name }}"
    type: "{{ .sources.source.type }}"
    data:
      .dockerconfigjson: "{{ index .sources.source.data \".dockerconfigjson\" }}"
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - controlplane
      location: NextTo
status:
  phase: Ready
  observedGeneration: 0
  conditions:
  - message: "Successfully accessed cluster 'user-01/cp-01'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_user-01.cp-01
  - message: "Successfully accessed cluster 'user-02/cp-01'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_user-02.cp-01
  - message: "Successfully accessed cluster 'user-02/cp-02'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_user-02.cp-02
  - message: ""
    reason: Meta_True
    status: "True"
    type: Meta
  - message: "Source resource 'registry/imagepull' (id: source) successfully read"
    reason: SourceRead
    status: "True"
    type: Source_registry.imagepull_Secret.v1
  - message: "Target resource 'user-01/imagepull-cp-01' (/v1, Kind=Secret) successfully synced to cluster 'user-01/cp-01'"
    reason: TargetSynced
    status: "True"
    type: Target_user-01.cp-01_user-01.imagepull-cp-01_Secret.v1
  - message: "Target resource 'user-02/imagepull-cp-01' (/v1, Kind=Secret) successfully synced to cluster 'user-02/cp-01'"
    reason: TargetSynced
    status: "True"
    type: Target_user-02.cp-01_user-02.imagepull-cp-01_Secret.v1
  - message: "Target resource 'user-02/imagepull-cp-02' (/v1, Kind=Secret) successfully synced to cluster 'user-02/cp-02'"
    reason: TargetSynced
    status: "True"
    type: Target_user-02.cp-02_user-02.imagepull-cp-02_Secret.v1
  replicas:
  - type:
      group: ""
      version: v1
      kind: Secret
    resources:
    - name: imagepull-cp-01
      namespace: user-01
    - name: imagepull-cp-01
      namespace: user-02
    - name: imagepull-cp-02
      namespace: user-02
```
<!-- end inject -->
