# Plan: Record no history point for a failed reading and flush history on shutdown

Source: Issue #198
Tier: standard — two behavior changes in the collector (which points a tick records, and the order of the final history write) that create, change or remove no security control.
Security areas touched: File writes — `<data_dir>/history.bin` in `internal/collector/persist.go` (when and with which state the final write runs; the atomic write and the decoder are unchanged)

## Problem / root cause

1. `fastTick` (`internal/collector/collector.go:586-592`) adds a point to all seven scalar series on every tick,
   with the sample value whatever the collection returned. The per-source errors are known at that point
   (`fastTickSamples.cpuErr`, `tempErr`, `memErr`), except the load average's, which `collectFastTickSamples`
   keeps only in the local `err` (`collector.go:507-510`). A failed reading therefore records the 0 the snapshot
   falls back to.
2. `Run`'s `ctx.Done()` branch (`collector.go:352-361`) calls `persistHistory`, whose try-lock
   (`persist.go:359-363`) returns at once when a slow-tick write is still running; `persistWG.Wait()` then waits
   only for that earlier write, so the file keeps its older snapshot.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: a failed collection adds a point with value 0 to the scalar series | `collector.go:586-592` adds unconditionally; collectors return zero values with their error (`cpu.go:97-105`, `loadavg.go:47-53`, `memory.go:93-101`, `temperature.go:152-153`) | confirmed |
| 2 | Issue: a host without a thermal zone gets a `temperature` series of zeros | `TemperatureCollector.Collect` returns `Temperature{}` and "no CPU thermal zone detected" (`temperature.go:152-153`); the point is added regardless (`collector.go:590`) | confirmed |
| 3 | Issue: the final write is dropped by the try-lock when a slow-tick write is running | `persist.go:359-363`, called from `collector.go:359` | confirmed |
| 4 | The load-average error is not kept in `fastTickSamples` | `collector.go:468-490` has no load error field; `collector.go:507-510` | confirmed |
| 5 | Memory and swap come from one read with one error | `MemoryCollector.Collect` returns `(Memory, Swap, error)` (`memory.go:93`), stored as `memErr` | confirmed |
| 6 | An empty scalar series marshals as `[]`, not `null`, also through `Since` | `RingBuffer.Snapshot` returns `make([]T, r.size)` (`ringbuffer.go:62`); `dropPrefix` re-slices it (`collector.go:146-148`); handler marshals `hist.Since(since)` (`internal/httpapi/handlers.go:88`) | confirmed |
| 7 | Per-device series already get no point when their collection fails | `recordDeviceHistory` adds only for devices listed in the tick (`collector.go:638-682`); a failed disk, disk I/O or network collection lists none | confirmed |
| 8 | `cpu_percent` on the first tick after start is 0 without an error | `CPUCollector.Collect` returns `CPUUsage{}` and nil when `c.prev == nil` (`cpu.go:110-129`); not a failure, out of scope | confirmed |
| 9 | The process bounds the collector wait by the 10 s shutdown context | `cmd/pimonitor/main.go:102-112` | confirmed |
| 10 | The try-lock is released before `persistWG.Done`, after `writeFile` returned | defer order in `persist.go:368-369` | confirmed |
| 11 | The dashboard copes with an empty or shorter series | `drawSparkline` returns for fewer than 2 points (`internal/web/assets/chart.js:18-20`); `mergeHistory` concatenates per series and takes bounds over all series (`app.js:1040-1094`) | confirmed |
| 12 | `historyGen` is read by the history cache, not as a point count | `internal/httpapi/handlers.go:95-113` | confirmed |

## Acceptance criteria

- AC1: A failed collection in a tick adds no point to the affected scalar series; other series of the same tick
  still get theirs. Affected series per failure: `cpu_percent` (CPU), `load1`/`load5`/`load15` (load average),
  `temperature` (CPU thermal zone), `memory_used_percent` and `swap_used_percent` (`/proc/meminfo`). The snapshot
  value of that tick is unchanged (zero).
- AC2: A host without a thermal zone has an empty `temperature` series, not zeros.
- AC3: The history response shape does not change; series just have fewer points: every scalar key is still
  present, an empty one as `[]`, in the full response and through `Since`.
- AC4: When shutdown starts while a write is running, the final write runs after it and the file holds the latest
  state.
