# HTTP API

## Scope

The HTTP server PiMonitor runs on `listen_addr`: which routes exist and under which conditions, the rules every
response follows (authentication, compression, caching, concurrency limit, security headers), the liveness probe,
the Prometheus text endpoint, the self-traffic counters, error responses, timeouts and optional TLS. Third-party
integrations (openHAB, Home Assistant, Prometheus, uptime monitors), the bundled [Dashboard](dashboard.md) and the
operator rely on it. The JSON shape of each endpoint is the contract in [`API.md`](../API.md) and is not repeated
here.

## Behavior

### Versioning and stability

- Every JSON endpoint lives under `/api/v1/...`. The JSON shape of an existing `/api/v1/...` response is not changed
  in place: a change that would break it is published under a new `/api/v2/...` path, and `v1` keeps answering as
  before.
- Adding a key to an object of an existing `/api/v1/...` response, with its own documented presence rule, is no shape
  change and stays in `v1` (`GET /api/v1/metrics/history` gained `openhab_*` keys, present only while their series
  have points; `GET /api/v1/openhab` gained `history_window_hours`;
  [0018](../decisions/0018-new-key-in-v1-response-is-no-shape-change.md)). Removing, renaming or retyping a key,
  changing its meaning or when it is present, or changing the status code of a documented response is a shape change
  and needs `/api/v2/...`; a `500` for a response that cannot be encoded is none
  ([0022](../decisions/0022-unencodable-v1-response-answers-500.md)).
  Clients must ignore keys they do not know.
- A new metric of the alert engine adds entries with a new `metric` value to `states` and `events` of
  `GET /api/v1/alerts` and to the webhook payloads; this is no shape change, provided `API.md` and
  [Notifications](notifications.md) list the value (`openhab_down`, `openhab_rss_percent` and
  `openhab_things_offline` were added this way). Clients must ignore `metric` values they do not know; changing what an
  existing value measures is a shape change
  ([0025](../decisions/0025-new-metric-in-v1-alert-lists-is-no-shape-change.md)).
- Optional query parameters and new endpoints are added to `v1` only when they are additive: a request that does
  not use them gets exactly the response it got before (`?since=` on the history endpoint, the per-metric
  sub-resources).
- A per-metric sub-resource `GET /api/v1/metrics/<name>` returns exactly the value of the field `<name>` of
  `GET /api/v1/metrics` — the same JSON, sliced out, with no wrapper and no shape of its own. A field that carries no
  data is answered with `200` and that field's empty/zero value, never with `404`.
- `GET /healthz` and `GET /metrics` are not versioned: the first is plain text, the second is the Prometheus text
  exposition format.

### Routes

| Route | Registered | Behind the API chain (key, gzip, no-store, in-flight limit) |
| ----- | ---------- | ------------------------------------------------------------ |
| `GET /healthz` | always | no |
| `GET /api/v1/metrics` | always | yes |
| `GET /api/v1/metrics/cpu`, `/temperature`, `/memory`, `/disks`, `/network`, `/updates`, `/pihole` | always | yes |
| `GET /api/v1/metrics/history` | always | yes |
| `GET /api/v1/alerts` | always | yes |
| `GET /api/v1/config` | always | yes |
| `GET /api/v1/serverstats` | always | yes |
| `GET /api/v1/processes` | only when `processes_enabled` is true | yes |
| `GET /api/v1/openhab` | only when `openhab.enabled` is not `false` | yes |
| `GET /metrics` | only when `prometheus_enabled` is true | yes |
| `GET /` and every other path | always (the embedded dashboard assets) | no |

- Every route answers `GET` and `HEAD`. Paths are matched exactly; a sub-path or a trailing slash that is not in
  the table (`/api/v1/metrics/cpu/overall`, `/api/v1/metrics/`) is not an API route.
- Every other method on a registered route gets `405` with `Allow: GET, HEAD` and the plain-text body
  `method not allowed`, through the route's own layer: on an API-chain route after the key check and the concurrency
  limit, on `/healthz` without either. The request body is never read
  ([0030](../decisions/0030-wrong-method-answers-405-in-v1.md)).
