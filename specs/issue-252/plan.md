# Plan: Out-of-range /proc/meminfo content gives a negative memory used percent

Source: Issue #252
Tier: standard — a behavior change (content that read as a negative or invented memory figure becomes a failed read) that creates, changes or removes no security control.
Security areas touched: Parsing of local input (`/proc/meminfo` in `internal/collector/memory.go`): a validation that keeps malformed input from becoming a reading, no guard.

## Problem / root cause

`parseMeminfo` (`internal/collector/memory.go:40-47`) accepts any content with the four required fields and
`MemTotal` above 0. `memAndSwapFromFields` then computes `memUsedPct = (1 - available/memTotal) * 100` (line 68)
without a bound, so an available value above `MemTotal` — `MemAvailable`, or the `MemFree` fallback (line 64) —
gives a negative percent. Related defect of the same kind, found on the way: `SwapFree` above `SwapTotal` hits the
underflow guard at line 74-75 and reads as 0 % swap used, an invented reading. Both reach the snapshot, history,
alerts and Prometheus because the read counts as successful (`collector.go:582`, `612-613`).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: used percent is (1 − available / MemTotal) × 100 and goes negative when available > MemTotal | `memory.go:68`; no clamp, no check in `parseMeminfo` (`memory.go:40-47`) | confirmed |
| 2 | Issue: MemFree is the fallback when MemAvailable is missing (pre-3.14) | `memory.go:61-65`; broader than stated: the fallback also applies when MemAvailable is present but 0 | confirmed |
| 3 | Issue: since #249 such content is a valid reading that reaches snapshot, history, alerts, Prometheus | `parseMeminfo` returns no error; `collector.go:582`, `612-613`, `635-637` gate on `memErr` only | confirmed |
| 4 | Issue example MemTotal 100, MemFree 50, MemAvailable 300 gives −200 % | (1 − 300/100) × 100 = −200 | confirmed |
| 5 | `si_mem_available` has no upper clamp at `totalram` in its source | `mm/show_mem.c` (torvalds/linux master, read 2026-11-14): free − reserve + ≥half file LRU + ≥half reclaimable slab/misc; clamped at 0 only, no upper clamp. Bounded only because these page sets are disjoint parts of managed pages; global counters are per-CPU approximations. Exceeding on a real host not observed (real sample in `memory_test.go`: gap 491688 kB) | confirmed |
| 6 | MemTotal is `totalram_pages()`, MemFree `NR_FREE_PAGES`, SwapFree/SwapTotal `nr_swap_pages`/`total_swap_pages` both plus `nr_to_be_unused` | `mm/show_mem.c` `si_meminfo`, `mm/swapfile.c` `si_swapinfo`, `fs/proc/meminfo.c` lines 60-62, 89-90 | confirmed |
| 7 | Failure texts in `parseMeminfo` are fixed, without values | `memory.go:42`, `46`; decision 0011 | confirmed |
| 8 | No existing fixture holds a free value above its total | `meminfoFixture`, `meminfoRealSample` (`memory_test.go`), the inline content at `collector_test.go:1826` | confirmed |
| 9 | Decision 0011 is unreleased (editable in place) | added in 1dc50e2; `git tag --contains 1dc50e2` empty; tags end at v1.1.0 | confirmed |

## Acceptance criteria

