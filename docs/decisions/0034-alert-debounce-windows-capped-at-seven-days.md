# 0034: Both alert debounce windows are capped at seven days

- **Status:** Accepted
- **Date:** 2026-11-28
- **Area:** Configuration
- **Source:** Issue #272
- **Supersedes:** —

## Context

`alerts.for_seconds` and `alerts.openhab_for_seconds` were checked only for finiteness and `>= 0`, then multiplied
into a `time.Duration`. A product above `math.MaxInt64` nanoseconds (about 292 years) converts to a
platform-dependent value; on amd64 it is negative, and the engine's `now.Sub(since) >= window` holds on the first
sample, so a typo such as `1e12` silently turns the debounce off. Any bound rejects some configuration that starts
today. The Product Manager chose the bound in the issue's spec.

## Options considered

1. **604800 s (7 days) for both keys** — chosen. Far below the overflow; above the largest
   `openhab.poll_interval_seconds` (86400), so the openHAB window can still span several polls; equal to the largest
   `openhab.history_window_hours` (168 h).
2. **86400 s (1 day) for both keys** — stricter, but at the largest openHAB poll interval the openHAB window covers a
   single poll.
3. **Only the overflow limit (about 9.2e9 s)** — fixes the defect, but the bound and its error text mean nothing to an
   operator.
4. **A bound tied to `openhab.poll_interval_seconds`** — couples two options and their error texts for no case the
   fixed bound fails.

## Decision

Both keys must be at most 604800; a larger value fails startup with `<key> must be <= 604800 (got <value>)`, after
the finiteness and `>= 0` checks, whose texts are unchanged. The rules are in
[Configuration](../areas/configuration.md#alerts).

## Consequences

- A configuration with either key above 7 days, accepted until now, no longer starts.
- The other float seconds and minutes options got their bounds in a decision of their own
  ([0035](0035-float-duration-options-capped-at-seven-or-365-days.md)).
- A longer window would need a new record and a check that the larger product still fits a `time.Duration`.
