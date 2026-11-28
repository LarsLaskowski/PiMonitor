# Plan: Upper bound for alerts.for_seconds and alerts.openhab_for_seconds

Source: Issue #272 | [spec.md](spec.md)
Tier: standard — a startup check that keeps an oversized configuration value from breaking the debounce is input
handling, a behavior change that creates no security control.
Security areas touched: Parsing of local input (the YAML config in `internal/config`)

## Problem / root cause

See [spec.md](spec.md), *Problem*. The cause is `AlertFor()` and `OpenHABAlertFor()` (`internal/config/config.go:334-342`),
which convert with no upper bound, while `Validate()` (`config.go:453-458`) checks only `>= 0`.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Both keys are checked only for finiteness and `>= 0` | `config.go:494-495` (`validateFinite`), `config.go:453-458` | confirmed |
| 2 | Both conversions are `time.Duration(x * float64(time.Second))` without a bound | `config.go:335`, `config.go:341` | confirmed |
| 3 | The engine confirms a side when `now.Sub(since) >= cut.window`, so a negative window confirms on the first sample | `internal/alert/alert.go:272` | confirmed |
| 4 | An out-of-range float-to-int conversion is implementation-defined in Go, and amd64 gives a negative value | Go spec, *Conversions*; the comment in `config.go` at the history-capacity check (around line 530) and its regression test `config_test.go:487-495` | confirmed |
| 5 | The finiteness check runs before any range check | `config.go:441` (`validateFinite` first in `Validate`) | confirmed |
| 6 | `%v` of the float64 `1e12` prints `1e+12`, and of `604800.5` prints `604800.5` | `fmt` shortest-representation `%g` for `%v`; Devil's Advocate checked `1e+12` | confirmed |
| 7 | `604800 * float64(time.Second)` is exactly 6.048e14, so `AlertFor()` returns exactly 168 h | 6.048e14 < 2^53, exact in float64 | confirmed |
| 8 | No existing test, document or example config holds either key above 604800 | grep of `for_seconds` and `ForSeconds` across the repository; spec challenge | confirmed |
| 9 | `run()` returns `load config: <reason>` for a validation error, and `main()` adds `pimonitor: ` and exits 1 | `cmd/pimonitor/main.go:26-30`; `TestRun_ConfigErrorNamesOption_AC1` (`cmd/pimonitor/main_test.go:346-365`) | confirmed |
| 10 | The window checks run before `Alerts.validate()` and the openHAB checks, whatever `alerts.enabled` and `openhab.enabled` are | `config.go:453-463` | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestValidate_AlertWindowUpperBound` cases `AC1 for_seconds 604800.5`, `AC1 for_seconds 1e12`, `AC1 for_seconds -1 keeps its text`, `AC1 for_seconds NaN keeps its text`, `AC1 for_seconds 604800 passes`; `TestAlertFor_AtUpperBound_AC1` | `internal/config/config_test.go` |
| AC2 | `TestValidate_AlertWindowUpperBound` cases `AC2 openhab_for_seconds 604800.5`, `AC2 openhab_for_seconds 604800.5 with alerts and openhab disabled`, `AC2 openhab_for_seconds -5 keeps its text`, `AC2 openhab_for_seconds -Inf keeps its text`, `AC2 openhab_for_seconds 604800 passes`; `TestOpenHABAlertFor_AtUpperBound_AC2` | `internal/config/config_test.go` |
| AC3 | `TestRun_AlertWindowAboveBound_AC3` | `cmd/pimonitor/main_test.go` |

The `Validate` cases start from `Default()` and change one field, comparing `err.Error()` with the exact text. The
"disabled" case sets `Alerts.Enabled = false` and `OpenHAB.Enabled` to its `false` value. `TestRun_AlertWindowAboveBound_AC3`
follows `TestRun_ConfigErrorNamesOption_AC1`: a temporary file
`listen_addr: 127.0.0.1:0\nalerts:\n  for_seconds: 1e12\n`, `run` in a goroutine, the exact error
`load config: alerts.for_seconds must be <= 604800 (got 1e+12)`, and a failure if `run` has not returned after 3 s.

## Approach

- In `internal/config/config.go`, add the unexported constant `maxAlertWindowSeconds = 604800`, with a doc comment that
  gives the reason for the bound ([0034](../../docs/decisions/0034-alert-debounce-windows-capped-at-seven-days.md)).
- Add one unexported helper `validateAlertWindow(key string, seconds float64) error`, shared by both keys. It returns
  `fmt.Errorf("%s must be >= 0 (got %v)", key, seconds)` for a negative value, and
  `fmt.Errorf("%s must be <= %d (got %v)", key, maxAlertWindowSeconds, seconds)` above the bound, otherwise `nil`.
- In `Validate()`, replace the two `< 0` checks at lines 453-458 with two calls in the same order and the same
  place, `alerts.for_seconds` first. Finiteness still runs first, and the negative texts are byte-identical. The
  `if err := …; err != nil` form keeps `Validate`'s cognitive complexity unchanged.
- `AlertFor()` and `OpenHABAlertFor()` stay unchanged: once `Validate()` passes, their product is at most 6.048e14 ns.
- Output: only the error text above, one line, unchanged in form from the existing `>= 0` messages.

Early draft PR: no

## Options considered

The bound value was the Product Manager's choice (spec Q1). The options are in
[0034](../../docs/decisions/0034-alert-debounce-windows-capped-at-seven-days.md): 86400 s, the overflow limit only,
and a bound tied to `openhab.poll_interval_seconds`. Two implementation alternatives were weighed:

1. **Clamp in `AlertFor()` / `OpenHABAlertFor()` instead of rejecting** — rejected. It silently changes the
   operator's value, and the issue asks for a validation error that names the key.
2. **One `must be between 0 and 604800` text for both directions** — rejected in spec Q2. It changes a documented
   and tested text (`config_test.go:1306`, `:1427`) for no gain.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go`: `Config.Validate`, new `validateAlertWindow`, `maxAlertWindowSeconds` | bound check |
