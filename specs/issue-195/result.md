RESULT: DONE — tier `patch/security`. I wrote no plan.md. The result below is meant for `specs/issue-195/result.md`.

## Tier
`patch/security`. The fix changes one literal in a length check (`8` → `9`), adds one test and edits the area document. That comes to about 27 changed lines outside `specs/`, under `PATCH_MAX_LINES` = 30, with no change to control flow. The edit is in a security area: *Parsing of local input: `/proc`… in `internal/collector`* (`.squad/project.md`). Security therefore runs the diff review in place of the Reviewer. No decision record is needed: this is a bug fix toward a behavior the area document already guarantees.

Security areas touched: Parsing of local input (`internal/collector/cpu.go`, `/proc/stat`). It creates, changes or removes no security control; it only brings the parser in line with the existing "never a crash" guarantee.

## Root cause
`/home/user/PiMonitor/internal/collector/cpu.go:35` skips a line only when `len(fields) < 8`. The loop at lines 39–40 then reads `fields[i+1]` for `i` from 0 to 7, which reaches `fields[8]`. A `cpu`/`cpuN` line with exactly seven counters has 8 fields, passes the check and panics with index out of range.

## Issue claims checked
- **Confirmed:** the check at line 35 and the read of `fields[1..8]` at line 40.
- **Confirmed:** `grep -rn "recover()" internal cmd` finds nothing, not even in tests. A panic in any goroutine with no recover ends the process.
- **Confirmed:** `packaging/pimonitor.service:22` has `Restart=on-failure`, and the fast tick calls `c.cpu.Collect()` (`internal/collector/collector.go:512`) on every tick.
- **Related finding:** the same parser also runs once at startup through `CoreCount()` (`collector.go:464`), so the same input would also crash startup. The same fix covers it.
- **Confirmed:** with an error, `Collect` returns `CPUUsage{}`, `collector.go:514` logs a warning, and `CPUValid` is false (`collector.go:628`). So "degrades to zero for that tick" holds when no valid cpu line remains.
- **Side effect, accepted:** skipping a single bad `cpuN` line shifts the indices of later entries in `per_core_percent`. Real kernels always print ≥ 8 counters, so I see no need for anything beyond the skip the issue proposes.

## Exact edits
1. `/home/user/PiMonitor/internal/collector/cpu.go` line 35:
   `if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {` → `if len(fields) < 9 || !strings.HasPrefix(fields[0], "cpu") {`
   - Covered by: the new test `TestParseProcStat_SevenCounters` (edit 3), written by `squad-tester` before the fix. It panics today, so it fails.
2. `/home/user/PiMonitor/docs/areas/metrics.md`
   - In *CPU and load*, after the bullet ending "`per_core_percent` follows the order of the `cpuN` lines." (line 42), add:
     ```
     - A `cpu`/`cpuN` line with fewer than eight counters is skipped. When no `cpu` line remains, or a counter is not a
       number, `/proc/stat` counts as unparsable: the CPU values are 0 for that tick and a warning is logged.
     ```
   - Delete the whole *Open questions* section at the end of the file (lines 221–226: the blank line before the heading, the `## Open questions` heading, the blank line and the three-line bullet on #195). The file then ends after line 220 with one newline.
   - Covered by: Security's diff review against `collector.go:512–514/628`. This is documentation only.
3. `/home/user/PiMonitor/internal/collector/cpu_test.go`: new test after `TestParseProcStat_MalformedField`, written by `squad-tester`:
   ```go
   func TestParseProcStat_SevenCounters(t *testing.T) {
   	t.Run("AC1 only cpu line has seven counters: error, no panic", func(t *testing.T) {
   		if _, err := parseProcStat("cpu  100 0 100 700 0 0 0\n"); err == nil {
   			t.Fatal("got nil error, want error for a cpu line with seven counters")
   		}
   	})
   	t.Run("AC2 cpuN line with seven counters is skipped, eight counters parse", func(t *testing.T) {
   		lines, err := parseProcStat("cpu  100 0 100 700 0 0 0 0\ncpu0 50 0 50 350 0 0 0\ncpu1 50 0 50 350 0 0 0 0\n")
   		if err != nil {
   			t.Fatalf("parseProcStat: got error %v, want nil", err)
   		}
   		if len(lines) != 2 || lines[0].name != "cpu" || lines[1].name != "cpu1" {
   			t.Fatalf("got %d lines %+v, want cpu and cpu1", len(lines), lines)
   		}
   	})
   }
   ```

Existing tests stay as they are. Their fixtures have ten counters per line, which parse the same before and after the fix.

## Acceptance criteria
- **AC1:** when every `cpu`/`cpuN` line in the `/proc/stat` content has exactly seven counters, `parseProcStat` returns an error and does not panic. Test: `TestParseProcStat_SevenCounters/AC1`.
- **AC2:** a `cpuN` line with seven counters is skipped. The lines with exactly eight counters are still returned, in order. Test: `TestParseProcStat_SevenCounters/AC2`.

## Documentation sweep
- `docs/areas/metrics.md` is covered by edit 2.
- `docs/ARCHITECTURE.md` has no enumeration this change extends, and there is no CHANGELOG file in the repository.
- No README, API.md or packaging changes are needed.

## Options considered
- **Return an error for a short `cpu` line instead of skipping it** (the issue's alternative). Not chosen because it is inconsistent with how the parser already treats short and non-`cpu` lines, which is to skip them, and because the issue's *Fix* names the skip.
- **Add a `recover()` around the collector.** Not chosen because it hides the defect instead of fixing it, and goes beyond what a patch should change.
