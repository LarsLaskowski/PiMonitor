# Tasks: Poll only when visible and guard the fallback values with a test

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: add `let initialLoadStarted = false;` and `async function initialLoad()` (sets the flag, awaits `reloadAll()`), not yet called; no Go API | internal/web/assets/app.js | AC1 | — | Dev | [x] |
| 2 | G1 | Write the AC1 and AC2 scans and the AC3 fallback-value test with its negative cases; confirm the AC1/AC2 scans fail on the skeleton and AC3 passes on today's values | internal/web/polling_test.go, internal/web/thresholds_test.go | AC1, AC2, AC3, AC4 | 1 | Tester | [x] |
| 3 | G1 | Implement: `main` calls `initialLoad` only when visible; the handler's first show runs `initialLoad` instead of the refresh; `reloadAll` starts the timers only while visible | internal/web/assets/app.js | AC1, AC2, AC4 | 2 | Dev | [x] |
| 4 | G1 | Documentation: dashboard area document, ARCHITECTURE.md, as plan *Documentation updates* | docs/areas/dashboard.md, docs/ARCHITECTURE.md | AC1, AC2, AC3 | 3 | Dev | [x] |
| 5 | G1 | Test documentation: *Static-asset guard tests* sentence on `polling_test.go` | docs/UNIT_TESTS.md | AC1, AC2 | 2 | Tester | [x] |
| 6 | PM | Manual browser check, cases (a)–(c) of AC5, on the stamped head | — | AC5 | 3 | Product Manager | [x] |
