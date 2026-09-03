# Templating: Combining Multiple Sources

The following example combines two source secrets into a single target secret using an inline string template.
Only the `password` key is taken from the first secret; all keys are copied from the second secret.

## System State

How the clusters look before anything is copied.

### Replica

The template is specified as an inline string.
It selects one specific key from the `db` source and copies all keys from the `tls` source.

<!-- inject: multi_source_before/replica.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: combined-secret
  namespace: test-01
spec:
  sources:
  - id: db
    group: ""
    version: v1
    kind: Secret
    name: db-credentials
    namespace: infra
  - id: tls
    group: ""
    version: v1
    kind: Secret
    name: tls-cert
    namespace: infra
  template: |
    apiVersion: v1
    kind: Secret
    metadata:
      name: combined
      namespace: test-01
    type: Opaque
    data:
      password: {{ index .sources.db.data "password" }}
      {{- range $key, $value := .sources.tls.data }}
      {{ $key }}: {{ $value }}
      {{- end }}
  targets:
  - namespace:
      selector:
        matchIdentities:
        - name: test-01
```
<!-- end inject -->

### Source: DB Credentials

Only the `password` key will be used; `username`, `host`, and `port` are intentionally ignored.

<!-- inject: multi_source_before/source_db.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: db-credentials
  namespace: infra
type: Opaque
data:
  username: YWRtaW4=
  password: c3VwZXJzZWNyZXQ=
  host: ZGIuaW50ZXJuYWw=
  port: NTQzMg==
```
<!-- end inject -->

### Source: TLS Certificate

All keys from this secret are copied into the target.

<!-- inject: multi_source_before/source_tls.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: tls-cert
  namespace: infra
type: kubernetes.io/tls
data:
  tls.crt: LS0tLS1CRUdJTi0tLS0t
  tls.key: LS0tLS1CRUdJTi0tLS0t
```
<!-- end inject -->

## Replication Results

### Target Secret

<!-- inject: multi_source_after/target.yaml -->
```yaml
# Cluster: platform
apiVersion: v1
kind: Secret
metadata:
  name: combined
  namespace: test-01
  labels:
    openmcp.cloud/managed-by: replica
    replica.core.open-control-plane.io/generation: "0"
    replica.core.open-control-plane.io/kind: replica
    replica.core.open-control-plane.io/name: combined-secret
    replica.core.open-control-plane.io/namespace: test-01
type: Opaque
data:
  password: c3VwZXJzZWNyZXQ=
  tls.crt: LS0tLS1CRUdJTi0tLS0t
  tls.key: LS0tLS1CRUdJTi0tLS0t
```
<!-- end inject -->

### Replica Status

<!-- inject: multi_source_after/replica_status.yaml -->
```yaml
# Cluster: platform
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: combined-secret
  namespace: test-01
  finalizers:
  - replica.core.open-control-plane.io/finalizer
spec:
  sources:
  - id: db
    group: ""
    version: v1
    kind: Secret
    name: db-credentials
    namespace: infra
  - id: tls
    group: ""
    version: v1
    kind: Secret
    name: tls-cert
    namespace: infra
  template: |
    apiVersion: v1
    kind: Secret
    metadata:
      name: combined
      namespace: test-01
    type: Opaque
    data:
      password: {{ index .sources.db.data "password" }}
      {{- range $key, $value := .sources.tls.data }}
      {{ $key }}: {{ $value }}
      {{- end }}
  targets:
  - namespace:
      selector:
        matchIdentities:
        - name: test-01
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
  - message: "Source resource 'infra/db-credentials' (id: db) successfully read"
    reason: SourceRead
    status: "True"
    type: Source_infra.db-credentials_Secret.v1
  - message: "Source resource 'infra/tls-cert' (id: tls) successfully read"
    reason: SourceRead
    status: "True"
    type: Source_infra.tls-cert_Secret.v1
  - message: "Target resource 'test-01/combined' (/v1, Kind=Secret) successfully synced to cluster '<hosting-platform-cluster>'"
    reason: TargetSynced
    status: "True"
    type: Target_HostingPlatformCluster_test-01.combined_Secret.v1
  replicas:
  - type:
      group: ""
      version: v1
      kind: Secret
    resources:
    - name: combined
      namespace: test-01
```
<!-- end inject -->
