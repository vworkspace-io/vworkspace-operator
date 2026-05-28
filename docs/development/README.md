# Development

**Status:** Alpha — APIs are at `v1alpha1` and may evolve.
**Last Updated:** 2026-05-28

This chapter is the developer's reference for contributing to `vworkspace-operator`. The repository root's `CONTRIBUTING.md` covers the process (how to file issues, how to propose changes, DCO sign-off); this chapter expands on the engineering details a contributor needs once their PR has been triaged.

The reader is assumed to be a Go developer who has read the [concepts chapter](../concepts/README.md) and wants to make changes to the operator itself. If you are an operator (the person) rather than a contributor, the [operate chapter](../operate/README.md) is probably what you want.

## Read in order

1. [contributing.md](contributing.md) — Dev workflow. Setting up a local kind cluster, installing CRDs, running the operator locally against kind, running unit tests, generating CRDs and RBAC, running envtest, debugging conditions.
2. [project-layout.md](project-layout.md) — The intended Kubebuilder Go layout. Planned, not yet implemented. What each directory holds and where the Argo CD adapter will live later.
3. [build-and-test.md](build-and-test.md) — Make targets and concrete commands. `make manifests`, `make generate`, `make fmt vet`, `make test`, `make docker-build`, `make deploy`, `make undeploy`. Running unit tests, envtest, and e2e tests against kind.
4. [release-process.md](release-process.md) — SemVer pre-1.0; signed tags; container image publishing; OCI Helm chart releases; `CHANGELOG.md`; Release-Please-like vs hand-curated options; CRD conversion-webhook discipline for breaking changes.
5. [coding-style.md](coding-style.md) — Go style: `gofmt`, `goimports`, `golangci-lint`. Naming. Logging fields. Reflection. Test layout. Conventional commits.

## How this chapter differs from `CONTRIBUTING.md`

The root `CONTRIBUTING.md` covers things every contributor needs to know once: the project's communication norms, code of conduct, DCO sign-off requirement, how to report security issues. The documents in this chapter are working references that a contributor returns to repeatedly while they are coding: what to run, what to check, how the project is organized internally.

If you find a contradiction between this chapter and `CONTRIBUTING.md`, `CONTRIBUTING.md` is the procedural source of truth; the documents here are the engineering reference.

## Current state versus planned state

The operator is in active early development; not every file in the repository tree exists yet. Where this chapter describes a layout, a tool, or a CI gate, it is the **intended** state — what the project is converging on. Where the description matches what is on disk today, it is also the current state.

The intended-vs-actual gap is called out in [project-layout.md](project-layout.md) (the Kubebuilder Go layout described there is the planned layout). The build commands in [build-and-test.md](build-and-test.md) work today against the parts of the repository that exist; commands that depend on files that have not been written yet are marked as such.

## Related material

- The repository's `CONTRIBUTING.md` and `CODE_OF_CONDUCT.md` at the project root.
- [../adr/README.md](../adr/README.md) — Architecture decisions that constrain what a contribution can change.
- [../rfcs/README.md](../rfcs/README.md) — How larger proposals enter the project.
