# Example Environment

This directory contains the manifests to construct an environment for the provided examples. It is the same for all examples.

Each YAML file from this directory is meant to be applied to the platform cluster, while all manifests from a subdirectory prefixed with `cluster_` are expected to go onto the cluster represented by the `Cluster` resource from the manifest with the same name as the subdirectory.