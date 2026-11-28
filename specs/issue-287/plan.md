# Plan: Upper bound for the remaining float-seconds config options

Source: Issue #287
Tier: standard — a behavior change in startup validation (configurations accepted today are rejected), an input bound that creates or removes no security control.
Security areas touched: Parsing of local input (the YAML config in `internal/config`)

## Problem / root cause

Eight number options are multiplied into a `time.Duration` with only a lower bound checked:
`FastInterval` (`internal/config/config.go:347`), `SlowInterval` (352), `UpdatesStaleThreshold` (358),
`HealthzMaxStaleness` (386), `HistoryWindow` (395) and, in `NewNotifier`, the webhook timeout, retry backoff and rate-limit
window (`internal/alert/notify.go:121,150,151`). `validateTiming` (config.go:535), `validateServer` (562) and
`Alerts.validate` (596) check only `> 0` or `>= 0`, so a value whose product passes `math.MaxInt64` nanoseconds
converts to a platform-dependent duration — the defect fixed for the debounce windows in #288
([0034](../../docs/decisions/0034-alert-debounce-windows-capped-at-seven-days.md)).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The issue's eight conversions and line numbers are right; no other float-to-`Duration` conversion exists outside the two debounce windows. | `grep "float64(time\."` over non-test Go files: config.go:335,341,347,352,358,386,395, notify.go:121,150,151. | confirmed |
| 2 | An out-of-range float-to-int64 conversion is platform-dependent (amd64 gives `math.MinInt64`, arm64 saturates to `math.MaxInt64`). | Comment in `validateTiming` (config.go:546-551); decision 0034, *Context*. | confirmed |
| 3 | What a wrapped (negative) value does today: a tick interval is clamped to 1 s with a warning (`clampInterval`, collector.go:421); the stale flag never fires (`staleThreshold > 0`, updates.go:141); `/healthz` never reports stale (`maxStale > 0`, handlers.go:33); restored history is never trimmed (`HistoryWindow > 0`, persist.go:450); a timeout falls back to 10 s (notify.go:124); a backoff sleeps 0 (`sleepCtx`, notify.go:419); the rate limit never applies (`>= n.minInterval`, notify.go:283). On arm64 (the Pi) the saturated value waits about 292 years instead. | Read at the lines named. | confirmed |
| 4 | `NewNotifier` only ever gets a validated configuration, so a check in `Config.Validate` covers the three notifier options; `notify.go` needs no change. | Its only caller, cmd/pimonitor/main.go:46, runs after `config.Load` (main.go:34), which calls `Validate` (config.go `load`). | confirmed |
| 5 | Every duration at the planned bounds fits `time.Duration`: 604800 s = 6.05e14 ns, the derived `/healthz` default 3 × 604800 s + 34 s = 1.8e15 ns, 365 days = 3.15e16 ns, all far below 9.22e18. `fastInterval × HistoryCapacity` (collector.go:815) is at most max(window, poll interval), since the capacity is ⌊window × 60 / poll⌋, at least 1. | Arithmetic; config.go:383-389, 400-410. | confirmed |
| 6 | The doubling backoff (`backoff *= 2`, notify.go:332, with `notify_max_retries` unbounded) wraps at 604800 s after 14 doublings, i.e. only after more than 300 years of cumulative sleep: no reachable defect. | 6.05e14 × 2^14 > 9.22e18; the sleeps before sum to 604800 s × (2^14 − 1). | confirmed |
| 7 | The ratio check (`history_window_minutes × 60 / poll_interval_seconds ≤ 1,000,000`, config.go:552) stays needed: the poll interval has no lower bound above 0, so 525600 / 1e-300 still gives 3.15e307. At the default poll interval of 5 s the ratio already caps the window at 83333 min, so 525600 min is reachable only with a poll interval ≥ 31.536 s. | Arithmetic on config.go:552. | confirmed |
| 8 | The dashboard passes `Math.max(1, poll_interval_seconds) * 1000` to `setInterval` (app.js:1604-1609); browsers store the delay as a signed 32-bit integer and fire at once above 2147483647 ms (24.8 days). The planned 604800 s bound gives 6.05e8 ms, below the limit whatever browsers do above it; only the record's *Context* sentence rests on the browser behavior. | to verify: the Reviewer reads MDN `setTimeout`, *Maximum delay value*, in step 8, for the browser behavior (the app.js lines are read). | unverified |
| 9 | `%v` renders `1e12` as `1e+12` and `604800.5` as `604800.5` in the error text. | `TestValidate_AlertWindowUpperBound` (config_test.go:1462) passes with those texts on `main`. | confirmed |
| 10 | Two existing tests rely on values above the new bounds: `TestValidate_HistoryCapacityBoundary` (config_test.go:609, window 1,000,000 min must pass) and the row "history capacity ratio overflows int64" of `TestValidate_RejectsBadValues` (config_test.go:487, window 1e300, which would now stop at the upper bound and no longer reach the ratio check it guards). | Read. | confirmed |
| 11 | Decision 0034 is unreleased and may be edited in place. | `git ls-remote --tags origin`: newest tag v1.3.0 (d4b02b9); `git merge-base --is-ancestor 44d746d d4b02b9` fails. | confirmed |

