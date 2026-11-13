# Notifications

## Scope

Delivery of alert transition events to HTTP webhooks: which events reach which webhook, the request each delivery
sends (default JSON payload or a custom template), queueing, retries, rate limiting, timeouts and what is logged.
Operators rely on it to push alerts to Slack, Discord, Home Assistant, ntfy or any endpoint that accepts an HTTP
POST; the collector relies on it never to slow down metric collection.

## Behavior

### When notifications are active

- Webhooks are configured as a list under `alerts.webhooks`; each entry has `url`, `min_level`, `template`,
  `content_type` and `timeout_seconds`. Retry and rate-limit settings (`alerts.notify_max_retries`,
  `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds`) apply to all webhooks. Defaults and
  valid ranges of all of them are in [Configuration](configuration.md).
- Notifications are active only when at least one webhook is configured **and** `alerts.enabled` is true. With
  webhooks configured but `alerts.enabled: false`, a warning is logged once at startup and nothing is ever sent.
- The events delivered are exactly the `fired` and `cleared` events the alert engine produces, including the final
  `cleared` event of a vanished filesystem ([Alerts](alerts.md)). There is no notification on startup, shutdown or
  for an alert that is still active, and nothing is resent after a restart.

### Severity filter

- Each webhook receives an event only when the higher of its two levels (`from`, `to`) reaches the webhook's
  `min_level`, with the order `ok` < `warn` < `crit`.
- With `min_level: warn` a webhook receives every event (every transition has a side at or above `warn`).
- With `min_level: crit` a webhook receives `ok` → `crit`, `warn` → `crit`, `crit` → `warn` and `crit` → `ok`, but
  not `ok` → `warn` or `warn` → `ok` — so it also receives the `cleared` event when a metric leaves `crit`.

### Queueing and ordering

- Each webhook has its own queue of **256** events and its own delivery worker. A slow, failing or unreachable
  webhook delays only its own deliveries, never another webhook's, and never metric collection.
- Handing events to the webhooks never blocks: when a webhook's queue is full, the event is dropped for that
  webhook only and a warning is logged.
- A webhook's events are delivered one at a time, in the order the alert engine produced them. An event waits
  until every earlier event of the same webhook has been delivered, rejected or given up.

### Request

- Each delivery is one HTTP `POST` to the webhook's `url` with the `Content-Type` header set to `content_type`.
  No other header can be configured, and no authentication is added.
