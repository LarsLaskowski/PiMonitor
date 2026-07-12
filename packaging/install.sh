#!/usr/bin/env bash
# Installs or upgrades PiMonitor as a systemd service.
#
# Usage:
#   sudo ./install.sh [path-to-pimonitor-binary]
#
# If no binary path is given, this script tries to build one with the
# local Go toolchain (useful when running directly on a Pi with Go
# installed). Safe to re-run: it won't overwrite an existing config file,
# and re-enabling already-enabled units is a no-op.

set -euo pipefail

if [[ $EUID -ne 0 ]]; then
  echo "error: this script must be run as root (e.g. with sudo)" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"

BINARY_PATH="${1:-}"
if [[ -z "$BINARY_PATH" ]]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "error: no binary path given and 'go' is not installed to build one" >&2
    echo "       either pass a path to a cross-compiled binary, or install Go" >&2
    exit 1
  fi
  echo "No binary path given; building with the local Go toolchain..."
  BUILD_OUT="$(mktemp -d)/pimonitor"
  (cd "$REPO_ROOT" && CGO_ENABLED=0 go build -o "$BUILD_OUT" ./cmd/pimonitor)
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
mkdir -p /etc/pimonitor
if [[ ! -f /etc/pimonitor/config.yaml ]]; then
  install -m 644 "$SCRIPT_DIR/pimonitor.example.yaml" /etc/pimonitor/config.yaml
  echo "  wrote default config to /etc/pimonitor/config.yaml"
else
  echo "  /etc/pimonitor/config.yaml already exists, leaving it untouched"
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
