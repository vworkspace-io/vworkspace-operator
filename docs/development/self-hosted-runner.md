# Self-hosted GitHub Actions runner

**Status:** Alpha
**Last Updated:** 2026-05-29

This repository's CI workflow (`.github/workflows/ci.yml`) targets runners labeled `self-hosted`. Jobs run **in parallel** with isolated checkout directories (`verify/`, `test/`, `lint/`, `e2e/`) so multiple jobs on one machine do not clobber `GITHUB_WORKSPACE`.

## Recommended setup

- Install **multiple** runner processes on the same host if you have spare CPU and memory (each registers with label `self-hosted`).
- Pre-install tools below so CI skips slow `apt` steps.
- Configure **passwordless sudo** for the runner user if you rely on CI to install missing packages (`sudo apt-get`, `systemctl start docker`). Otherwise pre-install everything in this section.
- Give each runner its own work directory via the default GitHub Actions layout; do not share a single manual checkout path between runners.

## Pre-install commands

Run on Ubuntu/Debian as a user that will run the runner service.

### Build tools (make, gcc)

Required for `make test`, `make lint`, `make test-e2e`, and `hack/verify-generated.sh`.

```bash
sudo apt-get update
sudo apt-get install -y make build-essential git curl
```

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

See also [local-setup.md](local-setup.md) for workstation install without sudo.

Use the official tarball (adjust version and architecture as needed):

```bash
curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go && sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.profile
```

Or install via your distribution package manager / snap if it provides Go 1.22 or newer.


## Runner user and sudo

CI runs as the dedicated service user (for example `github-runner` on **harbor**). That user must not block non-interactive jobs.

### If you set a password by mistake

Setting a login password with `sudo passwd github-runner` does **not** by itself break CI, but any step that runs `sudo` will prompt for that password and the job will fail with:

`sudo: a terminal is required to read the password`

**Recommended — lock password login (service account):**

```bash
sudo passwd -l github-runner
```

SSH key authentication can still work; password login is disabled.

**Remove the password (empty password — not recommended on exposed hosts):**

```bash
sudo passwd -d github-runner
```

### Recommended host setup (harbor)

Run once as root or with sudo:

```bash
# 1) Lock password login (optional but recommended)
sudo passwd -l github-runner

# 2) Pre-install CI tools (avoids apt in the workflow)
sudo apt-get update
sudo apt-get install -y make build-essential git curl

# 3) Docker without sudo in jobs
curl -fsSL https://get.docker.com | sh
sudo usermod -aG docker github-runner
# Restart the runner service so group membership applies, e.g.:
# sudo systemctl restart actions.runner.*.service

# 4) Optional: kind + kubectl (or let CI install to ~/.local/bin if tools are missing)
```

### Passwordless sudo (only if you want CI to install packages)

Prefer pre-installing tools above. If CI must run `apt-get` or `systemctl` as the runner user:

```bash
sudo visudo -f /etc/sudoers.d/github-runner
```

Example (narrow):

```
github-runner ALL=(ALL) NOPASSWD: /usr/bin/apt-get, /usr/bin/systemctl, /usr/bin/service, /usr/bin/install
```

Broader (less secure):

```
github-runner ALL=(ALL) NOPASSWD: ALL
```

Workflow steps use `sudo -n` and fail with a clear error if NOPASSWD is not configured.

### Add user to docker group (avoid sudo for Docker)

```bash
sudo usermod -aG docker github-runner
```

Restart the GitHub Actions runner service after changing groups.

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
