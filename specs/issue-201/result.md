Mode plan, issue #201. Tier **patch**, so there is no plan.md. The result below is for `specs/issue-201/result.md`; please write it verbatim with `squad-notes.py specs/issue-201 result`. I wrote nothing to the repository.

## Tier
`patch` (not `patch/security`). The fix replaces one literal in production code: nil slices become empty slice literals. Control flow is unchanged, and there is no new dependency, permission or schema. The diff is about 30 changed lines in all, which is exactly `PATCH_MAX_LINES` (code 2, docs 6, test at most 22).

**Security areas touched:** none. `internal/collector/collector.go`, its test and two Markdown files are not listed in `.squad/project.md`.

**Decision record:** none. The Product Manager made the decision in the issue, and the change makes the code match the contract `docs/API.md` already documents. No guarantee changes, and there was no choice between real alternatives left to the Lead. The PR description gives the reason.

## Verified facts
- **Confirmed:** with alerts off, both fields encode as `null`. Root cause is `internal/collector/collector.go:724`: `Collector.Alerts()` returns `alert.Report{Enabled: false}` when `c.alerts == nil`. Its `States`/`Events` are nil, and `alert.Report` (`internal/alert/alert.go:122-126`) has no `omitempty`, so `encoding/json` writes `null`.
- **Confirmed:** `docs/API.md:556-557` says "with empty `states`/`events`".
- **Confirmed:** the other endpoints return `[]`. Processes are initialised with `[]Process{}` (`collector.go:288`), and `docs/API.md:404` documents `[]` for disks.
- **Refuted as a defect, AC2 already holds:** with alerts on and nothing to report, `Engine.Report()` builds `make([]State, 0, …)` (`alert.go:365`) and `make([]Event, len(e.events))` (`alert.go:383`). Both are non-nil, so they encode as `[]`. The engine exists from `New()` on (`collector.go:273`), before the first tick. No code edit is needed for AC2, only a test.
- **Related defect:** `TestCollector_Alerts_DisabledByDefault` (`internal/collector/collector_test.go:459`) only checks `len(...) != 0`. That cannot tell nil from empty, which is why this bug got through. The new test below closes that gap; the existing test stays as it is.
- **Architecture check:** the guarantee that `/api/v1` response shapes are stable (`.squad/project.md:60`) is not weakened. The documented shape already promises empty arrays, so no `/api/v2` is needed. The dashboard already reads `report.states || []` (`internal/web/assets/app.js:387`), so it handles both.

## Exact edits (applied by the orchestrator)
1. `internal/collector/collector.go:724`
   - Replace `return alert.Report{Enabled: false}`
   - with `return alert.Report{Enabled: false, States: []alert.State{}, Events: []alert.Event{}}`
   - **Covered by** the new test below (AC1, AC3).
2. `docs/areas/alerts.md:106`
   - Replace `` `events` carry no entries (both are encoded as `null`).``
   - with `` `events` carry no entries (both are encoded as empty arrays, `[]`).``
   - **Check:** *Format check*.
3. `docs/areas/alerts.md:143-145`
   - Replace the whole three-line bullet under `## Open questions` with the single line `None.`
   - Keep the heading. `configuration.md:196` already uses this convention.
   - **Check:** *Format check*.
4. `docs/API.md`: no edit. "with empty `states`/`events`" (line 556-557) already matches the fixed behavior.

The sweep found no other enumeration or claim that the change touches:
- `docs/API.md:402` (network returns `null` when disabled) is a different endpoint and does not change.
- `docs/ARCHITECTURE.md`, `README.md` and the PR template's project block need nothing (no config, packaging or route change). The repository has no changelog.

## Test the Tester adds (the only extra launch)
- **Where:** `internal/collector/collector_test.go`, new function `TestCollector_Alerts_EncodesEmptyArrays`, placed after `TestCollector_Alerts_DisabledByDefault`.
- **Shape:** table-driven with two `t.Run` cases. Each builds `New(Config{FastInterval: time.Second, SlowInterval: time.Minute, HistoryCapacity: 10, AlertsEnabled: <case>}, nil)` with no tick. It then runs `json.Marshal(c.Alerts())` (`encoding/json` is already imported) and compares the whole string, failing with got and want:
  - `"AC1 AC3 alerts disabled"` → `{"enabled":false,"states":[],"events":[]}`
  - `"AC2 AC3 alerts enabled, nothing evaluated"` → `{"enabled":true,"states":[],"events":[]}`
- **Budget:** 22 changed lines at most, including the blank line that separates it. No doc comment.
- **Must fail before edit 1** on the AC1 case (`"states":null`).

## Acceptance criteria (from the issue)
- **AC1:** with `alerts.enabled: false`, `GET /api/v1/alerts` answers `200` with `"enabled": false`, `"states": []`, `"events": []`. *Test:* case "AC1 AC3 alerts disabled". The 200 status is unchanged and already covered by `TestHandleAlerts`, `internal/httpapi/handlers_test.go:455`.
- **AC2:** with alerts enabled and nothing to report, both fields are `[]`, not `null`. *Test:* case "AC2 AC3 alerts enabled, nothing evaluated".
- **AC3:** no other field of the response changes. *Test:* both cases compare the full encoded body, so any added, removed or renamed field fails.

## Gates (step 7, *Gates of a patch*, production code row)
*Format check*, *Analyzer gate*, *Test with coverage*, *Coverage gate*, then `scope-check.py --tier patch`. If the test exceeds its 22-line budget, the scope check fails; I would rather have the test shortened than the tier raised.

## Options considered
One option: the issue already fixes the decision. The alternative, documenting `null` in `docs/API.md`, was rejected there. A guard inside `Engine.Report()` or the handler would add code without fixing the one place that builds nil slices.

## Files
- /home/user/PiMonitor/internal/collector/collector.go
- /home/user/PiMonitor/internal/collector/collector_test.go
- /home/user/PiMonitor/docs/areas/alerts.md

RESULT: DONE — tier patch
