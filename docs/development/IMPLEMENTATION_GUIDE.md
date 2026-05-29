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

### Phase 1c — Install path and registration (done)

**Goal:** Documented end-to-end path on kind/k3s with cluster registration and persistent Pull-mode idempotency.

| Deliverable | Path | Status |
|-------------|------|--------|
| Cluster registration flow | `internal/controller/cluster_controller.go`, `internal/agent/register.go` | Done |
| Persistent idempotency store | `internal/agent/idempotency.go` | Done |
| Agent runtime + credential reload | `internal/agent/runtime.go`, `cmd/main.go` | Done |
| Pull-mode metrics | `internal/agent/metrics.go` | Done |
| Register CLI | `internal/cli/register.go` (`manager register`) | Done |
| Operation validating webhook (stub) | `internal/webhook/operation_webhook.go` | Done |
| Sample Cluster CR | `config/samples/ops_v1alpha1_cluster.yaml` | Done |
| Quickstart / bootstrap docs | `docs/install/quickstart.md`, `docs/install/cluster-bootstrap.md` | Done |
| RBAC review | `config/rbac/role.yaml` vs `docs/security/rbac.md` | Remaining |

**Acceptance criteria**

- [x] Cluster reconciler exchanges `spec.registrationToken` for bootstrap credential in Secret `vworkspace-agent-credentials`.
- [x] Applied Pull-mode `idempotencyKey` values persist in ConfigMap across operator restarts.
- [x] Agent poller reloads credentials from Secret after registration.
- [x] Prometheus metrics: `vworkspace_operator_pull_job_lag_seconds`, `vworkspace_operator_connectivity_state`, `vworkspace_operator_applied_jobs_total`.
- [ ] `make deploy IMG=...` installs operator + CRDs on kind (manual validation).
- [ ] Sample `ApplicationInstance` reconciles when Flux CRDs are present (envtest/e2e gap).
- [ ] Velero CRD present for backup `Operation` (documented prerequisite).

### Phase 1d — Parallel tracks (mock Odoo, Helm, webhooks)

Phase 1d splits into three **non-blocking** branches. Use **mock Odoo** until real Odoo modules exist in the parent vWorkspace repo.

#### Phase 1d-a — Mock Odoo server (`feat/mock-odoo-server`)

**Goal:** In-repo HTTP server implementing the Pull-mode agent API for dev and CI without Odoo.

| Deliverable | Path |
|-------------|------|
| Mock server library | `test/mockodoo/server.go` |
| Runnable binary | `test/mockodoo/cmd/mockodoo` (`go run ./test/mockodoo/cmd/mockodoo`) |
| Poller integration tests | `test/mockodoo/server_test.go` |
| Documentation | `docs/development/mock-odoo.md` |

**Acceptance criteria**

- [x] `POST /api/agent/register` returns bootstrap token for a configured registration token.
- [x] `GET /api/agent/jobs` long-polls and returns enqueued jobs for the authenticated cluster.
- [x] `POST .../ack`, `.../status`, `.../result`, and `POST /api/agent/events` behave per [job-protocol.md](../connectivity/job-protocol.md).
- [x] Operator `AgentPoller` + `Applier` integration test passes against mock server (httptest).
- [x] `go test ./test/mockodoo/...` and `make test` pass.

**Branch:** `feat/mock-odoo-server` (merged).

### Phase 1e — Pull-mode loop integration (done)

**Goal:** Prove the full Pull loop without real Odoo: mock enqueue → poller → applier → `ApplicationInstance` reconciler → result/ack on mock.

| Deliverable | Path | Status |
|-------------|------|--------|
| Mock test server helper | `test/mockodoo/testserver.go` | Done |
| Pull loop integration tests | `test/integration/pull_loop_test.go` | Done |
| Poller single-iteration API | `internal/agent/poller.go` (`PollOnce`) | Done |
| E2E placeholder (kind + mock deferred) | `test/e2e/pull_loop_test.go` | Skipped with reason |
| Local dev script | `hack/dev-pull-loop.sh` | Done |
| Documentation | `docs/development/mock-odoo.md`, this guide | Done |

**Acceptance criteria**

- [x] Integration test enqueues `apply` job on mock Odoo, runs `AgentPoller.PollOnce`, verifies `ApplicationInstance` CR exists.
- [x] Integration test runs `ApplicationInstanceReconciler` with `helmengine.FluxEngine` (fake client) and verifies `HelmRelease` materialized (no real Flux controller).
- [x] Mock Odoo records ack and terminal `succeeded` result for the job.
- [x] Second integration test verifies idempotent replay returns `noop` on mock Odoo.
- [x] `make test`, `make lint`, and `./hack/verify-generated.sh` pass.
- [ ] E2e on kind with in-cluster mock Odoo sidecar (Phase 1f).

**Branch:** `feat/phase-1e-e2e-pull-loop`.

