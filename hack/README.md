# hack/

Project-local developer tooling. End users should not need this directory.

## Files

| Path | Purpose |
|------|---------|
| `boilerplate.go.txt` | License header injected by `controller-gen` (`make generate`). |
| `verify-generated.sh` | CI helper: runs `make manifests generate` and fails if git diff is non-empty. |
| `tools/` | Reserved for pinned tool module (optional; versions are pinned in the top-level `Makefile`). |

## Common commands

```bash
./hack/verify-generated.sh   # before pushing API/RBAC changes
make controller-gen          # download controller-gen to ./bin/
make setup-envtest           # download envtest apiserver binaries
```

Release automation (`release.sh`, `update-codegen.sh`) will land in later phases; see [docs/development/release-process.md](../docs/development/release-process.md).
