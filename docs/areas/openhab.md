# openHAB

## Scope

The read-only status of an openHAB installation on the same Pi: how PiMonitor detects it, the states it reports, the
values and where each comes from, the poll cadence, the limits and the loopback guard on its requests, logging and
the handling of the optional API token. Operators rely on it to see openHAB's health next to the Pi's own metrics;
integrations read it from `GET /api/v1/openhab`.

## Behavior

### Detection and states

- Detection runs on every openHAB poll, so installing, starting or stopping openHAB is picked up without a restart.
  openHAB counts as present when one of `/usr/share/openhab`, `/etc/openhab` or `/var/lib/openhab` exists or its
  Java process runs.
- The openHAB process is a process whose command line starts with a `java` binary (the base name of the first
  argument is `java`) and has an argument starting with `-Dopenhab.home=`. Other processes that mention openHAB (the
  `karaf` start script, an editor on `/etc/openhab/…`) are not taken for it. When several match, the one with the
  lowest PID is used. A process that vanishes while it is read, or whose files are hidden (`hidepid=`), counts as
  not found.
- The state is exactly one of:

| State | Meaning |
| ----- | ------- |
| `pending` | No poll has finished since startup; no other value is reported. |
| `not_installed` | `openhab.enabled` is `auto` and neither an install path nor the process was found. Nothing else runs: no request is sent and nothing is logged. |
| `ok` | The root call answered as openHAB (HTTP 200, a JSON object with a non-empty `runtimeInfo.version`). |
| `running_unreachable` | The process was found but the root call failed. |
| `installed_not_running` | No process was found and the root call failed. |

- With `openhab.enabled: true` the state is never `not_installed`. With `false` there is no route, no detection and
  no request ([HTTP API](http-api.md)).
- A page answering `base_url` that is not openHAB (HTML, another JSON service, PiMonitor itself) fails the root call.
  It is never reported as values.

### Sources and values

Each source is collected on its own; a failing source leaves only its values absent (omitted, never `null` or a made-up `0`).

| Source | Values | Notes |
| ------ | ------ | ----- |
| Process | PID, resident memory in bytes, its percentage of the total RAM of the latest metrics snapshot | Reported whenever the process is found, whatever the REST calls do. The percentage is absent when the total RAM is 0. |
| `GET /rest/` | `version` | Decides `ok`; when it fails the other REST calls are skipped. |
| `GET /rest/systeminfo` | start level, JVM heap used (total minus free) and total | Heap used is absent when free exceeds total. openHAB reports no heap maximum. |
| `GET /rest/things?summary=true` | Things total, the count per Thing status, the Things that are not ONLINE | Only the statuses `UNINITIALIZED`, `INITIALIZING`, `UNKNOWN`, `ONLINE`, `OFFLINE`, `REMOVING` and `REMOVED` are counted per status; the total counts every Thing. |
| `GET /rest/items?fields=name` | number of Items | Entries are counted whatever they hold; no Item state is read, stored or served. |
| `GET /rest/addons` | number of installed add-ons | Entries with `installed: true`. |

- **Uptime** is the time since the process started (`/proc/uptime` minus the start time), in whole seconds. openHAB's
  own `systemInfo.uptime`, also in seconds, is used only when no process was found.
- **Not-ONLINE list.** Up to 10 Things whose status is not `ONLINE`, sorted by UID, each with UID, label, status,
  status detail and, when openHAB gives one, its description. A disabled Thing (status `UNINITIALIZED`, detail
  `DISABLED`) is counted under its status but neither listed nor a reason for the card's warning state.
- **Refused sources.** A source answered with `401` or `403` is named in `refused` (`root`, `systeminfo`, `things`,
  `items`, `addons`, in the order of the calls). On a default openHAB 5.2.4 install without an API token, system
  info, Things and add-ons are refused; the root call and the Item list answer.
- `collected_at` is the time the poll finished; it is reported in every state except `pending` and `not_installed`.
- The parsers follow real openHAB 5.2.4 responses; every field is optional, so a release that differs shows the
  affected values as absent ([0015](../decisions/0015-openhab-values-from-real-5x-responses.md)).

### Cadence

- The first poll runs at startup, after the first fast metrics tick, and then every `openhab.poll_interval_seconds`
  (default 60, 60 to 86400). It runs on a loop of its own and never delays the fast or the slow tick or `/healthz`;
  the endpoint serves the result of the last finished poll
  ([0014](../decisions/0014-openhab-poll-runs-on-its-own-loop.md)).
