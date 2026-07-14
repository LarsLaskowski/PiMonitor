#!/usr/bin/env bash
# Installs or upgrades PiMonitor as a systemd service.
#
# Usage:
#   sudo ./install.sh [path-to-pimonitor-binary]
#
# When run from an extracted release archive, no argument is needed: the
# 'pimonitor' binary sits next to this script and is picked up automatically.
# You can still pass an explicit path to the binary file itself (e.g.
# ./pimonitor), which must point to the binary, not a directory.
#
# If no binary is found next to the script and no path is given, this script
# tries to build one with the local Go toolchain (useful when running directly
# on a Pi with Go installed, from a source checkout). Safe to re-run: it won't
# overwrite an existing config file, and re-enabling already-enabled units is a
# no-op.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "error: this script must be run as root (e.g. with sudo)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY_PATH="${1:-}"
# When run from an extracted release archive, the binary sits next to this
# script; pick it up automatically so no path argument is needed.
if [[ -z "$BINARY_PATH" && -f "$SCRIPT_DIR/pimonitor" ]]; then
  BINARY_PATH="$SCRIPT_DIR/pimonitor"
fi
if [[ -z "$BINARY_PATH" ]]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "error: no binary path given and 'go' is not installed to build one" >&2
    echo "       either pass a path to a cross-compiled binary, or install Go" >&2
    exit 1
  fi
  echo "No binary path given; building with the local Go toolchain..."
  BUILD_OUT="$(mktemp -d)/pimonitor"
  # Embed the version (shown on the dashboard) the same way the Makefile and
  # GoReleaser do. Outside a git checkout (e.g. an extracted source archive
  # with no tags) this falls back to "dev".
  VERSION="$(cd "$REPO_ROOT" && git describe --tags --always --dirty 2>/dev/null || echo dev)"
  (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=$VERSION" -o "$BUILD_OUT" ./cmd/pimonitor)
  BINARY_PATH="$BUILD_OUT"
fi

if [[ ! -f "$BINARY_PATH" ]]; then
  echo "error: binary not found at $BINARY_PATH" >&2
  exit 1
fi

echo "Installing binary..."
install -m 755 "$BINARY_PATH" /usr/local/bin/pimonitor

echo "Ensuring system user 'pimonitor' exists..."
if ! id -u pimonitor >/dev/null 2>&1; then
  useradd --system --no-create-home --shell /usr/sbin/nologin pimonitor
fi

echo "Installing configuration..."
# The config file may contain the api_key secret, so keep the directory and
# file readable only by root and the pimonitor service user (the 'pimonitor'
# group is created by useradd above).
mkdir -p /etc/pimonitor
chown root:pimonitor /etc/pimonitor
chmod 750 /etc/pimonitor
if [[ ! -f /etc/pimonitor/config.yaml ]]; then
  install -m 640 -o root -g pimonitor "$SCRIPT_DIR/pimonitor.example.yaml" /etc/pimonitor/config.yaml
  echo "  wrote default config to /etc/pimonitor/config.yaml"
else
  echo "  /etc/pimonitor/config.yaml already exists, leaving it untouched"
  chown root:pimonitor /etc/pimonitor/config.yaml
  chmod 640 /etc/pimonitor/config.yaml
fi

echo "Installing systemd units..."
install -m 644 "$SCRIPT_DIR/pimonitor.service" /etc/systemd/system/pimonitor.service
install -m 644 "$SCRIPT_DIR/pimonitor-apt-update.service" /etc/systemd/system/pimonitor-apt-update.service
install -m 644 "$SCRIPT_DIR/pimonitor-apt-update.timer" /etc/systemd/system/pimonitor-apt-update.timer

systemctl daemon-reload

echo "Enabling and starting services..."
systemctl enable --now pimonitor.service
systemctl enable --now pimonitor-apt-update.timer

echo "Running an initial apt cache refresh so the update count isn't empty on first view..."
systemctl start pimonitor-apt-update.service || true

echo
echo "Done. Check status with:"
echo "  systemctl status pimonitor.service pimonitor-apt-update.timer"
echo "  journalctl -u pimonitor -f"
