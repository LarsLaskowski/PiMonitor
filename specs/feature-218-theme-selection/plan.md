# Plan: Let the user choose a dashboard theme, stored per browser

Source: Issue #218 | [spec.md](spec.md)
Tier: standard — a behavior change of the dashboard (a new per-browser preference, its selector and the canvas
token readers) that creates, changes or removes no security control.
Security areas touched: none

## Problem / root cause

See [spec.md](spec.md) (AC1–AC17).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The mode is `pimonitor-theme` / `data-theme`, applied before first paint by `theme-init.js`, a sync `<script>` in `<head>` after `style.css` | `theme-init.js:9-19`; `index.html:8-9` | confirmed |
| 2 | Clear's palette is three unscoped blocks (`:root`, `:root[data-theme="dark"]`, `@media (prefers-color-scheme: dark) :root:not([data-theme])`) with the same 37 tokens | `style.css:1,46,86-87`; key sets compared by a scratch script (identical) | confirmed |
| 3 | `applyTheme` repaints canvases via `renderMetrics`/`renderHistory`; `renderHistory` redraws sparklines, the openHAB memory chart and the detail chart | `app.js:90-99,1255-1272,1250-1252` | confirmed |
| 4 | Canvas properties read: `--accent` (`chart.js:6`, `app.js:1414`), `--spark-fill-alpha` `|| 0.28` (`chart.js:43`), `--card-bg` (`chart.js:44`), `--track` (`gauge.js:43`), `--ok/--warn/--crit` without fallback (`gauge.js:44`), `--muted` with fallback `#2563eb` (Clear's `--accent`, not `--muted`), `--warn` (`app.js:1414-1421`); line widths are literals (`chart.js:85,135,147`, `gauge.js:28`) | source | confirmed |
| 5 | The layout dialog body is hint `<p class="layout-hint">`, `#layout-list`, `#layout-reset`; `openLayoutModal` rebuilds the list on open; `resetLayout` touches only the layout | `index.html:167-177`; `app.js:777-858` | confirmed |
| 6 | Today's storage entries are four: `pimonitor-api-key`, `pimonitor-theme`, `pimonitor-layout`, `pimonitor-openhab-things` | `app.js:37,65,590,1336`; `dashboard.md` *Browser storage* | confirmed |
| 7 | Mode button labels: static `aria-label="Toggle theme"`, script `'Switch to light theme' : 'Switch to dark theme'`, asserted by `checkUpdateThemeToggle` | `index.html:22`; `app.js:86`; `icons_test.go:350-362` | confirmed |
| 8 | `checkApplyThemeRedraws` asserts `renderMetrics(latestSnapshot)` and `renderHistory(latestHistory)` inside `applyTheme` | `icons_test.go:365-369` | confirmed |
| 9 | JS assets are outside the coverage measure (`COVERAGE_PATHSPECS = ["*.go"]`; `sonar.coverage.exclusions` for the assets) and the tests in `internal/web` check sources only | `squad_settings.py:30`; `sonar-project.properties`; `stack.md` *Layout* | confirmed |
| 10 | Inline style/script and foreign origins are already guarded (`look_test.go` AC11 tests), `innerHTML` by `xss_test.go` | source | confirmed |
| 11 | Top-level `const` and function declarations of one classic script are visible to later classic scripts | HTML/ECMAScript global lexical environment; `app.js` already calls `drawGauge` from `gauge.js` | confirmed |
| 12 | `CSS.supports('color', v)` answers false for a value that is no colour | to verify: Tester, manual case M9 (`--track: notacolor` draws `#e8eaef`), step 6 | unverified |
| 13 | Headless `google-chrome` and `node` 22 are on the session host | `which` → `/usr/bin/google-chrome`, `/opt/node22/bin/node` | confirmed |
| 14 | Records 0038–0040 are unreleased | `git tag --contains` of 0038's adding commit: empty; latest tag v1.4.1 | confirmed |

## Tests per criterion

All Go tests read the embedded assets as text, like the existing ones. A test-only second registry entry exists as
fixture strings in `theme_test.go` (for the source parsers of AC9/AC10) and as the probe patch of the manual check;
it never ships.

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestThemeInit_DefaultLeavesNoAttribute_AC1` (sets `dataset.style` only for a registered id other than `PIMONITOR_THEMES[0].id`; `<html>` in `index.html` has no `data-style`); manual M1 | `internal/web/theme_test.go` |
| AC2 | `TestThemeInit_AppliesStoredThemeBeforePaint_AC2` (reads `pimonitor-style`; `theme-init.js` stays sync in `<head>` before `<body>`; registry parser on a two-entry fixture); manual M2 | `internal/web/theme_test.go` |
| AC3 | `TestThemeInit_UnknownValueIsKept_AC3` (exact `=== ` match, no `toLowerCase`/`trim` on the value, no `removeItem` of `STYLE_KEY` anywhere); manual M3 | `internal/web/theme_test.go` |
| AC4 | `TestAppJS_StylePickerFromRegistry_AC4` (`buildStylePicker` iterates `PIMONITOR_THEMES`, checks the option whose id is `activeStyle()`, is called from `openLayoutModal`; `#style-picker` precedes `.layout-hint`); manual M4 | `internal/web/theme_test.go` |
| AC5 | `TestAppJS_SelectStyleAppliesAndStores_AC5` (`selectStyle` stores via `setItem(STYLE_KEY,` then `applyStyle`; `applyStyle` calls `repaintCanvases()`; no `.close()` in the picker code); adapted `checkApplyThemeRedraws`; manual M5 | `internal/web/theme_test.go`, `internal/web/icons_test.go` |
| AC6 | `TestStylePicker_HiddenWithOneTheme_AC6` (static `hidden` on `#style-picker`; `buildStylePicker` sets `hidden` from `PIMONITOR_THEMES.length < 2`); manual M6 | `internal/web/theme_test.go` |
| AC7 | `TestAppJS_ModeAndThemeIndependent_AC7` (`applyStyle` never writes `dataset.theme`, `applyTheme`/`toggleTheme` never `dataset.style`); AC10's block test for the OS-dark selector of every theme; manual M7 | `internal/web/theme_test.go` |
| AC8 | `TestAppJS_StyleStorageFailureIsCaught_AC8` (both storage calls in `try`; `activeStyle` reads the attribute, not storage); manual M8 | `internal/web/theme_test.go` |
| AC9 | `TestThemeRegistry_SingleSource_AC9` (ids parsed from `theme-init.js` unique, default `clear` first; `app.js` holds no `{ id: '…', name:` entry and no registered id as a literal; parser on a two-entry fixture) | `internal/web/theme_test.go` |
| AC10 | `TestStyleCSS_ThemeTokenSets_AC10` (for each registered id its three blocks — Clear's unscoped — share one key set; each property passed to `themeColor`/`themeNumber` in `chart.js`, `gauge.js`, `app.js` is in it; checker run on a fixture with a probe theme missing a token reports it) | `internal/web/theme_test.go` |
| AC11 | `TestIndexHTML_StylePickerIsRadioGroup_AC11` (`<fieldset id="style-picker">` with `<legend>Theme</legend>`; `buildStylePicker` creates `input` with `type = 'radio'` and `name = 'style-choice'` inside a `label`); existing `dialog_test.go` unchanged; manual M10 | `internal/web/theme_test.go` |
| AC12 | `TestAssets_StorageKeys_AC12` (every `pimonitor-…` string literal in the JS assets is one of the five keys, all five occur) | `internal/web/theme_test.go` |
| AC13 | `TestAppJS_ResetLayoutKeepsTheme_AC13` (`resetLayout` body mentions no `STYLE_KEY`/`applyStyle`/`selectStyle`); manual M11 | `internal/web/theme_test.go` |
| AC14 | existing `look_test.go` (AC11 tests) and `xss_test.go`, unchanged; `TestThemeInit_AppliesStoredThemeBeforePaint_AC2` for the sync script | `internal/web/look_test.go`, `internal/web/xss_test.go` |
| AC15 | adapted `checkUpdateThemeToggle` (`'Switch to light mode' : 'Switch to dark mode'`); `TestIndexHTML_ModeButtonLabel_AC15` (static `aria-label="Toggle mode"`; no `aria-label` in `index.html` or `app.js` contains "theme") | `internal/web/icons_test.go`, `internal/web/theme_test.go` |
| AC16 | `TestIndexHTML_ThemesLoadedStatically_AC16` (only `style.css` is linked, no `<link>` or stylesheet created by script: no `createElement('link')`, no `insertRule`); existing `embed_test.go` cache/ETag tests | `internal/web/theme_test.go` |
| AC17 | `TestThemeInit_DrawingFallbacks_AC17` (keys of `DRAWING_FALLBACKS` equal the set of properties read; each value equals Clear's light block; `themeNumber` checks `Number.isFinite` and the range, no `\|\|` default; no `getPropertyValue(` left in `chart.js`/`gauge.js`/`app.js`); manual M9 | `internal/web/theme_test.go` |

**Manual check** (`manual: the Tester, headless google-chrome on a local build of a scratch copy of the stamped
head with the probe patch`; if the sandbox denies the Tester's Chrome run, the orchestrator runs the Tester's
script, *A denied measurement*). The probe patch, never committed: add `Object.freeze({ id: 'probe', name: 'Probe' })`
to `PIMONITOR_THEMES` and three `:root[data-style="probe"]…` blocks copying Clear's with `--accent: #ff00ff`,
`--spark-fill-alpha: 0` and `--track: notacolor`. Driven over the DevTools protocol from a `node` script in the
scratchpad, fresh profile per case, against `go build ./cmd/pimonitor` run with `packaging/pimonitor.example.yaml`:

- M1 no storage → no `data-style`. M2 `pimonitor-style=probe` (+ `pimonitor-theme=dark`) → both attributes set
  when the parser reaches `<body>` (a `MutationObserver` installed with `Page.addScriptToEvaluateOnNewDocument`
  logs the attributes at the first `body` insertion); `clear` stored → no attribute.
- M3 `Probe`, ` probe`, `gone` → no attribute, stored value unchanged, no console error.
- M4 dialog open: legend Theme, options Clear, Probe in order, the active one checked; again after a reload.
- M5 click Probe: `data-style="probe"`, dialog still `open`, storage `probe`, a sparkline pixel now `#ff00ff`; click
  Clear: attribute gone, storage `clear`.
- M6 unpatched build: `#style-picker` not rendered (`hidden`, no box).
- M7 mode toggle keeps `data-style`; Probe keeps `data-theme`; no stored mode with `Emulation.setEmulatedMedia`
  dark/light flips `--bg` in Probe.
- M8 `localStorage` getter throwing (`addScriptToEvaluateOnNewDocument`): Probe applies, no exception.
- M9 Probe: sparkline fill area shows the card background (alpha `0` honoured); gauge track drawn `#e8eaef`.
- M10 keyboard: Tab reaches the group, ArrowDown selects and applies the next option.
- M11 "Reset to default" with Probe: attribute and storage unchanged, layout default.

The Tester records each case's result in the verification matrix.

## Approach

**Registry and readers (`theme-init.js`, record 0042).** Top-level, before the IIFE:
`PIMONITOR_THEMES` (frozen `{ id, name }` entries, `clear` first, only Clear shipped), `DRAWING_FALLBACKS` (frozen
map: `--accent` `#2563eb`, `--muted` `#5f6876`, `--card-bg` `#ffffff`, `--track` `#e8eaef`, `--ok` `#16a34a`,
`--warn` `#d97706`, `--crit` `#dc2626`, `--spark-fill-alpha` `'0.28'` — Clear's light values), `themeColor(name)`
(trimmed computed value if non-empty and `CSS.supports('color', v)` — when `window.CSS?.supports` is missing, any
non-empty value — else the fallback) and `themeNumber(name, min, max)` (`Number(v)` of a non-empty trimmed value if
finite and within `[min, max]`, else `Number` of the fallback). The IIFE gets a second `try` after the mode's:
read `pimonitor-style`; when it is a registered id (exact `===`) other than `PIMONITOR_THEMES[0].id`, set
`document.documentElement.dataset.style`. Nothing is written or removed.

**Renderers.** `chart.js:6` → `themeColor('--accent')`, `:43` → `themeNumber('--spark-fill-alpha', 0, 1)`, `:44` →
`themeColor('--card-bg')`; `gauge.js:43-44` → `themeColor('--track')`, `themeColor(gaugeColorVar(colorClass))`;
`app.js` `drawOpenHABChart` → `themeColor(colors[line.key])`, `themeColor('--warn')`. No `getPropertyValue` remains
in the three files; line widths stay literal.

**Stylesheet.** One file (record 0038, option 4 lost). Clear stays unscoped. A further theme appends, after Clear's
palette, `:root[data-style="<id>"]`, `:root[data-style="<id>"][data-theme="dark"]` and
`@media (prefers-color-scheme: dark) { :root[data-style="<id>"]:not([data-theme]) }`, each with the full token
set: the dark selectors (0,3,0) beat the theme's light block and Clear's dark blocks (0,2,0) in either order. This
feature adds no such block. New rules for the picker use existing tokens only.

**Markup (`index.html`).** First child of the layout dialog's `.modal-body`, before the hint:
`<fieldset class="style-picker" id="style-picker" hidden><legend>Theme</legend><div class="style-options"
id="style-options"></div></fieldset>`. The mode button's static label becomes `aria-label="Toggle mode"`.

**`app.js`.** `const STYLE_KEY = 'pimonitor-style';` beside `THEME_KEY`; comments mark "theme" names as the mode.
- `activeStyle()`: `document.documentElement.dataset.style` if registered, else `PIMONITOR_THEMES[0]?.id`.
- `repaintCanvases()`: the two repaint lines moved out of `applyTheme`, which calls it.
- `applyStyle(id)`: remove `dataset.style` for the default id, set it otherwise; `repaintCanvases()`.
- `selectStyle(id)`: ignore an unregistered id; `try { localStorage.setItem(STYLE_KEY, id) } catch (e) {
  console.warn('failed to persist theme style', e) }`; `applyStyle(id)`.
- `buildStylePicker()`: `#style-picker.hidden = PIMONITOR_THEMES.length < 2`; empty `#style-options` with
  `innerHTML = ''`, then per entry a `label.style-option` holding `input` (`type = 'radio'`,
  `name = 'style-choice'`, `value = id`, `checked = id === activeStyle()`, `change` → `selectStyle(id)`) and a
  `span` with `textContent = name`. Called at the start of `openLayoutModal`.
- `updateThemeToggle` labels: `'Switch to light mode' : 'Switch to dark mode'`.

Layout: the fieldset as above, legend styled like the dialog's section text, options stacked in registry order, each
label at least 44 px high; spacing free for the Dev, approved in step 9.

Early draft PR: no

## Options considered

- Registry: a copy in `app.js` kept equal by a test (the issue's suggestion), a fetched JSON registry — record 0042.
- Stylesheet: one override file per theme, linked statically (the issue's alternative) — record 0038, option 4.
- Default as explicit `data-style="clear"`; a combined value in `pimonitor-theme`; a server-side default — record
  0041.
- Delivering with Console (#219) in one PR (the issue allows it): lost, Console's design would join a plan whose
  infrastructure is reviewable alone, and the hidden picker lets this merge first.
- Swatches (spec question 4): not built.
- A JS test runner (node) in Go tests: lost, a new tool the CI does not have; the manual check covers runtime
  behavior.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/web | `assets/theme-init.js` | registry, fallbacks, readers, stored theme applied |
| internal/web | `assets/app.js` | `STYLE_KEY`, picker, `selectStyle`/`applyStyle`/`activeStyle`/`repaintCanvases`, readers, mode labels |
| internal/web | `assets/chart.js`, `assets/gauge.js` | readers instead of `getPropertyValue` |
| internal/web | `assets/index.html` | picker fieldset, mode button label |
| internal/web | `assets/style.css` | picker rules (tokens only) |

## Signatures (for the Dev's skeleton)

No Go API. Page-wide script declarations the skeleton adds in `theme-init.js` (bodies as *Approach*):

- `const PIMONITOR_THEMES` — frozen array of frozen entries with an id and a display name, only Clear
- `const DRAWING_FALLBACKS` — frozen map of the eight properties of *Approach*
- `function themeColor(name)` → string
- `function themeNumber(name, min, max)` → number

The functions of `app.js` named in *Approach* (active style, apply, select, build picker, repaint canvases) are
private to its IIFE and have no declaration here. The skeleton holds the markup, the registry and `STYLE_KEY` for
real.

## Test files

- `internal/web/theme_test.go` (new; reuses `readAsset`, `jsFunctionBody`, `requireContains`, `parseCSSRules`,
  `paletteBlocks`).
- `internal/web/icons_test.go`: `checkUpdateThemeToggle` (new labels, AC15) and `checkApplyThemeRedraws` (assert
  `repaintCanvases()` in `applyTheme` and the two repaint calls in `repaintCanvases`) — Tester.

Existing call sites of a changed signature: none (no Go API changes).

Files no unit test can cover: the JS assets, outside the coverage measure already (fact 9); no new exclusion.

## Areas

- `docs/areas/dashboard.md (+380 words)`: as the spec's *Area documents*; a new *Theme* section after *Mode*.

## Documentation updates

Dev, once green:
- `docs/areas/dashboard.md`: *Scope* adds the theme; *Mode* names the button "Switch to dark/light mode"; new
  *Theme* (registered themes, default Clear as absent `data-style`, `pimonitor-style`, unknown/unreadable → Clear,
  kept, applied before first paint, independent of mode, picker shown with two or more themes, immediate apply and
  redraw, one DOM, palette blocks per theme with one key set, link to 0041/0042); *Look* lists exactly the
  properties of `DRAWING_FALLBACKS` and the Clear fallback for an empty, unparseable or out-of-range value;
  *Card layout* adds the Theme section and that reset leaves the theme; *Browser storage* five entries;
  *Related decisions* adds 0041, 0042.
- `docs/ARCHITECTURE.md:26`: "layout, mode and look" → "layout, mode, theme and look".
- `README.md` feature list: after the mode bullet, a bullet on choosable themes (Clear today), remembered in the
  browser and set in the Customize dialog.

## Architecture check

No guarantee of `.squad/project.md` changes: no API, configuration or server state; CSP unchanged (external sync
script, no inline code); `innerHTML` only cleared.

## Security considerations

- [x] No limits (no parsed input beyond a stored id compared for equality).
- [x] None: storage errors are caught and logged with `console.warn`; nothing reaches the page.

## Decision records

- `docs/decisions/0041-theme-choice-per-browser-as-pimonitor-style.md` (Proposed)
- `docs/decisions/0042-theme-registry-in-theme-init-js.md` (Proposed)
- `docs/decisions/0038-dashboard-mode-and-theme-as-token-overrides.md` extended in place (unreleased): option 4,
  one DOM, canvas properties as tokens.

## Out of scope / follow-ups

Console (#219), Berry (#220) and their contrast tests; swatches; a "follow the OS" mode control.
