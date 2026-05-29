# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until `v1.0.0`, breaking changes may occur on minor version bumps; CRDs follow the documented deprecation policy described in [docs/operate/upgrades.md](docs/operate/upgrades.md).

## [Unreleased]

### Added

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

