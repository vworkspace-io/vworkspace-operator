# Publishing documentation (GitHub Pages)

**Last Updated:** 2026-05-30

This repository keeps documentation as Markdown under [`docs/`](README.md). For the public release, the same content can be published as a static site on GitHub Pages without changing the on-disk layout.

## Recommended approach: MkDocs Material

1. Add a `mkdocs.yml` at the repository root (example skeleton):

```yaml
site_name: vworkspace-operator
site_url: https://vworkspace-io.github.io/vworkspace-operator/
repo_url: https://github.com/vworkspace-io/vworkspace-operator
edit_uri: edit/main/docs/

theme:
  name: material

nav:
  - Home: README.md
  - Concepts: concepts/overview.md
  - Connectivity: connectivity/README.md
  - API: api/README.md
  - Install: install/README.md
  - Operate: operate/README.md
  - Security: security/README.md
  - Development: development/README.md
  - ADRs: adr/README.md

docs_dir: docs
```

2. Add a GitHub Actions workflow (`.github/workflows/docs.yml`) that runs on `push` to `main`:

```yaml
name: docs
on:
  push:
    branches: [main]
permissions:
  contents: read
  pages: write
  id-token: write
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-python@v5
        with:
          python-version: "3.x"
      - run: pip install mkdocs-material
      - run: mkdocs build
      - uses: actions/upload-pages-artifact@v3
        with:
          path: site
      - uses: actions/deploy-pages@v4
```

3. In the repository **Settings → Pages**, set source to **GitHub Actions**.

4. Enable Pages on the organization or repository as required.

## Alternative: docsify (markdown-only)

If you prefer zero build step, host `docs/` with [docsify](https://docsify.js.org/) and a single `index.html` at the repo root or on a `gh-pages` branch. The nav structure in [docs/README.md](README.md) maps directly to sidebar links.

## Link conventions

- Internal doc links use relative paths (`../connectivity/pull-mode.md`) so they work in GitHub's native renderer and in MkDocs.
- Cross-repo links to [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) and the parent [vworkspace](https://github.com/vworkspace-io/vworkspace) project use full GitHub URLs.
- After enabling Pages, update the site URL in `mkdocs.yml` and add a badge to the root [README](../README.md) if desired.

## What not to publish

Do not commit secrets, kubeconfigs, or environment-specific URLs into docs intended for Pages. Use placeholders (`https://your-server.example.org`) as in the install guides.
