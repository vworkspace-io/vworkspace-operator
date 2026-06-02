# Build and test

**Status:** Alpha — Makefile targets match the Phase 1 scaffold.
**Last Updated:** 2026-05-30

This document is the cookbook for building, testing, and running the operator during development. The Makefile is the canonical interface; if a command does something other than what this document says, the Makefile is right and this document is stale. Open an issue (or a PR) when that happens.

The commands below reflect the current repository. Tool versions are pinned in the `Makefile` (`controller-gen`, `kustomize`, `envtest`, `golangci-lint`).

## Make targets

| Target              | What it does                                                                                                            |
|---------------------|--------------------------------------------------------------------------------------------------------------------------|
| `make manifests`    | Regenerates CRD YAML, RBAC YAML, and webhook configurations from kubebuilder markers. Runs `controller-gen` under the hood.  |
| `make generate`     | Regenerates `zz_generated.deepcopy.go` files for the API types.                                                          |
| `make fmt`          | Runs `gofmt -w` (and `goimports -w` if available) on the tree.                                                            |
| `make vet`          | Runs `go vet ./...`.                                                                                                     |
| `make lint`         | Runs `golangci-lint run` with the configuration in `.golangci.yaml`.                                                     |
| `make test`         | Runs unit tests and envtest. Downloads envtest binaries on first run.                                                    |
| `make e2e`          | Runs end-to-end tests against a fresh kind cluster.                                                                       |
| `make docker-build` | Builds the operator container image. Accepts `IMG=<registry>/<repo>:<tag>`.                                              |
| `make docker-push`  | Pushes the image built by `docker-build`. Accepts the same `IMG=`.                                                        |
| `make install`      | Applies `config/crd` to the current kubectl context. Useful for `make run`.                                              |
| `make uninstall`    | Removes the CRDs applied by `install`.                                                                                   |
| `make deploy`       | Renders `config/default` with `kustomize`, applies it to the current context. Accepts `IMG=`.                            |
| `make undeploy`     | Reverses `deploy`.                                                                                                       |
| `make run`          | Runs the operator binary locally against the current kubectl context. Pass flags with `make run -- <flags>` or `make run ARGS="<flags>"`. |
| `make controller-gen` / `make kustomize` / `make envtest` | Downloads the pinned version of the tool into `./bin/`. Run automatically by other targets. |
| `make help`         | Lists the available targets with descriptions.                                                                            |

## A typical workflow

The shortest feedback loop for changes to a reconciler:

```
# 1. Make your code change in internal/controller/operation_controller.go
# 2. Regenerate manifests if you changed an RBAC marker
make manifests
# 3. Run the unit tests
make test
# 4. Spin up the operator locally against kind
make run
# 5. In another terminal, apply a CR and watch it reconcile
kubectl apply -f config/samples/operations/backup-velero.yaml
kubectl get operation -A -w
```

When you change the API types under `api/apps/v1alpha1/` or `api/ops/v1alpha1/`, also run `make generate` to refresh the deep-copy methods.

## Running unit tests and envtest

```
make test
```

The Makefile sets `KUBEBUILDER_ASSETS` to the downloaded envtest binary suite and runs `go test ./...` with race detection enabled. Some tests are pure Go (no envtest); some use envtest to bring up an in-process kube-apiserver. Both run as part of the same target.

To run a single package:

```
go test ./internal/engines/velero/... -v
```

To run a single test:

```
go test ./internal/engines/velero/... -v -run TestVeleroBackupMaterialize
```

To run with verbose envtest logging (useful when a test is failing because the apiserver behaves differently than you expected):

```
KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.30 -p path)" \
  go test ./internal/controllers/... -v -count=1 -run TestApplicationInstanceReconciler
```

The `-count=1` flag disables Go's test caching so a re-run actually re-runs.

## Integration test against live vWorkspace Server

Optional smoke test against a running [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) docker-compose stack. Not part of `make test` or default CI.

```bash
export CONTROL_PLANE_BASE_URL=http://127.0.0.1:8069
export VWORKSPACE_REGISTRATION_TOKEN=vwksp-reg-...   # fresh one-time token
go test -tags=integration ./test/integration/... -run TestRealControlPlane -count=1 -v
```

See [real-control-plane.md](real-control-plane.md) and `./hack/dev-real-control-plane.sh`.

## Running e2e tests against kind

```
make e2e
```

This target:

