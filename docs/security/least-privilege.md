# Least privilege and separation of duties

**Status:** Alpha
**Last Updated:** 2026-05-28

This document explains the separation-of-duties model the operator is built on. The mechanical RBAC reference — every `ClusterRole`, `Role`, and `RoleBinding` — is in [rbac.md](rbac.md); this document is the why behind those rules and the blast-radius reasoning that justifies the extra wiring.

## The principle

The operator does one thing: it materializes the cluster's expression of intent (a `HelmRelease`, a `velero.io/Backup`, a `Workflow`, a `Job`, a `VolumeSnapshot`, an `ExternalSecret`) and reads the resulting status. It does not run the workload, sync the secret, take the snapshot, or drive the workflow. Each of those is the job of a controller that already exists, has its own audit history, and runs under its own service account.

The corollary: a controller's blast radius is bounded by the credentials it holds. If the operator does not need to be `cluster-admin`, it should not be. If the operator does not need to read every Secret in the cluster, it should not. If the workload inside a Job does not need to call the Kubernetes API at all, the Job's Pod should run under a ServiceAccount with no API rights. The platform is a chain of small principals doing small jobs, not one large principal doing every job.

## The principals on a vWorkspace cluster

A running cluster has the following principals, with their reciprocal credentials:

| Principal                              | What it does                                                                                  | Where its credentials live                                                                                            |
|----------------------------------------|-----------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------|
| `vworkspace-app-operator` (the operator) | Reconciles `ApplicationInstance` and `Operation`; materializes child resources; reads status. | ServiceAccount `vworkspace-system/vworkspace-app-operator`; `ClusterRole` for its own CRDs; per-namespace `Role`s for child resources. |
| `helm-controller`, `source-controller` (Flux) | Reconciles `HelmRelease`, `HelmRepository`, `OCIRepository`. Runs the chart.                | ServiceAccount in the `flux-system` (or `vworkspace-system`) namespace; its own RBAC; not impersonated by the operator. |
| `velero`                                | Reconciles `velero.io/Backup` and `velero.io/Restore`. Talks to object storage and CSI snapshot controller. | ServiceAccount in the `velero` namespace; its own RBAC; not impersonated by the operator.                              |
| `external-secrets-controller`          | Reconciles `ExternalSecret`; creates/updates target `Secret`s; talks to external secret stores. | ServiceAccount in `external-secrets`; its own RBAC; not impersonated by the operator.                                  |
| `cert-manager`                         | Reconciles `Certificate`; talks to ACME / DNS providers; creates TLS Secrets.                | ServiceAccount in `cert-manager`; its own RBAC; not impersonated by the operator.                                       |
| `argo-workflows-controller`            | Reconciles `Workflow` and `WorkflowTemplate`; schedules Pods.                                 | ServiceAccount in `argo`; its own RBAC; not impersonated by the operator.                                              |
| `vworkspace-operation-runner` (per managed namespace) | Runs Pods inside Jobs and Workflows created by the operator. Has only the rights the template needs. | ServiceAccount per managed namespace; small `Role` per RBAC profile; not impersonated by the operator.                  |
| CSI snapshot controller and storage drivers | Reconcile `VolumeSnapshot`; talk to the underlying storage system.                            | Controller's own ServiceAccount; storage-system credentials in its own Secrets; not impersonated by the operator.       |
| Chart-rendered ServiceAccounts          | Run the application's own workloads (e.g., the Nextcloud Pod's ServiceAccount).                | Created by the chart in the application's namespace; the operator does not modify them.                                |

The operator does not impersonate any of these. It writes a CR that the matching controller reconciles. The most important property of this list is that compromise of any one principal does not give an attacker the others. A compromised Velero pod cannot create arbitrary `HelmRelease`s; a compromised operator pod cannot read external-secrets's outbound credential to the upstream secret store.

## What the operator can do

| The operator can…                                                                                       | The operator cannot…                                                                                                            |
|---------------------------------------------------------------------------------------------------------|---------------------------------------------------------------------------------------------------------------------------------|
| Create, update, delete `ApplicationInstance` and `Operation` cluster-wide.                              | Read or modify Secrets in any namespace other than by `get`-by-name from `spec.values.secretRef` references.                    |
| In a managed namespace: create, update, delete `HelmRelease`, `Workflow`, `Job`, `VolumeSnapshot`, `ReplicationSource`/`Destination`. | Run Pods directly. Pods are only created by the controllers that reconcile the CRs the operator writes.                          |
| In the Velero namespace: create, update, delete `Backup` and `Restore`.                                  | Modify Velero's `BackupStorageLocation` credentials at runtime. Initial install writes them; running reconciler does not.        |
| Read its own bootstrap credential (a Kubernetes Secret in `vworkspace-system`).                          | Read Flux's, Velero's, or external-secrets's credentials.                                                                       |
| Read Pod status and logs in managed namespaces (for `Operation.status.outputs.logsRef`).                 | `exec` into any Pod. Quiesce hooks are run by the workflow runner, not the operator.                                            |
| Read Namespace metadata cluster-wide.                                                                   | Create or modify Namespaces.                                                                                                    |
| Read CustomResourceDefinitions (to validate that required CRDs are installed).                            | Modify CRDs. CRD lifecycle is owned by the operator's Helm chart, applied by Flux on the cluster, not by the operator at runtime. |
| Talk outbound to Odoo (Pull mode) or accept inbound CR writes from Odoo (Push mode).                     | Talk to the cluster API as anyone other than its own ServiceAccount.                                                            |

