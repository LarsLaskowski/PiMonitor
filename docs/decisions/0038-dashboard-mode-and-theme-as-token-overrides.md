# 0038: Dashboard "mode" is light or dark, a "theme" is a set of custom-property overrides

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Dashboard
- **Source:** Issue #217
- **Supersedes:** —

## Context

Until v1.4 the dashboard called its light/dark palette switch the "theme" (`pimonitor-theme`, `data-theme`, the
header button). Issue #217 introduces the visual design Clear, and later issues of the v1.5.0 milestone add the
designs Console and Berry and a selection between them, each in a light and a dark variant. Two concepts now need two
names, and each design has to be swappable without touching the markup or the scripts. The page runs under
`style-src 'self'`, so no style can be injected at run time.

## Options considered

1. **"Mode" for light/dark, "theme" for the design; every colour, radius, shadow, padding and gap a custom property
   on `:root`, defined in a light block and two identical dark blocks** — a theme becomes a set of overrides of
   these properties; canvas widgets read the same properties when they draw. Cost: more tokens, and the two dark
   blocks must be kept identical (guarded by a test).
2. **Keep "theme" for light/dark and call the designs "skins"** — no rename in the documentation. Cost: the stored
   key and the attribute keep a name that no longer matches the user-facing concept of the milestone's issues.
3. **One stylesheet per design** — each design free in its rules. Cost: three copies of every layout rule to keep in
   step, more embedded bytes on a Pi Zero, and a design switch that reloads a stylesheet.

## Decision

Option 1. Light/dark is the *mode*; its storage key `pimonitor-theme` and attribute `data-theme` keep their names,
since renaming them would reset every stored choice. The look is described in
[Dashboard](../areas/dashboard.md#look); the per-card colours are tokens of the palette blocks like every other
colour.

## Consequences

- A later theme only adds override blocks; `style.css` holds no colour literal outside the palette blocks.
- The stored key keeps the old word: code and documentation say "mode" and name the key as it is.
