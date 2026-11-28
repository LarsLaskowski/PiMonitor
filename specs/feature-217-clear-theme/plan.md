# Plan: Refresh the default look: theme Clear (light and dark)

Source: Issue #217 | [spec.md](spec.md)
Tier: standard — a user-visible behaviour and look change of the dashboard assets that creates, changes or removes no
security control (CSP and the `innerHTML` rule stay as they are).
Security areas touched: Dashboard XSS (`app.js` builds new DOM nodes for the bar labels and the network rates; CSP
compatibility of the new markup)

## Problem / root cause

See [spec.md](spec.md). Related defect found while planning, fixed here (spec E8): the openHAB process-memory value
and the `Things offline` toggle never show their alert colour (fact 4).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Ten cards exist, the openHAB card included (issue says nine) | `index.html` 32–127 | confirmed |
| 2 | Header buttons are 36 px; light status text below 4.5:1 | `style.css` 100–101 (`2.25rem`); computed 3.30:1 / 3.19:1 | confirmed |
| 3 | The issue's AC12 token pairs reach 4.5:1 in both modes (lowest: light `--ok-ink` on `--bg` 4.56:1); light updates-button label at 10 % tint 4.49:1, at 8 % 4.65:1; `#fff` on dark accent 2.54:1 | WCAG formula over the issue's values, scratch script | confirmed |
| 4 | `.bar-label .bar-pct` (0,2,0) and `.openhab-things summary` (0,1,1) beat `.metric-warn/.metric-crit` (0,1,0) | `style.css` 286, 417, 353–359; `app.js` 471–478 | confirmed |
| 5 | Pi-hole status colour relies on rule order (`.kv-val` and `.metric-*` both 0,1,0) | `style.css` 331, 349; `app.js` 372–373 | confirmed |
| 6 | `gauge_test.go` has no clipping test; the radius bound is in `gauge.js` 30–38 | `gauge_test.go` 13 (only test) | confirmed |
| 7 | At the narrowest gauge (72 px wide, canvas 70 px tall) the arc radius is 27.5 px, inner width at the baseline 46 px; `88.88` in bold system-ui measures 52.0 px at 18 px and 41.6 px at 14.4 px | `gauge.js` formula; headless Chrome `measureText` with the sandbox's fonts (other fonts: the AC8 manual check) | confirmed |
| 8 | 72 px = (320 − 48 main padding − 36 card padding − 2 border − 2 × 8 gap) / 3 | `style.css` 160, issue §6 | confirmed |
| 9 | `chart.js` fill is `color + '26'`, valid only for a six-digit hex | `chart.js` 57 | confirmed |
| 10 | The detail chart uses `drawSparkline`, so it gets the sparkline style without extra code | `app.js` 964 | confirmed |
| 11 | ↑ ↓ → · ° × are not `Extended_Pictographic`; ⚙ ☀ ⚠ ⚡ 🌙 are | Node 22 ICU (Unicode 17.0), 156 ranges | confirmed |
| 12 | `hostname_test.go` pins the `<h1>` markup with the emoji and `<header>\s*<div class="header-brand">\s*<h1>` | `hostname_test.go` 14, 113 | confirmed |
| 13 | The assets are outside both coverage measures | `COVERAGE_PATHSPECS = ["*.go"]`; `sonar.coverage.exclusions=internal/web/assets/**` | confirmed |
| 14 | Canvas `destination-out` with a linear gradient fades any fill colour to transparent | to verify: Tester, manual check of AC7 in headless Chrome, step 7 | unverified |

## Tests per criterion

