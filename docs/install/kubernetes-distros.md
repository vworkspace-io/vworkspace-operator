# Kubernetes distros

**Status:** Alpha
**Last Updated:** 2026-05-30

`vworkspace-operator` is designed to install on stock Kubernetes and avoids distro-specific assumptions where possible. This document captures the small set of distro-specific gotchas worth knowing about before running [quickstart.md](quickstart.md) on each of the supported substrates: k3s, Talos, Harvester, managed K8s (EKS / GKE / AKS), and a single-node Docker host running embedded k3s.

The five-line summary, if you only read the first paragraph: every supported distro works with the same Helm install and the same registration step; the distro-specific items below are mostly about which ingress controller is present by default, which StorageClass exists, and whether Pod Security Admission and read-only-root filesystems impose constraints the chart already accommodates.

## k3s

k3s is the lightest-weight supported distro and the recommended choice for single-node and small-cluster self-hosted deployments. Two notes:

- **Default Traefik vs Nginx.** k3s ships Traefik as the default ingress controller, installed by the `k3s-server` service. The vWorkspace chart catalog defaults assume an Ingress class named `nginx`. There are three options:
  1. Run k3s with `--disable=traefik`, install Nginx Ingress Controller yourself, and use the catalog defaults.
  2. Keep Traefik, override the catalog entry's `ingress.className` to `traefik`, and verify the chart's Ingress annotations are Traefik-compatible.
  3. Run both controllers and pin per-application Ingress class.

   The first option is the most predictable in our testing matrix; pick it unless you have a reason to keep Traefik.

- **Local-path StorageClass.** k3s installs `local-path` as the default StorageClass. It does not support `VolumeSnapshot`. If you need snapshots (recommended for production), install Longhorn or TopoLVM, set their `VolumeSnapshotClass` as the default for snapshots (`snapshot.storage.k8s.io/is-default-class: "true"`), and verify with:

   ```
   kubectl get volumesnapshotclass
   ```

   The bundle works without snapshots (Velero falls back to Restic / Kopia file-system backups), but the CSI snapshot engine in [../operations/engines/csi-snapshots-volsync.md](../operations/engines/csi-snapshots-volsync.md) is unavailable until snapshots are wired up.

- **`servicelb` / `klipper-lb`.** k3s's default `LoadBalancer` provider is fine for single-node and small homelab clusters. If you run multi-node k3s, consider MetalLB; the operator does not depend on the choice.

## Talos

Talos is read-only-root, immutable, and ships with strict Pod Security Admission. The operator and the bundled controllers are written to be PSA-restricted compatible: no host paths, no privileged containers, no `hostNetwork`, no `hostPID`. Concretely:

- **PSA on the system namespaces.** Apply `pod-security.kubernetes.io/enforce: restricted` on `vworkspace-system`, `velero`, `cert-manager`, `external-secrets`, and every managed namespace. The chart's pod templates pass `restricted` validation; the operator's mutating webhook enforces it on Job-engine Pods.

- **Read-only root.** Talos's read-only-root filesystem applies to the node, not to containers. All bundled containers run with their writable storage on emptyDirs or PVCs; no controller in the bundle writes to the node filesystem.

