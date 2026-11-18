# 0023: A zero openHAB alert threshold turns its cutoff off

- **Status:** Accepted
- **Date:** 2026-11-18
- **Area:** Alerts
- **Source:** Issue #184
- **Supersedes:** —

## Context

Issue #184 adds four thresholds for the openHAB alert rules (`thresholds.openhab_rss_warn_percent`,
`_rss_crit_percent`, `_things_offline_warn`, `_things_offline_crit`) and asks for `0` to disable a rule, "consistent
with existing semantics". The existing thresholds have no such rule: a reading is at or above a cutoff when
`value >= cutoff`, so `0` is reached by every reading ([Alerts](../areas/alerts.md#raw-level-of-a-reading)). A Things
count is `0` on a healthy install, so a crit cutoff of `0` would raise `crit` at once; the issue's default of `0` for
the Things crit cutoff only works as "off". The RSS defaults the issue proposes (25 / 40 %) are examples; the
repository's openHAB process sample holds 33 % of a 4 GiB host's RAM, so 25 % would warn permanently on a healthy
install right after an upgrade.

## Options considered

1. **`0` is off for the four new thresholds only; the existing ones keep their rule** — gives the issue's defaults
   their meaning without changing what any existing configuration does; the two rules differ by threshold.
2. **`0` is off for every threshold** — one rule, but an existing configuration with a `0` cutoff (an alert on any
   reading) silently stops alerting after an upgrade.
3. **Keep `>=` everywhere and give the Things crit cutoff a large default instead** — no exception, but there is no
   value that means "never", and an operator cannot turn a single rule off.

## Decision

For the four openHAB thresholds a cutoff of `0` is never reached; warn ≤ crit is checked only when both are non-zero.
The existing thresholds are unchanged (option 1). The RSS defaults are 50 % and 75 %, above the 33 % sample. The
rules are in [Alerts](../areas/alerts.md) and [Configuration](../areas/configuration.md#thresholds).

## Consequences

- With only the warn cutoff off, a rule goes `ok` → `crit` directly and back to `ok` once below crit is confirmed.
- With both off, the rule has no state at all.
- A host where openHAB alone holds half the RAM warns; the general `memory` rule still covers the whole Pi.
- Unifying the rules later would change the meaning of existing configurations and needs a record of its own.
