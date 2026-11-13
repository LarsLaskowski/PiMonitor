# Tasks: Show the host name in the dashboard header and browser tab

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `SystemInfo.Hostname`, `collector.Config.HostnameEnabled`, `config.Config.HostnameEnabled`, `readHostname` stub, `collectorConfig` with the fields `run()` maps today (plan, *Signatures*) | `internal/collector/types.go`, `internal/collector/collector.go`, `internal/collector/sysinfo.go`, `internal/config/config.go`, `cmd/pimonitor/main.go` | AC1, AC2, AC3 | — | Dev | [ ] |
| 2 | G1 | Failing tests for the collector, config, wiring and API (plan, *Tests per criterion*) | `internal/collector/sysinfo_test.go`, `internal/collector/collector_test.go`, `internal/config/config_test.go`, `cmd/pimonitor/main_test.go`, `internal/httpapi/handlers_test.go` | AC1, AC2, AC3 | 1 | Tester | [ ] |
| 3 | G1 | Implement `readHostname`, `hostname()`, the gated read and warning in `collectSysInfo`, the default, and `run()` using `collectorConfig` | as row 1 | AC1, AC2, AC3 | 2 | Dev | [ ] |
| 4 | G2 | Failing static scans of the dashboard assets (no new API) | `internal/web/hostname_test.go` | AC4, AC5, AC6, AC7, AC8 | 3 | Tester | [ ] |
| 5 | G2 | Header element, System card row, `renderHostname` and its call, styles (plan, *Approach*) | `internal/web/assets/index.html`, `internal/web/assets/app.js`, `internal/web/assets/style.css` | AC4, AC5, AC6, AC7, AC8 | 4 | Dev | [ ] |
| 6 | G2 | Documentation updates (plan, *Documentation updates*) | `docs/areas/metrics.md`, `docs/areas/configuration.md`, `docs/areas/dashboard.md`, `docs/areas/deployment.md`, `docs/API.md`, `packaging/pimonitor.example.yaml`, `README.md`, `AGENTS.md`, `.github/copilot-instructions.md` | AC1, AC2, AC3, AC4, AC5, AC6, AC8 | 5 | Dev | [ ] |
| 7 | PM | Manual test cases 1–4 on a local `make build` of the branch head (plan, *Tests per criterion*) | — | AC3, AC4, AC5, AC6, AC7, AC8 | 6 | Product Manager | [ ] |
