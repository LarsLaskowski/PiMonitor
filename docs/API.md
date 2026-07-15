# PiMonitor REST API

PiMonitor exposes a small, versioned REST API under `/api/v1/...` intended
both for its own web dashboard and for third-party consumers (e.g. home
automation systems such as openHAB, Home Assistant, or Node-RED).

Breaking changes to an existing version's response shape will not happen
in place — a new `/api/v2/...` path would be introduced instead, so
existing integrations against `/api/v1/...` keep working.

## Authentication

By default, no authentication is required — PiMonitor is meant to run on a
trusted local network. If you set `api_key` in `config.yaml` (or the
`-api-key` flag), every `/api/v1/...` request must include one of:

- `Authorization: Bearer <api_key>`
- `X-Api-Key: <api_key>`

Requests without a valid key receive `401 Unauthorized`. `GET /healthz` is
never gated by the API key, so external health checks keep working
regardless of authentication configuration.

## Endpoints

### `GET /healthz`

Plain-text liveness check, always `200 ok`. Not versioned; intended for
`systemd`/monitoring tooling, not for metric data.

### `GET /api/v1/metrics`

Returns the most recently collected snapshot of every metric. This is the
main endpoint for third-party integrations: poll it on an interval and
extract the fields you need (e.g. via JSONPath in openHAB's HTTP binding).

```json
{
  "timestamp": "2026-07-12T18:32:00Z",
  "uptime_seconds": 372014.5,
  "cpu": {
    "overall_percent": 12.4,
    "per_core_percent": [10.1, 14.8, 11.2, 13.5]
  },
  "load_average": { "load1": 0.42, "load5": 0.38, "load15": 0.31 },
  "cpu_count": 4,
  "temperature": { "zone": "cpu-thermal", "celsius": 48.6 },
  "gpu_temperature": { "celsius": 47.8 },
  "memory": {
    "total_bytes": 4137000000, "available_bytes": 2900000000, "used_percent": 29.9
  },
  "swap": { "total_bytes": 104857600, "used_bytes": 0, "used_percent": 0 },
  "disks": [
    {
      "mountpoint": "/",
      "device": "/dev/root",
      "fstype": "ext4",
      "total_bytes": 31000000000,
      "used_bytes": 8000000000,
      "used_percent": 25.8
    }
  ],
  "network": [
    { "name": "eth0", "rx_bytes_per_sec": 1240.5, "tx_bytes_per_sec": 302.1 }
  ],
  "system": {
    "kernel_version": "6.6.31+rpt-rpi-v8",
    "distribution": "Raspberry Pi OS Bookworm (Debian 12)",
    "pi_model": "Raspberry Pi 4 Model B Rev 1.4",
    "cpu_model": "ARMv8 Processor rev 1 (v8l)"
  },
  "updates": {
    "count": 3,
    "packages": [
      {
        "name": "curl",
        "new_version": "7.88.1-10+deb12u5",
        "old_version": "7.88.1-10+deb12u4",
        "arch": "arm64"
      }
    ],
    "cache_age_seconds": 1800,
    "stale": false,
    "checked_at": "2026-07-12T18:20:00Z"
  }
}
```

Notes:

- `timestamp` is the Pi's own clock at collection time (useful as the
  displayed device time), and `uptime_seconds` is the time elapsed since
  boot.
- `system.cpu_model` is best-effort: it is empty on kernels whose
  `/proc/cpuinfo` omits a `model name` field (common on some Raspberry Pi
  kernels).
- `disks[].used_percent` follows `df`'s semantics: it is computed as
  `used / (used + available)`, where `available` counts only blocks
  writable by unprivileged processes. Blocks reserved for root (typically
  5% on ext4) therefore count as used capacity, and the value reaches 100
  when services can no longer write — matching `df`'s `Use%` rather than a
  raw `used / total` ratio (which would still read ~95% on a full ext4
  filesystem). `total_bytes` and `used_bytes` remain the raw filesystem
  totals, so `used_percent` can slightly exceed
  `used_bytes / total_bytes * 100`.
