# Spec: Upper bound for alerts.for_seconds and alerts.openhab_for_seconds

Source: Issue #272
Status: Draft

## Problem

An operator who sets `alerts.for_seconds` or `alerts.openhab_for_seconds` to a huge value (a typo such as `1e12`)
gets no startup error: `Config.Validate()` checks only `>= 0` and finiteness (`internal/config/config.go`), and
`AlertFor()` / `OpenHABAlertFor()` convert the seconds to a `time.Duration` with no upper bound. A product above
`math.MaxInt64` nanoseconds (about 292 years) converts to a platform-dependent value; on amd64 it is negative, and
the engine's `now.Sub(since) >= window` (`internal/alert/alert.go`) then holds on the first sample, so the debounce
the operator asked for silently becomes none: every spike fires.

## Scenarios

- S1: Given a config file with `alerts.for_seconds` above 604800, when `pimonitor` starts, then it writes
  `pimonitor: load config: alerts.for_seconds must be <= 604800 (got <value>)` to standard error and exits with
  status 1 before any port opens.
- S2: Given `alerts.openhab_for_seconds` above 604800, when `pimonitor` starts, then it fails the same way with
  `alerts.openhab_for_seconds must be <= 604800 (got <value>)`, also while `alerts.enabled` is `false`.
- S3: Given either key at most 604800, when `pimonitor` starts, then it starts and debounces with exactly that
  window, 7 days at 604800.

## Errors and edge cases

- E1: A value above the bound (`alerts.for_seconds: 1e12`) ends startup with
  `pimonitor: load config: alerts.for_seconds must be <= 604800 (got 1e+12)`, exit status 1, before any port opens.
- E2: The same for `alerts.openhab_for_seconds`:
  `pimonitor: load config: alerts.openhab_for_seconds must be <= 604800 (got <value>)`, also while
  `alerts.enabled` or `openhab.enabled` is `false`.
- E3: A value equal to the bound (`604800`) is valid and gives a debounce window of exactly 7 days.
- E4: Negative, NaN and infinite values keep their current messages
  (`… must be >= 0 (got <value>)`, `… must be a finite number`); the finiteness check still runs first.

## Non-functional requirements

- N1: Compatibility: a configuration that is valid today and holds both keys at or below the bound stays valid with
  the same window; only values above the bound, which today are accepted, now fail startup.
- N2: No new configuration option; the bound is fixed in code.

## Acceptance criteria

- AC1: `alerts.for_seconds` above 604800 fails `Config.Validate()` with
  `alerts.for_seconds must be <= 604800 (got <value>)`; `604800` passes and `AlertFor()` returns 168 h (S3, E1, E3, E4,
  N1).
- AC2: `alerts.openhab_for_seconds` above 604800 fails `Config.Validate()` with
  `alerts.openhab_for_seconds must be <= 604800 (got <value>)`, also with `alerts.enabled: false`; `604800` passes and
  `OpenHABAlertFor()` returns 168 h (S2, S3, E2, E3,
  E4, N1).
- AC3: `pimonitor` started with a config file holding `alerts.for_seconds: 1e12` writes the one line of E1 to
  standard error and exits with status 1 (S1, E1, N2).

## Out of scope

- The same unbounded float-to-`time.Duration` conversion of `poll_interval_seconds`, `updates_check_minutes`,
  `updates_stale_threshold_minutes`, `healthz_max_staleness_seconds`, `history_window_minutes`
  (`internal/config/config.go`), `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds` and
  `alerts.webhooks[].timeout_seconds` (`internal/alert/notify.go`): a related defect, proposed as a follow-up issue.
- A bound tied to another option (for example a multiple of `openhab.poll_interval_seconds`).

## Open questions

- Q1: Which upper bound, and one for both keys? Answered: the Product Manager chose "(a) 604800 s (7 days) for both
  keys." — far below the overflow, above `openhab.poll_interval_seconds`' maximum of 86400 so
  `alerts.openhab_for_seconds` can still span several polls, and equal to the `openhab.history_window_hours`
  maximum (168 h).
- Q2: Keep the existing `>= 0` message and add a separate `<=` message, or one `must be between 0 and <bound>` text
  for both? Answered: keep the existing documented negative message unchanged (`docs/areas/configuration.md`) and add
  the `<=` message, so no documented text changes.

## Area documents

- `docs/areas/configuration.md`: the *Valid values* of both keys in the `alerts` table read "0 to 604800; `0` fires
  on the first crossing"; *Validation and startup failure* adds the `alerts.for_seconds must be <= 604800 (got
  <value>)` and `alerts.openhab_for_seconds must be <= 604800 (got <value>)` texts beside the existing `>= 0` ones.
- `packaging/pimonitor.example.yaml` (not an area document): the comments of both keys name the maximum.

## Challenge notes

Devil's Advocate, mode `spec`: /home/user/PiMonitor/specs/feature-272-alert-for-seconds-bound/spec.md

The spec holds up. I checked its factual claims against the code and found them correct:
- `Validate()` checks only the finiteness and `>= 0` of both keys (`internal/config/config.go:453-458`, `:494-495`).
- Both conversions have no upper bound (`config.go:334-342`).
- The finiteness check runs first (`config.go:441`), so E4 holds.
- `openhab.poll_interval_seconds` has a maximum of 86400 (`config.go:221`), so the Q1 rationale holds.
- `%v` of `1e12` prints `1e+12`, so the E1 text is exact.
- No existing test, document or example config holds a value above 604800, so N1 holds.

I have only three minor objections:

1. `minor` — AC3 cannot be checked as written at the test seam the project uses; restate it so it can.
   - The `pimonitor: ` prefix and exit status 1 come from `main()` (`cmd/pimonitor/main.go:26-30`), which no test exercises.
   - The existing pattern, `TestRun_ConfigErrorNamesOption_AC1` (`cmd/pimonitor/main_test.go:346-365`), checks only the error that `run()` returns.
   - Instruction for the Tester: assert that `run([]string{"-config", path})` returns the error `load config: alerts.for_seconds must be <= 604800 (got 1e+12)` and returns before the server starts (the 3 s guard of that test). Leave `main()`'s prefix and exit path as they are, unchanged and untested.

2. `minor` — AC1 and AC2 would pass on a wrong build that uses a looser bound (for example 1e9) if the only failing value tested is `1e12`.
   - Instruction for the Tester: for each key, test a value just above the bound (for example `604801` or `604800.5`, expecting `must be <= 604800 (got 604801)` or `(got 604800.5)`) next to `604800`, which must pass.
   - Also test `alerts.openhab_for_seconds` above the bound with `alerts.enabled: false` and `openhab.enabled: false`, since E2 names both.

3. `minor` — the *Area documents* section says to add the `<=` texts "beside the existing `>= 0` ones", but only one such text exists.
   - `docs/areas/configuration.md:184-186` documents only `alerts.openhab_for_seconds must be >= 0 (got <value>)`, in the openHAB alert options bullet. There is no `alerts.for_seconds` `>= 0` text in *Validation and startup failure*.
   - Instruction for the Lead or Dev: add the `alerts.openhab_for_seconds must be <= 604800 (got <value>)` text to that bullet.
   - Add `alerts.for_seconds must be <= 604800 (got <value>)` as its own bullet in the same section, next to its `>= 0` text, which may be documented there at the same time.
   - Q2's "no documented text changes" still holds.