- One poll makes its requests one after the other. A poll that is still running at shutdown is cancelled.

### Requests and the loopback guard

- Requests go only to `openhab.base_url`, over plain `http`. The URL must have a loopback host (an address in
  `127.0.0.0/8`, `::1` or `localhost`) and no user info, path, query or fragment ([Configuration](configuration.md)).
- Every connection is checked again when it is made: an address that is not loopback is refused before connecting
  (a `localhost` that `/etc/hosts` maps elsewhere fails the call). No proxy is used.
- Each request is abandoned after 5 seconds. At most 8 MiB of a response (after decompression) is read; a longer
  response fails that call. A redirect is not followed and counts as a failed call.
- A response that is malformed, of an unexpected shape or not `200` fails only that call.

### API token

- `openhab.api_token` or `PIMONITOR_OPENHAB_TOKEN` (which wins when non-empty; [Configuration](configuration.md)) is
  sent as `Authorization: Bearer <token>`, only when set, only to `base_url`.
- With a token set, a connection is used only when its server side runs as the owner of `/var/lib/openhab`, which the
  kernel lists for that very connection in `/proc/net/tcp` or `/proc/net/tcp6`. Otherwise the connection is closed
  before a byte is sent and the call fails with `openHAB server on <host:port> does not run as the owner of
  /var/lib/openhab` (or the lookup error). This keeps another local user, who binds openHAB's port while openHAB is
  down, from receiving the token. Without `/var/lib/openhab`, and on platforms other than Linux, the token is never
  sent. Without a token nothing is checked ([0013](../decisions/0013-openhab-requests-only-to-loopback-http.md)).
- The token never appears in a log line, in `GET /api/v1/openhab` (only `api_token_configured`), in
  `GET /api/v1/config` or in any other response.

### Logging

- Each source logs per the log-once rule of [0001](../decisions/0001-optional-sources-log-failures-once.md): a
  warning on its first failure and when its error text changes, debug while the same error repeats, info on
  recovery. The messages are `openhab process`, `openhab root`, `openhab systeminfo`, `openhab things`,
  `openhab items` and `openhab addons`. A log line carries the HTTP status or the transport error, never a response
  body or header.
- A source refused with `401` or `403` while no token is configured is logged at info instead of warning, as it is the
  expected state of a default install ([0016](../decisions/0016-refused-openhab-call-without-token-logs-info.md)).
- When openHAB is present but no process is found, `openhab process` logs `no openHAB process found` the same way.
- While the state is `not_installed` nothing is logged.

## Limits and errors

- Invalid options fail startup with an error that names the option and never echoes the URL or the token
  ([Configuration](configuration.md)).
- Unreachable, slow, hostile or malformed openHAB answers never fail PiMonitor's own collection; they leave values
  absent and are logged as above.
- With an API token set, `GET /api/v1/openhab` serves Thing UIDs, labels and status descriptions that openHAB itself
  serves only to authenticated users; the endpoint is open to everyone who can reach PiMonitor unless `api_key` is set.

## Related decisions

- [`decisions/0013-openhab-requests-only-to-loopback-http.md`](../decisions/0013-openhab-requests-only-to-loopback-http.md) — loopback `http` only, checked again at connect time, token sent only to openHAB's account.
- [`decisions/0014-openhab-poll-runs-on-its-own-loop.md`](../decisions/0014-openhab-poll-runs-on-its-own-loop.md) — the poll has its own loop and a `pending` state before the first result.
- [`decisions/0015-openhab-values-from-real-5x-responses.md`](../decisions/0015-openhab-values-from-real-5x-responses.md) — parsers from real 5.2.4 responses; uptime from the process.
- [`decisions/0016-refused-openhab-call-without-token-logs-info.md`](../decisions/0016-refused-openhab-call-without-token-logs-info.md) — a refusal without a token logs at info.
- [`decisions/0001-optional-sources-log-failures-once.md`](../decisions/0001-optional-sources-log-failures-once.md) — the log-once rule of every optional source.

## Not here

- The response shape: [`API.md`](../API.md). The route, API key and headers: [HTTP API](http-api.md).
- The card, its texts and polling in the browser: [Dashboard](dashboard.md).
- Option ranges and the environment variable's precedence: [Configuration](configuration.md).
- Using PiMonitor values inside openHAB: [`integrations/openhab.md`](../integrations/openhab.md).
