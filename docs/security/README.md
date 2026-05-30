# Security

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-30

`vworkspace-operator` runs in a cluster the operator owns, holds credentials the operator trusts it with, and reaches an vWorkspace Server control plane the operator runs themselves. The security posture is correspondingly self-hosted: minimum credentials, namespace-scoped actuation rights, no blanket cluster-admin, and an explicit threat model the operator can read, disagree with, and propose changes to.

This chapter is the security reference for `vworkspace-operator`. It is organized so an operator can read the chapter front to back and walk away with a concrete picture of what the operator can and cannot do, where its credentials live, what an attacker on each side of a trust boundary can achieve, and which mitigations are in place.

## Read in order

1. [rbac.md](rbac.md) — RBAC model. The operator's own `ClusterRole` for its CRDs, namespace-scoped `Role`s for actuation rights bounded to namespaces labeled `app.vworkspace.io/managed-by=vworkspace`, and example YAML.
2. [least-privilege.md](least-privilege.md) — Separation of duties. Velero, external-secrets, cert-manager, the Helm engine, and the Argo Workflows controller each run under their own service accounts. What the operator can and cannot do; blast-radius reasoning.
3. [secrets-handling.md](secrets-handling.md) — How chart values that contain secrets are stored, referenced, and rejected when inlined. The admission webhook rules and the recommended use of external-secrets.
4. [authentication.md](authentication.md) — Cluster identity, one-time registration tokens, long-lived bootstrap credentials, scoped tokens per cluster, optional mTLS, optional signed/encrypted payloads, token rotation, and how the control plane authenticates operator status posts.
5. [threat-model.md](threat-model.md) — Assets, adversaries, trust boundaries, mitigations mapped onto the controls above, and open issues.

## Threat model at a glance

The full threat model is in [threat-model.md](threat-model.md). The condensed version, in plain prose: the operator holds an outbound credential to the control plane (Pull mode) or accepts inbound writes from the control plane (Push mode) or syncs from Git (GitOps mode). Everything it does on the cluster — applying CRDs, creating `HelmRelease`s, creating `velero.io/Backup`s, creating `Workflow`s, creating `Job`s — is constrained to namespaces labeled `app.vworkspace.io/managed-by=vworkspace`. The Helm engine, Velero, external-secrets, and the workflow runner run under their own service accounts that the operator does not impersonate. Cluster-scoped power is limited to the operator's own CRDs.

The five adversaries we design against, in increasing order of intimacy:

| Adversary                          | What they can attempt                                                                                                                                | What the design prevents                                                                                                                       |
|------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------------------|
| External internet                  | Reach the cluster API directly; reach the operator's metrics/health endpoints.                                                                       | Pull mode requires no inbound port; metrics endpoints are scoped to in-cluster access via NetworkPolicy.                                        |
| Lateral attacker in the cluster    | Read operator credentials from disk; impersonate the operator's ServiceAccount; bypass admission webhooks.                                            | Credentials are Kubernetes Secrets restricted to the operator's namespace; the operator is not `cluster-admin`; webhooks are mTLS-enforced.    |
| Compromised Odoo                   | Issue jobs that create destructive `Operation` CRs on every cluster; modify chart values to inject secrets.                                            | Per-cluster scoped tokens; admission webhook gates `Operation.spec.type` per namespace; chart values may be required to use external-secrets; optional payload signing.  |
| Compromised chart author / catalog | Ship a chart that escalates privileges in the cluster.                                                                                                  | Charts run under namespace-scoped service accounts; PSA "restricted" is the default; chart provenance verification is planned (see open issues). |
| Compromised maintainer / supply chain | Ship a malicious operator release.                                                                                                                | Signed container images and signed Helm releases; release process documented in [../development/release-process.md](../development/release-process.md). |

## What the operator does, security-wise, on every reconcile

For every CR it admits, the operator:

- Validates the request against its admission webhook (operation template, namespace allow-list, RBAC profile required, capability declared).
- Sets ownership labels (`app.vworkspace.io/managed-by`, `app.vworkspace.io/cluster-id`) and an owner reference on the resources it creates, so `kubectl auth can-i` and the audit log can answer "who created this".
- Uses server-side apply with a deterministic field manager (`vworkspace-app-operator`) so its writes do not stomp human edits to fields it does not own.
- Logs the action via the controller-runtime logger with `cluster_id`, `org_id`, `namespace`, `application_instance`, and `operation_id` fields (see [../operate/observability.md](../operate/observability.md)).
- Emits a Kubernetes `Event` on the affected resource and a coalesced audit event to the control plane's `POST /api/agent/events` endpoint (Pull mode) or Odoo's equivalent in Push/GitOps mode.

Reads of secret material — chart values that resolve to a Secret, Velero credentials, the operator's own bootstrap credential — never appear in logs or events. The condition messages mirror Velero/Helm/Argo messages verbatim; those upstream controllers are not authorized to log secrets either, but the operator further redacts any value that looks like a secret based on a simple pattern set.

## Related material

- [../api/labels-and-annotations.md](../api/labels-and-annotations.md) — Well-known labels and capability annotations the operator reads and writes.
- [../operate/observability.md](../operate/observability.md) — Structured logs, metrics, and the audit-event stream to Odoo.
- [../adr/0003-pull-mode-as-default-connectivity.md](../adr/0003-pull-mode-as-default-connectivity.md) — Rationale behind Pull mode as the default.
