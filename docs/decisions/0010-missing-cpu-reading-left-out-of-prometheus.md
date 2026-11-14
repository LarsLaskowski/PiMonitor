# 0010: A missing CPU reading is left out of Prometheus, the v1 snapshot keeps its 0

- **Status:** Accepted
- **Date:** 2026-11-14
- **Area:** HTTP API
- **Source:** Issue #243
- **Supersedes:** —

## Context

Since [0009](0009-first-cpu-tick-is-no-reading.md) a fast tick without a CPU reading — the first one after start, or
one whose `/proc/stat` read failed — adds no `cpu_percent` history point and is not evaluated by the alert engine.
The snapshot still carries `cpu.overall_percent` 0 for it, and the Prometheus renderer writes that 0 as
`pimonitor_cpu_usage_percent 0`, so a scrape on such a tick stores a fabricated sample that `.squad/project.md`
(*Guarantees*) rules out. The temperature already has the answer for Prometheus: `Snapshot.TemperatureValid`, a
field kept out of the JSON, lets the renderer leave the family out. `GET /api/v1/metrics` is part of the stable v1
contract, where a changed response shape needs `/api/v2/...`.

## Options considered

1. **A `CPUValid` field of the snapshot, kept out of the JSON, and the renderer leaves both CPU families out while
   it is false** — the pattern `TemperatureValid` set; a scraper sees a missing sample, which Prometheus already
   handles (a gap, staleness), and the v1 shape stays as it is.
2. **Write `NaN` for the missing reading** — keeps the series continuous, but `NaN` poisons `sum`, `avg` and
   comparisons in PromQL, and every other family without a reading is left out instead.
3. **A validity flag in the v1 JSON** (`cpu.valid`, or `overall_percent` as `null`) — tells JSON clients apart too,
   but changes the v1 shape, which needs `/api/v2/...`; no client has asked for it, and the window is one poll
   interval after start or a failed read.
4. **Keep the 0 and document it** — leaves the fabricated sample in every Prometheus database that scrapes on such a
   tick.

## Decision

The snapshot gets `CPUValid`, true exactly when the latest fast tick read the CPU usage, excluded from the JSON; the
Prometheus renderer writes neither `pimonitor_cpu_usage_percent` nor `pimonitor_cpu_core_usage_percent` while it is
false (option 1). `GET /api/v1/metrics` keeps its 0 and its shape. The rules are in
[HTTP API](../areas/http-api.md#prometheus-endpoint-metrics).

## Consequences

- A scrape on the first tick after start, or on a tick whose `/proc/stat` read failed, has no CPU sample; queries
  over the CPU family see a gap there instead of a dip to 0.
- JSON clients and the dashboard still read 0 for such a tick and cannot tell it from a real 0; telling them apart
  needs a nullable or flagged CPU value in `/api/v2/...` (option 3).
- Memory and swap still render a failed `/proc/meminfo` read as 0 in Prometheus; that is a separate issue.
