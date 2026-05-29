# Implementation guide

**Status:** Alpha — living handoff document for Phase 1 foundation work.
**Last Updated:** 2026-05-29
**Audience:** Engineers continuing vworkspace-operator development.

This document breaks Phase 1 into continuable sub-phases, defines acceptance criteria, and explains how to resume work on any day. It complements [ROADMAP.md](../../ROADMAP.md) (milestones) and [project-layout.md](project-layout.md) (directory contract).

## Source of truth

| Topic | Document |
|-------|----------|
| Product design | [ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md) |
| ApplicationInstance API | [docs/api/application-instance.md](../api/application-instance.md) |
| Operation API | [docs/api/operation.md](../api/operation.md) |
| Conditions | [docs/api/conditions.md](../api/conditions.md) |
| Pull-mode protocol | [docs/connectivity/job-protocol.md](../connectivity/job-protocol.md) |
| ADRs | [docs/adr/README.md](../adr/README.md) |

## Phase breakdown

### Phase 1a — Scaffold and CRDs (done)

**Goal:** Runnable Kubebuilder project with typed CRDs and generated manifests.

| Deliverable | Path | Status |
|-------------|------|--------|
| Go module | `go.mod`, `go.sum` | Done |
| ApplicationInstance types | `api/apps/v1alpha1/` | Done |
| Operation + Cluster types | `api/ops/v1alpha1/` | Done |
| Generated CRDs | `config/crd/bases/*.yaml` | Done |
| Kustomize install layout | `config/default/`, `config/manager/`, `config/rbac/` | Done |
| Makefile / Dockerfile / CI | `Makefile`, `Dockerfile`, `.github/workflows/ci.yml` | Done |
| Condition helpers | `internal/conditions/` | Done |
| Label constants | `internal/labels/` | Done |

**Acceptance criteria:** met (`make test`, `./hack/verify-generated.sh`).

### Phase 1b — Reconcilers, engines, and Pull-mode agent (done)

**Goal:** Idempotent reconciliation with interface-driven engines and a working Pull-mode job loop.

| Deliverable | Path | Status |
|-------------|------|--------|
| ApplicationInstance reconciler | `internal/controller/applicationinstance_controller.go` | MVP |
| Operation reconciler | `internal/controller/operation_controller.go` | MVP |
| Cluster reconciler | `internal/controller/cluster_controller.go` | MVP (heartbeat) |
| Flux Helm engine | `internal/helmengine/flux.go` | MVP (+ secretRef/configMapRef) |
| Helm upgrade engine | `internal/engines/helm.go` | MVP |
| Velero engine | `internal/engines/velero.go` | MVP |
| Engine registry | `internal/engines/registry.go` | Done |
| Agent credential loader | `internal/agent/credentials.go` | Done |
| Job applier (SSA) | `internal/agent/applier.go` | Done |
| Agent poller | `internal/agent/poller.go` | Done |
| Event batcher | `internal/agent/events.go` | Done |
| Wire agent in `cmd/main.go` | flags + goroutines | Done |
| Docker Hub publish | `.github/workflows/ci.yml` `docker` job | Done |

**Acceptance criteria**

- [x] Applying a valid `ApplicationInstance` creates `HelmRelease` + chart source (Flux).
- [x] Invalid spec sets `Blocked=True` without panicking.
- [x] `Operation` with `engine: velero` creates `velero.io/Backup`.
- [x] Pull-mode `apply` / `delete` / `intent` jobs applied with field manager `vworkspace-agent`.
- [x] Idempotent replay via `idempotencyKey`.
- [x] `values.secretRef` / `values.configMapRef` resolved into HelmRelease values.
- [x] Agent enabled via `--agent-enabled` and credentials Secret or flags.
- [x] `make test` and `make lint` pass.

**Tests**

- [x] `internal/agent/applier_test.go` — apply, delete, intent, idempotency.
- [x] `internal/agent/poller_test.go` — httptest end-to-end ack/apply/result.
- [x] `internal/agent/credentials_test.go` — Secret loading.
- [x] `internal/helmengine/flux_test.go` — secretRef/configMapRef values.

