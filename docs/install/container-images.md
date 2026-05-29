# Container images

**Status:** Alpha
**Last Updated:** 2026-05-29

## Docker Hub

Published images:

| Repository | Tags |
|------------|------|
| `docker.io/vworkspace/vworkspace-operator` | `:latest` (main branch), `:<git-sha>`, `:<semver>` (git tags `v*`) |

Build locally:

```bash
make docker-build IMG=docker.io/vworkspace/vworkspace-operator:$(git describe --tags --always)
make docker-push IMG=docker.io/vworkspace/vworkspace-operator:$(git describe --tags --always)
```

Deploy with kustomize:

```bash
make deploy IMG=docker.io/vworkspace/vworkspace-operator:v0.0.1
```

## CI publishing

On push to `main` or tags matching `v*`, the `docker` job in `.github/workflows/ci.yml` builds and pushes to Docker Hub.

Required GitHub repository secrets:

| Secret | Purpose |
|--------|---------|
| `DOCKERHUB_USERNAME` | Docker Hub user or organization name (`vworkspace`) |
| `DOCKERHUB_TOKEN` | Docker Hub access token with push permission |

See [../development/release-process.md](../development/release-process.md) for the full release flow.
