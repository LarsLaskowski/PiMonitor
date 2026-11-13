# Plan: First fast tick records a 0 % CPU reading as valid

Source: Issue #241
Tier: standard — a behavior change in the collector (what the first CPU tick records and evaluates) that creates, changes or removes no security control.
Security areas touched: Parsing of local input (`internal/collector/cpu.go` reads `/proc/stat`; no parsing rule changes), Logging (the condition of the existing `cpu collection failed` warning in `internal/collector/collector.go`; no new log call or field)

## Problem / root cause

`CPUCollector.Collect` (`internal/collector/cpu.go:116-135`) builds `usage := CPUUsage{}`, fills it only
`if c.prev != nil` (line 117), stores the sample and returns `usage, nil` (line 135) — so the first call after
construction reports a zero usage with a nil error. `fastTick` treats a nil error as a reading: it adds a
`cpu_percent` point (`internal/collector/collector.go:604`) and passes `CPUValid: s.cpuErr == nil` (line 628) to
the alert engine, which evaluates 0 % (`internal/alert/alert.go:197`). The fix makes the first call report "no
previous sample" as an error the fast tick tells apart from a failure.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: the first `Collect` has no previous sample yet returns a nil error | `cpu.go:116-135` | confirmed |
| 2 | Issue: the fast tick records a 0 in `cpu_percent` | `collector.go:599-604`: a point is added when the error is nil | confirmed |
| 3 | Issue: the 0 is evaluated as a valid alert sample | `collector.go:627-628`, `alert.go:197-198` | confirmed |
| 4 | Issue: documented in `docs/areas/metrics.md` (*CPU and load*) and left open by record 0003 | `metrics.md:47-48`, `history.md:32-33`, 0003 *Consequences* | confirmed |
| 5 | `Run` runs the first fast tick right after start, with no interval before it | `collector.go:330-339` | confirmed |
| 6 | A failed CPU reading already gives no history point and skips alert evaluation; the snapshot keeps 0 and omits `per_core_percent` | `collector.go:570`, `599-604`, `628`; test `TestCollector_FastTick_MissingAggregateCPULine_AC4` | confirmed |
| 7 | Disk I/O and network, also delta-based, record nothing on the first tick | `metrics.md:111`, `119`; `history.md:35-36` | confirmed |
| 8 | Every `cpu collection failed` warning comes from one call site | `collector.go:512-515` | confirmed |
| 9 | The aggregate-line check and read/parse errors precede the use of `c.prev`, so a broken first sample already fails before the new check | `cpu.go:101-113` | confirmed |
| 10 | `CPUCollector` is used only inside `internal/collector` | `grep -rn CPUCollector --include=*.go` outside the package: no hit | confirmed |
| 11 | Unexported sentinel errors are the package's convention | `errStatfsTimeout` (`disk.go:29`), `errVcgencmdUnavailable` (`vcgencmd.go:23`) | confirmed |
| 12 | Record 0003 is unreleased, so its *Consequences* bullet is edited in place | `git tag --contains 6638280` prints nothing; tags on origin end at v1.1.0 | confirmed |
| 13 | `/proc/stat` counters are in USER_HZ (typically 100 per second) | proc(5), `/proc/stat` | confirmed |

## Acceptance criteria

- AC1: The first `Collect` of a `CPUCollector` on usable `/proc/stat` content returns a zero `CPUUsage` and an
  error for which `errors.Is(err, errNoPreviousSample)` holds, and stores that sample: a second call on
  `procStatFixture2` after `procStatFixture1` returns a nil error, `OverallPercent` ≈ 66.67 and two per-core values.
- AC2: A first `Collect` whose content is unusable (no aggregate `cpu` line; file missing) returns its failure
  error, for which `errors.Is(err, errNoPreviousSample)` is false; a following call on usable content is then still
  the first sample and returns `errNoPreviousSample`.
- AC3: The first `fastTick` with a readable `/proc/stat` adds no `cpu_percent` point, logs no `cpu collection
  failed` warning, and leaves the snapshot's `cpu.overall_percent` 0 and `per_core_percent` nil; the second tick
  adds one `cpu_percent` point.
- AC4: With alerts enabled, the first `fastTick` produces no `cpu` alert state; the second tick produces one with
  the second tick's value.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCPUCollector_Collect_FirstCallIsNoReading`: `t.Run("AC1 first call returns errNoPreviousSample and zero usage")`, `t.Run("AC1 second call diffs against the first sample")` | `internal/collector/cpu_test.go` |
| AC2 | `TestCPUCollector_Collect_FirstCallFailure`: `t.Run("AC2 missing aggregate line is a failure")`, `t.Run("AC2 missing file is a failure")`, `t.Run("AC2 valid call after a failed first call is the first sample")` | `internal/collector/cpu_test.go` |
| AC3 | `TestCollector_FastTick_FirstCPUTickIsNoReading_AC3` | `internal/collector/collector_test.go` |
| AC4 | `TestCollector_FastTick_FirstCPUTickNotEvaluated_AC4` | `internal/collector/collector_test.go` |

AC3 and AC4 point `c.cpu` at a fixture written with `writeStatFile` (`procStatFixture1`, then
`rewriteStatFile` to `procStatFixture2`), never at the host's `/proc/stat`; AC4 uses zero thresholds as
`TestCollector_Alerts_EvaluatedOnFastTick` does and compares the `cpu` state's `value` with ≈ 66.67.

## Approach