### Phase 1c — Install path and samples (next)

**Goal:** Documented end-to-end path on kind/k3s.

| Deliverable | Path |
|-------------|------|
| Sample CRs | `config/samples/` |
| Quickstart validation | `docs/install/quickstart.md` |
| RBAC review | `config/rbac/role.yaml` vs `docs/security/rbac.md` |

**Acceptance criteria**

- `make deploy IMG=...` installs operator + CRDs on kind.
- Sample `ApplicationInstance` reconciles when Flux CRDs are present.
- Velero CRD present for backup `Operation`.

## Dependency order

```mermaid
flowchart TD
  A[Phase 1a: CRD types + codegen] --> B[Phase 1b: Reconcilers]
  A --> C[internal/conditions + labels]
  B --> D[helmengine Flux adapter]
  B --> E[engines registry]
  E --> F[helm engine]
  E --> G[velero engine]
  A --> H[Phase 1b: agent HTTP + applier]
  H --> I[Cluster reconciler connectivity]
  B --> J[Phase 1c: samples + install docs]
  D --> J
  G --> J
```

## How to resume work

### Branch strategy

- `main` — merged Phase 1a/1b; container images published from CI.
- `feat/phase-1b-pull-mode` — Phase 1b Pull-mode and CI (this session).
- Future: `feat/phase-1c-install` for install hardening.

### Daily startup checklist

```bash
cd vworkspace-operator
git fetch origin
git checkout main   # or your topic branch
make setup-envtest  # first time only
make test
make run            # optional, against kind
```

### Definition of done (per sub-phase)

1. All acceptance criteria above are met.
2. `make test` and `./hack/verify-generated.sh` pass.
3. Relevant docs updated in the same PR.
4. CHANGELOG `[Unreleased]` entry added.

## Rollback and versioning

### Git tags

- Pre-release tags: `v0.0.x` aligned with [ROADMAP.md](../../ROADMAP.md).
- Container image tag matches git tag on release.

### Feature flags

| Flag / env | Purpose |
|------------|---------|
| `--odoo-base-url` / `ODOO_BASE_URL` | Odoo host for Pull-mode |
| `--agent-token` / `VWORKSPACE_AGENT_TOKEN` | Bearer token |
| `--cluster-id` / `VWORKSPACE_CLUSTER_ID` | Cluster identity |
| `--agent-enabled` | Start long-poll job loop |
| `--agent-poll-interval` | Long-poll wait (default 30s) |
| `--agent-credentials-secret` | Secret with `odoo-base-url`, `cluster-id`, `token` |

Disable Pull-mode by leaving `--agent-enabled=false`; in-cluster reconcilers continue.

## Testing requirements summary

| Area | Package | Type |
|------|---------|------|
| ApplicationInstance validation | `internal/controller` | unit |
| HelmRelease materialization | `internal/helmengine` | fake client |
| Agent HTTP + applier | `internal/agent` | httptest + fake client |
| Reconciler integration | `internal/controller` | envtest |

Run everything: `make test`.

## Related ADRs

- [ADR 0002 — Helm-first via Flux HelmRelease](../adr/0002-helm-first-via-flux-helmrelease.md)
- [ADR 0003 — Pull mode as default connectivity](../adr/0003-pull-mode-as-default-connectivity.md)
- [ADR 0004 — Two CRDs](../adr/0004-two-crds-applicationinstance-and-operation.md)
- [ADR 0005 — One operator per cluster](../adr/0005-one-operator-per-cluster.md)

## Phase 1c next session (suggested)

1. Harden `make deploy` on kind with published Docker Hub image.
2. Add Flux CRDs to envtest or document e2e-only Helm assertions.
3. Registration token exchange in Cluster reconciler (credential bootstrap).
4. Persist applied `idempotencyKey` set across operator restarts (ConfigMap).
