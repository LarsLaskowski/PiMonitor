# PiMonitor REST API

PiMonitor exposes a small, versioned REST API under `/api/v1/...` intended
both for its own web dashboard and for third-party consumers (e.g. home
automation systems such as openHAB, Home Assistant, or Node-RED).

A `/api/v1/...` response may gain keys: clients must ignore keys they do not
know. Removing, renaming or retyping a key, or changing its meaning or when it
is present, will not happen in place — a new `/api/v2/...` path would be
introduced instead, so existing integrations against `/api/v1/...` keep
working. The rule is stated in
[HTTP API](areas/http-api.md#versioning-and-stability).

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
`Authorization` and `X-Api-Key`, plus `Accept-Encoding` when the response
is compressed. Metric/alert/
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

## Errors

Any `/api/v1/...` JSON endpoint answers `500 Internal Server Error` with the plain-text body `internal error` when its
response cannot be encoded. Clients treat it as a server error.

Every method other than `GET` and `HEAD` on a registered route — every `/api/v1/...` route, `/metrics` when
`prometheus_enabled` is set, and `/healthz` — answers `405 Method Not Allowed` with `Allow: GET, HEAD` and the
plain-text body `method not allowed`. On a route behind `apiRoute` the same layers apply as for `GET`, so a missing or
wrong API key gets `401` and a full in-flight limit gets `503` before the `405`. A path that is not a registered route,
or a route switched off by its option, keeps answering `404`.

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
  "pmic_temperature": { "celsius": 52.1 },
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
  "wireless": [
    { "interface": "wlan0", "link_quality": 70, "signal_dbm": -40 }
  ],
  "sensors": [
    { "chip": "cpu_thermal", "label": "cpu_thermal temp1", "hwmon": "hwmon0", "celsius": 48.6 },
    { "chip": "nvme", "label": "Composite", "hwmon": "hwmon1", "celsius": 34.9 }
  ],
  "system": {
    "hostname": "raspberrypi",
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
    "checked_at": "2026-07-12T18:20:00Z",
    "check_status": "ok"
  },
  "pihole": {
    "is_installed": true,
    "is_update_available": true,
    "components": [
      {
        "name": "core",
        "installed_version": "v6.5",
        "latest_version": "v6.6",
        "branch": "master",
        "is_update_available": true
      },
      {
        "name": "web",
        "installed_version": "v6.6.1",
        "latest_version": "v6.6.1",
        "branch": "master",
        "is_update_available": false
      },
      {
        "name": "ftl",
        "installed_version": "v6.8",
        "latest_version": "v6.8",
        "branch": "master",
        "is_update_available": false
      }
    ],
    "cache_age_seconds": 64800,
    "checked_at": "2026-07-12T18:20:00Z"
  }
}
```

Notes:

- `timestamp` is the Pi's own clock at collection time (useful as the
  displayed device time), and `uptime_seconds` is the time elapsed since
  boot; it reads 0 after a failed `/proc/uptime` read (a non-finite value included).
- `cpu.overall_percent` reads 0 on the first fast tick after start and after a failed `/proc/stat` read, which in v1
  is indistinguishable from a real 0 (`GET /metrics` leaves the CPU families out for such a tick instead).
- After a failed `/proc/meminfo` read (one that fails, lacks a field the kernel always writes or holds a value out of range,
  [Metrics](areas/metrics.md#memory-and-swap)) every `memory` and `swap` figure reads 0, which in v1 is indistinguishable from
  a real 0 (`GET /metrics` leaves the memory and swap families out for such a tick instead).
- After a failed `/proc/loadavg` read (a non-finite value included) every `load_average` figure reads 0, which in v1
  is indistinguishable from a real 0.
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
  actually visible at that path when a mountpoint is overmounted) and
  per filesystem (a filesystem mounted at several mountpoints appears
  once, at its shortest mountpoint), and
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
- `wireless` reports link quality and signal level per wireless interface,
  parsed from `/proc/net/wireless`, and is omitted entirely when the host has
  no wireless interface with a current reading (this includes a host with no
  wireless hardware at all: the file is usually present regardless — created
  by kernels built with wireless extensions support, which is the common
  case — but then simply lists no interfaces, or lists one with no reading
  yet, e.g. not currently associated to a network). It is also omitted when
  `/proc/net/wireless` cannot be read or holds a malformed or non-finite value.
  `link_quality` is the raw driver-reported value from the file's `link`
  column - its maximum is driver-dependent (commonly, but not always, 70) so
  it is not normalized to a 0-100 percentage. `signal_dbm` is the received
  signal strength in dBm on drivers that report an absolute value; some
  drivers instead report a driver-relative signal quality number in this
  same field. More negative (or lower) means weaker signal either way.
- `sensors` enumerates every temperature channel exposed by the kernel's
  hwmon subsystem (`/sys/class/hwmon/hwmon*/temp*_input`) — the SoC sensor
  itself, plus, depending on the board and attached hardware, a PoE-HAT fan
  controller, NVMe/SSD drives, or user-attached I2C/1-Wire sensors. This is
  additive breadth alongside `temperature` above, not a replacement for it:
  `temperature` remains the primary, clearly-labelled CPU/SoC reading used
  by the dashboard gauge and the alert engine. `chip` is the hwmon driver's
  name (e.g. `cpu_thermal`, `nvme`); `label` is the sysfs `temp*_label` file
  when present, otherwise `chip` plus the channel index. `hwmon` is the
  sysfs directory the reading came from (e.g. `hwmon0`), included because
  `chip`/`label` alone cannot distinguish two identical chips (e.g. two NVMe
  drives, both named `nvme` with a `Composite` channel) — but note that
  hwmon numbering is not guaranteed stable across reboots, so `hwmon`
  identifies a sensor only within a single boot, not across restarts. The
  array is omitted entirely when no hwmon sensor is found (this includes
  hosts without `/sys/class/hwmon` at all) or when disabled via
  `hwmon_enabled: false`.
- `cpu_frequency` is one entry per CPU core with a readable sysfs `cpufreq`
  directory (`scaling_cur_freq`, `scaling_governor`), sorted by `core`
  index. It is omitted entirely on systems without a cpufreq driver (e.g.
  many development machines, or a kernel built without `CONFIG_CPU_FREQ`),
  and a core that is offline or whose driver doesn't expose both files is
  simply left out rather than failing the whole reading.
- `gpu_temperature` is only present if `vcgencmd` is installed and
  responded with a finite temperature; otherwise the field is omitted. Note that it is
  not a second physical sensor: CPU and GPU share the SoC die, so this
  reads the same sensor as `temperature`, by a different route.
- `pmic_temperature` is the Power-Management IC's own sensor — genuinely
  separate silicon from the SoC, and useful for spotting power-delivery or
  board-level heat distinct from CPU load. It exists only on the Raspberry
  Pi 4 and 5, is read via `vcgencmd measure_temp pmic`, and is omitted
  (exactly like `gpu_temperature`) whenever `vcgencmd` is unavailable,
  the board has no PMIC sensor, or the answer is not a finite temperature. It is not exposed through sysfs hwmon on
  Raspberry Pi OS, so it never appears in `sensors` either.
- `throttled` decodes the Raspberry Pi `vcgencmd get_throttled` bitmask.
  The `*_now` flags reflect the current state; the `*_since_boot` flags
  latch whether the condition has occurred at any point since boot. A set
  `under_voltage_*` flag usually means an inadequate power supply or cable.
  Like `gpu_temperature`, the whole object is omitted when `vcgencmd` is
  unavailable (e.g. off-Pi), and `raw` carries the original hex bitmask.
- `network` is omitted entirely when network monitoring is disabled
  (`network_enabled: false`). This toggle covers `network` (interface
  throughput) only — `wireless` (link quality/signal level) is unaffected by
  it and has no config toggle of its own; see the `wireless` bullet above for
  when it is omitted instead.
- `updates.stale` is `true` when the underlying apt cache (refreshed by a
  separate root-privileged systemd timer, not by this process) is older
  than the configured staleness threshold — treat the update count as
  possibly outdated when this is set.
- `updates.check_status` is always present: `not_checked` until the first apt
  check has finished after the start, `ok` after a successful check, `failed`
  after a failed one. After a failure the other `updates` keys keep the last
  successful result (a `count` of `0` with `not_checked` or `failed` is not a
  reading); the count means something only while the status is `ok`. `GET
  /metrics` leaves `updates_pending` out instead.
- `pihole` reports a native [Pi-hole](https://pi-hole.net/) installation
  and its own updates. It is always present, and deliberately separate from
  `updates`: Pi-hole is not installed through apt, so its updates are never
  part of `updates.count`.
  - `is_installed` is `true` only for a native Pi-hole **v6 or newer**, i.e.
    when both `/usr/bin/pihole-FTL` and `/etc/pihole/versions` exist. Pi-hole
    v5 and older, and Pi-hole running in Docker, report `false`. When it is
    `false`, only `is_installed`, `is_update_available` (`false`) and
    `checked_at` are sent.
  - `components` holds one entry per component (`core`, `web`, `ftl`). On
    the release branch (`master`) `is_update_available` is `true` when
    `installed_version` and `latest_version` differ; on any other branch the
    installed and latest commit hashes are compared instead, so an update
    there can share the installed version number. A value Pi-hole could not
    determine (it writes the literal `null`, e.g. when its remote check
    failed or was rate-limited, and always for the latest version on
    non-master branches) is treated as unknown: `latest_version` is then
    omitted, and an unknown value never counts as an update. The top-level `is_update_available` is `true` when any
    component has one.
  - PiMonitor only reads `/etc/pihole/versions`, which Pi-hole's own update
    checker rewrites once a day (cron) and after every reboot or `pihole -up`.
    The result can therefore lag a live `pihole -up` by up to about a day:
    `cache_age_seconds` is the file's age as of `checked_at`, and `stale` is
    set (and otherwise omitted) once it is older than 48h, i.e. at least one
    daily refresh was missed. `pihole -up` additionally re-downloads an FTL
    binary whose checksum changed under an unchanged version number; that
    case is not visible in the versions file and is not reported.
- `system.hostname` is the host name read at startup. The key is omitted when
  the name is unknown or `hostname_enabled` is off.
- Fields may read as zero values (`0`, `""`, empty arrays) briefly after
  process start, before the first collection tick completes, or
  permanently on non-Pi/non-Linux systems for hardware-specific fields
  like `temperature` or `pi_model`.
  `system.hostname` may likewise be absent briefly after process start.

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
| `GET /api/v1/metrics/pihole` | the `pihole` object |

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
  `cpu_frequency`, `swap`, `gpu_temperature`, `pmic_temperature`,
  `throttled`, `system`,
  `disk_io`, `wireless`, `sensors`).
  Poll the full snapshot if you need several metrics at once — six narrow
  requests cost more than one full one.
- A field that carries no data is never a `404` — the endpoint exists and
  is answering, and a `404` would be indistinguishable from a misspelled
  path. What it returns instead depends on the field's type, exactly as in
  the full snapshot:
  - The **array-valued** endpoints (`disks`, `network`) return a `200`:
    `GET /api/v1/metrics/network` returns `null` with `network_enabled: false`
    (where the full snapshot omits the key entirely), until the second
    collection tick has completed, and after a failed collection; `GET /api/v1/metrics/disks` returns `[]`
    before the first tick and after a failed collection.
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
minutes; up to `openhab.history_window_hours` for the `openhab_*` keys) for
every time-series metric. When history persistence is enabled
(`history_persist_enabled`, on by default), history is periodically
snapshotted to disk and restored on startup, so the returned window may
span service restarts and reboots; points older than the configured window
are dropped on restore. With persistence disabled, history is in-memory
only and starts empty after every restart.

A scalar series has no point for a tick whose reading failed, so the series
may differ in length or be empty (`temperature` on a host without a thermal
zone). `cpu_percent` also has no point for the first fast tick after start,
because no previous sample exists yet; the shape is unchanged and each of the seven scalar keys (`cpu_percent`, `load1`, `load5`, `load15`, `temperature`, `memory_used_percent`, `swap_used_percent`) is always present.

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
  },
  "openhab_rss_bytes": [{ "t": "2026-07-12T18:00:00Z", "v": 1417166848 }],
  "openhab_heap_used_bytes": [{ "t": "2026-07-12T18:00:00Z", "v": 664407376 }],
  "openhab_restarts": [{ "t": "2026-07-12T09:14:00Z", "v": 938 }]
}
```

