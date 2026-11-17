# 0019: openHAB memory history has its own window and capacity

- **Status:** Proposed
- **Date:** 2026-11-17
- **Area:** History
- **Source:** Issue #183
- **Supersedes:** —

## Context

The history window `history_window_minutes` (default 60) and the fast tick (`poll_interval_seconds`, default 5 s)
size every series today. openHAB is polled every `openhab.poll_interval_seconds` (default 60 s), and the memory leak
the openHAB history is meant to show climbs over hours or days. Each ring buffer allocates its capacity at start.

## Options considered

1. **A dedicated option `openhab.history_window_hours` (default 24, 1 to 168), capacity window ÷ openHAB poll
   interval** — 1,440 points per series at the defaults, 10,080 at most; an operator tunes it without stretching
   every other series.
2. **The existing window at the openHAB cadence** (`history_window_minutes × 60 / openhab.poll_interval_seconds`) — no
   new option, but 60 points, one hour, at the defaults: too short to tell a leak from a plateau.
3. **The fast-tick buffers at 5 s** — 12 times more data than openHAB is polled for (#181) and still one hour.
4. **A longer `history_window_minutes`** — grows every series, and the full history response with it.

## Decision

The openHAB series take their window from `openhab.history_window_hours` and their capacity from that window and
`openhab.poll_interval_seconds` (option 1). The rules are in [History](../areas/history.md) and
[Configuration](../areas/configuration.md).

## Consequences

- One more option to document and validate; the bound of 168 h keeps memory (three series of at most 10,080 points)
  and the full history response bounded.
- The dashboard needs the openHAB window to trim what it keeps; `GET /api/v1/openhab` reports it
  ([0018](0018-new-key-in-v1-response-is-no-shape-change.md)).
- A full `GET /api/v1/metrics/history` holds up to a day of openHAB points beside an hour of everything else.
