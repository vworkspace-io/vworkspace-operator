# Project layout

**Status:** Alpha — Phase 1 foundation implemented; some packages are stubs.
**Last Updated:** 2026-05-29

This is the directory layout for the operator's Go source tree. It follows Kubebuilder v4 conventions with vWorkspace-specific packages under `internal/agent/`, `internal/engines/`, and `internal/helmengine/`.

## The tree

```
.
├── api/
│   ├── apps/v1alpha1/           # ApplicationInstance CRD types
│   │   ├── applicationinstance_types.go
│   │   ├── conditions.go
│   │   ├── groupversion_info.go
│   │   └── zz_generated.deepcopy.go
│   └── ops/v1alpha1/              # Operation + Cluster CRD types
│       ├── operation_types.go
│       ├── cluster_types.go
│       ├── conditions.go
│       ├── groupversion_info.go
│       └── zz_generated.deepcopy.go
├── cmd/
│   └── main.go                    # Manager entrypoint
├── internal/
│   ├── controller/                # Reconcilers + validation helpers
│   ├── agent/                     # Pull-mode HTTP client + poller stub
│   ├── engines/                   # Operation engine registry (helm, velero)
│   ├── helmengine/                # Flux HelmRelease adapter
│   ├── conditions/                # metav1.Condition helpers
│   ├── labels/                    # Well-known label constants
│   └── webhook/                   # Admission webhook placeholder (Phase 2)
├── config/                        # Kustomize (CRD, RBAC, manager, samples)
├── hack/                          # verify-generated.sh, boilerplate header
├── test/e2e/                      # Ginkgo e2e scaffold (sparse)
├── docs/
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

## What each directory holds

### `api/apps/v1alpha1/` and `api/ops/v1alpha1/`

Go types for `ApplicationInstance` (`apps.vworkspace.io/v1alpha1`), `Operation`, and `Cluster` (`ops.vworkspace.io/v1alpha1`). Edit `*_types.go`, then run `make generate manifests`.

### `internal/controller/`

- `ApplicationInstanceReconciler` — validates spec, delegates to `helmengine.Engine`, sets conditions, finalizer on delete.
- `OperationReconciler` — validates target, concurrency guard, delegates to `engines.Registry`.
- `ClusterReconciler` — heartbeat via `agent.Client` (Pull-mode connectivity stub).
- `validation.go` — extracted validation for reuse by future admission webhooks.

### `internal/agent/`

Pull-mode client implementing `FetchJobs`, `AckJob`, `ReportStatus`, `ReportResult`, `PostEvents`, `Heartbeat`. Job application (server-side apply) is Phase 1b.

### `internal/engines/`

`Engine` interface and registry. Implemented engines: `helm` (HelmRelease upgrade), `velero` (Backup/Restore CRs).

### `internal/helmengine/`

`FluxEngine` materializes `HelmRelease`, `HelmRepository`, or `OCIRepository` and maps Flux conditions back to `ApplicationInstance` status.

### `config/`

See [config/README.md](../../config/README.md). CRD and RBAC YAML under `config/crd/` and `config/rbac/` are generated.

### `test/e2e/`

Kind-based e2e scaffold from Kubebuilder. Full e2e scenarios land in Phase 1d.

## Planned but not yet present

- `internal/helmengine/argocd/` — Argo CD adapter (Phase 3+).
- `internal/engines/workflow/`, `job/`, `snapshot/`, `volsync/` — additional operation engines.
- `internal/admission/` — validating/mutating webhooks (Phase 2).
- `internal/agent/credentials.go`, `applier.go` — credential rotation and job apply (Phase 1b).

## Related material

- [IMPLEMENTATION_GUIDE.md](IMPLEMENTATION_GUIDE.md) — phased deliverables and acceptance criteria.
- [build-and-test.md](build-and-test.md) — Makefile targets.
- [contributing.md](contributing.md) — dev workflow.
