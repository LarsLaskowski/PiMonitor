# 0031: A dashboard loaded hidden defers its whole initial load until it is first shown

- **Status:** Proposed
- **Date:** 2026-11-27
- **Area:** Dashboard
- **Source:** Issue #203
- **Supersedes:** —

## Context

Issue #111 paused polling when the dashboard's tab becomes hidden, but a page opened in a background tab never sees
that change: it read `/api/v1/config`, fetched metrics, history, alerts and openHAB and started its timers, polling
the Pi at the browser's background-timer rate until someone looked at it. Issue #203 decided that a page loaded
hidden does not poll until it becomes visible. It left open whether the one-off reads of the initial load count as
polls, and the repository has no JavaScript test framework, so the behavior can be guarded only by a source scan of
`app.js` and a manual browser check.

## Options considered

1. **Defer the whole initial load, the config read included** — a hidden page sends no API request at all; the first
   show runs exactly the load a foreground page runs, in the same order. Cost: the first show waits for the config
   answer before the cards fill, as a fresh load does.
2. **Run the initial load, defer only the timers** — the cards are filled when the user switches to the tab. Cost:
   five requests per background tab that may never be looked at, and the first show would refresh on top of data
   that may be minutes old, so the saving is small.
3. **Read only `/api/v1/config` while hidden, defer the rest** — the first show could start with the server's interval.
   Cost: a second rule for a gain the config read already gives on the first show; the page still costs the Pi a
   request nobody needs.

## Decision

A page loaded hidden sends no `/api/v1/...` request until it is first shown; that first show runs the initial load
once, instead of the refresh a later show does. A load that a hide interrupts runs to its end but starts no timer
while the page is hidden. The rules are in [Dashboard](../areas/dashboard.md#data-sources-and-polling).

## Consequences

- A background tab costs the Pi nothing until it is shown.
- The rules are guarded by source scans in `internal/web/polling_test.go` that check where `app.js` tests
  `document.hidden`; the runtime sequence is checked by hand in a browser once per change, since no test executes
  `app.js`. A JavaScript test framework would have to come first to test it automatically.
