# Tasks: Let the user choose a dashboard theme, stored per browser

Plan: [plan.md](plan.md)

| # | Group | Task | Files | Criteria | Depends on | Owner | Done |
| - | ----- | ---- | ----- | -------- | ---------- | ----- | ---- |
| 1 | G1 | Skeleton: `PIMONITOR_THEMES`, `DRAWING_FALLBACKS`, stubs of `themeColor` and `themeNumber` in `theme-init.js`; `STYLE_KEY` and empty picker functions in `app.js`; picker fieldset and `aria-label="Toggle mode"` in `index.html` | `internal/web/assets/theme-init.js`, `internal/web/assets/app.js`, `internal/web/assets/index.html` | AC1–AC17 | — | Dev | [x] |
| 2 | G1 | Tests first: `theme_test.go` and the adapted `checkUpdateThemeToggle` / `checkApplyThemeRedraws` | `internal/web/theme_test.go`, `internal/web/icons_test.go` | AC1–AC17 | 1 | Tester | [x] |
| 3 | G1 | Implement: stored theme applied in `theme-init.js`, readers, renderers on the readers, picker, `selectStyle`/`applyStyle`/`repaintCanvases`, mode labels, picker CSS | `internal/web/assets/*` | AC1–AC17 | 2 | Dev | [x] |
| 4 | G1 | Manual check M1–M11 with the probe patch in a scratch copy; results into the verification matrix | — | AC1–AC8, AC11, AC13, AC17 | 3 | Tester | [ ] |
| 5 | G1 | Documentation: `docs/areas/dashboard.md`, `docs/ARCHITECTURE.md`, `README.md` as *Documentation updates* | `docs/areas/dashboard.md`, `docs/ARCHITECTURE.md`, `README.md` | AC1–AC17 | 3 | Dev | [ ] |
