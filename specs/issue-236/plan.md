# Plan: Threshold validation errors name keys that do not exist

Source: Issue #236
Tier: trivial — the change rewrites the wording of three error messages without changing behavior or control flow; with the tests it needs it exceeds the `patch` line budget (about 18 production lines plus 30 or more test lines).
Security areas touched: none

## Problem / root cause

`Thresholds.validate` (`internal/config/config.go:467-490`) builds each threshold key from a short stem and a
hard-coded suffix: the table rows carry `name` = `temperature`, `cpu`, `disk`, `swap`, `memory` (lines 472-476),
and the three messages print `thresholds.%s_warn` / `thresholds.%s_crit` (lines 480, 483) and
`thresholds.%s_warn (%v) must be <= %s_crit (%v)` (line 486). The YAML keys (struct tags, lines 25-34) carry a unit
suffix (`_c` for temperature, `_percent` for the other four), so every threshold message names a key that does not
exist. The third message also drops the `thresholds.` prefix from the crit key.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The messages report e.g. `thresholds.cpu_warn must be >= 0` | `internal/config/config.go:480` with row `{"cpu", …}` at line 473 | confirmed |
| 2 | The real key is `thresholds.cpu_warn_percent`, and `temperature_warn_c` for temperature | yaml tags at `internal/config/config.go:25-34` | confirmed |
| 3 | There are three such messages | `internal/config/config.go:480, 483, 486` | confirmed |
| 4 | The defect affects all five pairs, not only cpu and temperature | rows at lines 472-476; every yaml tag has a unit suffix | confirmed |
| 5 | No document quotes the wrong keys or these messages | grep for `_warn must`, `_crit must`, `must be <=` in `*.md`, `*.go` outside `specs/`: only `config.go` matches; `docs/areas/configuration.md` already says "The reason names the option" | confirmed |
| 6 | No test compares these messages (`TestValidate_RejectsBadValues` checks only `err != nil`), and none sets a negative crit | `internal/config/config_test.go:459-518` | confirmed |
| 7 | Every other message of `Validate` names its real key | its `Errorf` calls against the yaml tags | confirmed |

## Acceptance criteria

Defaults used below (`Default()`, `internal/config/config.go:182-193`): crit 75 (temperature), 95 (cpu, disk,
memory), 90 (swap). `%v` of a whole float prints without a decimal point.

- AC1: A negative warn value fails `Validate()` with exactly `thresholds.<warn key> must be >= 0 (got -1)`, for each
  of the five warn keys `temperature_warn_c`, `cpu_warn_percent`, `disk_warn_percent`, `swap_warn_percent`,
  `memory_warn_percent` (e.g. `thresholds.cpu_warn_percent must be >= 0 (got -1)`).
- AC2: A negative crit value, with its warn value at the default, fails with exactly
  `thresholds.<crit key> must be >= 0 (got -1)`, for each of the five crit keys (e.g.
  `thresholds.temperature_crit_c must be >= 0 (got -1)`).
- AC3: A warn value of 1000 with the crit value at its default fails with exactly
  `thresholds.<warn key> (1000) must be <= thresholds.<crit key> (<default crit>)`, for each of the five pairs (e.g.
  `thresholds.disk_warn_percent (1000) must be <= thresholds.disk_crit_percent (95)`).

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestValidate_ThresholdErrorsNameYAMLKey`, subtests `AC1 <warn key>` (five) | `internal/config/config_test.go` |
| AC2 | `TestValidate_ThresholdErrorsNameYAMLKey`, subtests `AC2 <crit key>` (five) | `internal/config/config_test.go` |
| AC3 | `TestValidate_ThresholdErrorsNameYAMLKey`, subtests `AC3 <warn key>` (five) | `internal/config/config_test.go` |

One table per pair (warn key, crit key, setters), expected strings typed in the test, never derived from the
production table. Written by the Tester in step 6 (tier `trivial` skips tests-first).

## Approach

In `Thresholds.validate` replace the row field `name string` by `warnKey, critKey string` holding the full YAML key
without the `thresholds.` prefix (`temperature_warn_c` / `temperature_crit_c`, `cpu_warn_percent` /
`cpu_crit_percent`, and so on for disk, swap, memory — each copied from the struct tag), and the three formats by
`thresholds.%s must be >= 0 (got %v)` (warn key, then crit key) and
`thresholds.%s (%v) must be <= thresholds.%s (%v)`. Order and conditions of the checks stay as they are. The
message layout is fixed by the criteria.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `Thresholds.validate`, `internal/config/config.go` | row fields and three message formats |
| `internal/config` | `internal/config/config_test.go` | new test `TestValidate_ThresholdErrorsNameYAMLKey` |

## Signatures (for the Dev's skeleton)

None: the only changed member is the unexported method Thresholds.validate, whose signature stays the same.

## Test files

- `internal/config/config_test.go` (existing, colocated).

Existing test code that calls a changed signature: none; `TestValidate_RejectsBadValues` stays unchanged (it asserts
only that an error occurs).

Files of the change no unit test can cover: none.

## Areas

none: `docs/areas/configuration.md` already states that a failure reason names the option and lists the real keys;
the fix makes the code match it.

## Documentation updates

none (fact 5); no changelog exists, and no option is added or changed.

## Architecture check

No guarantee of `docs/ARCHITECTURE.md` or `.squad/project.md` is touched: the same values are rejected in the same order; only
three message texts change, and they quote no secret.

## Security considerations

- [x] no limits.
- [x] The three changed messages reach the user as the startup failure reason
  (`pimonitor: load config: <message>`), with the texts of AC1-AC3; no other exception changes.

## Decision records

- none: no decision beyond the obvious fix.

## Out of scope / follow-ups

- none.