1. Creates a kind cluster named `vworkspace-e2e` (idempotent — if it exists, the target reuses it).
2. Builds the operator image as `local/vworkspace-app-operator:e2e`.
3. Loads the image into kind via `kind load docker-image`.
4. Deploys the operator via `make deploy IMG=local/vworkspace-app-operator:e2e`.
5. Runs the e2e suite under `test/e2e/`.
6. Tears down the cluster on success (set `E2E_KEEP_CLUSTER=1` to keep it for debugging).

Expected runtime: 2 to 5 minutes depending on hardware and how many tests are in the suite.

The e2e suite uses Ginkgo as the test driver and assumes the operator can complete a full `ApplicationInstance` reconcile against a tiny "no-op" chart that the suite ships. It does not pull real charts from the internet; everything the e2e needs is local.

## Building the container image

```
make docker-build IMG=ghcr.io/local/vworkspace-app-operator:dev
```

The image is built from `Dockerfile` at the repository root. A multi-stage build: Go compile in a `golang:` builder, copy the binary into a small distroless base image. The result is a single-layer image that runs as `nonroot` (UID 65532) with `readOnlyRootFilesystem: true`.

The image's labels carry the version (`org.opencontainers.image.version`), the source commit, and the build date. These are also embedded in the binary so `vworkspace-app-operator version` reports them at runtime.

## Pushing the image

```
make docker-push IMG=ghcr.io/local/vworkspace-app-operator:dev
```

The Makefile does not log in to the registry for you; run `docker login` first. For multi-arch images:

```
make docker-buildx IMG=ghcr.io/local/vworkspace-app-operator:dev PLATFORMS=linux/amd64,linux/arm64
```

`docker-buildx` requires Docker buildx; the target installs the QEMU bootstrap into a buildx builder named `vworkspace-builder` on first run.

## Deploying to a cluster

For a kind cluster (or any cluster the current `kubectl` context points at):

```
make docker-build IMG=ghcr.io/local/vworkspace-app-operator:dev
kind load docker-image ghcr.io/local/vworkspace-app-operator:dev --name vworkspace-dev
make deploy IMG=ghcr.io/local/vworkspace-app-operator:dev
```

`make deploy` runs `kustomize build config/default | kubectl apply -f -`. The default kustomization includes the manager Deployment, ServiceAccount, ClusterRole/ClusterRoleBinding, Role/RoleBinding bases, the CRDs, and the webhook configurations. It does **not** install the bundled controllers (Flux, Velero, cert-manager, external-secrets); for those, install the operator's Helm bundle instead (see [../install/quickstart.md](../install/quickstart.md)).

`make undeploy` reverses the apply.

## CI parity

The same `make` targets run in CI. To reproduce a CI failure locally:

```
make fmt vet lint
make manifests generate
./hack/verify-generated.sh
make test
make test-e2e    # requires kind; optional locally
```

Running them in order matches the CI's gating set. If `git diff --exit-code` reports a diff, you forgot to commit a generated file; commit it.

## Bumping dependencies

```
go get -u <module>@<version>
go mod tidy
make manifests generate
make test
```

The Makefile pins the versions of `controller-gen`, `kustomize`, and `envtest` in `hack/tools/`. To bump those:

```
# edit hack/tools/go.mod or the relevant version variable in the top-level Makefile
make controller-gen kustomize envtest
make manifests generate test
```

CI runs against the pinned versions; if the bump changes a generator's output, regenerate and commit.

## Troubleshooting

- **`make test` fails with `kube-apiserver not found`.** Run `make envtest` to download the binaries.
- **`make run` panics on startup with "no cert files".** The webhook expects TLS material; running outside a cluster, you need either `--enable-webhook=false` (the Makefile default for `make run`) or to provide certs via env vars.
- **`make deploy` fails with "no matches for kind HelmRelease".** You ran `make deploy` (which deploys only the operator) before installing Flux's CRDs. Either install the operator's Helm bundle (which brings Flux) or apply Flux's CRDs first.
- **`make e2e` is slow.** Most of the time is Docker pulling base images. Set `E2E_KEEP_CLUSTER=1` to keep the kind cluster between runs.

## Related material

- [contributing.md](contributing.md) — Workflow that uses these targets.
- [project-layout.md](project-layout.md) — Where the source the targets compile lives.
- [release-process.md](release-process.md) — What happens to the artifacts these targets produce.
