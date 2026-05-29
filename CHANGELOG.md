# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until `v1.0.0`, breaking changes may occur on minor version bumps; CRDs follow the documented deprecation policy described in [docs/operate/upgrades.md](docs/operate/upgrades.md).

## [Unreleased]

### Added

- Pull-mode agent loop: credential loader (`internal/agent/credentials.go`), job applier with server-side apply (`internal/agent/applier.go`), long-poll poller, and event batcher wired from `cmd/main.go` (`--agent-enabled`, `--agent-poll-interval`, `--agent-credentials-secret`).
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