## Blast radius if the operator is compromised

The operator's Secret store and ServiceAccount are the credentials an attacker would target. The realistic worst case:

- An attacker with the operator's ServiceAccount token can create arbitrary `HelmRelease`s in managed namespaces. This translates to "an attacker can run any Helm chart inside the cluster, scoped to whichever ServiceAccount the chart wires up". This is not nothing — a chart can install a Pod that mounts the host filesystem if Pod Security Admission allows it — but it is bounded by PSA "restricted" (the default), and the chart cannot escalate beyond what its rendered service accounts permit.
- The attacker can create arbitrary `velero.io/Backup` resources. This translates to "an attacker can request a backup of a managed namespace and exfiltrate its contents through the configured object storage". The mitigation is the network-layer control on the `BackupStorageLocation` (egress firewall) and the audit stream that reports unexpected backups.
- The attacker can create arbitrary `Workflow` and `Job` resources. The workflows and jobs run under `vworkspace-operation-runner`, which has only the narrow rights described in [rbac.md](rbac.md). Lateral movement out of the operation runner into another principal is bounded by what the runner can read.
- The attacker cannot directly read Secrets in arbitrary namespaces (no `list secrets`). They can however read Secrets they reference by name from a chart they install — same as any chart author.

The defensive posture against this is twofold: the operator's ServiceAccount token is mounted only inside the operator's pod (no automount on other pods); and the operator's outbound credential to Odoo is in a separate Secret (`vworkspace-operator-credentials`) the operator reads at startup and refreshes via rotation, not on every reconcile.

## Blast radius if Velero is compromised

Velero has read access to every namespace it can back up. If Velero is compromised, an attacker can read every Secret, ConfigMap, and PV in those namespaces, and exfiltrate them through the `BackupStorageLocation`. This is intrinsic to what Velero is and why a separate principal carries this power. The operator's role here is to ensure that Velero runs in its own namespace, with its own ServiceAccount, with its credentials in its own Secrets, so the operator's compromise does not imply Velero's compromise.

Operationally:

- The Velero pod's image and binary are pinned by the bundle's Helm chart; an attacker who can update the chart can run anything as Velero, but that requires the operator (which the operator chart already trusts) or a human with `cluster-admin`.
- The `BackupStorageLocation` credential is a Kubernetes Secret in the `velero` namespace; the operator has no rights on Secrets in that namespace.
- Egress to the storage backend is the only outbound path Velero needs. A NetworkPolicy on the `velero` namespace that restricts egress to the configured object-storage endpoints is recommended in production.

## Blast radius if external-secrets is compromised

external-secrets has the rights to create/update `Secret` objects in the target namespaces. If it is compromised, an attacker can overwrite Secrets — including the operator's own credentials Secret in `vworkspace-system` if external-secrets is configured to write there. The realistic mitigations:

- Do not let external-secrets write to `vworkspace-system`. The operator's bundle confines external-secrets's RBAC to namespaces labeled `app.vworkspace.io/managed-by=vworkspace`.
- Outbound credentials to the upstream secret store (Vault, AWS Secrets Manager, etc.) are in external-secrets's own Secret store, not in the operator's.
- A NetworkPolicy on the `external-secrets` namespace restricts egress to the configured stores.

## Why this matters at install time

The install bundle (`vworkspace-app-operator` Helm chart) sets up the principals above. Three properties to verify before going to production:

1. The operator does **not** carry `cluster-admin`. Run `kubectl describe clusterrolebinding | grep vworkspace-app-operator`; the only binding is to the `vworkspace-app-operator` `ClusterRole` described in [rbac.md](rbac.md).
2. Velero, external-secrets, cert-manager, and Flux run in their own namespaces with their own ServiceAccounts. Run `kubectl get sa -A | grep -E '(velero|external-secrets|cert-manager|flux|argo)'`.
3. The operator's `vworkspace-operator-credentials` Secret is restricted to `vworkspace-system` and is not duplicated into any other namespace. Run `kubectl get secrets -A -l app.vworkspace.io/managed-by=vworkspace-system`.

If any of these fail, the install has drifted from the bundle. Reconcile to the chart before continuing.

## Related material

- [rbac.md](rbac.md) — RBAC reference with concrete YAML.
- [secrets-handling.md](secrets-handling.md) — Why the operator's `get`-by-name on Secrets is enough, and what happens when a chart's values contain credentials.
- [authentication.md](authentication.md) — Where the operator's outbound credential to Odoo lives and how it rotates.
- [threat-model.md](threat-model.md) — The adversary model that justifies the separation above.
