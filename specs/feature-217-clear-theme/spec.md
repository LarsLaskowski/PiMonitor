# Spec: Refresh the default look: theme Clear (light and dark)

Source: Issue #217
Status: Draft

The issue text (sections 1–13, *Constraints*) is the binding design specification: token values, card colours, icon
paths and sizes are taken from it and not restated here. The mockups in `docs/design/v1.5.0/` only illustrate it.
Terms as the issue defines them: **mode** is light or dark (`pimonitor-theme`, `data-theme`, the header button),
**theme** is the visual design; after this change there is one theme, Clear.

## Problem

People viewing the dashboard, on a desktop or a phone, see emoji icons that render differently on every system and
ignore the palette, ten coloured top borders competing with the status colours that carry meaning, status text in
light mode below 4.5:1 contrast (`#16a34a` on white 3.3:1, `#d97706` 3.2:1), 36 px header buttons below the 44 px
touch target, and flat charts with a bar percentage no more prominent than the byte figures beside it.

## Scenarios

- S1: Given the page in light mode, when it loads, then header, banner, cards, dialogs and footer show the Clear
  light palette (issue §1), with the same cards, order, texts and data as before.
- S2: Given the operating system in dark mode and no stored choice, when the page loads, then it shows the Clear dark
  palette; when the operating system switches, the page follows live.
- S3: Given any mode, then every card heading shows a 28 px chip with a stroke icon in the card's colour (§2, §3) and
  no card has a coloured top border; the header shows the 44 px logo tile, the title and the subtitle.
- S4: Given light mode, when the user presses the mode button (moon icon), then the page switches to dark, the button
  shows the sun icon, its `aria-label` reads `Switch to light theme` and `aria-pressed` is `true`, the choice is stored,
  and sparklines and gauges are redrawn in the dark palette without a reload; pressing again reverses all of it.
- S5: Given a value in `warn` or `crit` (CPU, temperature, a bar), then its text uses the level's ink colour and its
  bar fill or gauge arc the level's base colour.
- S6: Given Memory & Swap and Filesystems, then each bar row shows the name on the left and, on the right, the
  percentage emphasised (primary text colour, or the ink colour at `warn`/`crit`) followed by the muted byte figures,
  e.g. `41.2 %` `· 1.6 GB / 3.8 GB`.
- S7: Given a sparkline with at least two points, then it shows a 2 px accent line, a gradient fill fading to
  transparent and a ringed dot on the newest point.
- S8: Given the Load Average card, then each of the three gauges shows its value inside the arc and its label
  (`1 min`, `5 min`, `15 min`) below.
- S9: Given a clickable card (CPU Usage, Load Average, Temperature, Memory & Swap), then its heading button ends with
  a chevron and opens the detail view as today; a screen reader announces the same name as today, including
  `— alert: warn` / `— alert: crit`.
- S10: Given an active alert, then the banner shows the alert icon and the same text as today, in the worst level's
  ink colour on a tinted background.
- S11: Given the Network card, then each interface row shows the name and, on the right, a download and an upload rate,
  each after an arrow icon; a screen reader announces them as "Download …" and "Upload …".
- S12: Given a phone-width viewport, then the header wraps, and every header button, clickable heading, the updates
  button and every dialog button is a touch target of at least 44 px.

## Errors and edge cases

- E1: Storage unavailable or no stored choice: the mode follows `prefers-color-scheme` as today; the mode button
  still shows the icon of the mode it switches to.
- E2: A stored mode is applied before the first paint (`theme-init.js` unchanged): no flash of the other palette or of
  the other mode icon.
- E3: A sparkline with fewer than two points draws nothing (no line, fill or dot); a flat series is padded as today.
- E4: A load gauge in a narrow single column (phone) or a wide three-column layout is never clipped; the value inside
  the arc does not overlap it.
- E5: A long mountpoint or interface name is cut with an ellipsis as today; the percentage and byte figures stay
  visible.
