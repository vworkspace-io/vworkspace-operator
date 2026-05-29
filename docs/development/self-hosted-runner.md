# Self-hosted GitHub Actions runner

**Status:** Alpha
**Last Updated:** 2026-05-29

This repository's CI workflow (`.github/workflows/ci.yml`) targets runners labeled `self-hosted`. Jobs run **in parallel** with isolated checkout directories (`verify/`, `test/`, `lint/`, `e2e/`) so multiple jobs on one machine do not clobber `GITHUB_WORKSPACE`.

## Recommended setup

- Install **multiple** runner processes on the same host if you have spare CPU and memory (each registers with label `self-hosted`).
- Pre-install tools below so CI skips slow `apt` steps.
- Give each runner its own work directory via the default GitHub Actions layout; do not share a single manual checkout path between runners.

## Pre-install commands

Run on Ubuntu/Debian as a user that will run the runner service.

### Docker

```bash
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker "$USER"   # re-login for group membership
```

### kubectl

```bash
curl -LO "https://dl.k8s.io/release/$(curl -L -s https://dl.k8s.io/release/stable.txt)/bin/linux/amd64/kubectl"
sudo install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl
```

### kind

```bash
curl -Lo ./kind https://kind.sigs.k8s.io/dl/v0.24.0/kind-linux-amd64
chmod +x ./kind
sudo install -o root -g root -m 0755 kind /usr/local/bin/kind
```

### Go (1.22+)

Use the official tarball (adjust version and architecture as needed):

```bash
curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.profile
```

Or install via your distribution package manager / snap if it provides Go 1.22 or newer.

## CI isolation details

| Job    | Checkout path | `LOCALBIN` override              |
|--------|---------------|----------------------------------|
| verify | `verify/`     | `${{ github.workspace }}/bin-${{ github.job }}` |
| test   | `test/`       | `${{ github.workspace }}/bin-test` |
| lint   | `lint/`       | (default under checkout)         |
| e2e    | `e2e/`        | `${{ github.workspace }}/bin-e2e` |

E2E uses a unique Kind cluster name per workflow run: `vworkspace-operator-e2e-${{ github.run_id }}`.

Workflow concurrency for the same branch cancels in-progress runs: `group: ${{ github.workflow }}-${{ github.ref }}`.

## Registering a runner

Follow [GitHub's self-hosted runner documentation](https://docs.github.com/en/actions/hosting-your-own-runners/managing-self-hosted-runners/about-self-hosted-runners). Use labels `self-hosted` and `linux` to match the workflow.
