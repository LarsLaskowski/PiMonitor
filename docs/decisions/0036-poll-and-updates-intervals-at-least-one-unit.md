# 0036: The fast and slow collection intervals are at least one second and one minute

- **Status:** Accepted
- **Date:** 2026-11-28
- **Area:** Configuration
- **Source:** Issue #289
- **Supersedes:** —

## Context

`poll_interval_seconds` and `updates_check_minutes` were validated only as `> 0`. A positive value below 1 ns
converts to a zero `time.Duration`, which the collector's defensive clamp replaces by 1 s with a warning, and a
small positive value such as `0.001` runs the fast tick every millisecond, or the slow tick (apt check,
Pi-hole, processes and a `history.bin` write to the SD card) every 60 ms. The upper bounds of both keys were set in
[0035](0035-float-duration-options-capped-at-seven-or-365-days.md); a lower bound, like any bound, rejects some
configuration that starts today. The dashboard already never polls faster than once per second, and a CPU
reading from `/proc/stat` counts in 10 ms ticks, so a sub-second fast tick gives no better readings.

## Options considered

1. **1 second and 1 minute, one unit of each key** — chosen. Far below the defaults (5 s, 15 min), no tick runs
   more often than the dashboard reads it, and the number is the key's own unit, easy to state and remember.
2. **Only values that truncate to zero** (at least 1 ns) — fixes the zero duration but still runs `0.001` every
   millisecond, the case the issue names.
3. **0.1 second and 0.1 minute** — keeps sub-second fast ticks, which the dashboard does not show and which give
   coarse CPU readings, and an apt check and history write every 6 s.
4. **A runtime clamp to the minimum with a warning** — the service keeps running on a value it does not use,
   against the fail-fast validation at startup.
5. **The old `> 0` text for zero and below, a second text below the minimum** — two texts for one rule.

## Decision

`poll_interval_seconds` below 1 and `updates_check_minutes` below 1 fail validation at startup with an error that
names the key; the texts are in [Configuration](../areas/configuration.md#validation-and-startup-failure).

## Consequences

- A configuration with either key between 0 and 1, accepted until now, no longer starts.
- Zero and negative values get the new text instead of `must be > 0`.
- The collector's clamp of a non-positive interval stays as defense in depth for callers that skip validation.
- The ratio check of `history_window_minutes` and `poll_interval_seconds` stays: at 1 s, 525600 minutes still
  make 31,536,000 points.
