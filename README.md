# PiMonitor

A lightweight system-monitoring dashboard for the Raspberry Pi. A single
Go binary collects system metrics, serves a self-contained web dashboard,
and exposes a versioned REST API for third-party integration (e.g. home
automation systems like openHAB). Runs as a systemd service.

![PiMonitor dashboard](docs/dashboard.png)

## Features

- **CPU usage** - overall and per-core percentage, with a trend sparkline
- **Load average** - 1/5/15 minute values shown as gauges, scaled to CPU
  core count
- **CPU temperature** - auto-detected thermal zone, with an optional
  `vcgencmd`-sourced GPU temperature if available
- **Memory & swap** usage
- **Filesystem usage** - per mounted filesystem, pseudo filesystems
  (tmpfs, proc, overlay, ...) excluded by default
- **Network throughput** per interface (optional, can be disabled)
- **System identity** - kernel version, OS distribution, Raspberry Pi
  model
- **Uptime** - device clock and time since boot
- **Available apt updates** - count and package list, with a staleness
  indicator for the underlying apt cache
- Short in-memory history (default: last 60 minutes) for sparklines - no
  database, nothing persisted across restarts
- A versioned REST API (`/api/v1/...`) for third-party consumers, with
  optional API-key authentication - see [`docs/API.md`](docs/API.md)

## Architecture

```
                  ┌─────────────────────────────┐
                  │        pimonitor (Go)       │
                  │                             │
 /proc, /sys ───▶ │  collector  ──▶  httpapi   │───▶ Web dashboard
 apt cache        │  (in-memory          │      │     (embedded assets)
                  │   ring buffers)      ▼      │
                  │                 /api/v1/... │───▶ Third-party clients
                  └─────────────────────────────┘        (e.g. openHAB)

 pimonitor.service          pimonitor-apt-update.timer (root)
 runs unprivileged    <--   refreshes apt cache every 6h;
 reads /proc, /sys,         pimonitor itself never needs
 and the apt cache          root privileges
 read-only
```

The main service (`pimonitor.service`) never requires root: it only reads
world-readable files under `/proc`, `/sys/class/thermal`,
`/etc/os-release`, and the apt cache, plus the read-only
`apt list --upgradable` command. A separate, root-privileged systemd timer
(`pimonitor-apt-update.timer`) refreshes the apt cache periodically -
see [`SECURITY.md`](SECURITY.md) for the full threat model.

## Building

Requires Go 1.22+.

```sh
make build          # native build, for local development -> bin/pimonitor
make build-arm64     # cross-compile for 64-bit Raspberry Pi OS (Pi 3/4/5)
make build-arm       # cross-compile for 32-bit / Pi Zero/1 (GOARM=6)
make test            # go test ./...
make lint             # golangci-lint (also enforced in CI)
```

`make run` starts the server locally against
`packaging/pimonitor.example.yaml` - useful for frontend/API development on
a non-Pi machine. Hardware-specific metrics (e.g. CPU temperature) simply
report as unavailable rather than failing.

Pre-built binaries for tagged releases are published via GitHub Actions
using [goreleaser](https://goreleaser.com/) - see the
[Releases](https://github.com/larslaskowski/pimonitor/releases) page.

## Installing on a Raspberry Pi

```sh
# Copy the binary (or the release tarball) and the packaging/ directory
# to the Pi, then:
sudo ./packaging/install.sh path/to/pimonitor-arm64
```

This creates an unprivileged `pimonitor` system user, installs the binary
to `/usr/local/bin/pimonitor`, writes a default config to
`/etc/pimonitor/config.yaml` (if one doesn't already exist), installs the
two systemd units, and enables/starts both. See
[`packaging/pimonitor.example.yaml`](packaging/pimonitor.example.yaml) for
every configuration option.

If you have a Go toolchain installed directly on the Pi, you can omit the
binary path and `install.sh` will build one for you:

```sh
sudo ./packaging/install.sh
```

Check status with:

```sh
systemctl status pimonitor.service pimonitor-apt-update.timer
journalctl -u pimonitor -f
```

The dashboard is then available at `http://<pi-address>:8080/`.

## Updating

PiMonitor is distributed as a single binary, so upgrading is a matter of
replacing that binary and restarting the service. `install.sh` is safe to
re-run for this purpose: it overwrites the binary and the systemd units but
leaves an existing `/etc/pimonitor/config.yaml` untouched.

1. **Get the new version.** Either download the binary (or release tarball)
   for your architecture from the
   [Releases](https://github.com/larslaskowski/pimonitor/releases) page, or
   pull the latest source and cross-compile it (`make build-arm64` /
   `make build-arm`).

2. **Re-run the installer** with the updated packaging directory and the new
   binary:

   ```sh
   sudo ./packaging/install.sh path/to/pimonitor-arm64
   ```

   This replaces `/usr/local/bin/pimonitor`, refreshes the systemd units, and
   runs `systemctl daemon-reload`. Your configuration is preserved.

3. **Restart the service** so the running process picks up the new binary.
   `install.sh` starts the units but does not restart an already-running
   service, so do it explicitly:

   ```sh
   sudo systemctl restart pimonitor.service
   ```

   If the `pimonitor-apt-update.timer` or its service unit changed, also run
   `sudo systemctl restart pimonitor-apt-update.timer`.

4. **Verify** the new version is running:

   ```sh
   pimonitor -version
   systemctl status pimonitor.service
   journalctl -u pimonitor -n 20
   ```

Because nothing is persisted across restarts (the in-memory history is
rebuilt from scratch), the sparklines will simply start empty again after an
update — there is no database to migrate.

**New configuration options:** upgrades never modify your existing
`config.yaml`. When a release adds settings, compare your file against the
current
[`packaging/pimonitor.example.yaml`](packaging/pimonitor.example.yaml) and
copy over any new keys you want to use. All settings have sensible defaults,
so a config from an older version keeps working unchanged.

**REST API compatibility:** the `/api/v1/...` contract is stable across
updates — a breaking change to an endpoint's JSON shape ships as
`/api/v2/...` instead, so existing integrations (e.g. openHAB) keep working
after an upgrade. See [`docs/API.md`](docs/API.md).

## REST API

See [`docs/API.md`](docs/API.md) for the full contract, including an
example openHAB HTTP Binding configuration. Quick example:

```sh
curl -s http://raspberrypi.local:8080/api/v1/metrics | jq '.cpu.overall_percent'
```

## Configuration

All settings have sensible defaults; override via `/etc/pimonitor/config.yaml`
(see [`packaging/pimonitor.example.yaml`](packaging/pimonitor.example.yaml))
or CLI flags (`-listen`, `-log-level`, `-api-key`, `-config`, `-version`).
Flags take precedence over the config file, which takes precedence over
built-in defaults.

## Development

See [`CLAUDE.md`](CLAUDE.md) for build/test conventions and project
structure notes. Contributions are welcome - see the issue and pull
request templates under `.github/` for what to include.

## License

[MIT](LICENSE.md)
