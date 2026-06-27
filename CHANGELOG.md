# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/), and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Until `v1.0.0`, breaking changes may occur on minor version bumps; CRDs follow the documented deprecation policy described in [docs/operate/upgrades.md](docs/operate/upgrades.md).

## [Unreleased]

## [0.0.10] - 2026-06-27

Phase 6 operator release — adds `ApplicationInstance.spec.mode: placeholder` for per-cluster cluster-ops sentinel instances (Option B from the hub design). **Upgrade the operator (CRDs + controller) before enabling placeholder mode from the control plane** — the server emits `spec.mode: placeholder` only after operator **v0.0.10+** is installed.

### Added

- `ApplicationInstanceSpec.mode` enum (`managed` default | `placeholder`). Placeholder instances reach `Ready=True` (`Reason=Placeholder`) without Helm; they advertise infra capability annotations (`ops.vworkspace.io/runcommand`, `runbook`, …) as the `targetRef` for cluster-scoped `Operation`s. Reconciler skips `EnsureRelease`/`DeleteRelease`; webhook rejects `chart`/`values`/`release` on placeholders and blocks managed→placeholder transitions when a Helm release may exist ([#77](https://github.com/vworkspace-io/vworkspace-operator/pull/77), hub `P6-T001`).
- ADR [0006 — ApplicationInstance placeholder mode](docs/adr/0006-applicationinstance-placeholder-mode.md).

### Upgrade note

Apply CRDs first, then roll the operator Deployment:

```bash
kubectl apply -f https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.10/crds.yaml
helm upgrade vworkspace-operator \
  https://github.com/vworkspace-io/vworkspace-operator/releases/download/v0.0.10/vworkspace-operator-0.0.10.tgz \
  --version 0.0.10 -n vworkspace-system --reuse-values --set image.tag=v0.0.10
```

Existing `ApplicationInstance` objects without `spec.mode` continue to reconcile as `managed` (CRD default).

## [0.0.9] - 2026-06-17

Bugfix release — makes cluster registration idempotent so a transient status-write conflict during a successful registration no longer pins the Cluster to `Error`. Surfaced during Rancher real-cluster validation.

### Fixed

- Cluster registration is now idempotent against existing bootstrap credentials. The reconciler treats a present, valid `vworkspace-agent-credentials` Secret as proof of registration and no longer re-exchanges the spent one-time token, and the post-registration status write retries on optimistic-concurrency conflicts. Previously a transient `the object has been modified` conflict during a successful registration left `status.phase` unset, so the next reconcile re-exchanged the consumed token and pinned the Cluster to `Error` (`RegistrationTokenInvalid`) until a new token was issued. A 401 with credentials already present is now treated as benign. Surfaced during Rancher real-cluster validation; tracked under hub golden-path task P4-T001.
- CI: suppress the `SA1019` staticcheck deprecation warning on the cluster-controller event recorder so `golangci-lint` passes on `main` ([#75](https://github.com/vworkspace-io/vworkspace-operator/pull/75)).

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
