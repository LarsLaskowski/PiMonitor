# Plan: Bound vcgencmd output carried into parse-failure errors

Source: Issue #205
Tier: standard — changes the text of parse errors (a behavior change) by adding a size bound on command output, which is input handling, not a security control (`.squad/routing.md`, *Tiers*).
Security areas touched: Subprocesses (`internal/collector/vcgencmd.go`), Parsing of local input (`vcgencmd` output in `internal/collector`), Logging (the bounded texts reach `slog` through the log-once rule)

## Problem / root cause

The parsers of a successful `vcgencmd` call quote its output into their errors without a bound:
`internal/collector/temperature.go:247` (`unexpected vcgencmd output: %q`, whole trimmed output) and `:253`
(`parse vcgencmd temp %q: %w`, whole output, plus the wrapped `strconv.NumError` that quotes the number text
again), `internal/collector/throttled.go:72` (`unexpected vcgencmd get_throttled output: %q`) and `:79`
(`parse throttled bitmask %q: %w`, plus the `NumError`). Only the exec-failure path of `run`
(`internal/collector/vcgencmd.go:109-118`) goes through `sanitizeVcgencmdDetail` (`:131-149`). The errors are
logged once per change of text (`docs/areas/metrics.md`, *Limits and errors*), and `CollectPMIC` keeps the
unsupported-output error in `pmicUnsupported` for the life of the process (`temperature.go:213-219`).

Related defect the issue does not name: the wrapped `strconv.NumError` is a second, independent unbounded copy
(row 3); bounding only the `%q` argument would leave it.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The four parse errors quote `vcgencmd` output without a length bound | `temperature.go:247,253`, `throttled.go:72,79` | confirmed |
| 2 | `run` returns the successful stdout uncapped | `vcgencmd.go:104` `cmd.Output()`, `:120` returns `strings.TrimSpace(string(out))` | confirmed |
| 3 | `strconv.NumError.Error()` quotes the whole input (`Num`), cloned without truncation | Go 1.27.4 `src/strconv/number.go:258-260`, `syntaxError`/`rangeError` `:272-278` | confirmed |
| 4 | Only the exec-failure path is bounded, by `sanitizeVcgencmdDetail` to `vcgencmdDetailMaxLen` (200) bytes, one line, cut at a rune boundary plus `...` | `vcgencmd.go:109-118,126,140-149`; `vcgencmd_test.go:236-258` | confirmed |
| 5 | Since #197 these errors are logged through the log-once rule and the PMIC unsupported-output error is latched | PR #206 merged as `c6be95f`, on this branch; `temperature.go:204-222`; `docs/areas/metrics.md:184-192` | confirmed |
| 6 | No existing test pins the text of these parse errors | `temperature_test.go:82-115,274-315` and `throttled_test.go:80-91,128-138` check only `err != nil` / `errors.Is` | confirmed |
| 7 | The bypass forms by which output reaches these errors are: the prefix mismatch (whole output), the number-parse failure (`ErrSyntax`, and `ErrRange` for `ParseFloat` overflow such as `1e999` and `ParseUint` overflow), each via the outer `%q` and the `NumError`; `baseError`/`bitSizeError` cannot occur (base 0 and 64-bit fixed) | `temperature.go:244-256`, `throttled.go:69-80`, `strconv/number.go:272-286` | confirmed |
| 8 | A fake `vcgencmd` script runs with an empty environment, so it has no `PATH` and should use shell builtins only | `vcgencmd.go:102` `cmd.Env = []string{}`; existing fakes use `echo` | confirmed |

## Acceptance criteria

`S(x)` below is `sanitizeVcgencmdDetail([]byte(x))`; `U` is the prefix `vcgencmd: output is not a temperature
reading: `. Tests compare the exact error text.

- AC1: `parseVcgencmdTemp` on an output without the `temp=` prefix returns `U + "unexpected vcgencmd output: " +
  strconv.Quote(S(output))`, and `errors.Is(err, errVcgencmdUnsupportedOutput)` holds. For 250 × `x` that is
  200 × `x` followed by `...` inside the quotes; for `"error=1\nerror_msg=x"` it is `"error=1 error_msg=x"`; for
  `garbage output` the text is unchanged from today.
- AC2: `parseVcgencmdTemp` on `temp=` followed by an unparsable number returns `U + "parse vcgencmd temp " +
  strconv.Quote(S(output)) + ": " + cause`, where cause is `invalid syntax` (`temp=` + 250 × `x`: the quote holds
  `temp=` + 195 × `x` + `...`) or `value out of range` (`temp=1e999'C`); `errors.Is` holds for both
  `errVcgencmdUnsupportedOutput` and `strconv.ErrSyntax` / `strconv.ErrRange` respectively. The output appears in
  the text once.
- AC3: `parseThrottled` on an output without the `throttled=` prefix returns `"unexpected vcgencmd get_throttled
  output: " + strconv.Quote(S(output))` (250 × `x` → 200 × `x` + `...`; `"foo\nbar"` → `"foo bar"`).
- AC4: `parseThrottled` on `throttled=` + an unparsable value returns `"parse throttled bitmask " +
  strconv.Quote(S(raw)) + ": " + cause`, cause `invalid syntax` (`throttled=` + 250 × `z`) or `value out of range`
  (`throttled=0x` + 300 × `f`: quote holds `0x` + 198 × `f` + `...`); `errors.Is` with `strconv.ErrSyntax` /
  `strconv.ErrRange` holds.
- AC5: `CollectPMIC` against a fake `vcgencmd` that prints 5000 × `x` returns the AC1 text with 200 × `x` + `...`,
  and a second call returns the same latched text without running the script again.
