# 0001: Optional metric sources log their failures once

- **Status:** Proposed
- **Date:** 2026-11-12
- **Area:** Metrics
- **Source:** Issue #197
- **Supersedes:** —

## Context

`AGENTS.md` and `.squad/project.md` (*Guarantees*) promise that a missing hardware source yields a logged warning.
Before issue #197 the collector kept that promise unevenly: a missing thermal zone was logged as a warning on every
fast tick (about 17,000 lines a day at the default 5 s interval, the journal flood issue #176 fixed for `throttled`),
while a missing `vcgencmd`, `/proc/net/wireless`, hwmon or cpufreq source, and a failed GPU or PMIC temperature
call, were not logged at all. Only `throttled` and Pi-hole used the log-once rule. Optional sources are the ones
whose absence is a normal property of a host (a board without a PMIC sensor, a machine without wireless or
`vcgencmd`): their failure is persistent and identical from tick to tick.

## Options considered

1. **Warn on every tick for every optional source** — the thermal zone's old behavior extended. Keeps the
   guarantee literally, but floods the journal on every host that lacks a source, which #176 already rejected.
2. **Warn once at startup and never again** — quiet, but a source that fails later, changes its error or
   recovers is never reported.
3. **The existing log-once rule for every optional source** — a warning on the first failure and whenever the
   error text changes, debug while it repeats, info when the source recovers. One rule, already tested, and every
   change of state stays visible.
4. **For `vcgencmd`: one availability line instead of one per reading** — fewer lines when `vcgencmd` is missing,
   but the collector would need to know the shared runner and route its "not available" error away from the
   three readings, and a failure of one reading (a board without PMIC sensor) still needs its own line.

## Decision

Every optional source — thermal zone, the GPU and PMIC temperature readings, `throttled`, `wireless`, hwmon
sensors, cpufreq and Pi-hole — logs its failures with the log-once rule (option 3), each reading under its own
message, so a missing `vcgencmd` is reported once by each of the three readings that need it. The sources a
running Linux host always has (`/proc/stat`, `/proc/meminfo`, `/proc/mounts`, …) keep logging every failure.
`updates` (apt) and process collection keep logging every failure too, since they are not hardware sources.

## Consequences

- A host that lacks a source logs one warning per source at startup instead of none or one per tick; the
  rules, and which source logs under which message, are in [Metrics](../areas/metrics.md#limits-and-errors).
- A board without a PMIC sensor logs one warning per process start; the reading is latched off and no
  "recovered" line follows.
- A persistent failure is visible after the first line only at debug level; an operator who needs it again
  restarts the service or raises the log level.
- Revisiting this means changing the shared log-once helper or the list of optional sources in the area
  document, not each collector.
