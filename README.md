# vworkspace-operator

[![CI](https://github.com/vworkspace-io/vworkspace-operator/actions/workflows/ci.yml/badge.svg)](https://github.com/vworkspace-io/vworkspace-operator/actions/workflows/ci.yml)
[![Docs](https://img.shields.io/badge/docs-operator.docs.vworkspace.io-blue)](https://operator.docs.vworkspace.io/)
[![License: AGPL-3.0](https://img.shields.io/badge/License-AGPL--3.0-blue.svg)](LICENSE)

> **Status:** Alpha — CRDs are at `v1alpha1` and the API may change before `v1.0.0`. Pin versions in production and read the [CHANGELOG](CHANGELOG.md) before upgrading.

**vworkspace-operator** is the cluster-side Kubernetes operator for [vWorkspace](https://github.com/vworkspace-io/vworkspace). It runs in your cluster, reconciles a small set of custom resources, and connects to **[vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server)** — the control plane — so applications can be installed, upgraded, backed up, and operated without giving the control plane a kubeconfig or opening inbound ports on the cluster.

The operator is **Helm-first**: every application is deployed from an upstream Helm chart (via [Flux Helm Controller](https://fluxcd.io) by default). Day-2 work is modeled as Kubernetes resources (`ApplicationInstance`, `Operation`) reconciled by the operator and proven third-party controllers (Velero, Argo Workflows, CSI snapshots, VolSync), not as imperative scripts on the control-plane side.

## Architecture at a glance

```text
┌─────────────────────┐         Pull / Push / GitOps          ┌──────────────────────┐
│  vWorkspace Server  │  ◄──────────────────────────────────► │ vworkspace-operator  │
│  (control plane)    │         narrow HTTP / CRD contract      │  (in your cluster)   │
└─────────────────────┘                                       └──────────┬───────────┘
                                                                           │
                                                                           ▼
                                                                Flux, Velero, workloads…
```

| Component | Role |
|-----------|------|
| [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) | Control plane (built on Odoo 19). Declares intent, creates Pull-mode jobs, receives status and audit events. Holds cluster identity and credentials — not a kubeconfig. |
| **vworkspace-operator** | Cluster agent. Reconciles `ApplicationInstance` and `Operation` CRDs, executes Pull-mode jobs, reports health. |
| **Kubernetes** | Execution environment. One operator deployment per cluster. |

**Pull mode** (default): the operator initiates outbound HTTPS to the control plane (`/api/agent/*`), fetches jobs, applies them locally, and posts status. **Push** and **GitOps** modes use the same CRDs with different transport. See [docs/connectivity/](docs/connectivity/README.md).

## Who this is for

- Platform teams running k3s, Talos, Harvester, managed Kubernetes, or homelab clusters who want a coherent, AGPL-licensed control plane.
- Organizations managing multiple clusters from one vWorkspace Server without exposing the Kubernetes API to the internet.
- Teams that prefer two predictable CRDs (`ApplicationInstance`, `Operation`) over dozens of per-app operators.

## Quick start

1. Install prerequisites ([docs/install/prerequisites.md](docs/install/prerequisites.md)).
2. Deploy the operator — **without cloning this repo**, use a [GitHub Release](https://github.com/vworkspace-io/vworkspace-operator/releases) chart or kubectl manifests ([docs/install/helm.md](docs/install/helm.md#install-from-github-release)):

```bash
helm upgrade --install vworkspace-operator \
  https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/vworkspace-operator-0.0.6.tgz \
  --version 0.0.6 \
  -n vworkspace-system \
  --create-namespace \
  --set image.tag=v0.0.6
```

Or apply manifests:

```bash
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/crds.yaml
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.6/operator.yaml
```

For development from a checkout: `make deploy IMG=docker.io/vworkspace/vworkspace-operator:latest` ([docs/install/quickstart.md](docs/install/quickstart.md)).

3. Register the cluster with vWorkspace Server and enable Pull mode ([docs/connectivity/pull-mode.md](docs/connectivity/pull-mode.md)):

```bash
./bin/manager register \
  --control-plane-endpoint https://your-server.example.org \
  --token <one-time-registration-token>
```

For local development without a real control plane, use the [mock control plane](docs/development/mock-control-plane.md).

## Documentation

Full documentation is indexed at [docs/README.md](docs/README.md). Highlights:

| Section | Description |
|---------|-------------|
| [Concepts](docs/concepts/README.md) | Architecture, reconciliation model, glossary |
| [Connectivity](docs/connectivity/README.md) | Pull, Push, GitOps modes and job protocol |
| [API reference](docs/api/README.md) | `ApplicationInstance`, `Operation`, `Cluster` CRDs |
| [Install](docs/install/README.md) | Helm, bootstrap, air-gapped installs |
| [Security](docs/security/README.md) | Authentication, RBAC, threat model |
| [Development](docs/development/README.md) | Build, test, mock control plane |
| [ADRs](docs/adr/README.md) | Architecture decision records |

Published docs: **[operator.docs.vworkspace.io](https://operator.docs.vworkspace.io/)** — setup in [docs/publication.md](docs/publication.md).

## Project status

Phase 1 foundation is in tree: Kubebuilder scaffold, reconcilers, Flux and Velero engines, Pull-mode agent loop, Helm chart, and CI (`make test`, `make lint`, e2e on kind). See [ROADMAP.md](ROADMAP.md) and [docs/development/IMPLEMENTATION_GUIDE.md](docs/development/IMPLEMENTATION_GUIDE.md).

Images: `docker.io/vworkspace/vworkspace-operator` — [container image docs](docs/install/container-images.md).

## License

Licensed under the [GNU Affero General Public License v3.0](LICENSE), consistent with the vWorkspace project.

## Community

- [Contributing](CONTRIBUTING.md) · [Code of Conduct](CODE_OF_CONDUCT.md) · [Security policy](SECURITY.md)
- [Governance](GOVERNANCE.md) · [Support](SUPPORT.md)
- Parent project: [vworkspace-io/vworkspace](https://github.com/vworkspace-io/vworkspace)
