# Publishing documentation (GitHub Pages)

**Last updated:** 2026-05-30

This repository publishes English documentation with [MkDocs Material](https://squidfunk.github.io/mkdocs-material/) to **GitHub Pages** at:

**https://operator.docs.vworkspace.io/**

Source Markdown lives under [`docs/`](README.md). The site is built on every push to `main` by [`.github/workflows/docs.yml`](https://github.com/vworkspace-io/vworkspace-operator/blob/main/.github/workflows/docs.yml)).

## URL strategy

| Approach | Operator URL | Server (future) | GitHub Pages fit |
|----------|----------------|-----------------|------------------|
| **Current (recommended)** | `https://operator.docs.vworkspace.io/` | `https://server.docs.vworkspace.io/` | One custom domain per repo/site root — works natively |
| **Preferred long-term** | `https://docs.vworkspace.io/vworkspace-operator/` | `https://docs.vworkspace.io/vworkspace-server/` | Requires a central **hub** repo (see below) |

We publish **operator docs only** for now. Server documentation can follow the same subdomain pattern (`server.docs.vworkspace.io`) when that repo is ready.

### Why path-based URLs need a hub repo later

GitHub Pages attaches a custom domain to **one site root** per deployment. Each project repo (`vworkspace-operator`, `vworkspace-server`) builds its own site at `/` on its Pages host. You cannot point `docs.vworkspace.io` at two repos and get `/vworkspace-operator` and `/vworkspace-server` without something in front that merges them.

A practical future layout:

```text
docs.vworkspace.io  (single GitHub Pages site: vworkspace-docs hub repo)
├── /vworkspace-operator/   ← built artifact from operator repo (subpath / base URL)
└── /vworkspace-server/     ← built artifact from server repo
```

At a high level:

1. Add a **`vworkspace-docs`** repository with Pages on `docs.vworkspace.io`.
2. CI in each product repo runs `mkdocs build` with `site_url` and (if needed) `use_directory_urls` / Material `site.base_url` set for the subpath (e.g. `/vworkspace-operator/`).
3. The hub workflow downloads both `site/` trees and copies them into `operator/` and `server/` directories before deploy, or uses a small static index that links to each section.

Until that hub exists, **two-level subdomains** (`operator.docs`, `server.docs`) are the simplest correct option.

## One-time setup checklist

### 1. DNS

Create a **CNAME** record for `operator.docs.vworkspace.io` pointing at the GitHub Pages host for the `vworkspace-io` organization (not a repo path).

| Type  | Name (host)     | Value                     | TTL  |
|-------|-----------------|---------------------------|------|
| CNAME | `operator.docs` | `vworkspace-io.github.io` | 300+ |

Notes:

- In most providers, if the zone is `vworkspace.io`, the **name** is `operator.docs` (full name: `operator.docs.vworkspace.io`).
- If you delegate `docs.vworkspace.io` to its own zone, create a CNAME for `operator` in that zone with target `vworkspace-io.github.io`.
- Point at **`vworkspace-io.github.io` only** — not `…/vworkspace-operator`.
- Do not use a path in DNS; paths are an application/hub concern.
- Propagation may take minutes up to 48 hours.

Keep [`docs/CNAME`](CNAME) and `site_url` in [`mkdocs.yml`](https://github.com/vworkspace-io/vworkspace-operator/blob/main/mkdocs.yml)) in sync with the hostname you configure in GitHub.

### 2. GitHub repository settings (`vworkspace-io/vworkspace-operator`)

1. Open **Settings → Pages**.
2. Under **Build and deployment**, set **Source** to **GitHub Actions** (not “Deploy from a branch”).
3. Merge to `main` and wait for the **docs** workflow to succeed.
4. Under **Custom domain**, enter `operator.docs.vworkspace.io`.
5. Wait until GitHub reports **DNS check successful**.
6. Enable **Enforce HTTPS** after the certificate is issued.

### 3. Organization settings

If Pages is disabled for the org, an admin must enable GitHub Pages for `vworkspace-io` under **Organization settings → Pages**.

## Local preview

```bash
cd /path/to/vworkspace-operator
python3 -m venv .venv-docs
source .venv-docs/bin/activate
pip install -r requirements-docs.txt
mkdocs serve
```

Open http://127.0.0.1:8000/ (local URLs omit the production subdomain).

Before pushing, run the same check as CI:

```bash
mkdocs build --strict
```

## What gets deployed

| File | Purpose |
|------|---------|
| [`mkdocs.yml`](https://github.com/vworkspace-io/vworkspace-operator/blob/main/mkdocs.yml)) | Site name, `site_url`, theme, navigation |
| [`docs/CNAME`](CNAME) | Custom domain file copied into the built site root |
| [`requirements-docs.txt`](https://github.com/vworkspace-io/vworkspace-operator/blob/main/requirements-docs.txt)) | MkDocs Material dependency |
| [`.github/workflows/docs.yml`](https://github.com/vworkspace-io/vworkspace-operator/blob/main/.github/workflows/docs.yml)) | Build on push to `main`, deploy via GitHub Pages |

The workflow uses `actions/upload-pages-artifact` and `actions/deploy-pages` with the `github-pages` environment — no `gh-pages` branch is required.

## Link conventions

- Use relative paths for in-repo doc links (`../connectivity/pull-mode.md`) so they work on GitHub and on MkDocs.
- Cross-repo links to [vWorkspace Server](https://github.com/vworkspace-io/vworkspace-server) and [vworkspace](https://github.com/vworkspace-io/vworkspace) use full GitHub URLs.
- The root [README](https://github.com/vworkspace-io/vworkspace-operator/blob/main/README.md)) includes a badge to the live docs URL once Pages is configured.

## Troubleshooting

| Symptom | What to check |
|---------|----------------|
| DNS check fails | CNAME targets `vworkspace-io.github.io`; no conflicting A/AAAA on `operator.docs` |
| 404 after deploy | Pages **Source** is **GitHub Actions**; `docs` workflow green on `main` |
| Certificate pending | DNS propagated; disable orange-cloud/proxy on the CNAME if verification fails |
| `mkdocs build --strict` fails | Broken nav entries or internal links to missing files |
| Wrong absolute URLs | `site_url` in `mkdocs.yml` matches the live custom domain |

## What not to publish

Do not commit secrets, kubeconfigs, or environment-specific credentials. Use placeholders (for example `https://your-server.example.org`) as in the install guides.

## Alternative: docsify (markdown-only)

For a zero-build preview, you can host `docs/` with [docsify](https://docsify.js.org/). MkDocs Material is the supported and CI-enforced path for this repository.
