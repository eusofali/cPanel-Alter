#!/usr/bin/env bash
# Updates HMEA Panel to the newest version from GitHub. Websites, databases
# and mail keep running; only the panel itself restarts.
#
#   bash /opt/hmea-panel/update.sh
set -euo pipefail

[ "$(id -u)" -eq 0 ] || { echo "Run as root: sudo bash $0" >&2; exit 1; }
HERE="$(cd "$(dirname "$0")" && pwd)"
cd "$HERE"
export HOME="${HOME:-/root}" GOFLAGS=-buildvcs=false

git pull --ff-only
make build
install -m 755 bin/hmea-panel /usr/local/bin/hmea-panel
install -m 644 scripts/hmea-panel.service /etc/systemd/system/hmea-panel.service
systemctl daemon-reload
systemctl restart hmea-panel
echo "HMEA Panel updated: $(git log -1 --format='%h %s')"
