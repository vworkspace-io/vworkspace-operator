# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until `v1.0.0`, breaking changes may occur on minor version bumps; CRDs follow the documented deprecation policy described in [docs/operate/upgrades.md](docs/operate/upgrades.md).

## [Unreleased]

## [0.0.7] - 2026-06-11

Phase 2 pre-release — first GitHub Release with installable Helm chart and kubectl manifests.

### Added

- Helm chart release artifacts on GitHub Releases: packaged `.tgz`, `crds.yaml`, `operator.yaml`, and `SHA256SUMS` (via `hack/package-release.sh` and CI `release` job on `v*` tags). Install without cloning this repository — see [docs/install/helm.md](docs/install/helm.md#install-from-github-release).
- Operation engines: `job`, `workflow`, and `helmHookJob` runtime materialization and status polling in the operation reconciler (Hub #9 spoke 4).
- Operation admission: typed concurrency conflict matrix (shared with reconciler) and `vws1` approval-claim HMAC verification via `--approval-claim-secret` / `VWORKSPACE_APPROVAL_CLAIM_SECRET` (aligned with server `vws_operations.approval_claim_secret`).
- Built-in `restore.velero` template marks `RequiresApproval`; reconciler blocks with `AwaitingApproval` until a valid claim is present.
- CI: Cursor Agent automated PR code review workflow (`.github/workflows/code-review.yml`, `hack/code-review.sh`).

### Changed

- CI: Cursor code review fails the workflow when **Findings** include `critical`, `major`, or `minor` (configurable via `REVIEW_FAIL_SEVERITIES`).
- CI: code review uses PR head SHA in comments, caps previous-review history, fails the job on agent errors, surfaces diff truncation, and documents untrusted-PR handling in CONTRIBUTING/SECURITY.
- CI: extracted the findings severity parser into `hack/code_review_findings.py` with unit tests (`hack/test_code_review_findings.py`, run in the verify job); the parser ignores illustrative `### [severity]` examples quoted under **Suggested fix** so it no longer fails CI on its own quoted markdown.
- CI: pin `softprops/action-gh-release` to a valid v2.2.1 commit so tagged releases publish successfully ([#71](https://github.com/vworkspace-io/vworkspace-operator/pull/71)).

## [0.0.6] - 2026-06-02

Phase 1 joint pre-release — Pull-mode agent contract aligned with [vworkspace-server v0.0.5](https://github.com/vworkspace-io/vworkspace-server/releases/tag/v0.0.5). Golden path (contract-only and full reconcile tiers) verified at this SHA; see [hub release note](https://github.com/vworkspace-io/vworkspace/blob/main/docs/releases/v0.0.x.md).

### Added

- Docs: Flux **contract-only** (CRDs via `INSTALL_FLUX_CRDS`) vs **full reconcile** (helm-controller/source-controller) for Phase 1 golden path — [cluster-bootstrap.md](docs/install/cluster-bootstrap.md#flux-contract-only-vs-full-reconcile), [helm.md](docs/install/helm.md#optional-flux-controllers-for-ready), [real-control-plane.md](docs/development/real-control-plane.md). Fixes [#23](https://github.com/vworkspace-io/vworkspace-operator/issues/23).
- Golden-path fixes: operator-owned target namespace before apply jobs; server UUID for `clusterId` in dev scripts; cluster-scoped Cluster CR in register CLI; Makefile flag forwarding; Helm CRD ownership on fresh clusters.
- Phase 3 operator-side real control plane path: `hack/dev-real-control-plane.sh`, [docs/development/real-control-plane.md](docs/development/real-control-plane.md), optional `go test -tags=integration` live server smoke test.
- Public-release documentation polish: [docs/publication.md](docs/publication.md), MkDocs sidebar navigation, refreshed README and docs index.

### Removed

- Deprecated Odoo-named compatibility aliases from pre-1.0 API cleanup: `--odoo-base-url` / `ODOO_BASE_URL`, `--odoo-endpoint`, Helm `agent.odooBaseUrl`, and credentials Secret key `odoo-base-url`.

### Changed

- `manager register` accepts `--cluster-id` / `VWORKSPACE_CLUSTER_ID` for the server-issued cluster UUID; no longer copies `--cluster-name` into `spec.clusterId`.
- **Breaking (pre-1.0):** `Cluster.spec.odooBaseUrl` renamed to `Cluster.spec.controlPlaneBaseUrl`; condition reason codes and `app.vworkspace.io/managed-by` label value updated to control-plane terminology.
- Pull-mode agent runtime, credential rotation, status reporting, admission webhooks, Helm chart, e2e/integration tests, and CI parallelization (see prior development history since v0.0.0).

## [0.0.0] - 2026-05-28

### Added

- Initial project scaffold: documentation, governance, license, ADRs, RFC process, issue and PR templates. No code yet.
