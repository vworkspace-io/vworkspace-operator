# Helm-first

**Status:** Alpha
**Last Updated:** 2026-05-28

The single most important architectural choice in `vworkspace-operator` is that **application deployment logic lives in upstream Helm charts, not in the operator**. The operator's job is to express Odoo's intent in a vocabulary a Helm controller already speaks — specifically a Flux `HelmRelease` — and to aggregate the resulting conditions back into the operator's own CRDs. This page records why we chose that posture and, given that we did, which Helm engine we ship by default.

## Why Helm-first

Three observations drove the decision.

- **The charts already exist.** Nextcloud, Mattermost, Vaultwarden, Immich, Gitea, OnlyOffice, and the rest of the curated vWorkspace catalog all ship Helm charts that already encode Deployments, Services, Ingresses, init Jobs, upgrade hooks, and integration with cert-manager and external-secrets. Re-implementing any of that in the operator would duplicate code that is already battle-tested, maintained, and supported by the upstream community. The chart authors know their applications better than we do.
- **The Helm contract is small and stable.** A chart is a templated set of Kubernetes manifests plus a values schema and an optional set of `helm.sh/hook` jobs. The contract between "the thing that decides what an application looks like" and "the thing that decides when to apply it" has been stable for years and is supported by every major Kubernetes platform. We can hold that line.
- **The platform must not become Helm.** The moment the operator starts re-implementing Helm internals — release history, chart-source authentication, drift detection, rollback — its surface area expands without bound. The operator becomes responsible for everything every chart could possibly do, which is a strictly larger problem than "deploy a curated catalog of self-hosted apps coherently." We prefer a small operator over a clever one.

The corollary is that the operator never contains app-specific code. There is no `nextcloud_controller.go`, no `mattermost_upgrade.py`, no per-app conditional logic anywhere in the reconciler. Day-2 work that does require knowledge of the application is expressed either through chart hooks (the chart knows) or through templated `Operation` engines (Velero for backup, Argo Workflows for multi-step DAGs); see [day-2-operations.md](day-2-operations.md).

## The three Helm-engine options

Given that the operator delegates Helm execution, the question becomes "delegate to what?" Three options were considered.

### Option A: Operator manages Helm directly (Helm SDK)

The operator embeds the Helm SDK (or shells out to a vendored `helm` binary), watches `ApplicationInstance`, and runs `helm upgrade --install` itself. Release state, history, drift remediation, and rollback live in the operator's own datastore.

- **Pros.** One controller for the whole pipeline; no extra dependency on the cluster; tightest integration possible.
- **Cons.** The operator now re-implements retries, drift handling, release histories, chart-source authentication, concurrency limits, upgrade windows, and rollback semantics. All of these already exist in production-grade Helm controllers. The platform effectively becomes Helm with extra steps. Integration with a future GitOps mode is awkward, because there is no Kubernetes-shaped resource the operator publishes that Git can render.
- **Verdict.** Rejected for the default deployment. Acceptable as a narrow fallback in a hypothetical air-gapped edge scenario, and only then.

### Option B1: Flux Helm Controller (`HelmRelease`)

The operator reconciles `ApplicationInstance` into a Flux `HelmRelease` (and the matching `HelmRepository` or `OCIRepository` for the chart source). Flux Helm Controller reconciles the Helm lifecycle. Source Controller fetches the chart artifact.

- **Pros.** Purpose-built Helm reconciliation. Native, well-documented condition types. Drift remediation and rollback have known semantics. The `HelmRelease` resource is itself a first-class Kubernetes object, which means it can be rendered by GitOps, inspected with `kubectl`, and observed by any Prometheus scrape that knows about Flux. Strong ecosystem fit: cert-manager, external-secrets, and Velero all play well with Flux's notion of namespaces and ownership.
- **Cons.** Requires installing Flux components on every cluster. We treat this as a bootstrap cost, paid once via the operator's install bundle (see [../install/README.md](../install/README.md)). The boundary between operator-owned fields on the `HelmRelease` and human-owned fields must be documented; in practice it is "the operator owns everything it writes, the human can edit nothing without taking over the field manager."
- **Verdict.** **This is the default.** It is the cleanest expression of "the operator orchestrates; Helm reconciles."

### Option B2: Argo CD `Application`

The operator reconciles `ApplicationInstance` into an Argo CD `Application` pointing at a chart repository or a Git repository plus values. Argo CD manages sync and health.

- **Pros.** Mature UI, healthy ecosystem, and a natural fit for organizations already standardized on Argo. The same `Application` resource expresses both Git-rendered and chart-rendered intent, which simplifies GitOps adoption.
- **Cons.** Argo's strongest path is Git-first; chart-with-values overrides require more conventions. The operator now bridges two control planes — its own intent in `ApplicationInstance`, plus Argo's in `Application` — which is more rope than the project needs in its early life. Day-2 operations across both control planes complicate the audit story.
- **Verdict.** Supported as an adapter for organizations that explicitly want Argo. The Flux path is the one the project actively maintains and tests by default.

### Option C: Hybrid

A Helm controller as the primary engine, plus a narrow direct-Helm fallback for environments where Flux cannot be installed.

- **Pros.** Maximum flexibility. Allows phased adoption.
- **Cons.** Two execution paths must be tested and supported. Two sets of conditions must be mapped into the operator's vocabulary.
- **Verdict.** Out of scope for the default. The cost of maintaining two engines outweighs the benefit unless and until a concrete air-gapped customer requirement forces it.

## Recommendation

**The default deployment uses Option B1 — Flux Helm Controller — as the Helm engine, and Pull mode as the control-plane connectivity model.** This combination is what the published Helm bundle installs and what the `curl | bash` installer configures (see [../install/README.md](../install/README.md)). Argo CD is supported as a documented alternative; direct Helm is supported only as a defensive option for highly constrained environments.

The reasons line up with the goals in [overview.md](overview.md):

- **Helm-first by design.** Upstream charts remain the authoritative deployment logic. The operator does not reimplement chart internals.
- **Small operator.** Flux owns release lifecycle. The operator owns intent and status.
- **Clean separation.** Flux reconciles Helm; Velero reconciles backups; Argo Workflows reconciles multi-step DAGs; the operator wires and observes.
- **Composable with GitOps.** Because the operator's outputs are Kubernetes resources (`HelmRelease`, `Backup`, ...), the same outputs can be rendered to Git for [GitOps mode](../connectivity/gitops-mode.md) without changing the in-cluster reconciliation loop.
- **One operator per cluster, surviving Odoo outages.** Because Flux is in the cluster, Helm reconciliation continues whether or not Odoo is reachable. The operator's job during an Odoo outage is to keep telling Flux the truth it already knows.

Where this leaves the operator is a comfortable place: it is the layer that translates "what Odoo wants" into "what Flux understands", and translates "what Flux is doing" back into "something Odoo and the AI assistant can reason about." Everything that is genuinely about Helm stays inside Helm; everything that is genuinely about Odoo stays inside Odoo; the operator sits cleanly between them.
