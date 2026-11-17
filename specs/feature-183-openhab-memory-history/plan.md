# Plan: openHAB memory history in the dashboard

Source: Issue #183 | [spec.md](spec.md)
Tier: standard — new behavior (series, option, API keys, chart) that creates, changes or removes no security control.
Security areas touched: File writes (`history.bin`, `internal/collector/persist.go`: new series kinds, decode limits
unchanged); Parsing of local input (YAML option `openhab.history_window_hours`); HTTP surface (new keys in the
responses of `GET /api/v1/metrics/history` and `GET /api/v1/openhab`, no route or middleware change); Dashboard XSS
(`internal/web/assets/app.js` builds new DOM nodes)

## Problem / root cause

Feature: [spec.md](spec.md), criteria AC1–AC9.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | A fast tick takes its timestamp before collecting and records under `c.mu` later | `internal/collector/collector.go:531`, `:586`, `:624` | confirmed |
| 2 | The openHAB poll runs on its own goroutine, stores its status under its own lock, `collected_at` = `c.now()` at the end of `poll` | `openhab.go:191-216`, `:286-287` | confirmed |
| 3 | `status.Process` is nil whenever `findProcess` fails; `findProcess` reads PID and `startTicks` | `openhab.go:262-272`, `:161-165`, `:321-324` | confirmed |
| 4 | Heap values are set only when systeminfo answers and free ≤ total | `openhab.go:498-506` | confirmed |
| 5 | The history cache refreshes on `HistoryGeneration`, which only `fastTick` increments | `httpapi/handlers.go:101-110`, `collector.go:636` | confirmed |
| 6 | The decoder skips unknown kinds; kinds end at 11; limit 2,097,152 points per series | `persist.go:30-32`, `:48-61`, `:43`, `seriesToHistory` | confirmed |
| 7 | Retention is by count: with no new points old points stay in the buffer | `ringbuffer.go:29`, `docs/areas/history.md` *Retention* | confirmed |
| 8 | `integerOptionNames` must list every int field of `Config` (test enforces) | `config.go:610`, `config_test.go:905-908` | confirmed |
| 9 | The dashboard merges and trims only the keys it lists; `since` is the newest point over the listed keys | `app.js:1024-1030`, `:1055-1060`, `:1100-1148`, `:1168` | confirmed |
| 10 | `drawSparkline` spaces points by index | `chart.js:33` | confirmed |
| 11 | Dashboard assets are outside both coverage measures | `sonar-project.properties` (`sonar.coverage.exclusions=internal/web/assets/**`), `COVERAGE_PATHSPECS = ["*.go"]` | confirmed |
| 12 | Exact JSON pins of the openHAB status that AC9 changes | `collector/openhab_test.go:246`, `:412`; `httpapi/handlers_test.go:1028`; `config_test.go:911` | confirmed |
| 13 | On load the dashboard fetches history before openHAB | `docs/areas/dashboard.md` *Data sources* | confirmed |

