# Release notes template

GitHub Release bodies are **generated automatically** on every `v*` tag by `hack/render-release-notes.sh` (CI `release` job). Maintainers curate content in `CHANGELOG.md`; the script adds install commands, asset tables, and doc links.

## Maintainer checklist (before tagging)

1. Move `[Unreleased]` entries into a new `## [X.Y.Z] - YYYY-MM-DD` section.
2. Add a **one-line summary** immediately under the version heading (before `### Added`). It opens the generated release body; the GitHub Release **title** is only the tag (e.g. `v0.0.7`).
3. Group changes under Keep a Changelog headings: `### Added`, `### Changed`, `### Fixed`, etc.
4. Tag and push; CI publishes assets and the formatted release page.

## Generated release structure

```markdown
<summary paragraph from CHANGELOG>

Container image: `docker.io/vworkspace/vworkspace-operator:vX.Y.Z`

## Install
(Helm + kubectl commands with this version's download URLs)

## Release assets
(table: .tgz, crds.yaml, operator.yaml, SHA256SUMS)

## What's changed
(### Added / ### Changed / … from CHANGELOG)

**Full changelog:** link to CHANGELOG.md at the tag
```

## Local preview

```bash
VERSION=0.0.7 ./hack/render-release-notes.sh
VERSION=0.0.7 ./hack/render-release-notes.sh --print-title
```

Package artifacts locally: `VERSION=0.0.7 make package-release`.

See also [development/release-process.md](development/release-process.md).
