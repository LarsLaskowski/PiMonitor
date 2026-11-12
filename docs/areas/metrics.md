# Metrics

## Scope

Which system metrics PiMonitor collects, where each one is read from, its unit, how often it is refreshed, and what
is reported when a source is missing or fails. The snapshot served by `GET /api/v1/metrics` (and the views,
history and exports built from it) relies on these rules; so do the operator reading the journal and integrations
such as openHAB that poll single values.

## Behavior

### Collection cadence

- At startup the service reads system identity (`system`) and the core count (`cpu_count`) once; neither is
  refreshed while the process runs. It then runs one fast tick and one slow tick immediately, and afterwards each
  on its own interval.
- The **fast tick** runs every `poll_interval_seconds` and refreshes `uptime_seconds`, `cpu`, `cpu_frequency`,
  `load_average`, `temperature`, `gpu_temperature`, `pmic_temperature`, `throttled`, `memory`, `swap`, `disks`,
  `disk_io`, `network` (only when `network_enabled`), `wireless` and `sensors` (only when `hwmon_enabled`). The
  sources are read one after another; a failing source never aborts the tick or affects the others.
- `timestamp` is the Pi's own clock when the fast tick started reading. All fields of one fast tick become visible
  together, when the tick completes; a reader never sees a mix of two fast ticks.
- The **slow tick** runs every `updates_check_minutes` and refreshes `updates`, `pihole` and, when
  `processes_enabled`, the top-N process rankings. Each of the three is independent: a failure of one never skips
  the others.
- Fast and slow ticks run one at a time, never in parallel. A tick that falls due while another one is running
  starts late, once that one ends; further ticks of the same kind that fall due meanwhile are dropped, not queued.
  A slow tick therefore delays the fast tick by as long as it runs.
- A non-positive interval (which configuration validation already rejects) is replaced by 1 s with a warning,
  never a crash.
- A fast tick may take longer than instant file reads in two cases: each `vcgencmd` call (at most three per tick)
  is bounded at **5 s**, and a hung `statfs` on a mountpoint at **2 s**. The worst case a healthy tick may run
  over is therefore **17 s** (more when several mountpoints hang at once); the default `/healthz` staleness bound
  accounts for it ([HTTP API](http-api.md)).
- Options named here are defined, with defaults and valid ranges, in [Configuration](configuration.md).

### CPU and load

- `cpu.overall_percent` and `cpu.per_core_percent` are busy time in percent (0–100) between two consecutive fast
  ticks, from the aggregate `cpu` line and the `cpuN` lines of `/proc/stat`: 100 × (1 − Δ(idle + iowait) / Δ(user +
  nice + system + idle + iowait + irq + softirq + steal)), clamped to 0–100, and 0 when no time elapsed.
  `per_core_percent` follows the order of the `cpuN` lines.
- On the first fast tick after start there is no previous sample: `overall_percent` is 0 and `per_core_percent` is
  omitted.
- `cpu_count` is the number of `cpuN` lines in `/proc/stat` at startup.
- `cpu_frequency` has one entry per core with a cpufreq directory under `/sys/devices/system/cpu/cpuN/cpufreq`:
  `mhz` is `scaling_cur_freq` (kHz) / 1000, `governor` is `scaling_governor`. Entries are sorted by `core`. A core
  missing either file is left out; the field is omitted when no core has both.
- `load_average` is the first three fields of `/proc/loadavg` (1, 5 and 15 minutes), unchanged.

### Temperature, sensors and throttling

- `temperature` is the CPU/SoC thermal zone under `/sys/class/thermal/thermal_zone*`. The zone is chosen by its
  `type`, in this priority: `cpu-thermal`, `soc_thermal`, `x86_pkg_temp`; if none matches, the first zone in
  lexical order. `zone` is that type, `celsius` the zone's `temp` file in millidegrees / 1000.
- While no zone is found, detection is retried at most once every **30 s**; a zone that appears later is used
  without a restart. When the chosen zone's file disappears, the zone is detected again under the same throttle.