- `disks` contains at most one entry per mountpoint (the filesystem
  actually visible at that path when a mountpoint is overmounted), and
  network filesystems (NFS, CIFS/SMB, SSHFS, ...) are excluded — only
  local storage is reported.
- `network` entries are sorted by interface name.
- `gpu_temperature` is only present if `vcgencmd` is installed and
  responded successfully; otherwise the field is omitted.
- `network` is omitted entirely when network monitoring is disabled
  (`network_enabled: false`).
- `updates.stale` is `true` when the underlying apt cache (refreshed by a
  separate root-privileged systemd timer, not by this process) is older
  than the configured staleness threshold — treat the update count as
  possibly outdated when this is set.
- Fields may read as zero values (`0`, `""`, empty arrays) briefly after
  process start, before the first collection tick completes, or
  permanently on non-Pi/non-Linux systems for hardware-specific fields
  like `temperature` or `pi_model`.

### `GET /api/v1/metrics/history`

Returns the retained history (a rolling window, typically the last 30-60
minutes) for every time-series metric. When history persistence is enabled
(`history_persist_enabled`, on by default), history is periodically
snapshotted to disk and restored on startup, so the returned window may
span service restarts and reboots; points older than the configured window
are dropped on restore. With persistence disabled, history is in-memory
only and starts empty after every restart.

```json
{
  "cpu_percent": [{ "t": "2026-07-12T18:00:00Z", "v": 10.2 }],
  "load1": [{ "t": "2026-07-12T18:00:00Z", "v": 0.4 }],
  "load5": [{ "t": "2026-07-12T18:00:00Z", "v": 0.38 }],
  "load15": [{ "t": "2026-07-12T18:00:00Z", "v": 0.31 }],
  "temperature": [{ "t": "2026-07-12T18:00:00Z", "v": 48.1 }],
  "memory_used_percent": [{ "t": "2026-07-12T18:00:00Z", "v": 29.9 }],
  "swap_used_percent": [{ "t": "2026-07-12T18:00:00Z", "v": 0 }],
  "disk_used_percent": {
    "/": [{ "t": "2026-07-12T18:00:00Z", "v": 25.8 }]
  },
  "network_rx_bytes_per_sec": {
    "eth0": [{ "t": "2026-07-12T18:00:00Z", "v": 1240.5 }]
  },
  "network_tx_bytes_per_sec": {
    "eth0": [{ "t": "2026-07-12T18:00:00Z", "v": 302.1 }]
  }
}
```

`disk_used_percent`, `network_rx_bytes_per_sec`, and
`network_tx_bytes_per_sec` are keyed by mountpoint/interface name and are
omitted entirely if empty (e.g. network history when monitoring is
disabled).

### `GET /api/v1/alerts`

Returns the server-side alert engine's current per-metric state plus a
rolling list of recent transition events. The engine maps each collected
snapshot against the configured `thresholds` into `ok`/`warn`/`crit` states,
applying a debounce (`alerts.for_seconds`) so a threshold crossing must
persist before it is reported — this suppresses short-lived spikes and
momentary dips. The states mirror the color-coding the dashboard already
shows; the events make sustained crossings actionable (e.g. an openHAB rule
polling this endpoint).

```json
{
  "enabled": true,
  "states": [
    { "metric": "cpu", "level": "ok", "value": 12.4, "since": "2026-07-12T18:00:00Z" },
    { "metric": "disk", "resource": "/", "level": "warn", "value": 82.1, "since": "2026-07-12T18:25:00Z" },
    { "metric": "swap", "level": "ok", "value": 0, "since": "2026-07-12T18:00:00Z" },
    { "metric": "temperature", "level": "crit", "value": 78.5, "since": "2026-07-12T18:30:10Z" }
  ],
  "events": [
    {
      "metric": "disk",
      "resource": "/",
      "kind": "fired",
      "from": "ok",
      "to": "warn",
      "value": 82.1,
      "at": "2026-07-12T18:25:00Z"
    },
    {
      "metric": "temperature",
      "kind": "fired",
      "from": "warn",
      "to": "crit",
      "value": 78.5,
      "at": "2026-07-12T18:30:10Z"
    }
  ]
}
```

