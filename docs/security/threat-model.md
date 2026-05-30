# Threat model

**Status:** Alpha
**Last Updated:** 2026-05-30

This document is the project's threat model for `vworkspace-operator`. It is deliberately concise: a list of assets, a list of adversaries, the trust boundaries we recognize, and the mitigations mapped onto the controls described elsewhere in this chapter. It also lists the open issues we know about and have not yet mitigated.

The intent is to be honest. If a control is "planned" rather than implemented, it is labelled "planned"; if a mitigation is partial, the gap is named. The threat model is meant to be argued with; new threats and revised mitigations belong as ADRs or RFCs that update this document.

## Assets

The assets the operator protects, in roughly descending order of impact if compromised:

| Asset                                        | Why it matters                                                                                                                              |
|----------------------------------------------|----------------------------------------------------------------------------------------------------------------------------------------------|
| The cluster Kubernetes API                   | A compromise here is total: every workload, every Secret, every PV. The cluster API is the highest-value target on the machine.            |
| Persistent data in managed namespaces (PVs)  | The actual customer data: Nextcloud files, Mattermost message history, Vaultwarden vault, Postgres tables.                                |
| Chart values that contain credentials         | A leaked SMTP password or a leaked OIDC client secret is a credible blast radius — see [secrets-handling.md](secrets-handling.md).         |
| The operator's outbound credential to the control plane    | A token that lets an attacker fetch jobs as the cluster, post status as the cluster, and (in some signing configurations) push payloads.  |
| The audit-event stream                        | The chain of custody for what was asked and what happened. A compromised audit stream undermines incident response.                       |
| The cluster's own private signing key (Pull) | The decryption key for any encrypted payloads the cluster has consumed; also the cluster's identity in signed-payload deployments.       |
| The operator's container image                 | An attacker who controls what binary runs as the operator owns everything the operator owns.                                              |

## Adversaries

We design against five adversary types.

### A1: External internet

A remote, unprivileged attacker on the public internet who has no credentials and is not on the cluster's network. They can attempt to:

- Reach the cluster API directly (typically blocked at the network boundary in self-hosted deployments behind NAT/firewall).
- Reach the operator's metrics or health endpoints if they are exposed.
- Brute-force or replay credentials they obtain elsewhere.

### A2: Lateral attacker in the cluster

An attacker who has gained code execution in some workload Pod on the cluster — a compromised application container, a malicious image, a vulnerable third-party Pod. They can attempt to:

- Read Secrets in their own namespace.
- Reach the Kubernetes API as their Pod's ServiceAccount.
- Discover other Pods on the network and reach them on cluster-internal addresses.
- Steal the operator's outbound credential if they can reach `vworkspace-system`.

### A3: Compromised Odoo

The vWorkspace Server control plane is compromised — the database is altered, the application is malicious, or an attacker has obtained Odoo admin credentials. They can attempt to:

- Issue destructive `Operation` requests on every cluster Odoo manages (delete an `ApplicationInstance`, restore an old backup, run an arbitrary `RunCommand`).
- Modify the `ApplicationInstance` to inject chart values that exfiltrate data or install a back door.
- Modify the catalog to mark a malicious chart as "recommended".
- Issue new bootstrap credentials for clusters and stop accepting the current ones.

### A4: Compromised chart author or catalog

A chart referenced by the catalog ships malicious code, or the catalog itself is compromised and points at a malicious chart. They can attempt to:

- Install a chart that runs a privileged Pod (mount the host filesystem, escape PSA).
- Install a chart whose rendered ServiceAccount has broader RBAC than the chart needs.
- Use chart hooks to exfiltrate data through outbound calls during install/upgrade.

### A5: Compromised maintainer or supply chain

An attacker compromises the operator's build pipeline, signing keys, or release publishing. They can attempt to:

- Ship a malicious operator binary or container image.
- Ship a malicious Helm chart for the operator itself.
- Add malicious dependencies (Go modules) that compromise the operator at build time.

## Trust boundaries

The operator recognizes the following trust boundaries:

| Boundary                                | What is on each side                                                                                                                                            |
|-----------------------------------------|-----------------------------------------------------------------------------------------------------------------------------------------------------------------|
| Cluster ⟷ Odoo                          | The HTTPS connection between the operator and Odoo. Pull-mode default: outbound from the cluster. Push: inbound to the cluster API. GitOps: both sides talk to Git. |
| Operator ⟷ third-party controllers       | The operator writes CRs; Flux/Velero/Argo/etc. reconcile them. Each runs under its own ServiceAccount; the operator does not impersonate them.                  |
| Operator ⟷ chart authors                  | The chart is code the operator runs without inspecting its internals. The chart is bounded by PSA and by its own rendered ServiceAccounts.                       |
| Operator ⟷ humans                        | Cluster admins can edit any resource the API server admits. The operator's mutating webhook does not stop a human with the right RBAC.                          |
| Tenant namespaces ⟷ vworkspace-system   | The operator's own credentials and signing keys live in `vworkspace-system`. No tenant workload has access to it.                                                |

## Mitigation mapping

Mitigations are listed against the assets they protect and the adversaries they bound.

