# Spec: Let the user choose a dashboard theme, stored per browser

Source: Issue #218
Status: Draft

## Problem

v1.5.0 ships three visual designs of the dashboard (Clear from #217, Console and Berry in their own issues), and
each user should be able to pick the one they prefer, with Clear as the default. Today the dashboard has one look
and only the light/dark *mode* switch; there is no place to register a second design, no storage for the choice and
no control to make it. The terms *mode* (light/dark: `pimonitor-theme`, `data-theme`) and *theme* (the design) are
those of the issue and of [decision 0038](../../docs/decisions/0038-dashboard-mode-and-theme-as-token-overrides.md).
The registry (ids `clear`, `console`, `berry`, display names Clear, Console, Berry, default first) is the issue's
*Terminology* table; this feature ships with `clear` alone registered, and the two others plug into it.

## Scenarios

- S1: Given a browser that never chose a theme, when the user opens the dashboard, then it shows the theme Clear in
  the mode of the operating system (or the stored mode), exactly as before this feature.
- S2: Given at least two registered themes, when the user opens "Customize dashboard", then the dialog starts with a
  section headed "Theme" — before the card hint "Show, hide, and reorder cards. Saved in this browser only." and the
  card list — holding one radio option per registered theme, labelled with its display name, in registry order,
  with the active theme checked.
- S3: Given the dialog open, when the user selects another theme, then the page takes that theme at once, without a
  reload; the dialog stays open with focus on the selected option; the sparklines, load gauges and openHAB memory
  chart are redrawn in the new theme; and the choice is stored in this browser.
- S4: Given a stored theme, when the user reloads the page, then the stored theme and the stored mode are both in
  effect from the first paint, the page never shows Clear first, and the dialog shows the stored theme checked.
- S5: Given any theme, when the user presses the header mode button, then the mode switches and the theme stays;
  given any mode, choosing a theme keeps the mode; with no stored mode the page follows the operating system's
  light/dark setting live in every theme.
- S6: Given a chosen theme, when the user presses "Reset to default" in the dialog, then the card layout returns to
  its default and the theme stays as chosen.
- S7: Given only one registered theme (the state this feature ships in), when the user opens "Customize dashboard",
  then there is no "Theme" section and the dialog looks and works as today.
- S8: Given a keyboard user in the dialog, when they Tab into the Theme group and press the arrow keys, then focus
  and selection move between the options and each selection applies as in S3; a screen reader announces a radio
  group named "Theme".
- S9: Given any theme, the header mode button's accessible name reads "Switch to dark mode" in light mode and
  "Switch to light mode" in dark mode, so it is not confused with the Theme group (today it says "… theme").

## Errors and edge cases

- E1: A stored value that is not a registered id — a theme removed later, a value written by a newer version before
  a downgrade, or a case or whitespace variant such as `Console` or ` clear` (only an exact id counts) — shows Clear
  with no message and no console error; the stored value stays untouched until the user chooses a theme. The dialog
  then shows Clear checked.
- E2: Storage that cannot be read (private browsing, blocked storage) means Clear on every load; no message.
- E3: Storage that cannot be written: choosing a theme still applies it for the current page, nothing is stored, no
  message is shown and nothing throws; after a reload the page shows Clear (or what was stored before).
- E4: With JavaScript disabled or `theme-init.js` failing, the page shows Clear.
- E5: Selecting the theme that is already active changes nothing visible.
- E6: Only one theme registered and a stored id of another theme (E1 applies): Clear, and no Theme section (S7).
- E7: A canvas drawing property that a theme does not declare, or declares with a value the renderer cannot use,
  draws as in Clear.

## Non-functional requirements

- N1: The page keeps running under `style-src 'self'; script-src 'self'`: no inline style or script, `theme-init.js`
  stays an external synchronous script in `<head>`; `app.js` assigns no non-empty `innerHTML`.
- N2: No API, configuration or server-side change: no `config.yaml` option, no API field, no server state. The
  choice is per browser like mode and layout. No external resources, web fonts or build step.
- N3: Compatibility: `pimonitor-api-key`, `pimonitor-theme`, `pimonitor-layout` and `pimonitor-openhab-things` keep
  their keys, formats and behavior; an upgraded browser with no `pimonitor-style` sees Clear (S1). An older version
  ignores `pimonitor-style`.
- N4: Storage: the choice is the theme id under `localStorage` key `pimonitor-style`; the dashboard then stores
  exactly five entries (the four of today and `pimonitor-style`).
- N5: Extensibility, as the issue's *Specification* §1–§2 requires: one registry of themes is the single source for
  the selector and for validating a stored id; adding a theme is a registry entry plus its style rules, with no
  change to the selection logic; one DOM for all themes (no per-theme markup in `index.html`, no per-theme HTML
  rendering branches in `app.js`); canvas widgets take theme differences only from declared custom properties
  (E7). Each theme defines the same token set in its light block and in both dark blocks.
- N6: Every theme's stylesheet rules are loaded statically with the page, never on demand; any new asset file is
  served like the others (`Cache-Control: no-cache`, per-file `ETag`) and covered by the asset tests.
- N7: Dialog behavior (modal, focus in and back, Escape, outside click, close button) is unchanged.

## Acceptance criteria

- AC1: With no stored theme the dashboard shows Clear, and `<html>` carries no `data-style` attribute (S1, E4).
- AC2: A stored, registered, non-default theme id is applied by `theme-init.js` as `data-style="<id>"` on `<html>`
  before the first paint, next to the stored mode; a stored `clear` leaves the attribute absent (S4).
- AC3: A stored value that is not exactly a registered id results in Clear, without an error and without the stored
  value being changed or removed (E1, E2, E6).
- AC4: With more than one registered theme, "Customize dashboard" shows a "Theme" section before the card hint and
  list, with one radio option per registered theme labelled with its display name in registry order, and the
  option of the active theme is checked whenever the dialog opens (S2, S4).
- AC5: Choosing an option applies the theme immediately without a reload, keeps the dialog open, stores the id under
  `pimonitor-style` (choosing Clear stores `clear` and removes the attribute), and redraws the sparklines, load
  gauges and openHAB memory chart in the new theme; a detail chart opened afterwards is drawn in it (S3, E5).
- AC6: With exactly one registered theme the "Theme" section is not rendered (S7).
- AC7: Theme and mode are independent: switching the mode keeps the theme, choosing a theme keeps the mode, and with
  no stored mode the page follows `prefers-color-scheme` live in every theme (S5).
- AC8: When `localStorage` cannot be read or written, choosing a theme works for the current page, nothing is stored
  and nothing throws (E2, E3).
- AC9: The id list `theme-init.js` validates against and the registry the selector is built from hold the same ids
  in the same order (N5).
- AC10: Every registered theme defines the same set of palette tokens in its light block and in both of its dark
  blocks, and a canvas property it leaves undeclared draws as in Clear (N5, E7).
- AC11: The selector is a `<fieldset>` with a `<legend>` "Theme" and native radio inputs sharing one name, reachable
  and operable by keyboard alone, and the dialog's modal, focus, Escape, outside-click and close behavior is
  unchanged (S8, N7).
- AC12: The dashboard stores no `localStorage` entry other than `pimonitor-api-key`, `pimonitor-theme`,
  `pimonitor-layout`, `pimonitor-openhab-things` and `pimonitor-style` (N3, N4).
- AC13: "Reset to default" restores the default card layout and leaves the chosen theme and `pimonitor-style` as
  they are (S6).
- AC14: `index.html` still has no inline style or script, `theme-init.js` is still an external synchronous script in
  `<head>`, `app.js` still assigns no non-empty `innerHTML`, and the page loads nothing from another origin and reads
  no API field or configuration option for the theme (N1, N2).
- AC15: The mode button's accessible name is "Switch to dark mode" in light mode and "Switch to light mode" in dark
  mode, in every theme (S9).
- AC16: Every theme's style rules are loaded by the page's static `<link>` elements before the first paint, never on
  demand, and every asset file the feature adds is served with `Cache-Control: no-cache` and its own `ETag` (N6).

## Out of scope

- The designs of Console and Berry (their own issues), and a contrast test over their palettes.
- Swatches or previews in the selector (question 4).
- A server-side default theme or a theme option in `config.yaml`.
- A control to return the mode to following the operating system.
- Syncing the choice across open tabs.
- User-defined themes or colour pickers.

## Open questions

1. Should "Reset to default" also reset the theme? Answered: no (the issue's default). The button sits under the
   card list and its hint speaks of cards only; AC13.
2. Default theme as `data-style="clear"` or as the absence of the attribute? Answered: absence. Clear's tokens are
   today's unscoped palette blocks of `style.css` (#217), so the page without JavaScript (E4), with unreadable
   storage or with an unknown value is already Clear, and Clear has one representation instead of two; other themes
   override under their `data-style` value. AC1, AC2, AC5.
3. One stylesheet or one file per theme? Answered: the plan's decision, as the issue says, within N6 and decision
   0038, which rejected per-design copies of the layout rules (a theme is overrides only).
4. Should the selector show swatches? Answered: no, not in this feature. A choice applies at once with the dialog
   open (S3), so the user sees the theme itself rather than a swatch, and a swatch would need each theme's tokens
   resolved outside its own scope, which only pays once more than one theme exists. It can become its own issue.

Corrections to the issue, checked against the code at `d05bb19`: the dashboard already stores four entries today
(`pimonitor-openhab-things` is the fourth, `docs/areas/dashboard.md`, *Browser storage*), so it stores five after
this feature (AC12). The detail chart and the layout dialog are both modal, so no detail chart can be open while a
theme is chosen; AC5 requires the next one opened to be in the new theme, and adds the openHAB memory chart, which
the mode switch also redraws. The mode button's accessible name still says "theme" (`app.js`,
`updateThemeToggle`), which S9 and AC15 change.

## Area documents

- `docs/areas/dashboard.md`: *Scope* names the theme among the per-browser preferences. *Mode* stays as is (the mode
  button's names in "mode"); a new *Theme* section states the registered themes and their order, the default Clear
  as the absence of `data-style`, storage under `pimonitor-style`, the fallback for an unknown or unreadable value
  (kept, not removed), application before the first paint, independence from the mode, immediate application with
  redrawn canvases, and that a theme is token overrides plus declared canvas properties over one DOM. *Look* names
  the canvas custom properties after #217 and the Clear fallback. *Card layout* says the dialog also holds the Theme
  section (shown only with more than one theme) and that "Reset to default" leaves the theme. *Browser storage*
  lists five entries.
- `README.md`: the feature list mentions selectable themes.