- The response status decides the outcome (see *Retries*); the response body is ignored.
- HTTPS certificates are verified against the system's trust store; there is no option to skip verification. The
  standard proxy environment variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`) of the service are honored.
- Redirects are followed, and the status of the final response counts. On a `301`, `302` or `303` redirect the
  follow-up request is a `GET` without body; configure the final URL rather than one that redirects.

### Default payload

Without a `template`, the body is a JSON object with these fields:

| Field | Content |
| ----- | ------- |
| `metric` | `cpu`, `temperature`, `memory`, `swap` or `disk` |
| `resource` | the mountpoint for `disk`; omitted for the other metrics |
| `kind` | `fired` or `cleared` |
| `from`, `to` | the levels before and after: `ok`, `warn` or `crit` |
| `value` | the reading of the event, as a JSON number |
| `at` | the event time, an RFC 3339 timestamp with fractional seconds and the host's UTC offset |
| `message` | a one-line summary, see below |

`metric`, `resource`, `kind`, `from`, `to`, `value` and `at` carry the same values and encoding as the event in
`GET /api/v1/alerts` ([`../API.md`](../API.md#get-apiv1alerts)).

`message` is `<metric> <kind>: <from> -> <to> (<value>)`, with ` (<resource>)` after the metric when there is a
resource, and the value rounded to one decimal — for example `cpu fired: ok -> crit (98.0)` or
`disk (/data) cleared: crit -> ok (60.0)`.

### Custom templates

- A non-empty `template` replaces the default payload. It is written in Go `text/template` syntax and rendered once
  per event and webhook; its output is sent as the body unchanged, without any check that it is valid JSON (or
  valid anything).
- The template sees the fields `.Metric`, `.Resource` (empty string for non-disk metrics), `.Kind`, `.From`, `.To`,
  `.Value`, `.At` and `.Message`, with the meanings of the default payload above.
- The template language does no escaping. The function `json` renders its argument as one complete JSON value —
  a string including its surrounding quotes and with every special character escaped, a number as a number, a
  time as a quoted RFC 3339 string. A template whose body is JSON must wrap every interpolated value in `json` and
  must not put quotes around the action: `{"text": {{json .Message}}}`. To combine literal text with a value,
  build the whole string first and wrap that: `{"text": {{json (printf "PiMonitor: %s" .Message)}}}`.
- Set `content_type` to match a template that renders a non-JSON body (for example `text/plain`).
- The template is parsed at startup. A syntax error stops startup, also when `alerts.enabled` is false (see
  *Limits and errors*).

### Retries

- A delivery succeeds on any `2xx` response.
- These failures are retried: transport errors (name resolution, refused or reset connections, TLS errors,
  timeouts, a URL the client cannot use), `408`, `429`, every `5xx`, and any other non-`2xx` status below `400`.
- Every other `4xx` is a permanent rejection: the delivery is given up after that attempt, without using the retry
  budget.
- A delivery makes at most `alerts.notify_max_retries` + 1 attempts. Before the first retry it waits
  `alerts.notify_retry_backoff_seconds`, and the wait doubles before each further retry; a backoff of `0` retries
  immediately.
- Each attempt is bounded by the webhook's `timeout_seconds` (a value of `0` uses the built-in default, see
  [Configuration](configuration.md)). The longest one event can occupy a webhook is therefore
  (retries + 1) × timeout + backoff × (2^retries − 1).
- A delivery that is given up is not repeated later. Because a timed-out attempt may still have reached the
  endpoint, a retried event can arrive more than once.

### Rate limit

- The rate limit is kept per webhook and per `metric` + `resource` pair; different metrics and different
  mountpoints never limit each other, and one webhook's limit never affects another.
- A `fired` event is dropped (with a warning) when its `at` is less than `alerts.notify_min_interval_seconds` after
  the `at` of the last **successful** delivery of a `fired` event of the same metric and resource to the same
  webhook. An event exactly at the interval is delivered.
- `cleared` events are never rate-limited.
- A `cleared` event ends the window of its metric and resource, also when its delivery fails, so the next `fired`
  event is always delivered. What the limit still coalesces is a `fired` event after another `fired` event with no
  `cleared` event between them (`ok` to `warn` to `crit`); a flapping alert is bounded by the debounce only.
- Only successful deliveries of a `fired` event count: a rejected, given-up or unrendered delivery does not hold
  back the next event.
- The interval is measured between event timestamps, not between delivery times, so time spent queued or retrying
  does not change the decision.
- `alerts.notify_min_interval_seconds: 0` disables the rate limit.

### Shutdown

- On shutdown (SIGINT or SIGTERM) the attempt in flight and any backoff wait are cancelled, and events still queued
  are dropped. Delivery is best-effort: nothing is persisted and nothing is sent after a restart.

### What is logged

All lines go to the service log (see [Configuration](configuration.md) for `log_level`). A successful delivery is
not logged.

| Level | Message | Fields |
| ----- | ------- | ------ |
| warn | `alert webhooks configured but alerts.enabled is false — no notifications will be sent` (startup) | — |
| warn | `alert notification queue full, dropping event` | `url`, `metric`, `resource`, `kind` |
| warn | `alert notification rate-limited, dropping event` | `url`, `metric`, `resource`, `kind` |
| warn | `alert notification delivery failed, will retry` | `url`, `attempt`, `error` |
| error | `alert notification rejected, not retrying` | `url`, `attempts`, `error` |
| error | `alert notification giving up after retries` | `url`, `attempts`, `error` |
| error | `alert notification render failed` | `url`, `error` |

- The webhook URL is logged in full, and transport error texts repeat it. A URL that carries a secret in its path
  or query (Slack, Discord and ntfy tokens) therefore appears in the service log; protect the log accordingly.
- Neither the request body nor the response body is logged.

Implemented in `internal/alert/notify.go`.

## Limits and errors

- At startup a webhook needs a non-empty `url`, a valid `min_level` and a non-negative `timeout_seconds`
  ([Configuration](configuration.md)). Beyond that the URL is not checked: a malformed or unsupported URL is
  accepted and every delivery to it fails with a transport error, which is retried until the budget is used up.
- A `template` that does not parse stops startup with exit status 1; the error names
  `alerts.webhooks[<index>].template`.
- A template that fails when rendered for an event (for example a field that does not exist), or an event whose
  value cannot be encoded as JSON, is logged as `alert notification render failed`; the event is dropped for that
  webhook and not retried.
- A full queue, a rate-limited event, a permanent rejection and an exhausted retry budget each drop the event for
  that webhook only, with the log line listed above. No error ever reaches metric collection or the HTTP API.
- There is no API endpoint for notifications and no record of delivered or dropped events beyond the log.

## Related decisions

- [0005 A cleared event ends the rate-limit window](../decisions/0005-cleared-event-ends-rate-limit-window.md)

## Not here

- When an event fires or clears, debounce and alert state: [Alerts](alerts.md).
- Defaults and valid ranges of `alerts.webhooks[*].*` and `alerts.notify_*`, and config validation at startup:
  [Configuration](configuration.md).
- The JSON shape of events in `GET /api/v1/alerts`: [`../API.md`](../API.md#get-apiv1alerts).
- Service environment, log destination and how the service is stopped: [Deployment](deployment.md).
- Reporting security issues and handling of secrets in general: [`../../SECURITY.md`](../../SECURITY.md).

## Open questions

None.
