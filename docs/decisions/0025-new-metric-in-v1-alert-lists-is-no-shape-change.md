# 0025: A new metric in the v1 alert lists is no shape change

- **Status:** Accepted
- **Date:** 2026-11-18
- **Area:** HTTP API
- **Source:** Issue #184
- **Supersedes:** —

## Context

`GET /api/v1/alerts` lists one entry per evaluated metric in `states` and one per transition in `events`, and
`API.md` names the metrics (`cpu`, `temperature`, `memory`, `swap`, `disk`); the webhook payload's `metric` names the
same set. Issue #184, written by the repository owner, adds `openhab_down`, `openhab_rss_percent` and
`openhab_things_offline` to these lists in v1. [0018](0018-new-key-in-v1-response-is-no-shape-change.md) covers a new
key in an object, not a new value of an existing key. `API.md` already tells clients that a metric may be absent.

## Options considered

1. **A new `metric` value, with its own entries in `states` and `events`, stays in v1** — existing entries keep their
   keys, types and meaning; a client that reads the metrics it knows is unaffected; a client that rejects unknown
   values breaks, which no known client does.
2. **Serve the openHAB alerts under `/api/v2/alerts`** — the dashboard and every integration would follow a fork for
   three entries.
3. **Serve them in a separate v1 endpoint** — keeps the lists closed, but splits one alert engine over two responses
   and leaves the webhook payload's `metric` open anyway.

## Decision

A new metric of the alert engine adds entries with a new `metric` value to `states` and `events` of
`GET /api/v1/alerts` and to the webhook payloads; this is no shape change, provided `API.md` and
[Notifications](../areas/notifications.md) list the value (option 1). The rule is stated in
[HTTP API](../areas/http-api.md#versioning-and-stability).

## Consequences

- Clients must ignore `metric` values they do not know; `API.md` says so.
- Changing what an existing `metric` value measures stays a shape change.
