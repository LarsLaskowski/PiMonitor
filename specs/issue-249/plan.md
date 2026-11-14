# Plan: Incomplete /proc/meminfo content counts as a failed memory reading

Source: Issue #249
Tier: standard — a behavior change of a collector parser (which content counts as a failed reading) that creates, changes or removes no security control.
Security areas touched: Parsing of local input (`/proc/meminfo` in `internal/collector/memory.go`)

## Problem / root cause

`parseMeminfo` (`internal/collector/memory.go:42-44`) fails only when the result map is empty (`len(result) == 0`),
or on a non-numeric value (`memory.go:34-36`). Content that parses but lacks `MemTotal` returns no error, so
`MemoryCollector.Collect` (`memory.go:113-117`) returns `nil`, `fastTick` sets `MemoryValid` true
(`collector.go:582`), adds history points (`collector.go:612-613`) and hands a valid sample to the alert engine
(`collector.go:634-637`), and the Prometheus renderer writes the families: a fabricated 0 % reading. The same path
reads a missing `SwapTotal`/`SwapFree` as a host without swap (`memory.go:62-67`), and a missing `MemAvailable` plus
`MemFree` as 100 % used (`memory.go:54-61`, the related defect found on the way). A `MemTotal` of 0 reads 0 % used
(`memory.go:62-64`).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: `parseMeminfo` fails only when no `key: number` line parses | `memory.go:42-44`; it also fails on a non-numeric value, `memory.go:34-36` (an addition, not a contradiction) | confirmed |
| 2 | Issue: content without `MemTotal` reads 0 % in snapshot, history, alerts and Prometheus | `memAndSwapFromFields` gives 0 for total 0 (`memory.go:51-64`); `memErr == nil` drives `MemoryValid` (`collector.go:582`), history (`collector.go:612-613`) and alert validity (`collector.go:635,637`); Prometheus keys on `MemoryValid` ([0010](../../docs/decisions/0010-missing-reading-left-out-of-prometheus.md)) | confirmed |
| 3 | Issue: a real kernel always provides `MemTotal` | `fs/proc/meminfo.c` (torvalds/linux master), `meminfo_proc_show`: `MemTotal`, `MemFree` (lines 60-61), `SwapTotal`, `SwapFree` (lines 89-90) written unconditionally, outside every `#ifdef`; so for the other three as well | confirmed |
| 4 | `MemAvailable` is missing before Linux 3.14 and handled by a fallback | comment and fallback at `memory.go:53-58`; `TestMemAndSwapFromFields_OldKernelFallback` | confirmed |
| 5 | Lines without a unit (`HugePages_Total:       0`) parse today | `strings.Fields(...)[0]` at `memory.go:28-33`; present in the real sample | confirmed |
| 6 | Every existing test fixture reaching `parseMeminfo` holds all four required fields with `MemTotal` above 0 | `meminfoFixture` (`memory_test.go:9-17`), used by `memory_test.go` and `collector_test.go:1074,1800-1824`; `collector_test.go:1826` and `TestParseMeminfo_Empty` expect an error already | confirmed |
| 7 | Real sample | `specs/issue-249/meminfo-sample.txt`: `/proc/meminfo` of the session host, Linux 6.18.44 x86_64, not a Pi; variants: modern kernel with `MemAvailable`, no swap (`SwapTotal`/`SwapFree` 0 kB), unit-less `HugePages_*` lines, `Zswap*`. Only numbers, no private values. Pi-specific fields (`CmaTotal`/`CmaFree`, `CONFIG_CMA`) are absent and unknown keys are ignored anyway | confirmed |

## Acceptance criteria

- AC1: `parseMeminfo` returns an error when one of `MemTotal`, `MemFree`, `SwapTotal`, `SwapFree` is missing; the
  text is `no <field> field in /proc/meminfo content` for the first missing one in that order (empty content:
  `no MemTotal field in /proc/meminfo content`).
- AC2: `parseMeminfo` returns the error `MemTotal is 0 in /proc/meminfo content` when `MemTotal` is 0.
- AC3: complete content still parses: the real sample (no swap: swap totals 0, `UsedPercent` 0) and content without
  `MemAvailable` (old kernel, `MemFree` used) return no error.
