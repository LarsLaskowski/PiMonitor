# Tasks: Refresh the default look: theme Clear (light and dark)

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: no new API (assets only) | — | — | — | Dev | [x] |
| 2 | G1 | Failing tests: palette blocks, colour literals, ink rules, chips, contrast | `internal/web/palette_test.go` | AC2, AC3, AC4, AC12 | 1 | Tester | [x] |
| 3 | G1 | Tokens, status rules, chips, cards, dialogs, header, footer styles | `internal/web/assets/style.css` | AC2, AC3, AC4, AC12 | 2 | Dev | [x] |
| 4 | G2 | Skeleton: no new API (assets only) | — | — | 3 | Dev | [x] |
| 5 | G2 | Failing tests: emoji scan, SVG icons, chevrons, mode icon, network names, touch targets, bar split, no inline style or other origin; adapt `hostname_test.go` | `internal/web/icons_test.go`, `internal/web/look_test.go`, `internal/web/hostname_test.go` | AC1, AC5, AC6, AC9, AC10, AC11, AC13 | 4 | Tester | [x] |
| 6 | G2 | Icons, header, chevrons, bar spans, templates, favicon; `updateThemeToggle`, `renderBar`, `barRow`, `networkRow`; matching styles | `internal/web/assets/index.html`, `internal/web/assets/app.js`, `internal/web/assets/style.css` | AC1, AC5, AC6, AC9, AC10, AC11, AC13 | 5 | Dev | [x] |
| 7 | G3 | Skeleton: no new API (assets only) | — | — | 6 | Dev | [ ] |
| 8 | G3 | Failing tests: sparkline style, gauge bound and value fit | `internal/web/chart_test.go`, `internal/web/gauge_test.go` | AC7, AC8 | 7 | Tester | [ ] |
| 9 | G3 | Sparkline line, gradient, dot; gauge stroke, track token, value inside the arc | `internal/web/assets/chart.js`, `internal/web/assets/gauge.js`, `internal/web/assets/index.html`, `internal/web/assets/style.css` | AC7, AC8 | 8 | Dev | [ ] |
| 10 | G3 | Manual check in headless Chrome on `make run` of the stamped head: 320, 900, 1400 px, light and dark, mode switch, CSP console | — | AC7, AC8, AC10, AC11 | 9 | Tester | [ ] |
| 11 | G3 | Area document, areas index, architecture, README wording | `docs/areas/dashboard.md`, `docs/areas/README.md`, `docs/ARCHITECTURE.md`, `README.md` | AC13 | 9 | Dev | [ ] |
| 12 | G3 | Test documentation sentence | `docs/UNIT_TESTS.md` | AC13 | 8 | Tester | [ ] |