- AC5: The shutdown still ends within its grace period when a write stalls.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCollector_FastTick_FailedReadingAddsNoHistoryPoint`, table with `t.Run("AC1 cpu …")`, `("AC1 load average …")`, `("AC1 memory …")`: the failing source is pointed at a missing file (`&CPUCollector{path: …}`, `&LoadAvgCollector{path: …}`, `&MemoryCollector{path: …}`), the temperature at a `writeThermalZone` fixture; after one `fastTick` the affected series are empty, every other scalar series has 1 point, and the snapshot field is zero | `internal/collector/collector_test.go` |
| AC2 | `TestCollector_FastTick_NoThermalZoneLeavesTemperatureHistoryEmpty`, `t.Run("AC2 …")`: `TemperatureCollector` with an empty glob dir, two ticks → `Temperature` has 0 points, `MemoryUsedPercent` 2 | `internal/collector/collector_test.go` |
| AC3 | `TestHistory_JSONKeepsEmptyScalarSeries`, `t.Run("AC3 full")` and `t.Run("AC3 since")`: one tick with no thermal zone and a failing CPU source; `json.Marshal` of `History()` and of `History().Since(<before the tick>)` decoded into `map[string]json.RawMessage` holds all seven scalar keys, with `temperature` and `cpu_percent` exactly `[]` | `internal/collector/collector_test.go` |
| AC4 | `TestCollector_Run_FinalWriteWaitsForRunningWrite`, `t.Run("AC4 …")`: `blockingWriteFile`; `persistHistory()` starts a write of the still-empty history and blocks; `Run` starts, `cancel()`; `Run` must not return while the write blocks; `close(proceed)`; after `Run` returns, `writeFile` was called twice and the file loaded by a second collector holds the same number of points per scalar series as `c.History()`, at least one (fails today: one call, empty file) | `internal/collector/persist_test.go` |
| AC5 | `TestAwaitCollector`, `t.Run("AC5 stalled collector returns at the deadline")`: a `done` channel never closed and an already canceled context → returns false (2 s test guard); plus `t.Run("finished collector")`: closed `done`, `context.Background()` → true | `cmd/pimonitor/main_test.go` |

## Approach

**Recording (AC1-AC3).** Add `loadErr error` to `fastTickSamples`, set from `c.loadAvg.Collect()` (the warning
stays). In `fastTick` add each scalar point only when its error is nil: `cpuHist` on `cpuErr`, `l1Hist`/`l5Hist`/
`l15Hist` on `loadErr`, `tempHist` on `tempErr`, `memHist`/`swapHist` on `memErr`. Snapshot assignments,
`recordDeviceHistory` and the alert sample are unchanged. `historyGen` keeps incrementing on every tick (eviction
can change the history on a tick without points, and a spurious increment only rebuilds the cache); the Dev
rewords its comment to "counts fastTicks", and the `fastTickSamples` comment to say the errors gate the history
points too.

**Final write (AC4).** New private method `flushHistory()` in `persist.go`: returns at once when `PersistPath` is
empty; otherwise sends on `c.persisting` blocking (waits until no write runs), encodes `c.History()` after that,
calls `c.writeFile` on the calling goroutine, logs a failure with the same warning as `persistHistory`, and
releases the try-lock. `Run`'s `ctx.Done()` branch calls `c.flushHistory()` instead of
`c.persistHistory(); c.persistWG.Wait()`, and its comment is rewritten (the "at most the points since the last
fast tick" claim holds now, bounded by the shutdown deadline in `main`). `persistHistory` is unchanged: writes
during operation still skip.

**Grace period (AC5).** Move the existing select in `run` (`main.go:109-112`) into
`awaitCollector(ctx, done) bool` (true when `done` closed first). `run` logs
`log.Warn("final history write did not finish within the shutdown grace period")` when it returns false, then
returns nil as today. No user-visible output beyond that log line, whose text is fixed here.

## Options considered

- Recording: a point with `null`/NaN instead of none — changes the v1 point shape (AC3) and NaN does not encode;
  repeating the last value — a fabricated reading. Both in [0003](../../docs/decisions/0003-failed-reading-leaves-history-gap.md).
- Final write: writing at once beside the running write — two renames race and the older snapshot can win; a
  `Close(ctx)` method called from `main` — API change for a bound `main` already holds. Both in
  [0004](../../docs/decisions/0004-final-history-write-waits.md).
- Including the first-tick `cpu_percent` 0 (fact 8): not a failed reading, needs a change to `CPUCollector`'s
  contract; proposed as a follow-up below.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `collector.go`: `fastTickSamples`, `collectFastTickSamples`, `fastTick`, `Run`, `historyGen` comment | `loadErr`; gated scalar points; final flush |
| `internal/collector` | `persist.go` | new private `flushHistory` |
| `cmd/pimonitor` | `main.go`: `run`, new `awaitCollector` | wait helper and warning |

## Signatures (for the Dev's skeleton)

- `func awaitCollector(ctx context.Context, done <-chan struct{}) bool` in `cmd/pimonitor/main.go` (called by a
  test; unexported).

`flushHistory` and `loadErr` are private and described under *Approach*. No existing signature changes.

## Test files

- `internal/collector/collector_test.go`, `internal/collector/persist_test.go`, `cmd/pimonitor/main_test.go`.

Existing test code that calls a changed signature: none. Existing tests kept unchanged:
`TestCollector_Run_ShutdownWaitsForFinalFlush` (its first `writeFile` call is the final flush, which still blocks
`Run` until released), `TestCollector_PersistHistory_SkipsOverlappingWrites` (`persistHistory` unchanged),
`TestCollector_FastTick_BuildsHistory` and `TestCollector_HistoryGeneration_IncrementsOnFastTick` (real `/proc`
sources succeed; `historyGen` still counts every tick).

Files no unit test can cover: none. The two changed lines in `run` (the call and the warning) are process glue no
unit test reaches; `awaitCollector` holds the logic and is tested.

## Areas

- `docs/areas/history.md`: *Recording* — a scalar series gets a point only when its metric was read in that tick
  (list the failure → series mapping of AC1; `temperature` empty without a thermal zone; `cpu_percent` still 0 on
  the first tick); *Persistence: when it is written* — the final write waits for a running write, then writes the
  current state, bounded by the shutdown deadline, and a stalled write is abandoned after it; remove both entries
  under *Open questions* (the section goes when empty); *Related decisions* lists 0003 and 0004.
- `docs/areas/metrics.md`: *Limits and errors*, bullet "No reading is invented" — add that the history records no
  point for such a tick ([History](history.md#recording)).
- `docs/areas/deployment.md`: *Process lifecycle* (line 114-117) — "waits for the collector's last history
  snapshot" becomes: waits for a history write still running and then for the final one.

## Documentation updates

All by the Dev, against the code as built:

- The three area documents above.
- `docs/API.md`, `GET /api/v1/metrics/history`: one sentence that a scalar series has no point for a tick whose
  reading failed, so series may differ in length or be empty (`temperature` without a thermal zone); the shape is
  unchanged.
- `docs/ARCHITECTURE.md`, *History persistence*, bullet "Written on the slow tick and on clean shutdown": the
  final write waits for the running one and then writes the latest state, linking 0004; bullet "Written
  asynchronously": the wait on shutdown is bounded via `awaitCollector`; also correct "hands the encode-and-write
  off" — the encode runs on the caller (`persist.go:365`). *Startup and shutdown* paragraph (line 82-85) still
  holds; check it.
- `.squad/project.md`, *Guarantees*, the missing-hardware-source line: drop "and in [History](...)" from "where
  the code falls short of this today is listed under *Open questions* there and in [History]" — the Dev reports
  the exact text, the orchestrator writes it.
- No changelog exists; README unaffected (checked lines 33, 410-419).

## Architecture check

- "A missing hardware source yields an empty/zero value and a logged warning, never … a fabricated reading":
  strengthened — the history no longer fabricates; the snapshot keeps its zero value.
- `/api/v1/...` shapes stable: kept (fact 6, AC3).
- `history.bin` atomic, corrupt file never stops startup, layout versioned: untouched (no format change; the
  final write uses the same `writeFile`).
- Shutdown bounded by the grace period: unchanged, still in `main` (AC5).

## Security considerations

- [x] No limits: no parsed input or limit changes.
- [x] Exceptions reaching a user: none; a failed final write logs the existing warning, the timeout the new log
  line (no secret in either: path and error only).
- The final write cannot hang the process: the blocking send waits at most until `main` exits at the deadline;
  an abandoned write leaves at most a `history.bin.tmp-*` file, as documented today.

## Decision records

- `docs/decisions/0003-failed-reading-leaves-history-gap.md` (Proposed)
- `docs/decisions/0004-final-history-write-waits.md` (Proposed)

## Out of scope / follow-ups

- Proposed issue — title: "[Collector] Record no cpu_percent point on the first tick after start"; body: "On the
  first fast tick `CPUCollector.Collect` has no previous `/proc/stat` sample and returns 0 without an error
  (`internal/collector/cpu.go:110-129`), so `cpu_percent` records a 0 that is no reading, and the snapshot reports
  0 %. Decide whether the first tick records no point (and how the snapshot marks it), as #198 decided for failed
  readings."
- The sparkline plots points by index (`chart.js`), so a gap is drawn as adjacent points; not a correctness issue
  of this change.
- Environment: no reported hardware; a failing `/proc` source is simulated by fixture paths. Real Pi hardware is
  not needed: the thermal-zone case is covered by the fixture.
