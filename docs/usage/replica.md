# The (Cluster)Replica Resource

To copy a resource into a different namespace or cluster, a `Replica` resource is required. The specification of this resource is explained in this document.

### What about ClusterReplica?

There is also a resource named `ClusterReplica`. The only difference between `Replica` and `ClusterReplica` is that the latter one is cluster-scoped. Apart from that, both are completely identical, regarding specification and functionality. For this reason, the documentation will only cover `Replica` in the following, but everything works exactly the same for `ClusterReplica`.

## Replica Manifest

```yaml
apiVersion: core.open-control-plane.io/v1alpha1
kind: Replica
metadata:
  name: mycopy
  namespace: foo
spec:
  sources:
  - id: main
    group: ""
    version: v1
    kind: Secret
    name: config-secret
    namespace: asdf
  - id: tls
    group: ""
    version: v1
    kind: Secret
    name: my-cert
    namespace: bar
  template: |
    apiVersion: v1
    kind: Secret
    metadata:
      name: combined
      namespace: base
    type: Opaque
    data:
      tls.key: {{ index .sources.tls.data "tls.key" }}
      tls.cert: {{ index .sources.tls.data "tls.cert" }}
      {{- range $key, $value := .sources.tls.data }}
      {{ $key }}: {{ $value }}
      {{- end }}
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - controlplane
      location: NextTo
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
  - cluster:
      selector:
        matchIdentities:
        - name: onboarding
          namespace: openmcp-system
    namespace:
      selector:
        matchIdentities:
        - name: core
  - namespace:
      selector:
        matchLabels:
          foo.bar.baz/inject-copy: "true"
```

### Sources

Source resources are referenced under `spec.sources`.

```yaml
  sources:
  - id: main
    group: ""
    version: v1
    kind: Secret
    name: config-secret
    namespace: asdf
  - id: tls
    group: ""
    version: v1
    kind: Secret
    name: my-cert
    namespace: bar
```

Sources consist of a unique `id`, which is used to reference the source in the template. Apart from that, each source specifies GVK and `name` and `namespace`. `namespace` must be empty for cluster-scoped resources.

There has to be at least one source.

### Template

The template for the target resource is specified under `spec.template`.

It can be specified as an inline text field
```yaml
  template: |
    apiVersion: v1
    kind: Secret
    metadata:
      name: combined
      namespace: base
    type: Opaque
    data:
      tls.key: {{ index .sources.tls.data "tls.key" }}
      tls.cert: {{ index .sources.tls.data "tls.cert" }}
      {{- range $key, $value := .sources.tls.data }}
      {{ $key }}: {{ $value }}
      {{- end }}
```
or as structured YAML
```yaml
  template:
    apiVersion: v1
    kind: Secret
    metadata:
      name: combined
      namespace: base
    type: Opaque
    data:
      tls.key: '{{ index .sources.tls.data "tls.key" }}'
      tls.cert: '{{ index .sources.tls.data "tls.cert" }}'
```

The structured variant is simpler to use, but more complex templating scenarios, especially regarding loops or nested structs, might not be feasible with it and require the text template approach.

> [!WARNING]
> While the templating can be used to create different resource kinds for a single `Replica`, this is discouraged. It is not the intended use-case and might result in undesired side effects or break in a future release.

#### Template Bindings

