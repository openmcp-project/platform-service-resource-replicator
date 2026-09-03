# Simple [ClusterReplica]

This is an example for the most simple `ClusterReplica`, which just copies one resource as-is into another namespace.

A `ClusterReplica` behaves exactly like a `Replica`, the only difference is that it is cluster-scoped.

## System State

How the clusters look before anything is copied.

### ClusterReplica

<!-- inject: clusterreplica_before/clusterreplica.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: ClusterReplica
metadata:
  name: simple
spec:
  sources:
  - id: source
    group: ""
    version: v1
    kind: Secret
    name: cryptic
    namespace: foo
  targets:
  - namespace:
      selector:
        matchIdentities:
        - name: bar
```
<!-- end inject -->

### Source Secret

<!-- inject: clusterreplica_before/source.yaml -->
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

<!-- inject: clusterreplica_after/target.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: cryptic
  namespace: bar
  labels:
    app: foo
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: clusterreplica
    replica.core.open-control-plane.io/name: simple
  annotations:
    foo.bar.baz/ann: "true"
type: Opaque
data:
  password: cGFzc3dvcmQxMjM=
```
<!-- end inject -->

> [!NOTE]
> Some fields, including finalizers and owner references, are not copied.

### ClusterReplica Status

<!-- inject: clusterreplica_after/clusterreplica_status.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: ClusterReplica
metadata:
  name: simple
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
  - namespace:
      selector:
        matchIdentities:
        - name: bar
status:
  phase: Ready
  observedGeneration: 0
  conditions:
  - message: "Successfully accessed cluster '<hosting-platform-cluster>'"
    reason: TargetClusterAccess
    status: "True"
    type: Cluster_HostingPlatformCluster
  - message: ""
    reason: Meta_True
    status: "True"
    type: Meta
  - message: "Source resource 'foo/cryptic' (id: source) successfully read"
    reason: SourceRead
    status: "True"
    type: Source_foo.cryptic_Secret.v1
  - message: "Target resource 'bar/cryptic' (/v1, Kind=Secret) successfully synced to cluster '<hosting-platform-cluster>'"
    reason: TargetSynced
    status: "True"
    type: Target_HostingPlatformCluster_bar.cryptic_Secret.v1
  replicas:
  - type:
      group: ""
      version: v1
      kind: Secret
    resources:
    - name: cryptic
      namespace: bar
```
<!-- end inject -->
