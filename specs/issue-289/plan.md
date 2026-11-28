# Plan: Lower bound for poll_interval_seconds and updates_check_minutes

Source: Issue #289
Tier: standard — a validation rule changes which configurations start (behavior), and it creates or changes no
security control.
Security areas touched: Parsing of local input (the YAML config in `internal/config`)

## Problem / root cause

`Config.validateTiming` (`internal/config/config.go:590-595`) rejects `poll_interval_seconds` and
`updates_check_minutes` only when `<= 0`. `FastInterval`/`SlowInterval` (`config.go:346-353`) multiply the float
by `time.Second`/`time.Minute` and convert to `time.Duration`, so a positive value below 1 ns truncates to `0`,
which `Collector.clampInterval` (`internal/collector/collector.go:422-427`) replaces by 1 s, and any small positive
value (`0.001`) passes as a 1 ms fast tick or a 60 ms slow tick.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | A positive value below 1 ns truncates to a zero `time.Duration` | `config.go:346-353`: `time.Duration(c.PollIntervalSeconds * float64(time.Second))`; Go float→int conversion truncates toward zero | confirmed |
| 2 | `clampInterval` "silently" replaces it by 1 s | `collector.go:422-427` replaces `d <= 0` by 1 s but logs `non-positive tick interval clamped to 1s` at warn, so not silently (the replacement itself is confirmed) | refuted |
| 3 | `0.001` runs the fast ticker every millisecond | `validateTiming` accepts it (`config.go:590`), `FastInterval()` = 1 ms > 0, `clampInterval` keeps it, `time.NewTicker(c.fastInterval)` (`collector.go:394`) | confirmed |
| 4 | The slow tick runs the apt check and a `history.bin` write | `collector.go:410-412`: `slowTick` then `persistHistory` | confirmed |
| 5 | The dashboard never polls faster than once per second | `internal/web/assets/app.js:1474,1604`: `Math.max(1, config.poll_interval_seconds) * 1000` | confirmed |
| 6 | Upper bounds (0035) run before `validateTiming`; no value can break both | `Validate`, `config.go:509-518`; bounds 604800 / 10080 | confirmed |
| 7 | Defaults 5 s and 15 min pass a minimum of 1 | `Default()`, `config.go:280-281` | confirmed |
| 8 | With a 1 s minimum the ratio check is still needed | 525600 × 60 / 1 = 31,536,000 > `maxHistoryCapacity` 1,000,000 (`config.go:417`) | confirmed |
| 9 | Record 0035 and its area text are unreleased | `git tag --contains a536905` prints nothing; latest tag `v1.3.0` | confirmed |
| 10 | Existing tests assert the old `> 0` text or rely on a sub-1 poll interval | `config_test.go:1547-1553` (`must be > 0 (got 0)`), `:485` (`0.05`), `:494` (`1e-300`) | confirmed |
| 11 | `HistoryCapacity`'s `<= 0` guard and `TestHistoryCapacity_ZeroPollInterval` do not call `Validate` | `config.go:401`, `config_test.go:126-132` | confirmed |

## Acceptance criteria

- AC1: `Validate` rejects `poll_interval_seconds` below 1 with exactly `poll_interval_seconds must be >= 1 (got <value>)`
  (`<value>` formatted `%v`) for `0.999`, `0.001`, `1e-10`, `0` and `-1`; `1` passes.
- AC2: `Validate` rejects `updates_check_minutes` below 1 with exactly `updates_check_minutes must be >= 1 (got <value>)`
  for `0.999`, `0.001`, `1e-12`, `0` and `-1`; `1` passes.
- AC3: `Load` of a file with `poll_interval_seconds: 0.001` fails with an error whose text contains
  `poll_interval_seconds must be >= 1 (got 0.001)`, and of `updates_check_minutes: 0.5` with
  `updates_check_minutes must be >= 1 (got 0.5)`.