Notes:

- `enabled` is `false` (with empty `states`/`events`) when alerting is
  disabled via `alerts.enabled: false`.
- `states` lists one entry per evaluated metric: `cpu`, `temperature`,
  `swap`, and one `disk` entry per mounted filesystem (distinguished by
  `resource`, the mountpoint). `resource` is omitted for non-per-device
  metrics.
- A metric whose collection fails on a given tick is skipped rather than
  evaluated against a bogus zero, so its state is left unchanged (or absent
  if it has never been collected). In particular, on hardware without a
  readable thermal zone (containers, non-Pi dev machines) there is no
  `temperature` entry at all — do not assume every metric is always present.
- A per-filesystem `disk` state is dropped when its mountpoint disappears
  from the sample (e.g. an unplugged USB drive). If that filesystem was
  still alerting, a final synthetic `cleared` event is emitted for it; that
  event's `value` is the last reading before the mount vanished (which may
  still be `>=` a threshold), so a `cleared`/`to: "ok"` event carrying a
  high `value` on an unmount is expected, not a bug.
- `level` is the debounced state actually reported; `value` is the most
  recent reading and `since` is when the current level was entered.
- Each `events` entry is a confirmed transition: `kind` is `fired` when the
  severity increased (e.g. `ok`→`warn`, `warn`→`crit`) and `cleared` when it
  decreased (e.g. `crit`→`ok`). `from`/`to` carry the levels and `at` is the
  transition time. The list is bounded to the most recent transitions and is
  in-memory only (it starts empty after a restart).
- The value cutoffs match the dashboard's card coloring: a level is `crit`
  when `value >= *_crit`, `warn` when `value >= *_warn`, otherwise `ok`.
- The same `fired`/`cleared` transitions can also be pushed to external HTTP
  webhooks (Slack, Discord, Home Assistant, ntfy, ...). This is delivery-only
  and configured under `alerts.webhooks` in the config file — it adds no new
  API endpoint; see [`packaging/pimonitor.example.yaml`](../packaging/pimonitor.example.yaml).

### `GET /api/v1/config`

Returns non-sensitive runtime configuration, so the web dashboard (or a
third-party client) doesn't need to hardcode values separately from the
server:

```json
{
  "version": "1.2.3",
  "poll_interval_seconds": 5,
  "network_enabled": true,
  "thresholds": {
    "temperature_warn_c": 60,
    "temperature_crit_c": 75,
    "cpu_warn_percent": 80,
    "cpu_crit_percent": 95,
    "disk_warn_percent": 80,
    "disk_crit_percent": 95,
    "swap_warn_percent": 50,
    "swap_crit_percent": 90
  }
}
```

Notes:

- `version` is the build-time version of the running binary, injected via
  `-ldflags "-X main.version=..."`. Release builds report the release tag; a
  local build made without version injection reports `dev`. The value may
  include a leading `v` depending on the build path (e.g. a `git describe`
  string like `v1.2.3-5-gabc123`); the dashboard strips that leading `v`
  when it renders the version in its footer.

## Example: polling with curl

```sh
curl -s http://raspberrypi.local:8080/api/v1/metrics | jq '.cpu.overall_percent'
```

With an API key configured:

```sh
curl -s -H "X-Api-Key: $PIMONITOR_API_KEY" \
  http://raspberrypi.local:8080/api/v1/metrics | jq '.temperature.celsius'
```

## Example: openHAB HTTP Binding

A Thing definition polling the temperature every 30 seconds:

```
Thing http:url:pimonitor "PiMonitor" [
    baseURL="http://raspberrypi.local:8080/api/v1/metrics",
    refresh=30
] {
    Channels:
        Type number : temperature "CPU Temperature" [
            stateExtension="temperature/celsius",
            stateTransformation="JSONPATH:$.temperature.celsius"
        ]
}
```
