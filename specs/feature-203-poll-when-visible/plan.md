# Plan: Poll only when visible and guard the fallback values with a test

Source: Issue #203 | [spec.md](spec.md)
Tier: standard — changes the dashboard's polling behavior (when the initial load and the timers start) and adds guard tests; no security control is created or changed.
Security areas touched: Dashboard XSS — `internal/web/assets/app.js` is edited (polling control flow only; no DOM-building code, no `.innerHTML`)

## Problem / root cause

See [spec.md](spec.md), *Problem*.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The page load runs config, metrics, history, alerts, openHAB, then `startPolling()`, never checking `document.hidden` | `app.js` `main` (1655–1663) → `reloadAll` (1645–1653) | confirmed |
| 2 | A hide during `reloadAll` is undone: its final `startPolling()` arms the timers after the handler's `stopPolling()` | `reloadAll` ends with an unconditional `startPolling()` (1652); handler 1623–1639 | confirmed |
| 3 | The openHAB timer is never armed while hidden | `scheduleOpenHAB` returns on `document.hidden` (1590) | confirmed |
| 4 | Each of the four polls has its own in-flight guard | `metricsInFlight`, `alertsInFlight`, `historyInFlight`, `openhabInFlight` (1483–1486, checked at 1491, 1512, 1531, 1562) | confirmed |
| 5 | The reload after an accepted API key is `reloadAll` | `submitAPIKey` ends with `await reloadAll()` (881) | confirmed |
| 6 | The fallback equals `config.Default()` today: interval 5, window 60, network true, all 14 thresholds | `app.js` 2–23 vs `config.go` 276–311 | confirmed |
| 7 | `config.Thresholds` has 12 `float64` and 2 `int` fields; `PollIntervalSeconds`, `HistoryWindowMinutes` are `float64`, `NetworkEnabled` is `bool` | `config.go` 29–45, 109–115 | confirmed |
| 8 | `internal/web` may import `internal/config` in tests (already does); only `httpapi`'s tests import `internal/web` | `thresholds_test.go` imports; `grep` of `internal/web"` | confirmed |
| 9 | `TestAppJS_AsyncCallsAreNotFloating` flags `name(...);` at a line start for every `async function`; `initialLoad().catch(…);` and `if (…) await initialLoad();` do not match | regex at `polling_test.go` 85 | confirmed |
| 10 | `app.js` is outside the coverage measure (`COVERAGE_PATHSPECS = ["*.go"]`); no exclusion needed | `squad_settings.py` 30 | confirmed |
| 11 | `document.hidden` is `undefined` (falsy) where the Page Visibility API is missing, so `!document.hidden` loads as visible (E4) | DOM spec / MDN `Document.hidden` | confirmed |
| 12 | The Product Manager can run `make run` (`go run ./cmd/pimonitor -config packaging/pimonitor.example.yaml`) and open `http://localhost:8080` in a desktop browser for AC5 | to verify: the orchestrator asks the Product Manager before the step-7 manual test; fallback in *Tests per criterion* | unverified |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestAppJS_InitialLoadWaitsForVisibility`: `t.Run("AC1 main starts the initial load only when the page is visible")`, `t.Run("AC1 the first show runs the initial load instead of the refresh")` | `internal/web/polling_test.go` |
| AC2 | `TestAppJS_ReloadStartsTimersOnlyWhenVisible`: `t.Run("AC2 reloadAll starts the timers only while visible")`; existing `TestAppJS_PollingPausesWhenHidden`, `TestAppJS_PollFunctionsGuardInFlightRequests` | `internal/web/polling_test.go` |
| AC3 | `TestAppJS_FallbackValuesMatchConfigDefaults`: one `t.Run("AC3 <key>")` per fallback field against `app.js`, and `t.Run("AC3 …")` cases on hand-made fallback blocks proving the check fails: a changed number, an expression (`12 * 60`), a missing key, `network_enabled: 1` | `internal/web/thresholds_test.go` |
| AC4 | `CI: go test ./internal/web/...` — every existing test, unchanged | `internal/web/*_test.go` |
| AC5 | `manual: the Product Manager, on a local build of the stamped head (make run, or the binary the orchestrator builds with make build and hands over when the PM has no Go toolchain), cases (a)–(c) of AC5, result in log.md and the PR's Test Plan` | — |

Each new scan must fail on today's `app.js` (the Tester confirms in step 5).

## Approach

**`internal/web/assets/app.js`** (Dev), in the polling block:

- New `let initialLoadStarted = false;` beside the timer variables.
- New private `async function initialLoad()`: sets `initialLoadStarted = true` before anything else, then
  `await reloadAll();`. Its comment says it runs once per page — from `main` when loaded visible, else from the
  first show — and that the flag is set before the first request so a second show during the load refreshes
  instead of loading again (E2).
- `main`: the last statement `await reloadAll();` becomes `if (!document.hidden) await initialLoad();`. `main` no
  longer calls `reloadAll` directly.
- `wireVisibilityPolling` handler, visible branch, before the four `void poll…()` calls:
  `if (!initialLoadStarted) { initialLoad().catch((e) => console.error('failed to initialise dashboard', e)); return; }`
  — the initial load instead of the refresh, never both (spec S2).
- `reloadAll`: the final `startPolling();` becomes `if (!document.hidden) startPolling();` (E1; covers the API-key
  reload, fact 5). The rest of a started load still runs (spec E1). The openHAB timer already follows fact 3.
- `submitAPIKey`, `startPolling`, `stopPolling` and the poll functions are unchanged.

**Scan rules** (Tester; regexes tolerant of whitespace, bodies cut as `TestAppJS_PollFunctionsGuardInFlightRequests`
does, at the first `\n  }\n` after the declaration):

- AC1a: the body of `async function main(` contains `if (!document.hidden) await initialLoad();` and no `reloadAll(`.
- AC1b: the body of `function wireVisibilityPolling(` contains `if (!initialLoadStarted)`, and within it
  `initialLoad()` appears before the first `pollMetrics()`; the body of `async function initialLoad(` sets
  `initialLoadStarted = true` before `await reloadAll()`.
- AC2: the body of `async function reloadAll(` contains `if (!document.hidden) startPolling();` and no
  `startPolling();` on a line of its own.

**Fallback guard** (Tester, AC3): the block from `let config = {` to the first `\n  };` is read; a helper in the test
file parses it into key → literal text for the three top-level fields and the `thresholds:` entries, and the
comparison takes that map plus `config.Default()`, so the negative cases feed it hand-made blocks. Thresholds are
enumerated by reflection over `config.Thresholds` json tags (a new threshold is compared without editing the test);
the top-level fields map fixed: `poll_interval_seconds` → `PollIntervalSeconds`, `history_window_minutes` →
`HistoryWindowMinutes`, `network_enabled` → `NetworkEnabled`. A numeric value must match `^-?\d+(\.\d+)?$` and is
compared with `strconv.ParseFloat` against the field as `float64` (the `int` fields converted); `network_enabled`
must be `true` or `false`. A failure names the key and both values, or the key and the unreadable text (E5).
`version` is not compared. The existing key-set test stays.

No new user-visible output.

Early draft PR: no

## Options considered

1. **Guard inside `startPolling` (return when hidden) instead of in `reloadAll`** — equivalent today (its only
   callers are `reloadAll` and the visible branch of the handler), but the spec's AC2 rule names `reloadAll`, and a
   guard in `startPolling` would also hide a future caller's mistake rather than state the rule where the load ends.
   Lost on keeping the spec's rule.
2. **Load while hidden, defer only the timers; or read only the config while hidden** — the record's options 2 and 3:
   [0031](../../docs/decisions/0031-dashboard-loaded-hidden-defers-initial-load.md).
3. **Reflect over `httpapi.ClientConfig` to enumerate the top-level fallback fields** — would catch a new response
   field, but still needs a hand-written map from each field to its `config.Config` default, adds a test import of
   `internal/httpapi`, and the existing key-set test already pins the threshold set. Lost on cost.
4. **A JavaScript test framework (jsdom) to execute the handler** — out of scope (spec), a new dependency the
   project deliberately avoids (`docs/UNIT_TESTS.md`).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/web | `assets/app.js` | `initialLoadStarted`, `initialLoad`; `main`, handler, `reloadAll` as above |
| internal/web | `polling_test.go`, `thresholds_test.go` | new scans and fallback-value test |

## Signatures (for the Dev's skeleton)

None: no Go API changes. The JavaScript members (`initialLoadStarted`, `initialLoad`) are private to the IIFE and
described in *Approach*; the skeleton adds `initialLoad` with its flag and changes nothing else, so the scans of AC1
and AC2 fail until step 6.

## Test files

`internal/web/polling_test.go`, `internal/web/thresholds_test.go`.

Existing test code that calls a changed signature: none.

Files of the change no unit test can cover: `internal/web/assets/app.js` runtime behavior — no JS test runner; it
is outside the coverage measure already (fact 10), so no exclusion; guarded by the scans and AC5.

## Areas

- `docs/areas/dashboard.md` (3217 words, +60 words): *Data sources and polling* — the page-load bullet: the initial
  load runs only while visible, a page loaded hidden runs it at its first show instead of the refresh; the hidden
  bullet: a load a hide interrupts runs to its end but starts no timer while hidden; the fallback bullet: a test
  keeps the values (every threshold, `poll_interval_seconds`, `history_window_minutes`, `network_enabled`) equal to
  `config.Default()`, not only the key sets. *API key*: the accepted-key reload restarts the timers only while
  visible. *Open questions*: both entries replaced by "None." Link [0031](../../docs/decisions/0031-dashboard-loaded-hidden-defers-initial-load.md) from the polling bullet.

## Documentation updates

Written once the code is green, against it:

- `docs/areas/dashboard.md` — as *Areas* (Dev).
- `docs/ARCHITECTURE.md` (Dev) — the paragraph at 466–470: a page loaded hidden starts polling at its first show,
  linking record 0031; the sentence at 398–401: the test guards the fallback's values against `config.Default()`, not
  only the key set.
- `docs/UNIT_TESTS.md` (Tester) — *Static-asset guard tests*, the `polling_test.go` sentence: it also asserts where
  `app.js` checks `document.hidden` before the initial load and the timer start (issue #203).
- No changelog file exists; the PR carries the `enhancement` label (`.github/release.yml`). `README.md`, `docs/API.md`,
  configuration: no change (sweep for "hidden", "visib", "fallback": no other hit).

## Architecture check

The guarantees of `.squad/project.md` are untouched: no server, API, privilege or DOM-building change. The
dashboard's hidden-tab pause (`ARCHITECTURE.md` 466) is extended to a page that starts hidden; the paragraph is
updated and links record 0031.

## Security considerations

- [x] No limits.
- [x] None: no message reaches the user; a failed initial load from the handler is logged to the console as from `main`.

The edit stays outside DOM building; `xss_test.go` keeps guarding `.innerHTML` (AC4).

## Decision records

- `docs/decisions/0031-dashboard-loaded-hidden-defers-initial-load.md` (Proposed)

## Out of scope / follow-ups

As spec *Out of scope*. No follow-up issue.

## Challenge notes

I found no major objections and one minor one. Every claim I checked holds against the code and the issue:

- **Facts 1–5 hold.** `main` ends in `await reloadAll()` (`internal/web/assets/app.js` 1663). `reloadAll` ends in an unconditional `startPolling()` (1652). `scheduleOpenHAB` returns when `document.hidden` is true (1590). The four in-flight guards exist (1483–1486). `submitAPIKey` ends in `await reloadAll()` (881).
- **The fallback values match.** `app.js` 2–23 equals `config.Default()` in `internal/config/config.go` 276–311.
- **No top-level field is missed.** The fixed map of three top-level keys covers every field of `ClientConfig` (`internal/httpapi/server.go` 57–71) except `version`, which the spec leaves out.
- **No other timer or fetch runs at load.** The only `setInterval`/`setTimeout` calls are at 1591 and 1604–1608, and `fetchJSON` is called only at 207, 869, 1494, 1515, 1536, 1545 and 1566.
- **Option 1's premise is correct.** `startPolling` has exactly two callers, 1637 and 1652.
- **The new calls will not trip the floating-call scan.** The regex in `internal/web/polling_test.go` 85 does not match `if (…) await initialLoad();` or `initialLoad().catch(…);`, whether `.catch` is on the same line or split onto a new one (fact 9).
- **The body cuts work.** Cutting a function body at the first `\n  }\n` gives the whole body for `main` and `wireVisibilityPolling`. The inner closers are indented four or more spaces.
- **Hide/show sequences come out right.** I traced loaded hidden → shown → hidden → shown during the load (E2), and loaded visible with a hide during `loadConfig` (E1). No endpoint gets two requests in flight, and the final `startPolling()` runs only while visible.

Objections:

1. (minor) The AC1b scan would pass on a wrong build that runs both the initial load and the refresh on the first show, which S2 and AC1 forbid ("instead of the S4 refresh, never both"). The plan's handler code ends the branch with `return;`. But the scan only asserts that `if (!initialLoadStarted)` exists and that `initialLoad()` comes before the first `pollMetrics()`. A build without the `return` (and without an `else`) satisfies both. Instruction for the Tester: in the body of `function wireVisibilityPolling(`, also assert that the `if (!initialLoadStarted)` block ends in `return;` before the first `pollMetrics()`, or that the refresh sits in its `else` branch. Confirm this check fails on a hand-made handler without the `return`. Why minor: the Tester can add the assertion without a plan change, and the planned implementation already satisfies it.
