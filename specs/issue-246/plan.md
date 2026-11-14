# Plan: Leave a failed memory reading out of the Prometheus output

Source: Issue #246
Tier: standard — the Prometheus output changes for a tick whose `/proc/meminfo` read failed (a behavior change), and no security control is created, changed or removed.
Security areas touched: none

## Problem / root cause

A fast tick whose `/proc/meminfo` read failed already adds no memory or swap history point and is not evaluated
by the alert engine (`internal/collector/collector.go:611-612` and `:634-636`, keyed on `s.memErr`), but `fastTick`
copies the zero `Memory` and `Swap` into the snapshot (`collector.go:580-581`) without recording that they are no
reading, unlike the CPU (`:572`, `CPUValid`) and the temperature (`:576`, `TemperatureValid`). The Prometheus
renderer therefore cannot tell them apart and writes both families unconditionally
(`internal/httpapi/prometheus.go:110-126`, called at `:34-35`): `pimonitor_memory_*` and `pimonitor_swap_*` 0 are
fabricated samples.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: `MemoryCollector.Collect` returns zero values on a failed read | `memory.go:96` (read error) and `:100` (parse error) return `Memory{}, Swap{}` | confirmed |
| 2 | Issue: such a tick adds no history point and is not evaluated by the alert engine | `collector.go:611-612` `addPoint(…, s.memErr, …)`; `:634` `MemoryValid: s.memErr == nil`, `:636` `SwapValid: s.memErr == nil` | confirmed |
| 3 | Issue: `pimonitor_memory_*` and `pimonitor_swap_*` still report 0 | `writeMemoryMetrics`/`writeSwapMetrics` write header and sample with no condition, `prometheus.go:110-126` | confirmed |
| 4 | Issue: the CPU and temperature families are left out without a reading (record 0010) | `prometheus.go:53` `if !snap.CPUValid`, `:75` `if !snap.TemperatureValid` | confirmed |
| 5 | Memory and swap come from one read: one error decides both | `MemoryCollector.Collect` returns both from one `os.ReadFile` (`memory.go:94-103`); `collector.go:532` | confirmed |
| 6 | A snapshot field with `json:"-"` leaves the v1 JSON unchanged; `CPUValid` and `TemperatureValid` are the precedent | `types.go:224`, `:240` | confirmed |
| 7 | Before the first fast tick the snapshot is the zero value apart from `Disks`/`DiskIO`, so the new flag is false and the families absent, as for CPU and temperature | `collector.go:286` | confirmed |
| 8 | A host without swap has a successful read with `SwapTotal` 0: a real reading, rendered as 0 | `memAndSwapFromFields`, `memory.go:60-70`; `docs/areas/metrics.md` *Memory and swap* | confirmed |
| 9 | Record 0010 is unreleased (edited and renamed in place) | `git ls-remote --tags origin`: latest `v1.1.0`; `git tag --contains 5caff12` (the commit that added it) prints nothing | confirmed |
| 10 | Related defect: `parseMeminfo` accepts content without `MemTotal` (any one `key: number` line) as a successful read, so memory then reads 0 with no error — a fabricated reading the new flag would mark valid; not fixed here (*Out of scope*) | `memory.go:39` errors only when no field at all parsed; `:48`, `:56-58` give 0 % for total 0 | confirmed |

## Acceptance criteria

- AC1: `Collector.Snapshot().MemoryValid` is false before the first fast tick and after a fast tick whose
  `MemoryCollector.Collect` returned an error — a tick whose `/proc/meminfo` cannot be read, and a tick whose
  content holds a non-numeric value — and true after a tick with a reading; the deciding path is
  `c.latest.MemoryValid = s.memErr == nil` in `fastTick`.
- AC2: `renderPrometheusMetrics` of a snapshot with `MemoryValid` false writes no line (no `# HELP`, `# TYPE` or
  sample) containing `pimonitor_memory_` or `pimonitor_swap_`, also when `Memory` and `Swap` hold non-zero values;
  the other families (CPU, temperature, disk, updates) are written as before.
