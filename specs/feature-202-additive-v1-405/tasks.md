# Tasks: Additive v1 changes, 405 for a wrong method, corrected 503 comment

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `writeMethodNotAllowed` stub and const `allowGetHead` (the registration in `route.register` is real) | internal/httpapi/routes.go | AC2, AC4 | — | Dev | [ ] |
| 2 | G1 | Tests first: `newTestServerWithDashboard` and the AC2–AC4 tests of the plan | internal/httpapi/routes_test.go | AC2, AC3, AC4 | 1 | Tester | [ ] |
| 3 | G1 | Implement the `405` handler and registration, and correct the `routeTable` comment | internal/httpapi/routes.go | AC2, AC3, AC4, AC5 | 2 | Dev | [ ] |
| 4 | G1 | Documentation: API.md introduction and *Errors*, http-api.md (*Routes*, *Limits and errors*, open questions, *Related decisions*), ARCHITECTURE.md `routeTable` paragraph | docs/API.md, docs/areas/http-api.md, docs/ARCHITECTURE.md | AC1, AC2, AC4 | 3 | Dev | [ ] |