- A route whose option is off is not registered at all: a request to it gets `404`, not an empty response.
- Any path that is not an API route falls through to the dashboard asset handler, which answers `404` for a path
  that is not an asset ([Dashboard](dashboard.md), *Asset serving*). Such a request is not API-key gated.
- `GET /api/v1/config` serves only non-sensitive values for clients: the binary's version string (`dev` for an
  unversioned build), `poll_interval_seconds`, `history_window_minutes`, `network_enabled` and the `thresholds`
  block ([`API.md`](../API.md#get-apiv1config)). It never contains `api_key`, TLS file paths, webhook settings or any
  other secret.
- `GET /api/v1/metrics/history?since=<RFC 3339 timestamp>` returns only points strictly newer than `since`, in the
  same shape as the full window; without `since` it returns the full retained window. Which points are retained is
  [History](history.md); the rules for `since` values are in [`API.md`](../API.md#incremental-polling-since).

### Authentication

- When `api_key` is empty, no route requires authentication. When it is set (from the config file, the
  `PIMONITOR_API_KEY` environment variable or the `-api-key` flag; precedence in
  [Configuration](configuration.md)), every route behind the API chain requires the key; `/healthz` and the
  dashboard assets never do.
- The key is accepted in either header: `X-Api-Key: <key>` or `Authorization: Bearer <key>`. When `X-Api-Key` is
  present and non-empty it is the one checked, and `Authorization` is ignored. The scheme word must be exactly
  `Bearer` followed by one space; the key is compared byte for byte, with no trimming or case folding.
- The provided and the configured key are each reduced to a SHA-256 digest and the digests are compared in
  constant time, so response timing reveals neither key bytes nor the configured key's length.
- A missing or wrong key gets `401` with the plain-text body `unauthorized`. The response does not name which
  header was expected and never echoes the provided value.
- The key is never written to the log: the access log line carries only method, path, status and duration.

### Concurrency limit

- At most **16** requests may be in progress at once across all routes behind the API chain together; the limit
  is a fixed constant, not an option.
- A request beyond the limit is not queued: it gets `503` with `Retry-After: 1` and the plain-text body
  `server busy`.
- The limit is taken before the API key is checked, so requests without a valid key also count against it while
  they are answered.
- `/healthz` and the dashboard assets are never subject to the limit, so liveness checks and the dashboard shell
  keep answering while the API sheds load.

### Compression

- A response behind the API chain is gzip-compressed when the request's `Accept-Encoding` accepts `gzip`; it then
  carries `Content-Encoding: gzip`, `Vary: Accept-Encoding` and no `Content-Length`. The decoded body is identical
  to the identity response.
- `Accept-Encoding` is read as a q-value list: an explicit `gzip` entry decides (`gzip;q=0` in any of its zero forms
  is a refusal, even next to `*`); otherwise `*` with a non-zero quality accepts gzip. `x-gzip` is not treated as
  `gzip`. A quality that does not parse is ignored, so the entry counts as accepted.
- A client that does not accept gzip gets the identity response unchanged.
- The compression applies to `401` responses too; the `503` of the concurrency limit is never compressed.
- `/healthz` and the dashboard assets are never compressed by the server.

### Caching

- Every response behind the API chain — including `401` and `503` — carries `Cache-Control: no-store` and `Vary`
  values naming `Authorization` and `X-Api-Key` (plus `Accept-Encoding` when compressed), so a shared cache in
  front of PiMonitor neither stores a response nor hands one client's authorized response to another.
- `/healthz` sets no caching headers. The caching rules of the dashboard assets are in [Dashboard](dashboard.md).

### Security headers

Every response, on every route, including errors and `404`s, carries:

- `Content-Security-Policy: default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self';
  frame-ancestors 'none'; base-uri 'none'; form-action 'self'; object-src 'none'` (sent as one line);
- `X-Content-Type-Options: nosniff`;
- `X-Frame-Options: DENY`;
- `Referrer-Policy: no-referrer`.

The policy allows no inline script or style and no third-party origin; dashboard code that needs either breaks
under it by design.

### Responses

- JSON endpoints answer `200` with `Content-Type: application/json; charset=utf-8`.
- Error responses are plain text (`Content-Type: text/plain; charset=utf-8`) with a fixed message; no error body
  contains request input or configuration values.

### Liveness: `/healthz`

- `GET /healthz` answers `200` with the plain-text body `ok` while the latest collected snapshot is no older than
  the staleness bound, and `503` with a plain-text message naming `healthz_max_staleness_seconds` once it is older.
  Until the first collection cycle after startup has completed, no snapshot has been collected and it answers `503`.
- The bound is `healthz_max_staleness_seconds`, or a value derived from the poll interval when that option is `0`
  (both in [Configuration](configuration.md)). In the running service the bound is always positive, so the
  staleness check is always active.
- `/healthz` is never API-key gated, never concurrency-limited and never compressed.

### Prometheus endpoint: `/metrics`

- Registered only when `prometheus_enabled` is true; it then behaves like every other route behind the API chain
  (key, gzip, no-store, the shared concurrency limit).
- It answers `200` with `Content-Type: text/plain; version=0.0.4; charset=utf-8`: the current snapshot followed by the
  openHAB status in the Prometheus text exposition format, every metric a gauge named `pimonitor_...`, each family preceded once by its
  `# HELP` and `# TYPE` lines.
- Per-device families carry one label (`core`, `zone`, `mount`, `iface`, `component`, `status` or `version`). In a label value, `\`, `"`
  and line feed are escaped as `\\`, `\"` and `\n`; everything else, non-ASCII text included, is passed through.
  Values are plain decimal numbers (never exponent notation), or `NaN`, `+Inf`, `-Inf`.
- The overall CPU usage is its own unlabeled family, never a member of the per-core family.
- A family whose source has no reading is left out entirely rather than reported as `0`: the temperature while the
  latest temperature collection has not succeeded, the GPU and PMIC temperatures without a `vcgencmd` answer,
  the CPU families (overall and per-core) while the latest fast tick has no CPU reading (the first tick after start,
  a failed `/proc/stat` read), the memory and swap families while the latest fast tick failed to read
  `/proc/meminfo`, per-core CPU without data, filesystem and network families when they hold no entry, the updates family while the latest apt check has not succeeded (`updates.check_status` is not `ok`), and the Pi-hole family when Pi-hole is not
  installed. The openHAB families are left out outside the states `ok`, `running_unreachable` and
  `installed_not_running`, each value family when its field is absent, and `pimonitor_openhab_info` when the version
  is empty or longer than 64 bytes; the Thing counts are the exception to "left out": all seven statuses are written,
  `0` included, whenever the Things source answered. Devices appear in the same order and with the same filtering as in `GET /api/v1/metrics`.
- The list of metric names and labels is in [`API.md`](../API.md#get-metrics-prometheus).

### Self-traffic counters: `/api/v1/serverstats`

- Every request the server answers, on every route and with every status, is counted once its response is
  complete: in a total, in its status class (`1xx` to `5xx`) and in exactly one route bucket. A call to
  `/api/v1/serverstats` therefore does not see itself; a later call does.
- The route buckets are fixed: one per path in the route table above — including `/metrics`,
  `/api/v1/processes` and `/api/v1/openhab` whether or not they are registered — plus `other-api` for any other path under `/api/v1/`
  and `static` for every other path. The bucket comes from the request path, not from the handler that answered,
  so a request to a disabled route is counted under its own path with a `4xx`. A client probing arbitrary paths
  cannot grow the set.
- Every bucket is present in the response from the start, with `0` until used.
- The counters live in memory only, start at zero on every start and are not persisted. They are read one by one,
  so under concurrent traffic the numbers in one response need not add up exactly.
- The counters are recorded whether or not `access_log_enabled` is set.

### Access log

- When `access_log_enabled` is true, every request produces one log line at debug level with method, path (without
  query string), status and duration; with `log_level` above debug the line is not emitted. Turning it off changes
  nothing else.

### Transport, timeouts and shutdown

- The server listens on `listen_addr`. With both `tls_cert` and `tls_key` set it serves HTTPS only, on that same
  address, with the certificate and key read once at startup; otherwise it serves plain HTTP. There is never a
  second listener and no redirect from HTTP to HTTPS. Setting only one of the two is a configuration error
  ([Configuration](configuration.md)).
- Connection timeouts: reading a request (headers and body) 10 s, writing a response 10 s, an idle keep-alive
  connection 60 s.
- On `SIGINT` or `SIGTERM` the server stops accepting connections and waits up to 10 s for requests in progress to
  finish.

## Limits and errors

| Situation | Status | Body / headers |
| --------- | ------ | -------------- |
| Missing or wrong API key on an API-chain route | `401` | `unauthorized` |
| More than 16 API-chain requests in progress | `503` | `server busy`, `Retry-After: 1` |
| `since` that is not an RFC 3339 timestamp | `400` | fixed message naming the format; never a full window |
| A JSON response could not be encoded (any `/api/v1/...` JSON endpoint) | `500` | `internal error`; the failure is logged at error level |
| No snapshot yet, or one older than the bound | `503` on `/healthz` | fixed message naming the option |
| A method other than `GET`/`HEAD` on a registered route | `405` | `method not allowed`, `Allow: GET, HEAD`; on an API-chain route only after `401`/`503` |
| Unknown path, or a route switched off by its option | `404` | from the dashboard asset handler |

- If the listen address cannot be bound, or the TLS certificate or key cannot be loaded, the process exits with a
  non-zero status and the error on standard error.
- If requests in progress have not finished within the 10 s shutdown grace period, the process exits with a
  non-zero status.
- There is no per-client rate limit and no request-size option; the concurrency limit is a resource bound, not an
  access control, and does not replace `api_key`.

## Related decisions

- [0030: A wrong method on a registered route answers 405 in v1](../decisions/0030-wrong-method-answers-405-in-v1.md)
- [0022: A v1 response that cannot be encoded answers 500 in v1](../decisions/0022-unencodable-v1-response-answers-500.md)
- [0025: A new metric in the v1 alert lists is no shape change](../decisions/0025-new-metric-in-v1-alert-lists-is-no-shape-change.md)
- [0017: openHAB Thing counts are exported to Prometheus as the fixed set of seven statuses](../decisions/0017-openhab-thing-statuses-exported-as-fixed-set.md)
- [0010: A missing CPU or memory reading is left out of Prometheus, the v1 snapshot keeps its 0](../decisions/0010-missing-reading-left-out-of-prometheus.md)

## Not here

- The JSON shape of each endpoint, field names, units and examples: [`API.md`](../API.md).
- Which metrics exist, their sources and units, and when they are empty: [Metrics](metrics.md).
- What the history window holds and how `since` slices it at the data level: [History](history.md).
- The meaning of `GET /api/v1/alerts`: [Alerts](alerts.md); of `GET /api/v1/openhab`: [openHAB](openhab.md).
- Defaults and valid ranges of `listen_addr`, `api_key`, `tls_cert`, `tls_key`, `access_log_enabled`,
  `healthz_max_staleness_seconds`, `prometheus_enabled`, `processes_enabled`, `openhab.*`, and where the key may come from:
  [Configuration](configuration.md).
- The embedded dashboard, its asset caching and its use of the API key: [Dashboard](dashboard.md).
- Protecting the config file that holds the key, and the service's privileges: [Deployment](deployment.md).
- Threat model and why the API is meant for a trusted LAN: [`SECURITY.md`](../../SECURITY.md).
- How API stability is handled in a pull request: [`CONTRIBUTING.md`](../CONTRIBUTING.md#rest-api-stability).

## Open questions

None.
