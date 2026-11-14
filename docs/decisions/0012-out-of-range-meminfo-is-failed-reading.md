# 0012: /proc/meminfo content with a free value above its total is a failed memory reading

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** Metrics
- **Source:** Issue #252
- **Supersedes:** —

## Context

Since [0011](0011-incomplete-meminfo-is-failed-reading.md) `/proc/meminfo` content with `MemTotal`, `MemFree`,
`SwapTotal` and `SwapFree`, and `MemTotal` above 0, is a reading. `memAndSwapFromFields`
(`internal/collector/memory.go`) computes the used percent as 100 × (1 − available / `MemTotal`), so an available
value (`MemAvailable`, or the `MemFree` fallback) above `MemTotal` gave a negative percent, and `SwapFree` above
`SwapTotal` a swap use of 0 — values that reached the v1 snapshot, the history, the alert engine and Prometheus,
which `.squad/project.md` (*Guarantees*: no fabricated reading) rules out.

In the kernel (`fs/proc/meminfo.c`, `mm/show_mem.c`, `mm/swapfile.c`) `MemFree` counts free pages, which are part
of `MemTotal`'s managed pages; `si_mem_available` sums free pages, part of the file cache and of the reclaimable
kernel memory, clamps the sum at 0 but not at `totalram`, so the source bounds it only through those page sets
being disjoint parts of the total; `SwapFree` is `nr_swap_pages` within `total_swap_pages`, both raised by the same
amount during a swapoff. No kernel is known to write a free value above its total; on a real host the gap is
hundreds of MB, far beyond the drift of per-CPU counters.

## Options considered

1. **Each free value above its total fails the read** — `MemFree` or `MemAvailable` above `MemTotal`, `SwapFree`
   above `SwapTotal`. A rule taken from what the kernel's counters mean, like 0011's required fields, and it
   covers the `MemFree` fallback without repeating its selection.
2. **Clamp the used percent to 0–100** — keeps a reading but invents its value: 0 % used next to an
   `available_bytes` above `total_bytes`, the fabricated reading the guarantee excludes.
3. **Clamp the available value to the total, and the percent with it** — the same invented 0 % with a consistent
   snapshot; the delivered value is replaced, not reported.
4. **Compare only the value the computation uses** (`MemAvailable`, or `MemFree` when it is missing or 0) — ties
   the rule to the formula, which 0011 rejected (its option 3), needs the fallback selection in two places, and
   accepts a `MemFree` above `MemTotal` next to a valid `MemAvailable`, content no kernel writes.
5. **Fail in `memAndSwapFromFields` instead of `parseMeminfo`** — changes its signature and its test call sites and
   splits the validity rules of one read over two functions; 0011 put them in `parseMeminfo`.
6. **Memory only, swap left as it is** — the issue's narrowest reading; `SwapFree` above `SwapTotal` would still
   read as 0 % swap used, the same fabricated reading through the same check.
7. **Leave it and document it** — unreachable on a real kernel, but it keeps a negative or invented reading for
   the price of three comparisons.

## Decision

`parseMeminfo` fails when `MemFree` or `MemAvailable` exceeds `MemTotal`, or `SwapFree` exceeds `SwapTotal`, so such
content is a failed memory reading like incomplete content (option 1); a free value equal to its total is a
reading. The rules and error texts are in [Metrics](../areas/metrics.md#memory-and-swap).

## Consequences

- Such a tick is treated as every failed `/proc/meminfo` read ([0011](0011-incomplete-meminfo-is-failed-reading.md)):
  a logged warning on every tick, 0 in the v1 snapshot, no history point, no alert evaluation and no Prometheus
  memory and swap families.
- With the read failing on this content, the used percent of a successful read lies in 0–100 and swap's "never
  below 0" guard in `memAndSwapFromFields` is no longer reachable from `Collect`.
- A kernel or a `/proc/meminfo` virtualization (a container tool) that wrote a free value above its total would
  stop PiMonitor's memory readings, with the offending field named in the logged warning; tolerating it then means
  changing this record.
