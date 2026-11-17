# Spec: openHAB memory history in the dashboard

Source: Issue #183
Status: Draft

## Problem

The openHAB card ([Dashboard](../../docs/areas/dashboard.md#rendering)) shows openHAB's resident memory and heap of
the latest poll only. An operator cannot tell a healthy plateau from a slow leak that climbs over hours until the Pi
swaps, the typical failure of openHAB on a Pi. Neither the dashboard nor `GET /api/v1/metrics/history` keeps any
openHAB value over time ([History](../../docs/areas/history.md#series)).

## Scenarios

- S1: Given openHAB runs with an `api_token`, when the operator opens the dashboard, then the openHAB card shows a
  chart of openHAB's resident memory (RSS) and JVM heap used over the last `openhab.history_window_hours`
  (default 24 h), with the scale in the card's byte units.
- S2: Given openHAB is detected, when an integration calls `GET /api/v1/metrics/history`, then the response also holds
  `openhab_rss_bytes`, `openhab_heap_used_bytes` and `openhab_restarts`, one point per openHAB poll (default every
  60 s), not per fast tick.
- S3: Given PiMonitor is running, when openHAB is restarted, then the chart marks the restart and shows a gap for the
  polls that found no process, never a drop to 0.
- S4: Given history persistence is on, when PiMonitor restarts, then the openHAB trend recorded before is shown again.

## Errors and edge cases

- E1: No `api_token` on a default install: `GET /rest/systeminfo` is refused
  ([openHAB](../../docs/areas/openhab.md#sources-and-values)), so no heap point is recorded; the chart shows the RSS
  line only and its legend names only `RSS`.
- E2: A poll that finds no process (openHAB stopped, `hidepid=`) adds no RSS point; a poll without heap values adds no
  heap point. A process hidden while REST answers yields heap points without RSS points.
- E3: The first process found after PiMonitor starts is no restart, and a restart while PiMonitor was down is not
  marked; the restored series shows the downtime as a gap.
- E4: openHAB not installed, never seen in the window, or `openhab.enabled: false`: the three keys are absent and the
  card stays hidden as today. After openHAB is removed, its points stay served until they leave the window.
- E5: `openhab.history_window_hours` outside 1 to 168, or not an integer: startup fails.
- E6: An openHAB poll that finishes while a fast tick is collecting: a client that passes the newest `t` back as
  `?since=` ([`API.md`](../../docs/API.md#incremental-polling-since)) still receives every point of every series.
- E7: Fewer than two openHAB points (just started, or `openhab.poll_interval_seconds` longer than the window, which
  leaves capacity 1): no chart is drawn.
- E8: A `history.bin` without the new series (written by v1.2.0) loads as before; a file with them read by an older
  PiMonitor loses only these series (unknown kinds are skipped).

## Non-functional requirements

- N1: New option `openhab.history_window_hours`, integer, default 24, valid 1 to 168, validated also while
  `openhab.enabled` is `false` like the other `openhab` options.
- N2: Each openHAB series holds at most max(1, ⌊`openhab.history_window_hours` × 3600 /
  `openhab.poll_interval_seconds`⌋) points (1,440 at the defaults, 10,080 at most), independent of
  `history_window_minutes` and `poll_interval_seconds`.
- N3: Compatibility: `history.bin` stays `PIMH` version 1, the new series take new kind numbers; the v1 history
  response only gains keys that are omitted when empty, every existing key is unchanged.
- N4: No extra request to openHAB and no change to the poll cadence; the series are served under `api_key` like the
  rest of the history. The PID in `openhab_restarts` is already served by `GET /api/v1/openhab`.

## Acceptance criteria

- AC1: Each finished openHAB poll whose result has a process adds one point (the process's RSS in bytes) to
  `openhab_rss_bytes`, and one whose result has heap values adds one point (heap used in bytes) to
  `openhab_heap_used_bytes`, both with the same timestamp; a poll without the value, and every fast tick, adds none to
  that series; the poll sends no request beyond those of today (S2, E1, E2, N4).
- AC2: A poll that finds a process whose PID or start time differs from those of the process found by the latest
  earlier poll of the same PiMonitor run adds a point to `openhab_restarts` with the poll's timestamp and the new PID
  as value; the first process found after start adds none (S3, E3).
- AC3: `openhab.history_window_hours` defaults to 24 and accepts the integers 1 to 168; another number fails
  startup with `openhab.history_window_hours must be between 1 and 168 (got <value>)`, a fractional one as the other
  integer options do ([Configuration](../../docs/areas/configuration.md)); each openHAB series holds at most the
  capacity of N2 and drops its oldest point when full (N1, N2, E5, E7).
- AC4: `GET /api/v1/metrics/history` holds each of the three keys, as an array of `{ "t", "v" }` points oldest
  first, exactly when its series has a point (with `?since=`: a point newer than `since`), never while
  `openhab.enabled` is `false`, and every other key exactly as before; the route keeps its `api_key` gating (S2,
  E4, N3, N4).
- AC5: A history request after a poll recorded openHAB points returns them, and every point of every series is
  served to a client that passes the newest `t` back as `?since=`, also when the openHAB poll and a fast tick overlap
  (E6).
- AC6: The three series are written to `history.bin` as new kinds under version 1 and restored at startup, dropping
  points older than `openhab.history_window_hours` and keeping at most the capacity; a file without them loads with
  the other series intact; with `openhab.enabled: false` none is restored (S4, E8, N3).
- AC7: The openHAB card shows a chart exactly while the RSS or the heap series holds at least two points: RSS as the
  primary line and heap used as a second line in another color, a legend naming only the lines drawn (`RSS`,
  `Heap used`), points placed by time from the oldest to the newest point, a y scale from 0 to the largest value shown
  with that value labelled in the card's byte format (`812 MB`, `1.2 GB`), the line broken where two neighboring
  points lie more than 1.5 × `openhab.poll_interval_seconds` apart, and a vertical marker at each restart in that
  range (S1, S3, E1, E2, E7).
- AC8: The dashboard keeps the openHAB series to `openhab.history_window_hours` rather than `history_window_minutes`,
  and its incremental history merge and periodic full re-sync apply to the three keys like to the existing ones (S1,
  S4).

## Out of scope

- A detail view or range buttons for the openHAB chart: the existing ranges (5 min to 1 h) do not fit a 24 h trend.
- Marking restarts that happened while PiMonitor was not running (E3), and a toggle between RSS and heap.
- Alerts or webhooks on openHAB memory growth, and history of other openHAB values (Things, uptime).
- Prometheus: `GET /metrics` keeps exporting the current values only; Prometheus keeps its own history.

## Open questions

- Should there be a dedicated, longer window (`openhab.history_window_hours`)? Answered: yes (Lead). With
  `history_window_minutes` at its default 60 the chart would hold 60 points, an hour, while a leak shows over hours;
  at 60 s, 24 h are 1,440 points per series. The upper bound of 168 h keeps a full history response bounded (N2).
- Does PIMH need a version bump? Answered: no. A reader skips series of a kind it does not know, so adding kinds
  needs no version change (`internal/collector/persist.go`, lines 30–32;
  [History](../../docs/areas/history.md#file-format)); the decode limits (2,097,152 points per series) exceed N2.
- How are restarts recorded? Answered: as the series `openhab_restarts` of AC2, so they reach the API, the
  persistence and the delta polling the same way as the memory points. The start time is compared besides the PID
  because a PID can be reused, which the issue's PID check alone would miss.
- Which keys are present without openHAB? Answered: a key is present exactly when its series has points (AC4), the
  rule of the per-device maps. The issue's author, the repository owner, states that adding keys to the v1 history
  response is additive and needs no v2; earlier keys omitted when empty (`wireless`, `pihole`,
  [0006](../../docs/decisions/0006-publish-host-name-by-default.md)) were added to v1 the same way.
- Heap as a second line or a toggle? Answered: a second line (AC7); one look shows both, and a toggle adds state.
- How does the dashboard learn `openhab.history_window_hours`? Answered: the plan chooses the existing endpoint
  that reports it; the spec fixes only the behavior (AC8).
- Do the issue's claims about today's code hold? Answered: the capacity is `history_window_minutes × 60 / poll_interval_seconds`
  (`internal/config/config.go:364`; the issue omits the × 60) capped by `maxHistoryCapacity` (line 378) — confirmed;
  the openHAB default of 60 s (line 293) — confirmed; restore trims to the window — confirmed; "PIMH v1 supports
  named scalar series" — refuted in wording: a scalar series is identified by its kind with an empty key, which is
  enough. Refuted: the cards have sparklines only for CPU and temperature (`internal/web/assets/index.html:65`,
  `:93`); load and memory have a chart only in the detail view. Found on the way: `drawSparkline` spaces points by
  index, not time (`internal/web/assets/chart.js:33`), so it cannot show a gap; and a fast tick timestamps its points
  when it starts collecting (`internal/collector/collector.go:531`) but records them later (line 624), which is why
  E6 needs a criterion.

## Area documents

- [History](../../docs/areas/history.md): the three series, when each gets a point, their capacity and window, the
  restart rule, the new kinds 12 to 14, restore and the `openhab.enabled: false` case.
- [openHAB](../../docs/areas/openhab.md): that each poll feeds the history, with a link to History.
- [Dashboard](../../docs/areas/dashboard.md): the chart in the openHAB card (AC7) and the openHAB window in the
  browser's history handling.
- [Configuration](../../docs/areas/configuration.md): the row of `openhab.history_window_hours`.
- [HTTP API](../../docs/areas/http-api.md): the answer to its open question for keys omitted when empty; `API.md`
  documents the keys, their sampling interval and the restart points.
