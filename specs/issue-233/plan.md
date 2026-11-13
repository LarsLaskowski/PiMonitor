# Plan: Missing aggregate cpu line in /proc/stat yields a valid 0 % CPU reading

Source: Issue #233
Tier: standard — a behavior change in the CPU collector's error path that creates, changes or removes no security control.
Security areas touched: Parsing of local input (`/proc/stat` in `internal/collector/cpu.go`)

## Problem / root cause

`CPUCollector.Collect` (`internal/collector/cpu.go:96-130`) sets `OverallPercent` only when it meets a line named
`cpu` (`cpu.go:115-116`); when none is there the field stays 0 and the method returns a nil error, because
`parseProcStat` fails only when no `cpu`/`cpuN` line at all is left (`cpu.go:58-60`). An aggregate line with fewer
than eight counters is skipped silently (`cpu.go:35`), so the same happens when the aggregate line is present but
short. The fast tick then treats the 0 as a real reading: it records a `cpu_percent` history point
(`internal/collector/collector.go:604`) and passes `CPUValid: true` to the alert engine (`collector.go:628`), and no
warning is logged (`collector.go:512-515` logs only on an error).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Without an aggregate `cpu` line `Collect` returns `OverallPercent` 0 and a nil error | `cpu.go:112-121` sets the field only for `l.name == "cpu"`; `cpu.go:58-60` errors only on zero lines | confirmed |
| 2 | An aggregate line "skipped as malformed" leads to the same result | `cpu.go:35` skips a line with fewer than 9 fields; a non-numeric counter is no skip but an error for the whole file (`cpu.go:41-43`), so "malformed" here means too few counters only | confirmed |
| 3 | The fast tick passes `CPUValid: true` with 0 % to the alert engine | `collector.go:627-628`, `CPUValid: s.cpuErr == nil` | confirmed |
| 4 | The fast tick also records the 0 in the `cpu_percent` history (not named in the issue) | `collector.go:598-604`, `addPoint` adds a point when the error is nil | confirmed |
| 5 | The dashboard and API show 0 % as a real value | `collector.go:570` copies the usage into the snapshot; after the fix the snapshot still shows 0 (with `per_core_percent` omitted), as for every failed fast-tick source: `docs/areas/metrics.md`, *Limits and errors*, says `GET /api/v1/metrics` cannot tell a failure's 0 from a real 0, so the fix changes history, alerts and logging, not the snapshot's 0 | confirmed |
| 6 | Real kernels always print the aggregate line | this host's `/proc/stat` starts with `cpu  756 0 298 …` before `cpu0`; the kernel's `show_stat` (`fs/proc/stat.c`) prints the `cpu` line unconditionally | confirmed |
| 7 | `docs/areas/metrics.md` names only the "neither `cpu` nor `cpuN`" case as unparsable | `docs/areas/metrics.md:43-45` | confirmed |
| 8 | A failed CPU collection already yields an invalid alert sample, no history point and a warning on every tick | `collector.go:512-515`, `:604`, `:628`; tested by `TestCollector_FastTick_FailedReadingAddsNoHistoryPoint` (`collector_test.go:1395`); `docs/areas/metrics.md:188` | confirmed |
| 9 | `CoreCount` counts only `cpuN` lines and does not need the aggregate line | `cpu.go:135-152` | confirmed |
| 10 | No existing test feeds `Collect` or `CoreCount` content without an aggregate line | `cpu_test.go` fixtures 1 and 2 and `TestParseProcStat_SevenCounters` AC2 all hold a `cpu` line; `collector_test.go` uses a missing path | confirmed |

## Acceptance criteria

- AC1: `Collect` on `/proc/stat` content with `cpuN` lines but no `cpu` line returns a non-nil error whose text
  contains `no aggregate cpu line in /proc/stat content`, and a zero `CPUUsage` (`OverallPercent` 0,
  `PerCorePercent` nil) — on the first call and on a call after a valid sample.
- AC2: The same holds when the `cpu` line is present but has fewer than eight counters and the `cpuN` lines are
  valid.
- AC3: A failed call leaves the stored previous sample unchanged: valid sample A, then content without a `cpu`
  line, then valid sample B yields the A→B delta for `OverallPercent` and both `PerCorePercent` values.
- AC4: A fast tick reading `/proc/stat` content without a `cpu` line adds no `cpu_percent` history point, and the
  snapshot's `cpu` has `overall_percent` 0 and no `per_core_percent`.
