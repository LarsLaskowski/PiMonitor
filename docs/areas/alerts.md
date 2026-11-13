# Alerts

## Scope

The server-side alert engine: how each collected sample is compared against the configured warn/critical
thresholds, how a debounce turns sustained threshold crossings into per-metric alert states (`ok`, `warn`,
`crit`), when a `fired` or `cleared` transition event is produced, and what `GET /api/v1/alerts` reports as the
current state. Operators and integrations (openHAB rules, scripts polling the API) rely on it, the dashboard shows
its states as badges, and [Notifications](notifications.md) delivers its events to webhooks.

## Behavior

### When the engine runs

- The engine runs only when `alerts.enabled` is true. The option is read once at startup; changing it needs a
  restart ([Configuration](configuration.md)).
- Every fast collection tick (`poll_interval_seconds`, see [Metrics](metrics.md)) produces one sample, and the
  engine evaluates each sample once, in the order the samples were taken. All times the engine uses — debounce,
  `since`, event `at` — are the timestamps of those samples, not the time of evaluation.
- A sample is evaluated as a whole: a reader of the alert state never sees a sample half applied.
- All alert state lives in memory only. After a restart every metric starts again at `ok` with an empty event
  list; an alert that was active before the restart fires again once its condition has held for the debounce
  window, and no `cleared` event is ever produced for the alert that was active before the restart.

### Evaluated metrics

| `metric` | `resource` | Reading | Thresholds |
| -------- | ---------- | ------- | ---------- |
| `cpu` | none | overall CPU usage, percent | `thresholds.cpu_warn_percent`, `thresholds.cpu_crit_percent` |
| `temperature` | none | CPU temperature, °C | `thresholds.temperature_warn_c`, `thresholds.temperature_crit_c` |
| `memory` | none | RAM used, percent | `thresholds.memory_warn_percent`, `thresholds.memory_crit_percent` |
| `swap` | none | swap used, percent | `thresholds.swap_warn_percent`, `thresholds.swap_crit_percent` |
| `disk` | the mountpoint | filesystem used, percent | `thresholds.disk_warn_percent`, `thresholds.disk_crit_percent` |

- No other metric (load average, network, hwmon sensors, GPU/PMIC temperature, updates, processes) is alerted on.
- `disk` has one independent state per mountpoint; all mountpoints share the same pair of thresholds. Which
  filesystems are reported is decided by the collector ([Metrics](metrics.md)).
- The thresholds are the same `thresholds.*` values the dashboard uses to color its cards. Their defaults and
  valid ranges are in [Configuration](configuration.md).

### Raw level of a reading

- A reading is at or above the crit cutoff when `value >= *_crit`, at or above the warn cutoff when
  `value >= *_warn`. Both comparisons are inclusive.
- A threshold of `0` is reached by every reading, including `0`. There is no per-metric switch to turn alerting
  off; a threshold above any possible reading never triggers.

### Debounce

- The warn cutoff and the crit cutoff are timed independently per metric (and per mountpoint). For each cutoff the
  engine remembers on which side of it the reading is and the timestamp of the first sample on that side; the
  timer restarts whenever the reading crosses that cutoff.
- A side is *confirmed* when the current sample's timestamp is at least `alerts.for_seconds` after the first sample
  on that side. With `alerts.for_seconds: 0` the first sample on a side confirms it.
- Because the cutoffs are timed separately, a reading that stays at or above warn while moving in and out of the
  crit band still confirms warn; the crit band never resets the warn timer.
- Both timers start with the first sample in which the metric is evaluated, so a metric that is already above a
  cutoff at startup is confirmed only after the debounce window.
- The debounce applies in both directions: a short spike never raises the level, and a short dip never lowers it.

### Level transitions

The reported level changes only on a confirmed side, at most once per metric per sample:

| Reported level | Becomes | When |
| -------------- | ------- | ---- |
| `ok` | `crit` | at or above crit is confirmed (directly, without passing through `warn`) |
| `ok` | `warn` | at or above warn is confirmed and at or above crit is not |
| `warn` | `crit` | at or above crit is confirmed |
| `warn` | `ok` | below warn is confirmed |
| `crit` | `ok` | below crit and below warn are both confirmed |
| `crit` | `warn` | below crit is confirmed and below warn is not |

