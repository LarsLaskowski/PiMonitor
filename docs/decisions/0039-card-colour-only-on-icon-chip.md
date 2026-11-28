# 0039: The per-card colour appears only on the icon chip; status colours have a fill and an ink variant

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Dashboard
- **Source:** Issue #217
- **Supersedes:** —

## Context

Every card had a 3 px top border in its own colour, so nine to ten strong colours competed with `ok`, `warn` and
`crit`, the colours that carry meaning. The status colours also served as text colours, and in light mode `#16a34a`
and `#d97706` on white reach only about 3.3:1 and 3.2:1, below the 4.5:1 that small text needs.

## Options considered

1. **Card colour only on a tinted icon chip and on the hover/focus highlight; a darker (light mode) or lighter (dark
   mode) `*-ink` token per status level for text, the base token for fills** — the status colours stand out, text
   reaches 4.5:1, fills keep their familiar hue. Cost: two tokens per level to keep consistent.
2. **Drop the per-card colours altogether** — calmest page. Cost: the cards lose the cue that tells them apart at a
   glance, which the issue keeps.
3. **Darken the base status colours for everything** — one token per level. Cost: bar fills and gauge arcs turn
   muddy in light mode, and dark mode needs the opposite shift.

## Decision

Option 1: no coloured card border; the card colour tints the icon chip and the highlight of clickable cards; text in
a status colour uses `--ok-ink`, `--warn-ink` or `--crit-ink`, fills use `--ok`, `--warn` or `--crit`. The rules are
in [Dashboard](../areas/dashboard.md#look).

## Consequences

- Every status rule for text is at least as specific as the rule that gives the element its default colour; until
  this change the openHAB process-memory value and the `Things offline` toggle never showed their documented
  colour because of that.
- A contrast test in `internal/web` computes the ratios from the parsed tokens, so a later palette edit cannot drop
  below 4.5:1 unnoticed.