- E6: The openHAB card (shown only when openHAB is detected; not in the issue's tables or mockups) gets the same
  treatment: chip, stroke icon, no top border; its warning state stays a warning-coloured border without a top border.
  Its icon is a house, its chip colour `#0369a1` light / `#38bdf8` dark (Q1).
- E7: Text on an `--accent` fill (the active range button, `Unlock`) uses `--on-accent`: the hard-coded white of today
  reaches only 2.5:1 on the dark accent.
- E8: Status text not named in the issue's list follows its rule too: the new version in the updates list, the API key
  error, the openHAB state line and `Things offline` toggle.
- E9: Without JavaScript the page shows the static placeholders as today, now with SVG icons.
- E10: Emoji in character-reference form (today's layout button is `&#9881;&#65039;`) count as emoji.

## Non-functional requirements

- N1: Content-Security-Policy unchanged (`style-src 'self'; script-src 'self'`): no `style` attribute, `<style>`
  element or inline script in `index.html`; dynamic sizes only through the CSSOM.
- N2: No external resource, web font or build step; the system font stack stays. Added bytes stay small (Pi Zero):
  icons are inline SVG paths, not images.
- N3: `app.js` never assigns non-empty content to `innerHTML` (security area *Dashboard XSS*, `xss_test.go`); server
  text only through `textContent`.
- N4: Contrast: text at least 4.5:1 against its background in both modes (3:1 at 24 px and above and for icons, bar
  fills and gauge arcs against `--card-bg`). Status is never shown by colour alone where it is not already accompanied
  by text.
- N5: Every colour, radius, shadow, card padding and gap is a custom property, so a later theme is a set of overrides.
- N6: No API, configuration, polling, history, alert, layout-format or storage change; card ids, default order, the
  multi-column breakpoints (3 / 2 at ≤ 1100 px / 1 at ≤ 720 px) and the element ids scripts and tests use stay.
- N7: Supported browsers are those with `color-mix()` (already used by `style.css`).

## Acceptance criteria

- AC1: `index.html` and `app.js` contain no emoji — no Unicode `Extended_Pictographic` code point and no U+FE0F,
  literal or as a character reference (`↑`/`↓` on the layout dialog's move buttons are neither and stay); every card
  heading, the header title, both header buttons and the alert banner show an inline SVG icon that is `aria-hidden`
  or has an accessible name; the favicon is an SVG drawing in a `data:` URI (S3, S10, E9, E10).
- AC2: The palette tokens of issue §1 exist with the listed values in the light block and in both dark blocks, and the
  two dark blocks hold the same set of tokens with the same values; `--gauge-track` is gone; outside these blocks and
  the per-card colour rules `style.css` holds no colour literal, and radii, shadows, card padding and gaps are custom
  properties (S1, S2, N5).
- AC3: Text in a status colour uses the `*-ink` token of its level — values, bar percentages, banner, badges, stale
  badges, Pi-hole status and the cases of E8; bar fills and gauge arcs use the base token; text on an `--accent` fill
  uses `--on-accent` (S5, S10, E7, E8).
- AC4: Cards have no coloured top border; each card's icon chip shows its card colour of issue §2 (the openHAB card:
  `#0369a1` / `#38bdf8`, Q1) in the matching mode at 12 % (light) / 16 % (dark) chip tint (S3, E6).
- AC5: The header buttons, the heading buttons of clickable cards, the updates button and the dialog buttons (close,
  range, move, reset, unlock) are at least 44 px high, and 44 px wide where icon-only (S12).
- AC6: In the bar label rows of Memory & Swap and Filesystems the percentage and the byte figures are separate
  elements, both filled with `textContent`; the percentage takes the `warn` / `crit` ink colour at or above the
  matching threshold and `--text` otherwise; the byte figures are `--muted` (S6, E5).
- AC7: A sparkline with at least two points is drawn with a 2 px accent line, a gradient fill that fades to
  transparent and a dot at the newest point; with fewer than two points nothing is drawn (S7, E3).
- AC8: Each load gauge shows its value inside the arc and its label below; the arc is not clipped at any column width
  from one-column phone width to three-column desktop width (S8, E4).
- AC9: Clickable cards show a chevron in their heading; the accessible name of the heading button is unchanged,
  including the alert-level suffix (S9).
- AC10: Switching the mode with the header button swaps the icon (moon in light mode, sun in dark mode), keeps
  `aria-label` / `aria-pressed` as today and redraws charts and gauges in the new palette, without a reload (S4, E1,
  E2).
- AC11: `index.html` contains no `style` attribute and no `<style>` element, and the page works under the unchanged
  Content-Security-Policy; no asset references another origin (N1, N2).
- AC12: The contrast ratio of `--text`, `--muted` and the three `*-ink` tokens against `--card-bg`, of `--text` and
  `--muted` against `--bg`, of `--on-accent` against `--accent`, and of the updates button's `--accent` label against
  its tint (`--accent` at 8 % light / 14 % dark over `--card-bg`, Q2) is at least 4.5:1 in both modes (N4, E7).
- AC13: Behaviour described in `docs/areas/dashboard.md` outside its *Theme* section (polling, API key, failures,
  rendering rules, alerts, detail view, card layout, browser storage) is unchanged, and the existing tests under
  `internal/web` pass, adapted only where they assert markup this change deliberately changes (S1, S11, N3, N6, N7).

## Out of scope

- A theme selection and the themes Console and Berry (later issues of the v1.5.0 milestone).
- Per-core meters (#221), the sparkline caption with min/max (#222) and the header status dot (#223), all shown in the
  mockups; the per-core line stays text.
- Any change to polling, history, alerts, layout customisation, API key handling, the API or configuration; the text
  of every label, including the mode button's `aria-label` (`Switch to … theme`).
- The openHAB card's rows keep their single value element (issue §7 names only Memory & Swap and Filesystems).

## Open questions

- Q1: Which icon and chip colour does the openHAB card get? The issue says "nine cards" and its §2 and §3 tables list
  nine, but the page has ten (`card-openhab`, emoji `🏠`, colour `#0ea5e9`, which reaches only 2.5:1 as a chip icon
  on white). AC1 removes its emoji, so it needs both. Answered: by the Product Manager — a house icon
  `<path d="M3 11l9-7 9 7"/><path d="M5 10v10h14V10"/><path d="M10 20v-5h4v5"/>` with chip colour `#0369a1` (light)
  / `#38bdf8` (dark), 4.98:1 and 5.78:1 on their tinted chips.
- Q2: The updates button's label (`--accent` on `--accent` at 10 % over `--card-bg`) reaches 4.49:1 in light mode,
  below the issue's own 4.5:1 constraint (dark: 5.17:1). Answered: by the Product Manager — the light background tint
  is 8 % (4.65:1); dark stays 14 %. All other values of issue §10 stay.
- Q3: Does an existing test protect the gauge arc from clipping, as §9 says (`gauge_test.go` "keeps holding")?
  Answered: no (code) — `gauge_test.go` holds only `TestAppJS_LoadGaugeUsesCPUThresholds`; the radius logic is in
  `gauge.js` untested, so AC8 needs its own test.
- Q4: Do the percentage texts change? Answered: no (Lead, AC13) — Memory & Swap keep `41.2 %`, Filesystems `34.2%` as
  today; only the split into two elements is new, the `· ` separator going with the byte figures.
- Q5: Do the mode button's labels change with the new terminology? Answered: no (issue §3: "behave as today").

## Area documents

- `docs/areas/dashboard.md`: *Theme* becomes *Mode* (light/dark, storage, live following, first paint, redraw — as
  today) plus a short *Look* paragraph stating the behaviour the look carries: status text in the ink colour of its
  level and fills in the base colour, the emphasised bar percentage with its threshold colouring, the gauge value
  inside the arc, the chevron on clickable cards, the per-card colour on the icon chip only, the accessible names of
  the network rates, icons as inline SVG and no emoji, and the 44 px touch targets. *Scope* says "mode" for "theme".
  *Related decisions* links the new record (terminology mode vs theme, per-card colour on the chip only).
