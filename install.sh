#!/usr/bin/env bash
# HMEA Panel one-step installer for Ubuntu 22.04/24.04 and Debian 12.
#
#   apt -y install git && git clone https://github.com/eusofali/cPanel-Alter.git /opt/hmea-panel && bash /opt/hmea-panel/install.sh
#
# It installs the build tools (Go, Node.js), builds the panel, then runs
# scripts/install.sh, passing on any options (--no-mail, --no-dns, --no-ftp,
# --no-apps, --no-firewall, --php-repo). Safe to run again.
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "Run as root: sudo bash $0" >&2; exit 1; }
. /etc/os-release
case "$ID" in ubuntu|debian) ;; *) echo "HMEA Panel supports Ubuntu and Debian only." >&2; exit 1 ;; esac
case "$(dpkg --print-architecture)" in amd64|arm64) ;; *) echo "Only 64-bit x86 and ARM servers are supported." >&2; exit 1 ;; esac

HERE="$(cd "$(dirname "$0")" && pwd)"
export DEBIAN_FRONTEND=noninteractive
say() { printf '\n==> %s\n' "$*"; }

# Building needs about 1.5 GB of memory; add swap on small servers.
RAM_MB=$(awk '/MemTotal/ {print int($2/1024)}' /proc/meminfo)
if [ "$RAM_MB" -lt 2500 ] && [ "$(swapon --noheadings | wc -l)" -eq 0 ] && [ ! -e /swapfile ]; then
  say "Adding a 2 GB swap file (server has ${RAM_MB} MB of memory)"
  fallocate -l 2G /swapfile && chmod 600 /swapfile && mkswap /swapfile >/dev/null && swapon /swapfile
  echo '/swapfile none swap sw 0 0' >> /etc/fstab
fi

say "Installing build tools"
apt-get update -q
apt-get install -y -q git make curl ca-certificates gnupg tar

if ! command -v go >/dev/null; then
  GO_VERSION=$(curl -fsSL 'https://go.dev/VERSION?m=text' | head -n1)
  say "Installing ${GO_VERSION}"
  curl -fsSL "https://go.dev/dl/${GO_VERSION}.linux-$(dpkg --print-architecture).tar.gz" | tar -C /usr/local -xz
  ln -sf /usr/local/go/bin/go /usr/local/bin/go
fi

if ! command -v node >/dev/null; then
  say "Installing Node.js 22"
  curl -fsSL https://deb.nodesource.com/gpgkey/nodesource-repo.gpg.key | gpg --dearmor --yes -o /usr/share/keyrings/nodesource.gpg
  echo "deb [signed-by=/usr/share/keyrings/nodesource.gpg] https://deb.nodesource.com/node_22.x nodistro main" > /etc/apt/sources.list.d/nodesource.list
  apt-get update -q
  apt-get install -y -q nodejs
fi

say "Building HMEA Panel (a few minutes on a small server)"
cd "$HERE"
export HOME="${HOME:-/root}" GOFLAGS=-buildvcs=false
make build

exec "$HERE/scripts/install.sh" "$@"
