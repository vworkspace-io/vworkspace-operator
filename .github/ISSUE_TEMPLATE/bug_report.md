---
name: Bug report
about: Report a problem with vworkspace-operator
title: "bug: "
labels: ["bug", "triage"]
assignees: []
---

## Description

A clear and concise description of what the bug is.

## Steps to reproduce

1.
2.
3.

## Expected behavior

What you expected to happen.

## Actual behavior

What happened instead. Include any error messages and the relevant `kubectl describe` output for the affected `ApplicationInstance`, `Operation`, or `Cluster` resource.

## Environment

- Operator image:
  ```
  kubectl -n vworkspace get deploy/vworkspace-app-operator \
    -o jsonpath='{.spec.template.spec.containers[0].image}'
  ```
- Operator version (from the image tag):
- Kubernetes version (`kubectl version --short`):
- Kubernetes distribution (k3s, Talos, Harvester, EKS, GKE, AKS, single-node Docker host, other):
- CNI:
- Default `StorageClass` and whether it supports `VolumeSnapshot`:
- Flux version (`kubectl -n flux-system get deploy -o jsonpath='{.items[*].spec.template.spec.containers[0].image}'`):
- Velero version (if relevant):
- Connectivity mode (Pull / Push / GitOps):

## Relevant resource YAML

Paste the relevant `ApplicationInstance` or `Operation` YAML (redact any secrets).

## Relevant logs

```
kubectl -n vworkspace logs deploy/vworkspace-app-operator --tail=200
```

Redact any tokens, secret material, or personally identifiable information.

## Additional context

Anything else that might help us reproduce or understand the issue.
