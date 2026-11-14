# Plan: Leave a missing CPU reading out of the Prometheus output

Source: Issue #243
Tier: standard — the Prometheus output changes for a tick without a CPU reading (a behavior change), and no security control is created, changed or removed.
Security areas touched: none

## Problem / root cause

A fast tick without a CPU reading — the first after start (`errNoPreviousSample`), or a failed `/proc/stat` read —
already adds no history point and is not evaluated by the alert engine (`internal/collector/collector.go:605` and
`:629`, both keyed on `s.cpuErr`), but `fastTick` copies the zero `CPUUsage` into the snapshot (`collector.go:571`)
without recording that it is no reading, unlike the temperature (`collector.go:575`, `TemperatureValid`). The
Prometheus renderer therefore cannot tell it apart and writes the overall family unconditionally
(`internal/httpapi/prometheus.go:51-52`): `pimonitor_cpu_usage_percent 0` is a fabricated sample.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Since #241/#245 a missing CPU reading gets no history point and no alert evaluation | `collector.go:605` `addPoint(c.cpuHist, s.cpuErr, …)`; `:629` `CPUValid: s.cpuErr == nil`; `collector.go:514` skips the warning only for `errNoPreviousSample` | confirmed |
| 2 | `GET /api/v1/metrics` shows 0 for it | `CPUCollector.Collect` returns `CPUUsage{}` with every error (`cpu.go:109`, `:113`, `:116`, `:129`); `collector.go:571` stores it; `handleMetrics` (`handlers.go:41`) and the `cpu` sub-resource (`routes.go:111`) encode it | confirmed |
| 3 | `pimonitor_cpu_usage_percent` shows 0 for it (issue: `prometheus.go:52`) | `writeCPUMetrics` writes header and sample with no condition, `prometheus.go:50-52` | confirmed |
| 4 | `pimonitor_temperature_celsius` is left out when the reading is invalid (issue: `prometheus.go:75`) | `if !snap.TemperatureValid { return }`, `prometheus.go:75` | confirmed |
| 5 | The per-core family is already absent on such a tick, but only because `PerCorePercent` is nil, not by a validity check | `prometheus.go:54`; fact 2 | confirmed |
| 6 | A snapshot field with `json:"-"` leaves the v1 JSON unchanged; `TemperatureValid` is the precedent | `types.go:233` and its comment | confirmed |
| 7 | Adding a key to a v1 response is a shape change that needs `/api/v2/...` | `AGENTS.md` (*Code style*), `.squad/project.md` (*Guarantees*) | confirmed |
| 8 | Before the first fast tick the snapshot is the zero value, so the new flag is false and the family absent too, as for the temperature | `Collector.latest` starts as `Snapshot{}` apart from the fields `New` sets (`collector.go:287-289`) | confirmed |
| 9 | Related defect: a failed `/proc/meminfo` read renders `pimonitor_memory_*` and `pimonitor_swap_*` as 0 | `memory.go:96`, `:100` return zero values; `prometheus.go` `writeMemoryMetrics`/`writeSwapMetrics` have no condition | confirmed |
| 10 | Records 0003 and 0009 are unreleased (edited in place) | `git ls-remote --tags origin`: latest `v1.1.0`; neither file exists at `v1.1.0` | confirmed |

## Acceptance criteria

- AC1: `Collector.Snapshot().CPUValid` is false after a fast tick whose `CPUCollector.Collect` returned an error —
  the first tick after start, a tick whose `/proc/stat` has no aggregate `cpu` line, and a tick whose `/proc/stat`
  cannot be read — and true after a tick with a reading (the second tick); the deciding path is
  `c.latest.CPUValid = s.cpuErr == nil` in `fastTick`.
- AC2: `renderPrometheusMetrics` of a snapshot with `CPUValid` false writes no line (no `# HELP`, `# TYPE` or
  sample) containing `pimonitor_cpu_usage_percent` or `pimonitor_cpu_core_usage_percent`, also when the snapshot's
  `CPU` holds non-zero overall and per-core values; the other families are written as before.
- AC3: With `CPUValid` true both CPU families are written as today, and a real reading of 0 gives the line
  `pimonitor_cpu_usage_percent 0`.