`disk_used_percent`, `disk_io_read_bytes_per_sec`,
`disk_io_write_bytes_per_sec`, `network_rx_bytes_per_sec`, and
`network_tx_bytes_per_sec` are keyed by mountpoint/device/interface name and
are omitted entirely if empty (e.g. network history when monitoring is
disabled).

`openhab_rss_bytes` (resident memory of the openHAB process, bytes), `openhab_heap_used_bytes` (JVM heap in use,
bytes) and `openhab_restarts` (the PID of each new openHAB process, one point per restart) are present only while
their series has a point, and never while `openhab.enabled` is `false`; a client must not assume them. They get one
point per openHAB poll (every `openhab.poll_interval_seconds`, 60 s by default), not per fast tick, and hold up to
`openhab.history_window_hours` (24 by default, at most 168) of points, so up to 10,080 per key, independently of
`history_window_minutes`. A poll that finds no process adds no RSS point and a poll without heap values (no
`api_token`: openHAB refuses systeminfo) adds no heap point, so these series show gaps, never a `0`. A restart point
is added when a poll finds a process whose PID or start time differs from the one found by the latest earlier poll
since PiMonitor started; the first process found after start, and a restart while PiMonitor was down, add none. A
point appears in the response up to one fast interval after its poll. Clients must ignore keys they do not
know; adding keys to a `v1` response is not a breaking change.

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
  whole map once every device is omitted. The three `openhab_*` keys are
  omitted when they have no newer point. Take the newest `t` over every
  series you hold as `since`; every point of every series is then newer than it.
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
    { "metric": "openhab_things_offline", "level": "warn", "value": 2, "since": "2026-07-12T18:20:00Z" },
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
      "metric": "openhab_things_offline",
      "kind": "fired",
      "from": "ok",
      "to": "warn",
      "value": 2,
      "at": "2026-07-12T18:26:00Z",
      "things": [
        { "uid": "hue:0210:bridge:lamp1", "label": "Hall lamp", "status": "OFFLINE", "status_detail": "COMMUNICATION_ERROR" },
        { "uid": "mqtt:topic:broker:door", "label": "Front door", "status": "OFFLINE", "status_detail": "NONE" }
      ]
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
  metrics. With alerts on and openHAB polled, up to three more metrics exist:
  `openhab_down` (value `1` while openHAB is not running or not reachable, `0`
  otherwise; only `ok` and `crit`), `openhab_rss_percent` (the openHAB
  process's resident memory as a percentage of the Pi's RAM) and
  `openhab_things_offline` (the number of Things that are not `ONLINE`,
  disabled ones and, during startup, `UNKNOWN` ones left out). They are absent
  while no openHAB is detected, before the first poll finishes, while both of
  their thresholds are `0`, and as long as their figure has never been available
  (`openhab_things_offline` needs an openHAB API token); a poll whose figure is
  missing leaves an existing state unchanged. Clients must ignore `metric`
  values they do not know. They are evaluated once
  per finished openHAB poll (`openhab.poll_interval_seconds`) with the window
  `alerts.openhab_for_seconds`, not on every fast tick. A threshold of `0` turns
  its cutoff off for the four `thresholds.openhab_*` keys. When openHAB is
  uninstalled, their states are removed, each with a final `cleared` event if it
  was alerting.