- `gpu_temperature` is `vcgencmd measure_temp` and `pmic_temperature` is `vcgencmd measure_temp pmic` (output
  `temp=NN.N'C`, in °C). Both are attempted on every fast
  tick, whether or not a thermal zone was read. Each is present only when its call succeeded in that tick; a failure
  affects neither the other reading nor `temperature` and is logged as stated under
  [Limits and errors](#limits-and-errors).
- Once `vcgencmd` answers `measure_temp pmic` with anything other than a temperature (a board without a PMIC
  sensor), the PMIC reading is not attempted again for the life of the process and the same error is reported
  from then on. A failed or timed-out call is
  retried on the next tick.
- `throttled` decodes `vcgencmd get_throttled` (`throttled=0x…`): bits 0–3 are `under_voltage_now`,
  `frequency_capped_now`, `throttled_now`, `soft_temp_limit_now`; bits 16–19 the matching `*_since_boot` flags.
  `raw` is the bitmask text exactly as `vcgencmd` printed it. The object is omitted when `vcgencmd` is not
  available or the call or its parsing failed.
- `vcgencmd` is looked up on `PATH` at startup; while it is not found, the lookup is repeated at most once every
  **30 s**, and once found its path is kept. It runs with a fixed argument list and an empty environment
  (implemented in `internal/collector/vcgencmd.go`).
- `sensors` lists every `temp<N>_input` channel under `/sys/class/hwmon/hwmon*`, in °C (millidegrees / 1000),
  sorted by hwmon directory name (lexical) and then channel number. `chip` is the directory's `name` file,
  `label` the `temp<N>_label` file, or `<chip> temp<N>` when there is none, or `temp<N>` when the chip has no name
  either. `hwmon` is the directory name, stable only within one boot. A channel that cannot be read is left out;
  the field is omitted when no channel is found.

### Memory and swap

- Read from `/proc/meminfo`, whose values are kB; bytes are kB × 1024.
- `memory.total_bytes` is `MemTotal`, `memory.available_bytes` is `MemAvailable`, or `MemFree` when `MemAvailable`
  is missing or 0. `memory.used_percent` is 100 × (1 − available / total), 0 when the total is 0.
- `swap.total_bytes` is `SwapTotal`, `swap.used_bytes` is `SwapTotal − SwapFree` (never below 0),
  `swap.used_percent` is used / total in percent, 0 on a host without swap.

### Filesystems and disk I/O

- `disks` lists the filesystems in `/proc/mounts`, octal escapes (`\040`, `\011`, `\012`, `\134`) decoded. A
  mountpoint listed several times is reported once, with its last entry (the one visible at the path), in the
  order mountpoints first appear.
- These filesystem types are never reported (a fixed list, not configurable): `tmpfs`, `devtmpfs`, `proc`,
  `sysfs`, `cgroup`, `cgroup2`, `overlay`, `debugfs`, `tracefs`, `devpts`, `securityfs`, `pstore`, `bpf`,
  `autofs`, `mqueue`, `hugetlbfs`, `configfs`, `fusectl`, `rpc_pipefs`, `binfmt_misc`, and the network
  filesystems `nfs`, `nfs4`, `cifs`, `smbfs`, `smb3`, `9p`, `fuse.sshfs`, `davfs`, `afs`, `glusterfs`, `ceph`,
  `curlftpfs`.
- Size comes from `statfs`: `total_bytes` = blocks × block size, `used_bytes` = (blocks − free blocks) × block
  size, `used_percent` = used / (used + blocks available to unprivileged users) × 100, as `df` computes it. A
  filesystem of size 0, or whose `statfs` fails, is left out of that tick.
- A `statfs` that does not return within 2 s is abandoned and its mountpoint skipped without being asked again
  for **60 s**.
- `disk_io` is per block device from `/proc/diskstats`: sectors read and written × 512 bytes, divided by the
  seconds between two fast ticks, in bytes/s. Devices whose name starts with `loop` or `ram` are left out, and so
  are partitions of the `sd*`/`hd*`/`vd*`/`xvd*` (letter then digits) and `mmcblk*`/`nvme*n*` (`p` then digits)
  naming schemes. A device seen for the first time, or whose counters went down, is left out of that tick.
  Entries are sorted by device name; on the first fast tick the list is empty.
- `disks` and `disk_io` are always arrays, empty rather than absent.

### Network and wireless

- `network` is per interface from `/proc/net/dev`: received bytes (first counter) and transmitted bytes (ninth
  counter), divided by the seconds between two fast ticks, in bytes/s. The loopback interface `lo` is left out,
  and so is an interface seen for the first time or whose counters went down. Entries are sorted by name. The field
  is omitted on the first fast tick and when `network_enabled` is off (then `/proc/net/dev` is not read).
- `wireless` is per interface from `/proc/net/wireless`: `link_quality` is the raw `link` column (not normalized),
  `signal_dbm` the `level` column. An interface whose line is the kernel's placeholder for "no reading" (both values
  0 and neither marked as updated by a trailing `.`) is left out. Entries are sorted by interface name. The field
  is omitted when no interface has a reading, including when the file does not exist. It does not depend on
  `network_enabled`.

