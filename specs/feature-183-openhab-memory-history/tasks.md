# Tasks: openHAB memory history in the dashboard

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `OpenHAB.HistoryWindowHours`, `OpenHAB.HistoryCapacity`, `OpenHAB.HistoryWindow`; `OpenHABConfig.HistoryWindowHours`, `OpenHABConfig.HistoryCapacity`; `OpenHABStatus.HistoryWindowHours`; `History.OpenHABRSSBytes`, `History.OpenHABHeapUsedBytes`, `History.OpenHABRestarts` | `internal/config/config.go`, `internal/collector/openhab.go`, `internal/collector/collector.go` | AC1–AC9 | — | Dev | [ ] |
| 2 | G1 | Tests first for config, collector, persistence and handlers, and the adapted pins | `internal/config/config_test.go`, `cmd/pimonitor/main_test.go`, `internal/collector/openhab_test.go`, `internal/collector/collector_test.go`, `internal/collector/persist_test.go`, `internal/httpapi/handlers_test.go` | AC1–AC6, AC9 | 1 | Tester | [ ] |
| 3 | G1 | Option and validation, wiring, sample and restart, waiting slice and drain, read cut, `Since`, kinds 12–14 | `internal/config/config.go`, `cmd/pimonitor/main.go`, `internal/collector/openhab.go`, `internal/collector/collector.go`, `internal/collector/persist.go` | AC1–AC6, AC9 | 2 | Dev | [ ] |
| 4 | G1 | Coverage pass | `internal/**/*_test.go`, `cmd/pimonitor/main_test.go` | AC1–AC6, AC9 | 3 | Tester | [ ] |
| 5 | G2 | Skeleton: no new API (browser code) | — | AC7, AC8 | 4 | Dev | [ ] |
| 6 | G2 | Source-scan tests for the chart, the card block and the history keys | `internal/web/chart_test.go`, `internal/web/openhab_test.go`, `internal/web/history_test.go` | AC7, AC8 | 5 | Tester | [ ] |
| 7 | G2 | `drawTimeChart`, history keys and trim, card block, legend styles | `internal/web/assets/chart.js`, `internal/web/assets/app.js`, `internal/web/assets/style.css` | AC7, AC8 | 6 | Dev | [ ] |
| 8 | G2 | Documentation updates of the plan | `docs/areas/history.md`, `docs/areas/openhab.md`, `docs/areas/dashboard.md`, `docs/areas/configuration.md`, `docs/areas/http-api.md`, `docs/API.md`, `docs/ARCHITECTURE.md`, `docs/CONTRIBUTING.md`, `AGENTS.md`, `.github/copilot-instructions.md`, `README.md`, `packaging/pimonitor.example.yaml`, `docs/integrations/openhab.md` | AC1–AC9 | 7 | Dev | [ ] |
| 9 | G2 | `.squad/project.md` v1 guarantee line, text reported by the Dev | `.squad/project.md` | AC4, AC9 | 8 | Orchestrator | [ ] |
| 10 | G2 | Manual check of AC7 and AC8 on a Pi with openHAB, stamped head built with `make build-arm64` | — | AC7, AC8 | 9 | Product Manager | [ ] |
