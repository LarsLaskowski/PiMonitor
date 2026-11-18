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
- The three openHAB metrics have their own feed: each finished openHAB poll (`openhab.poll_interval_seconds`,
  [openHAB](openhab.md#cadence)) is evaluated exactly once, in the order the polls finished, with its `collected_at` as
  the timestamp (the time the poll finished for a `not_installed` poll, which has no `collected_at`), whether or not a
  fast tick runs in between and also when the memory history drops waiting points. Their debounce window is
  `alerts.openhab_for_seconds`, not `alerts.for_seconds`. Fast ticks never re-evaluate the last poll, so they neither
  shorten the window nor produce `openhab_*` events. They exist only with `alerts.enabled` and openHAB polling on
  (`openhab.enabled` not `false`).
- With `openhab.enabled: true` on a host without openHAB the poll is `running_unreachable` or `installed_not_running`,
  so `openhab_down` fires and stays at `crit`.
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
| `openhab_down` | none | `1` when the openHAB poll is `installed_not_running` or `running_unreachable` (any failure of the root call while the process runs included), `0` when it is `ok`; only `ok` and `crit` | none |
| `openhab_rss_percent` | none | resident memory of the openHAB process as a percentage of the Pi's RAM | `thresholds.openhab_rss_warn_percent`, `thresholds.openhab_rss_crit_percent` |
| `openhab_things_offline` | none | number of Things that are not `ONLINE`, leaving out disabled Things (`UNINITIALIZED` with detail `DISABLED`) and, while the reported start level is below 100, `UNKNOWN` ones; `UNKNOWN` ones count when no start level is reported | `thresholds.openhab_things_offline_warn`, `thresholds.openhab_things_offline_crit` |

- No other metric (load average, network, hwmon sensors, GPU/PMIC temperature, updates, processes, the openHAB JVM
  heap, add-ons or Items) is alerted on.
- `disk` has one independent state per mountpoint; all mountpoints share the same pair of thresholds. Which
  filesystems are reported is decided by the collector ([Metrics](metrics.md)).
- `openhab_things_offline` is one count for all Things, not one alert per Thing
  ([0026](../decisions/0026-openhab-things-alert-is-one-count.md)); `value` is the count over all Things of the poll,
  while the card's `Things offline: <n>` counts the at most 10 Things it lists. The Things call needs an openHAB API
  token (`openhab.api_token` or `PIMONITOR_OPENHAB_TOKEN`): openHAB refuses it without one, and then the rule is
  not evaluated ([openHAB](openhab.md)).
- The thresholds are the same `thresholds.*` values the dashboard uses to color its cards. Their defaults and
  valid ranges are in [Configuration](configuration.md).

### Raw level of a reading

- A reading is at or above the crit cutoff when `value >= *_crit`, at or above the warn cutoff when
  `value >= *_warn`. Both comparisons are inclusive.
- A threshold of `0` is reached by every reading, including `0`. There is no per-metric switch to turn alerting
  off; a threshold above any possible reading never triggers.
- The exception are the four openHAB thresholds (`openhab_rss_warn_percent`, `openhab_rss_crit_percent`,
  `openhab_things_offline_warn`, `openhab_things_offline_crit`): a cutoff counts only when it is above `0`, so `0` is
  never reached. With only crit `0` the metric reaches at most `warn`; with only warn `0` it goes `ok` → `crit`
  directly and `crit` → `ok` once below crit is confirmed; with both `0` the metric has no state and no event
  ([0023](../decisions/0023-openhab-alert-thresholds-zero-is-off.md)). `openhab_down` has the cutoff 1 only, so it
  has no `warn` level.

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
- One openHAB poll produces its events in the order `openhab_down`, `openhab_rss_percent`,
  `openhab_things_offline`. `events` lists them in the order they were produced; `at` rises within one metric, but
  between an openHAB event and a fast-tick event it is not guaranteed to rise, because a poll and a fast tick finish
  independently.
- An `openhab_things_offline` event carries `things`: the first 10 counted Things sorted by UID, each with `uid`,
  `label`, `status` and `status_detail`, every string cut to at most 256 bytes at a UTF-8 character boundary; the
  list is empty when none is counted. Other events have no `things`. Thing labels come from openHAB and are untrusted.
- Every event is also handed to webhook delivery ([Notifications](notifications.md)).

### Failed readings and vanished filesystems

- A metric whose reading failed in a sample is skipped for that sample: its level, timers, `value` and `since` stay
  as they were, and no event is produced for it. A failed reading is never treated as a value of `0`, so it can
  never clear an alert. A metric that has never been read successfully since startup (for example `temperature`
  on hardware without a readable sensor) has no state at all. `cpu` has no state until the second sample after
  start, because the first fast tick has no previous CPU sample and counts as no reading.
- An openHAB metric whose figure is missing in a poll is skipped like any failed reading: `openhab_rss_percent`
  without a process or with an unknown RAM total, `openhab_things_offline` when the Things call failed or was refused.
  One that was never evaluated has no state.
- When the poll goes from installed to `not_installed`, every `openhab_*` state is removed in the order
  `openhab_down`, `openhab_rss_percent`, `openhab_things_offline`; one at `warn` or `crit` first produces a `cleared`
  event to `ok` with its last `value` (for the Things rule also its last list) and `at` the time that poll finished.
  After a reinstall the metrics start again at `ok` with fresh timers. Without a finished poll (`pending`), with
  `openhab.enabled: false` or `alerts.enabled: false` there is no `openhab_*` state or event.
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

- [0009](../decisions/0009-first-cpu-tick-is-no-reading.md): the first CPU tick is no reading and is not evaluated.
- [0023](../decisions/0023-openhab-alert-thresholds-zero-is-off.md): `0` turns the four openHAB thresholds off.
- [0024](../decisions/0024-openhab-alerts-evaluated-per-poll.md): the openHAB metrics are evaluated once per poll.
- [0026](../decisions/0026-openhab-things-alert-is-one-count.md): one count for all Things, not one alert per Thing.

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
