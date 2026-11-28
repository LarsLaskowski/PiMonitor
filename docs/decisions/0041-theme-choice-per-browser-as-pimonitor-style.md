# 0041: The theme choice is a per-browser preference, stored as `pimonitor-style` and applied as `data-style`

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Dashboard
- **Source:** Issue #218
- **Supersedes:** —

## Context

v1.5.0 lets the user choose the dashboard's theme (Clear, later Console and Berry) independently of the light/dark
mode ([0038](0038-dashboard-mode-and-theme-as-token-overrides.md)). The mode already occupies the names that would
suit the theme: the `localStorage` key `pimonitor-theme` and the attribute `data-theme` on `<html>`. Renaming them
would reset every stored mode on upgrade. Mode and layout are per-browser preferences without server-side state, and
the page applies the stored mode before the first paint from `theme-init.js` under `script-src 'self'`.

## Options considered

1. **Per browser, a new key `pimonitor-style` and attribute `data-style`; the default theme is the absence of the
   attribute** — mode and theme stay two independent values, and the page without script, with unreadable storage or
   with an unknown stored value is Clear by the unscoped palette. Cost: the code says "style" where the user reads
   "theme".
2. **One combined value in `pimonitor-theme`** (`clear-dark`, …) — one key. Cost: changes the key's format, so a
   stored mode is lost or needs a migration, and an older version reading the new value forgets the mode.
3. **A server-side default theme (`config.yaml` option, an API field)** — one look for every browser. Cost: a new
   configuration and API surface, and per-user choice still needs browser storage; the issue leaves it out of scope.
4. **Default as an explicit `data-style="clear"`** — every theme scoped the same way. Cost: two representations of
   Clear (absent before the script runs, explicit after), and Clear's palette, unscoped since #217, would move.

## Decision

Option 1. The theme id is stored per browser under `pimonitor-style`; a registered non-default id is applied as
`data-style="<id>"` on `<html>`, and Clear is the absence of the attribute. The rules (fallback, storage entries)
are in [Dashboard](../areas/dashboard.md#theme).

## Consequences

- An unknown stored value (downgrade, removed theme) means Clear and is kept, so an upgrade back restores the choice.
- Code names the theme "style" and the mode "theme" (`applyTheme`, `THEME_KEY`); comments say which is which.
- A server-side default would need its own decision and a new configuration option.