Issue claims checked: see the spec's *Open questions*.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestOpenHABCollector_Poll_HistorySample`: "AC1 process and heap give both values at collected_at", "AC1 no process gives no RSS value", "AC1 refused systeminfo gives no heap value", "AC1 not_installed gives no sample", "AC1 requests unchanged" (recorded URIs equal today's list) | `internal/collector/openhab_test.go` |
| AC1 | `TestCollector_FastTick_OpenHABSamples`: "AC1 tick without a waiting sample adds no openHAB point", "AC1 waiting sample recorded once with its collected_at" | `internal/collector/collector_test.go` |
| AC2 | `TestOpenHABCollector_Poll_RestartSample`: "AC2 first process gives no restart", "AC2 same PID and start time give none", "AC2 new PID gives the new PID", "AC2 same PID with new start time gives a restart", "AC2 A then no process then B gives one restart at B's poll" | `internal/collector/openhab_test.go` |
| AC3 | `TestValidate_OpenHABHistoryWindowHours`: 1, 24, 168 accepted; 0, 169, -1 rejected with `openhab.history_window_hours must be between 1 and 168 (got 0)` etc.; `TestLoad_OpenHABHistoryWindowHoursFractional`: `1.5` → `openhab.history_window_hours must be a whole number (got 1.5)`; `TestOpenHAB_HistoryCapacity`: (24, 60) → 1440, (168, 60) → 10080, (1, 86400) → 1; `TestDefault_OpenHAB_AC7` adapted to 24 | `internal/config/config_test.go` |
| AC3 | `TestCollector_OpenHABHistory_Retention`: "AC3 full series drops its oldest point", "AC3 History omits points older than the window before the latest tick" | `internal/collector/collector_test.go` |
| AC3 | `TestCollectorConfig_MapsFields`: rows `OpenHAB.HistoryWindowHours`, `OpenHAB.HistoryCapacity` | `cmd/pimonitor/main_test.go` |
| AC4 | `TestHandleHistory_OpenHABKeys`: "AC4 keys absent while series empty", "AC4 keys present as point arrays", "AC4 since omits a key without newer points", "AC4 other keys unchanged", "AC4 401 without key when api_key is set"; `TestCollector_History_OpenHABDisabled_AC4` (no openHAB field set); `TestHistorySince_FiltersOpenHABSeries_AC4` | `internal/httpapi/handlers_test.go`, `internal/collector/collector_test.go` |
| AC5 | `TestCollector_OpenHABHistory_DeltaSafe`: "AC5 waiting sample invisible before the next tick", "AC5 tick records it and increments the generation", "AC5 Since of the newest point after one tick returns every point of the next", "AC5 sample whose poll ended before tick k started but handed over after tick k's drain reaches a client passing tick k's newest t" (poll clock injected through `OpenHABConfig.Now` for the poll's end, hook called after `fastTick`), "AC5 waiting samples capped at the openHAB capacity without a tick" (capacity + 2 samples, no tick: exactly capacity remain, the two oldest dropped); `TestOpenHABCollector_Poll_CollectedAtFromHook_AC5` (status `collected_at` equals the hook's time) | `internal/collector/collector_test.go` |
| AC6 | `TestHistory_EncodeDecodeRoundTrip_OpenHAB_AC6` (kinds 12, 13, 14 in the bytes); `TestEncodeHistory_NoOpenHABSeriesWritesNoRecord_AC6`; `TestCollector_LoadHistory_OpenHAB`: "AC6 file without openHAB series restores the others", "AC6 drops points older than the openHAB window", "AC6 caps to the openHAB capacity", "AC6 disabled restores none" | `internal/collector/persist_test.go` |
| AC7 | `TestChartJS_TimeChart_AC7` (positions by time, break above `1.5 *` gap, markers, y from 0); `TestAppJS_OpenHABChart_AC7` (legend `RSS`/`Heap used`, `fmtBytes` label, block only with two points, `renderHistory` calls `renderOpenHAB` when the chart signature changes, `textContent` only) — plus manual: Product Manager, on the stamped head built with `make build-arm64` (or `make build-arm`) on a Pi with openHAB: chart shape, legend without token, a break after `systemctl stop openhab` for two polls, one marker after `systemctl restart openhab` | `internal/web/chart_test.go`, `internal/web/openhab_test.go` |
| AC8 | `TestAppJS_HistoryOpenHABKeys_AC8` (`HISTORY_OPENHAB_KEYS` with the three keys, used by `forEachHistorySeries`, `mergeHistory`, `trimHistoryWindow`; trim reads `history_window_hours`) — plus the manual run of AC7 (chart still full after 15 min open) | `internal/web/history_test.go` |
| AC9 | `TestOpenHABCollector_Status_HistoryWindowHours_AC9` (pending, not_installed, ok); pins at `openhab_test.go:246`, `:412` adapted; `TestHandleOpenHAB_HistoryWindowHours_AC9`; pin at `handlers_test.go:1028` adapted | `internal/collector/openhab_test.go`, `internal/httpapi/handlers_test.go` |

## Approach

