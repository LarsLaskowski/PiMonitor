# 0024: openHAB alert rules are evaluated once per finished poll, with their own window

- **Status:** Accepted
- **Date:** 2026-11-18
- **Area:** Alerts
- **Source:** Issue #184
- **Supersedes:** —

## Context

The alert engine evaluates one sample per fast tick (`poll_interval_seconds`, 5 s by default), and a side of a cutoff
is confirmed once the current sample is `alerts.for_seconds` after the first sample on it. openHAB is polled on a loop
of its own every 60 s or more ([0014](0014-openhab-poll-runs-on-its-own-loop.md)). Fed into the fast tick, one poll
would be counted as many confirmations, and a 30 s window would confirm an openHAB value that was read once. A finished
poll's values reach the history only through a bounded hand-over queue drained by the next fast tick
([0020](0020-openhab-points-recorded-by-next-fast-tick.md)), which drops the oldest entries when it is full, and a poll
that finds openHAB gone (`not_installed`) hands nothing over. Things often flap for a minute during a bridge
reconnect.

## Options considered

1. **Evaluate on every fast tick and repeat the last poll's values** — no new path, but stale values confirm sides.
2. **Evaluate on the fast tick, only when a new poll arrived, from the hand-over queue** — no stale confirmations, but
   a dropped entry is never evaluated and `not_installed` never arrives.
3. **Evaluate each finished poll once in the poll's own loop, timed by its `collected_at` (the time of the poll
   for `not_installed`), with a window of its own, `alerts.openhab_for_seconds` (default 300 s)** — every poll counts
   exactly once and the window fits the cadence; the engine is now fed from two goroutines.
4. **Option 3 with the shared `alerts.for_seconds`** — fewer options, but 30 s is shorter than one poll, so every
   flap would alert.

## Decision

The openHAB poll hands each finished poll, `not_installed` included, to the alert engine once, after its time is
stamped, outside the history's queue; the engine debounces the openHAB metrics with `alerts.openhab_for_seconds`
(option 3). The rules are in [Alerts](../areas/alerts.md).

## Consequences

- The engine's lock orders the two feeds; `events` stays in production order, and `at` rises within one metric but
  not necessarily between an openHAB event and a fast-tick event.
- A window shorter than the poll interval confirms on the second poll on a side; `0` confirms on the first.
- The fast tick never touches an `openhab_*` state.
- A webhook receives events in enqueue order, so an older openHAB event can follow a newer fast-tick event; the
  notifier's rate-limit bookkeeping prunes old entries only within one feed, where events arrive in sample order.
