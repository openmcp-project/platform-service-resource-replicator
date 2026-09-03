# What does the Resource Replicator do?

There are several scenarios within a productive open-control-plane setup which require copying k8s resources - mostly secrets - across namespaces and across clusters. This document will give a quick overview on how the Resource Replicator helps with this task.

The Resource Replicator watches `Replica` and `ClusterReplica` resources. Both are identical with the sole exception of the latter one being cluster-scoped, so the documentation will usually refer to `Replica` resources only, but everything can be applied to `ClusterReplica` resources as well.

Each `Replica` specifies one or more source resources. These can be arbitrary resources, but they must be on the platform cluster (the one which is also hosting the Resource Replicator).

Templating allows to merge multiple sources together or transform a source into a different kind. For simple cases, where there is only one source which should be copied unmodified, the template can be omitted.

Within a `Replica`, multiple target definitions can be specified. Each target definition is a combination of a cluster selector and a namespace selector. The cluster selector allows to target the various clusters of an open-control-plane landscape, it can be omitted to target the hosting platform cluster itself. It can also be used to target the namespaces the `Cluster` resources are in (no namespace selector may be provided in this use-case). The namespace selector allows selecting one or more namespaces to put the copied resource into, either by names or via a label selector.

Resource copies are automatically deleted if their `Replica` gets a deletion timestamp, the `Cluster` they are on gets a deletion timestamp, or their `Cluster` or namespace changes its labels to not match the selectors anymore.

For more details on how to configure a `Replica`, see the [examples](../examples/) and the [resource documentation](./replica.md).