### System identity and uptime

- `uptime_seconds` is the first field of `/proc/uptime`, refreshed every fast tick.
- `system.kernel_version` is the running kernel's release (as `uname -r` prints it), empty off Linux.
- `system.distribution` is `PRETTY_NAME` from `/etc/os-release`, else `ID` and `VERSION_ID` joined by a space, else
  `ID`, else empty. It is empty when `distro_info_enabled` is off.
- `system.pi_model` is `/proc/device-tree/model` without trailing NUL and newline bytes, else the `Model` field of
  `/proc/cpuinfo`, else empty. It is empty when `pi_model_enabled` is off.
- `system.cpu_model` is the first `model name` field of `/proc/cpuinfo`, empty when there is none.

### Available apt updates

- `updates` comes from `apt list --upgradable`, run with a fixed argument list, a **10 s** timeout and only the
  environment `LC_ALL=C`, `LANG=C`, `LANGUAGE=`, `DEBIAN_FRONTEND=noninteractive` and the service's `PATH`. It
  only reads the existing apt cache; PiMonitor never refreshes it ([Deployment](deployment.md)).
- Every output line of the form `name/suite new-version arch [upgradable from: old-version]` becomes one entry of
  `packages`; other lines are ignored. `count` is the number of entries, `checked_at` the time of the check.
- `cache_age_seconds` is the age of the newest file in `/var/lib/apt/lists`. `stale` is true when
  `updates_stale_threshold_minutes` is above 0 and the age exceeds it. When the age cannot be determined (directory
  unreadable or without files), `cache_age_seconds` is 0 and `stale` is false.
- When `apt` fails or times out, `updates` keeps the last successful result. Before the first success it holds zero
  values (`count` 0, `checked_at` the zero time).

### Pi-hole updates

- Pi-hole counts as installed when `/usr/bin/pihole-FTL` and `/etc/pihole/versions` both exist and the file's
  `CORE_VERSION` has a major number of 6 or higher (one leading `v` is ignored; a version that then does not start
  with a digit counts as installed). When either file does not exist, or the major number is below 6, `pihole` reports
  `is_installed: false`, `is_update_available: false` and `checked_at`. Pi-hole is never contacted and no command
  is run.
- At most the first **64 KiB** of the versions file are read. Lines are `KEY=VALUE`; surrounding quotes are removed
  and the literal value `null` means unknown.
- `components` holds `core`, `web` and `ftl`, in that order, each only when its installed version is known. On
  branch `master` or with no branch recorded, a component has an update when its latest version is known and
  differs from the installed one; on any other branch, when both commit hashes are known and differ. The top-level
  `is_update_available` is true when any component has one. Pi-hole updates are never counted in `updates.count`.
- `cache_age_seconds` is the versions file's age (never negative); `stale` is true when it is older than **48 h**.
- When Pi-hole looks installed but its state cannot be read (a permission error, or a versions file without
  `CORE_VERSION`), `pihole` keeps the last successful result.

### Top-N processes