| Mitigation                                                                                            | Protects asset(s)                                            | Bounds adversary(ies)                                            |
|--------------------------------------------------------------------------------------------------------|--------------------------------------------------------------|------------------------------------------------------------------|
| Outbound-only HTTPS to the control plane (Pull mode default).                                                       | Cluster API.                                                  | A1 (no inbound port to exploit).                                  |
| Operator does not hold `cluster-admin`; cluster-scoped rights limited to its own CRDs.                 | Cluster API; PVs.                                             | A2 (a compromised operator pod cannot read every Secret).         |
| Namespace-scoped `Role`s only in namespaces labeled `app.vworkspace.io/managed-by=vworkspace`.         | PVs in unrelated namespaces.                                  | A2 (lateral movement bounded by label).                           |
| `get`-by-name on Secrets, never `list`/`watch`.                                                        | Chart-values credentials.                                     | A2.                                                               |
| Workload Pods inside Jobs/Workflows run under `vworkspace-operation-runner`, not the operator's SA.    | Cluster API.                                                  | A2; A4 (a compromised chart hook cannot escalate via the operator's SA). |
| PSA "restricted" required in every managed namespace; no host paths, no privileged containers.         | Cluster API; node integrity.                                  | A4 (malicious chart's privilege is bounded).                      |
| One-time registration token; long-lived credential never sent over a channel Odoo did not originate.   | Cluster identity.                                              | A1; A3 (a compromised Odoo cannot retroactively become a different cluster). |
| Per-cluster scoped bearer tokens; cross-cluster reads return 403.                                      | Audit-event stream; cluster identity.                          | A3 (a compromised cluster cannot impersonate another cluster); A2 (a stolen token works only for the cluster it was issued to). |
| HMAC over status posts (or mTLS); bearer-token-only is insufficient for writes.                        | Audit-event stream.                                            | A1; A2.                                                            |
| Optional payload signing.                                                                              | Operator integrity; chart values.                              | A3 (a tampered relay or message broker cannot inject jobs); partial mitigation for A3 if Odoo itself is compromised (Odoo holds the signing key). |
| Optional payload encryption to the cluster's public key.                                               | Chart values; in-flight credentials.                          | A3 (a passively logging relay learns nothing); A1.                |
| Admission webhook rejects inline secret material via the placeholder rule set in [secrets-handling.md](secrets-handling.md). | Chart values.                                                  | A3 (a compromised Odoo that tries to ship a credential as inline values is blocked at admission). |
| Mandatory `ownerReferences` and labels on every resource the operator creates.                          | Audit-event stream.                                            | A2; A3 (orphan creations are detectable).                          |
| Structured logging redacts credential-shaped values.                                                   | Operator credentials; chart-values credentials.                | A2 (a log scraper learns less); A5.                                |
| Signed container images and signed Helm releases for the operator itself.                              | Operator container image.                                      | A5.                                                                |
| One operator per cluster; cluster reconciles autonomously when Odoo is down.                            | PVs; running applications.                                     | A3 (a takedown of Odoo does not cascade into cluster outage).      |
| Separation of duties (Velero, external-secrets, cert-manager, Argo each have their own SAs).             | PVs; chart-values credentials.                                  | A2 (compromise of one controller does not give the attacker the others). |

The full list of controls is in this chapter's other documents:

- RBAC: [rbac.md](rbac.md).
- Service-account separation: [least-privilege.md](least-privilege.md).
- Secrets handling: [secrets-handling.md](secrets-handling.md).
- Authentication, signing, encryption, rotation: [authentication.md](authentication.md).

## Residual risks and assumptions

We assume:

- The cluster's node OS is patched, the container runtime is current, and CVE management is the operator's responsibility outside the scope of this operator. We do not own kernel-level mitigations.
- The chart catalog Odoo distributes is curated. We do not provide chart provenance verification today (see Open issues below); a compromised catalog can ship a malicious chart, which the operator will install.
- Odoo's database is backed up and its access is audited at the Odoo layer. The operator's audit-event stream does not substitute for control-plane-side audit.
- Time is roughly synchronized (NTP). The TTL on registration tokens and the signature timestamps in signed payloads rely on this.
- The Kubernetes API server is itself trustworthy. An attacker who compromises the API server has the cluster regardless of what the operator does.

## Open issues

The following are known limitations we plan to address. Until each is closed, the corresponding adversary or asset has weaker protection than the mitigations above suggest.

| Open issue                                                                          | Affects                       | Status   | Tracked in                                                                  |
|-------------------------------------------------------------------------------------|-------------------------------|----------|------------------------------------------------------------------------------|
| Chart provenance verification (signed chart digests, cosign or sigstore integration). | A4 (compromised chart author/catalog). | Planned  | RFC TBD; see [../rfcs/README.md](../rfcs/README.md).                          |
| Mandatory signed payloads in Pull mode (currently "warn but admit").                  | A3 (compromised Odoo); A1.     | Planned  | The default is expected to flip to "require" before v1.0.                    |
| NetworkPolicy templates per managed namespace (limit lateral reach).                  | A2.                            | Planned  | Bundle change; not yet on by default.                                        |
| Egress firewall enforcement for Velero / external-secrets (limit data exfiltration).  | A2; A4.                        | Operator-configurable; not enforced by the bundle. | Documented in [least-privilege.md](least-privilege.md).                       |
| SBOM and supply-chain attestation for the operator's container image.                 | A5.                            | Planned  | Release-process change; see [../development/release-process.md](../development/release-process.md). |
| Conversion-webhook-based CRD evolution audit (changes to allowed `Operation` types).  | A3.                            | Planned  | Part of v1alpha1 → v1beta1 promotion work.                                    |
| Honeyfile detection for credential exfiltration (decoy Secrets in `vworkspace-system`). | A2.                          | Not committed; mentioned for completeness. | None.                                                                       |

## Reporting a security issue

Security issues should be reported privately via the channel described in the project's `SECURITY.md` at the repository root. Do not file public GitHub issues for vulnerabilities. The threat model above is a living document; if you believe an adversary or asset is missing, please open an RFC.

## Related material

- [rbac.md](rbac.md)
- [least-privilege.md](least-privilege.md)
- [secrets-handling.md](secrets-handling.md)
- [authentication.md](authentication.md)
- [../adr/0003-pull-mode-as-default-connectivity.md](../adr/0003-pull-mode-as-default-connectivity.md)