| `internal/config` | `config_test.go` | new tests |
| `cmd/pimonitor` | `main_test.go` | new test |

## Signatures (for the Dev's skeleton)

No exported API is new or changed. The private constant and helper are described in *Approach*. The skeleton adds the
helper with a body that returns `nil`, and leaves the two existing `< 0` checks in place until step 6, so the
existing negative-value tests stay green and the new upper-bound tests fail.

## Test files

- `internal/config/config_test.go`
- `cmd/pimonitor/main_test.go`

Existing test code that calls a changed signature: none. The existing negative-value cases (`config_test.go:496`,
`:1305-1306`, `:1426-1427`) keep their inputs and texts unchanged.

Files no unit test can cover: none.

## Areas

- `docs/areas/configuration.md` (2792 words, +50 words):
  - the *Valid values* cells of `alerts.for_seconds` and `alerts.openhab_for_seconds` read "0 to 604800; `0` fires on
    the first crossing";
  - in *Validation and startup failure*, the openHAB alert options bullet adds
    `alerts.openhab_for_seconds must be <= 604800 (got <value>)`;
  - a new bullet in the same section states `alerts.for_seconds must be >= 0 (got <value>)` and
    `alerts.for_seconds must be <= 604800 (got <value>)`, both checked also while `alerts.enabled` is `false`, and
    links [0034](../../docs/decisions/0034-alert-debounce-windows-capped-at-seven-days.md).

## Documentation updates

All are owned by the Dev and written once the code is green.

- `docs/areas/configuration.md`: as in *Areas*.
- `packaging/pimonitor.example.yaml`: the comments of `for_seconds` and `openhab_for_seconds` each add "At most
  604800 (7 days)."
- Sweep:
  - `README.md`, `docs/API.md`, `docs/areas/alerts.md` (line 177 points to Configuration), `docs/integrations/openhab.md`
    and `docs/ARCHITECTURE.md` name the keys or the debounce, but none states their range, so they get no change.
  - No other "only these" list or enumeration covers the set of bounded options.
- PR template:
  - Release-notes label: `enhancement`. The Description states that a configuration with either key above 604800 no
    longer starts; whether that also earns `breaking-change` is decided in step 9.
  - Reviewer Notes name the keys `alerts.for_seconds` and `alerts.openhab_for_seconds`; no Pi smoke test is needed
    beyond startup with an out-of-range value.

## Architecture check

The guarantees in `.squad/project.md` are untouched. Startup fails fast on an invalid value, as before, and the
check runs before any port opens or any metric is collected. `docs/ARCHITECTURE.md` needs no update, because no flow
or guarantee changes.

## Security considerations

- [x] No limits on parsed bytes. The new limit is on a decoded numeric value, compared after YAML decoding and after
  the finiteness check.
- [x] The error that reaches the user is the fixed text `<key> must be <= 604800 (got <value>)`, where the value is a
  finite float64 formatted with `%v`. No runtime exception text is added.

## Decision records

- `docs/decisions/0034-alert-debounce-windows-capped-at-seven-days.md` (Proposed)

## Out of scope / follow-ups

Follow-up issue for the orchestrator to create:

- **Title:** Upper bound for the remaining float-seconds config options
- **Body:** The options `poll_interval_seconds`, `updates_check_minutes`, `updates_stale_threshold_minutes`,
  `healthz_max_staleness_seconds` and `history_window_minutes` (`internal/config/config.go:347-395`), and
  `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds` and `alerts.webhooks[].timeout_seconds`
  (`internal/alert/notify.go:121,150,151`), convert float seconds or minutes to `time.Duration` with no upper bound.
  This is the same defect as #272: an out-of-range value gives a platform-dependent duration. Validate each key
  against a bound, with an error naming the key, and document the bound in `docs/areas/configuration.md`.