**Config** (`internal/config/config.go`). `OpenHAB.HistoryWindowHours` (default 24) validated in `OpenHAB.validate`
after the poll interval with the AC3 text (`%v` for the value, like the poll interval); constants
`minOpenHABHistoryHours = 1`, `maxOpenHABHistoryHours = 168`; added to `integerOptionNames` and the shadow struct
of `checkIntegerOptions`. `HistoryCapacity()` = max(1, hours × 3600 / `PollIntervalSeconds`), capped at
`maxHistoryCapacity`; with `PollIntervalSeconds` ≤ 0 it returns 1. `cmd/pimonitor/main.go` `collectorConfig` maps
both into `OpenHABConfig`.

**Poll side** (`openhab.go`). `poll` builds a private `openHABSample` (RSS and heap each with an ok flag; restart
PID, 0 for none). Restart: a private `last` (PID, `startTicks`) touched only by the poll goroutine, compared when a
process is found, updated then, never reset by a poll without a process. `Poll` then calls a private hook
`onSample func(openHABSample) time.Time` (set by `Collector.New`; nil in `newOHCollector`, so a test sets it), not
for `not_installed`; the time it returns becomes the status's `collected_at` (without a hook `collected_at` stays
`c.now()`). Order, with no lock held across the two: first the hook (it takes `c.mu`, reads the clock, appends,
releases), then `Poll` stores the status under `OpenHABCollector.mu`; `Poll` holds no `OpenHABCollector` lock while
calling the hook. `baseStatus` sets `HistoryWindowHours`.

**Recording** ([0020](../../docs/decisions/0020-openhab-points-recorded-by-next-fast-tick.md)). The collector's hook,
holding `c.mu`, reads the clock (`cfg.OpenHAB.Now`, else `time.Now`), stamps the sample with it, appends it to a
private waiting slice capped at `OpenHAB.HistoryCapacity` (the oldest dropped) and returns the time. So every
waiting sample is newer than every tick that drained before it. `fastTick`, inside its locked update before
`historyGen++`, drains the slice into three ring buffers (`OpenHAB.HistoryCapacity`), in a private method so
`fastTick` stays under `gocognit` 15. The buffers exist only with `cfg.OpenHAB.Enabled`.

**Reading.** `History()` adds the three series only with openHAB enabled, each cut by `dropPrefix` to points no older
than `HistoryWindowHours` before `c.latest.Timestamp` (no cut before the first tick); in a private helper.
`Since` applies `dropPrefix` to them. JSON tags with `omitempty` give AC4's absence.

**Persistence.** Kinds `seriesOpenHABRSSBytes` 12, `seriesOpenHABHeapUsedBytes` 13, `seriesOpenHABRestarts` 14,
appended to the `iota` block; `historySeriesList` appends them after the per-device series, each only when it has
points; `seriesToHistory` maps them; `importHistory` fills them with cutoff now − `HistoryWindowHours` when the
buffers exist. Version and limits unchanged.

**Dashboard.** `chart.js` gains `drawTimeChart(canvas, lines, opts)`: `lines` = `[{ points, color }]`, `opts` =
`{ from, to, gapMs, markers, markerColor }` (ms); x by time between `from` and `to`, y from 0 to the largest value
(flat padding as `drawSparkline`), `moveTo` where neighbors are more than `gapMs` apart, a 1 px dashed vertical line
per marker; no fill. `app.js`: `HISTORY_OPENHAB_KEYS` joins `forEachHistorySeries`, `mergeHistory` (concatenated
like scalars) and `trimHistoryWindow` (cutoff newest − `latestOpenHAB.history_window_hours` × 3,600,000; no cut
while unknown). `gapMs` = 1.5 × `latestOpenHAB.poll_interval_seconds` × 1000.

Layout: in `renderOpenHAB`, after the Things row and before the Things-offline toggle, a block
`div.openhab-history` with (1) an `openhabRow('Memory history', fmtBytes(max))`, (2) `canvas.openhab-chart`
(64 px high, full width), (3) `div.openhab-legend`: per drawn line, RSS first, a `span.legend-swatch` with class
`legend-rss` or `legend-heap` and the text `RSS` / `Heap used`. Colors: RSS `--accent`, heap `--muted`, markers
`--warn`, read with `getComputedStyle`. The block exists only while RSS or heap holds two points. It is drawn when
`renderOpenHAB` builds it and from `redrawSparklines` (history poll, theme, layout reveal). `renderHistory` checks
the chart signature (the two-point condition, which lines are drawn and the largest value, `openhabChartSignature`):
when it changes and `latestOpenHAB` holds a rendered state (not `pending`,
`not_installed`), it calls `renderOpenHAB(latestOpenHAB)`, which creates or removes the block; otherwise it only
redraws. Range: oldest to newest
point of RSS and heap; markers: restarts inside it.

