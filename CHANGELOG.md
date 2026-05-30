# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until `v1.0.0`, breaking changes may occur on minor version bumps; CRDs follow the documented deprecation policy described in [docs/operate/upgrades.md](docs/operate/upgrades.md).

## [Unreleased]

### Added

- Phase 2b polish: `Cluster.status.conditions[BufferOverflow]` when the outbound event buffer drops events (`EventBufferFull` / `BufferDrained`); clears after successful drain.
- Prometheus gauge `vworkspace_operator_credential_age_seconds` (seconds since bootstrap credentials Secret was last updated or rotated).
- Mock Odoo admin client `ListEvents` helper; e2e asserts ApplicationInstance condition transitions reach mock Odoo via `GET /api/admin/events`.
- Unit tests for event buffer overflow state and credential age metric.
- Phase 2 status reporting: `StatusReporter` and enhanced `EventBatcher` queue condition transitions from ApplicationInstance, Operation, and Cluster reconcilers to `POST /api/agent/events` with stable `eventKey` deduplication.
- Credential rotation: `POST /api/agent/credentials/rotate` client, `Cluster.spec.rotateCredentials`, Cluster reconciler Secret update flow.
- Mock Odoo: `POST /api/agent/credentials/rotate`, `GET /api/admin/events`, event deduplication by `eventKey`, `EventsFiltered` test helper.
- Prometheus metric `vworkspace_operator_event_buffer_occupancy`.
- Integration test `test/integration/status_report_test.go` (reconciler condition change → mock Odoo event).
- Unit tests for reporter, event batcher requeue, and rotation client.

### Changed

- RBAC: added `events` create/patch, `leases` for leader election, `ocirepositories` in Helm chart; aligned kustomize and chart with least-privilege operator needs (ConfigMap idempotency, credentials Secret).
- Pull-mode agent runtime shares a single `EventBatcher` with reconciler status reporting when `--agent-enabled=true`.
- Phase 1f-a admission webhook hardening: `Operation` validation (namespace allow-list via `ops.vworkspace.io/allowed-types`, target existence, concurrency) and `ApplicationInstance` inline-secret rejection (`internal/webhook/`).
- ApplicationInstance validating webhook (`internal/webhook/applicationinstance_webhook.go`).
- Envtest webhook suite (`internal/webhook/webhook_envtest_test.go`) and expanded unit tests with fake client coverage.
- Kustomize webhook bundle (`config/webhook/`, `config/default/manager_webhook_patch.yaml`) and Helm `webhooks.enabled` templates (`charts/vworkspace-operator/templates/webhook.yaml`).
- Phase 1f-b Helm kind validation: `hack/validate-helm-kind.sh`, [docs/install/helm.md](docs/install/helm.md), chart `NOTES.txt`, values polish (`agent.odooBaseUrl`, `image.repository` default `vworkspace/vworkspace-operator`).
- Phase 1e Pull-mode integration tests (`test/integration/pull_loop_test.go`): mock Odoo enqueue, poller `PollOnce`, applier SSA, `ApplicationInstance` reconciler with Flux engine on fake client, idempotent replay `noop`.
- Mock Odoo test helper `test/mockodoo/testserver.go` (`NewTestServer`, job result/status inspection).
- `hack/dev-pull-loop.sh` for local mock Odoo + operator agent workflow.
- E2E Pull-mode loop on kind with in-cluster mock Odoo (`test/e2e/pull_loop_test.go`): deploy mock Service, operator agent, Cluster registration, job enqueue via admin API, `ApplicationInstance` + `HelmRelease`, mock `succeeded` result.
- Mock Odoo admin enqueue API (`test/mockodoo/admin.go`) and container image (`Dockerfile.mockodoo`, `make docker-build-mockodoo`).
- Flux HelmRelease CRD install in e2e `BeforeSuite` (`test/utils/flux.go`); optional Velero Backup CRD when `E2E_INSTALL_VELERO=true`.
- Operation validating webhook tests (`internal/webhook/operation_webhook_test.go`, `webhook_envtest_test.go`).
- Helm chart scaffold at `charts/vworkspace-operator/` (Deployment, RBAC, CRDs, agent values).
- Quickstart Option A documents in-repo `helm install` path.
- In-repo mock Odoo Pull-mode agent API (`test/mockodoo/`) for development without real Odoo modules.
- [docs/development/local-setup.md](docs/development/local-setup.md) — Go install and local `make test` workflow.
- [docs/development/mock-odoo.md](docs/development/mock-odoo.md) — mock server usage and endpoints.

### Added (Phase 1c)

- Cluster registration flow: `spec.registrationToken` on `Cluster`, `POST /api/agent/register` client, credential persistence to `Secret/vworkspace-agent-credentials`, and `manager register` CLI subcommand.
- Persistent Pull-mode idempotency via ConfigMap `vworkspace-applied-jobs` (`internal/agent/idempotency.go`).
- Agent runtime with credential reload from Secret after registration (`internal/agent/runtime.go`).
- Pull-mode Prometheus metrics: `vworkspace_operator_pull_job_lag_seconds`, `vworkspace_operator_connectivity_state`, `vworkspace_operator_applied_jobs_total`.
- Operation validating admission webhook scaffold (`internal/webhook/operation_webhook.go`, `--webhooks-enabled`).
- Sample Cluster CR (`config/samples/ops_v1alpha1_cluster.yaml`).

### Changed

- Cluster reconciler performs registration token exchange before connectivity heartbeats; clears one-time token from spec after success.
- Pull-mode applier uses ConfigMap-backed idempotency store instead of in-memory map.
- Helm engine support for `values.secretRef` and `values.configMapRef` when materializing Flux `HelmRelease` objects.
- Docker Hub image publishing on `main` and `v*` tags (`docker.io/vworkspace/vworkspace-operator`).
- Parallel self-hosted CI jobs with per-job checkout paths; [docs/development/self-hosted-runner.md](docs/development/self-hosted-runner.md).
- [docs/install/container-images.md](docs/install/container-images.md) — image tags and registry secrets.

### Changed

- Default container image reference in kustomize manager manifest: `docker.io/vworkspace/vworkspace-operator`.
- CI workflow runs `verify`, `test`, `lint`, and `e2e` in parallel (no serial `needs` chain).

### Added (Phase 1a)

- Kubebuilder v4 Go scaffold: `ApplicationInstance`, `Operation`, and `Cluster` CRDs at `v1alpha1`.
- Flux `HelmRelease` engine adapter (`internal/helmengine`), operation engines (`helm`, `velero`), and Pull-mode HTTP agent client stub.
- Reconcilers for ApplicationInstance, Operation, and Cluster with condition helpers and basic concurrency guards.
- Unit and envtest coverage for validation, Helm materialization, Velero CR creation, and agent protocol parsing.
- Makefile, multi-stage Dockerfile, CI workflow (`.github/workflows/ci.yml`), and `hack/verify-generated.sh`.
- [docs/development/IMPLEMENTATION_GUIDE.md](docs/development/IMPLEMENTATION_GUIDE.md) — Phase 1 handoff document.

### Changed

- Project status from documentation-only to alpha scaffold with working `make test` / `make build`.
- Updated development, config, and hack README files to reflect the live layout.

## [0.0.0] - 2026-05-28

### Added

- Initial project scaffold: documentation, governance, license, ADRs, RFC process, issue and PR templates. No code yet.

