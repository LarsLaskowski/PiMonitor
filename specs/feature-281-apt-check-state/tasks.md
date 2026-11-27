# Tasks: Signal a never-run or failing apt updates check

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `CheckStatus`, `UpdatesCheckOK`, `UpdatesCheckNotChecked`, `UpdatesCheckFailed` | internal/collector/types.go | AC1, AC2, AC3 | — | Dev | [x] |
| 2 | G1 | Failing tests for the collector, the JSON key and `/metrics`; adapt `fullSnapshot()` | internal/collector/collector_test.go, internal/httpapi/handlers_test.go, internal/httpapi/prometheus_test.go | AC1, AC2, AC3 | 1 | Tester | [x] |
| 3 | G1 | Status in `New` and `slowTick`; `writeUpdatesMetrics` guard | internal/collector/collector.go, internal/httpapi/prometheus.go | AC1, AC2, AC3 | 2 | Dev | [x] |
| 4 | G2 | No new API. Failing source-scan tests for the Updates card; adapt `pihole_test.go:59` | internal/web/updates_test.go, internal/web/pihole_test.go | AC4 | 3 | Tester | [ ] |
| 5 | G2 | `renderUpdates` and `aptCheckedLine` | internal/web/assets/app.js | AC4 | 4 | Dev | [ ] |
| 6 | G2 | Documentation updates of the plan | docs/areas/metrics.md, docs/areas/http-api.md, docs/areas/dashboard.md, docs/API.md, docs/integrations/openhab.md, README.md | AC1, AC3, AC4 | 5 | Dev | [ ] |