- AC6: Valid outputs parse as today — the existing rows of `TestParseVcgencmdTemp` and `TestParseThrottled` stay
  green unchanged.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestParseVcgencmdTemp_ErrorText` rows "AC1 long output is bounded", "AC1 multi-line output on one line", "AC1 short output unchanged" | `internal/collector/temperature_test.go` |
| AC2 | `TestParseVcgencmdTemp_ErrorText` rows "AC2 long unparsable number is bounded", "AC2 out-of-range number" | `internal/collector/temperature_test.go` |
| AC3 | `TestParseThrottled_ErrorText` rows "AC3 long output is bounded", "AC3 multi-line output on one line" | `internal/collector/throttled_test.go` |
| AC4 | `TestParseThrottled_ErrorText` rows "AC4 long unparsable bitmask is bounded", "AC4 out-of-range bitmask" | `internal/collector/throttled_test.go` |
| AC5 | `TestTemperatureCollector_CollectPMIC_LongOutputBounded_AC5` | `internal/collector/temperature_test.go` |
| AC6 | existing `TestParseVcgencmdTemp`, `TestParseThrottled` | `internal/collector/temperature_test.go`, `internal/collector/throttled_test.go` |

Test notes: build long inputs with `strings.Repeat`; write `\n` as a Go escape. The AC5 fake script must use
builtins only (row 8), e.g. a `while` loop with `printf x`, and count its runs (a marker file written with a
builtin redirect) or swap the script after the first call to show the second call does not run it.

## Approach

- `vcgencmd.go`: add a private helper `numParseCause(err error) error` next to `sanitizeVcgencmdDetail`: when
  `errors.As` finds a `*strconv.NumError`, return its `Err`, else return `err` unchanged. Doc comment says why
  (the `NumError` quotes the unbounded input).
- `temperature.go` `parseVcgencmdTemp`: both errors quote `sanitizeVcgencmdDetail([]byte(output))` instead of
  `output`; the second wraps `numParseCause(err)` instead of `err`. Keep `%w` on `errVcgencmdUnsupportedOutput`
  (the PMIC latch depends on it).
- `throttled.go` `parseThrottled`: the same for `output` and `raw`.
- `run` and `sanitizeVcgencmdDetail` themselves are unchanged. No new user-visible output beyond the error texts
  fixed in the criteria.

## Options considered

1. Bound the quoted output and drop the `NumError` (chosen).
2. Also cap the bytes `run` reads from stdout (the issue's optional part) — lost: rewrites the exec path and its
   stderr fallback for a transient buffer that is freed after the tick, limited in time by `vcgencmdTimeout`, and
   fillable only by a replaced root-owned binary; nothing it holds is logged or kept once option 1 is in.
3. Keep the `NumError` and bound its `Num` — lost: the output would appear twice in one message.
4. A new truncate-only helper that keeps whitespace (exact bytes under `%q`) — lost: a second rule for the same
   output; `sanitizeVcgencmdDetail` already gives the one-line form exec failures use.

Details in [0002](../../docs/decisions/0002-bound-vcgencmd-output-in-errors.md).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `vcgencmd.go` | new private helper `numParseCause` |
| `internal/collector` | `temperature.go` `parseVcgencmdTemp` | bounded quotes, cause instead of `NumError` |
| `internal/collector` | `throttled.go` `parseThrottled` | bounded quotes, cause instead of `NumError` |

## Signatures (for the Dev's skeleton)

None: no exported or test-called member is added or changed; the one new private helper is described in
Approach. Step 4 does not run.

## Test files

`internal/collector/temperature_test.go`, `internal/collector/throttled_test.go`.

Existing test code that calls a changed signature: none.

Files of the change no unit test can cover: none.

## Areas

Metrics (`docs/areas/metrics.md`, *Limits and errors*): the bound now covers unparsable `vcgencmd` output too.

## Documentation updates

- `docs/areas/metrics.md` lines 191-192 (Dev): the sentence "A `vcgencmd` exec failure message carries at most 200
  bytes of the command's own output, on one line." covers unparsable output as well — a failure or
  unparsable-output message carries at most 200 bytes of that output (counted before quoting, plus `...` when cut),
  on one line.
- Sweep: `docs/ARCHITECTURE.md` and `SECURITY.md` state no bound on `vcgencmd` output (searched for `vcgencmd`,
  `200`, `truncat`, `stdout`); `README.md` and `docs/API.md` are untouched (no option, route or shape). No changelog
  file exists. None else.

## Architecture check

No guarantee of `.squad/project.md` changes: a malformed `vcgencmd` answer is still an error that omits the reading
and is logged once; no reading is invented; the exec path and its environment are unchanged.

## Security considerations

- [x] The bound is on raw bytes before `%q` escaping: at most 200 bytes of output plus `...`; escaping can grow
  each byte to at most 4 characters (`\xff`), so the quoted part stays under 810 characters.
- [x] Exceptions that reach a message: the four parse errors above, whose texts the criteria fix; the
  `strconv` cause appears only as `invalid syntax` or `value out of range`.

## Decision records

- `docs/decisions/0002-bound-vcgencmd-output-in-errors.md` (Proposed)

## Out of scope / follow-ups

- Kernel-file parsers that quote their input (`cpu.go:42`, `loadavg.go:17`, `uptime.go:17`, `process.go:42-78`,
  `hwmon.go:139`, `cpufreq.go:92`, `temperature.go:71`) read `/proc`/`/sys`, whose contents the kernel bounds; not
  `vcgencmd` output, not this issue.
- Capping the stdout read: option 2, rejected in 0002.