## Acceptance criteria

Each error text below is compared exactly; `<v>` is the value as `%v` prints it. Each upper-bound check runs right after
the finiteness check, so it wins over every other range check; values ≤ the bound, and the existing lower-bound and
finiteness texts, are unchanged.

- AC1: `poll_interval_seconds` above 604800 fails with `poll_interval_seconds must be <= 604800 (got <v>)`; 604800
  passes (with the history window below the ratio cap).
- AC2: `updates_check_minutes` above 10080 fails with `updates_check_minutes must be <= 10080 (got <v>)`; 10080 passes.
- AC3: `updates_stale_threshold_minutes` above 525600 fails with
  `updates_stale_threshold_minutes must be <= 525600 (got <v>)`; 525600 passes.
- AC4: `healthz_max_staleness_seconds` above 31536000 fails with
  `healthz_max_staleness_seconds must be <= 31536000 (got <v>)`; 31536000 passes.
- AC5: `history_window_minutes` above 525600 fails with `history_window_minutes must be <= 525600 (got <v>)`, also
  when the ratio to `poll_interval_seconds` exceeds 1,000,000 as well (`1e12` at the default poll interval names the
  key, not the ratio); 525600 with `poll_interval_seconds: 60` passes.
- AC6: `alerts.notify_retry_backoff_seconds` above 604800 fails with
  `alerts.notify_retry_backoff_seconds must be <= 604800 (got <v>)`, also while `alerts.enabled` is `false`; 604800
  passes.
- AC7: `alerts.notify_min_interval_seconds` above 604800 fails with
  `alerts.notify_min_interval_seconds must be <= 604800 (got <v>)`, also while `alerts.enabled` is `false`; 604800
  passes.
- AC8: `alerts.webhooks[<i>].timeout_seconds` above 604800 fails with
  `alerts.webhooks[<i>].timeout_seconds must be <= 604800 (got <v>)`, `<i>` the index of the first such webhook (the
  second of two gives `[1]`), also while `alerts.enabled` is `false`; 604800 passes.
