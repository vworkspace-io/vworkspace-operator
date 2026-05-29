# Local development setup

**Status:** Alpha
**Last Updated:** 2026-05-29

This page covers installing Go and running the operator test suite on a Linux workstation or self-hosted GitHub Actions runner. For CI runner isolation and parallel job layout, see [self-hosted-runner.md](self-hosted-runner.md).

## Go 1.22+ (required)

The module in `go.mod` may require a newer Go release (currently 1.25.x). Install **Go 1.22 or newer**; match the version in `go.mod` when possible.

### Option A — Official tarball (recommended)

Works without `sudo` when installed under your home directory:

```bash
curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
rm -rf "${HOME}/.local/go"
tar -C "${HOME}/.local" -xzf /tmp/go.tar.gz
echo 'export PATH="${HOME}/.local/go/bin:${PATH}"' >> ~/.profile
export PATH="${HOME}/.local/go/bin:${PATH}"
go version
```

System-wide install (requires sudo):

```bash
curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf /tmp/go.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.profile
export PATH=/usr/local/go/bin:$PATH
go version
```

Adjust the tarball URL for your CPU architecture (`linux-arm64`, etc.) and desired version from [https://go.dev/dl/](https://go.dev/dl/).

### Option B — Distribution package manager

```bash
sudo apt-get update
sudo apt-get install -y golang-go   # verify: go version >= 1.22
```

If the distro package is too old, use Option A.

### Self-hosted runner user (`github-runner`)

Run the same commands as the user that owns the runner service (for example `github-runner` on **harbor**):

```bash
sudo -u github-runner bash -lc '
  curl -fsSL https://go.dev/dl/go1.24.4.linux-amd64.tar.gz -o /tmp/go.tar.gz
  rm -rf ~/.local/go && tar -C ~/.local -xzf /tmp/go.tar.gz
  grep -q ".local/go/bin" ~/.profile || echo "export PATH=\$HOME/.local/go/bin:\$PATH" >> ~/.profile
  export PATH=$HOME/.local/go/bin:$PATH
  go version
'
```

Restart the runner service after changing `PATH` so job steps inherit it.

## Other build tools

```bash
sudo apt-get update
sudo apt-get install -y make build-essential git curl
```

Optional for e2e: Docker, kind, kubectl — see [self-hosted-runner.md](self-hosted-runner.md).

## Clone and test

After merging Phase 1c (or any topic branch) into `main`:

```bash
cd vworkspace-operator
git fetch origin
git checkout main
git pull origin main

export PATH="${HOME}/.local/go/bin:${PATH}"   # if using user-local Go
make setup-envtest   # first time only
make test
```

`make test` runs code generation, `go fmt`, `go vet`, envtest-backed controller tests, and unit tests with coverage written to `cover.out`.

## Run the mock Odoo server (Phase 1d)

For Pull-mode development without real Odoo modules:

```bash
go run ./test/mockodoo/cmd/mockodoo -addr :8080
```

See [mock-odoo.md](mock-odoo.md) for endpoints and integration with the operator agent.

## Dev container

The repository includes a [`.devcontainer/devcontainer.json`](../../.devcontainer/devcontainer.json) based on `golang:1.25` with Docker-in-Docker. Open the folder in VS Code or Cursor with Dev Containers to get Go, make, and kubectl tooling without a host install.

## Troubleshooting

| Symptom | Fix |
|---------|-----|
| `go: command not found` | Ensure Go is on `PATH` (`which go`, reload shell or restart runner service). |
| `sudo: a terminal is required` | Install Go under `$HOME/.local/go` (no sudo) or pre-install on the runner host. |
| envtest download fails | Run `make setup-envtest` once; check network access to `storage.googleapis.com`. |
| Tests timeout on first run | envtest downloads Kubernetes test binaries (~100MB); subsequent runs are faster. |
