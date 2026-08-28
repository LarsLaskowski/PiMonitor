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
`PIMONITOR_API_KEY` environment variable), every `/api/v1/...` request must
include one of:

- `Authorization: Bearer <api_key>`
- `X-Api-Key: <api_key>`

The key can be supplied three ways, in increasing precedence: `api_key` in
the config file, the `PIMONITOR_API_KEY` environment variable, and the
`-api-key` flag. **Prefer the config file** — `install.sh` restricts it to
mode `640 root:pimonitor`. `PIMONITOR_API_KEY` is the deployment-friendly
alternative (systemd `EnvironmentFile=` pointing at a file with the same
restricted permissions). The `-api-key` flag is for local development only:
command lines are world-readable via `/proc/<pid>/cmdline`, so every local
user on the machine can read a key passed that way.

Requests without a valid key receive `401 Unauthorized`. `GET /healthz` is
never gated by the API key, so external health checks keep working
regardless of authentication configuration.

The bundled web dashboard uses this same API: when an `api_key` is set it
shows an "API key required" prompt on first load, then stores the entered
key in the browser's `localStorage` and sends it as `X-Api-Key` on every
request. Setting a key therefore does not disable the dashboard — each
browser just has to unlock it once.

## Compression

`/api/v1/...` responses are gzip-compressed when the request sends
`Accept-Encoding: gzip` (the response then carries `Content-Encoding: gzip`
and `Vary: Accept-Encoding`); the JSON body is unchanged, only its wire
encoding differs. Requests without that header receive the identity
(uncompressed) response, so existing clients keep working unmodified.

The header is parsed as the q-value list it is (RFC 9110 §12.5.3): an
explicit refusal (`Accept-Encoding: gzip;q=0`) is honoured and yields an
identity response, `Accept-Encoding: *` counts as accepting gzip, and the
deprecated `x-gzip` token is not treated as `gzip`.

## Caching

`/api/v1/...` responses carry `Cache-Control: no-store` and a `Vary` naming
`Authorization` and `X-Api-Key` alongside `Accept-Encoding`. Metric/alert/
config snapshots are point-in-time data with no reuse value, and when
`api_key` is configured they are credential-protected, so a reverse proxy
placed in front of PiMonitor (see "Authentication" above) must not cache
them or serve a response captured with one client's credentials to another.

## Rate limiting

Every `/api/v1/...` endpoint shares a single limit on how many requests may be actively
processing at once. `GET /api/v1/metrics/history` is the expensive one — it can require
copying and re-serialising the whole retained history window — so an unbounded number of
concurrent callers could otherwise starve metric collection on constrained hardware such
as a Raspberry Pi Zero.

A request beyond the limit receives `503 Service Unavailable` with a `Retry-After: 1`
header and a plain-text body, instead of being queued. Clients should treat this the same
as any other transient server error: back off (the `Retry-After` value is in seconds) and
retry. `GET /healthz` is never subject to this limit, so liveness checks keep working even
while the API is shedding load.

## Endpoints

### `GET /healthz`

Plain-text liveness check. Not versioned; intended for `systemd`/monitoring
tooling, not for metric data.

Returns `200 ok` when the latest collected snapshot is fresh. It returns
`503 Service Unavailable` with a plain-text body when the snapshot is older
than `healthz_max_staleness_seconds` (default: 3x `poll_interval_seconds`)
— this catches a stalled collector goroutine that would otherwise leave
`/healthz` reporting healthy while `/api/v1/metrics` silently serves stale
data. Set `healthz_max_staleness_seconds` in the config file to tune the
bound; see [`packaging/pimonitor.example.yaml`](../packaging/pimonitor.example.yaml).

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
  "cpu_frequency": [
    { "core": 0, "mhz": 600, "governor": "ondemand" },
    { "core": 1, "mhz": 1500, "governor": "ondemand" },
    { "core": 2, "mhz": 600, "governor": "ondemand" },
    { "core": 3, "mhz": 1500, "governor": "ondemand" }
  ],
  "load_average": { "load1": 0.42, "load5": 0.38, "load15": 0.31 },
  "cpu_count": 4,
  "temperature": { "zone": "cpu-thermal", "celsius": 48.6 },
  "gpu_temperature": { "celsius": 47.8 },
  "throttled": {
    "under_voltage_now": false,
    "frequency_capped_now": false,
    "throttled_now": false,
    "soft_temp_limit_now": false,
    "under_voltage_since_boot": true,
    "frequency_capped_since_boot": false,
    "throttled_since_boot": true,
    "soft_temp_limit_since_boot": false,
    "raw": "0x50000"
  },
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
- `cpu_frequency` is one entry per CPU core with a readable sysfs `cpufreq`
  directory (`scaling_cur_freq`, `scaling_governor`), sorted by `core`
  index. It is omitted entirely on systems without a cpufreq driver (e.g.
  many development machines, or a kernel built without `CONFIG_CPU_FREQ`),
  and a core that is offline or whose driver doesn't expose both files is
  simply left out rather than failing the whole reading.
