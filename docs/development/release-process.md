# Release process

**Status:** Alpha — APIs are at `v1alpha1` and the project is pre-1.0.
**Last Updated:** 2026-05-28

This document describes how `vworkspace-operator` releases are produced, signed, and published. The project is pre-1.0 (`v0.x.y`); the release cadence and process are deliberately conservative. CRD evolution requires a conversion webhook and a deprecation window of at least one minor release; that constraint shapes the rest of the process.

The reader is a maintainer cutting a release, or a contributor curious about what happens to their merge.

## Versioning

The operator uses Semantic Versioning. Pre-1.0:

| Bump          | Trigger                                                                                                                                              |
|---------------|------------------------------------------------------------------------------------------------------------------------------------------------------|
| `v0.x.y → v0.x.y+1` (patch) | Bug fix; no API or behavior change visible to users. Safe to roll out without operator intervention.                                                  |
| `v0.x.y → v0.x+1.0` (minor) | Additive API change (a new field, a new CRD value); new feature; behavior change visible to users. Documented in `CHANGELOG.md`; release notes call out breaking expectations. |
| `v0.x.y → v1.0.0` (eventually major) | Stable API; the v1alpha1 CRDs have a successor (v1beta1, then v1) and the operator has gone through enough hands to make the API contract real.        |

A `v0.x.0` minor may introduce breaking changes in the CRDs **only through the conversion-webhook discipline** described below. There is no "the next minor bump just renames `spec.foo` to `spec.bar`" path.

Once at `v1.0.0`, the project follows standard SemVer: breaking changes only in major bumps. The roadmap targets v1 in Q1 2027 in line with the parent project's [ROADMAP](https://github.com/vworkspace-io/vworkspace/blob/main/docs/ROADMAP.md); the parent project's date can slip and so can this one.

## What ships in a release

A release produces four artifacts:

1. **A Git tag.** Signed (`git tag -s vX.Y.Z`) by a maintainer whose key is in the project's `KEYS` file at the repository root.
2. **A container image.** Pushed to the project's OCI image registry. Multi-arch (`linux/amd64`, `linux/arm64`). Signed with `cosign` against a transparency log; the signature lives next to the image in the registry.
3. **A Helm chart.** Packaged as an OCI artifact and pushed to the project's OCI chart registry. The chart's `Chart.yaml` records the operator image tag the chart deploys; `appVersion` matches the operator's SemVer.
4. **A GitHub release.** With the auto-generated release notes (sourced from `CHANGELOG.md`), the binary `SHA256SUMS` file, and a link to the container image and Helm chart.

Container images for development and CI are published to **Docker Hub**: `docker.io/vworkspace/vworkspace-operator` (`:latest` on `main`, `:<git-sha>`, and `:<tag>` for `v*` git tags). Configure repository secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` for the `docker` job in `.github/workflows/ci.yml`. See [../install/container-images.md](../install/container-images.md).

Helm chart OCI registry hostnames remain placeholders until the chart packaging pipeline is wired.

## Signing

Tags are signed with the maintainer's GPG key (`git tag -s`). The project's `KEYS` file lists the public keys of every active maintainer; the file is itself part of the repository so a verifier can clone, import, and verify in three commands.

Container images are signed with `cosign sign --key cosign.key ghcr.io/vworkspace-io/vworkspace-app-operator:vX.Y.Z`. The cosign signing key is held by the project's release automation; a Fulcio-based keyless-signing variant is on the roadmap and will replace the long-lived signing key once the project's release infrastructure is public.

Helm charts are pushed via `helm push` with the same cosign signature attached.

Verifiers can confirm an image was produced by the project's release pipeline with:

```
cosign verify --key https://example.com/vworkspace-cosign.pub \
  ghcr.io/vworkspace-io/vworkspace-app-operator:vX.Y.Z
```

(Substitute the real cosign public key URL when the project is public.)

## Release-Please-like vs hand-curated

The project will pick one of two processes; both are documented so the choice is informed.

### Option A: Release-Please-like (automated)

A bot (Release Please or an in-house equivalent) parses Conventional Commit messages, opens a "release PR" with the version bump and the regenerated `CHANGELOG.md`, and tags + publishes when the release PR is merged.

- **Pros.** Mechanical. Reduces the chance a release ships without a changelog. Reduces human error.
- **Cons.** Requires strict Conventional Commit discipline. Forces a particular notation; some changes are awkward to encode.

### Option B: Hand-curated

A maintainer prepares a release PR by hand: bumps the version in the relevant files, writes the release notes in `CHANGELOG.md`, runs the release scripts, and ships.

- **Pros.** Maximum flexibility in release-note writing. Easier to bundle a coherent narrative across multiple commits.
- **Cons.** Slower. Easier to forget a changelog entry.

Both options share the same artifact set and the same signing path; the only difference is who writes the version bump and the release notes.

The project is hand-curating today and will switch to automated once the cadence is steady and Conventional Commits are uniformly enforced.

## CHANGELOG.md

The project maintains a single `CHANGELOG.md` at the repository root. The format follows Keep a Changelog: one `## [vX.Y.Z] - YYYY-MM-DD` heading per release, with `### Added`, `### Changed`, `### Deprecated`, `### Removed`, `### Fixed`, `### Security` subheadings as relevant. Each entry is a single sentence; deeper context lives in the linked PR or issue.

