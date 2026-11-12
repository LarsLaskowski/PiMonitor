# 0005: A cleared event ends the webhook rate-limit window

- **Status:** Proposed
- **Date:** 2026-11-12
- **Area:** Notifications
- **Source:** Issue #199
- **Supersedes:** —

## Context

The notifier rate-limits deliveries per metric and resource, per webhook, with `alerts.notify_min_interval_seconds`
between event timestamps. Every successful delivery started the window, a delivered `cleared` event included. When
an alert cleared and fired again within the interval, that `fired` event was dropped, and a state-based consumer
(a Home Assistant `binary_sensor`) kept showing "ok" while the alert was active again. `.squad/project.md`
(*Guarantees*) already lets `cleared` events bypass the limiter for the same reason: a consumer must not get stuck
in a state the alert has left. The issue settles that only delivered `fired` events start the window and that a
`fired` event after a `cleared` event is always delivered.

## Options considered

1. **A `cleared` event that reaches the webhook's worker ends the window of its metric and resource, whatever its
   delivery outcome; only delivered `fired` events start one** — every state change a consumer can observe is
   sent, also after a `cleared` delivery that failed; the limiter still coalesces consecutive `fired` events.
2. **Only stop recording delivered `cleared` events** — the smallest edit, but a `fired` event after a `cleared`
   one still falls into the window of the `fired` event before it (fired, cleared, fired within the interval), so
   the consumer keeps showing "ok": the defect stays.
3. **End the window only when the `cleared` event was delivered** — matches "a delivered cleared" literally, but
   after a failed `cleared` delivery the next `fired` event is still dropped, which contradicts "always delivered"
   and keeps the limiter's state depending on the outcome of an event it never limits.

## Decision

A `cleared` event that reaches a webhook's worker removes the rate-limit window of its metric and resource for that
webhook, and only a delivered `fired` event starts a window (option 1). The rules are in
[Notifications](../areas/notifications.md#rate-limit).

## Consequences

- The limiter no longer bounds an alert that flaps between `fired` and `cleared`: each such pair is delivered, and
  the event rate is bounded only by the debounce (`alerts.for_seconds`) and the poll interval. What it still
  coalesces is a `fired` event that follows another `fired` event without a `cleared` one between them (`ok` →
  `warn` → `crit`).
- A `cleared` event dropped because the webhook's queue was full never reaches the worker and ends no window.
- Revisiting this to damp flapping again means a limiter on `fired`/`cleared` pairs, which would have to keep the
  consumer's last observed state consistent — the problem this record removes.
