# Implementation guide

**Status:** Alpha — living handoff document for Phase 1 foundation work.
**Last Updated:** 2026-06-02
**Audience:** Engineers continuing vworkspace-operator development.

This document breaks Phase 1 into continuable sub-phases, defines acceptance criteria, and explains how to resume work on any day. It complements [ROADMAP.md](https://github.com/vworkspace-io/vworkspace-operator/blob/main/ROADMAP.md) (milestones) and [project-layout.md](project-layout.md) (directory contract).

## Source of truth

| Topic | Document |
|-------|----------|
| Product design | [ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md](https://github.com/vworkspace-io/vworkspace/blob/main/docs/technical/ODOO_K8S_APPLICATION_MANAGER_OPERATOR.md) |
| ApplicationInstance API | [docs/api/application-instance.md](../api/application-instance.md) |
| Operation API | [docs/api/operation.md](../api/operation.md) |
| Conditions | [docs/api/conditions.md](../api/conditions.md) |
| Pull-mode protocol | [docs/connectivity/job-protocol.md](../connectivity/job-protocol.md) |
| ADRs | [docs/adr/README.md](../adr/README.md) |
| Flux CRDs vs controllers (golden path) | [docs/install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile](../install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile) |

## Phase 1 golden path — Flux tiers

Phase 1 exit and the kind golden path intentionally support two verification tiers:

| Tier | Install | Phase 1 exit proof |
|------|---------|-------------------|
| **Contract-only** | `INSTALL_FLUX_CRDS=true` (CRDs from helm/source-controller release manifests; no controller pods) | Pull job → `ApplicationInstance` → `HelmRelease` CR exists |
| **Full reconcile** | Operator Helm bundle or `flux install` (controllers running) | `HelmRelease` and `ApplicationInstance` reach `Ready`; chart workloads deploy |

**CRDs installed ≠ Flux controllers running.** Do not treat a server instance stuck in `deploying` as a failure on the contract-only path. Optional controller install for dev: [helm.md#optional-flux-controllers-for-ready](../install/helm.md#optional-flux-controllers-for-ready).

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
| RBAC review | `config/rbac/role.yaml` vs `docs/security/rbac.md` | Done (Phase 2) |

**Acceptance criteria**

- [x] Cluster reconciler exchanges `spec.registrationToken` for bootstrap credential in Secret `vworkspace-agent-credentials`.
- [x] Applied Pull-mode `idempotencyKey` values persist in ConfigMap across operator restarts.
- [x] Agent poller reloads credentials from Secret after registration.
- [x] Prometheus metrics: `vworkspace_operator_pull_job_lag_seconds`, `vworkspace_operator_connectivity_state`, `vworkspace_operator_applied_jobs_total`.
- [x] RBAC review against `docs/security/rbac.md` (Phase 2).
- [ ] `make deploy IMG=...` installs operator + CRDs on kind (manual validation).
- [x] Sample `ApplicationInstance` materializes `HelmRelease` when Flux CRDs are present (contract tier; e2e/integration).
- [ ] Sample `ApplicationInstance` reaches `Ready` when Flux **controllers** are running on kind (full reconcile tier).
- [ ] Velero CRD present for backup `Operation` (documented prerequisite).

### Phase 1d — Parallel tracks (mock control plane, Helm, webhooks)

Phase 1d splits into three **non-blocking** branches. Use **mock control plane** until the vWorkspace Server control plane API exist in the [vworkspace-server](https://github.com/vworkspace-io/vworkspace-server) repo.

#### Phase 1d-a — Mock control plane server (`feat/mock-control-plane-server`)

**Goal:** In-repo HTTP server implementing the Pull-mode agent API for dev and CI without vWorkspace Server.

| Deliverable | Path |
|-------------|------|
| Mock server library | `test/mockcontrolplane/server.go` |
| Runnable binary | `test/mockcontrolplane/cmd/mockcontrolplane` (`go run ./test/mockcontrolplane/cmd/mockcontrolplane`) |
| Poller integration tests | `test/mockcontrolplane/server_test.go` |
| Documentation | `docs/development/mock-control-plane.md` |

**Acceptance criteria**

- [x] `POST /api/agent/register` returns bootstrap token for a configured registration token.
- [x] `GET /api/agent/jobs` long-polls and returns enqueued jobs for the authenticated cluster.
- [x] `POST .../ack`, `.../status`, `.../result`, and `POST /api/agent/events` behave per [job-protocol.md](../connectivity/job-protocol.md).
- [x] Operator `AgentPoller` + `Applier` integration test passes against mock server (httptest).
- [x] `go test ./test/mockcontrolplane/...` and `make test` pass.

**Branch:** `feat/mock-control-plane-server` (merged).

### Phase 1e — Pull-mode loop integration (done)

**Goal:** Prove the full Pull loop without real Odoo: mock enqueue → poller → applier → `ApplicationInstance` reconciler → result/ack on mock.

| Deliverable | Path | Status |
|-------------|------|--------|
| Mock test server helper | `test/mockcontrolplane/testserver.go` | Done |
| Pull loop integration tests | `test/integration/pull_loop_test.go` | Done |
| Poller single-iteration API | `internal/agent/poller.go` (`PollOnce`) | Done |
| E2E placeholder (kind + mock deferred) | `test/e2e/pull_loop_test.go` | Done (Phase 1f-c) |
| Local dev script | `hack/dev-pull-loop.sh` | Done |
| Documentation | `docs/development/mock-control-plane.md`, this guide | Done |

**Acceptance criteria**

- [x] Integration test enqueues `apply` job on mock control plane, runs `AgentPoller.PollOnce`, verifies `ApplicationInstance` CR exists.
- [x] Integration test runs `ApplicationInstanceReconciler` with `helmengine.FluxEngine` (fake client) and verifies `HelmRelease` materialized (no real Flux controller).
- [x] Mock control plane records ack and terminal `succeeded` result for the job.
- [x] Second integration test verifies idempotent replay returns `noop` on mock control plane.
- [x] `make test`, `make lint`, and `./hack/verify-generated.sh` pass.

### Phase 1f-c — E2E Pull loop with mock control plane (done)

**Goal:** Ginkgo e2e on kind: in-cluster mock control plane, operator agent enabled, registration, job enqueue, ApplicationInstance + HelmRelease, mock result.

| Deliverable | Path | Status |
|-------------|------|--------|
| Mock control plane container image | `Dockerfile.mockcontrolplane`, `make docker-build-mockcontrolplane` | Done |
| Mock control plane admin enqueue API | `test/mockcontrolplane/admin.go` | Done |
| E2E Pull loop tests | `test/e2e/pull_loop_test.go`, `pull_loop_helpers.go` | Done |
| Flux CRD install in e2e suite | `test/e2e/e2e_suite_test.go`, `test/utils/flux.go` | Done |
| Optional Velero backup e2e | `test/e2e/pull_loop_test.go` (skips without CRD) | Done |
| Documentation | `docs/development/mock-control-plane.md`, this guide | Done |

**Acceptance criteria**

- [x] Mock control plane runs in-cluster (Deployment + Service); operator reaches it via cluster DNS.
- [x] Operator deployed with `--agent-enabled=true` and pre-seeded credentials Secret.
- [x] Cluster CR registration exchanges token and persists credentials.
- [x] Admin API enqueues `apply` job; operator poller applies `ApplicationInstance`; reconciler materializes `HelmRelease` when Flux CRDs installed (controllers optional for `Ready`).
- [x] Mock control plane records terminal `succeeded` result for the job.
- [x] Optional backup operation e2e creates Velero `Backup` CR when Velero CRD installed (`E2E_INSTALL_VELERO=true`).
- [x] `make test-e2e` passes on kind with docker.

**Branch:** `feat/e2e-mock-control-plane`.

#### Phase 1d-b — Helm install bundle (`feat/helm-install-bundle`)

**Goal:** Helm chart installing operator, CRDs, and RBAC (complement to kustomize).

| Deliverable | Path |
|-------------|------|
| Helm chart | `charts/vworkspace-operator/` |
| Values | agent enabled flag, control plane URL placeholder, image `docker.io/vworkspace/vworkspace-operator` |
| Install docs | `docs/install/quickstart.md` — `helm install` section |

**Acceptance criteria**

- [x] `helm template` renders Deployment, ServiceAccount, ClusterRole(Binding), CRDs.
- [x] Values override image, agent flags, and control plane base URL.
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

- [x] `agent.enabled`, `agent.controlPlaneBaseUrl`, `agent.credentialsSecret`, `image.repository`, `image.tag` in values.
- [x] CRDs installed via chart template (`templates/crds.yaml`) when `crds.install=true`.
- [x] `./hack/validate-helm-kind.sh` installs chart on kind and waits for Deployment Ready.
- [x] Optional Flux CRDs via `INSTALL_FLUX_CRDS=true` (contract tier — CRDs only; see [cluster-bootstrap.md](../install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile)).
- [x] `make test` and `make lint` pass.
- [ ] CI helm-kind job optional (commented; run manually).

**Branch:** `feat/helm-kind-validate`.

#### Phase 1f-a — Admission webhook hardening (`feat/webhook-hardening`)

**Goal:** Harden validating webhooks beyond the Phase 1d-c scaffold: namespace allow-lists, target existence, concurrency, and inline-secret rejection.

| Deliverable | Path | Status |
|-------------|------|--------|
| Shared validation | `internal/webhook/validation.go` | Done |
| Operation webhook | `internal/webhook/operation_webhook.go` | Done |
| ApplicationInstance webhook | `internal/webhook/applicationinstance_webhook.go` | Done |
| Unit tests | `internal/webhook/operation_webhook_test.go` | Done |
| Envtest suite | `internal/webhook/webhook_envtest_test.go` | Done |
| Kustomize webhook bundle | `config/webhook/`, `config/default/manager_webhook_patch.yaml` | Done |
| Helm webhooks | `charts/vworkspace-operator/templates/webhook.yaml`, `values.yaml` | Done |

**Acceptance criteria**

- [x] Reject unknown `Operation` types and types not listed in `ops.vworkspace.io/allowed-types` on the namespace.
- [x] Reject `Operation` when target `ApplicationInstance` does not exist.
- [x] Reject concurrent conflicting operations (e.g. second `Upgrade` while one is running).
- [x] Reject inline secret-like values in `ApplicationInstance.spec.values.inline` (password/secret/token keys).
- [x] Webhook unit and envtest coverage for accept and reject cases.
- [x] `--webhooks-enabled` and Helm `webhooks.enabled` documented with TLS prerequisites.

**Branch:** `feat/webhook-hardening`.

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
- `feat/mock-control-plane-server` — Phase 1d-a mock control plane API (merged).
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

- Pre-release tags: `v0.0.x` aligned with [ROADMAP.md](https://github.com/vworkspace-io/vworkspace-operator/blob/main/ROADMAP.md).
- Container image tag matches git tag on release.

### Feature flags

| Flag / env | Purpose |
|------------|---------|
| `--control-plane-base-url` / `CONTROL_PLANE_BASE_URL` | Control plane host for Pull-mode |
| `--agent-token` / `VWORKSPACE_AGENT_TOKEN` | Bearer token |
| `--cluster-id` / `VWORKSPACE_CLUSTER_ID` | Cluster identity |
| `--agent-enabled` | Start long-poll job loop |
| `--agent-poll-interval` | Long-poll wait (default 30s) |
| `--agent-credentials-secret` | Secret with `control-plane-base-url`, `cluster-id`, `token` |

Disable Pull-mode by leaving `--agent-enabled=false`; in-cluster reconcilers continue.

## Testing requirements summary

| Area | Package | Type |
|------|---------|------|
| ApplicationInstance validation | `internal/controller` | unit |
| HelmRelease materialization | `internal/helmengine` | fake client |
| Agent HTTP + applier | `internal/agent` | httptest + fake client |
| Pull loop (mock control plane → applier → reconciler) | `test/integration` | fake client + mock control plane |
| Reconciler status events to mock control plane | `test/integration/status_report_test.go` | fake client + mock control plane |
| Pull loop e2e (kind + in-cluster mock control plane) | `test/e2e` | kind + ginkgo |
| Reconciler integration | `internal/controller` | envtest |

Run everything: `make test`.

## Related ADRs

- [ADR 0002 — Helm-first via Flux HelmRelease](../adr/0002-helm-first-via-flux-helmrelease.md)
- [ADR 0003 — Pull mode as default connectivity](../adr/0003-pull-mode-as-default-connectivity.md)
- [ADR 0004 — Two CRDs](../adr/0004-two-crds-applicationinstance-and-operation.md)
- [ADR 0005 — One operator per cluster](../adr/0005-one-operator-per-cluster.md)

#### Phase 1f-a — Admission webhook hardening (`feat/webhook-hardening`)

**Goal:** Harden validating admission webhooks for `Operation` and `ApplicationInstance` beyond the Phase 1d-c scaffold.

| Deliverable | Path |
|-------------|------|
| Shared validation helpers | `internal/webhook/validation.go` |
| Operation webhook | `internal/webhook/operation_webhook.go` |
| ApplicationInstance webhook | `internal/webhook/applicationinstance_webhook.go` |
| Unit tests | `internal/webhook/operation_webhook_test.go` |
| Envtest integration | `internal/webhook/webhook_envtest_test.go` |
| Kustomize webhook bundle | `config/webhook/` |
| Helm webhook templates | `charts/vworkspace-operator/templates/webhook.yaml` |

**Acceptance criteria**

- [x] Reject unknown or namespace-disallowed `Operation` types (`ops.vworkspace.io/allowed-types` namespace annotation).
- [x] Reject concurrent `Operation` requests when the target `ApplicationInstance` already has a Running/Accepted operation.
- [x] Reject `Operation` requests whose target `ApplicationInstance` does not exist.
- [x] Reject inline secret-like values in `ApplicationInstance.spec.values.inline`; prefer `secretRef` / `configMapRef`.
- [x] Envtest coverage: allowed type passes, disallowed type rejected, concurrent rejected, inline secret rejected.
- [x] `--webhooks-enabled` registers both webhooks; Helm/kustomize manifests document TLS prerequisites.

**Branch:** `feat/webhook-hardening`.

## Phase 2 — Status reporting, credential rotation, RBAC (in progress)

**Goal:** Report reconciler condition transitions to the control plane via Pull-mode outbound events; support credential rotation; align RBAC with least-privilege docs.

**Baseline:** v0.0.4 (all Phase 1 PRs merged).

| Deliverable | Path | Status |
|-------------|------|--------|
| Status reporter | `internal/agent/reporter.go` | Done |
| Event batcher flush + requeue | `internal/agent/events.go` | Done |
| Reconciler wiring | `internal/controller/*_controller.go` | Done |
| Credential rotation client | `internal/agent/client.go` (`RotateCredentials`) | Done |
| Cluster rotation flow | `internal/controller/cluster_controller.go`, `spec.rotateCredentials` | Done |
| Mock control plane events + rotate | `test/mockcontrolplane/server.go` | Done |
| Integration test | `test/integration/status_report_test.go` | Done |
| RBAC alignment | `config/rbac/role.yaml`, `charts/.../rbac.yaml` | Done |
| Event buffer metric | `internal/agent/metrics.go` | Done |
| Documentation | this guide, pull-mode, mock-control-plane, observability, CHANGELOG | Done |

**Acceptance criteria**

- [x] ApplicationInstance, Operation, and Cluster condition transitions enqueue batched `POST /api/agent/events`.
- [x] Events carry stable `eventKey` for control-plane-side deduplication (documented in mock-control-plane).
- [x] EventBatcher requeues on control plane unreachable; sets connectivity gauge to reconnecting.
- [x] `POST /api/agent/credentials/rotate` implemented in client and mock control plane; Cluster reconciler updates Secret.
- [x] RBAC includes ConfigMap/Secret for idempotency and credentials, events create/patch, leases.
- [x] `make test`, `make lint`, and `./hack/verify-generated.sh` pass.

**Branch:** `feat/phase-2-status-and-polish`.

## Phase 2b — Deferred polish (done)

**Goal:** Close Phase 2 deferred items: buffer overflow visibility, credential age metric, Helm CRD sync, and e2e status-event coverage.

| Deliverable | Path | Status |
|-------------|------|--------|
| BufferOverflow Cluster condition | `internal/agent/events.go`, `internal/controller/cluster_controller.go` | Done |
| Credential age metric | `internal/agent/metrics.go`, `internal/agent/credentials_store.go` | Done |
| Helm Cluster CRD sync (`rotateCredentials`) | `charts/vworkspace-operator/files/crds/ops.vworkspace.io_clusters.yaml` | Done |
| E2E status events on mock control plane | `test/e2e/pull_loop_test.go`, `test/mockcontrolplane/admin.go` | Done |
| Unit tests | `internal/agent/events_test.go`, `internal/agent/metrics_test.go` | Done |
| Documentation | this guide, CHANGELOG, observability, conditions | Done |

**Acceptance criteria**

- [x] Event buffer overflow sets `Cluster.status.conditions[BufferOverflow=True, reason=EventBufferFull]` with drop count; clears on successful drain.
- [x] `vworkspace_operator_credential_age_seconds` gauge updates on credential load, persist, and rotation.
- [x] Helm chart Cluster CRD includes `spec.rotateCredentials`; `helm template` renders.
- [x] E2e verifies `ConditionTransition` events reach mock control plane after ApplicationInstance reconcile.
- [x] `make test`, `make lint`, and `./hack/verify-generated.sh` pass.

**Branch:** `feat/phase-2b-deferred`.

## Phase 3 — vWorkspace Server integration and public release (in progress)

**Goal:** Align operator releases with [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) (the control plane product, built on Odoo 19) and ship the first public operator release.

**Depends on:** Server branch `fix/align-with-operator-pr16` merged (agent API aligned with operator PR #16).

| Deliverable | Path / repo | Status |
|-------------|-------------|--------|
| Control-plane terminology in docs and flags | `docs/`, `cmd/main.go`, Helm chart | Done (pre-release polish) |
| Real control plane dev script | `hack/dev-real-control-plane.sh` (UUID `clusterId`, slug vs CR name) | Done |
| Live server integration test (`-tags=integration`) | `test/integration/real_control_plane_test.go` | Done (manual / optional CI) |
| Real control plane dev docs | `docs/development/real-control-plane.md` | Done |
| Quickstart + pull-mode server registration docs | `docs/install/quickstart.md`, `docs/connectivity/pull-mode.md` | Done |
| Golden path: server job → ApplicationInstance + HelmRelease (contract tier) | kind + server docker-compose | In progress (Platform coordination) |
| Golden path: optional Flux controllers → `Ready` | `flux install` or bundled chart | Documented; optional in dev |
| Real Pull-mode job enqueue from server UI | upstream `vworkspace-server` | Planned (server-owned) |
| Argo Workflows / CSI / VolSync engines | `internal/engines/` | Planned |
| mTLS and signed Pull-mode payloads | `internal/agent/` | Planned |
| GitHub Pages doc publish | `docs/publication.md`, CI workflow | In progress (see publication.md checklist) |
| Public `v0.2` release | tags, signed images | Planned |

**Acceptance criteria**

- [x] Operator docs and CLI use "control plane" / vWorkspace Server naming; Odoo-named compatibility aliases removed pre-1.0.
- [x] Documented path from vWorkspace Server registration to operator agent flags without mock control plane.
- [x] Dev scripts and `manager register` use server-issued UUID for `spec.clusterId` (not registry slug); see [real-control-plane.md](real-control-plane.md) and [vworkspace-server#9](https://github.com/vworkspace-io/vworkspace-server/issues/9).
- [x] Golden path documents Flux **contract-only** (CRDs) vs **full reconcile** (controllers); no implied `Ready` without controllers — [cluster-bootstrap.md](../install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile).
- [ ] End-to-end install: vWorkspace Server registers a cluster; operator deploys an app via Pull mode without the in-repo mock.
- [ ] Published doc site on GitHub Pages.

See [ROADMAP.md](https://github.com/vworkspace-io/vworkspace-operator/blob/main/ROADMAP.md) Phase 3 for milestone dates.

## Phase 1f next session (suggested)

1. Wire reconciler status/events to `ReportStatus` / `EventBatcher` (condition transitions back to Odoo). **Done in Phase 2.**
2. RBAC review against `docs/security/rbac.md` (Phase 1c carry-over). **Done in Phase 2.**
3. Enable Velero backup e2e in CI (`E2E_INSTALL_VELERO=true`) once Velero CRD install is stable on runners.
4. Sample `ApplicationInstance` reaches `Ready` on kind with Flux controllers (`flux install` or bundled chart; `INSTALL_FLUX_CRDS=true` alone is contract-only).
