# Helm install guide

**Status:** Alpha
**Last Updated:** 2026-06-06

This document is the full reference for installing `vworkspace-operator` with the in-repo Helm chart at `charts/vworkspace-operator/`. For the shortest path, start with [quickstart.md](quickstart.md) Option A.

**Default values** install the operator controller, CRDs, and RBAC only. Optional **bundle v1** flags (`flux.enabled`, `velero.enabled`) add Flux controllers and Velero + MinIO for Session 3 dogfooding — see [Bundle v1 (Session 3)](#bundle-v1-session-3) and `values-kind.yaml`.

## Prerequisites

- Kubernetes 1.28 or newer with `cluster-admin` access via `kubectl`.
- Helm 3.13 or newer.
- A container registry pull path for `vworkspace/vworkspace-operator` (Docker Hub by default) or a locally built image.
- Optional: [kind](https://kind.sigs.k8s.io/) for local validation (`hack/validate-helm-kind.sh`).

## Install from repository checkout

```bash
helm install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=latest
```

## Install from GitHub Release

Published on every `v*` git tag ([release process](../development/release-process.md)). Replace `0.0.6` with the version you are installing ([releases page](https://github.com/vworkspace-io/vworkspace-operator/releases)).

**Helm** (operator + CRDs + RBAC; default bundle flags off):

```bash
helm upgrade --install vworkspace-operator \
  https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/vworkspace-operator-0.0.6.tgz \
  --version 0.0.6 \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=v0.0.6
```

The chart pins `image.tag` to the git tag (`v0.0.6`) because container images use the `v`-prefixed tag on Docker Hub while chart `appVersion` is bare SemVer.

**kubectl** (no Helm; operator-only profile — apply CRDs first):

```bash
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/crds.yaml
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/operator.yaml
kubectl -n vworkspace-system wait --for=condition=Available \
  deployment/vworkspace-operator --timeout=300s
```

Verify checksums: download `SHA256SUMS` from the same release assets.

Maintainers package locally: `VERSION=0.0.6 make package-release` (outputs under `dist/release/`).

The release name (`vworkspace-operator`) and namespace (`vworkspace-system`) are conventions. The chart works with any release name; if you change the namespace, set `Cluster.spec` and registration commands to match.

### Tested values (kind validation)

These values are exercised by `./hack/validate-helm-kind.sh`:

| Key | Value | Notes |
|-----|-------|-------|
| `image.repository` | `vworkspace/vworkspace-operator` | Chart default |
| `image.tag` | `latest` or locally built `helm-validate` | Use `latest` when pulling from Docker Hub |
| `crds.install` | `true` | Installs ApplicationInstance, Operation, Cluster CRDs |
| `agent.pollIntervalSeconds` | `30` (default) or `5` in `values-kind.yaml` | Operator-wide long-poll interval |
| `agent.credentialsSecret` | `vworkspace-agent-credentials` | Default name; written by `Cluster` reconciler |

Pull-mode connectivity is **not** configured at install time. After `helm install`, apply a token `Secret` and `Cluster` CR ([cluster-bootstrap.md](cluster-bootstrap.md)). Example templates: `charts/vworkspace-operator/examples/cluster-bootstrap/`.

## Values reference

| Key | Default | Description |
|-----|---------|-------------|
| `image.repository` | `vworkspace/vworkspace-operator` | Operator container image |
| `image.tag` | Chart `appVersion` | Image tag (`latest` for CI-published builds on `main`) |
| `image.pullPolicy` | `IfNotPresent` | Kubernetes pull policy |
| `replicaCount` | `1` | Manager Deployment replicas |
| `crds.install` | `true` | Render CRDs from `files/crds/` via chart template |
| `agent.credentialsSecret` | `vworkspace-agent-credentials` | Default credentials Secret name (overridden by `Cluster.status.credentialsSecretRef`) |
| `agent.pollIntervalSeconds` | `30` | Operator-wide Pull-mode long-poll interval |
| `rbac.create` | `true` | ClusterRole and ClusterRoleBinding |
| `serviceAccount.create` | `true` | Dedicated ServiceAccount |
| `manager.metricsBindAddress` | `0` (off) | Set to `:8443` to expose HTTPS `/metrics` (see [observability.md](../operate/observability.md#prometheus-scrape)) |

See [charts/vworkspace-operator/values.yaml](https://github.com/vworkspace-io/vworkspace-operator/blob/main/charts/vworkspace-operator/values.yaml) for manager flags, resources, scheduling, and bundle keys (`flux`, `velero`).

## Bundle v1 (Session 3)

Hub design: [session-3-helm-path-design.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/dogfooding/session-3-helm-path-design.md).

| Key | Default | Description |
|-----|---------|-------------|
| `flux.enabled` | `false` | Install helm-controller + source-controller into `flux-system` |
| `flux.installed` | `true` | Set `false` when Flux already exists (skip chart install) |
| `velero.enabled` | `false` | Install Velero server + CRDs |
| `velero.minio.enabled` | `false` | In-cluster MinIO + BSL for kind (matches server [BACKUP_E2E.md](https://github.com/vworkspace-io/vworkspace-server/blob/main/docs/development/BACKUP_E2E.md)) |
| `velero.installed` | `true` | Set `false` when Velero already exists |
| `certManager.enabled` | `false` | Placeholder — not bundled in v1 |
| `externalSecrets.enabled` | `false` | Placeholder — not bundled in v1 |

**Kind / dogfood profile** — single install with metrics, Flux, Velero, and MinIO (no connectivity Helm values):

```bash
helm upgrade --install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system --create-namespace \
  -f charts/vworkspace-operator/values-kind.yaml
```

Then apply cluster bootstrap manifests ([examples/cluster-bootstrap/](https://github.com/vworkspace-io/vworkspace-operator/blob/main/charts/vworkspace-operator/examples/cluster-bootstrap/)). Pin the operator image at run time: `--set image.tag=<sha-tag>`.

## CRD installation

CRDs ship under `charts/vworkspace-operator/files/crds/` (not Helm's reserved `crds/` folder) and are applied when `crds.install=true` through `templates/crds.yaml`. Using `files/crds/` avoids Helm installing CRDs twice — once without release ownership and again via the template — which fails with invalid ownership metadata on fresh clusters.

To manage CRDs outside Helm (GitOps or cluster bootstrap), set `crds.install=false` and apply CRDs separately:

```bash
kubectl apply -f charts/vworkspace-operator/files/crds/
```

## Post-install: connect to the control plane

After `helm install`, the operator is running but not yet connected to vWorkspace Server. Connectivity is declarative — no second `helm upgrade`, no `agent.enabled` toggle.

1. **Issue a registration token** in vWorkspace Server (Cluster Registry). See [cluster-bootstrap.md](cluster-bootstrap.md) Step 3.

2. **Apply bootstrap manifests** — token `Secret` + `Cluster` CR referencing that Secret:

   ```bash
   # Edit placeholders first, then apply from repo root:
   kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/registration-token.secret.yaml
   kubectl apply -f charts/vworkspace-operator/examples/cluster-bootstrap/cluster.yaml
   kubectl get cluster cluster-local -w
   ```

   The reconciler exchanges the token, writes `Secret/vworkspace-agent-credentials`, and the Pull-mode agent starts automatically when credentials exist.

3. **Validate** — `Cluster.status.phase=Connected` and `Connected=True`; see [quickstart.md](quickstart.md) Step 3.

Helm prints the same hints in the release notes (`NOTES.txt`). Break-glass `kubectl exec … /manager register` is documented in [cluster-bootstrap.md#break-glass-register-cli](cluster-bootstrap.md#break-glass-register-cli).

## Upgrade and uninstall

Upgrade after changing values or image tag:

```bash
helm upgrade vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system \
  --reuse-values \
  --set image.tag=<new-tag>
```

Uninstall the release ([uninstall.md](uninstall.md)):

```bash
helm uninstall vworkspace-operator -n vworkspace-system
```

CRDs installed by the chart are not removed automatically. Delete them explicitly if decommissioning the cluster:

```bash
kubectl delete -f charts/vworkspace-operator/files/crds/ --ignore-not-found
```

## Local validation on kind

Run the automated check (creates a kind cluster, installs the chart, waits for Ready, optional Flux CRDs):

```bash
chmod +x hack/validate-helm-kind.sh
./hack/validate-helm-kind.sh
```

Environment variables:

| Variable | Default | Purpose |
|----------|---------|---------|
| `KIND_CLUSTER` | `vworkspace-operator-helm-validate` | kind cluster name |
| `USE_PUBLISHED_IMAGE` | `0` | Set to `1` to pull `latest` from Docker Hub instead of building locally |
| `VALIDATE_BUNDLE` | `false` | Set to `true` to install with `values-kind.yaml` (Flux + Velero + MinIO + metrics) |
| `INSTALL_FLUX_CRDS` | `false` | Apply Flux **CRDs only** (contract tier — no controller pods). Ignored when `VALIDATE_BUNDLE=true`. |
| `DELETE_CLUSTER` | `true` | Delete kind cluster when the script created it |

Or via Makefile:

```bash
make validate-helm-kind
```

Contract tier (CRDs only, matches e2e and Phase 1 golden path):

```bash
INSTALL_FLUX_CRDS=true ./hack/validate-helm-kind.sh
```

Session 3 bundle tier (Flux controllers + Velero + MinIO; pulls published operator image):

```bash
VALIDATE_BUNDLE=true ./hack/validate-helm-kind.sh
```

## Optional Flux controllers for Ready

`INSTALL_FLUX_CRDS=true` installs the API types the operator needs to create `HelmRelease` resources. It does **not** install `helm-controller` or `source-controller`. Without those Deployments, `HelmRelease` objects are not reconciled and `ApplicationInstance` will not reach `Ready` — see [cluster-bootstrap.md#flux-contract-only-vs-full-reconcile](cluster-bootstrap.md#flux-contract-only-vs-full-reconcile).

To reach **full reconcile** on kind after the in-repo chart and CRDs:

```bash
# Flux CLI — installs controllers into flux-system
flux check --pre
flux install
```

Or install Flux via the Helm bundle:

```bash
helm upgrade --install vworkspace-operator ./charts/vworkspace-operator \
  -n vworkspace-system --reuse-values \
  --set flux.enabled=true
```

Or use `values-kind.yaml` for the full Session 3 profile ([Bundle v1](#bundle-v1-session-3)).

Verify controllers:

```bash
kubectl get deploy -n flux-system helm-controller source-controller
kubectl get helmreleases -A
```

## Render without applying

```bash
helm template vworkspace-operator ./charts/vworkspace-operator \
  --namespace vworkspace-system \
  -f charts/vworkspace-operator/values-kind.yaml
```

## Related material

- [quickstart.md](quickstart.md) — Supported install path and validation steps.
- [cluster-bootstrap.md](cluster-bootstrap.md) — Registration token flow on the control-plane side.
- [container-images.md](container-images.md) — Published tags and registry secrets.
- [charts/vworkspace-operator/README.md](https://github.com/vworkspace-io/vworkspace-operator/blob/main/charts/vworkspace-operator/README.md) — Chart maintainer notes.
