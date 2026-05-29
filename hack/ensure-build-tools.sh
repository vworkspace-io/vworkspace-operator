#!/usr/bin/env bash
# Install make and common build dependencies on fresh self-hosted runners.
set -euo pipefail
missing=()
command -v make >/dev/null 2>&1 || missing+=(make)
command -v gcc >/dev/null 2>&1 || missing+=(build-essential)
command -v git >/dev/null 2>&1 || missing+=(git)
command -v curl >/dev/null 2>&1 || missing+=(curl)
if [ "${#missing[@]}" -eq 0 ]; then
  command -v make
  command -v git
  command -v curl
  exit 0
fi
# apt-get requires root on Debian/Ubuntu; pre-install on the runner host when sudo is not passwordless.
if ! command -v apt-get >/dev/null 2>&1; then
  echo "::error::Missing tools (${missing[*]}). Install them on the runner host (no apt-get on this OS)."
  exit 1
fi
apt_install() {
  if command -v sudo >/dev/null 2>&1; then
    if ! sudo -n true 2>/dev/null; then
      echo "::error::Runner user needs passwordless sudo for apt-get, or pre-install: make build-essential git curl"
      echo "::error::On harbor: sudo apt-get install -y make build-essential git curl"
      echo "::error::Or: sudo visudo -f /etc/sudoers.d/github-runner — see docs/development/self-hosted-runner.md"
      exit 1
    fi
    sudo -n apt-get update
    sudo -n DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
  else
    apt-get update
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
  fi
}
apt_install make build-essential git curl
command -v make
command -v git
command -v curl