- **etcd snapshots.** Talos manages etcd; you do not need to back up etcd via Velero (and you should not try, since Talos's snapshot path is distinct). Velero in this bundle backs up workloads and their PVs, not control-plane state. Use Talos's `talosctl etcd snapshot` for control-plane backups.

- **Kernel parameters and security profiles.** Talos's defaults (seccomp `RuntimeDefault`, AppArmor where applicable) are already what the bundle expects.

## Harvester

Harvester is HCI-style: K8s plus KubeVirt plus Longhorn, deployable as a single appliance image or as a multi-node cluster. The relevant items:

- **Longhorn as the default StorageClass.** Harvester ships Longhorn; the bundle works out of the box with Longhorn's `VolumeSnapshotClass`. Verify:

   ```
   kubectl get sc
   kubectl get volumesnapshotclass
   ```

   If `longhorn-snapshot` is not the default, mark it explicitly with `snapshot.storage.k8s.io/is-default-class: "true"` so Velero picks it up.

- **Ingress.** Harvester does not ship an Ingress controller for tenant workloads by default; you install Nginx (or Traefik) into the cluster. The operator does not care which.

- **Guest workloads.** Harvester runs both KubeVirt VMs and pods. The operator targets pods only. If you intend to run vWorkspace applications as Kubevirt VMs (an experimental path, see the parent project's ROADMAP), this operator is not the right tool; it is the Helm-application operator.

- **RKE2 underlay.** Harvester's underlying K8s is RKE2. Treat it as a stock K8s 1.28+ cluster; nothing in the bundle is RKE2-specific.

## Managed K8s (EKS, GKE, AKS)

The bundle installs without distro-specific modification on EKS, GKE, and AKS. Per-cloud notes:

### EKS

- **EBS CSI driver.** The Amazon EBS CSI driver supports CSI snapshots; install `aws-ebs-csi-driver` if it is not present and create a `VolumeSnapshotClass` referencing `ebs.csi.aws.com`. Velero's S3 backend pairs naturally with EBS snapshots.
- **IAM for Velero and external-secrets.** Use IRSA (IAM Roles for Service Accounts) for Velero's S3 access and external-secrets's AWS Secrets Manager access. The bundle's Helm values include hooks for the IRSA role ARN.
- **Default StorageClass.** Mark `gp3` (or `gp2`) as default if you have not already.

### GKE

- **PD CSI driver.** GKE ships the `pd.csi.storage.gke.io` driver and a `VolumeSnapshotClass` (`csi-gce-pd-snapshot-class`). No extra install required.
- **Workload Identity.** Recommended for Velero and external-secrets; the bundle's Helm values include hooks for Workload Identity bindings.
- **Default StorageClass.** GKE Standard ships `standard-rwo` as default.

### AKS

- **Azure Disk CSI driver.** Azure ships `disk.csi.azure.com` with a default `VolumeSnapshotClass`. Verify.
- **Azure AD Workload Identity.** Recommended for Velero (Azure Blob backend) and external-secrets (Azure Key Vault).
- **Default StorageClass.** AKS ships `default` (Azure managed disks) as the default.

In all three, the bundle's `cert-manager` and Pull-mode outbound to the control plane work without further changes; the only cluster-specific items are the storage and the IAM/Workload-Identity bindings for Velero and external-secrets.

## Single-node Docker host with embedded k3s

The smallest supported substrate. The vWorkspace control plane's `curl | bash` installer (for the control-plane side of the world) targets this scenario; the operator side is identical to k3s above, with a few additional considerations:

- **Embedded k3s lifecycle.** k3s is installed by the host's package manager (`curl -sfL https://get.k3s.io | sh -`). The operator does not manage k3s itself.
- **Local-path persistence.** Single-node deployments commonly use `local-path` for PVs. This is fine for development and small homelab; for production-grade backup, install Longhorn (or another snapshot-capable driver) even on a single node.
- **No multi-node Velero requirements.** Velero's node-agent (formerly Restic) DaemonSet places a pod on every node; on a single-node cluster, that is one pod. Configure storage in the usual way.
- **DNS for application URLs.** On a single-node host that is not on a managed DNS plane (homelab behind NAT, for instance), use a DNS provider you control (Cloudflare with API access for the vWorkspace Server control plane's `curl | bash` installer is the common path; the operator does not configure DNS for you).
- **Resource budget.** A 2 vCPU / 4 GiB RAM host comfortably runs the operator and one or two small applications (Nextcloud + Vaultwarden). For Mattermost or OnlyOffice, plan for 4 vCPU / 8 GiB RAM minimum.

## Verifying any distro

Independent of the distro, the following one-liners are useful right after `helm install`:

```
kubectl get nodes -o wide
kubectl get sc
kubectl get volumesnapshotclass
kubectl get pods -A
kubectl get crd | grep -E '(vworkspace.io|fluxcd|velero|cert-manager|external-secrets)'
```

If `kubectl get volumesnapshotclass` returns nothing, decide before going to production whether you can live with file-system backups only or whether to install a snapshot-capable driver.

## Related material

- [prerequisites.md](prerequisites.md) — General prerequisites.
- [quickstart.md](quickstart.md) — Two-command install.
- [cluster-bootstrap.md](cluster-bootstrap.md) — Full bootstrap and registration.
- [../operations/engines/csi-snapshots-volsync.md](../operations/engines/csi-snapshots-volsync.md) — Why snapshots matter and what works without them.