- AC9: started with such a config file, the binary returns before it serves, with
  `load config: history_window_minutes must be <= 525600 (got 1e+12)` for `history_window_minutes: 1e12`.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestValidate_DurationUpperBounds`: "AC1 poll_interval_seconds 604800.5", "AC1 … 1e12", "AC1 … 604800 passes", "AC1 … 0 keeps its text" | `internal/config/config_test.go` |
| AC2 | same test: "AC2 updates_check_minutes 10080.5", "AC2 … 10080 passes", "AC2 … 0 keeps its text" | `internal/config/config_test.go` |
| AC3 | same test: "AC3 updates_stale_threshold_minutes 525600.5", "AC3 … 525600 passes", "AC3 … -1 keeps its text" | `internal/config/config_test.go` |
| AC4 | same test: "AC4 healthz_max_staleness_seconds 31536000.5", "AC4 … 31536000 passes", "AC4 … 2 below poll interval keeps its text" | `internal/config/config_test.go` |
| AC5 | same test: "AC5 history_window_minutes 525600.5 with poll 60", "AC5 … 1e12 at poll 5 names the key", "AC5 … 525600 with poll 60 passes", "AC5 … NaN keeps its text" | `internal/config/config_test.go` |
| AC6 | same test: "AC6 notify_retry_backoff_seconds 604800.5", "AC6 … with alerts disabled", "AC6 … 604800 passes", "AC6 … -1 keeps its text" | `internal/config/config_test.go` |
| AC7 | same test: "AC7 notify_min_interval_seconds 604800.5", "AC7 … with alerts disabled", "AC7 … 604800 passes" | `internal/config/config_test.go` |
| AC8 | same test: "AC8 second webhook timeout 604800.5 names index 1", "AC8 … with alerts disabled", "AC8 … 604800 passes", "AC8 … -1 keeps its text" | `internal/config/config_test.go` |
| AC9 | `TestRun_DurationOptionAboveBound_AC9` (pattern of `TestRun_AlertWindowAboveBound_AC3`) | `cmd/pimonitor/main_test.go` |

`TestValidate_DurationUpperBounds` is one table as `TestValidate_AlertWindowUpperBound`: `Default()`, a mutation, the
exact error text or `""` for a pass.

## Approach

Early draft PR: no

In `internal/config/config.go`:

- Constants beside `maxAlertWindowSeconds`, each with a comment naming decision 0035: `maxWaitSeconds` (defined as
  `maxAlertWindowSeconds`, 7 days), `maxWaitMinutes = 10080`, `maxAgeSeconds = 31536000` and `maxAgeMinutes = 525600`
  (365 days).
- A helper `atMost(key string, value float64, limit int) error`: nil when `value <= float64(limit)`, else
  `fmt.Errorf("%s must be <= %d (got %v)", key, limit, value)`. `validateAlertWindow` uses it for its upper check, so
  the format exists once (its texts stay as they are).
- A method `func (c Config) validateUpperBounds() error`, a table in the style of `validateFinite`: the seven top-level
  and `alerts.*` keys with their value and limit (poll interval, updates check, backoff, min interval: the wait limits;
  stale threshold, healthz, history window: the age limits), then a loop over `c.Alerts.Webhooks` naming
  `alerts.webhooks[%d].timeout_seconds` with `maxWaitSeconds`. First failure returns.
- `Validate` calls it directly after `validateFinite`, before `validateTiming`. A value cannot fail both a lower and an
  upper bound, so every existing lower-bound text and the order among them stay unchanged.
- `validateTiming`, `validateServer`, `Alerts.validate`, the accessors and `internal/alert/notify.go` are unchanged
  (fact 4). No new user-visible output besides the error texts the criteria fix.

The PR carries the `breaking-change` label, as #288: a configuration above a bound no longer starts.

## Options considered

The bounds themselves — two by kind (chosen), 7 days for all eight, a realistic bound per key, the overflow limit only
— are weighed in [0035](../../docs/decisions/0035-float-duration-options-capped-at-seven-or-365-days.md).

Where the check sits:

1. **One table method after `validateFinite`** — chosen: one place, no extra branches in the three existing
   validators (`gocognit`), and the key-naming bound wins over the ratio check (AC5).
2. **Each upper check beside its lower check** — keeps per-key checks together, but adds a branch to each validator,
   and the history window's bound would have to precede the ratio check inside `validateTiming` anyway.
3. **Fold the two debounce windows into the new table and drop `validateAlertWindow`** — one mechanism, but rewrites
   code merged hours ago for no behavior change and moves the debounce checks ahead of the timing checks; the shared
   `atMost` already gives one error format.
4. **Clamp in the accessors and `NewNotifier`** — a silently different value instead of a startup failure, against
   *fail fast at startup* (`docs/ARCHITECTURE.md`, *Configuration*) and the issue.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go` | four constants, `atMost`, `validateUpperBounds`, one call in `Validate`, `validateAlertWindow` uses `atMost` |
