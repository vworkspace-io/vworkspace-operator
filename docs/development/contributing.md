# Dev workflow

**Status:** Alpha
**Last Updated:** 2026-05-30

This document describes the dev workflow for working on `vworkspace-operator` itself. The root `CONTRIBUTING.md` covers the process (filing issues, DCO sign-off, PR review); this expands on what to run locally between writing code and pushing a PR.

The expected setup is a Linux or macOS workstation with Go, Docker (or Podman), and `kubectl`. Windows under WSL 2 also works.

## Prerequisites

| Tool                | Version                           | How to install                                                                                  |
|---------------------|-----------------------------------|-------------------------------------------------------------------------------------------------|
| Go                  | The version named in `go.mod`     | `https://go.dev/dl/`                                                                            |
| `kubectl`           | 1.28 or newer                     | `https://kubernetes.io/docs/tasks/tools/`                                                       |
| `kind`              | 0.20 or newer                     | `go install sigs.k8s.io/kind@latest`                                                            |
| `helm`              | 3.13 or newer                     | `https://helm.sh/docs/intro/install/`                                                            |
| `controller-gen`    | matches the Kubebuilder version   | Installed via `make controller-gen` (the Makefile pins the version).                            |
| `kustomize`         | 5.x                               | Installed via `make kustomize`.                                                                  |
| `golangci-lint`     | The version named in `.golangci.yaml` | `https://golangci-lint.run/usage/install/`                                                  |
| Docker or Podman    | recent                            | For building images and for kind's container runtime.                                            |

The project does not require global installs of `controller-gen`, `kustomize`, or `envtest`; the Makefile downloads them into `./bin/` on demand and pins their versions.

## A local kind cluster

The development cluster of choice is `kind`. One node, one operator, fast feedback.

```
kind create cluster --name vworkspace-dev --image kindest/node:v1.30.0
kubectl cluster-info --context kind-vworkspace-dev
```

Confirm `kubectl get nodes` works. The cluster is empty; the next steps install what you need.

## Install the CRDs

Before running the operator (either in-cluster or locally), install its CRDs:

```
make manifests
make install        # applies config/crd to the current context
```

`make manifests` regenerates the CRD YAML from the Go types under `api/v1alpha1/` (planned layout; see [project-layout.md](project-layout.md)). `make install` applies the resulting CRDs to the cluster `kubectl` is pointed at.

Verify:

```
kubectl get crd | grep vworkspace.io
# applicationinstances.apps.vworkspace.io
# operations.ops.vworkspace.io
# clusters.ops.vworkspace.io
```

## Running the operator locally against kind

For tight feedback during development, run the operator binary on your workstation against the kind cluster:

```
make run
```

The Makefile sets `KUBECONFIG` to the local context, sets `--leader-elect=false`, and runs the operator with debug logging. The webhook is disabled by default in `make run`; CRD admission relies on the API server's structural schema validation, which is enough for most development.

For changes that touch the webhook, deploy the operator in-cluster so the webhook's TLS certificates are valid:

```
make docker-build IMG=ghcr.io/local/vworkspace-app-operator:dev
kind load docker-image ghcr.io/local/vworkspace-app-operator:dev --name vworkspace-dev
make deploy IMG=ghcr.io/local/vworkspace-app-operator:dev
```

Undeploy when you're done:

```
make undeploy
```

## Running tests

Unit tests live next to the code they test, with `_test.go` suffix:

```
make test         # unit tests + envtest
```

The Makefile's `test` target invokes `go test ./...` with the `envtest` binary suite already on PATH (downloaded by the target on first run). Tests that need a real API server use `envtest`'s in-process kube-apiserver; tests that only need pure Go run without it.

E2E tests run against a kind cluster:

```
make e2e
```

The `e2e` target spins up a fresh kind cluster, deploys the operator from the local Docker image, applies a small set of CR fixtures, and asserts on `ApplicationInstance.status` and `Operation.status`. It is slower than unit tests; expect 2–5 minutes per run.

## Generating CRDs and RBAC

