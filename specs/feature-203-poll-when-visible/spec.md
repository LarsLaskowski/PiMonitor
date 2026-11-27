# Spec: Poll only when visible and guard the fallback values with a test

Source: Issue #203
Status: Draft

## Problem

A user who opens the dashboard in a background tab (a middle-click, a restored browser session, a bookmark folder
opened at once) makes the Pi answer requests nobody looks at: on page load the dashboard reads `/api/v1/config`,
fetches metrics, history, alerts and openHAB once each and starts its timers without checking whether the page is
visible (`main` → `reloadAll` → `startPolling` in `internal/web/assets/app.js`). Polling stops only at the first
change to hidden, which never comes for a tab that is hidden from the start, so such a tab keeps polling at the
browser's background-timer rate until the user looks at it. Issue #111 added the pause for a page that becomes
hidden; a page that starts hidden is not covered.

The same sequence has a gap for a page hidden while it loads: `reloadAll` starts the timers when its last request
returns, after the hide already stopped them, so that page polls on while hidden.

Separately, until `/api/v1/config` answers (and whenever it fails) the dashboard colors values and spaces its polls
with built-in fallback values. They equal the server defaults today, but `internal/web/thresholds_test.go` checks
only that the threshold key sets match; a changed server default leaves the dashboard showing other colors and
another cadence than the server it talks to, and no test notices.

## Scenarios

- S1: Given the dashboard is opened in a background tab, when the page has loaded, then it sends no request to
  `/api/v1/...` while the tab stays hidden.
- S2: Given the page of S1, when the user switches to its tab, then the dashboard does its initial load once —
  `/api/v1/config`, then metrics, history, alerts and openHAB in that order — and starts its timers, exactly as a
  page opened in the foreground does.
- S3: Given the dashboard is opened in the foreground, then it loads and polls immediately, as today.
- S4: Given a polling page, when the tab is hidden, then all timers stop and no further polls are sent; when it is
  shown again, metrics, history, alerts and openHAB are fetched immediately and the timers restart (as today).
- S5: Given a developer changes a server default threshold or the default poll interval in `internal/config` but not
  the fallback in `app.js` (or the other way round), when the unit tests run, then a test in `internal/web` fails and
  names the value that differs, with both numbers.

## Errors and edge cases

- E1: The tab is hidden while the initial load (S2 or S3) is still running: the requests already sent complete and
  render, but no timer is running once the load has finished; showing the tab again refreshes and restarts the timers
  as in S4.
- E2: The tab is shown and hidden again repeatedly before it was ever visible long enough for the initial load to
  finish: the initial load is done once, not once per show, and each endpoint has at most one request outstanding at
  any time (the existing one-request-per-poll rule).
- E3: The API key is required and the page was opened hidden: the "API key required" dialog appears only once the tab
  is shown, since no request was sent before.
- E4: A browser without the Page Visibility API (`document.hidden` undefined) is treated as visible: it loads and
  polls as in S3.
- E5: A fallback value in `app.js` the test cannot read as a number (an expression, a variable, a missing key): the
  test fails and names the key, never passes silently.

## Non-functional requirements

- N1: No new configuration, endpoint, response key or dependency; the server is unchanged.
- N2: The guard test follows the source-scan approach of `internal/web/polling_test.go` and
  `internal/web/thresholds_test.go` (the repository has no JavaScript test framework) and reads the server defaults
  from `config.Default()`, not from a copy.

## Acceptance criteria

- AC1: A page loaded while `document.hidden` is true sends no `/api/v1/...` request; when it first becomes visible it
  does the initial load once (config, metrics, history, alerts, openHAB, in that order) and then starts the timers
  (S1, S2, E2, E3).
- AC2: A page loaded visible loads and polls immediately as before; hiding stops every timer and showing again
  refreshes and restarts them, also when the hide comes during the initial load, with at most one request per poll
  outstanding (S3, S4, E1, E2, E4).
- AC3: A test in `internal/web` fails, naming the field and both values, when any fallback threshold,
  `poll_interval_seconds`, `history_window_minutes` or `network_enabled` in `app.js` differs from the value in
  `config.Default()`, or cannot be read as a literal (S5, E5, N2).
- AC4: The existing guard tests in `internal/web` (`xss_test.go`, `polling_test.go`, `thresholds_test.go` among them)
  keep passing (N1, N2).

## Out of scope

- Pausing or deferring the asset loads (`/`, `app.js`, `style.css`): the browser fetches them before any script runs.
- Making the fallback values follow the server at runtime (they are a fallback for when `/api/v1/config` cannot be
  read); `version: 'dev'` has no server default and is not compared.
- A JavaScript test framework or a browser-driven test.

## Open questions

- Is the `/api/v1/config` read on load a "poll" that a hidden page defers? Answered: yes. The area document describes
  the config read as the first step of one initial load, and the issue's goal is that a hidden page costs the Pi
  nothing; deferring the whole load also keeps S2 identical to a foreground load.
- The issue's AC3 names the thresholds and the poll interval; the fallback also holds `history_window_minutes` and
  `network_enabled`. Answered: compared too. The issue's decision is that "a test compares the fallback values in
  `app.js` with the server defaults", both fields have a server default in `config.Default()`, and the area document
  says the fallback covers every field of the `/api/v1/config` response.
- The hide during the initial load (E1) is not named in the issue. Answered: in scope as part of AC2, since the issue
  requires that hiding "still pauses" polling and the initial load is where the gap lies.

## Area documents

- `docs/areas/dashboard.md`, *Data sources and polling*: the initial load and the timers start only while the page
  is visible; a page loaded hidden sends no request until it is first shown, then does the initial load once; a hide
  during the initial load leaves no timer running. The fallback bullet says a test keeps the fallback values (every
  threshold, `poll_interval_seconds`, `history_window_minutes`, `network_enabled`) equal to the server defaults, not
  only the key sets. Both entries of *Open questions* are removed.