Release notes are written for the operator (the person), not the developer. "Fixed a typo in `applicationinstance_controller.go`" is a poor entry; "Fixed a missing condition transition when an `ApplicationInstance` was deleted while a Helm upgrade was in flight (#127)" is the shape we aim for.

Breaking changes get a "**Breaking:**" prefix and a link to the migration guide for that release. The migration guide lives under `docs/operate/upgrades.md` ([../operate/upgrades.md](../operate/upgrades.md)) for cross-version skew rules and, for any specific breaking change, in a dedicated section of `CHANGELOG.md`.

## CRD evolution

Breaking CRD changes go through a conversion webhook and a deprecation window of at least one minor release. The procedure:

1. **Introduce the new version.** Add `api/v1beta1/` (or whatever the new version is). Mark `v1alpha1` `served: true, storage: true` and `v1beta1` `served: true, storage: false`. Add the conversion webhook that converts back and forth.
2. **Test in candidate.** A `candidate` release of the operator ships with both versions served. The operator's reconcilers internally use the new version; the conversion webhook handles requests against the old.
3. **Promote storage.** A follow-up minor release marks `v1beta1` `storage: true` and runs a one-time conversion of stored objects (a startup job).
4. **Deprecate the old version.** The next minor marks `v1alpha1` `deprecated: true` and emits a `CRDVersionDeprecated` audit event on every write to the old version.
5. **Remove the old version.** At least one further minor release later, drop `v1alpha1`. The conversion webhook can be retired at the same time.

Skipping any of these steps breaks the contract. A maintainer who is unsure should write an RFC describing the change and the migration before opening a PR.

## CRD changes that are not breaking

Adding a new field, adding a new enum value (additive), adding a new optional sub-CRD: these are not breaking and ride along on minor releases without the conversion-webhook overhead. The `CHANGELOG.md` entry under `### Added` mentions them.

Renaming a field, removing an enum value, changing required/optional, changing validation in a tighter direction: breaking. Go through the conversion webhook path.

## Cutting a release (maintainer checklist)

For a hand-curated release:

1. Verify CI is green on `main`. No outstanding failing checks.
2. Verify `make manifests generate` produces no diff. If it does, commit the regenerated artifacts first.
3. Update `CHANGELOG.md`: replace the `## [Unreleased]` heading with `## [vX.Y.Z] - YYYY-MM-DD` and re-open a fresh `## [Unreleased]`.
4. Update the operator's version in the Helm chart's `Chart.yaml` (`version` and `appVersion`).
5. Update the compatibility matrix in `docs/operate/upgrades.md` if any minimum versions changed.
6. Open the release PR (`release: vX.Y.Z`); get one review.
7. Merge the PR.
8. Tag the merge commit: `git tag -s vX.Y.Z -m "Release vX.Y.Z"`. Push the tag.
9. The release workflow builds the multi-arch image, signs it, pushes it; packages the Helm chart, signs it, pushes it; opens the GitHub release with the notes pulled from `CHANGELOG.md`.
10. Announce on the project's communication channels.

For an automated release (when adopted), steps 3 through 7 are done by the bot's release PR; the maintainer reviews and merges it.

## Hotfixes

Critical bug fixes (security, broken release, regression) follow the same process compressed:

1. Branch from the affected release tag (`git checkout -b hotfix-vX.Y.Z+1 vX.Y.Z`).
2. Cherry-pick or hand-write the fix.
3. Update `CHANGELOG.md` with a `### Fixed` (or `### Security`) entry.
4. Tag and release as `vX.Y.Z+1`.

Hotfixes are released on `stable` immediately and do not wait for the next regular release cycle.

## Yanking

A released version that turns out to be unsafe can be marked yanked: the OCI registry entry is annotated; the Helm chart is republished with a `deprecated: true` field; the `CHANGELOG.md` `## [vX.Y.Z]` heading gets a "**Yanked:**" prefix and a link to the follow-up release. Container images and Helm charts are not deleted (other clusters may still be running them); they are marked.

Yanking is rare; the project would rather fix forward.

## Related material

- [coding-style.md](coding-style.md) — Conventional commits, which feed the release notes whether automated or hand-curated.
- [../operate/upgrades.md](../operate/upgrades.md) — How an operator (the person) consumes the releases this process produces.
- The repository root's `CHANGELOG.md` (when present) — The historical record.