- AC3: With `MemoryValid` true all six memory and swap families are written as today, and a successful read of a
  host without swap gives `pimonitor_swap_total_bytes 0`, `pimonitor_swap_used_bytes 0` and
  `pimonitor_swap_used_percent 0`.
- AC4: The bodies of `GET /api/v1/metrics` and `GET /api/v1/metrics/memory` are byte-identical for a snapshot with
  `MemoryValid` true and the same snapshot with `MemoryValid` false (the v1 shape is unchanged).

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCollector_FastTick_MemoryValid_AC1` (subtests: before the first tick false; valid fixture true; file removed after a valid tick false; non-numeric value after a valid tick false) | `internal/collector/collector_test.go` |
| AC2 | `TestRenderPrometheusMetrics_OmitsMemoryWhenInvalid_AC2` | `internal/httpapi/prometheus_test.go` |
| AC3 | `TestRenderPrometheusMetrics_RendersRealZeroSwap_AC3` | `internal/httpapi/prometheus_test.go` |
| AC4 | `TestHandleMetrics_MemoryValidNotInJSON_AC4` (table over both routes) | `internal/httpapi/handlers_test.go` |

## Approach

1. `internal/collector/types.go`: add `MemoryValid` to `Snapshot` right after `Swap`, tag `json:"-"`, with a doc
   comment in the style of `CPUValid`: true only when the latest fast tick read `/proc/meminfo`; false before the
   first tick and after a failed read or parse; it covers `Memory` and `Swap`, which come from that one read; kept
   out of the JSON so the v1 shape stays; for in-process consumers such as the Prometheus renderer (record 0010).
2. `internal/collector/collector.go` `fastTick`: set `c.latest.MemoryValid = s.memErr == nil` next to
   `c.latest.Swap = s.swap`.
3. `internal/httpapi/prometheus.go`: `renderPrometheusMetrics` calls `writeMemoryMetrics` and `writeSwapMetrics`
   only when `snap.MemoryValid` is true (one `if` around both calls), with a comment saying why (no fabricated 0,
   like CPU and temperature; record 0010). The two writers keep their signatures.
4. The JSON responses are untouched. No new user-visible text.

## Options considered

1. Chosen: a `json:"-"` validity field `MemoryValid` and the renderer leaves the memory and swap families out — the
   `CPUValid`/`TemperatureValid` pattern the issue names; the v1 shape is unchanged.
2. `NaN` for the missing reading — lost as in 0010: poisons PromQL aggregations, and every other family without a
   reading is left out instead.
3. A validity flag or `null` in the v1 JSON — lost: a v1 shape change needs `/api/v2/...`; recorded in 0010 as the
   way to revisit.
4. Keep the 0 and document it — lost: leaves the fabricated samples the issue reports.
5. Guard on `Memory.TotalBytes == 0` instead of a field (the issue's question whether a flag is needed) — lost: swap
   has a real total of 0 on a host without swap (fact 8) and would have to borrow the memory total, and history and
   alerts already decide on `s.memErr`; the renderer would apply a second rule (also fact 10's content would pass).
6. Separate `MemoryValid` and `SwapValid` snapshot fields, as `alert.Sample` has — lost: always equal (fact 5).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `Snapshot` in `types.go` | new field `MemoryValid` |
| `internal/collector` | `fastTick` in `collector.go` | sets `MemoryValid` |
| `internal/httpapi` | `renderPrometheusMetrics` in `prometheus.go` | guard on `MemoryValid` |

## Signatures (for the Dev's skeleton)

- `MemoryValid bool` — new field of `collector.Snapshot` (`internal/collector/types.go`), struct tag json:"-".

## Test files

`internal/collector/collector_test.go`, `internal/httpapi/prometheus_test.go`, `internal/httpapi/handlers_test.go`.

Existing tests whose input the new criterion handles differently (the memory and swap families vanish without
`MemoryValid: true`), adapted by the **Tester** in step 5 by setting `MemoryValid: true`, no assertion touched:
`fullSnapshot()` in `prometheus_test.go`, whose memory and swap lines `TestRenderPrometheusMetrics_LabelsAndValues`,
`_HelpAndTypeComments` and the #243 test `_OmitsCPUWhenInvalid_AC2` (`pimonitor_memory_used_percent 29.9`) expect. The snapshots of `_OmitsAbsentOptionalFields` and `_OmitsTemperatureWhenInvalid` and of `newTestServer`
assert nothing about memory and stay unchanged. No call site of a changed signature exists.

Files no unit test can cover: none.

## Areas

- `docs/areas/http-api.md`, *Prometheus endpoint*: the "left out entirely" bullet adds the memory and swap
  families while the latest fast tick has no `/proc/meminfo` reading (a failed read or parse); *Related decisions*:
  the 0010 entry gets the new title and file name.
- `docs/areas/metrics.md`, *Memory and swap*: a bullet that a failed read or parse of `/proc/meminfo` leaves memory
  and swap at 0 for that tick and `GET /metrics` leaves their families out (link to HTTP API); *Related decisions*:
  the 0010 entry gets the new title and file name.

## Documentation updates

All by the Dev, once the code is green:

- `docs/API.md`, `GET /metrics` table: the `memory_*` and `swap_*` rows — omitted entirely after a failed
  `/proc/meminfo` read, as for `cpu_usage_percent`; a real `0` (no swap) is written.
- `docs/API.md`, notes of `GET /api/v1/metrics`: after a failed `/proc/meminfo` read every `memory` and `swap` figure
  reads 0, indistinguishable from a real 0 in v1 (`GET /metrics` leaves those families out instead) — next to the
  `cpu.overall_percent` note.
- The two area documents under *Areas*, whose links to `0010-missing-cpu-reading-left-out-of-prometheus.md` are
  broken by the rename until then.
- Sweep done: the enumeration "A family whose source has no reading is left out" in `http-api.md` (above) is the only
  "only these" list of omitted families; `docs/ARCHITECTURE.md` lists no Prometheus families; `README.md` has no
  family list; `docs/areas/history.md` and `alerts.md` already state the memory behavior; no changelog file exists;
  the PR template's REST API box asks for `API.md` (above).

## Architecture check

The guarantee "never a fabricated reading" is extended to the memory and swap families in Prometheus;
"`/api/v1/...` shapes are stable" is kept by `json:"-"` (AC4). `/metrics` is not versioned (`http-api.md`), and
leaving a family out is its documented behavior for a source without a reading. No `docs/ARCHITECTURE.md` change.

## Security considerations

- [x] No limits.
- [x] No exception reaches a user-visible text: none.

## Decision records

- `docs/decisions/0010-missing-reading-left-out-of-prometheus.md` (Accepted, unreleased: extended in place and renamed
  from `0010-missing-cpu-reading-left-out-of-prometheus.md` to cover memory and swap, options 5 and 6 added; index row
  and the link in 0009 updated in this step).

## Out of scope / follow-ups

- Proposed issue: "[Metrics] /proc/meminfo without MemTotal counts as a reading of 0" — body: `parseMeminfo`
  (`internal/collector/memory.go`) fails only when no `key: number` line parses at all, so content without `MemTotal`
  is a successful read and memory reads 0 (0 % used) in the snapshot, history, alerts and Prometheus; decide whether
  a missing `MemTotal` (and `SwapTotal`/`SwapFree`) is a parse error. Not reachable with a real kernel's
  `/proc/meminfo`.
- Load average: its failed read also reads 0 in the v1 JSON, but it has no Prometheus family; nothing to do here.