## Options considered

- Window: the four options of [0019](../../docs/decisions/0019-openhab-history-has-its-own-window.md).
- Recording: the three options of [0020](../../docs/decisions/0020-openhab-points-recorded-by-next-fast-tick.md).
- v1 keys: the three options of [0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md).
- Restarts as a PID series (`openhab_pid`, one point per poll, change = restart): finds a change after a PiMonitor
  restart, but misses a reused PID after a reboot and triples nothing useful; lost to AC2's restart points.
- Heap as a toggle: adds browser state for no gain (spec).
- Age trim in the ring buffer on every tick: copies the series each time something ages out; the cut in
  `History()` is a binary search on the copy that is made anyway.
- `history_window_hours` on `GET /api/v1/config`: would carry an openHAB value while the feature is off (spec).
- Extending `drawSparkline` with time positions: changes the CPU and temperature sparklines; a separate function
  leaves them as they are.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `OpenHAB`, `config.go` | option, validation, `HistoryCapacity` |
| `cmd/pimonitor` | `collectorConfig`, `main.go` | maps the two values |
| `internal/collector` | `OpenHABConfig`, `OpenHABStatus`, `OpenHABCollector`, `openhab.go` | sample, restart, field |
| `internal/collector` | `History`, `Collector`, `collector.go` | fields, buffers, drain, read cut, `Since` |
| `internal/collector` | `persist.go` | kinds 12–14, list, import |
| `internal/web/assets` | `chart.js`, `app.js`, `style.css` | chart, keys, card block, legend classes |

## Signatures (for the Dev's skeleton)

- `internal/config`: field `HistoryWindowHours int` on `OpenHAB` (YAML key history_window_hours);
  `func (o OpenHAB) HistoryCapacity() int`.
- `internal/collector`, `OpenHABConfig`: fields `HistoryWindowHours int` and `HistoryCapacity int`.
- `internal/collector`, `OpenHABStatus`: field `HistoryWindowHours int` (JSON key history_window_hours, no
  omitempty), placed directly after the poll interval field.
- `internal/collector`, `History`, after the network transmit field: `OpenHABRSSBytes []HistoryPoint`,
  `OpenHABHeapUsedBytes []HistoryPoint`, `OpenHABRestarts []HistoryPoint`, with the JSON keys openhab_rss_bytes,
  openhab_heap_used_bytes and openhab_restarts, each with omitempty.

No file is rewritten whole; no signature changes.

## Test files

`internal/config/config_test.go`, `cmd/pimonitor/main_test.go`, `internal/collector/openhab_test.go`,
`internal/collector/collector_test.go`, `internal/collector/persist_test.go`, `internal/httpapi/handlers_test.go`,
`internal/web/chart_test.go` (new, for `chart.js`), `internal/web/openhab_test.go`, `internal/web/history_test.go`.

Existing tests whose input a criterion handles differently, adapted by the Tester in step 5: `TestDefault_OpenHAB_AC7`
(AC3), `TestCollectorConfig_MapsFields` (AC3), the JSON pins of fact 12 (AC9).

Files no unit test can cover: `internal/web/assets/chart.js`, `app.js`, `style.css` — browser code without a JS
test runner; already outside both coverage measures (fact 11), so no new exclusion and no record.

## Areas

- History: series, recording, retention, file format, restore (details below).
- openHAB, Dashboard, Configuration, HTTP API: as below.

## Documentation updates

All by the Dev after the code, except where noted.

- `docs/areas/history.md`: *Series* (three series, units, key names); *Recording* (sample waits for the next fast
  tick, `collected_at`, restart rule with E3/E9); *Retention* (openHAB capacity, cut by age on read); *File format*
  (kinds 12–14, written after the per-device series and only when non-empty); *Restore* (openHAB window, disabled);
  *Related decisions* 0019, 0020; *Not here* adds `openhab.history_window_hours`.
