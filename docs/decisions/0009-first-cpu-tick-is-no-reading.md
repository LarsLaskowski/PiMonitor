# 0009: The first CPU tick after start counts as no reading

- **Status:** Accepted
- **Date:** 2026-11-13
- **Area:** Metrics
- **Source:** Issue #241
- **Supersedes:** —

## Context

CPU usage is a delta between two samples of `/proc/stat`. The first fast tick after start has no earlier sample,
yet the CPU collector reported it as a successful reading of 0 %. The tick therefore recorded a 0 in the
`cpu_percent` history — a dip to 0 after every restart, also next to history restored from `history.bin` — and the
alert engine evaluated 0 % as a real sample, giving `cpu` a state with value 0 that dates from startup.
`.squad/project.md` (*Guarantees*) rules out a fabricated reading, and
[0003](0003-failed-reading-leaves-history-gap.md) left this 0 open as a separate question. Disk I/O and network,
also delta-based, already report nothing on the first tick. The snapshot's `cpu.overall_percent` cannot carry
"no value" without a change to the v1 shape.

## Options considered

1. **The first call reports "no previous sample" as an error the fast tick tells apart from a failure** — the
   tick's existing handling of a failed reading (no history point, metric not evaluated by the alert engine) applies
   unchanged, and only the warning is left out; the collector's signature stays as it is.
2. **Take a baseline sample before the first tick** — the first tick follows `Run`'s start immediately, so the
   delta would span a few milliseconds: at the kernel's 100 Hz counter rate that is 0 or 100 % noise, or no
   elapsed time at all, which yields 0 again. Delaying the first tick instead would leave the service without any
   snapshot (`/healthz` at `503`) for a full interval.
3. **A validity flag next to the usage** (a third return value or a field of the usage type) — says the same as
   option 1 but changes the collector's API and every caller, and a field of the usage type would need hiding
   from the JSON shape.
4. **Keep the 0 and document it** — leaves a fabricated reading in the history and in alert evaluation.

## Decision

The first CPU sample after start is stored as the baseline and counts as no reading: the fast tick records no
`cpu_percent` point for it, the alert engine skips `cpu` for that sample, and no warning is logged (option 1). The
snapshot keeps its 0, as for a failed reading. The rules are in [Metrics](../areas/metrics.md#cpu-and-load).

## Consequences

- `cpu_percent` holds one point fewer than `memory_used_percent` per start, and `cpu` has no alert state until the
  second fast tick.
- `GET /api/v1/metrics` still shows 0 on the first tick, and Prometheus leaves the CPU families out:
  [0010](0010-missing-cpu-reading-left-out-of-prometheus.md).
