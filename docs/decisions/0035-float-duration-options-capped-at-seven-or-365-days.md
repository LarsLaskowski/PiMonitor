# 0035: The other float seconds and minutes options are capped at seven or 365 days

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Configuration
- **Source:** Issue #287
- **Supersedes:** —

## Context

Eight number options are converted to a `time.Duration` by multiplying them with `time.Second` or `time.Minute`
and had only a lower bound: `poll_interval_seconds`, `updates_check_minutes`, `updates_stale_threshold_minutes`,
`healthz_max_staleness_seconds`, `history_window_minutes`, `alerts.notify_retry_backoff_seconds`,
`alerts.notify_min_interval_seconds` and `alerts.webhooks[].timeout_seconds`. A product above `math.MaxInt64`
nanoseconds (about 292 years) converts to a platform-dependent value, negative on amd64, which silently turns the
option into something else: a tick interval clamped to 1 s, a `/healthz` check or stale flag that never fires, a
history that is never trimmed, a rate limit that never applies, retries without delay. A `poll_interval_seconds`
above about 24.8 days also makes the dashboard's `setInterval` delay leave the 32-bit range browsers store. The
alert debounce windows got their bound in [0034](0034-alert-debounce-windows-capped-at-seven-days.md); any bound
rejects some configuration that starts today, so it should sit far above every realistic value.

## Options considered

1. **Two bounds by kind: 7 days for how often or how long PiMonitor waits, 365 days for how old or how far back** —
   chosen. Intervals, delays, timeouts and the rate-limit window (`poll_interval_seconds`,
   `updates_check_minutes`, the three notifier options) share the 7 days of 0034; the ages and windows
   (`updates_stale_threshold_minutes`, `healthz_max_staleness_seconds`, `history_window_minutes`) get 365 days,
   because a history window of a few weeks at a slow poll is a real configuration that 7 days would reject. Every
   product at these bounds, and the derived `/healthz` bound (3 × 7 days), fits a `time.Duration` with a wide
   margin.
2. **7 days for all eight keys** — one number, but it rejects history windows and stale thresholds above a week
   that start and work today.
3. **A realistic bound per key** (a webhook timeout of minutes, a poll interval of a day) — tighter, but each
   number is a guess, more configurations break, and the defect is the overflow, not the size.
4. **Only the overflow limit** — fixes the defect with bounds (about 9.2e9 s, 1.5e8 min) and error texts that mean
   nothing to an operator, and leaves the dashboard timer defect.

## Decision

Each of the eight keys has an upper bound — 604800 s or 10080 min for the waits, 31536000 s or 525600 min for the ages
and windows — checked right after the finiteness check, so a value above it fails startup with
`<key> must be <= <bound> (got <value>)` before any other range check; the lower-bound texts are unchanged. The
bounds are in [Configuration](../areas/configuration.md#validation-and-startup-failure).

## Consequences

- A configuration with one of the keys above its bound, accepted until now, no longer starts.
- The ratio check of `history_window_minutes` and `poll_interval_seconds` stays: a tiny poll interval still makes
  the ratio huge.
- A larger bound needs a new record and a check that every derived duration still fits: the `/healthz` default
  (3 × `poll_interval_seconds` + 34 s), and the doubling retry backoff, which at 7 days wraps only after more than
  300 years of waiting.
