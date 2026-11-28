# Tasks: Upper bound for alerts.for_seconds and alerts.openhab_for_seconds

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `maxAlertWindowSeconds` and `validateAlertWindow` returning `nil`, not yet called (no new exported API) | `internal/config/config.go` | AC1, AC2 | — | Dev | [ ] |
| 2 | G1 | Tests first: `TestValidate_AlertWindowUpperBound`, `TestAlertFor_AtUpperBound_AC1`, `TestOpenHABAlertFor_AtUpperBound_AC2` | `internal/config/config_test.go` | AC1, AC2 | 1 | Tester | [ ] |
| 3 | G1 | Tests first: `TestRun_AlertWindowAboveBound_AC3` | `cmd/pimonitor/main_test.go` | AC3 | 1 | Tester | [ ] |
| 4 | G1 | Implement `validateAlertWindow` and call it for both keys in `Validate()` in place of the two `< 0` checks | `internal/config/config.go` | AC1, AC2, AC3 | 2, 3 | Dev | [ ] |
| 5 | G1 | Documentation: the configuration area document, the example config comments, and a link to 0034 | `docs/areas/configuration.md`, `packaging/pimonitor.example.yaml` | AC1, AC2 | 4 | Dev | [ ] |