- Collected only when `processes_enabled` is on, on the slow tick, by reading `/proc/<pid>/stat` and
  `/proc/<pid>/status` for every process; served separately from the snapshot ([HTTP API](http-api.md),
  [`GET /api/v1/processes`](../API.md#get-apiv1processes)).
- `name` is the process's `comm`; `rss_bytes` is `VmRSS` (kB × 1024), 0 for a process without one.
- `cpu_percent` is the process's user + system time between two slow ticks (at 100 ticks per second) divided by
  the elapsed time and by `cpu_count` (1 when unknown), in percent of total CPU capacity. It is 0 on the first slow
  tick, for a process new since the previous one, and for a PID that now belongs to a different process (detected
  by its start time).
- `by_cpu` and `by_memory` are ranked independently, highest first, ties by lower PID, each cut to
  `processes_top_n` entries. A process that exits or cannot be read during the walk is left out.
- Both rankings are empty arrays before the first slow tick and while the feature is off. When `/proc` itself
  cannot be listed, the previous rankings are kept.

## Limits and errors

- A failing source never stops the service or the other sources. Its field takes the zero or empty value for that
  tick (fast-tick fields) or keeps the last good value (`updates`, `pihole`, process rankings).
- These failures are logged as a warning on **every** tick they occur: `cpu`, `load_average`, `memory`/`swap`,
  `disks` (only when `/proc/mounts` cannot be read), `disk_io`, `network`, `uptime`, `updates`, processes (only when
  `/proc` cannot be listed), and, once at startup, the core count.
- Failures of the optional hardware sources are logged once: a warning on the first failure and whenever the error
  text changes, debug while the same error repeats, and an info line `<message>: recovered` when the source
  recovers. These are `temperature` (including "no CPU thermal zone detected"), `gpu_temperature`,
  `pmic_temperature`, `throttled`, `wireless` (a missing, unreadable or malformed `/proc/net/wireless`), `sensors`
  (no hwmon chip directory; only with `hwmon_enabled`, nothing is logged when it is off), `cpu_frequency` (no cpufreq
  directory) and `pihole`. A missing `vcgencmd` is reported once by each of the three readings that need it
  (`gpu_temperature`, `pmic_temperature`, `throttled`). A board without a PMIC sensor logs one warning for
  `pmic_temperature` and no recovery line. A `vcgencmd` failure or unparsable-output message carries at most 200
  bytes of the command's own output (counted before quoting, plus `...` when cut), on one line
  ([0002](../decisions/0002-bound-vcgencmd-output-in-errors.md)).
- Not logged at all: a single mountpoint whose `statfs` fails or hangs, a single hwmon channel or cpufreq core that
  cannot be read, cpufreq directories present but no core readable, hwmon chips present but no channel readable, a
  single unreadable process, and system identity values that cannot be determined.
- No reading is invented: a value the source did not deliver is reported as 0, empty or absent, as stated per
  metric above. `GET /api/v1/metrics` cannot tell a 0 caused by a failure from a real 0; see
  [`API.md`](../API.md#get-apiv1metrics).

## Related decisions

- [0001: Optional metric sources log their failures once](../decisions/0001-optional-sources-log-failures-once.md)
- [0002: Bound vcgencmd output in error texts, not the read](../decisions/0002-bound-vcgencmd-output-in-errors.md)

## Not here

- JSON shapes of `GET /api/v1/metrics`, `/api/v1/metrics/<metric>` and `/api/v1/processes`: [`API.md`](../API.md).
- Retained time series, eviction and `history.bin`: [History](history.md).
- Thresholds and when an alert fires: [Alerts](alerts.md).
- Prometheus rendering, `/healthz`, route registration of `/api/v1/processes`: [HTTP API](http-api.md).
- Defaults and valid ranges of `poll_interval_seconds`, `updates_check_minutes`, `network_enabled`,
  `hwmon_enabled` and the other options: [Configuration](configuration.md).
- The root apt-cache refresh timer and the `video` group `vcgencmd` needs: [Deployment](deployment.md).
- Which files and commands the service reads, as a threat model: [`SECURITY.md`](../../SECURITY.md).

## Open questions

- A `/proc/stat` `cpu` line with exactly seven counters passes the parser's length check and then indexes past the
  end of the line, which crashes the process. Real kernels print at least eight counters, but this contradicts
  "never a crash" for malformed input. Tracked in LarsLaskowski/PiMonitor#195.
