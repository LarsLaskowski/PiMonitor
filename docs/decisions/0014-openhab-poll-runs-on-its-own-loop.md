# 0014: The openHAB poll runs on its own loop and serves its last finished result

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** openHAB
- **Source:** Issue #181
- **Supersedes:** —

## Context

The fast and the slow tick run one at a time on the collector's single loop, so a slow tick delays the fast tick by
as long as it runs ([Metrics](../areas/metrics.md#collection-cadence)), and `/healthz` reports unhealthy once the
snapshot is older than its bound. One openHAB poll makes up to five HTTP calls of up to 5 s each, so a hung
openHAB holds a poll for about 25 s. Until the first poll has finished there is nothing to report, and a guessed
`not_installed` would hide the dashboard card after every restart and mislead integrations.

## Options considered

1. **Poll openHAB in the slow tick** — no new loop, but a hung openHAB delays the fast tick by up to 25 s and
   ties the openHAB cadence to `updates_check_minutes`.
2. **A third ticker on the collector's loop** — same delay as option 1.
3. **Its own loop, started and joined by the collector, serving the result of the last finished poll; state
   `pending` before the first one** — chosen.
4. **Answer `503` before the first poll** — integrations and the dashboard would treat a normal startup as an
   error.

## Decision

The openHAB poll runs on a loop of its own with its own interval, started by `Collector.Run` and stopped with it;
`GET /api/v1/openhab` serves the last finished poll and `pending` before the first. Cadence and states are in
[openHAB](../areas/openhab.md).

## Consequences

- No openHAB failure can delay the fast or slow tick or `/healthz`.
- The collector now runs two loops; the openHAB loop reads the latest memory total from the snapshot, under the
  snapshot's lock.
- A dashboard opened right after a restart shows the card only after its next openHAB poll.
