# 0011: Incomplete /proc/meminfo content is a failed memory reading

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** Metrics
- **Source:** Issue #249
- **Supersedes:** —

## Context

`parseMeminfo` (`internal/collector/memory.go`) failed only when no `key: number` line parsed at all. Content
without `MemTotal` was a successful read, so memory read 0 % used, and content without `SwapTotal`/`SwapFree`
read as a host without swap; content without both `MemAvailable` and `MemFree` read 100 % used. Since
[0003](0003-failed-reading-leaves-history-gap.md) and [0010](0010-missing-reading-left-out-of-prometheus.md) a
failed read keeps the value out of the history, the alert engine and Prometheus, but these readings counted as
real ones, which `.squad/project.md` (*Guarantees*: no fabricated reading) rules out. The kernel writes
`MemTotal`, `MemFree`, `SwapTotal` and `SwapFree` unconditionally (`fs/proc/meminfo.c`, `meminfo_proc_show`);
`MemAvailable` exists only since Linux 3.14, which the reader already falls back from. None of this content is
reachable from a real kernel, so the decision is about which defensive rule to keep, not about a reported failure.

## Options considered

1. **The fields every kernel writes are required, and `MemTotal` must be above 0** — a missing one, or a zero
   total, fails the read; `MemAvailable` stays optional. One fixed list, taken from the kernel source, and every
   figure the reader computes then rests on a delivered value.
2. **Only `MemTotal` required** — the issue's narrowest reading; a missing `SwapTotal` would still read as a host
   without swap, and a missing `MemFree` (without `MemAvailable`) as 100 % used.
3. **`MemTotal` plus "`MemAvailable` or `MemFree`" plus the swap pair** — accepts exactly what the computation
   needs, but ties the rule to the formula instead of to what the kernel writes, and accepts content no kernel
   produces.
4. **A separate swap validity**, so missing swap fields invalidate only swap — memory and swap come from one read,
   and [0010](0010-missing-reading-left-out-of-prometheus.md) (option 6) already keeps one validity for both.
5. **Leave it and document it** — the path is unreachable on a real kernel, but it keeps a fabricated reading in
   the code that the guarantee rules out, for the price of a short check.

## Decision

`parseMeminfo` fails when `MemTotal`, `MemFree`, `SwapTotal` or `SwapFree` is missing, or `MemTotal` is 0, so such
content is a failed memory reading like an unreadable file (option 1). The rules are in
[Metrics](../areas/metrics.md#memory-and-swap).

## Consequences

- Such a tick is treated as every failed `/proc/meminfo` read: a logged warning, 0 in the v1 snapshot, no history
  point, no alert evaluation and no Prometheus memory and swap families.
- A kernel that dropped one of the four fields would stop PiMonitor's memory readings altogether, with the field
  named in the logged warning; relaxing the list then means changing this record.
- The rule covers missing fields and a zero total only. Content with all four fields whose `MemAvailable` (or the
  `MemFree` fallback) exceeds `MemTotal` still reads as a negative used percent; whether such content fails the
  read or is clamped is left to a follow-up issue, since no kernel writes it and the issue did not report it.
