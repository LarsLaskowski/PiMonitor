# 0040: Dashboard icons are inline stroke SVG in the page, not emoji

- **Status:** Accepted
- **Date:** 2026-11-28
- **Area:** Dashboard
- **Source:** Issue #217
- **Supersedes:** —

## Context

The dashboard used emoji as icons. They render differently on every operating system and browser, cannot take a
palette colour and clash with the charts. The page must stay self-contained (`default-src 'self'`, `img-src 'self'
data:`, no inline style), embedded in the binary, small enough for a Pi Zero, and `app.js` must not build markup with
`innerHTML`.

## Options considered

1. **Inline `<svg>` at each use in `index.html`, drawn with `currentColor`; icons the script adds (the network
   arrows) cloned from a `<template>` in the page** — colours follow the tokens, no request, no markup string in the
   script. Cost: a repeated icon (the chevron) is repeated in the page, a few hundred bytes.
2. **An SVG sprite of `<symbol>`s referenced by `<use href="#…">`** — each icon once. Cost: an indirection for about
   15 small icons, and tests must resolve references to check what a heading shows.
3. **Icon images or an icon font as extra embedded assets** — Cost: extra requests and files; an image cannot take
   `currentColor`, a font is far larger than the icons.
4. **Keep emoji** — Cost: the problem of the issue.

## Decision

Option 1. The favicon stays a `data:` URI, now an SVG drawing in a fixed accent colour, since a favicon cannot read
the page's tokens. The rules (sizes, accessible names) are in [Dashboard](../areas/dashboard.md#look).

## Consequences

- A test scans `index.html` and `app.js` for emoji, escapes included, against a table of Unicode's
  `Extended_Pictographic` ranges generated once with Node's ICU (the generator line stands in the test).
- The mode icon is chosen by CSS from the same selectors as the palette, so it is right before the first paint.
