# 0003: A failed reading leaves a gap in the history

- **Status:** Accepted
- **Date:** 2026-11-12
- **Area:** History
- **Source:** Issue #198
- **Supersedes:** —

## Context

Every fast tick added one point to every scalar history series, also when the metric's collection had failed in
that tick: the point then carried the 0 the snapshot falls back to. A host without a CPU thermal zone served a
`temperature` series of zeros, which a chart or an integration reads as a real 0 °C. `AGENTS.md` and
`.squad/project.md` (*Guarantees*) rule out a fabricated reading for a missing source. The snapshot of
`GET /api/v1/metrics` already marks a missing temperature (`TemperatureValid`, a zone name), and the alert engine
already ignores failed readings; only the history recorded them. The `/api/v1/metrics/history` shape is part of
the stable v1 contract.

## Options considered

1. **Add no point for a failed reading** — the series simply has fewer points; the response shape stays as it is,
   and every client already handles series of different lengths (per-device series and empty series before the
   first tick exist today).
2. **Add a point with a null or NaN value** — keeps the series aligned in time, but `null` changes the v1 point
   shape and `encoding/json` cannot encode NaN at all; it would need `/api/v2/...`.
3. **Repeat the last good value** — keeps charts smooth, but it is a fabricated reading too, only a less obvious
   one.

## Decision

A fast tick whose collection of a scalar metric failed adds no point to that metric's series (option 1); the
snapshot keeps its zero or empty value for that tick. The rules are in
[History](../areas/history.md#recording).

## Consequences

- Scalar series of one response may hold different numbers of points, and a series can stay empty for a whole
  window (`temperature` on a host without a thermal zone). Clients that align series by index rather than by
  timestamp must not assume equal lengths.
- A `history.bin` written before this change still holds the zeros; they age out of the window and are not
  migrated.
- `cpu_percent` on the first tick after start, which has no previous sample, is no failure but gets no point
  either: [0009](0009-first-cpu-tick-is-no-reading.md).
- Revisiting this to keep series aligned means a new API version with a nullable value (option 2).
