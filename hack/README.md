# hack/

This directory holds developer tooling and release scripts. It is intentionally empty until the Phase 1 Go scaffold lands; see [ROADMAP.md](../ROADMAP.md).

Expected contents over time:

- `hack/boilerplate.go.txt` — license header prepended to generated Go files.
- `hack/tools/` — pinned developer tool versions (controller-gen, kustomize, golangci-lint, envtest).
- `hack/release.sh` — release helper (tag, build, sign, publish chart) referenced from [docs/development/release-process.md](../docs/development/release-process.md).
- `hack/update-codegen.sh` — wrapper around `controller-gen` and `kustomize` invocations.

This is project-local tooling, not user-facing. End users should not need to run anything here.
