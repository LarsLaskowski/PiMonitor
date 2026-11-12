# History

## Scope

The short time series PiMonitor keeps for every trended metric: which series exist, how many points each holds, when
series of vanished devices are dropped, and how the history is saved to `history.bin` and restored at startup so it
survives restarts and reboots. The dashboard's sparklines and every client of `GET /api/v1/metrics/history` rely on
it; the operator relies on the file rules.

## Behavior

### Series

- Scalar series, one each: `cpu_percent` (`cpu.overall_percent`), `load1`, `load5`, `load15`, `temperature`
  (`temperature.celsius`), `memory_used_percent`, `swap_used_percent`.
- Per-device series, one per device key: `disk_used_percent` per mountpoint, `disk_io_read_bytes_per_sec` and
  `disk_io_write_bytes_per_sec` per block device, `network_rx_bytes_per_sec` and `network_tx_bytes_per_sec` per
  interface.
- A point is a timestamp and a value, in the unit of the metric it is taken from ([Metrics](metrics.md)). Points in
  a series are ordered oldest first.
- Per-core CPU, CPU frequency, GPU and PMIC temperatures, hwmon sensors, wireless, throttling, updates, Pi-hole and
  processes have no history.

### Recording

- Every fast tick adds a point, timestamped with the tick's `timestamp`, to each scalar series whose metric was
  read in that tick. A failed collection adds no point to the series it feeds, while the other series of the tick
  still get theirs, and the snapshot keeps its 0 for the metric ([Metrics](metrics.md#limits-and-errors)):
  `cpu_percent` when the CPU statistics cannot be read, `load1`, `load5` and `load15` when the load average
  cannot, `temperature` when there is no CPU thermal zone or it cannot be read, `memory_used_percent` and
  `swap_used_percent` when `/proc/meminfo` cannot. A host without a thermal zone therefore has an empty
  `temperature` series, and series can differ in length. `cpu_percent` on the first tick after start is not a
  failure and is still recorded as 0.
- A per-device series gets a point only on ticks whose snapshot lists that device. Its series is created the first
  time the device appears. So there is no network history while `network_enabled` is off, and no disk I/O or
  network point on the first tick after start.
- All points of one tick are recorded together: a reader of the history never sees a partly recorded tick.

### Retention and capacity

- Each series holds at most *capacity* points: `history_window_minutes` × 60 / `poll_interval_seconds`, rounded
  down, and at least 1. When a series is full, adding a point drops its oldest one.
- Configuration validation rejects a combination whose capacity would exceed **1,000,000** points per series, so
  the service does not start with it ([Configuration](configuration.md)).
- Retention while running is by count, not by age: with ticks on time the series cover `history_window_minutes`;
  when ticks run late or are dropped ([Metrics](metrics.md)), the same number of points spans a longer time.
- History lives in memory. Without persistence it starts empty at every start.

### Series of vanished devices

- A per-device series is deleted when its device is missing from the current tick and its newest point is older
  than capacity × `poll_interval_seconds` (the history window). A device missing for a single tick, or for less
  than that window, keeps its series, with a gap where it was missing.
- Until it is deleted, the series of a vanished device stays in the history with its old points.

### Persistence: when it is written

- Persistence is on when `history_persist_enabled` is set; the file is `<data_dir>/history.bin`. With persistence
  off the file is neither read nor written, and an existing one is left untouched.
- The history is written after every slow tick that follows start-up (not after the one run at start), so the
  first write happens `updates_check_minutes` after start. It is written once more on a clean shutdown.
- The final write on shutdown does not skip when a slow-tick write is still running: it waits for that write to
  finish and then writes the history as it is at that moment, so the file holds the latest state.
- A write captures the history as it is at that moment, then runs in the background, so collection never waits for
  the disk. A write that falls due while the previous one is still running is skipped, not queued.
- On shutdown the process waits for the running write and the final one only until the shutdown deadline it shares
  with the HTTP server's graceful shutdown ([HTTP API](http-api.md)); a write still running then is abandoned
  (the process logs that the final history write did not finish), and the atomic replacement below keeps the
  previous file intact.
- An unclean stop (crash, power cut) loses the points recorded since the last completed write, at most about
  `updates_check_minutes` of them.

### Persistence: how it is written

- The file is replaced atomically: the data goes to a temporary file `history.bin.tmp-*` in the same directory,
  which is flushed to disk and then renamed over `history.bin`; the directory is then flushed too, on a best-effort
  basis (a failure there is ignored, since the new file is already in place). A crash at
  any point leaves either the previous file or the new one, never a partial one (implemented in
  `internal/collector/persist.go`).
- The directory is created with mode `0750` when it does not exist. The file gets mode `0600`, owned by the service
  user.

### File format

The format is `PIMH` version 1. All integers are little-endian.

| Field | Type | Meaning |
| ----- | ---- | ------- |
| magic | 4 bytes | ASCII `PIMH` |
| version | uint16 | `1` |
| series count | uint32 | number of series records that follow |
| per series: kind | uint8 | series kind, see below |
| per series: key length, key | uint16, bytes | mountpoint, block device or interface name; empty for scalar series |
| per series: point count | uint32 | number of points that follow |
| per point: timestamp | int64 | Unix time in milliseconds |
| per point: value | float64 | IEEE 754 bits |

- Series kinds, fixed forever: 0 `cpu_percent`, 1 `load1`, 2 `load5`, 3 `load15`, 4 `temperature`,
  5 `memory_used_percent`, 6 `swap_used_percent`, 7 `disk_used_percent`, 8 `network_rx_bytes_per_sec`,
  9 `network_tx_bytes_per_sec`, 10 `disk_io_read_bytes_per_sec`, 11 `disk_io_write_bytes_per_sec`. A new series gets
  a new number; a number is never reused or renumbered.
- Scalar series are written first, in kind order, then the per-device series of each kind with keys sorted, so the
  same history always encodes to the same bytes. A device key longer than 4,096 bytes is not written.
- Timestamps are stored to the millisecond; restored points carry millisecond precision.
- A reader skips series of a kind it does not know, so adding a kind needs no version change. Any other change of
  the layout raises the version; there is no migration, an older file is simply discarded.

### Restore on startup

- At start, before the first tick, the service reads `history.bin` and replaces the empty history with it.
- Points older than `history_window_minutes` before the current time are dropped; then each series keeps at most
  its newest *capacity* points, by the current configuration. A per-device series left without points is not
  restored.
- A restored per-device series is subject to the vanished-device rule like any other; restored points continue to
  be served until they leave the window.

## Limits and errors

- A missing `history.bin` is normal and silent; the history starts empty.
- An unreadable file, or one that is not valid, is logged as a warning and ignored: the service starts with empty
  history and overwrites the file at the next write. A file is not valid when its magic is not `PIMH`, its version is
  not 1, it declares more than **4,096** series, a key longer than **4,096** bytes or more than **2,097,152** points
  in one series, it ends early, or it has bytes after the last series. These limits are checked before anything is
  allocated for them.
- A failed write is logged as a warning and retried at the next write; collection and the HTTP API are not affected.
- A temporary file left behind by a crash or power cut during a write is not cleaned up.
- None of these failures stops the service.

## Related decisions

- [0003](../decisions/0003-failed-reading-leaves-history-gap.md): a failed reading leaves a gap in the history.
- [0004](../decisions/0004-final-history-write-waits.md): the final history write waits for a running write.

## Not here

- The JSON shape of `GET /api/v1/metrics/history` and the `?since=` parameter:
  [`API.md`](../API.md#get-apiv1metricshistory).
- What each metric measures, its source and unit: [Metrics](metrics.md).
- How the dashboard merges and re-syncs history: [Dashboard](dashboard.md).
- Defaults and valid ranges of `history_window_minutes`, `poll_interval_seconds`, `updates_check_minutes`,
  `history_persist_enabled` and `data_dir`: [Configuration](configuration.md).
- The service's data directory, its creation and write permission under systemd: [Deployment](deployment.md).
