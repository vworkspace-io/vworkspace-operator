# Coding style

**Status:** Alpha
**Last Updated:** 2026-05-28

This document captures the project's Go coding-style conventions. The principle: prefer the language's defaults and the controller-runtime idioms; reach for a project-specific convention only where there is a concrete reason. None of this is meant to be a substitute for taste; it is a record of the recurring decisions we have already made so they do not get re-argued.

## Tooling

| Tool             | What it does                                                                                                         |
|------------------|------------------------------------------------------------------------------------------------------------------------|
| `gofmt`           | The canonical formatter. CI fails if `make fmt` would produce a diff. `goimports` ordering applies (stdlib, then a blank line, then third-party, then a blank line, then this project's modules). |
| `goimports`       | Run as part of `make fmt`. Required for the import-grouping convention above.                                        |
| `go vet`          | Run as part of `make vet`. CI gates merges on it.                                                                    |
| `golangci-lint`   | Configured in `.golangci.yaml`. Linters enabled: `govet`, `staticcheck`, `revive`, `errcheck`, `ineffassign`, `unparam`, `gosec`, `gocritic`, `misspell`, `gofumpt`, `unused`. CI gates merges on `make lint`. |
| `controller-gen`  | Generates DeepCopy methods, CRD YAML, and RBAC YAML from kubebuilder markers. Pinned in `hack/tools/`.                  |
| `kustomize`       | Composes the manifests under `config/`.                                                                              |
| `envtest`         | Drives reconciler tests against an in-process kube-apiserver. Pinned in `hack/tools/`.                                |

Pinning tool versions is part of the contract: a tool bump is a code change, not an environmental drift. Bumps go through PRs and CI.

## Naming

### Reconcilers

Each reconciler is `<Resource>Reconciler` in a file named `<resource>_controller.go` under `controllers/`:

```go
type ApplicationInstanceReconciler struct {
    client.Client
    Scheme *runtime.Scheme
    Recorder record.EventRecorder
    HelmEngine helmengine.Engine
    EngineRegistry engines.Registry
}

func (r *ApplicationInstanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) { ... }
func (r *ApplicationInstanceReconciler) SetupWithManager(mgr ctrl.Manager) error { ... }
```

### Conditions and reasons

Condition types are `PascalCase` (`Ready`, `Reconciling`, `Degraded`, `Suspended`, `Blocked`, `Accepted`, `Running`, `Succeeded`, `Failed`, `Cancelled`, `Connected`, `Disconnected`, `Authenticated`, `ControllersHealthy`, `CRDsRegistered`).

Condition reasons are also `PascalCase`. The vocabulary lives in `internal/conditions/` and in [../api/conditions.md](../api/conditions.md); reasons tend to be specific and verb-like (`VeleroBackupSucceeded`, `WorkflowFailed`, `HookNotFound`, `CapabilityMissing`, `TargetNotReady`). When mirroring an upstream controller's reason, copy it verbatim (preserve `VeleroBackupPartiallyFailed` rather than translating to a project-local term).

### Constants and well-known strings

Well-known labels and annotations live in `internal/labels/` as `const`s:

```go
const (
    LabelManagedBy    = "app.vworkspace.io/managed-by"
    LabelClusterID    = "app.vworkspace.io/cluster-id"
    AnnotationBackup  = "ops.vworkspace.io/backup"
    AnnotationRestore = "ops.vworkspace.io/restore"
    AnnotationOperation = "ops.vworkspace.io/operation"
)
```

Do not duplicate the strings inline; the lint passes flag literal occurrences of these strings outside the `labels` package.

### Errors

Errors get descriptive prefixes:

```go
return fmt.Errorf("velero backup %q: status read: %w", backup.Name, err)
```

The format `"<context>: <action>: %w"` reads top-down. Wrap with `%w`; never log-and-return-and-let-the-caller-log-again.

Define sentinel errors for cases callers must branch on:

```go
var ErrHookNotFound = errors.New("helm hook not found in release")
```

## Logging

The operator uses the controller-runtime logger (zap under the hood). Conventions:

- **Always carry context.** Use the request-scoped logger from `log.FromContext(ctx)`.
- **Structured fields.** Every line carries `cluster_id`, `org_id`, `namespace`, `application_instance`, `operation_id`, and `operator_version` where applicable. The fields are described in [../operate/observability.md](../operate/observability.md).
- **No `fmt.Sprintf` in messages.** Use structured fields instead:

  ```go
  // bad
  log.Info(fmt.Sprintf("reconcile failed: %v", err))

  // good
  log.Error(err, "reconcile failed",
      "applicationInstance", ai.Name,
      "namespace", ai.Namespace)
  ```

- **No secrets, ever.** The logger redacts credential-shaped fields; do not rely on the redaction as a license to log raw values that might contain secrets. The discipline is "do not pass it to the logger in the first place".
- **Levels.**
  - `Info` for routine state changes (condition transitions, applied resources, accepted operations).
  - `V(1).Info` (`debug`) for per-reconcile traces.
  - `Error` for non-retryable failures. `controller-runtime` already logs retryable reconcile errors; do not double-log.

## Reflection

Avoid reflection where avoidable. The places it is acceptable:

- The DeepCopy machinery generated by `controller-gen` (out of our hands).
- The `meta.SetStatusCondition` helper from `apimachinery` (a small, well-understood reflection use).
- The conversion webhook scaffold generated by Kubebuilder.

Places to **not** reach for reflection:

- Generic resource-walking. If you need to handle multiple kinds, prefer an explicit type switch or an `Engine` interface. The engine pattern under `internal/engines/` is the right shape; it avoids a reflection-heavy "any CR I might see" path.
- Validating fields. The admission webhook does validation against the CRD's structural schema; no reflection in webhook code.
- Translating between CRDs. The conversion webhook is where translations live, and it uses the apimachinery-generated paths.

If you find yourself reaching for `reflect`, stop and consider whether an interface, a generic (Go's type parameters), or a code generator is a better shape.

## Tests

### Layout

Tests live next to the code they test, with the `_test.go` suffix. Each package has at most one test file; if it grows past ~500 lines, split into `package_unit_test.go` and `package_integration_test.go`.

End-to-end tests live under `test/e2e/`, driven by Ginkgo. Unit and envtest tests use the standard `testing` package; we do not use Ginkgo outside e2e.

### Conventions

- **Use `t.Helper()`** in helper functions that report errors.
- **Use `require` for setup**, `assert` for assertions. The library is `testify`.
- **Subtests with `t.Run`** for table-driven tests. Each row of the table has a descriptive name.
- **Use envtest** when the reconciler's behavior depends on the apiserver (finalizers, status subresource, server-side apply, watches). Use plain Go tests otherwise.
- **Condition assertions** use the helper in `internal/conditions/`:

  ```go
  conditions.AssertCondition(t, op.Status.Conditions, "Succeeded", metav1.ConditionTrue, "VeleroBackupCompleted")
  ```

  This asserts both `status` and `reason`. Asserting `lastTransitionTime` is rare and only done when the test is specifically verifying that a condition did or did not transition.

### Anti-patterns

- **Do not pad tests with happy-path assertions for unrelated fields.** A test for "the engine materialized a Backup" should assert the Backup exists and its key fields, not every label on it.
- **Do not test the apiserver.** If a test would pass against a `Fake` client but fails against envtest, the test is wrong; switch to envtest.
- **Do not sleep.** Use `Eventually` (testify) or the controller-runtime test framework's polling helpers; never `time.Sleep`.

## Conventional commits

Commit messages follow the Conventional Commits format:

```
<type>(<scope>): <subject>

<body>

<footer>
```

Where `<type>` is one of `feat`, `fix`, `refactor`, `docs`, `test`, `build`, `ci`, `chore`, `perf`, `style`. `<scope>` is one of the directory names under the project root or a short scope keyword (`api`, `controllers`, `agent`, `engines/velero`, `engines/workflow`, `docs/operate`, `webhook`, `bundle`, ...).

Examples:

```
feat(engines/velero): map PartiallyFailed phase to Degraded condition

fix(agent): close the long-poll body even when no jobs are returned

docs(operate): add a backup-and-restore runbook
```

Breaking changes use `feat!:` (or `fix!:`) plus a `BREAKING CHANGE:` footer:

```
feat(api)!: rename Operation.spec.parameters.ttl to ttlHours

BREAKING CHANGE: existing Operation resources written with `ttl` must be
re-applied with `ttlHours`. The conversion webhook handles existing CRs at
read time but not at admission.
```

The Conventional Commit shape feeds the release-notes pipeline (whether automated or hand-curated) and makes the history greppable.

## DCO sign-off

Every commit must carry a Developer Certificate of Origin sign-off:

```
Signed-off-by: Your Name <your.email@example.com>
```

Use `git commit -s` to add it automatically. CI rejects PRs whose commits are missing a sign-off.

## Pull-request hygiene

- One topic per PR. If the change touches both a reconciler and the docs and the webhook, the PR description must call out each surface.
- Manifests and generated code are committed alongside the source they generate from. CI fails the PR if they drift.
- Tests are committed alongside the feature. A PR adding a new engine without tests will not pass review.
- Reviewers expect the PR description to answer "what changed" (in the code), "why" (the user-visible reason), and "how to verify" (the test or manual procedure that confirms the change).

## Related material

- [contributing.md](contributing.md) — Dev workflow that exercises these conventions.
- [project-layout.md](project-layout.md) — Where each kind of file lives.
- [release-process.md](release-process.md) — How Conventional Commits flow into release notes.