- AC4: the ratio check still applies at the minimum: `poll_interval_seconds: 1` with `history_window_minutes: 525600`
  fails with the existing ratio text (`history_window_minutes (525600) / poll_interval_seconds (1) implies 3.1536e+07 history points per series, exceeding the 1000000 limit; increase poll_interval_seconds or reduce history_window_minutes`).

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestValidate_IntervalLowerBounds` (`AC1 …` subtests, exact text) | `internal/config/config_test.go` |
| AC2 | `TestValidate_IntervalLowerBounds` (`AC2 …` subtests, exact text) | `internal/config/config_test.go` |
| AC3 | `TestLoad_RejectsIntervalBelowMinimum` (`AC3 …` subtests) | `internal/config/config_test.go` |
| AC4 | `TestValidate_IntervalLowerBounds` (`AC4 ratio at poll 1`, exact text) | `internal/config/config_test.go` |

## Approach

- In `internal/config/config.go` add the constants `minPollIntervalSeconds = 1` and `minUpdatesCheckMinutes = 1`
  (doc comment naming record 0036) next to the upper-bound constants, and a private helper
  `atLeast(key string, value float64, limit int) error` beside `atMost`, returning
  `fmt.Errorf("%s must be >= %d (got %v)", key, limit, value)` when `value < float64(limit)`.
- In `validateTiming` replace the two `<= 0` checks by `atLeast("poll_interval_seconds", …, minPollIntervalSeconds)`
  and `atLeast("updates_check_minutes", …, minUpdatesCheckMinutes)`, same position, so the order of checks stays
  finiteness → upper bounds → these lower bounds → the rest; the ratio check below is unchanged.
- Update the comment at `maxHistoryCapacity` (`config.go:411-416`): its example `poll_interval_seconds: 0.05` is now
  rejected by the minimum; use `history_window_minutes: 525600 with poll_interval_seconds: 1`.
- `clampInterval` stays unchanged (defense in depth for a `collector.Config` built without validation).
- No new user-visible output beyond the two error texts fixed by AC1/AC2.

Early draft PR: no

## Options considered

1. **1 s and 1 min** — chosen (record 0036): far below the defaults, no faster than the dashboard reads, the key's own unit.
2. **At least 1 ns (only what truncates to zero)** — lost: `0.001` still ticks every millisecond, the issue's case.
3. **0.1 s and 0.1 min** — lost: the dashboard never shows sub-second ticks (fact 5), `/proc/stat` counts 10 ms ticks,
   and the slow tick would run the apt check and an SD-card write every 6 s.
4. **Runtime clamp to the minimum with a warning** — lost: the service would run on a value other than configured,
   against fail-fast validation; the issue asks for rejection.
5. **Keep `must be > 0` for `<= 0`, a second text for (0, 1)** — lost: two texts for one rule; the old text is
   only on unreleased tests from #287 and one doc example.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go` | two constants, `atLeast` helper, `validateTiming`, one comment |
| `internal/config` | `config_test.go` | new tests, adapted cases |

## Signatures (for the Dev's skeleton)

None: no exported API changes. The private constants and the `atLeast` helper are described in *Approach*.

## Test files

- `internal/config/config_test.go`

Existing test code to adapt, by the Tester:
- `config_test.go:1547-1548` (`AC1 poll_interval_seconds 0 keeps its text`) and `:1552-1553` (`AC2 updates_check_minutes 0 keeps its text`):
  expect `poll_interval_seconds must be >= 1 (got 0)` / `updates_check_minutes must be >= 1 (got 0)`, rename without "keeps its text".
- `config_test.go:483-486` (`history capacity exceeds sanity cap`, poll `0.05`): poll `1`, window `525600`, so it
  still reaches the ratio check.