- AC5: `CoreCount` on content with `cpuN` lines but no `cpu` line still returns the number of `cpuN` lines and a nil
  error.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCPUCollector_Collect_MissingAggregateLine`, subtests "AC1 first call" and "AC1 after a valid sample" | `internal/collector/cpu_test.go` |
| AC2 | `TestCPUCollector_Collect_MissingAggregateLine`, subtest "AC2 aggregate line with seven counters" | `internal/collector/cpu_test.go` |
| AC3 | `TestCPUCollector_Collect_FailedReadKeepsPreviousSample_AC3` | `internal/collector/cpu_test.go` |
| AC4 | `TestCollector_FastTick_MissingAggregateCPULine_AC4` | `internal/collector/collector_test.go` |
| AC5 | `TestCPUCollector_CoreCount`, new subtest "AC5 no aggregate line" (the existing body becomes a subtest of a table, or stays and the new case is a second subtest) | `internal/collector/cpu_test.go` |

Fixtures are hand-typed constants beside `procStatFixture1`/`2`: a content without an aggregate line is no format
variant any kernel produces (fact 6), so no real sample exists for it; the cpuN-only fixture is `procStatFixture1`
without its first line.

## Approach

In `Collect`, after `parseProcStat` succeeds and before taking `c.mu`, check that one of the parsed lines is named
`cpu`; if none is, return `CPUUsage{}` and `fmt.Errorf("no aggregate cpu line in /proc/stat content")` (an
unexported helper or an inline loop, the Dev's choice; no wrapped error exists to `%w`). Returning before the lock
leaves `c.prev` untouched (AC3), and the check sits before the `c.prev != nil` branch, so the first call fails too.
`parseProcStat` and `CoreCount` are unchanged. The existing fast-tick handling of `s.cpuErr` (warning, no history
point, `CPUValid: false`) then applies without a change to `collector.go`. No new user-visible output beyond the
error text inside the existing `cpu collection failed` warning.

## Options considered

1. **Check in `Collect`, drop the per-core values too** (chosen): one rule — no aggregate line, no CPU values for
   the tick — matching the existing "unparsable" behavior in `docs/areas/metrics.md`; content no kernel produces
   is not worth a partial result.
2. **Return the per-core values with the error** (the issue leaves it open): the fast tick copies `s.cpuUsage` into
   the snapshot regardless of the error, so `per_core_percent` would still be served; but a `/proc/stat` without
   its aggregate line is not trustworthy for the per-core lines either, and the area document would need a second,
   partial failure mode for a case that does not occur on real kernels. Lost.
3. **Check in `parseProcStat`**: covers `CoreCount` too, so `cpu_count` would become 0 with a startup warning for
   such content, although the `cpuN` count is still correct and `cpu_count` is defined by the `cpuN` lines alone
   (fact 9). Lost: it widens the change to a value that is not fabricated.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `CPUCollector.Collect` in `cpu.go` | error on missing aggregate line, before state update |
| `internal/collector` | `cpu_test.go`, `collector_test.go` | new tests (Tester) |
| docs | `docs/areas/metrics.md` | unparsable rule (Dev) |

## Signatures (for the Dev's skeleton)

None: no new or changed API member. The check is private to `Collect` (Approach); the doc comment of `Collect`
gains one sentence that it fails when the aggregate line is missing. Existing files the skeleton rewrites: none.

## Test files

`internal/collector/cpu_test.go` and `internal/collector/collector_test.go`.

Existing test code that calls a changed signature: none. Existing tests whose input the change handles
differently: none (fact 10), so all stay unchanged.

Files of the change no unit test can cover: none.

## Areas

`docs/areas/metrics.md` (*CPU and load*): the unparsable rule gains the missing aggregate line.

## Documentation updates

- `docs/areas/metrics.md`, *CPU and load*, the bullet at lines 43-45 (Dev, against the green code): `/proc/stat`
  counts as unparsable also when no aggregate `cpu` line remains (none present, or the one present has fewer than
  eight counters), even when `cpuN` lines remain; then `overall_percent` is 0, `per_core_percent` is omitted, and
  the existing consequences hold (warning logged). Add that `cpu_count` needs no aggregate line (the `cpu_count`
  bullet at line 48 or the same bullet). *Limits and errors* line 188 already lists `cpu` among the failures logged
  every tick: no change.
- Sweep: `docs/ARCHITECTURE.md`, `README.md`, `docs/API.md`, `docs/areas/history.md` and `docs/areas/alerts.md`
  carry no enumeration of `/proc/stat` failure cases (searched for `/proc/stat`, `overall_percent`,
  `per_core_percent`); `docs/areas/history.md` and `alerts.md` state the generic failed-reading rule, which now
  simply applies. The PR template asks for no changelog; its project block asks the Reviewer Notes to name the
  collector touched (`CPUCollector`) — orchestrator, in the PR body.

## Architecture check

Strengthens the guarantee "a missing source yields an empty value and a logged warning, never a fabricated
reading" (`AGENTS.md`, *Code style*; `.squad/project.md`, *Guarantees*); it reuses the failed-reading path of
[0003](../../docs/decisions/0003-failed-reading-leaves-history-gap.md). No `/api/v1` shape changes.

## Security considerations

- [x] No limits: the change adds no byte or length limit; input is a local file read as today.
- [x] The error text reaches only the service log (`cpu collection failed` warning), never an API client; it
  contains no file content.

## Decision records

- none: no decision beyond the obvious fix; the per-core choice is recorded under *Options considered* and in the
  area document.

## Out of scope / follow-ups

- Related, pre-existing, not changed here: the first fast tick after start reports `overall_percent` 0 with a nil
  error, so it records a `cpu_percent` history point of 0 and passes `CPUValid: true` (`cpu.go:112`, documented at
  `docs/areas/metrics.md:46` and left open in 0003's *Consequences*). Proposed issue: "[Collector] First fast tick
  records a 0 % CPU reading as valid" — body: the first `Collect` has no previous sample yet returns a nil error, so
  the fast tick records a 0 in `cpu_percent` and evaluates 0 % as a valid alert sample; decide whether the first
  tick should count as no reading.
- Related, minor: a `cpuN` line missing from the previous sample (a core hot-plugged in) is skipped
  (`cpu.go:114`), so `per_core_percent` shifts for one tick; real Pis do not hot-plug cores. No issue proposed.
