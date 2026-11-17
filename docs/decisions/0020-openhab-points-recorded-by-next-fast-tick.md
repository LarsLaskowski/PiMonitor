# 0020: openHAB history points are recorded by the next fast tick

- **Status:** Proposed
- **Date:** 2026-11-17
- **Area:** History
- **Source:** Issue #183
- **Supersedes:** —

## Context

A client of `GET /api/v1/metrics/history?since=` passes back the newest `t` it holds and relies on every later point
being newer. A fast tick takes its timestamp when it starts collecting (`collectFastTickSamples`) and records its
points under the collector's lock only when all sources are read, which can take seconds. The openHAB poll runs on a
loop of its own ([0014](0014-openhab-poll-runs-on-its-own-loop.md)) and finishes at any time. Recorded directly, an
openHAB point timestamped after a running tick's start could be served before that tick's points, which carry an
older timestamp, and a client would then never receive them. The history response cache also changes only with the
fast tick's generation counter.

## Options considered

1. **The poll hands its sample to the collector, and the next fast tick records it together with its own points** —
   everything recorded in one tick is older than the next tick's start, so `?since=` loses nothing; the cache and the
   persistence need no change; a point appears up to one fast interval after its poll.
2. **Record at once and timestamp the fast tick when it records** — changes the meaning of every snapshot and history
   timestamp (`/healthz` staleness, alerts) for the sake of one series.
3. **Record at once with the recording time** — the fast tick's older timestamp still lands after it; the race stays.

## Decision

A finished openHAB poll hands its values to the collector, which stamps them with the time it reads while holding
its lock (that time becomes the poll's `collected_at`); they wait until the next fast tick records them in the same
locked update as its own points (option 1). A value stamped before it is handed over could be older than a tick that
already drained, which is why the time is taken under the lock. The rules are in
[History](../areas/history.md#recording).

## Consequences

- openHAB points reach the API at most one fast interval after the poll; a poll finished just before shutdown may be
  lost, like the points since the last history write.
- The waiting samples are bounded: a fast tick drains them all, and the collector keeps at most
  `OpenHAB.HistoryCapacity()` of them, dropping the oldest, so a delayed tick cannot grow them further.
- `collected_at` is the hand-over time, a moment after the poll's last request, not the end of `poll` itself.
- Within one tick's batch an openHAB point may be newer than the tick's own points; clients take the newest `t` of
  all series, as `API.md` says.
