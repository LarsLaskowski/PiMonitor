# 0021: A non-finite point in history.bin is dropped on restore

- **Status:** Proposed
- **Date:** 2026-11-17
- **Area:** History
- **Source:** Issue #263
- **Supersedes:** —

## Context

`history.bin` stores each point's value as raw IEEE 754 bits, and the decoder accepted every bit pattern, NaN and
±Inf among them. `encoding/json` cannot encode a non-finite float, so one such restored point made every
`GET /api/v1/metrics/history` answer 500 until it left the window — with the openHAB window of up to 168 hours, for
up to a week. A damaged or tampered file brings one in; writing that file needs write access to `data_dir`. The file is disposable derived state, and
the other decode rules (`docs/areas/history.md`, *Limits and errors*) discard a file that breaks the format.

## Options considered

1. **Drop the point while decoding, keep the rest of the series and the file** — the history keeps every
   readable point; the dropped one leaves a gap, as a failed reading does
   ([0003](0003-failed-reading-leaves-history-gap.md)). The file is not reported.
2. **Treat a non-finite value as an invalid file** — one rule with the existing format violations, and the
   warning tells the operator; but one bad point costs the whole history, although the layout is intact and the
   point is the only unusable part.
3. **Filter non-finite values when the history is served** (in `internal/httpapi` or a JSON encoder of
   `HistoryPoint`) — would also cover a non-finite value from live collection, but leaves the value in the ring
   buffers and in every later `history.bin`, and moves a decode concern into every reader of the history.
4. **Replace the value with 0 or the previous value** — keeps the series aligned in time, but is a fabricated
   reading, which `.squad/project.md` (*Guarantees*) rules out.

## Decision

The decoder of `history.bin` drops every point whose value is NaN or ±Inf and keeps the other points of the series
and the rest of the file (option 1). The rule is in [History](../areas/history.md#restore-on-startup).

## Consequences

- A restored series may hold fewer points than the file declares; a series left without points is restored empty
  (scalar) or not at all (per-device), by the existing restore rules.
- The drop is silent: no warning names the file or the number of dropped points.
- The guard sits on the file alone. The live readers of the history series take finite values (integer sysfs
  temperatures, kernel load averages, computed rates); should one ever record a non-finite value, the history
  response fails again, and that reader is fixed then.
