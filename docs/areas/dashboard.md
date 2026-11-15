# Dashboard

## Scope

The web UI served at `/`: how its assets are delivered, which API endpoints it reads and how often, how it handles
a configured API key, failures and hidden tabs, how it renders server data, colors values and shows alerts, and the
per-browser preferences (theme, card layout) it keeps. The user viewing the Pi in a browser relies on it; it is a
client of the [HTTP API](http-api.md) like any third-party integration and has no server-side state of its own.

## Behavior

### Asset serving

- The dashboard is a fixed set of static files (one HTML page, its scripts and one stylesheet) embedded in the
  binary; it changes only with the binary. There is no build step and no file is fetched from another origin.
- `GET /` serves the page; every asset is served at `/<file name>`. `/index.html` redirects to `./`, and a trailing
  slash after a file name redirects to the file. Any other path answers `404`.
- Every asset response carries `Cache-Control: no-cache`, so the browser revalidates on every load.
- An asset served with `200`, `206` or `304` carries a strong `ETag`: the first 16 bytes of the SHA-256 of that
  file's contents, hex-encoded, in double quotes. Each file has its own value, it changes exactly when the file's
  bytes change, and a matching `If-None-Match` gets `304`. Redirects, `404`s and other statuses carry no `ETag`.
- The assets are never API-key gated and never subject to the API's concurrency limit, so the page loads even when
  the API refuses it; the security headers of the [HTTP API](http-api.md#security-headers) apply to them.
  Implemented in `internal/web/embed.go`.

### Data sources and polling

- The dashboard reads five endpoints: `GET /api/v1/config`, `GET /api/v1/metrics`, `GET /api/v1/alerts`,
  `GET /api/v1/metrics/history` and `GET /api/v1/openhab`. It uses no other endpoint, and the browser never contacts
  openHAB itself.
- On page load it reads `/api/v1/config` once, then fetches metrics, history, alerts and openHAB once each in that
  order, and then starts the timers. `/api/v1/config` is read again only after an API key has been accepted; a
  server-side configuration change reaches an open page on the next reload.
- Metrics and alerts are polled every `poll_interval_seconds` (as reported by `/api/v1/config`), but never more
  often than once per second. History is polled every `poll_interval_seconds` or every 60 s, whichever is longer.
- Until `/api/v1/config` has answered, and whenever it fails, the dashboard uses built-in fallback values for every
  field of that response. The fallback holds the same threshold keys the server serves; a test keeps the two key
  sets identical.
- The openHAB poll is its own timer: it runs every `poll_interval_seconds` of the `/api/v1/openhab` response (at
  least 60 s), and at the metrics interval while the response's state is `pending`, so the card appears within
  seconds of the server's first finished poll. After an answer of `404` (`openhab.enabled: false`) it stops until the
  page is reloaded. An answer of `401` shows the "API key required" dialog; any other failure leaves the card as it
  was.
- Each of the four polls has at most one request outstanding: a timer tick that fires while the previous request
  of the same kind is still open is skipped, not queued.
- When the page becomes hidden, all timers stop and no further polls are sent. When it becomes visible
  again, metrics, history, alerts and openHAB are fetched immediately and the timers restart.

### History in the browser

- The first history request, and every request after a failure, fetches the full window. After that the dashboard
  asks only for newer points with `?since=`, passing back the `t` of the newest point it holds exactly as the
  server sent it (never re-formatted, which would lose sub-millisecond precision).
- A delta is appended only when every one of its points is strictly newer than the newest point held and its
  oldest point follows that newest point by no more than three poll intervals or 15 s, whichever is longer. A delta
  that fails either check, a timestamp that does not parse, or an empty local window makes the dashboard discard
  its window and fetch the full one instead.
- After 10 consecutive incremental history polls the next poll fetches the full window regardless, so a history
  that the server replaced rather than extended (a restart restoring a persisted window, a clock step) is picked up
  within eleven history polls.
- The local window is trimmed to `history_window_minutes`, measured back from the newest point held (the Pi's
  clock); a device left with no points is dropped.

### API key

- When a metrics, alerts or openHAB poll answers `401`, the dashboard shows the "API key required" dialog and the header
  shows "API key required". The dialog cannot be dismissed with Escape or by clicking outside it and has no close
  button.
- The entered key is trimmed of surrounding whitespace; an empty entry does nothing. The key is verified with
  `GET /api/v1/config` before it is kept: a `401` shows "Invalid API key", any other failure shows "Could not verify
  the key: connection error", and in both cases nothing is stored.
- An accepted key is stored in clear text in the browser's `localStorage` under `pimonitor-api-key` and also kept in
  memory, so it works for the current page even when storage is unavailable. The dashboard then reloads
  `/api/v1/config`, fetches all data again and restarts the timers with the reported interval.
- Every API request carries the key as `X-Api-Key` when one is known; the stored key takes precedence over the
  in-memory one. The dashboard never sends the key in another header or in a URL.
- The dashboard has no control to remove a stored key. A key that stops working (rotated on the server) leads to a
  `401` and the dialog, and the newly accepted key replaces it. Why clear-text browser storage is accepted is in
  [`SECURITY.md`](../../SECURITY.md#threat-model--design-notes).

### Failures

- A failed metrics poll (other than `401`) shows "Connection error" in the header and leaves every card showing its
  last values; the next scheduled poll tries again. A `503` from the API's concurrency limit is handled the same
  way; `Retry-After` is not read.
- A failed metrics poll and a `401` leave the host name and the tab title as they were.
- A failed alerts poll leaves the badges and the banner as they were: a failed request is not taken as "cleared".
- A failed history poll (including a `401`) leaves the charts as they were and makes the next poll fetch the full
  window.

### Rendering

- Text that comes from the server (mountpoints, interface names, package names and versions, Pi-hole versions,
  host name, kernel, distribution, model, openHAB's version and its Thing labels, UIDs and status texts) is inserted only as text nodes; the host name also goes to `document.title`
  and the `title` property of its header element. The page script never assigns non-empty content to `innerHTML`;
  clearing a container with an empty string is the only allowed use. `internal/web/xss_test.go` fails the build
  otherwise.
- The host name, when the snapshot carries `system.hostname`, is shown in parentheses after the brand in the header,
  muted and in normal weight, in its original case; a long name is cut with an ellipsis and the full name is its
  tooltip, while the closing parenthesis stays visible.
- The tab title is `<host name> – PiMonitor`, or `PiMonitor` without a host name.
- `Hostname` is the first row of the System card and is hidden without a host name. Header, title and row are
  evaluated on every snapshot.
- The page uses no inline script or style, so it runs under the strict Content-Security-Policy of the
  [HTTP API](http-api.md#security-headers).
- Times shown on the page ("Last updated", "Raspberry Pi time", "Checked …") are instants taken from the Pi
  (snapshot and check timestamps), displayed in the browser's locale and time zone; ages and history ranges are
  computed between Pi timestamps, so a skewed browser clock does not change them.
- The footer shows the version string from `/api/v1/config`; a leading `v` directly followed by a digit is dropped,
  any other value (such as `dev`) is shown as-is. The footer also links to `/api/v1/metrics`.
- The temperature card shows `n/a` when the snapshot names no thermal zone, instead of a `0` reading. GPU and PMIC
  temperatures are shown only when the snapshot carries them.
- The openHAB card is shown exactly while the latest `GET /api/v1/openhab` answered `200` with a state other than
  `not_installed` or `pending` and its layout preference is visible; it is hidden after a `404`. It shows the
  process memory (absolute and as a percentage of RAM), the JVM heap used and total with a bar, the uptime, the
  number of Items and of add-ons, the Things by status (ONLINE first, the others by name) and the version, with
  `n/a` for an absent value. A value of a source the server lists as refused reads `needs api_token` (no token
  configured) or `api_token refused`. The state line reads `Running · <version>`, `Starting (start level N)` while
  the start level is below 100, `Not running` or `Not reachable`; when the server lists the root call as refused
  (a token openHAB rejects makes it refuse the root call too) the hint is appended, for example
  `Not reachable · api_token refused`. The card is in its warning state (warning border, state line colored `warn`)
  when the state is `installed_not_running` or `running_unreachable` or a Thing is listed as not ONLINE. The Things
  the server lists as not ONLINE (at most 10, disabled ones left out) are counted in a toggle,
  `Not-online Things (<n>)`, that is closed by default and holds only their labels (the UID when a Thing has no
  label); no status, status detail or description is shown. The
  toggle's state is kept per browser and survives the card's refreshes. Values of [openHAB](openhab.md).
- The network card is shown only when its layout preference is visible, `network_enabled` is true in
  `/api/v1/config` and the snapshot holds at least one interface; all three must hold.
- The Pi-hole section of the Updates card is shown only when the snapshot reports Pi-hole as installed. A Pi-hole
  update is shown separately from the apt update count, never added to it.
- The Updates card shows the apt update count, when it was checked and a stale marker when the server reports the
  cache as stale; its button opens a list of the upgradable packages with current and new version, kept up to date
  while open.
- An empty filesystem list shows "No data".

### Value coloring

- CPU usage, temperature, memory, swap and each filesystem are colored `crit` when the value is greater than or
  equal to the matching `*_crit` threshold from `/api/v1/config`, otherwise `warn` when greater than or equal to the
  `*_warn` threshold, otherwise `ok` — the same comparison the alert engine uses.
- The load-average gauges (1, 5 and 15 min) run from 0 to the CPU core count and are colored against the CPU
  thresholds scaled to the core count (`cores × cpu_warn_percent / 100`, `cores × cpu_crit_percent / 100`).
- Coloring follows the raw value of every poll and is independent of alert state: a value can be colored `crit`
  before its alert fires, because alerts are debounced ([Alerts](alerts.md)).

### Alerts

- When `GET /api/v1/alerts` reports alerting as disabled, no badge and no banner is shown.
- Otherwise every state whose level is not `ok` lights a badge with its level on the card it belongs to: `cpu` on
  CPU Usage, `temperature` on Temperature, `memory` and `swap` on Memory & Swap, every `disk` state on Filesystems.
  A card shows the worst level of its states, and the level is also added to the card button's accessible name.
- A banner lists every metric with a non-`ok` state and its worst level, worst first, and takes the color of the
  worst level. With no such metric the banner is hidden.
- Badges and banner are recomputed from the latest report on every alerts poll, so a cleared condition disappears
  on the first poll that reports it `ok` or no longer reports it.

### Detail view

- Clicking the header of the CPU Usage, Load Average, Temperature or Memory & Swap card opens a larger chart of the
  CPU usage, 1-minute load, temperature or memory-used history.
- The range is 5 min, 15 min or 1 h, 15 min when first opened, measured back from the newest point (the Pi's clock)
  and limited to what the browser holds. Below the chart the current, minimum, average and maximum value and the
  sample count are shown; with fewer than two points no line is drawn and the view says so.
- An open detail view updates with every history poll.
- All dialogs (detail, updates list, layout, API key) are modal; focus moves into the dialog on open and back to the
  element that opened it on close. Except for the API key dialog, Escape, a click outside the dialog or its close
  button closes it.

### Theme

- The page has a light and a dark palette. With no stored choice it follows the operating system's
  `prefers-color-scheme` and follows live changes of it.
- The theme button switches to the other palette and stores the choice under `pimonitor-theme` (`light` or `dark`)
  in `localStorage`; from then on the stored choice wins over the operating system setting. The page offers no
  control to return to following the operating system.
- A stored choice is applied before the first paint, so the page never flashes the other palette. Charts and gauges
  are redrawn in the new palette immediately on a switch.

### Card layout

- The cards are System, Updates, Uptime, CPU Usage, Load Average, Temperature, Memory & Swap, Filesystems,
  Network and openHAB, in that default order. The layout dialog lets the user hide, show and move each card one place up or
  down, and reset to the default. Changes take effect immediately; a card that becomes visible has its charts and
  gauges redrawn at their new size at once.
- The layout is stored per browser in `localStorage` under `pimonitor-layout` as a JSON array of
  `{ "id": <card id>, "visible": <bool> }` in display order, with the card ids `system`, `updates`, `uptime`, `cpu`,
  `load`, `temperature`, `memory`, `disks`, `network`, `openhab`. There is no server-side layout.
- A stored layout is reconciled on every load: unknown ids and duplicates are dropped, and a known card missing from
  it is appended, visible. A missing or unreadable value means the default layout.
- Like the Network card, the openHAB card is shown only when its data allows it as well as its layout preference: a
  hidden preference keeps it hidden in every state.
- With `network_enabled` false the Network card's visibility checkbox is disabled and labelled "(disabled on
  server)"; the card can still be moved.
- Cards flow in three columns, two at a viewport width of 1100 px or less and one at 720 px or less.

### Browser storage

- The dashboard stores exactly four `localStorage` entries: `pimonitor-api-key`, `pimonitor-theme`,
  `pimonitor-layout` and `pimonitor-openhab-things` (the value `open` while the openHAB card's list of not-online
  Things is expanded; the entry is absent otherwise). When storage is unavailable (private browsing, blocked storage) every feature keeps working
  for the current page with defaults, and nothing is stored.

## Limits and errors

- The dashboard needs JavaScript; without it the page shows only its static placeholders and no data.
- The dashboard shows at most `history_window_minutes` of history and at most 1 h in the detail view.
- After a server restart or configuration change, values from `/api/v1/config` (interval, thresholds,
  `network_enabled`, window) in an open page stay those of the page load until it is reloaded or a key is accepted.
- A stored API key is readable by anyone with access to the browser profile, and without TLS it travels in clear
  text with every request.

## Related decisions

None yet.

## Not here

- The API endpoints, their JSON shapes and the `?since=` rules: [`API.md`](../API.md) and
  [HTTP API](http-api.md).
- Authentication, security headers and CSP on the server side: [HTTP API](http-api.md).
- Defaults and valid ranges of `poll_interval_seconds`, `history_window_minutes`, `network_enabled`, `api_key`,
  the thresholds and the `openhab` options: [Configuration](configuration.md).
- When an alert fires or clears: [Alerts](alerts.md).
- What each metric means and when it is empty: [Metrics](metrics.md).
- What the server's history window holds: [History](history.md).
- Why the API key may be kept in browser storage: [`SECURITY.md`](../../SECURITY.md).

## Open questions

- The fallback values the dashboard uses before `/api/v1/config` answers equal the server defaults today, but only
  the threshold key set is checked by a test, not the values. Should the values be guaranteed too?
- Polling starts on page load even when the page is opened in a background tab; it stops only at the first change
  to hidden. Is that intended, or should a page loaded hidden not poll?
