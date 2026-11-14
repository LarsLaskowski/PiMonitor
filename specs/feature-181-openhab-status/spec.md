# Spec: Show the local openHAB status in the dashboard

Source: Issue #181
Status: Approved by Lead

## Problem

Many Pis that run PiMonitor also host openHAB, usually their largest process, yet PiMonitor shows nothing
about it. The operator wants a compact health view of the local openHAB in the dashboard and the API, without
item states ([issue #181](https://github.com/LarsLaskowski/PiMonitor/issues/181)).

## Scenarios

- S1: Given openHAB runs on the Pi and answers on `base_url`, when the operator opens the dashboard, then an
  "openHAB" card shows the process memory (absolute and in % of RAM), the JVM heap used/total, the Things by status, the number of Items and of installed add-ons, the openHAB version and its uptime.
- S2: Given one or more Things are not ONLINE, when the card refreshes, then it is in its warning state and lists
  those Things (label, UID, status and status detail).
- S3: Given openHAB is installed but its process is stopped, when the card refreshes, then it says openHAB is not
  running and is in its warning state.
- S4: Given a default openHAB install and no `api_token` (openHAB 5.2.4 then refuses system info, Things and
  add-ons with `401`, while the root call and the Item list answer), when the card refreshes, then the process
  memory, uptime, version and Item count are shown and heap, start level, Things and add-ons read "needs
  api_token"; with a token openHAB refuses, they read "api_token refused". The card says openHAB is not reachable
  when the root call itself fails.
- S5: Given openHAB is neither installed nor running, when the dashboard loads, then no openHAB card appears and
  PiMonitor sends no request to `base_url` and logs nothing about openHAB.
- S6: Given `openhab.enabled: false`, then PiMonitor never looks for openHAB, `GET /api/v1/openhab` answers `404`
  and the dashboard shows no card.
- S7: Given an integration polls `GET /api/v1/openhab`, then it gets the card's values, as `docs/API.md` documents.
- S8: Given openHAB is installed, started or stopped while PiMonitor runs, then the next openHAB poll reflects it
  without a PiMonitor restart, and an open dashboard shows or hides the card on its next openHAB poll.

- S9: Given PiMonitor has just started and its first openHAB poll has not finished, then `GET /api/v1/openhab`
  answers with state `pending` and no openHAB value and the dashboard shows no card until a poll has finished.
## Errors and edge cases

- E1: A process whose command line mentions openHAB but is not the openHAB Java process (the `karaf` start script,
  an editor on `/etc/openhab/…`) is not taken for openHAB.
- E2: Several processes match: the one with the lowest PID is reported.
- E3: `/proc` is mounted with `hidepid=`, or the process vanished during the read: the process values are absent;
  the state then follows from the installation paths and the REST root call (N2).
- E4: `base_url` answers but is not openHAB (for example PiMonitor itself on the same port, or an HTML page):
  counts as not reachable, never as fabricated values.
- E5: openHAB answers a call with a redirect: the redirect is not followed, and that value is absent.
- E6: openHAB hangs or is very slow: each call is abandoned after its timeout (N4); the other calls and PiMonitor's
  own metrics carry on.
- E7: A response is malformed, of an unexpected shape, or larger than the size limit (N4): only that call's values
  are absent.
- E8: Thing labels, status details and the version are hostile text (markup, script): the card shows them as
  literal text.
- E9: More than 10 Things are not ONLINE: the list holds the first 10 by UID, the counts by status still cover all.
- E10: A disabled Thing (status `UNINITIALIZED`, detail `DISABLED`) is counted under its status but is neither
  listed nor a reason for the warning state — it is off by the operator's choice.
- E11: openHAB is still starting (start level below 100): the card shows "starting (start level N)"; this alone is
  no warning.
- E12: PiMonitor's memory reading failed in the latest fast tick: the RSS percentage is absent, the absolute RSS is
  still shown.
- E13: Invalid configuration — `enabled` other than `auto`, `true` or `false` in any letter case (so `yes`, `on`,
  `1` are invalid), `base_url` that is not `http` with a loopback host (no `https`: N4), `poll_interval_seconds` below 60, above 86400 or not
  an integer: startup fails with an error naming the
  option, like every other invalid option ([Configuration](../../docs/areas/configuration.md)).
- E14: openHAB omits an optional field (another 4.x/5.x release): that value is absent, the rest is shown.
- E15: The anonymous Item list holds empty objects instead of names: the Items are still counted.
- E16: With a token configured, the program answering on `base_url` does not run as the owner of
  `/var/lib/openhab` (another local user's listener while openHAB is down), or that directory is missing: no
  request is sent, the REST values are absent and a warning names the reason.

## Non-functional requirements

- N1: Detection runs on every openHAB poll. With `enabled: auto` openHAB counts as present when one of its
  standard installation paths (`/usr/share/openhab`, `/etc/openhab`, `/var/lib/openhab`) exists or its Java
  process runs; only then is any request sent. `enabled: true` always polls and never reports `not_installed`.
- N2: Until the first poll after startup has finished the state is `pending`, with no other value. Then it is
  `not_installed` (only with `auto`, nothing found), `ok` (the REST root call answers as openHAB),
  `running_unreachable` (process found, root call fails) or `installed_not_running` (no process found, root call
  fails). The process values are reported whenever the process is found, whatever the REST calls do.
- N3: Each source — process, root, system info, Things, Items, add-ons — is collected independently; a failure
  leaves only its values absent (key omitted, never `null` or a made-up `0`). A REST source answered with `401` or
  `403` is named in a list of refused sources in the response, so the operator learns that a token is missing or
  wrong. The Item and add-on lists are only
  counted; no item state is read, stored or served.
- N4: Every request goes only to `base_url` over plain `http` (openHAB's HTTPS port has a self-signed certificate,
  and skipping verification is no option), has a 5 s timeout, reads at most 8 MiB per response (the add-on list of a real install is about 270 KB)
  and follows no redirect.
- N5: The openHAB poll has its own interval, `poll_interval_seconds` (default 60, minimum 60), runs once at startup,
  and never delays the fast or the slow tick or `/healthz`.
- N6: `api_token` (default empty) comes from the config file or from `PIMONITOR_OPENHAB_TOKEN`, which overrides the
  file when non-empty and is removed from the environment after reading, as `PIMONITOR_API_KEY` is. There is no
  flag. The token is sent only to `base_url` as `Authorization: Bearer <token>`, only when set, only over a
  connection whose server side belongs to the owner of `/var/lib/openhab`, and never appears in a log line, an API
  response or the dashboard.
- N7: Logging follows the log-once rule of [0001](../../docs/decisions/0001-optional-sources-log-failures-once.md)
  per source: a warning on the first failure and when its error changes, debug while it repeats, info on recovery.
  A `401`/`403` while no token is configured is the expected default, logged once at info instead of warning.
  While the state is `not_installed` nothing is logged.
- N8: `GET /api/v1/openhab` is a new, additive `v1` route behind the API chain (key, gzip, no-store, in-flight
  limit); it is registered only when `enabled` is not `false`. The one change to an existing `v1` response is the
  additive `by_route` key of `GET /api/v1/serverstats` that every route gets, present whatever `enabled` says; the
  CSP is unchanged and the browser never contacts openHAB.
- N9: No new dependency; the main service stays unprivileged and its systemd unit keeps its directives (comments
  may change).
- N10: Compatibility: openHAB 4.x and 5.x, installed from the apt package or openHABian; built from real
  responses of openHAB 5.2.4, the residual risk for 4.x recorded in a decision record.

## Acceptance criteria

- AC1: With `enabled: auto`, no installation path and no openHAB process, `GET /api/v1/openhab` answers `200` with
  state `not_installed` and no openHAB value, no request reaches `base_url` and nothing about openHAB is logged (S5, N1, N7).
- AC2: The state is `pending` until the first poll finishes, then follows N2 for every combination of installation paths found, process found and root call
  succeeding, with `enabled: auto` and `enabled: true`, and is re-evaluated on every poll (S3, S8, S9, E3, E4, N1, N2).
- AC3: The openHAB Java process is recognised by its command line, other processes that mention openHAB are not
  (E1), the lowest PID wins (E2), and the response carries its PID, RSS in bytes and RSS in percent of
  `memory.total_bytes` (absent when that is 0) — also when every REST call fails (S1, S4, E12, N2).
- AC4: The response carries the openHAB version, start level, uptime in seconds (from the process start time,
  openHAB's own value only when no process is found), JVM heap used and total in bytes, the Item count (also from
  nameless entries) and the installed add-on count; each is absent when its call fails or openHAB does not report it,
  without affecting the others; every source refused with `401`/`403` is listed as refused; `collected_at` is the
  time the poll finished, in every state but `pending` and `not_installed`; the parsers are tested against anonymized
  excerpts of real openHAB 5.2.4 responses (S1, S4, E7, E14, E15, N3, N10).
- AC5: Things are reported as a total and a count per openHAB Thing status (other status strings are not
  counted per status), plus up to 10 Things that are not ONLINE and not
  disabled, sorted by UID, each with UID, label, status, status detail and, when openHAB gives one, its description (S2,
  E9, E10).
- AC6: Requests go to `base_url` only, are abandoned after the timeout, read at most the body limit, follow no
  redirect, and carry `Authorization: Bearer <token>` exactly when a token is configured, never to a server owned
  by another user than the owner of `/var/lib/openhab` (E16); `go.mod` gains no
  dependency and `pimonitor.service` changes in comments only (E5, E6, E7, E16, N4, N6, N9).
- AC7: The `openhab` block has `enabled` (default `auto`), `base_url` (default `http://127.0.0.1:8080`), `api_token`
  (default empty) and `poll_interval_seconds` (default 60); the invalid values of E13 fail startup with an error
  naming the option; `enabled` accepts `auto`, `true` and `false` in any letter case, quoted or not, and rejects
  `yes`, `no`, `on`, `off`, `1` and `0`; a scheme other than `http` and a host other than `127.0.0.0/8`, `::1` or
  `localhost` are invalid (E13, N4).
- AC8: `PIMONITOR_OPENHAB_TOKEN` overrides a file token when non-empty, is removed from the environment after
  reading, and the token never appears in a log line, in `GET /api/v1/openhab`, `GET /api/v1/config` or any other
  response (N6).
- AC9: `GET /api/v1/openhab` requires the API key when one is set, has the API chain's headers and limits, and
  answers `404` when `enabled` is `false`; with `enabled: false` no detection runs (S6, S7, N8).
- AC10: Each source's failures are logged per N7: one warning per new error, one info line instead for a
  `401`/`403` without a configured token, info on recovery, no repeat on later polls (S4, N7).
- AC11: The first openHAB poll runs at startup and then every `poll_interval_seconds`; a hung openHAB does not delay
  the fast tick (E6, N5).
- AC12: The dashboard shows the openHAB card exactly while the latest `GET /api/v1/openhab` answered `200` with a
  state other than `not_installed` or `pending` and its layout preference is visible, polls it at the openHAB
  interval (at the metrics interval while `pending`) with the other polls' rules (one request
  outstanding, pause while hidden, `401` handling), and lists the card in the layout dialog under id `openhab`
  (S1, S5, S6, S8, S9).
- AC13: The card shows the values of S1 (`n/a` for an absent one, the texts of S4 for a refused one), the state text of S3/S4/E11, the not-ONLINE list,
  and its warning state exactly when the state is `installed_not_running` or `running_unreachable` or a listed Thing
  exists; every text from openHAB is inserted as text, which `internal/web/xss_test.go` keeps enforcing (S1–S4, E8,
  E11).

## Out of scope

- Item states, sitemaps, commands to openHAB ([issue #181](https://github.com/LarsLaskowski/PiMonitor/issues/181)).
- A remote openHAB instance; port conflicts between PiMonitor and openHAB (the installer's concern).
- Prometheus metrics (#182), a memory history sparkline (#183) and alert rules (#184) for openHAB.
- A list of add-on names (the issue's optional tooltip); the JVM heap maximum, which openHAB 5.2.4 does not
  report.

## Open questions

- Does `enabled: false` answer `{"state":"not_installed"}` or `404`? Answered: `404` — a route whose option is
  off is not registered ([HTTP API](../../docs/areas/http-api.md#routes)), and the feature is then fully inert.
- What does the endpoint answer before the first poll has finished? Answered: `pending` (N2), never a guessed
  `not_installed`, which would hide the card after every restart and mislead integrations.
- Which state applies when no process is found but REST answers (a hidden `/proc`, openHAB in a container)?
  Answered: `ok`, without process values (N2); the issue's states cover only the other cases.
- `null` or omitted for a value not collected? Answered: omitted, as `gpu_temperature` is
  ([`API.md`](../../docs/API.md)).
- In which unit does openHAB report its own uptime? Answered: unverified (the Product Manager's `ps -o etimes=`
  pair settles it); the uptime comes from the process start time, openHAB's value only without a process.
- Do disabled Things warn? Answered: no (E10), Lead decision: the list is meant for actionable failures.
- How does the dashboard learn the openHAB interval? Answered: from the new response, not from a changed `v1` one.
- Do the issue's claims about today's PiMonitor hold? Answered: yes (the `/proc` walk and `VmRSS` reading, the
  unset `PIMONITOR_API_KEY`, config mode `640`, a unit that lets the service see the paths, the process and
  loopback), except in letter: the CSP limits connections through `default-src 'self'`, the test conventions are
  `docs/UNIT_TESTS.md`, and the log-once precedent is decision 0001.

## Area documents

- New area *openHAB* (`docs/areas/openhab.md`): detection, states, sources, cadence, limits, logging, token.
- Configuration, HTTP API, Dashboard, Deployment, `docs/API.md`, `docs/ARCHITECTURE.md`, `SECURITY.md`,
  `docs/integrations/openhab.md`, `packaging/pimonitor.example.yaml`: as the plan's *Documentation updates* lists.

## Challenge

1. (major) No answer before the first poll — accepted: state `pending` (N2, S9, AC2, AC12).
2. (major) Refused calls unexplained — accepted: refused sources listed (N3, AC4), card texts (S4, AC13), info
   instead of warning without a token (N7, AC10); the samples show which calls need a token.
3. (minor) `serverstats` shape — accepted: its `by_route` key named in N8 and *Area documents*.
4. (minor) Unit comment, missing documents — accepted: N9, AC6, *Area documents*.
5. (minor) YAML booleans in `enabled` — accepted: E13, AC7.
6. (minor) `collected_at` — accepted: AC4.
7. (minor) Uptime fallback — accepted: E14, AC4.
8. (minor) `https` certificates — accepted: `https` dropped (N4, E13, AC7).