- `docs/areas/openhab.md`: *Cadence*: each poll feeds the history ([History](../../docs/areas/history.md));
  `collected_at` is taken when the finished poll hands its values to the history (also `API.md`'s field row).
- `docs/areas/dashboard.md`: *Data sources* (`history_window_hours` from the openHAB poll); *History in the browser*
  (openHAB keys trimmed by it); *Rendering* (the chart, AC7); *Limits and errors* ("at most `history_window_minutes`"
  gets the openHAB exception).
- `docs/areas/configuration.md`: row `openhab.history_window_hours`; the integer-options list of line 36.
- `docs/areas/http-api.md`: *Versioning and stability* gets the rule of 0018; its first open question is removed.
- `docs/API.md`: history section (the keys, omitted when empty, one point per openHAB poll, restarts, up to hours of
  points; "every scalar key is always present" limited to the seven); `?since=` bullet on omitted keys; openHAB field
  table, example and "three always-present fields" → four; a sentence that clients ignore unknown keys.
- `docs/ARCHITECTURE.md`: *Snapshot + history* (openHAB buffers, `OpenHAB.HistoryCapacity()`); *openHAB poll* (sample
  handed to the next fast tick, link 0020).
- `docs/CONTRIBUTING.md` *stability* block; `AGENTS.md` *code-style* block and `.github/copilot-instructions.md`
  identically: the rule of 0018.
- `.squad/project.md` *Guarantees*, v1 line: the Dev reports the text, the orchestrator writes it.
- `README.md`: openHAB feature bullet (memory history chart).
- `packaging/pimonitor.example.yaml`: `history_window_hours: 24` with a comment (1 to 168 hours).
- `docs/integrations/openhab.md`: the card sentence (line 165) names the memory chart.

## Architecture check

The openHAB loop still never delays a tick (it only appends to a slice under `c.mu`, as short as `memoryTotal`'s
read); `history.bin` stays atomic, bounded and version 1; a missing source records no point (no fabricated
reading); the v1 guarantee is reworded by 0018 on the Product Manager's statement.

## Security considerations

- [x] Limits: the decode limits apply to the raw file before allocation, unchanged; the waiting slice is capped at `OpenHAB.HistoryCapacity()` (pinned by "AC5 waiting samples capped at the
  openHAB capacity without a tick"); the capacity is bounded by validation.
- [x] Exceptions reaching the user: the AC3 startup errors only; they name the option and the number.

## Decision records

- `docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md` (Proposed)
- `docs/decisions/0019-openhab-history-has-its-own-window.md` (Proposed)
- `docs/decisions/0020-openhab-points-recorded-by-next-fast-tick.md` (Proposed)

## Challenge

- 1 (major, a sample stamped before tick k's start but appended after its drain is lost to `?since=`). Accepted:
  the hook now takes the time while holding `c.mu` and that time becomes the status's `collected_at`
  (*Poll side*, *Recording*); 0020's Decision and Consequences say so; a test case with an injected poll clock is
  added to AC5. The spec needs no change: AC1, AC2 and E9 name the poll's `collected_at`, which is still the value
  `GET /api/v1/openhab` reports and still taken right after the poll finishes.
- 2 (minor, the chart block appears only on the next browser openHAB poll). Accepted: `renderHistory` re-renders the
  card when the two-point condition changes (*Dashboard*), with a source-scan case in AC7.

## Security review

- 1 (blocking, the waiting slice's bound depends on tick timing). Accepted: capped at `OpenHAB.HistoryCapacity()`,
  oldest dropped (*Recording*), a test case in AC5, *Security considerations* and 0020 reworded.
- 2 (non-blocking, lock order of the hook). Accepted: *Poll side* states the order — hook under `c.mu` first, then
  the status under `OpenHABCollector.mu`, never one held while taking the other.

## Out of scope / follow-ups

The spec's *Out of scope*. Noticed: `HISTORY_DEVICE_KEYS` in `app.js` lacks the disk I/O keys, which the
dashboard does not chart, so a merge drops them locally; harmless, not changed here.