The following values are available for templating:
- `sources`: A map of the manifests of all resources referenced under `spec.sources`, using their `id` as key.
- `target`: Information about the target for which the resource is currently being rendered. Contains the following fields:
  - `cluster`: Information about the `Cluster` being targeted. If this is empty, the hosting platform cluster is targeted. Contains the following fields otherwise:
    - _Note: In case of a cluster selector with `location: NextTo`, this will also contain information about the corresponding `Cluster` resource, although the target will always be created on the hosting platform cluster._
    - `name`: Name of the `Cluster` resource.
    - `namespace`: Namespace of the `Cluster` resource.
    - `purposes`: List of purposes of the `Cluster` (from the `Cluster`'s `spec.purposes`).
  - `namespace`: Namespace the target resource is being rendered for. This field is only available if the namespace is determined by a namespace selector, or by a cluster selector with `location: NextTo`. In all other cases, the namespace is determined by the template itself and therefore not available as input.
- `replica`: Information about the `Replica` (or `ClusterReplica`) resource responsible for the rendered target resource. It contains the following fields:
  - `name`: Name of the `Replica` (or `ClusterReplica`) resource.
  - `namespace`: Namespace of the `Replica` resource. Always empty for `ClusterReplica` resources.
  - `labesl`: Labels of the `Replica` (or `ClusterReplica`) resource.
  - `annotations`: Annotations of the `Replica` (or `ClusterReplica`) resource.

### Targets

A single `Replica` can specify multiple target definitions under `spec.targets`, and each one can result in multiple resources being created.

```yaml
  targets:
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - controlplane
      location: NextTo
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
  - cluster:
      selector:
        matchIdentities:
        - name: onboarding
          namespace: openmcp-system
    namespace:
      selector:
        matchIdentities:
        - name: core
```

A target definition can contain the following fields:

- **`cluster`** _(optional)_: For targeting `Cluster` resources.
  - **`selector`** _(optional)_: For selecting `Cluster`s. Matches all `Clusters` if not specified.
    - **`matchIdentities`** _(optional)_: Lists specific clusters which should match, overrides other selectors. See the [selector library documentation](https://github.com/openmcp-project/openmcp-operator/blob/main/docs/libraries/selectors.md) for details.
    - **`matchPurposes`** _(optional)_: Matches `Cluster`s according to their purposes. See the [selector library documentation](https://github.com/openmcp-project/openmcp-operator/blob/main/docs/libraries/selectors.md) for details.
    - **`matchLabels`** and **`matchExpressions`** _(both optional)_: Default k8s label selector. See the [selector library documentation](https://github.com/openmcp-project/openmcp-operator/blob/main/docs/libraries/selectors.md) for details.
  - **`location`** _(optional, default `Onto`)_: This can either be `Onto` or `NextTo`, with the former one being the default. If `Onto`, the generated resources are created _on_ the respective clusters. In case of `NextTo`, the generated resources are put on the hosting platform cluster, _in the namespaces of the respective `Cluster` resources_.
    - If this is set to `NextTo`, no namespace selector may be specified, as this special case of the cluster selector actually targets namespaces.
- **`namespace`** _(optional)_: For targeting namespaces.
  - **`selector`** _(optional)_: For selecting namespaces. If `nil` (`null` or not specified in YAML), no namespace is matched. If non-nil, but empty, all namespaces are matched.
    - **`matchIdentities`** _(optional)_: List of namespace references (`name: <name>`).
      - If `matchIdentities` is `nil` (`null` or not specified in YAML), it is ignored.
      - If `matchIdentities` is non-nil, but empty, no namespace is matched. This means that no target resource will be created.
      - If this field is non-nil - this also includes the empty case described above - the label selector is ignored and will not have any effect.
    - **`matchLabels`** and **`matchExpressions`** _(both optional)_: Default k8s label selector. No effect if `matchIdentities` is non-nil.
- **`targetConflictPolicy`** _(optional, default `Fail`)_: Must be one of `Overwrite`, `Skip`, or `Fail`, defaulting to the latter one. The policies are described in more detail [below](#conflicts).
- **`namespacePolicy`** _(optional, default `Create`)_: Must be one of `Create`, `Skip`, or `Fail`, defaulting to the first one. The policies are described in more detail [below](#conflicts).

#### Commonly Used Targets

The target definitions can quickly become very complex, therefore this section lists some of the more commonly used ones.

##### Onto Clusters with specific purpose (fixed namespace)

Example: Copy a secret into the `kube-system` namespace of every `Cluster` with purpose `workload`:
```yaml
  - cluster:
      selector:
        matchPurposes:
        - operator: ContainsAll
          values:
          - workload
    namespace: # optional, depending on the situation, see below
      selector:
        matchIdentities:
        - name: kube-system
```

- Since its `values` consist of only a single value, it does not matter whether `ContainsAny` or `ContainsAll` is used as operator for the purpose selector.
- The complete `namespace` should be omitted in the following situations:
  - The generated resource is cluster-scoped.
  - The `kube-system` namespace is hard-coded in the specified `template`.
- Instead of `matchIdentities` in the namespace selector, a label selector could be used, in which case the resource would be copied into multiple namespaces (all which match the selector) on the respective clusters.

##### Next to Clusters with specific purpose

Example: For each `Cluster` with purpose `controlplane`, create a copy of the secret named `imagepull-<cluster_name>`
```yaml
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

- No `namespace` must be specified in the target definition, as a cluster selector with `location: NextTo` basically results in a namespace selector.
- In the template, `{{ .target.cluster.name }}` will contain the name of the `Cluster` resource, although the rendered resource will be created on the hosting platform cluster _in that `Cluster`'s namespace_ and not _on_ the cluster, due to the `NextTo`.

## Conflicts

There are various conflicts which can occur during resource replication. This section covers the important ones and explains the operator's behavior. Note that conflicts can not only occur when new `Replica` resources are created, but also if new `Cluster` or `Namespace` resources are created, or if existing ones are altered and match an existing `Replica`'s selector as a result.

Whenever 'identities' are mentioned, this usually refers to the combination of name, namespace, and GVK of a k8s resource, and the cluster the resource lives on.

#### Multiple resource with same identity created by the same Replica

A single `Replica` creating multiple resources with the same identity usually results in an error.

**Exception:** If the resources are completely identical - not only their identities, but the rendered template must be absolutely identical - then the conflict is silently ignored.

#### Multiple Replicas create resources with the same identity

If two `Replica`s try to manage the same target resource, the result depends on the target conflict policy. 

Note that resources managed by other `Replica`s cannot be 'overwritten', so a policy of `Overwrite` behaves the same as the `Fail` one in this case.

This also means that the `Replica` whichever created the conflicting resource first is managing it. Multiple `Replica`s fighting over the same target resource can still become healthy, but only if all but the first one use the `Skip` target conflict policy. The policy of the `Replica` that created the resource first doesn't matter in this case.

#### Resource created by Replica clashes with existing (unmanaged) resource

The specified target conflict policy is applied.

### Policies

There are currently two policies which can be specified to control the operator's behavior when it encounters existing or missing resources.

#### Target Conflict Policy

**Specified via:** `targetConflictPolicy` field of a target definition
**Solves:** What to do if a resource which should be managed by a `Replica` already exists and is not managed by this `Replica`?
**Values:**
- `Overwrite`
  - The `Replica` takes over the existing resource and manages it from that point on.
  - Resources already managed by other `Replica`s cannot be taken over. In this case, `Overwrite` behaves like `Fail`.
- `Skip`
  - The `Replica` skips creating/managing the conflicting resource.
  - There will be a condition showing that the resource was skipped, but no error will be logged and the `Replica` will appear healthy.
- `Fail`
  - **This is the default.**
  - Reconciliation will fail with an error.
  - The `Replica`'s status will show the error in its conditions, and it will not become healthy as long as the problem persists.

#### Namespace Policy

**Specified via:** `namespacePolicy` field of a target definition
**Solves:** What to do if the namespace the resource should be created in does not exist?
**Values:**
- `Create`
  - **This is the default.**
  - The namespace is silently created.
  - Namespaces are not managed by the Resource Replicator, so namespaces created this way will continue to exist, even when the `Replica` which created it is removed.
- `Skip`
  - The resource(s) which would be created in the missing namespace will be skipped.
  - The `Replica` will show the skipped resource in its conditions, but no error will be logged and the `Replica` will appear healthy.
- `Fail`
  - An error will occur for every resource which cannot be created due to its missing namespace.
  - The errors are reflected in the `Replica`'s conditions and the `Replica` will not become healthy as long as the problem exists.
