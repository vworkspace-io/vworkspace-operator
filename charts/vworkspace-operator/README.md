# vworkspace-operator Helm chart

Install the operator, CRDs, and RBAC via Helm. User-facing install guide: [docs/install/helm.md](../../docs/install/helm.md).

Session 3 dogfood profile: [values-kind.yaml](values-kind.yaml) — see hub [session-3-helm-path-design.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/dogfooding/session-3-helm-path-design.md).

## Install from repository checkout

Operator only (default):

```bash
helm install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=latest
```

Session 3 bundle (Flux + Velero + MinIO + metrics on kind):

```bash
helm upgrade --install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system --create-namespace \
  -f charts/vworkspace-operator/values-kind.yaml
```

Validate rendering without applying:

```bash
# Operator only (default values)
helm template vworkspace-operator ./charts/vworkspace-operator \
  --namespace vworkspace-system

# Session 3 bundle profile
helm template vworkspace-operator ./charts/vworkspace-operator \
  --namespace vworkspace-system \
  -f charts/vworkspace-operator/values-kind.yaml
```

Validate on kind:

```bash
./hack/validate-helm-kind.sh
VALIDATE_BUNDLE=true ./hack/validate-helm-kind.sh
```

## Cluster bootstrap (after install)

Connectivity is not configured via Helm values. Apply a token `Secret` and `Cluster` CR — see [examples/cluster-bootstrap/](examples/cluster-bootstrap/).

```bash
# Edit placeholders, then:
kubectl apply -f examples/cluster-bootstrap/registration-token.secret.yaml
kubectl apply -f examples/cluster-bootstrap/cluster.yaml
kubectl get cluster cluster-local -w
```

## Values

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `vworkspace/vworkspace-operator` | Operator image |
| `image.tag` | Chart `appVersion` | Image tag |
| `agent.pollIntervalSeconds` | `30` | Pull-mode job long-poll interval |
| `agent.credentialsSecret` | `vworkspace-agent-credentials` | Default credentials Secret name (overridden by Cluster status) |
| `crds.install` | `true` | Install CRDs from `files/crds/` |
| `flux.enabled` | `false` | Bundle: Flux helm-controller + source-controller |
| `velero.enabled` | `false` | Bundle: Velero server + CRDs |
| `velero.minio.enabled` | `false` | Bundle: in-cluster MinIO for kind |
| `manager.metricsBindAddress` | `0` (off) | Set `:8443` for HTTPS `/metrics` |

See [values.yaml](values.yaml) and [values-kind.yaml](values-kind.yaml) for the full list.

## Notes

- Default values install **only** the operator. Optional bundle flags add Flux and Velero — [prerequisites.md](../../docs/install/prerequisites.md).
- CRD files live under `files/crds/` (operator) and `charts/velero-crds/crds/` (Velero bundle, installed before Velero templates when `velero.enabled=true`); Flux manifests are under `files/flux/`.
- Post-install hints are rendered in `templates/NOTES.txt`.
