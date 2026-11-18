# Tasks: Alert rules for a local openHAB

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `Thresholds` fields OpenHABRSSWarnPercent, OpenHABRSSCritPercent, OpenHABThingsOfflineWarn, OpenHABThingsOfflineCrit; `Alerts` field OpenHABForSeconds; `OpenHABAlertFor` on `Config` and on `collector.Config` | internal/config/config.go, internal/collector/collector.go | AC7, AC8 | — | Dev | [ ] |
| 2 | G1 | Tests first: options, defaults, validation texts; `/api/v1/config` mapping | internal/config/config_test.go, cmd/pimonitor/main_test.go | AC7, AC8 | 1 | Tester | [ ] |
| 3 | G1 | Defaults, `validateFinite`, `checkIntegerOptions`, `Thresholds.validate`, `alerts.openhab_for_seconds` check; `main.go` mapping | internal/config/config.go, cmd/pimonitor/main.go | AC7, AC8 | 2 | Dev | [ ] |
| 4 | G2 | Skeleton: `New` signature, `MetricOpenHABDown`, `MetricOpenHABRSSPercent`, `MetricOpenHABThingsOffline`, `OpenHABThing`, `OpenHABSample`, `EvaluateOpenHAB`, `Event` field Things; `alert.New` call in the collector | internal/alert/alert.go, internal/alert/openhab.go, internal/collector/collector.go | AC1, AC2, AC3, AC4, AC5, AC6, AC8, AC9 | 3 | Dev | [ ] |
| 5 | G2 | Tests first: engine rules, window, order, pruning, default RSS sample, JSON; payload and template; rate limit across feeds; adapt the 13 `New` calls | internal/alert/openhab_test.go, internal/alert/notify_test.go, internal/alert/alert_test.go | AC1, AC2, AC3, AC4, AC5, AC6, AC7, AC8, AC9 | 4 | Tester | [ ] |
| 6 | G2 | Engine evaluation and pruning in fixed order; `evalMetric` window and cutoff inputs; `eventView.Things`; per-feed pruning in `recordSent` | internal/alert/alert.go, internal/alert/openhab.go, internal/alert/notify.go | AC1, AC2, AC3, AC4, AC5, AC6, AC8, AC9 | 5 | Dev | [ ] |
| 7 | G3 | Skeleton: no new API (private `onAlert` hook and counted-Thing figures) | — | AC1, AC2, AC3, AC5, AC6, AC8 | 6 | Dev | [ ] |
| 8 | G3 | Tests first: alert sample per state, counted Things, hook per poll and for `not_installed`, 256-byte cut, owner check, wiring, polls evaluated once | internal/collector/openhab_test.go, internal/collector/openhab_linux_test.go, internal/collector/collector_test.go | AC1, AC2, AC3, AC5, AC6, AC8 | 7 | Tester | [ ] |
| 9 | G3 | Counted Things in `summarizeThings` with the 256-byte cut, alert sample in `Poll`, `onAlert` wiring in `collector.New` | internal/collector/openhab.go, internal/collector/collector.go | AC1, AC2, AC3, AC5, AC6, AC8 | 8 | Dev | [ ] |
| 10 | G4 | Skeleton: no new API (dashboard assets) | — | AC10 | 6 | Dev | [ ] |
| 11 | G4 | Tests first: badge mapping and highlight source checks | internal/web/alerts_test.go | AC10 | 10 | Tester | [ ] |
| 12 | G4 | Badge in the openHAB card, `ALERT_BADGE_IDS`, `applyOpenHABAlertLevels` | internal/web/assets/index.html, internal/web/assets/app.js | AC10 | 11 | Dev | [ ] |
| 13 | G5 | No new API. Area documents, `API.md`, example YAML, integration guide, README, SECURITY.md, ARCHITECTURE.md (plan, *Areas* and *Documentation updates*) | docs/areas/*.md, docs/API.md, docs/ARCHITECTURE.md, docs/integrations/openhab.md, packaging/pimonitor.example.yaml, README.md, SECURITY.md | AC1, AC2, AC3, AC4, AC5, AC6, AC7, AC8, AC9, AC10 | 9, 12 | Dev | [ ] |
| 14 | G5 | `.squad/project.md` entries, text from the plan | .squad/project.md | AC8, AC9 | 13 | Orchestrator | [ ] |
| 15 | PM | Manual check of AC10 on the Pi with the PR head | — | AC10 | 12 | Product Manager | [ ] |
