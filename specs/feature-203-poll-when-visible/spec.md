# Spec: Poll only when visible and guard the fallback values with a test

Source: Issue #203
Status: Draft

## Problem

A dashboard opened in a background tab polls the Pi although nobody looks at it: `main` → `reloadAll` →
`startPolling` in `internal/web/assets/app.js` never checks `document.hidden`, and polling stops only at a change to
hidden, which a tab hidden from the start never sees (issue #111 covered only a page that becomes hidden). A page
hidden while it loads has the same gap: `reloadAll` starts the timers after the hide already stopped them.

Until `/api/v1/config` answers, the dashboard uses built-in fallback values; `internal/web/thresholds_test.go`
checks only their threshold key set, so a changed server default goes unnoticed.

## Scenarios

- S1: Given the dashboard is opened in a background tab, when the page has loaded, then it sends no request to
  `/api/v1/...` while the tab stays hidden.
- S2: Given the page of S1, when the user switches to its tab, then the dashboard does its initial load once —
  `/api/v1/config`, then metrics, history, alerts and openHAB in that order — and starts its timers, exactly as a
  page opened in the foreground does. This first show runs the initial load instead of the refresh of S4, never
  both; from the second show on, the page refreshes as in S4.
- S3: Given the dashboard is opened in the foreground, then it loads and polls immediately, as today.
- S4: Given a polling page, when the tab is hidden, then all timers stop and no further polls are sent; when it is
  shown again, metrics, history, alerts and openHAB are fetched immediately and the timers restart (as today).
- S5: Given a developer changes a server default threshold or the default poll interval in `internal/config` but not
  the fallback in `app.js` (or the other way round), when the unit tests run, then a test in `internal/web` fails and
  names the value that differs, with both values.

## Errors and edge cases

- E1: The tab is hidden while a load is still running — the initial load (S2, S3) or the reload after an API key was
  accepted: the started load runs to its end (its remaining requests still go out, each once) and renders, but the
  timers are not started at its end while the page is hidden; showing the tab again refreshes and restarts the
  timers as in S4.
- E2: The tab is shown and hidden again repeatedly before it was ever visible long enough for the initial load to
  finish: the initial load is done once, not once per show, and each endpoint has at most one request outstanding at
  any time (the existing one-request-per-poll rule).
- E3: The API key is required and the page was opened hidden: the "API key required" dialog appears only once the tab
  is shown, since no request was sent before.
- E4: A browser without the Page Visibility API (`document.hidden` undefined) is treated as visible: it loads and
  polls as in S3.
- E5: A fallback value in `app.js` the test cannot read as a literal — a numeric field (thresholds,
  `poll_interval_seconds`, `history_window_minutes`) not written as a number literal, `network_enabled` not written
  as `true` or `false`, or a missing key: the test fails and names the key, never passes silently.

## Non-functional requirements

- N1: No new configuration, endpoint, response key or dependency; the server is unchanged.
- N2: The guard test follows the source-scan approach of `internal/web/polling_test.go` and
  `internal/web/thresholds_test.go` (the repository has no JavaScript test framework) and reads the server defaults
  from `config.Default()`, not from a copy.

## Acceptance criteria

- AC1: A page loaded while `document.hidden` is true sends no `/api/v1/...` request; when it first becomes visible it
  does the initial load once (config, metrics, history, alerts, openHAB, in that order) instead of the S4 refresh,
  and then starts the timers (S1, S2, E2, E3). A source scan of `app.js` in `internal/web`, failing on today's build,
  checks: `main` starts the initial load only when `document.hidden` is false; the `visibilitychange` handler, on a
  show, starts the initial load when it has not run yet and the four-poll refresh only otherwise.
- AC2: A page loaded visible loads and polls immediately as before; hiding stops every timer and showing again
  refreshes and restarts them; a load the hide interrupts (the initial load, the reload after an accepted API key)
  runs to its end but starts no timer while hidden; at most one request per poll is outstanding (S3, S4, E1, E2, E4).
  A source scan of `app.js`, failing on today's build, checks that `reloadAll` tests `document.hidden` before it
  calls `startPolling()`; the existing scans of `polling_test.go` keep the in-flight guards.
- AC3: A test in `internal/web` fails, naming the field and both values, when any fallback threshold,
  `poll_interval_seconds`, `history_window_minutes` or `network_enabled` in `app.js` differs from the value in
  `config.Default()`, or cannot be read as a literal (S5, E5, N2).
- AC4: The existing guard tests in `internal/web` (`xss_test.go`, `polling_test.go`, `thresholds_test.go` among them)
  keep passing (N1, N2).
- AC5: manual check — the runtime sequences of AC1 and AC2, which no source scan executes, are checked once by hand in a
  browser on the built dashboard, with the network panel open, and the result recorded in the PR: (a) open in a
  background tab, wait two poll intervals — no `/api/v1/...` request; switch to it — config, metrics, history,
  alerts, openHAB once each in that order, then timer polls; (b) open in the foreground — polls at once, hide —
  polls stop, show — four requests and polls resume; (c) hide during the initial load (throttled network) — the
  remaining requests finish and no timer poll follows while hidden. (S1–S4, E1).

## Out of scope

- Pausing or deferring the asset loads (`/`, `app.js`, `style.css`): the browser fetches them before any script runs.
- Making the fallback values follow the server at runtime (they are a fallback for when `/api/v1/config` cannot be
  read); `version: 'dev'` has no server default and is not compared.
- A JavaScript test framework or a browser-driven test.

## Open questions

- Is the `/api/v1/config` read on load a "poll" that a hidden page defers? Answered: yes — it is the first step of
  the one initial load (`dashboard.md`), and deferring the whole load keeps S2 identical to a foreground load.
- The issue's AC3 names the thresholds and the poll interval; the fallback also holds `history_window_minutes` and
  `network_enabled`. Answered: compared too — the issue's
  *Decision* covers "the fallback values", and both have a default in `config.Default()`.
- The hide during the initial load (E1) is not named in the issue. Answered: in scope (AC2): the issue
  requires that hiding "still pauses" polling.

## Area documents

- `docs/areas/dashboard.md`, *Data sources and polling*: the initial load and the timers start only while the page
  is visible; a page loaded hidden sends no request until it is first shown, then does the initial load once instead
  of the refresh; a load the hide interrupts runs to its end but starts no timer while hidden. *API key*: after an
  accepted key the timers restart only while the page is visible. The fallback bullet says a test keeps the fallback
  values equal to the server defaults, not only the key sets. Both *Open questions* entries are removed.

## Challenge

- 1 (major) — AC1/AC2 only behavioral, while the only test means is a source scan that today's build could pass:
  accepted. AC1 and AC2 now name the rules a scan of `app.js` checks, each failing on today's build (`main` starts the
  initial load only when not hidden; the `visibilitychange` handler runs the initial load on the first show and the
  refresh only afterwards; `reloadAll` tests `document.hidden` before `startPolling()`). The runtime sequences no
  scan executes became AC5, a manual browser check with the network panel recorded in the PR.
- 2 (minor) — first show must run the initial load instead of the refresh, never both: accepted. S2 and AC1 say so;
  from the second show on, the S4 refresh applies.
- 3 (minor) — E1 must say whether the rest of a started load still goes out: accepted as proposed. A started load
  runs to its end, each request once; only the timer start at its end is skipped while hidden (E1, AC2, area
  document outline).
- 4 (minor) — E1 also covers the reload after an accepted API key: accepted (checked: `submitAPIKey` ends with
  `await reloadAll()`; `dashboard.md`, *API key*, says it "restarts the timers"). E1 and AC2 name that reload; the
  area-document outline adds that the timers restart only while visible.
- 5 (minor) — `network_enabled` is a boolean: accepted. E5 reads the numeric fields as number literals and
  `network_enabled` as `true`/`false`; any other form or a missing key fails naming the key.
