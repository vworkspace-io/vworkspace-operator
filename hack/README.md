# hack/

Project-local developer tooling. End users should not need this directory.

## Files

| Path | Purpose |
|------|---------|
| `boilerplate.go.txt` | License header injected by `controller-gen` (`make generate`). |
| `verify-generated.sh` | CI helper: runs `make manifests generate` and fails if git diff is non-empty. |
| `validate-helm-kind.sh` | Installs the in-repo Helm chart on kind and waits for the operator Deployment (Phase 1f-b). |
| `dev-pull-loop.sh` | Starts mock control plane and prints operator env hints for local Pull-mode dev. |
| `dev-real-control-plane.sh` | Prints register/run commands for a live vWorkspace Server (no mock). |
| `code-review.sh` | CI: runs Cursor Agent on a PR diff and posts/updates the canonical review comment. |
| `code_review_findings.py` | CI helper: parses `## Findings` severities from a review to gate the workflow (`REVIEW_FAIL_SEVERITIES`). |
| `test-code-review-findings.sh` | CI: runs the `code_review_findings.py` unit tests (`test_code_review_findings.py`). |
| `tools/` | Reserved for pinned tool module (optional; versions are pinned in the top-level `Makefile`). |

## Common commands

```bash
./hack/verify-generated.sh   # before pushing API/RBAC changes
./hack/validate-helm-kind.sh # Helm install smoke test on kind
make controller-gen          # download controller-gen to ./bin/
make setup-envtest           # download envtest apiserver binaries
```

Release automation (`release.sh`, `update-codegen.sh`) will land in later phases; see [docs/development/release-process.md](../docs/development/release-process.md).
