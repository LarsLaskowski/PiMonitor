# 0010: A missing CPU or memory reading is left out of Prometheus, the v1 snapshot keeps its 0

- **Status:** Accepted
- **Date:** 2026-11-14
- **Area:** HTTP API
- **Source:** Issues #243, #246
- **Supersedes:** —

## Context

Since [0009](0009-first-cpu-tick-is-no-reading.md) a fast tick without a CPU reading — the first one after start, or
one whose `/proc/stat` read failed — adds no `cpu_percent` history point and is not evaluated by the alert engine.
A fast tick whose `/proc/meminfo` read failed is handled the same way for memory and swap
([0003](0003-failed-reading-leaves-history-gap.md)). The snapshot still carries 0 for such a tick
(`cpu.overall_percent`, every `memory` and `swap` figure), and the Prometheus renderer wrote those zeros as samples
(`pimonitor_cpu_usage_percent 0`, `pimonitor_memory_*`, `pimonitor_swap_*`), so a scrape on such a tick stored a
fabricated sample that `.squad/project.md` (*Guarantees*) rules out. The temperature already has the answer for
Prometheus: `Snapshot.TemperatureValid`, a field kept out of the JSON, lets the renderer leave the family out.
`GET /api/v1/metrics` is part of the stable v1 contract, where a changed response shape needs `/api/v2/...`.
Memory and swap come from one read of one file, so they fail together.

## Options considered

1. **A validity field of the snapshot per source, kept out of the JSON (`CPUValid`, `MemoryValid`), and the
   renderer leaves the source's families out while it is false** — the pattern `TemperatureValid` set; a scraper
   sees a missing sample, which Prometheus already handles (a gap, staleness), and the v1 shape stays as it is.
2. **Write `NaN` for the missing reading** — keeps the series continuous, but `NaN` poisons `sum`, `avg` and
   comparisons in PromQL, and every other family without a reading is left out instead.
3. **A validity flag in the v1 JSON** (`cpu.valid`, or `overall_percent` as `null`) — tells JSON clients apart too,
   but changes the v1 shape, which needs `/api/v2/...`; no client has asked for it, and the window is one poll
   interval after start or a failed read.
4. **Keep the 0 and document it** — leaves the fabricated sample in every Prometheus database that scrapes on such a
   tick.
5. **Infer the missing reading from the values** (CPU: overall 0 without per-core data; memory: `total_bytes` 0) —
   needs no field, but a real idle CPU reads 0 too, a host without swap has a real `swap_total_bytes` 0, so swap
   would have to borrow the memory total, and the history and the alert engine already decide on the collection
   error, which the renderer would then second-guess with a different rule.
6. **Separate `MemoryValid` and `SwapValid` fields**, as `alert.Sample` has — they would always be equal, since
   both come from the one `/proc/meminfo` read, and a pair invites setting them from different conditions.

## Decision

The snapshot gets `CPUValid`, true exactly when the latest fast tick read the CPU usage, and `MemoryValid`, true
exactly when the latest fast tick read `/proc/meminfo`, both excluded from the JSON; the Prometheus renderer
writes neither CPU family while `CPUValid` is false and neither the memory nor the swap families while
`MemoryValid` is false (option 1). `GET /api/v1/metrics` keeps its zeros and its shape. The rules are in
[HTTP API](../areas/http-api.md#prometheus-endpoint-metrics).

## Consequences

- A scrape on the first tick after start, or on a tick whose `/proc/stat` read failed, has no CPU sample, and one
  on a tick whose `/proc/meminfo` read failed has no memory and swap samples; queries see a gap there instead of
  a dip to 0.
- JSON clients and the dashboard still read 0 for such a tick and cannot tell it from a real 0; telling them apart
  needs a nullable or flagged value in `/api/v2/...` (option 3).
- A further fast-tick source exported to Prometheus that can fail gets its own field the same way.