All tests read the embedded assets as text (`readAsset`), as the existing `internal/web` tests do; criterion ids in
the subtest names.

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestAssets_NoEmoji_AC1` (index.html, app.js; literal, `&#…;`/`&#x…;`, `\uXXXX` with surrogate pairs, `\u{…}`; cases: `&#9881;&#65039;` and `☀` caught, `→` and `↑` allowed); `TestIndexHTML_IconsAreInlineSVG_AC1` (each `card-*` `<h2>`, `.logo-tile`, both header buttons, `#alert-banner` hold an `<svg viewBox="0 0 24 24"` with `aria-hidden="true"`; favicon `data:image/svg+xml` without `<text`) | `internal/web/icons_test.go` |
| AC2 | `TestStyleCSS_PaletteBlocks_AC2` (issue §1 values, `--accent-<card>` values, `--dialog-shadow`, `--backdrop`, `--spark-fill-alpha`, `--chip-tint`, `--btn-tint`, `--btn-border-tint`, radius/padding/gap tokens; dark blocks equal; no `--gauge-track`); `TestStyleCSS_NoColourLiteralOutsidePalette_AC2` | `internal/web/palette_test.go` |
| AC3 | `TestStyleCSS_StatusTextUsesInk_AC3` (selector table in *Approach*; no `color:` declaration uses `var(--ok)`, `var(--warn)` or `var(--crit)`; fills use the base token; `.range-button.active`, `.apikey-submit`, `.logo-tile` use `--on-accent`) | `internal/web/palette_test.go` |
| AC4 | `TestStyleCSS_CardChips_AC4` (no `border-top` on `.card`, no `border-top` with `--card-accent`; `.accent-<id>` → `var(--accent-<id>)` for all ten; chip 28 px, radius token, `color-mix` with `var(--chip-tint)`) | `internal/web/palette_test.go` |
| AC5 | `TestStyleCSS_TouchTargets_AC5` (selectors in *Approach*) | `internal/web/look_test.go` |
| AC6 | `TestAppJS_BarLabelSplit_AC6` (`renderBar`, `barRow`: two elements, `textContent`, level class only at warn/crit); CSS rules of *Approach* | `internal/web/look_test.go` |
| AC7 | `TestChartJS_Sparkline_AC7` (`lineWidth = 2`, `lineJoin = 'round'`, `createLinearGradient`, `destination-out`, `--spark-fill-alpha`, newest-point `arc(` with `--card-bg` ring; `points.length < 2` return before any drawing; no `color + '`); manual: Tester, headless Chrome on `make run` of the stamped head, light and dark | `internal/web/chart_test.go` |
| AC8 | `TestGaugeJS_ArcNeverClipped_AC8` (`lineWidth = 9`, the `Math.min(cssWidth / 2 - lineWidth / 2 - margin, cy - lineWidth / 2 - margin)` bound kept, `--track`; table over widths 72, 100, 150, 250, 400 px computing the bound in Go: arc inside the canvas; at 72 px `2.89 em × font` ≤ inner width, font = `clamp(12px, 19cqi, 18px)`); CSS `.gauge`, `.gauge-dial`, `.gauge-value` rules; manual: Tester at 320, 900, 1400 px viewports | `internal/web/gauge_test.go` |
| AC9 | `TestIndexHTML_ClickableCardChevron_AC9` (four heading buttons end with an `aria-hidden` chevron SVG, `aria-label` values unchanged); existing badge-suffix tests pass | `internal/web/icons_test.go` |
| AC10 | `TestModeToggle_IconByCSS_AC10` (both icons in `#theme-toggle`; CSS shows the moon in light and the sun under both dark selectors; `updateThemeToggle` sets `aria-label`/`aria-pressed` as today and no `textContent`; `applyTheme` still redraws); manual: Tester toggles in Chrome | `internal/web/icons_test.go` |
| AC11 | `TestIndexHTML_NoInlineStyle_AC11` (no `style=` attribute, no `<style`, no inline `<script>` body); `TestAssets_NoOtherOrigin_AC11` (no `http(s)://` or `//host` reference except the SVG namespace `http://www.w3.org/2000/svg`); manual: Tester, no CSP violation in Chrome's console | `internal/web/look_test.go` |
| AC12 | `TestStyleCSS_Contrast_AC12` (WCAG ratio from the parsed tokens, both modes; updates-button label over `--accent` mixed at `--btn-tint` into `--card-bg`) | `internal/web/palette_test.go` |
| AC13 | All existing `internal/web` tests; `TestAppJS_NetworkRateNames_AC13` (`networkRow` clones `tpl-icon-download`/`tpl-icon-upload`, adds `Download`/`Upload` in `.sr-only` via `textContent`) | `internal/web/icons_test.go` |

## Approach

Layout as issue §4–§12 and the spec fix it; what the issue leaves open is fixed here.

