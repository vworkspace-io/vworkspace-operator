# Prerequisites

**Status:** Alpha
**Last Updated:** 2026-05-28

This document is the prerequisites checklist for installing `vworkspace-operator`. Most items are properties of the cluster you already run; a few have opinionated recommendations for the cases that warrant them.

## Kubernetes version

| Component             | Minimum supported | Recommended       | Tested by the project       |
|-----------------------|-------------------|-------------------|------------------------------|
| Kubernetes API server | 1.28              | 1.30 or newer     | 1.28, 1.30, 1.31             |
| `kubectl` (admin's machine) | 1.28        | Same major/minor as the cluster | n/a                |

The minimum is set by the CRDs (`apiVersion: apiextensions.k8s.io/v1` with status subresources and structural schemas) and the snapshot APIs the operator integrates with. Older clusters may work but are not tested.

Distro-specific notes (k3s, Talos, Harvester, EKS/GKE/AKS, single-node Docker host with embedded k3s) are in [kubernetes-distros.md](kubernetes-distros.md).

## CNI

Any standard CNI plugin is supported. The operator does not care which one is installed; it does not configure NetworkPolicies on its own (planned: see [../security/threat-model.md](../security/threat-model.md) open issues), it does not depend on any vendor-specific feature, and it does not depend on a service-mesh sidecar.

If you plan to enforce NetworkPolicies (recommended for production), the CNI must support `networking.k8s.io/v1` `NetworkPolicy`. Common choices that do: Cilium, Calico, Canal, Antrea, and the CNI shipped with EKS/GKE/AKS.

## Storage

The operator does not require any specific storage backend. Two recommendations for production:

- **A default StorageClass.** Most charts in the vWorkspace catalog request a PVC without specifying a `storageClassName`, and rely on the cluster's default. At least one StorageClass should have the `storageclass.kubernetes.io/is-default-class: "true"` annotation.
- **A `VolumeSnapshotClass` whose driver supports snapshots.** Velero's CSI path and the operator's `snapshot` engine both rely on the CSI snapshot controller and a `VolumeSnapshotClass` mapped to the storage driver. Without this, backups fall back to file-system backup (Velero's Restic/Kopia path), which is supported but slower and less consistent.

For single-node clusters with no cloud storage, `local-path` (from Rancher's `local-path-provisioner`) is a fine default StorageClass; snapshot support requires a snapshot-capable driver such as TopoLVM or Longhorn. The bundle works without snapshots (Velero falls back to Restic/Kopia for file-system backup), but the storage-only engine in [../operations/engines/csi-snapshots-volsync.md](../operations/engines/csi-snapshots-volsync.md) is unavailable until snapshots are wired up.

## Controllers installed by the operator's bundle

The Helm bundle installs the following controllers if they are not already present. Where you already run them (some operators standardize on cluster-wide versions), pass the chart values that point at your existing install rather than installing again.

| Controller             | Why                                                                                              | Bundle default          | Override                                              |
|------------------------|--------------------------------------------------------------------------------------------------|-------------------------|-------------------------------------------------------|
| cert-manager           | TLS for ingresses and chart-internal services.                                                    | Installed by the chart. | `certManager.installed=false` and provide your install.  |
| external-secrets       | Sync chart-values credentials from Vault / cloud secret stores.                                   | Installed by the chart. | `externalSecrets.installed=false`.                       |
| Flux Helm Controller and Source Controller | The default Helm engine ([ADR 0002](../adr/0002-helm-first-via-flux-helmrelease.md)). | Installed by the chart. | `flux.installed=false`.                                  |
| Velero                 | Backup and restore primitives ([engines/velero.md](../operations/engines/velero.md)).             | Installed by the chart. | `velero.installed=false`.                                |

Argo Workflows is **not** installed by the chart; the chart installs it only if you set `argoWorkflows.installed=true`. Operation templates that require Argo Workflows will be admitted but block (`Blocked=True/EngineNotInstalled`) until it is present.

## Ingress controller

You provide the ingress controller of your choice (Nginx Ingress Controller, Traefik, HAProxy Ingress, Contour, or the cloud provider's ingress). The chart does not install one. Application charts that need an ingress create the Ingress object; your controller routes to it.

Two notes:

- The chart enables cert-manager by default, so charts that request a TLS certificate will resolve through cert-manager. If you use a different certificate workflow (your own ACME client, a corporate PKI), set `certManager.installed=false` and bring your own.
- For air-gapped or single-node installs, k3s ships Traefik by default; if you run with `--disable=traefik` and bring Nginx, take note in [kubernetes-distros.md](kubernetes-distros.md).

## Egress

The operator's Pull-mode default needs outbound HTTPS (port 443) to the Odoo control plane. No inbound port on the cluster is required for Pull. Concretely:

- The operator pod's outbound HTTP client must reach `https://<odoo-host>` for `/api/agent/*` endpoints. The host and (optional) proxy are configured in `Cluster.spec.odoo.endpoint` and `Cluster.spec.odoo.proxy`.
- Velero needs outbound access to the configured `BackupStorageLocation` (S3, GCS, Azure Blob, MinIO, on-prem object store). Configure your egress firewall accordingly.
- external-secrets needs outbound access to whichever upstream secret store you use (Vault, AWS, GCP, Azure, Akeyless, etc.).
- cert-manager needs outbound access to your ACME endpoint (typically Let's Encrypt).
- The Helm engine needs outbound access to your chart sources (an OCI registry, a Helm repository).

Push and GitOps modes change the cluster-side egress profile: Push needs Odoo to reach the cluster API (an inbound credential on the cluster's side); GitOps needs the cluster to reach Git. See [../security/authentication.md](../security/authentication.md) for the credential model in each mode.

## RBAC to install

Installing the bundle requires `cluster-admin` privileges on the target cluster (or an equivalent role that can install CRDs, ClusterRoles, ClusterRoleBindings, and webhooks). This is true for the **install only**; the operator does not run with `cluster-admin` after install — see [../security/rbac.md](../security/rbac.md) and [../security/least-privilege.md](../security/least-privilege.md).

In practice, `helm install` runs as a user with `cluster-admin` (your `kubeconfig` admin context); the operator's runtime principal is the ServiceAccount the chart creates, which has only the rights described in the RBAC reference.

## Time

Time sync (NTP or equivalent) must be working on every node and on the admin's machine. The one-time registration token has a short TTL; signed payloads in Pull mode rely on timestamps; certificate issuance via cert-manager assumes clock skew within a few minutes. Most managed Kubernetes deployments handle this automatically; bare-metal clusters should run `chronyd` or `systemd-timesyncd`.

## Resource footprint

The bundle adds the following pods to the cluster (approximate steady-state, exclusive of applications):

| Pod                                  | Replicas | CPU request | Memory request | Notes                              |
|--------------------------------------|----------|-------------|----------------|-------------------------------------|
| `vworkspace-app-operator`            | 1        | 100m        | 128Mi          | Leader-elected; only one active.    |
| `helm-controller`                    | 1        | 100m        | 64Mi           | Flux.                                |
| `source-controller`                  | 1        | 50m         | 64Mi           | Flux.                                |
| `cert-manager` + `cert-manager-webhook` + `cert-manager-cainjector` | 1 each | ~100m total | ~150Mi total | Standard cert-manager footprint.    |
| `external-secrets`                   | 1        | 50m         | 64Mi           |                                     |
| `velero`                             | 1        | 100m        | 128Mi          | Plus node agents if file-system backup is enabled. |

A 2 vCPU / 4 GiB RAM single-node cluster is sufficient to install the operator and the bundle's controllers. Application workloads are on top of this footprint.

## What to have ready before starting

A short checklist before you run the first `helm install`:

- A kubeconfig pointing at the target cluster with `cluster-admin` rights (or equivalent for installing CRDs and webhooks).
- The cluster's default StorageClass identified (`kubectl get sc`). If none is default, decide whether to set one before install.
- A reachable Odoo URL (Pull mode) and the ability to log in as an organization admin to issue a registration token.
- The cluster's egress destinations approved in your firewall (Odoo URL, chart source registry, `BackupStorageLocation`, external secret store, ACME endpoint).
- For air-gapped installs: a private OCI registry mirror and a private Helm chart mirror, plus the procedure described in [offline-and-airgapped.md](offline-and-airgapped.md).

Once you have these, [quickstart.md](quickstart.md) is two commands.

## Related material

- [quickstart.md](quickstart.md) — The supported install path.
- [cluster-bootstrap.md](cluster-bootstrap.md) — The full six-step bootstrap.
- [../security/authentication.md](../security/authentication.md) — Credentials and connectivity modes.
- [../security/rbac.md](../security/rbac.md) — RBAC the operator ends up with.
