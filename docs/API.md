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
`PIMONITOR_API_KEY` environment variable), every request to an endpoint
behind `apiRoute` — every `/api/v1/...` route, plus `GET /metrics` when
`prometheus_enabled` is set (see below) — must include one of:

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

Responses from any endpoint behind `apiRoute` (every `/api/v1/...` route,
plus `GET /metrics`) are gzip-compressed when the request sends
`Accept-Encoding: gzip` (the response then carries `Content-Encoding: gzip`
and `Vary: Accept-Encoding`); the body itself is unchanged, only its wire
encoding differs. Requests without that header receive the identity
(uncompressed) response, so existing clients keep working unmodified. A
Prometheus server sends `Accept-Encoding: gzip` by default, so a scrape of
`GET /metrics` normally takes this path rather than the identity one.

The header is parsed as the q-value list it is (RFC 9110 §12.5.3): an
explicit refusal (`Accept-Encoding: gzip;q=0`) is honoured and yields an
identity response, `Accept-Encoding: *` counts as accepting gzip, and the
deprecated `x-gzip` token is not treated as `gzip`.

## Caching

Responses from any endpoint behind `apiRoute` (every `/api/v1/...` route,
plus `GET /metrics`) carry `Cache-Control: no-store` and a `Vary` naming
`Authorization` and `X-Api-Key` alongside `Accept-Encoding`. Metric/alert/
config snapshots — and the Prometheus rendering of the same snapshot — are
point-in-time data with no reuse value, and when `api_key` is configured
they are credential-protected, so a reverse proxy placed in front of
PiMonitor (see "Authentication" above) must not cache them or serve a
response captured with one client's credentials to another.

## Rate limiting

Every endpoint behind `apiRoute` (every `/api/v1/...` route, plus `GET
/metrics`) shares a single limit on how many requests may be actively
processing at once. `GET /api/v1/metrics/history` is the expensive one — it can require
copying and re-serialising the whole retained history window — so an unbounded number of
concurrent callers could otherwise starve metric collection on constrained hardware such
as a Raspberry Pi Zero.

A request beyond the limit receives `503 Service Unavailable` with a `Retry-After: 1`
header and a plain-text body, instead of being queued. Clients should treat this the same
as any other transient server error: back off (the `Retry-After` value is in seconds) and
retry. `GET /healthz` is never subject to this limit, so liveness checks keep working even
while the API is shedding load.

A Prometheus scrape of `GET /metrics` that lands on this `503` is recorded as a *failed*
scrape (`up == 0` for that target) rather than a gap in an otherwise-successful one — unlike
a JSON client, Prometheus doesn't retry mid-scrape-interval. This is normally a non-issue
(the limit is generous relative to a typical scrape concurrency), but worth knowing if
`/metrics` is scraped from multiple Prometheus instances, or alongside heavy dashboard
polling of `GET /api/v1/metrics/history`, against the same PiMonitor instance.

## Endpoints

### `GET /healthz`

Plain-text liveness check. Not versioned; intended for `systemd`/monitoring
tooling, not for metric data.