- `config_test.go:487-495` (`history capacity ratio overflows int64`, poll `1e-300`): now rejected by the minimum and
  no longer reaches the ratio; delete the case (AC1's `1e-10` covers a tiny poll value).
- `TestValidate_RejectsBadValues` zero/negative cases (`:464-466`) and `TestLoad_RejectsZeroPollInterval` only check
  for an error: unchanged.

Files of the change no unit test can cover: none.

## Areas

- `docs/areas/configuration.md (+45 words)`: the *Top level* rows of `poll_interval_seconds` (`1 to 604800`) and
  `updates_check_minutes` (`1 to 10080`); in *Validation and startup failure* a bullet after the upper-bound bullet:
  `poll_interval_seconds` and `updates_check_minutes` below 1 fail with `<option> must be >= 1 (got <value>)`, also
  for zero and negative values, linking 0036; the startup example `poll_interval_seconds must be > 0 (got 0)`
  becomes `poll_interval_seconds must be >= 1 (got 0)`; *Related decisions* gains 0036.

## Documentation updates

All by the Dev, once the code is green:
- `docs/areas/configuration.md` as in *Areas*.
- `packaging/pimonitor.example.yaml`: the comments of `poll_interval_seconds` (lines 18-20) and
  `updates_check_minutes` (lines 23-26) state the minimum 1 beside the existing maximum.
- `docs/areas/metrics.md:31` ("A non-positive interval (which configuration validation already rejects) is replaced
  by 1 s") stays true: no change. `metrics.md:273`, `history.md:175`, `dashboard.md:248` point to the configuration
  document for valid ranges: no change. `README.md` and `docs/API.md` state no range of these keys: no change.
- PR (orchestrator): label `breaking-change`, the description says a value between 0 and 1 no longer starts.

## Architecture check

No guarantee of `docs/ARCHITECTURE.md` or `.squad/project.md` is weakened: startup validation stays fail-fast, the
collector keeps never crashing on a non-positive interval. No flow changes; no `ARCHITECTURE.md` update.

## Security considerations

- [x] No limits on parsed input bytes; the bound is on a decoded number.
- [x] The error texts quote only the non-secret value of the key (`%v` of a finite float); no other exception reaches the user.

## Decision records

- `docs/decisions/0036-poll-and-updates-intervals-at-least-one-unit.md` (Proposed), indexed.
- `docs/decisions/0035-float-duration-options-capped-at-seven-or-365-days.md` (unreleased, edited in place by the Dev): its lower-bound sentence and its "tiny poll interval" consequence point to 0036.

## Out of scope / follow-ups

- The dashboard's `Math.max(1, …)` floors in `app.js` become redundant; harmless, left as they are.

## Challenge notes

I found no major problem with the plan for issue #289 (specs/issue-289/plan.md) or with decision 0036. One minor fix is needed: two sentences in the unreleased record 0035 become false once 0036 lands.

**What I checked and found correct**
- **Root cause:** in /home/user/PiMonitor/internal/config/config.go, `validateTiming` only rejects values `<= 0` (lines 590-595). `FastInterval`/`SlowInterval` (lines 346-353) truncate toward zero. `Validate` runs finiteness, then upper bounds, then timing (lines 509-518). `atMost` (lines 468-473) is the model for the planned `atLeast`.
- **No other input path:** the only command-line flags are `-config`, `-listen`, `-log-level`, `-api-key` and `-version` (config.go:835-840). Nothing else can set either option without passing `Validate`.
- **Exact error texts:** I ran Go's `%v` on the plan's values. It prints `0.999`, `1e-10`, `1e-12`, `-1`, and `3.1536e+07` for 525600×60/1, so the texts in AC1, AC2 and AC4 are right.
- **Existing tests:** the cases the plan lists to adapt (config_test.go:483-495 and 1547-1553) are the complete set. The setters in `TestValidate_RejectsNonFiniteNumbers` and `numberOptionYAML` only use NaN/Inf, and the other tests use 5, 7, 10, 30 or 60. Nothing in other packages builds a sub-1 config through `Validate`.
- **Deleting the `1e-300` overflow case is safe:** with a 1 s minimum and a 525600-minute window cap, the ratio is at most 3.15e7, so it can no longer overflow.
- **Other dependent rules still hold:** the `healthz_max_staleness_seconds >= poll_interval_seconds` rule (config.go:625) is unaffected. docs/ARCHITECTURE.md:503, docs/areas/metrics.md:31 and docs/UNIT_TESTS.md:220 all still read correctly. README.md states no range for these keys, and the repo has no CHANGELOG.
- **Need, alternatives and tier:** the five options are compared fairly. Tier standard is appropriate.

**Objections**

1. `minor` — Record 0035 (/home/user/PiMonitor/docs/decisions/0035-float-duration-options-capped-at-seven-or-365-days.md) is unreleased (`git tag --contains a536905` prints nothing) and would contradict 0036. Line 42 says "the lower-bound texts are unchanged", and the Consequences say "a tiny poll interval still makes the ratio huge". Once 0036 sets a 1 s minimum, there is no tiny poll interval and the `> 0` texts change. The project rule is to edit an unreleased record in place rather than contradict it. Instruction for the Dev: edit 0035 in place. Make the lower-bound sentence say the lower bounds of `poll_interval_seconds` and `updates_check_minutes` are set by [0036](0036-poll-and-updates-intervals-at-least-one-unit.md). Replace the "tiny poll interval" consequence with "the ratio check stays: at the minimum `poll_interval_seconds` of 1 ([0036](0036-poll-and-updates-intervals-at-least-one-unit.md)), 525600 minutes still make 31,536,000 points". Add 0036 to the plan's *Decision records* list as touching 0035.