- `cpu.go`: add the unexported sentinel `errNoPreviousSample` (`errors.New("no previous cpu sample")`). In
  `Collect`, after the existing checks and under the lock, when `c.prev` is nil store the sample as today and
  return `CPUUsage{}, errNoPreviousSample`; otherwise compute as today. Rewrite the `Collect` doc comment to say
  the first call returns `errNoPreviousSample`.
- `collector.go`, `collectFastTickSamples`: log `cpu collection failed` only when `s.cpuErr` is non-nil and not
  `errors.Is(s.cpuErr, errNoPreviousSample)`. `fastTick` itself stays unchanged: its existing `s.cpuErr != nil`
  handling already adds no point and sets `CPUValid` false. Update the comment on `fastTickSamples` and above
  `Evaluate` so they say "failed or not yet available", not only "failed".
- No new user-visible text; no new log line.

## Options considered

1. Sentinel error from the first `Collect` (chosen) — reuses the tick's failed-reading path, no API change.
2. Baseline sample before the first tick — lost: the first tick follows at once, so the delta spans milliseconds
   of USER_HZ counters (0, 100 or no elapsed time); delaying the first tick leaves `/healthz` at `503` for an
   interval.
3. A validity flag (third return value or a `CPUUsage` field) — lost: same effect, but changes the API and every
   call site, and a field must be hidden from the v1 JSON.
4. Keep the 0 and document it (no change) — lost: it is a fabricated reading against the guarantee in
   `.squad/project.md`, and 0003 already called it a question.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `cpu.go` (`CPUCollector.Collect`) | first call returns `errNoPreviousSample` |
| `internal/collector` | `collector.go` (`collectFastTickSamples`) | no warning for `errNoPreviousSample` |

## Signatures (for the Dev's skeleton)

None: no exported member changes, and `Collect` keeps its signature. The new sentinel is an unexported package
variable, described in *Approach*; no skeleton step.

## Test files

`internal/collector/cpu_test.go`, `internal/collector/collector_test.go`.

Existing tests whose input the change handles differently, adapted by the Tester (no signature changes):

- `cpu_test.go` `TestCPUCollector_Collect` expects a nil error from the first call — replaced by the AC1 test.
- `cpu_test.go` first calls that `t.Fatalf` on any error: `TestCPUCollector_Collect_MissingAggregateLine` (subtests
  "AC1 after a valid sample", "AC2 aggregate line with seven counters") and
  `TestCPUCollector_Collect_FailedReadKeepsPreviousSample_AC3` — accept `errNoPreviousSample` there.
- `collector_test.go` `TestCollector_Alerts_EvaluatedOnFastTick` asserts a `cpu` state after one tick — tick
  twice.
- `collector_test.go` `TestCollector_FastTick_FailedReadingAddsNoHistoryPoint` (via `assertFailedTickSeries`)
  expects one `cpu_percent` point in the non-CPU cases — prime the CPU collector with one `Collect` before the
  tick; in the CPU case prime a readable fixture and then make the same collector's path missing, so the case
  still tests a failure and not the first tick.
- `collector_test.go` `TestCollector_FastTick_MissingAggregateCPULine_AC4` would pass with `before == 0` without
  testing anything — tick twice on the valid fixture before taking `before`.

Files no unit test can cover: none.

## Areas

- Metrics: the first fast tick's CPU reading counts as no reading.
- History: `cpu_percent` gets no point on the first tick.
- Alerts: `cpu` has no state until the second sample.

## Documentation updates

All by the Dev, against the code as built:

- `docs/areas/metrics.md`, *CPU and load* (lines 47-48): the first fast tick keeps `overall_percent` 0 and omits
  `per_core_percent`, and counts as no reading — no `cpu_percent` history point, no alert evaluation, no warning;
  *Related decisions*: add 0009.
- `docs/areas/history.md`, *Recording* (lines 32-33): replace the sentence that the first tick's `cpu_percent`
  is still recorded as 0 with: it gets no point, linking `metrics.md#cpu-and-load`; *Related decisions*: add 0009.
- `docs/areas/alerts.md`, *Failed readings and vanished filesystems* (lines 91-94): `cpu` has no state until the
  second sample after start; *Related decisions*: replace "None yet." with 0009.
- Sweep done: `docs/ARCHITECTURE.md` (line 101, "never a fabricated reading": still true, no edit), `docs/API.md`
  (no CPU first-tick statement; shape unchanged), `README.md` (none), `metrics.md` *Limits and errors* (the list of
  warnings per tick is about failures; unchanged). No changelog file exists; the PR template asks for none.

## Architecture check

The guarantee "never a fabricated reading" (`.squad/project.md`, `ARCHITECTURE.md:99-101`) is strengthened; the
v1 response shapes and `history.bin` layout are unchanged.

## Security considerations

- [x] No limits: no input bound changes.
- [x] Exceptions reaching a user: none — `errNoPreviousSample` is never logged or served.

## Decision records

- `docs/decisions/0009-first-cpu-tick-is-no-reading.md` (Proposed); record 0003's *Consequences* bullet now
  points to it (unreleased, edited in place).

## Out of scope / follow-ups

- The snapshot and Prometheus `pimonitor_cpu_usage_percent` (`internal/httpapi/prometheus.go:52`) still show 0
  on the first tick and on a failed reading, while `pimonitor_temperature_celsius` is omitted when invalid
  (`prometheus.go:75`): proposed follow-up issue below.
- `.squad/project.md` (*Guarantees*) points to *Open questions* in `docs/areas/metrics.md`, which has no such
  section: proposed follow-up issue below.