In every other case the level stays as it is.

### Events

- Every level change produces exactly one event with `metric`, `resource` (only for `disk`), `kind`, `from`, `to`,
  `value` (the reading of the sample that confirmed it) and `at` (that sample's timestamp).
- `kind` is `fired` when the severity rises (`ok` → `warn`, `ok` → `crit`, `warn` → `crit`) and `cleared` when it
  falls (`crit` → `warn`, `crit` → `ok`, `warn` → `ok`). A `cleared` event therefore does not always mean "back to
  ok"; `to` carries the new level.
- Within one sample, events are produced in this order: `cpu`, `temperature`, `memory`, `swap`, then `disk` in the
  order the collector lists the filesystems, then the `cleared` events of vanished filesystems (below), among
  which the order is not defined.
- The engine keeps the most recent **100** events; when a new event would exceed that, the oldest is dropped.
- Every event is also handed to webhook delivery ([Notifications](notifications.md)).

### Failed readings and vanished filesystems

- A metric whose reading failed in a sample is skipped for that sample: its level, timers, `value` and `since` stay
  as they were, and no event is produced for it. A failed reading is never treated as a value of `0`, so it can
  never clear an alert. A metric that has never been read successfully since startup (for example `temperature`
  on hardware without a readable sensor) has no state at all.
- When the filesystem reading fails, all `disk` states are left untouched — neither evaluated nor removed.
- When the filesystem reading succeeds, the state of every mountpoint missing from it is removed. If that state was
  `warn` or `crit`, a final `cleared` event to `ok` is produced first, with `value` set to the last reading before
  the mountpoint vanished (which may still be above a threshold) and `at` set to the current sample's timestamp.
  A mountpoint that reappears later starts again at `ok` with fresh timers.

### `GET /api/v1/alerts`

The response shape is defined in [`../API.md`](../API.md#get-apiv1alerts); its fields mean:

- `enabled` is `true` when the engine runs. When `alerts.enabled` is false it is `false` and `states` and
  `events` carry no entries (both are encoded as empty arrays, `[]`).
- `states` holds one entry per metric (and per mountpoint for `disk`) that has been evaluated at least once and
  not removed since, sorted by `metric` and then by `resource`, both ascending. It is empty until the first sample
  has been evaluated.
- In a state, `level` is the debounced level, `value` is the last successfully read value (also while it is not
  yet confirmed), and `since` is the timestamp of the sample in which the current level was entered — for a
  metric that has never changed level, the sample in which it was first evaluated.
- `events` lists the retained events oldest first.
- The response is a point-in-time copy; nothing in it changes after it is produced.

## Limits and errors

- The event list is capped at 100 entries; older events are lost, also for API clients that poll too rarely.
- Nothing is persisted: states, timers and events are lost on restart (see *When the engine runs*).
- The engine itself has no error path visible to clients: an invalid threshold configuration is rejected at
  startup ([Configuration](configuration.md)), and a failed reading only skips the metric for that sample (the
  warning about the failed reading is logged by the collector, see [Metrics](metrics.md)).
- Access control, caching and compression of `GET /api/v1/alerts` follow the server-wide rules in
  [HTTP API](http-api.md).

## Related decisions

None yet.

## Not here

- Delivery of events to webhooks (filters, retries, rate limit): [Notifications](notifications.md).
- The JSON shape of `GET /api/v1/alerts`: [`../API.md`](../API.md#get-apiv1alerts).
- Defaults and valid ranges of `thresholds.*`, `alerts.enabled` and `alerts.for_seconds`:
  [Configuration](configuration.md).
- How the dashboard colors cards from the raw thresholds (not debounced) and shows alert badges and the banner:
  [Dashboard](dashboard.md).
- Which metrics are collected, their sources, units and the fast-tick cadence: [Metrics](metrics.md).
- API key, headers and other rules shared by every endpoint: [HTTP API](http-api.md).

## Open questions

None.