Changes to the Go types under `api/v1alpha1/` require regenerating the manifests:

```
make generate    # regenerates DeepCopy methods (controller-gen object)
make manifests   # regenerates CRD YAML and RBAC YAML
```

These are also run by `make test`, but it is faster to run them separately during iterative editing. CI fails if the committed manifests do not match what `make manifests` would produce, so always commit the regenerated files alongside the Go changes.

The RBAC YAML under `config/rbac/` is generated from kubebuilder-style `//+kubebuilder:rbac:` markers in the reconciler code. Editing those markers is how you change the operator's RBAC; do not edit the generated YAML by hand.

## Running envtest

`envtest` is a controller-runtime testing pattern: it starts an in-process kube-apiserver (and optionally etcd), applies your CRDs, and lets your reconciler run against a real API surface without a real cluster. It is the right tool for testing reconcile logic that depends on watches, finalizers, owner references, and similar API-server behavior that fakes do not model correctly.

```
make envtest    # download envtest assets if missing
go test ./internal/controllers/... -v   # then run the tests directly
```

Use envtest where the reconciler's behavior depends on:

- Status subresources (the apiserver enforces `metadata.resourceVersion` correctly).
- Finalizers (apiserver garbage-collects only when finalizers are clear).
- Server-side apply (the apiserver tracks field managers).

Use plain Go tests where the behavior is local to the code (a helper, a parser, a mapping).

## Debugging conditions

Conditions are the operator's primary user-visible surface, and they are the most common bug class during development. Three patterns help:

- **Read the diff.** When a reconciler sets a condition, log the old and new condition. `meta.SetStatusCondition` carries a `lastTransitionTime` that is only updated when the status actually changes; that is what you want to compare against in tests.
- **Test condition transitions explicitly.** A reconciler test should assert both "condition X is True with reason Y" and "the previous condition's `lastTransitionTime` did not change". The two together catch the common bug of "always rewrite the condition, drifting the timestamp every reconcile".
- **Mirror the engine's reason verbatim.** When the operator translates `HelmRelease.status.conditions` or `velero.io/Backup.status` into `ApplicationInstance.status.conditions` / `Operation.status.conditions`, copy the upstream reason. Tests should pin the reason to what the upstream actually emits, not to what the test author guessed.

If a condition is not transitioning as expected, look at the reconcile loop's event-driven shape: a missed `Reconcile` call usually means the reconciler is not watching the resource whose change should have triggered it. The `OwnerReference` and `controllerutil.SetControllerReference` discipline is the most common place this slips.

## Running the operator against a real Odoo

For changes that exercise the Pull-mode client, you need an vWorkspace Server instance. The smallest path:

1. Run an Odoo with the vWorkspace addons enabled (`docker-compose -f develop/docker-compose.yml up` in the parent project's repo).
2. Issue a registration token from the control plane's Cluster Registry.
3. Apply a `Cluster` CR pointing at the local Odoo:

   ```yaml
   apiVersion: ops.vworkspace.io/v1alpha1
   kind: Cluster
   metadata:
     name: dev-local
     namespace: vworkspace-system
   spec:
     connectivityMode: pull
     odoo:
       endpoint: http://host.docker.internal:8069
     registration:
       token: <issued token>
   ```

`host.docker.internal` is reachable from inside the kind cluster on macOS and Windows; on Linux you may need to use the host's IP directly.

## What CI runs

The project's CI runs on every PR. The gating checks are:

- `make generate manifests` produces no diff.
- `make fmt vet` passes.
- `make lint` (i.e., `golangci-lint run`) passes.
- `make test` passes (unit + envtest).
- `make e2e` passes (in a separate, larger workflow run).

If any check fails, the PR is blocked. CI does not pre-commit changes for you; rerun the generation locally and push.

## Related material

- [project-layout.md](project-layout.md) — Where each file lives.
- [build-and-test.md](build-and-test.md) — The full list of `make` targets.
- [coding-style.md](coding-style.md) — Style and conventional-commits guidance.
- [release-process.md](release-process.md) — What happens to your change after it merges.