Returns `200 ok` when the latest collected snapshot is fresh. It returns
`503 Service Unavailable` with a plain-text body when the snapshot is older
than `healthz_max_staleness_seconds` (default: `3x poll_interval_seconds`
plus a margin for the slowest a single healthy collection cycle may
legitimately take, e.g. a hung `vcgencmd` call) — this catches a stalled
collector goroutine that would otherwise leave `/healthz` reporting healthy
while `/api/v1/metrics` silently serves stale
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
  "disk_io": [
    { "device": "mmcblk0", "read_bytes_per_sec": 20480, "write_bytes_per_sec": 8192 }
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
- `disk_io` is a delta-based reading (sectors × 512 bytes from
  `/proc/diskstats`, see `Documentation/admin-guide/iostats.rst` in the
  kernel source) between two collection ticks, so it reads as an empty array
  on the first tick after process start, before a prior sample exists to
  diff against. Loop and RAM devices are excluded, and so are partitions of
  the usual `sd*`/`hd*`/`vd*`/`xvd*`/`mmcblk*`/`nvme*` naming schemes (e.g.
  `sda1`, `mmcblk0p1`): the kernel already folds a partition's sectors into
  its parent device's own counters, so reporting both would double-count
  the same I/O. Entries are sorted by device name. Unlike `disks`,
  `disk_io` reports per block device, not per mountpoint, so its entries do
  not correspond 1:1 with `disks` — and should not be summed for a host
  total, since not every double-counting case (e.g. LVM/LUKS-mapped
  devices layered over a whole disk) is filtered out.
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

### `GET /api/v1/metrics/<metric>`

Narrow, read-only views of the snapshot above, for integrators that poll a
single value and would rather not fetch and parse the whole thing:

| Endpoint | Body |
| --- | --- |
| `GET /api/v1/metrics/cpu` | the `cpu` object |
| `GET /api/v1/metrics/temperature` | the `temperature` object |
| `GET /api/v1/metrics/memory` | the `memory` object |
| `GET /api/v1/metrics/disks` | the `disks` array |
| `GET /api/v1/metrics/network` | the `network` array |
| `GET /api/v1/metrics/updates` | the `updates` object |

Each endpoint returns **exactly** the correspondingly named field of
`GET /api/v1/metrics` — the same JSON, sliced out, with no wrapper object
and no shape of its own:

```sh
curl -s http://raspberrypi.local:8080/api/v1/metrics/temperature
```

```json
{ "zone": "cpu-thermal", "celsius": 48.1 }
```

Field names, types and units are therefore the ones documented under
[`GET /api/v1/metrics`](#get-apiv1metrics) above, and cannot drift from
them.

Notes:

- These are additive to `v1`: `GET /api/v1/metrics` is unchanged, and
  keeps returning every field, including the ones with no endpoint of
  their own (`timestamp`, `uptime_seconds`, `load_average`, `cpu_count`,
  `cpu_frequency`, `swap`, `gpu_temperature`, `throttled`, `system`,
  `disk_io`).
  Poll the full snapshot if you need several metrics at once — six narrow
  requests cost more than one full one.
- A field that carries no data is never a `404` — the endpoint exists and
  is answering, and a `404` would be indistinguishable from a misspelled
  path. What it returns instead depends on the field's type, exactly as in
  the full snapshot:
  - The **array-valued** endpoints (`disks`, `network`) return `null` with
    a `200`: `GET /api/v1/metrics/network` with `network_enabled: false`
    (where the full snapshot omits the key entirely), and either of them
    before the first collection tick completes.
  - The **object-valued** endpoints degrade to zero values rather than
    `null` — `GET /api/v1/metrics/temperature` returns
    `{"zone": "", "celsius": 0}` on a host with no readable thermal zone,
    the same bytes the `temperature` field of `GET /api/v1/metrics` carries
    there. A `0` from such an endpoint is therefore not distinguishable
    from a genuine reading; see the last note under
    [`GET /api/v1/metrics`](#get-apiv1metrics).
- Any other sub-path (`/api/v1/metrics/cpu/overall`, a typo, ...) is not a
  route and returns `404`.
- Authentication, gzip, `Cache-Control: no-store` and the shared in-flight
  limit apply exactly as they do to every other `/api/v1/...` endpoint, and
  each endpoint gets its own key in
  [`GET /api/v1/serverstats`](#get-apiv1serverstats).

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
  "disk_io_read_bytes_per_sec": {
    "mmcblk0": [{ "t": "2026-07-12T18:00:00Z", "v": 20480 }]
  },
  "disk_io_write_bytes_per_sec": {
    "mmcblk0": [{ "t": "2026-07-12T18:00:00Z", "v": 8192 }]
  },
  "network_rx_bytes_per_sec": {
    "eth0": [{ "t": "2026-07-12T18:00:00Z", "v": 1240.5 }]
  },
  "network_tx_bytes_per_sec": {
    "eth0": [{ "t": "2026-07-12T18:00:00Z", "v": 302.1 }]
  }
}
```

`disk_used_percent`, `disk_io_read_bytes_per_sec`,
`disk_io_write_bytes_per_sec`, `network_rx_bytes_per_sec`, and
`network_tx_bytes_per_sec` are keyed by mountpoint/device/interface name and
are omitted entirely if empty (e.g. network history when monitoring is
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
  `disk_io_read_bytes_per_sec`, `disk_io_write_bytes_per_sec`,
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

### `GET /api/v1/serverstats`

Returns in-memory counters of PiMonitor's own HTTP traffic: total requests
served, broken down by response status class and by route. These are
recorded for every request regardless of the `access_log_enabled` config
setting (see [`packaging/pimonitor.example.yaml`](../packaging/pimonitor.example.yaml)),
so request volume stays visible even with per-request debug logging turned
off.

```json
{
  "total": 143,
  "by_status_class": {
    "1xx": 0,
    "2xx": 140,
    "3xx": 0,
    "4xx": 3,
    "5xx": 0
  },
  "by_route": {
    "/healthz": 12,
    "/metrics": 0,
    "/api/v1/metrics": 100,
    "/api/v1/metrics/history": 20,
    "/api/v1/metrics/cpu": 0,
    "/api/v1/metrics/temperature": 4,
    "/api/v1/metrics/memory": 0,
    "/api/v1/metrics/disks": 0,
    "/api/v1/metrics/network": 0,
    "/api/v1/metrics/updates": 0,
    "/api/v1/alerts": 5,
    "/api/v1/config": 3,
    "/api/v1/serverstats": 1,
    "other-api": 0,
    "static": 2
  }
}
```

Notes:

- Counters are process-lifetime totals, in-memory only: they start at zero
  after every restart and are not persisted.
- `by_route` covers every registered route by exact path; a request to any
  other `/api/v1/...` path is counted under `other-api`, and any other path
  (the dashboard's static assets) under `static` — this keeps the counter
  set a fixed, bounded size regardless of what a client (or a scanner)
  requests. `/metrics` (see [`GET /metrics`](#get-metrics-prometheus)) always
  has its own key, regardless of `prometheus_enabled` — the response shape
  doesn't vary with configuration. The key stays `0` until something
  actually requests that path; a scraper pointed at an instance with the
  endpoint disabled shows up here too, with a matching `4xx` count, since the
  bucket comes from the request path, not from which handler — or the mux's
  `404` fallback — ended up serving the request.
- A request is only counted once its response has been fully written, so a
  call to this endpoint never sees itself reflected in the numbers it
  returns — a following call does.

### `GET /metrics` (Prometheus)

Returns the current snapshot rendered in the
[Prometheus text exposition format](https://prometheus.io/docs/instrumenting/exposition_formats/),
for a Prometheus server to scrape directly instead of polling the JSON
`GET /api/v1/metrics` endpoint. Unlike the rest of this document, this path
is deliberately **not** under `/api/v1/...` — it is a different wire format
entirely, not a versioned JSON contract.

It is **off by default**: set `prometheus_enabled: true` in the config file
(see [`packaging/pimonitor.example.yaml`](../packaging/pimonitor.example.yaml))
to register the route. Left disabled, `GET /metrics` returns `404 Not
Found` rather than existing but empty. When `api_key` is set, `GET /metrics`
honours it exactly like every other endpoint (`Authorization: Bearer` or
`X-Api-Key`) — configure the same value as your Prometheus scrape config's
`authorization`/`bearer_token`.

`GET /metrics` goes through the same middleware chain as every `/api/v1/...`
route (it's registered via the same `apiRoute` wrapper — see "Compression",
"Caching", and "Rate limiting" above), so it is gzip-compressed, marked
`Cache-Control: no-store`, and shares the same concurrency limit. The one
worth calling out here specifically: if that limit is ever hit, the scrape
gets `503 Service Unavailable` rather than a slow response, which Prometheus
records as a failed scrape (`up == 0`) — see "Rate limiting" for when that
can realistically happen.

```
# HELP pimonitor_cpu_usage_percent Overall CPU usage percentage.
# TYPE pimonitor_cpu_usage_percent gauge
pimonitor_cpu_usage_percent 12.4
# HELP pimonitor_cpu_core_usage_percent Per-core CPU usage percentage.
# TYPE pimonitor_cpu_core_usage_percent gauge
pimonitor_cpu_core_usage_percent{core="0"} 10.1
# HELP pimonitor_temperature_celsius CPU temperature in Celsius.
# TYPE pimonitor_temperature_celsius gauge
pimonitor_temperature_celsius{zone="cpu-thermal"} 48.6
# HELP pimonitor_memory_used_percent RAM used percentage.
# TYPE pimonitor_memory_used_percent gauge
pimonitor_memory_used_percent 29.9
# HELP pimonitor_disk_used_percent Filesystem used percentage (df semantics).
# TYPE pimonitor_disk_used_percent gauge
pimonitor_disk_used_percent{mount="/"} 25.8
# HELP pimonitor_network_receive_bytes_per_second Network interface receive throughput in bytes/sec.
# TYPE pimonitor_network_receive_bytes_per_second gauge
pimonitor_network_receive_bytes_per_second{iface="eth0"} 1240.5
# HELP pimonitor_updates_pending Number of upgradable apt packages.
# TYPE pimonitor_updates_pending gauge
pimonitor_updates_pending 3
```

Metrics exposed (all gauges, prefixed `pimonitor_`):

| Metric | Labels | Notes |
| --- | --- | --- |
| `cpu_usage_percent` | — | Overall CPU usage. Deliberately its own unlabeled family rather than a `core="overall"` value inside `cpu_core_usage_percent`, so a naive `sum()`/`avg by (...)` over the per-core family can't silently double-count it |
| `cpu_core_usage_percent` | `core` (0-based index) | Omitted entirely on platforms without per-core data |
| `temperature_celsius` | `zone` | Omitted entirely — the whole family is skipped — whenever the most recent temperature collection failed (e.g. no readable thermal zone) or hasn't completed yet; a `0` reading is never fabricated for a missing sensor |
| `gpu_temperature_celsius` | — | Only present when `vcgencmd` responded, like `gpu_temperature` in `GET /api/v1/metrics` |
| `memory_total_bytes`, `memory_available_bytes`, `memory_used_percent` | — | |
| `swap_total_bytes`, `swap_used_bytes`, `swap_used_percent` | — | |
| `disk_total_bytes`, `disk_used_bytes`, `disk_used_percent` | `mount` | One series per mounted filesystem, same set as `disks` in `GET /api/v1/metrics` (pseudo-filesystems and network filesystems already excluded) |
| `network_receive_bytes_per_second`, `network_transmit_bytes_per_second` | `iface` | Omitted entirely when network monitoring is disabled (`network_enabled: false`), same as `network` in `GET /api/v1/metrics` |
| `updates_pending` | — | Count of upgradable apt packages |

Example `prometheus.yml` scrape config:

```yaml
scrape_configs:
  - job_name: pimonitor
    static_configs:
      - targets: ["raspberrypi.local:8080"]
    # Only needed when api_key is set in PiMonitor's config.
    # authorization:
    #   credentials: "your-api-key"
```

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
            stateTransformation="JSONPATH:$.temperature.celsius"
        ]
}
```

The HTTP binding fetches `baseURL` once per refresh cycle, and each Channel's
`stateTransformation` runs its JSONPath against whatever that Channel's own
request actually returns — `baseURL` alone, or `baseURL` plus that Channel's
`stateExtension` if it sets one. A `stateTransformation` must match that
body: the example above has no `stateExtension`, so its JSONPath is written
against the full snapshot at `baseURL`. An earlier version of this example
got that wrong two ways at once: its `stateExtension="temperature/celsius"`
polled `.../api/v1/metrics/temperature/celsius`, which isn't a route and
returns `404`, so the Channel never gets a value and the Item stays
`NULL`/`UNDEF`; and even a valid `stateExtension` would still have broken
the JSONPath, since it was written for the full-snapshot body, not the
narrower one a sub-resource returns.

Polling the full snapshot like this pays off once a Thing has several
Channels, since they all share one request. For a Thing with only one or two
Channels, set that Channel's `stateExtension` to a
[per-metric sub-resource](#get-apiv1metricsmetric) instead (e.g.
`stateExtension="temperature"`) and write its `stateTransformation` against
that narrower body (`JSONPATH:$.celsius`) — one request per Channel, but
each one smaller. See
[`docs/integrations/openhab.md`](integrations/openhab.md) for a full,
multi-channel example and troubleshooting.