- AC4: a fast tick on content without `MemTotal` leaves `Snapshot().MemoryValid` false and adds no
  `memory_used_percent` and no `swap_used_percent` history point.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestParseMeminfo_MissingField_AC1`: table over the four fields, each removed from the real sample, plus empty content; compares the exact error text | `internal/collector/memory_test.go` |
| AC2 | `TestParseMeminfo_ZeroMemTotal_AC2`: real sample with `MemTotal` set to `0 kB`; exact error text | `internal/collector/memory_test.go` |
| AC3 | `TestParseMeminfo_Complete_AC3`: subtests "AC3 real sample" (no error; through `memAndSwapFromFields`: `MemTotal` 16473776 × 1024 bytes, swap all 0) and "AC3 without MemAvailable" (no error, `AvailableBytes` = `MemFree` × 1024) | `internal/collector/memory_test.go` |
| AC4 | `TestCollector_FastTick_IncompleteMeminfo_AC4`: collector with `MemoryCollector{path}` on the real sample without its `MemTotal` line, one `fastTick`; `MemoryValid` false, `History().MemoryUsedPercent` and `SwapUsedPercent` empty | `internal/collector/collector_test.go` |

The real sample is inlined by the Tester as a string constant in `memory_test.go` (`docs/UNIT_TESTS.md`: fixtures
are inline constants), copied from `specs/issue-249/meminfo-sample.txt` unchanged; the variants of AC1, AC2 and AC4
are derived from it in the test (a line removed or replaced), not hand-typed.

## Approach

In `parseMeminfo`, replace the `len(result) == 0` check (`memory.go:42-44`) by a loop over a private package-level
list of required keys, in the order `MemTotal`, `MemFree`, `SwapTotal`, `SwapFree`, returning
`fmt.Errorf("no %s field in /proc/meminfo content", key)` for the first one absent from the map (presence via the
two-value map lookup, so a real `SwapTotal: 0 kB` passes), then `fmt.Errorf("MemTotal is 0 in /proc/meminfo
content")` (or `errors.New`) when the total is 0. Nothing else changes: `memAndSwapFromFields` keeps its guards, the
`MemFree` fallback stays (`MemAvailable` stays optional), and `Collect` passes the error on as today. The error text
reaches only the existing warning `memory collection failed` (`collector.go:533-535`), logged every tick; no new
user-visible output.

## Options considered

From [0011](../../docs/decisions/0011-incomplete-meminfo-is-failed-reading.md): only `MemTotal` required (the
issue's narrow reading — leaves the missing swap pair reading as "no swap" and missing `MemFree` as 100 % used);
"`MemAvailable` or `MemFree`" instead of `MemFree` (ties the rule to the formula, accepts content no kernel writes);
a separate swap validity (rejected by 0010, option 6: one read, one validity); leave and document (the issue notes the
path is unreachable, but it keeps a fabricated reading the guarantee rules out, for a short check). Chosen: the four
fields every kernel writes, plus `MemTotal` above 0.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `memory.go`, `parseMeminfo` | required-field and zero-total checks replace the empty-map check |
| `internal/collector` | `memory_test.go`, `collector_test.go` | new tests (Tester) |

## Signatures (for the Dev's skeleton)

None: `parseMeminfo` keeps its signature, the list of required keys is private. No skeleton (step 4 skipped).

## Test files

- `internal/collector/memory_test.go`
- `internal/collector/collector_test.go`

Existing test code that calls a changed signature: none. Existing tests stay unchanged (fact 6):
`TestParseMeminfo_Empty` still gets an error, the `memAndSwapFromFields` tests call it with maps directly.

Files of the change no unit test can cover: none.

## Areas

- [Metrics](../../docs/areas/metrics.md), *Memory and swap*: which content counts as a failed read.

## Documentation updates

Sweep: `MemTotal`/`meminfo`/"failed … read" across `docs/`, `README.md`, `docs/ARCHITECTURE.md`. Owner: Dev.

- `docs/areas/metrics.md`, *Memory and swap*: a read also fails when `MemTotal`, `MemFree`, `SwapTotal` or
  `SwapFree` is missing or `MemTotal` is 0 (`MemAvailable` stays optional); the clause "0 when the total is 0" of
  `memory.used_percent` goes, since a zero total is now a failed read; link 0011 in the bullet and add it to
  *Related decisions*.
- `docs/API.md:231`: "After a failed `/proc/meminfo` read" gains "(one that fails or lacks a field the kernel always
  writes, [Metrics](areas/metrics.md#memory-and-swap))" or equivalent wording.
- Checked, no change: `docs/API.md:811-812`, `docs/areas/http-api.md:144-145`, `docs/areas/history.md:30-31` speak
  of a failed `/proc/meminfo` read, which Metrics now defines; `docs/ARCHITECTURE.md` names no meminfo rule;
  `README.md` none. No changelog file exists; the PR template asks for none.

## Architecture check

Strengthens the guarantee "never a fabricated reading" (`.squad/project.md`, *Guarantees*; Metrics, *Limits and
errors*): no guarantee is weakened, the v1 shape is unchanged (the snapshot keeps its 0), and the failure path is the
existing one of 0003/0010. `docs/ARCHITECTURE.md` needs no update.

## Security considerations

- [x] No limits: the check runs on the parsed map; no byte or length limit is added.
- [x] The two new error texts reach only the `memory collection failed` log warning, with a fixed field name from
  the list; no file content is quoted. Not user-visible in any API response.

## Decision records

- `docs/decisions/0011-incomplete-meminfo-is-failed-reading.md` (Proposed)

## Out of scope / follow-ups

- Telling a failed memory reading from a real 0 in the v1 JSON (needs `/api/v2/...`, 0010 option 3).
- Real-Pi check: none needed; the four fields are written by every kernel, Pi kernels included (fact 3).