**Tokens (`style.css`).** The three palette blocks hold: issue §1; `--accent-system` … `--accent-openhab` (issue §2,
openHAB `#0369a1`/`#38bdf8`); `--chip-tint` 12 %/16 %; `--btn-tint` 8 %/14 %; `--btn-border-tint` 30 %/35 %;
`--spark-fill-alpha` 0.28/0.30; `--dialog-shadow` `0 12px 40px rgba(0, 0, 0, 0.35)` and `--backdrop`
`rgba(0, 0, 0, 0.5)` (today's values, both modes); `--radius-card` 14px, `--radius-control` 12px, `--radius-button`
10px, `--radius-chip` 8px, `--card-padding` `8px 18px 18px`, `--gap` 16px (same in both modes). `.accent-<id>
{ --card-accent: var(--accent-<id>); }`. `body` gets `font-variant-numeric: tabular-nums`.

**Status colours.** `.metric-ok/-warn/-crit` → ink. Rules at least as specific as the default:
`.kv-val.metric-ok|warn|crit`, `.bar-label .bar-pct.metric-warn|crit`, `.openhab-things summary.metric-warn|crit`,
`.alert-banner.metric-warn|crit` (text ink, background base at 14 %, border base at 35 %), `.alert-badge.metric-*`,
`.stale-badge` (`--warn-ink`), `.updates-table td.pkg-new` (`--ok-ink`), `.apikey-error` (`--crit-ink`). `.card-warn`
keeps `border-color: var(--warn)`.

**Markup (`index.html`).** Icons inline (record 0040): `<svg class="icon" viewBox="0 0 24 24" fill="none"
stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">`; sizes by
context in CSS (chip 16, header buttons and banner 18, logo 22). Header: `<div class="header-brand"><span
class="logo-tile" aria-hidden="true">…</span><div class="header-title"><h1>PiMonitor<span class="header-hostname …">…
</h1><div class="subtitle" …></div></div></div>`; `header` wraps (`flex-wrap: wrap`); `.header-title` has `min-width:
0`. `#theme-toggle` holds `.mode-icon-moon` and `.mode-icon-sun`; CSS hides the sun by default and the moon under
`:root[data-theme="dark"]` and under `@media (prefers-color-scheme: dark) { :root:not([data-theme]) … }`. Clickable
headings end with a chevron (`margin-left: auto`). Memory & Swap: `<span class="bar-pct" id="mem-pct">` and `<span
class="bar-bytes" id="mem-bytes">` (likewise `swap-`). Load gauges: `<div class="gauge"><div class="gauge-dial"><canvas
id="gauge-load1"></canvas><div class="gauge-value" id="load1-value">` …`</div></div><div class="gauge-label">`.
Two `<template id="tpl-icon-download">`/`tpl-icon-upload` hold the arrow SVGs. Favicon: the logo drawing, stroke
`%232563eb`.

**Scripts.** updateThemeToggle keeps its `aria-label`/`aria-pressed` logic and drops the `textContent` line.
renderBar (parameters barId, pctId, bytesId, value, warn, crit, bytesText) and barRow (name, pct, warn, crit,
bytesText) write the percentage (formats as today: `41.2 %`, `34.2%`) and `· ` + bytes into two elements by `textContent`; the
percentage gets `metric-warn`/`metric-crit` only at that level. openHAB rows keep one `.bar-pct` value, now in `--text`
weight 600 like the bar percentages (their alert colour now shows). New networkRow (parameter n) builds `.net-row` (name left;
per rate: cloned template icon, `<span class="sr-only">Download </span>`/`Upload `, the rate), rows divided by
`--border`; `applyNetworkVisibility` calls it. `chart.js drawSparkline`: line 2 px, round joins; fill path drawn first
in `color` with `globalAlpha = --spark-fill-alpha`, then `globalCompositeOperation = 'destination-out'` with a vertical
gradient from transparent (top) to opaque (bottom), clipped to the fill path, reset to `source-over`; then the line
and the dot (radius 4, ring 3 px in `--card-bg`). Height 52 px. `gauge.js`: `lineWidth = 9`, reads `--track`.

**Gauge value.** `.gauge { container-type: inline-size; }`, `.gauge-dial { position: relative; }`, `.gauge canvas`
70 px; `.gauge-value { position: absolute; left: 0; right: 0; bottom: 6px; text-align: center; line-height: 1;
font-size: clamp(12px, 19cqi, 18px); font-weight: 700; }` — at 72 px: 13.7 px, `88.88` ≈ 39.6 px ≤ 46 px.

**Touch targets (AC5).** `min-height: 44px` on `.card h2`, `.card-header-btn`, `.updates-button`, `.range-button`,
`.layout-reset`, `.apikey-submit`; `width`/`height: 44px` on `.theme-toggle`, `.layout-toggle`, `.modal-close`,
`.layout-move`. Each as its own rule or a list the test's selector-list parsing reads.

Early draft PR: no

## Options considered

- Icons: inline SVG at each use (chosen) vs a `<symbol>`/`<use>` sprite, images/icon font, emoji — record 0040.
- Mode icon by CSS (chosen: right before first paint, no script) vs JS toggling `hidden` (wrong icon until `app.js`
  runs).
- Sparkline fade by `destination-out` (chosen: any colour) vs `color-mix()` strings in `addColorStop` (canvas support
  not verified) vs today's hex suffix (hex only, spec N5).
- Gauge value: `clamp` with `cqi` (chosen) vs a fixed 18 px (52 px > 46 px at 72 px, fact 7) vs `measureText`
  scaling in JS (more script for the same result).
- Per-card colours as palette tokens (chosen, record 0038) vs `.accent-*` rules repeated under both dark selectors.
- Emoji scan: a range table generated from Node's ICU (chosen) vs Go's category `So` (flags `°`) vs Unicode's
  `emoji-data.txt` (host not reachable from the session).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/web | `assets/style.css` | tokens, rules above |
| internal/web | `assets/index.html` | icons, header, chevrons, bar spans, gauge dial, templates, favicon |
| internal/web | `assets/app.js` | `updateThemeToggle`, `renderBar`, `barRow`, `networkRow`, callers |
| internal/web | `assets/chart.js`, `assets/gauge.js` | sparkline style, gauge stroke and track token |

## Signatures (for the Dev's skeleton)

No Go API changes, so no skeleton: the assets are edited in step 6. The JavaScript functions the tests read are
described in *Approach* (renderBar gains a bytes element id, barRow renames its last parameter, networkRow is new,
drawSparkline and drawGauge keep their parameters).

## Test files

New: `internal/web/icons_test.go`, `internal/web/palette_test.go`, `internal/web/look_test.go`; extended:
`internal/web/chart_test.go`, `internal/web/gauge_test.go`. Shared helpers: reuse `readAsset`, `jsFunctionBody`,
`cssDeclarations`, `requireCSS`, `checkAssetParts`; a palette-block parser and the WCAG ratio live once in
`palette_test.go`; the Extended_Pictographic table in `icons_test.go`, its Node generator command in a comment.

Existing tests adapted (Tester): `hostname_test.go` — `hostnameHeaderH1` without the emoji, the header regex allowing
the logo tile and `.header-title` before `<h1>`.

Files no unit test can cover: the five assets (browser code, no JS test runner); already outside both coverage
measures (fact 13, `docs/UNIT_TESTS.md`); no new exclusion.

## Areas

- `docs/areas/dashboard.md` (3226 words, +260): *Scope* "theme" → "mode"; *Theme* → *Mode* (button wording); new
  `### Look` after it; *Related decisions* 0038–0040.

## Documentation updates

- `docs/areas/dashboard.md` (Dev): as *Areas*; *Look* states: status text in ink, fills in base colour; bar
  percentage emphasised and coloured at `warn`/`crit`; gauge value inside the arc; chevron on clickable cards; card
  colour only on the chip and the highlight; icons inline SVG, no emoji; network rates named Download/Upload; 44 px
  touch targets; contrast at least 4.5:1, guarded by a test. The anchor `#look` is linked by records 0038–0040.
- `docs/areas/README.md` row Dashboard (Dev): "theme" → "mode and look".
- `docs/ARCHITECTURE.md` line 26 (Dev): "layout and theme" → "layout, mode and look".
- `README.md` line 55 (Dev): "Light/dark theme toggle" → "Light/dark mode toggle".
- `docs/UNIT_TESTS.md` *Static-asset guard tests* (Tester): one sentence on `palette_test.go` (tokens, contrast) and
  `icons_test.go` (emoji scan).
- PR template: no API/config/packaging change; release-notes label by the orchestrator. Records: me.

## Architecture check

CSP (`withSecurityHeaders`) unchanged and still sufficient: no inline style or script, SVG inline, favicon `data:`.
No build step, no external resource, no API change; guarantees of `.squad/project.md` untouched.

## Security considerations

- [x] No limits: no parsed input is added.
- [x] None: no exception reaches a user-visible message.
- Server text (interface names, mountpoints) stays `textContent`; templates are cloned, never filled from strings.

## Decision records

- `docs/decisions/0038-dashboard-mode-and-theme-as-token-overrides.md` (Proposed)
- `docs/decisions/0039-card-colour-only-on-icon-chip.md` (Proposed)
- `docs/decisions/0040-dashboard-icons-inline-svg.md` (Proposed)

## Out of scope / follow-ups

Spec *Out of scope*. Follow-up issue: "Replace the README screenshot with the Clear look, light and dark".