- AC4: The bodies of `GET /api/v1/metrics` and `GET /api/v1/metrics/cpu` are byte-identical for a snapshot with
  `CPUValid` true and the same snapshot with `CPUValid` false (the v1 shape is unchanged).

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCollector_FastTick_CPUValid_AC1` (subtests: first tick false; second tick true; no aggregate line after a valid tick false; file removed after a valid tick false) | `internal/collector/collector_test.go` |
| AC2 | `TestRenderPrometheusMetrics_OmitsCPUWhenInvalid_AC2` | `internal/httpapi/prometheus_test.go` |
| AC3 | `TestRenderPrometheusMetrics_RendersRealZeroCPU_AC3` | `internal/httpapi/prometheus_test.go` |
| AC4 | `TestHandleMetrics_CPUValidNotInJSON_AC4` (table over both routes) | `internal/httpapi/handlers_test.go` |

## Approach

1. `internal/collector/types.go`: add `CPUValid` to `Snapshot` right after `CPU`, tag `json:"-"`, with a doc comment
   in the style of `TemperatureValid`: true only when the latest fast tick read the CPU usage; false before the
   first tick, on the first tick after start (no previous sample) and after a failed read; kept out of the JSON
   so the v1 shape stays; for in-process consumers such as the Prometheus renderer (record 0010).
2. `internal/collector/collector.go` `fastTick`: set `c.latest.CPUValid = s.cpuErr == nil` next to
   `c.latest.CPU = s.cpuUsage`.
3. `internal/httpapi/prometheus.go`: `writeCPUMetrics` takes the snapshot (as `writeTemperatureMetrics` does) and
   returns before writing anything when `CPUValid` is false, so both families are left out; its doc comment says
   why (no fabricated 0, like the temperature). `renderPrometheusMetrics` passes `snap`.
4. The JSON responses are untouched. No new user-visible text.

## Options considered

1. Chosen: a `json:"-"` validity field and the renderer leaves both CPU families out — the `TemperatureValid`
   pattern; the v1 shape is unchanged.
2. Write `NaN` for the missing reading — lost: poisons `sum`/`avg`/comparisons in PromQL, and every other family
   without a reading is left out instead.
3. A validity flag or `null` in the v1 JSON (the issue's "v1 shape question") — lost: a v1 shape change needs
   `/api/v2/...` (fact 7), no client asked for it, and the window is one poll interval. Recorded in 0010 as the
   way to revisit.
4. Keep the 0 and document it — lost: leaves a fabricated sample in Prometheus.
5. Guard on the snapshot's zero values (overall 0 and no per-core) instead of a flag — lost: a real idle 0 would
   vanish too (AC3).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `Snapshot` in `types.go` | new field `CPUValid` |
| `internal/collector` | `fastTick` in `collector.go` | sets `CPUValid` |
| `internal/httpapi` | `writeCPUMetrics`, `renderPrometheusMetrics` in `prometheus.go` | guard on `CPUValid` |

## Signatures (for the Dev's skeleton)

- `CPUValid bool` — new field of `collector.Snapshot` (`internal/collector/types.go`), struct tag json:"-".
- `writeCPUMetrics` (unexported, `prometheus.go`) changes its second parameter from the CPU usage to the whole
  snapshot; no test calls it directly.

## Test files

`internal/collector/collector_test.go`, `internal/httpapi/prometheus_test.go`, `internal/httpapi/handlers_test.go`.

Existing tests whose input the new criterion handles differently (the CPU families vanish without
`CPUValid: true`), adapted by the **Tester** in step 5 by setting `CPUValid: true`, no assertion touched:
`fullSnapshot()` (used by `TestRenderPrometheusMetrics_LabelsAndValues` and `_HelpAndTypeComments`) and the
`snapshot` in `newTestServer` (`TestHandlePrometheusMetrics`), and the snapshots of
`TestRenderPrometheusMetrics_OmitsAbsentOptionalFields` and `_OmitsTemperatureWhenInvalid`, so they keep testing
the absence they name and not the new one. No call site of a changed signature exists in tests.

Files no unit test can cover: none.

## Areas

- `docs/areas/http-api.md`, *Prometheus endpoint*: the "left out entirely" bullet adds both CPU families while the
  latest fast tick has no CPU reading (first tick after start, failed `/proc/stat` read); *Related decisions*
  lists 0010 instead of "None yet."
- `docs/areas/metrics.md`, *CPU and load*: the first-tick and unparsable bullets say the CPU families are left out
  of `/metrics` for such a tick (link to HTTP API); *Related decisions* adds 0010.

## Documentation updates

All by the Dev, once the code is green:

- `docs/API.md`, `GET /metrics` table: `cpu_usage_percent` and `cpu_core_usage_percent` rows — omitted entirely on
  the first fast tick after start and after a failed `/proc/stat` read, as for `temperature_celsius`.
- `docs/API.md`, notes of `GET /api/v1/metrics`: `cpu.overall_percent` reads 0 on the first fast tick after start
  and after a failed read, indistinguishable from a real 0 in v1 (`GET /metrics` leaves it out instead) — the
  statement `docs/areas/metrics.md` already points to there and which `API.md` lacks today.
- The two area documents under *Areas*.
- Sweep done: `docs/ARCHITECTURE.md` names no list of Prometheus families or omitted fields; `README.md` has no
  Prometheus family list; no changelog file exists; the PR template's REST API box asks for `API.md` (above).

## Architecture check

The guarantee "never a fabricated reading" is strengthened for Prometheus; "`/api/v1/...` shapes are stable" is
kept by `json:"-"` (AC4). `/metrics` is not versioned (`docs/areas/http-api.md`), and leaving a family out is
already its documented behavior for a source without a reading. No `docs/ARCHITECTURE.md` change.

## Security considerations

- [x] No limits.
- [x] No exception reaches a user-visible text: none.

## Decision records

- `docs/decisions/0010-missing-cpu-reading-left-out-of-prometheus.md` (Proposed, indexed).
- `docs/decisions/0009-first-cpu-tick-is-no-reading.md` (unreleased): its consequence about the v1 shape question now
  links 0010 — done in this step.

## Out of scope / follow-ups

- Proposed issue: "[API] A failed /proc/meminfo read shows as 0 in Prometheus memory and swap families" — body: a
  failed `/proc/meminfo` read (`MemoryCollector.Collect` returns zero values, `internal/collector/memory.go`) adds no
  history point and is not evaluated by the alert engine, but `pimonitor_memory_*` and `pimonitor_swap_*`
  (`internal/httpapi/prometheus.go`) still report 0; decide whether those families are left out like the CPU and
  temperature families (record 0010).
- A nullable CPU value for JSON clients and the dashboard: only with `/api/v2/...` (record 0010).