- AC1: `parseMeminfo` fails with exactly `MemAvailable exceeds MemTotal in /proc/meminfo content` when
  `MemAvailable` is greater than `MemTotal` (the issue's example: MemTotal 100, MemFree 50, MemAvailable 300).
- AC2: `parseMeminfo` fails with exactly `MemFree exceeds MemTotal in /proc/meminfo content` when `MemFree` is
  greater than `MemTotal`, both without a `MemAvailable` line (the fallback case) and next to an in-range
  `MemAvailable`; when `MemFree` and `MemAvailable` both exceed, the `MemFree` text is returned.
- AC3: `parseMeminfo` fails with exactly `SwapFree exceeds SwapTotal in /proc/meminfo content` when `SwapFree` is
  greater than `SwapTotal`.
- AC4: a free value equal to its total is a reading: `MemAvailable` = `MemFree` = `MemTotal` parses without error
  and gives `UsedPercent` 0; `SwapFree` = `SwapTotal` (non-zero) parses and gives swap `UsedBytes` 0.
- AC5: `MemoryCollector.Collect` on content of AC1 returns that error and zero `Memory` and `Swap`.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestParseMeminfo_OutOfRange_AC1` | `internal/collector/memory_test.go` |
| AC2 | `TestParseMeminfo_OutOfRange_AC2` (subtests "AC2 without MemAvailable", "AC2 with MemAvailable in range", "AC2 both exceed") | `internal/collector/memory_test.go` |
| AC3 | `TestParseMeminfo_OutOfRange_AC3` | `internal/collector/memory_test.go` |
| AC4 | `TestParseMeminfo_EqualToTotal_AC4` (subtests "AC4 memory", "AC4 swap") | `internal/collector/memory_test.go` |
| AC5 | `TestMemoryCollector_Collect_OutOfRange_AC5` | `internal/collector/memory_test.go` |

The tests build their content from `meminfoRealSample` with `meminfoWith`/`meminfoWithout` (the real sample from
#249), replacing only the lines a case is about; AC3 and the AC4 swap case replace both swap lines (the sample has
no swap). AC4's memory case checks `UsedPercent` via `memAndSwapFromFields`. AC5 writes the file with
`writeTempFile` into `t.TempDir()`.

## Approach

In `parseMeminfo`, after the `MemTotal == 0` check, compare each free field with its total in this order:
`MemFree`/`MemTotal`, `MemAvailable`/`MemTotal`, `SwapFree`/`SwapTotal`; the first pair whose free value is
strictly greater fails with `fmt.Errorf("%s exceeds %s in /proc/meminfo content", free, total)` (or `errors.New`
per pair — the Dev's choice; the texts of AC1–AC3 are fixed). The pairs live in a private package-level slice next
to `requiredMeminfoKeys`. A missing `MemAvailable` reads 0 from the map and never exceeds, so it stays optional
with no extra branch; checking `MemFree` always covers the fallback `memAndSwapFromFields` may choose.
`memAndSwapFromFields` and `Collect` are unchanged; the swap underflow guard stays as a defensive line for direct
callers. The doc comment of `requiredMeminfoKeys` or the new slice says why the pairs exist (one sentence).

No new user-visible output beyond the error texts, which reach the log through the existing
`memory collection failed` warning.

## Options considered

Detailed in [0012](../../docs/decisions/0012-out-of-range-meminfo-is-failed-reading.md):

1. Fail the read on each free value above its total — chosen: no fabricated reading, a rule from the kernel's
   counters, consistent with 0011.
2. Clamp the used percent to 0–100 (issue's alternative) — lost: an invented 0 % next to `available_bytes` above
   `total_bytes`.
3. Clamp the available value to the total — lost: replaces the delivered value, still an invented 0 %.
4. Compare only the value the computation uses (issue's question) — lost: ties the rule to the formula (0011
   rejected that), duplicates the fallback selection, accepts `MemFree` > `MemTotal` beside a valid `MemAvailable`.
5. Return an error from `memAndSwapFromFields` — lost: signature and test call-site changes, validity split over
   two functions.
6. Memory only, swap unchanged — lost: the same fabricated reading (0 % swap) through the same check.
7. Leave and document — lost: keeps the reading the guarantee excludes.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `memory.go`: `parseMeminfo`, new private slice of free/total pairs | three range checks |
| `internal/collector` | `memory_test.go` | tests of AC1–AC5 |

## Signatures (for the Dev's skeleton)

None: no new or changed API member; the pair slice is private and described in *Approach*. No skeleton.

## Test files

- `internal/collector/memory_test.go`

Existing test code that calls a changed signature: none. Existing tests kept unchanged: none of their inputs holds
a free value above its total (fact 8).

Files of the change no unit test can cover: none.

## Areas

- Metrics (`docs/areas/metrics.md`, *Memory and swap*): the read also fails on a free value above its total; the
  swap "never below 0" note goes; 0012 under *Related decisions*.

## Documentation updates

All by the Dev, once the code is green:

- `docs/areas/metrics.md`, *Memory and swap*, first bullet: add that the read also fails when `MemFree` or
  `MemAvailable` exceeds `MemTotal` or `SwapFree` exceeds `SwapTotal` (equal is a reading), with the three error
  texts and a link to 0012; third bullet: drop "(never below 0)"; *Related decisions*: add the 0012 entry.
- `docs/API.md` line 231: the parenthetical "(one that fails or lacks a field the kernel always writes, …)" names
  out-of-range content too (e.g. "one that fails, lacks a field the kernel always writes or holds a free value above
  its total").
- Sweep done: `docs/areas/history.md:31`, `docs/areas/http-api.md:145`, `docs/API.md:812-813` and
  `docs/ARCHITECTURE.md:101` speak of a failed `/proc/meminfo` read generally and need no change; no `CHANGELOG`
  exists; `README.md` does not describe the rule.
- Decision records (Lead, done in step 2): 0012 created and indexed; 0011's last consequence now points to 0012.

## Architecture check

Strengthens the guarantee "never a fabricated reading" (`.squad/project.md`, `docs/ARCHITECTURE.md:101`); the v1
JSON shape is unchanged (a failed read already yields 0s), and the failure path (gap in history, no alert
evaluation, no Prometheus families) is the existing one of 0003, 0010 and 0011.

## Security considerations

- [x] No limits: comparisons of already-parsed `uint64` values, no new byte or length limit.
- [x] The error texts reaching the log are the three fixed texts of AC1–AC3, naming fields only, no values from
  the file; nothing new reaches an HTTP response.

## Decision records

- `docs/decisions/0012-out-of-range-meminfo-is-failed-reading.md` (Proposed)
- `docs/decisions/0011-incomplete-meminfo-is-failed-reading.md` (Accepted, unreleased): last consequence edited to
  link 0012.

## Out of scope / follow-ups

- Consistency of fields PiMonitor does not read (e.g. `Buffers` + `Cached` against `MemTotal`): not checked.

## Challenge notes

I challenged plan.md for issue #252 once, in mode plan. The plan holds up. I found nothing major and one minor point.

**What I checked against the code**
- **Root cause:** confirmed. `/home/user/PiMonitor/internal/collector/memory.go:40-47` checks only that the required keys are present and that `MemTotal` is not 0. Line 68 computes the used percent with no bound. Lines 61-65 choose the fallback, which includes a `MemAvailable` that is present but 0. Lines 74-75 give 0 swap used when `SwapFree` is above `SwapTotal`.
- **Covering the fallback:** checking `MemFree` against `MemTotal` on every read covers every value the fallback can pick. A missing `MemAvailable` reads 0 from the map and never exceeds the total. No extra branch is needed.
- **Callers:** `Collect` (`memory.go:109`) is the only production caller of `parseMeminfo` and `memAndSwapFromFields`. Everything else that calls them is in `memory_test.go`. No sibling path is missed.
- **Test fixtures:**
  - `meminfoWith` and `meminfoWithout` (`memory_test.go:157-187`) replace or drop a line that already exists.
  - `meminfoRealSample` (`memory_test.go:100`) has `SwapTotal: 0` and `SwapFree: 0` lines (lines 114-115), so AC3 and the AC4 swap case can replace both swap lines, as the plan says.
  - The sample's `MemFree` (15666136) and `MemAvailable` (15982088) are below `MemTotal` (16473776), so the "in range" subtest of AC2 holds.
  - `writeTempFile` exists in `testhelpers_test.go:13`.
- **AC4 values:** with all three memory values equal, (1 − 1) × 100 is exactly 0. With `SwapTotal` equal to `SwapFree`, the condition `swapTotal > swapFree` is false, so `UsedBytes` is 0. Both values follow from `memory.go:68` and `memory.go:74-76`.
- **Documentation:** the plan's sweep matches the text: `docs/areas/metrics.md:89-95` (including "(never below 0)" on line 94) and the parenthetical at `docs/API.md:231`.
- **Decision 0012:** consistent with the plan. It already names the container-virtualization risk as a consequence.

**Objections**

1. *minor* — Add a follow-up line under *Out of scope / follow-ups* in plan.md. The new checks compare raw kB values, but `memAndSwapFromFields` multiplies by 1024 afterwards (`memory.go:59-60, 71-72`), and that multiplication can overflow `uint64`. Example: `MemTotal` 18014398509481985 kB (2^54 + 1) passes every check and becomes 1024 bytes. That is a different defect from this issue's, so it stays out of this change. It belongs in the follow-ups because it is the same guarantee ("never a fabricated reading").