- An `openhab_things_offline` event (and no other) carries `things`: up to 10
  of the counted Things sorted by `uid`, each with `uid`, `label`, `status` and
  `status_detail`, every string cut to at most 256 bytes at a UTF-8 character
  boundary; `[]` when none is counted. `value` is the count of all Things of
  the poll. The strings come from openHAB and are untrusted. The list is
  governed by `api_key` like the rest of this endpoint.
- Events are listed in the order they were produced: within one openHAB poll
  `openhab_down`, `openhab_rss_percent`, `openhab_things_offline`. `at` rises
  within one metric, but between an openHAB event and a fast-tick event it is
  not guaranteed to, because a poll and a fast tick finish independently.
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
    "memory_crit_percent": 95,
    "openhab_rss_warn_percent": 50,
    "openhab_rss_crit_percent": 75,
    "openhab_things_offline_warn": 1,
    "openhab_things_offline_crit": 0
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

### `GET /api/v1/processes`

Returns the top-N running processes by CPU usage and by resident memory
(RSS), each ranked independently.

It is **off by default**: set `processes_enabled: true` in the config file
(see [`packaging/pimonitor.example.yaml`](../packaging/pimonitor.example.yaml))
to register the route — walking every `/proc/<pid>` entry is more invasive
than the other metrics (it exposes what's running on the host, not just
aggregate load), so this is a deliberate opt-in, the same pattern as
[`GET /metrics`](#get-metrics-prometheus). Left disabled, `GET
/api/v1/processes` returns `404 Not Found` rather than existing but empty.
`processes_top_n` controls how many entries each ranking returns (default
5).

```json
{
  "by_cpu": [
    { "pid": 1234, "name": "python3", "cpu_percent": 34.2, "rss_bytes": 52428800 },
    { "pid": 5678, "name": "pimonitor", "cpu_percent": 2.1, "rss_bytes": 18874368 }
  ],
  "by_memory": [
    { "pid": 1234, "name": "python3", "cpu_percent": 34.2, "rss_bytes": 52428800 },
    { "pid": 5678, "name": "pimonitor", "cpu_percent": 2.1, "rss_bytes": 18874368 }
  ]
}
```

Notes:

- This is recomputed on the **slow tick** (`updates_check_minutes`), not the
  fast one: walking every `/proc/<pid>` entry is far more expensive than
  reading a single `/proc` file, so doing it at `poll_interval_seconds`'s
  cadence would cost too much on constrained hardware such as a Pi Zero.
  Expect the reported CPU percentages to reflect average usage over that
  slower interval, not an instantaneous reading.
- `cpu_percent` is 0 for every process immediately after startup (there is
  no prior sample yet to compute a delta from); it becomes meaningful from
  the second slow tick onward. `rss_bytes` is accurate immediately.
- `cpu_percent` is normalized against total CPU capacity, the same way
  `cpu.overall_percent` in `GET /api/v1/metrics` is: a process pegging a
  single core on a 4-core Pi is reported around 25%, not 100%, so the two
  values are on a comparable 0-100 scale rather than following `top`'s
  default per-core convention (which can exceed 100% on multi-core
  hardware).
- A process that exits between two slow ticks, or whose `/proc/<pid>` files
  are momentarily unreadable (e.g. a zombie), is silently excluded from
  that tick's rankings rather than causing an error.
- A process ID reused by a different process between two slow ticks is
  detected (via the process's start time) and reported at 0% CPU for that
  tick — the same as a genuinely new process — rather than a bogus delta
  computed against the previous, unrelated process's counters.
- This is deliberately its own endpoint rather than a field of `GET
  /api/v1/metrics`, so the main snapshot's payload size doesn't grow with
  however many processes are running on the host.

### `GET /api/v1/openhab`

Returns the status of the openHAB installation on the same Pi, as of the last finished poll: process memory, JVM
heap, Things, Items, add-ons, version and uptime. Detection, states and sources are described in
[`docs/areas/openhab.md`](areas/openhab.md). The state, process memory, JVM heap, uptime, start level, Thing counts per status, Item and add-on counts and version
are also exported on [`GET /metrics`](#get-metrics-prometheus) as `pimonitor_openhab_*` series.

It is registered unless `openhab.enabled` is `false`; with `false` the route returns `404 Not Found` and no request
to openHAB is ever made. Authentication, gzip, `Cache-Control: no-store` and the shared in-flight limit apply as for
every other `/api/v1/...` endpoint.

```json
{
  "state": "ok",
  "poll_interval_seconds": 60,
  "history_window_hours": 24,
  "api_token_configured": true,
  "collected_at": "2026-11-14T12:00:00Z",
  "version": "5.2.4",
  "start_level": 100,
  "uptime_seconds": 362335,
  "process": { "pid": 938, "rss_bytes": 1417166848, "rss_percent_of_ram": 17.4 },
  "jvm": { "heap_used_bytes": 664407376, "heap_total_bytes": 729808896 },
  "things": {
    "total": 3,
    "by_status": { "ONLINE": 2, "OFFLINE": 1 },
    "not_online": [
      {
        "uid": "hue:device:b1:lamp_2D1",
        "label": "Hall lamp",
        "status": "OFFLINE",
        "status_detail": "NONE",
        "description": "Can't find device"
      }
    ]
  },
  "items_count": 214,
  "addons_installed": 12
}
```

| Field | Type | Meaning |
| ----- | ---- | ------- |
| `state` | string | Always present. `pending` (no poll has finished since startup; no other value follows), `not_installed` (`openhab.enabled` is `auto` and nothing was found), `ok` (the REST root answered as openHAB), `running_unreachable` (the process runs, the root call fails) or `installed_not_running` (no process, the root call fails). |
| `poll_interval_seconds` | number | Always present. The `openhab.poll_interval_seconds` in use. |
| `history_window_hours` | number | Always present. The `openhab.history_window_hours` in use, an integer: how long `openhab_*` history is kept ([history](#get-apiv1metricshistory)). |
| `api_token_configured` | bool | Always present. Whether an API token is set; the token itself is never served. |
| `collected_at` | string | Time the poll handed its values to the history, a moment after its last request (RFC 3339, UTC); the timestamp of its history points. Absent in `pending` and `not_installed`. |
| `version` | string | openHAB's version (`runtimeInfo.version`). |
| `start_level` | number | openHAB's start level; below 100 it is still starting. |
| `uptime_seconds` | number | Seconds since the openHAB process started; openHAB's own value when no process was found or `/proc/uptime` cannot be read or its first field is not a finite number. |
| `process` | object | `pid`, `rss_bytes` and `rss_percent_of_ram` (percent of `memory.total_bytes`, absent when that is 0) of the openHAB Java process with the lowest PID. Present whenever the process is found, also when every REST call fails. |
| `jvm` | object | `heap_used_bytes` (total minus free) and `heap_total_bytes` of the JVM heap. |
| `things` | object | `total` (every Thing), `by_status` (counts for `UNINITIALIZED`, `INITIALIZING`, `UNKNOWN`, `ONLINE`, `OFFLINE`, `REMOVING`, `REMOVED`; other status strings are not counted per status) and `not_online`, always an array: up to 10 Things that are not `ONLINE` and not disabled (`UNINITIALIZED` with detail `DISABLED`), sorted by `uid`, each with `uid`, `label`, `status`, `status_detail` and, when openHAB gives one, `description`. |
| `items_count` | number | Number of Items; no Item state is read or served. |
| `addons_installed` | number | Number of installed add-ons. |
| `refused` | array | Names of the sources openHAB answered with `401` or `403` (`root`, `systeminfo`, `things`, `items`, `addons`, in the order of the calls). Omitted when empty. |

Notes:

- Every value except the four always-present fields is **omitted** when it is not available (its source failed, was
  refused or openHAB does not report it), never `null` or `0`.
- Before the first poll finishes the answer is `{"state":"pending","poll_interval_seconds":60,"history_window_hours":24,"api_token_configured":false}`.
- On a default openHAB 5.2.4 install without an API token, `/rest/systeminfo`, `/rest/things` and `/rest/addons` are
  refused: `start_level`, `jvm`, `things` and `addons_installed` are then absent and `refused` lists `systeminfo`,
  `things` and `addons`; `uptime_seconds` still comes from the process.
- Thing labels, status details and descriptions are openHAB's text; clients must treat them as plain text. With an
  `openhab.api_token` set they are served to everyone who can reach PiMonitor unless `api_key` is set.
- The values come from a poll that runs on its own interval, not per request.

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
    "/api/v1/metrics/pihole": 0,
    "/api/v1/alerts": 5,
    "/api/v1/config": 3,
    "/api/v1/processes": 0,
    "/api/v1/openhab": 0,
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
  has its own key, regardless of `prometheus_enabled`, and so do `/api/v1/processes` and `/api/v1/openhab`
  regardless of their options — the response shape doesn't vary with configuration. The key stays `0` until something
  actually requests that path; a scraper pointed at an instance with the
  endpoint disabled shows up here too, with a matching `4xx` count, since the
  bucket comes from the request path, not from which handler — or the mux's
  `404` fallback — ended up serving the request.
- A request is only counted once its response has been fully written, so a
  call to this endpoint never sees itself reflected in the numbers it
  returns — a following call does.

### `GET /metrics` (Prometheus)

Returns the current snapshot and the openHAB status rendered in the
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
# HELP pimonitor_pihole_update_available Whether a Pi-hole component has an update available (1) or not (0).
# TYPE pimonitor_pihole_update_available gauge
pimonitor_pihole_update_available{component="core"} 1
pimonitor_pihole_update_available{component="web"} 0
pimonitor_pihole_update_available{component="ftl"} 0
# HELP pimonitor_openhab_up Whether the openHAB REST API answered (1) or not (0).
# TYPE pimonitor_openhab_up gauge
pimonitor_openhab_up 1
# HELP pimonitor_openhab_things Number of openHAB Things per status.
# TYPE pimonitor_openhab_things gauge
pimonitor_openhab_things{status="UNINITIALIZED"} 0
pimonitor_openhab_things{status="INITIALIZING"} 0
pimonitor_openhab_things{status="UNKNOWN"} 0
pimonitor_openhab_things{status="ONLINE"} 41
pimonitor_openhab_things{status="OFFLINE"} 1
pimonitor_openhab_things{status="REMOVING"} 0
pimonitor_openhab_things{status="REMOVED"} 0
```

Metrics exposed (all gauges, prefixed `pimonitor_`):

| Metric | Labels | Notes |
| --- | --- | --- |
| `cpu_usage_percent` | — | Overall CPU usage. Deliberately its own unlabeled family rather than a `core="overall"` value inside `cpu_core_usage_percent`, so a naive `sum()`/`avg by (...)` over the per-core family can't silently double-count it. Omitted entirely on the first fast tick after start and after a failed `/proc/stat` read, as for `temperature_celsius`; a real reading of `0` is written |
| `cpu_core_usage_percent` | `core` (0-based index) | Omitted entirely on platforms without per-core data, and on the first fast tick after start and after a failed `/proc/stat` read |
| `temperature_celsius` | `zone` | Omitted entirely — the whole family is skipped — whenever the most recent temperature collection failed (e.g. no readable thermal zone) or hasn't completed yet; a `0` reading is never fabricated for a missing sensor |
| `gpu_temperature_celsius` | — | Only present when `vcgencmd` responded, like `gpu_temperature` in `GET /api/v1/metrics` |
| `pmic_temperature_celsius` | — | Only present when `vcgencmd measure_temp pmic` responded (Raspberry Pi 4/5), like `pmic_temperature` in `GET /api/v1/metrics` |
| `memory_total_bytes`, `memory_available_bytes`, `memory_used_percent` | — | Omitted entirely after a failed `/proc/meminfo` read, as for `cpu_usage_percent` |
| `swap_total_bytes`, `swap_used_bytes`, `swap_used_percent` | — | Omitted together with the memory families after a failed `/proc/meminfo` read; a host without swap writes real `0` values |
| `disk_total_bytes`, `disk_used_bytes`, `disk_used_percent` | `mount` | One series per mounted filesystem, same set as `disks` in `GET /api/v1/metrics` (pseudo-filesystems and network filesystems already excluded) |
| `network_receive_bytes_per_second`, `network_transmit_bytes_per_second` | `iface` | Omitted entirely when network monitoring is disabled (`network_enabled: false`), same as `network` in `GET /api/v1/metrics` |
| `updates_pending` | — | Count of upgradable apt packages. Omitted entirely while `updates.check_status` is not `ok` |
| `pihole_update_available` | `component` (`core`, `web`, `ftl`) | `1` when that Pi-hole component has an update, else `0`; same rules as `pihole` in `GET /api/v1/metrics`. Omitted entirely when Pi-hole is not installed |
| `openhab_up` | — | `1` when the REST root answered (state `ok`), else `0`. All `openhab_*` series are written only in the states `ok`, `running_unreachable` and `installed_not_running` of `GET /api/v1/openhab`; none in `pending`, `not_installed` or when `openhab.enabled` is `false` |
| `openhab_process_running` | — | `1` when the openHAB Java process was found, else `0`; written together with `openhab_up` |
| `openhab_process_resident_bytes` | — | `process.rss_bytes`; omitted when no process was found |
| `openhab_jvm_heap_used_bytes`, `openhab_jvm_heap_total_bytes` | — | `jvm.heap_used_bytes` and `jvm.heap_total_bytes`; omitted together when `jvm` is absent. openHAB reports no heap maximum, so there is none |
| `openhab_uptime_seconds` | — | `uptime_seconds`; omitted when absent |
| `openhab_start_level` | — | `start_level`; omitted when absent |
| `openhab_things` | `status` (`UNINITIALIZED`, `INITIALIZING`, `UNKNOWN`, `ONLINE`, `OFFLINE`, `REMOVING`, `REMOVED`) | Seven series in this order whenever `things` is present, `0` for a status no Thing has; omitted when `things` is absent. A disabled Thing counts as `UNINITIALIZED`. Thing UIDs and labels are never exported |
| `openhab_items` | — | `items_count`; omitted when absent |
| `openhab_addons_installed` | — | `addons_installed`; omitted when absent |
| `openhab_info` | `version` | Value always `1`; omitted when `version` is empty or longer than 64 bytes (counted before escaping; never truncated) |

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