| `internal/config` | `config_test.go` | new table test; two existing tests adapted |
| `cmd/pimonitor` | `main_test.go` | new run test |
| `packaging` | `pimonitor.example.yaml` | bounds in eight comments |

## Signatures (for the Dev's skeleton)

None: the change adds only unexported members of `internal/config` (described in *Approach*); no skeleton step.

## Test files

- `internal/config/config_test.go`
- `cmd/pimonitor/main_test.go`

Existing test code: no signature changes. Two existing tests use values above the new bounds (fact 10); the
**Tester** adapts them in step 5:

- `TestValidate_HistoryCapacityBoundary`: `PollIntervalSeconds = 30` with `HistoryWindowMinutes = 500000` (exactly
  1,000,000 points, passes) and `500000.5` (1,000,001, fails), so the boundary stays under the new window bound.
- `TestValidate_RejectsBadValues`, row "history capacity ratio overflows int64": `HistoryWindowMinutes = 525600`,
  `PollIntervalSeconds = 1e-300`, so it still reaches the float ratio check it guards.

Every other existing test keeps values within the bounds. Files no unit test can cover: none.

## Areas

- `docs/areas/configuration.md` (2837 words, +170 words): the *Valid values* of the eight keys gain their upper
  bound; *Validation and startup failure* gains one bullet with the text `<key> must be <= <bound> (got <value>)`, the
  bounds by key, that it is checked right after finiteness and also while `alerts.enabled` is `false`, and a link to
  0035; *Related decisions* gains 0034 and 0035.
- `docs/areas/notifications.md` (2138 words, +3 words): *Limits and errors* "a non-negative `timeout_seconds`" becomes
  "a `timeout_seconds` from 0 to 604800".

## Documentation updates

All by the Dev, once the code is green:

- `docs/areas/configuration.md` and `docs/areas/notifications.md`: as *Areas*. Sweep result: `history.md`,
  `metrics.md`, `dashboard.md` and `http-api.md` only point to Configuration for ranges; `docs/API.md` and
  `docs/ARCHITECTURE.md` state no range of these keys (ARCHITECTURE's *Configuration* paragraph stays true);
  `README.md` has no option table.
- `packaging/pimonitor.example.yaml`: the comment of each key ends with its bound in the style of #288 — "At most
  604800 (7 days)." for `poll_interval_seconds`, `notify_retry_backoff_seconds`, `notify_min_interval_seconds` and the
  commented-out `timeout_seconds`; "At most 10080 (7 days)." for `updates_check_minutes`; "At most 525600 (365 days)."
  for `updates_stale_threshold_minutes` and `history_window_minutes`; "At most 31536000 (365 days)." for
  `healthz_max_staleness_seconds`.
- Decision records: 0035 (new) and the in-place edit of 0034's consequence are the Lead's, done.

## Architecture check

*Fail fast at startup* (`docs/ARCHITECTURE.md`, *Configuration*): strengthened — an option that converted to a
wrong duration now stops startup with a text naming the key. No guarantee of `.squad/project.md` is weakened; no
`/api/v1` response changes (`GET /api/v1/config` reports the same values).

## Security considerations

- [x] No byte or length limits on parsed input; the bounds compare decoded `float64` values after the finiteness check.
- [x] Errors that reach the user: only the fixed texts of AC1–AC8, prefixed `load config: ` on standard error; they
  quote the offending number, never a secret option.

## Decision records

- `docs/decisions/0035-float-duration-options-capped-at-seven-or-365-days.md` (Proposed), indexed; 0034 (unreleased,
  fact 11) edited in place to point to it.

## Out of scope / follow-ups

- Proposed issue — title: "Lower bound for poll_interval_seconds and updates_check_minutes"; body: a positive value
  below 1 ns truncates to a zero `time.Duration`, which `clampInterval` silently replaces by 1 s, and values such as
  `0.001` run the fast ticker every millisecond; decide a minimum interval with an error text naming the key.
