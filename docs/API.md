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
    "pi_model": "Raspberry Pi 4 Model B Rev 1.4"
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

Returns the retained in-memory history (a rolling window, typically the
last 30-60 minutes) for every time-series metric. History is **not**
persisted across service restarts.

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

### `GET /api/v1/config`

Returns non-sensitive runtime configuration, so the web dashboard (or a
third-party client) doesn't need to hardcode values separately from the
server:

```json
{
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
