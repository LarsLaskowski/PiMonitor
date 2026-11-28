# 0042: The theme registry and the canvas token readers live once in `theme-init.js`

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Dashboard
- **Source:** Issue #218
- **Supersedes:** —

## Context

Two scripts need the list of known themes: `theme-init.js`, which validates the stored id before the first paint,
and `app.js`, which builds the selector after the page loaded. The issue allows a second copy of the list in `app.js`
if a test keeps the two identical, as the fallback threshold keys are kept in step with the server. Three scripts
(`chart.js`, `gauge.js`, `app.js`) also read custom properties for canvas drawing, each with its own inline fallback
literal today, one of which turns a valid `0` into `0.28` and one of which has no fallback at all.

## Options considered

1. **One registry as a global constant in `theme-init.js`, read by `app.js`; the token readers and Clear's fallback
   values as one map and two functions in the same file** — `theme-init.js` loads first, synchronously in `<head>`,
   so every later script sees them; nothing is duplicated, and a test only checks that `app.js` keeps no list and
   that the fallback map equals Clear's light block. Cost: two more page-wide names, and `theme-init.js` is more than
   a pre-paint snippet.
2. **A copy of the id list in `app.js`, kept equal by a Go test** — the issue's suggestion. Cost: two lists to edit
   per theme, and a test that only exists to keep them in step.
3. **A JSON registry asset fetched by both scripts** — one file. Cost: a fetch cannot run before the first paint
   without blocking, so `theme-init.js` would still need its own copy.
4. **Readers and fallbacks in `chart.js`** — loaded before `gauge.js` and `app.js`. Cost: `chart.js` is a drawing
   library; the fallbacks belong next to the theme list they describe.

## Decision

Option 1. `theme-init.js` declares the registry `PIMONITOR_THEMES` (id and display name, default first), the map
`DRAWING_FALLBACKS` of every custom property a canvas renderer reads with Clear's light value, and the readers
`themeColor` and `themeNumber`; `app.js`, `chart.js` and `gauge.js` use only these. The rules are in
[Dashboard](../areas/dashboard.md#theme).

## Consequences

- Adding a theme is one registry entry plus its palette blocks; a new drawing property is one map entry plus a
  token in every palette block (guarded by the token key set test).
- `app.js`, `chart.js` and `gauge.js` depend on `theme-init.js` having run, as `app.js` already depends on the
  drawing functions of `chart.js` and `gauge.js`.