- `gpu_temperature` is only present if `vcgencmd` is installed and
  responded successfully; otherwise the field is omitted.
- `throttled` decodes the Raspberry Pi `vcgencmd get_throttled` bitmask.
  The `*_now` flags reflect the current state; the `*_since_boot` flags
  latch whether the condition has occurred at any point since boot. A set
  `under_voltage_*` flag usually means an inadequate power supply or cable.
  Like `gpu_temperature`, the whole object is omitted when `vcgencmd` is
  unavailable (e.g. off-Pi), and `raw` carries the original hex bitmask.
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

#### Incremental polling: `?since=`

A client that already holds the window can ask for only what it hasn't seen
yet, instead of re-downloading (and making the Pi re-serialise) the whole
window on every poll:

```
GET /api/v1/metrics/history?since=2026-07-12T18:31:00Z
```

- `since` is an **RFC 3339** timestamp. Fractional seconds and any UTC
  offset are accepted; a bare local time without an offset is not.
- Only points **strictly newer** than `since` are returned. A point whose
  timestamp is exactly `since` is excluded, so passing back the `t` of the
  newest point you hold returns exactly the points you are missing, with no
  duplicate. Pass that `t` back **verbatim**: timestamps carry
  sub-millisecond precision, and a value rounded to milliseconds (as
  JavaScript's `Date.toISOString()` produces) asks for a point you already
  have and gets it back every time.
- The response has the **same shape** as the full-window response. Scalar
  series are present but may be empty; `disk_used_percent`,
  `network_rx_bytes_per_sec` and `network_tx_bytes_per_sec` are filtered per
  device, and a device with no newer points is omitted entirely — as is the
  whole map once every device is omitted.
- A `since` **newer than every retained point** returns empty series and no
  device maps — not an error.
- A `since` **older than the retained window** returns the full window: the
  server has nothing older to give.
- A `since` the server cannot parse returns **`400 Bad Request`**; it is
  never silently treated as a full-window request.
- **Omitting `since` returns the full window, exactly as before** — the
  parameter is optional and purely additive.

Beware of gaps when appending deltas locally. Points leave the retained
window as new ones arrive, so a client that stops polling for longer than
`history_window_minutes` (a backgrounded tab, a lost connection) will get a
delta that starts *after* the newest point it holds — appending that yields
a series with a silent hole. A restart also replaces the window wholesale
(history is restored from disk). A client that accumulates history should
therefore re-request the full window whenever the returned points do not
continue where its own leave off, and re-sync periodically regardless; the
bundled dashboard does both (`mergeHistory` in `app.js`).

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
    { "metric": "memory", "level": "ok", "value": 45.2, "since": "2026-07-12T18:00:00Z" },
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
  `memory`, `swap`, and one `disk` entry per mounted filesystem (distinguished
  by `resource`, the mountpoint). `resource` is omitted for non-per-device
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
  "history_window_minutes": 60,
  "network_enabled": true,
  "thresholds": {
    "temperature_warn_c": 60,
    "temperature_crit_c": 75,
    "cpu_warn_percent": 80,
    "cpu_crit_percent": 95,
    "disk_warn_percent": 80,
    "disk_crit_percent": 95,
    "swap_warn_percent": 50,
    "swap_crit_percent": 90,
    "memory_warn_percent": 80,
    "memory_crit_percent": 95
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
- `history_window_minutes` is how far back `GET /api/v1/metrics/history`
  retains points. A client accumulating history from `?since=` deltas needs
  it to bound its local window to the same span the server keeps.

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
