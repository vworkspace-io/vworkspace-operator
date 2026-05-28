# Install

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-28

This chapter is the installation reference for `vworkspace-operator`. It covers prerequisites, the supported quickstart path, distro-specific notes, the full cluster bootstrap procedure, offline and air-gapped installs, and clean uninstall.

The intended reader is a platform engineer or homelab operator who already runs a Kubernetes cluster (or is about to) and wants to put a working operator on it, register it with an Odoo control plane, and watch the first `ApplicationInstance` come up. The exact opinions ("Pull mode by default", "Flux Helm Controller as the Helm engine", "Velero for backups", "one operator per cluster") are the same ones described in the [concepts chapter](../concepts/README.md) and recorded as ADRs.

## Read in order

1. [prerequisites.md](prerequisites.md) — Kubernetes version, CNI, default StorageClass with snapshot support, ingress controller, egress to Odoo, RBAC required to install.
2. [quickstart.md](quickstart.md) — The supported install path: one Helm install plus one registration command. Validation steps.
3. [kubernetes-distros.md](kubernetes-distros.md) — Distro-specific gotchas for k3s, Talos, Harvester, EKS/GKE/AKS, and a single-node Docker host running embedded k3s.
4. [cluster-bootstrap.md](cluster-bootstrap.md) — The six-step bootstrap in full, including the one-time registration token exchange in Odoo.
5. [offline-and-airgapped.md](offline-and-airgapped.md) — Image mirroring, OCI chart mirroring, transferring the registration token, configuring HTTP/HTTPS proxy for outbound Pull-mode connections.
6. [uninstall.md](uninstall.md) — Pause reconciliation, optional final backup, delete `Cluster`, `helm uninstall`. What persists, how to fully purge.

## Choosing an install path

Most operators want the quickstart in [quickstart.md](quickstart.md). The longer paths exist for specific situations:

| Situation                                                                  | Read                                                                                            |
|----------------------------------------------------------------------------|-------------------------------------------------------------------------------------------------|
| Running a stock K8s cluster with internet access and DNS resolution.       | [quickstart.md](quickstart.md).                                                                  |
| Running k3s, Talos, Harvester, EKS/GKE/AKS, or single-node Docker with embedded k3s. | [quickstart.md](quickstart.md) plus the relevant section of [kubernetes-distros.md](kubernetes-distros.md). |
| First time setting up an organization in Odoo and registering a new cluster. | [cluster-bootstrap.md](cluster-bootstrap.md).                                                   |
| Air-gapped cluster or cluster behind a corporate proxy.                    | [offline-and-airgapped.md](offline-and-airgapped.md).                                            |
| Decommissioning a cluster or replacing the operator.                       | [uninstall.md](uninstall.md).                                                                    |

## What gets installed

The bundle's Helm chart (`vworkspace-app-operator`) installs:

- The operator deployment, ServiceAccount, ClusterRole/ClusterRoleBinding, Role/RoleBinding (see [../security/rbac.md](../security/rbac.md)), and the operator's two CRDs (`apps.vworkspace.io/v1alpha1`, `ops.vworkspace.io/v1alpha1`).
- The Flux Helm Controller and Source Controller (Helm engine).
- cert-manager (chart and ingress TLS).
- external-secrets (chart values that reference external secret stores).
- Velero (backup and restore primitives).
- A small set of `WorkflowTemplate` resources under `vworkspace-system`.

Argo Workflows is **not** installed by default — it is the only engine listed in [../operations/operation-templates.md](../operations/operation-templates.md) that the chart treats as optional. Install it separately if your operation templates require it.

The chart does not install an ingress controller or a CSI driver. Those depend on the distro and the storage you already run. The prerequisites doc lists what you need to have.

## Related material

- [../security/README.md](../security/README.md) — Security posture summary; the install ships these defaults.
- [../operate/README.md](../operate/README.md) — Day-2 operation of the operator itself once it is installed.
- [../adr/0002-helm-first-via-flux-helmrelease.md](../adr/0002-helm-first-via-flux-helmrelease.md) — Why Flux is the default Helm engine.