#### Phase 1d-b — Helm install bundle (`feat/helm-install-bundle`)

**Goal:** Helm chart installing operator, CRDs, and RBAC (complement to kustomize).

| Deliverable | Path |
|-------------|------|
| Helm chart | `charts/vworkspace-operator/` |
| Values | agent enabled flag, Odoo URL placeholder, image `docker.io/vworkspace/vworkspace-operator` |
| Install docs | `docs/install/quickstart.md` — `helm install` section |

**Acceptance criteria**

- [x] `helm template` renders Deployment, ServiceAccount, ClusterRole(Binding), CRDs.
- [x] Values override image, agent flags, and Odoo base URL.
- [x] Chart README or quickstart documents install on kind/k3s.
- [x] `make test` unchanged (chart validation optional in CI).

**Branch:** `feat/helm-install-bundle` (merged).

#### Phase 1f-b — Helm chart kind validation (`feat/helm-kind-validate`)

**Goal:** Validate Helm install path on kind; polish chart from Phase 1d-b.

| Deliverable | Path | Status |
|-------------|------|--------|
| Chart values polish | `charts/vworkspace-operator/values.yaml` | Done |
| Post-install NOTES | `charts/vworkspace-operator/templates/NOTES.txt` | Done |
| Kind validation script | `hack/validate-helm-kind.sh` | Done |
| Helm install guide | `docs/install/helm.md` | Done |
| Quickstart Option A (tested values) | `docs/install/quickstart.md` | Done |

**Acceptance criteria**

- [x] `agent.enabled`, `agent.odooBaseUrl`, `agent.credentialsSecret`, `image.repository`, `image.tag` in values.
- [x] CRDs installed via chart template (`templates/crds.yaml`) when `crds.install=true`.
- [x] `./hack/validate-helm-kind.sh` installs chart on kind and waits for Deployment Ready.
- [x] Optional Flux CRDs via `INSTALL_FLUX_CRDS=true`.
- [x] `make test` and `make lint` pass.
- [ ] CI helm-kind job optional (commented; run manually).

**Branch:** `feat/helm-kind-validate`.

#### Phase 1d-c — Admission webhooks (`feat/admission-webhooks`)

**Goal:** Harden Operation validating webhook beyond type enum check.

| Deliverable | Path |
|-------------|------|
| Webhook validation | `internal/webhook/operation_webhook.go` |
| Shared validation | `internal/controller/operation_validation.go` |
| Webhook tests | `internal/webhook/operation_webhook_test.go` (envtest) |
| Kustomize enablement | cert-manager or dev self-signed in `config/webhook/` |

**Acceptance criteria**

- [ ] Reject unsupported `Operation` types and invalid engine/type pairs.
- [ ] Reject concurrent conflicting operations (e.g. restore during upgrade) per namespace.
- [ ] Reject inline secrets in referenced `ApplicationInstance` values where policy requires refs only.
- [ ] Webhook unit/envtest coverage for accept and reject cases.
- [ ] `--webhooks-enabled` documented with TLS prerequisites.

**Branch:** `feat/admission-webhooks` (merge after 1d-a; independent of Helm chart).

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

- `main` — merged Phase 1a–1c; container images published from CI.
- `feat/mock-odoo-server` — Phase 1d-a mock Odoo API (merged).
- `feat/helm-install-bundle` — Phase 1d-b Helm chart (merged).
- `feat/helm-kind-validate` — Phase 1f-b Helm kind validation.
- `feat/admission-webhooks` — Phase 1d-c validating webhook hardening (merged).
- `feat/phase-1e-e2e-pull-loop` — Phase 1e Pull-mode integration tests.

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
| Pull loop (mock Odoo → applier → reconciler) | `test/integration` | fake client + mock Odoo |
| Reconciler integration | `internal/controller` | envtest |

Run everything: `make test`.

## Related ADRs

- [ADR 0002 — Helm-first via Flux HelmRelease](../adr/0002-helm-first-via-flux-helmrelease.md)
- [ADR 0003 — Pull mode as default connectivity](../adr/0003-pull-mode-as-default-connectivity.md)
- [ADR 0004 — Two CRDs](../adr/0004-two-crds-applicationinstance-and-operation.md)
- [ADR 0005 — One operator per cluster](../adr/0005-one-operator-per-cluster.md)

## Phase 1f next session (suggested)

1. Deploy mock Odoo in kind (sidecar or Service) and enable skipped e2e in `test/e2e/pull_loop_test.go`.
2. Wire reconciler status/events to `ReportStatus` / `EventBatcher` (condition transitions back to Odoo).
3. RBAC review against `docs/security/rbac.md` (Phase 1c carry-over).
4. Sample `ApplicationInstance` with Flux controllers on kind (extend `hack/validate-helm-kind.sh` with `INSTALL_FLUX_CRDS=true`).
