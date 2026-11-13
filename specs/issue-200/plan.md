# Plan: Reject non-finite numbers and fractional values for integer options

Source: Issue #200
Tier: standard — a behavior change in config validation (new startup failures) that creates, changes or removes no security control.
Security areas touched: Parsing of local input (the YAML config in `internal/config`)

## Problem / root cause

- Non-finite numbers: yaml.v3 resolves `.nan`/`.inf`/`+.inf`/`-.inf` (three case forms each) to NaN/±Inf
  (`gopkg.in/yaml.v3@v3.0.1/resolve.go:55-58`), and a float64 field takes them as-is (`decode.go:706`). Every check
  in `Config.Validate()` (`internal/config/config.go:308-441`) is a comparison such as `c.PollIntervalSeconds <= 0`
  (line 332), which is false for NaN, so NaN passes every float option; `+Inf` passes every `> 0` / `>= 0` check.
- Fractional integers: for an `int` field yaml.v3 converts a resolved float64 with `int64(resolved)`
  (`decode.go:641-645`), truncating `2.5` to `2`; `Validate()` only ever sees the truncated value
  (`config.go:324`, `config.go:393`).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | `poll_interval_seconds: .nan` is accepted | NaN `<= 0` is false (`config.go:332`); the ratio check `ratio > maxHistoryCapacity` (line 350) is false for NaN | confirmed |
| 2 | ...and "yields a negative interval" | `FastInterval()` is `time.Duration(NaN * 1e9)` (`config.go:215`), an implementation-specific conversion: negative on amd64, not necessarily on arm; either way non-positive or meaningless, and the collector clamps a non-positive interval to 1 s (`docs/areas/metrics.md`, line 29) (the sign is platform-dependent) | confirmed |
| 3 | `processes_top_n: 2.5` becomes `2` | `decode.go:641-645` (`out.SetInt(int64(resolved))`) | confirmed |
| 4 | The integer options are exactly `processes_top_n` and `alerts.notify_max_retries` | the only `int` fields of `Config`/`Alerts`/`Webhook`/`Thresholds` (`config.go:56`, `config.go:158`); every other number option is float64 | confirmed |
| 5 | No command-line flag is numeric (AC4's flag clause is empty) | `load()` defines `-config`, `-listen`, `-log-level`, `-api-key` (strings) and `-version` (bool), `config.go:503-508` | confirmed |
| 6 | Only the literals of fact 1's map yield a non-finite float; an out-of-range literal such as `1e400` does not | `resolve.go:175-178`, `204-209`: a `strconv.ParseFloat` error leaves the scalar a string, which a number field rejects as a type error; `nan`/`inf` without the dot start with a letter that is no float hint and resolve to strings | confirmed |
| 7 | `.nan` and `+.inf` for an `int` field already fail today, inside the decode, with a yaml type error, not the AC1 text; `-.inf` decodes to an implementation-specific integer | `decode.go:642` (`resolved <= math.MaxInt64` is false for NaN and +Inf) | confirmed |
| 8 | A NaN threshold breaks `GET /api/v1/config`: `encoding/json` cannot encode NaN and `writeJSON` only logs the error after the header is set | `internal/httpapi/server.go:67`, `internal/httpapi/handlers.go:13-17` (a consequence AC1 fixes) | confirmed |
| 9 | Error lines read `pimonitor: load config: <reason>`, exit 1 | `cmd/pimonitor/main.go:27-30`, `:36` | confirmed |
| 10 | A second decode of the same bytes with a decoder without `KnownFields` into a struct holding only the two keys resolves aliases, merge keys (`<<`) and explicit tags exactly as the real decode does, and fails only where the real decode fails too (float64 accepts every resolved int, int64, uint64 and float64 that an `int` field accepts, `decode.go:641-645` vs `696-709`) | yaml.v3 source as cited, by reading; the AC2 alias, merge and tag cases exercise it | confirmed |

## Acceptance criteria

- AC1: A NaN or ±Inf value — YAML `.nan`, `.NaN`, `.NAN`, `.inf`, `+.Inf`, `-.INF` (all case and sign forms of
  `resolve.go:55-58` accepted the same way) — for any number or integer option fails `config.Load` with exactly
  `<option> must be a finite number`, where `<option>` is the key path as written in the file
  (`poll_interval_seconds`, `thresholds.cpu_warn_percent`, `alerts.webhooks[0].timeout_seconds`, `processes_top_n`).
  `run()` returns `load config: <option> must be a finite number`; `main()` prints it after `pimonitor: ` and exits 1
  (unchanged path, fact 9).
- AC2: A fractional value for `processes_top_n` or `alerts.notify_max_retries` fails `config.Load` with exactly
  `<option> must be a whole number (got <value>)` (`processes_top_n must be a whole number (got 2.5)`,
  `alerts.notify_max_retries must be a whole number (got 0.5)`) — also when the value arrives through an alias
  (`*n` to `&n 2.5`), a merge key (`alerts: {<<: {notify_max_retries: 1.5}}`) or an explicit `!!float 2.5` tag, and
  also with `-version` (decision record 0008).
- AC3: Whole values are accepted as before: `processes_top_n` written `5`, `5.0`, `1e1` or `!!float 5` loads as
  5, 5, 10, 5; `alerts.notify_max_retries: 3.0` as 3; `poll_interval_seconds` written `5` or `5.0` as 5.
- AC4: The AC1 check covers every number option: `poll_interval_seconds`, `updates_check_minutes`,
  `updates_stale_threshold_minutes`, `history_window_minutes`, `healthz_max_staleness_seconds`, the ten
  `thresholds.*` keys under their real names (`thresholds.temperature_warn_c` … `thresholds.memory_crit_percent`),
  `alerts.for_seconds`, `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds` and
  `alerts.webhooks[<i>].timeout_seconds` for every index; the AC1 and AC2 checks cover both integer options. No flag
  is numeric (fact 5).

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestLoad_RejectsNonFiniteNumbers` (subtests "AC1 AC4 …": each YAML form of AC1 on `poll_interval_seconds`, `thresholds.cpu_warn_percent`, `alerts.webhooks[0].timeout_seconds`, `processes_top_n`, `alerts.notify_max_retries`; exact error text) | `internal/config/config_test.go` |
| AC1 | `TestRun_ConfigErrorNamesOption_AC1` (`run` with a file `poll_interval_seconds: .nan` returns exactly `load config: poll_interval_seconds must be a finite number`) | `cmd/pimonitor/main_test.go` |
| AC2 | `TestLoad_RejectsFractionalIntegerOptions` (subtests "AC2 …": plain, alias, merge key, `!!float` tag, with `-version`; exact error text) | `internal/config/config_test.go` |
| AC3 | `TestLoad_AcceptsWholeNumbersWrittenAsDecimals` (subtests "AC3 …", the forms and values of AC3) | `internal/config/config_test.go` |
| AC4 | `TestValidate_RejectsNonFiniteNumbers` (subtests "AC4 …": every option of AC4 × NaN, +Inf, −Inf set on `Default()`, the webhook at index 1 of two; exact error text) | `internal/config/config_test.go` |

## Approach

1. `internal/config/config.go`, `Validate()`: call a new unexported `validateFinite()` first, before
   `validateTiming()`. It loops over a table of name/value pairs for every float option of AC4 (thresholds under their
   YAML keys, e.g. `thresholds.swap_warn_percent`), then over `Alerts.Webhooks` with the name
   `alerts.webhooks[%d].timeout_seconds`, and returns `fmt.Errorf("%s must be a finite number", name)` for the first
   value where `math.IsNaN(v) || math.IsInf(v, 0)`. Running first means a NaN never reaches a range message, and
   `-Inf` reports "finite", not "must be > 0". A separate function keeps `Validate()`'s cognitive complexity unchanged.
2. `loadYAMLFile()`: after `os.ReadFile`, before the real decode, call a new unexported
   `checkIntegerOptions(data []byte) error`. It decodes `data` with a fresh `yaml.NewDecoder` (no `KnownFields`)
   into a local struct of two `*float64` fields — `processes_top_n` at the top level and `notify_max_retries` inside
   an `alerts` struct. A decode error or `io.EOF` returns nil (the real decode then reports the error, fact 10). For
   each non-nil value: non-finite → `<option> must be a finite number`; `v != math.Trunc(v)` →
   `fmt.Errorf("%s must be a whole number (got %v)", name, v)`. `loadYAMLFile` returns this error unwrapped (no
   `parse <file>:` prefix), so the line reads like a validation message.
3. No other file of production code changes; consumers of the int fields stay as they are.

## Options considered

1. Chosen: approach above.
2. Walk the `yaml.Node` tree for the two keys — must re-implement alias and merge-key resolution; a missed form is
   a bypass.
3. A named integer type with `UnmarshalYAML` for the two fields — changes exported field types read by
   `cmd/pimonitor/main.go:184`, `internal/collector` and `internal/alert/notify.go:148`.
4. `UnmarshalYAML` on `Config` or `Alerts` — `yaml.Node.Decode` (`yaml.go:142-154`) uses a decoder without
   `KnownFields`, so unknown keys there would pass silently.
5. Check the integer options after the `-version` short-circuit, keeping `-version` independent of values — `.nan`
   and `+.inf` for an `int` field fail inside the real decode first (fact 7), so AC1's text could not hold for them.
6. Check finiteness during decoding instead of in `Validate()` — would cover the file only; `Validate()` sees the
   merged result of all sources.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go`: `Validate`, `loadYAMLFile`, new `validateFinite`, `checkIntegerOptions` | finiteness and whole-number checks |
| `internal/config` | `config_test.go` | new tests |
| `cmd/pimonitor` | `main_test.go` | one new test |

## Signatures (for the Dev's skeleton)

None: no exported member is added or changed; the two new functions are unexported and described in *Approach*.

## Test files

`internal/config/config_test.go`, `cmd/pimonitor/main_test.go`.

Existing test code that calls a changed signature: none. Existing tests stay valid: none feeds a NaN, an infinity
or a fractional integer (`TestLoad_VersionFlagSkipsValidation` uses `poll_interval_seconds: 0`).

Files of the change no unit test can cover: none.

## Areas

`docs/areas/configuration.md`: integer and number rules, the `-version` exception, the new messages, *Open
questions* emptied, record 0008 under *Related decisions*.

## Documentation updates

All by the Dev, against the code as built:

- `docs/areas/configuration.md`:
  - *Options*, the "Types:" sentence: a *number* must be finite (`.nan`, `.inf` are rejected); an *integer* may be
    written with a zero fraction (`5.0`, `1e1`) but a fractional value is rejected.
  - *Config file*: a bullet that a fractional or non-finite value for an integer option is an error while the file
    is read.
  - *Command-line flags*, the `-version` bullet: it does not skip that integer check either.
  - *Validation and startup failure*: the finiteness check runs first; add the two message forms as examples.
  - *Related decisions*: replace "None yet." with a link to 0008.
  - *Open questions*: remove both entries; the section reads "None."
- `docs/ARCHITECTURE.md` lines 484-486 (the `-version` paragraph): add that a fractional or non-finite value for an
  integer option still fails, linking record 0008.
- Sweep result, no change needed: `README.md:471` (flag list unchanged), `packaging/pimonitor.example.yaml` (comments
  state ranges only), `docs/areas/metrics.md:29`, `docs/areas/history.md:43`, `docs/areas/notifications.md:181`
  (they point to Configuration), `docs/areas/deployment.md:110,118`. The PR template's checklist asks for no
  changelog; `docs/API.md` is unaffected (no shape change).

## Architecture check

`docs/ARCHITECTURE.md` (config section): validation still runs once after merging, unknown keys are still rejected
(the real decode keeps `KnownFields(true)`), `-version` still skips `Validate()`. The guarantees in
`.squad/project.md` are unaffected; `gopkg.in/yaml.v3` stays the only dependency.

## Security considerations

- [x] No limits: the file is read once (as today) and decoded twice; no new size bound.
- [x] Errors reaching the user: `<option> must be a finite number` and `<option> must be a whole number (got <v>)`,
  both built from fixed option names and a float64 — never from file text, never the `api_key`. A failure of the
  second decode is discarded and never shown; the real decode's error stays as today.

## Decision records

- `docs/decisions/0008-reject-non-finite-and-fractional-config-numbers.md` (Proposed)

## Out of scope / follow-ups

Related defect found: the existing threshold messages name `thresholds.<metric>_warn`/`_crit`
(`config.go:431-437`), not the real keys (`thresholds.cpu_warn_percent`, `thresholds.temperature_warn_c`). Proposed
follow-up issue — title "[Config] Threshold validation errors name keys that do not exist"; body: "`Thresholds.validate`
(`internal/config/config.go`) reports e.g. `thresholds.cpu_warn must be >= 0`, but the key is
`thresholds.cpu_warn_percent` (`temperature_warn_c` for temperature). Name the real YAML key in all three messages and
update `docs/areas/configuration.md` if it quotes one."

## Security notes

Security plan review, round 1. The plan is `/home/user/PiMonitor/specs/issue-200/plan.md` (branch head 673ebfb), with decision record `/home/user/PiMonitor/docs/decisions/0008-reject-non-finite-and-fractional-config-numbers.md`.

**Tier and areas**
- Tier `standard` is correct. The finiteness check and the whole-number check only keep malformed input from breaking the program. They are input handling, not a guard over what an attacker can reach, so no security control is created, changed or removed (`.squad/routing.md`, `security` row).
- *Security areas touched* ("Parsing of local input", the YAML config) is complete:
  - There are no new log calls, so the Logging area is untouched.
  - The `api_key` handling and the `KnownFields(true)` guard against unknown keys in the real decode are unchanged.
  - No dependency, CI or build file changes.

**Bypass enumeration for the second decode**
I read `loadYAMLFile`/`load`/`Validate` in `/home/user/PiMonitor/internal/config/config.go`. I then measured the plan's central claim: the second decode, with no `KnownFields` and `*float64` fields, sees exactly what the real decode sees, and any error it discards also fails the real decode.

Measurement:
- Command: `GOFLAGS=-mod=mod GOPROXY=off go run .`
- Program: `/tmp/claude-0/-home-user-PiMonitor/091a2e5e-3780-5e9e-8eaa-5bad4a980d46/scratchpad/m/main.go`, using gopkg.in/yaml.v3 v3.0.1 from the module cache.
- It decodes each input twice: once into a `Config`-shaped struct (int fields, `KnownFields(true)`) and once into the plan's two-field `*float64` struct.

Excerpt of the output (`real` = the real decode, `2nd` = the plan's second decode):
```
"processes_top_n: 2.5"                     real: N=2 err=false | 2nd: N=2.5 err=<nil>
"processes_top_n: .nan"                    real: err=true      | 2nd: N=NaN err=<nil>
"processes_top_n: +.inf"                   real: err=true      | 2nd: N=+Inf err=<nil>
"processes_top_n: -.INF"                   real: N=-9223372036854775808 err=false | 2nd: N=-Inf err=<nil>
"processes_top_n: !!float 2.5"             real: N=2 err=false | 2nd: N=2.5 err=<nil>
"alerts: {<<: {notify_max_retries: 1.5}}"  real: R=1 err=false | 2nd: R=1.5 err=<nil>
"alerts: {<<: [{notify_max_retries: 1.5}, {notify_max_retries: 2}]}" real: R=1 | 2nd: R=1.5
"alerts: {<<: {notify_max_retries: 1.5}, notify_max_retries: 2}"     real: R=2 | 2nd: R=2
"? processes_top_n\n: 2.5"                 real: N=2 err=false | 2nd: N=2.5
"\"processes_top_n\": 2.5"                 real: N=2 err=false | 2nd: N=2.5
"processes_top_n: 2.5\n---\nprocesses_top_n: 3" real: N=2 | 2nd: N=2.5   (first document only, both)
"﻿processes_top_n: 2.5"               real: N=2 | 2nd: N=2.5
"processes_top_n: 1_0.5"                   real: N=10 | 2nd: N=10.5
"processes_top_n: 1e400" / "nan" / "'2.5'" / "!!int 2.5" / "!foo 2.5" / duplicate key / "2.5\nalerts: [1]"
                                            real: err=true | 2nd: err (discarded -> real decode fails anyway)
"processes_top_n: 18446744073709551615" / "9.3e18"  real: err=true | 2nd: whole -> passes, real decode fails
"poll_interval_seconds: .NaN"              real: P=NaN err=false   (fact 1: reaches Validate today)
```

Forms covered and the outcome:
- **Forms the second decode catches as the plan intends:** plain value, explicit tag, alias, single merge key, merge-key list, an explicit key overriding a merged one, complex `?` key, quoted key, BOM, multiple documents, underscore digits, hex.
- **Forms where the second decode errors:** in every one the real decode also errors, so there is no case where the second decode errors and the real decode succeeds. Discarding the second decode's error therefore lets nothing through.
- **The `-.inf` case:** the real decode silently gives `MinInt64` today, and the planned non-finite check catches it.
- **Facts 1, 3, 6, 7 and 10:** confirmed by this run.
- **Fractional values above 2^53:** a float64 there is always whole, the real decode rounds the same way, and the range check in `Validate()` rejects the result. No bypass.

**Other checks**
- **Error text:** both messages are built from fixed option names plus a float64 formatted with `%v`. No file text and no `api_key` reaches the error. Discarding the second decode's error leaks nothing.
- **Resource use:** the file is read once and parsed twice. yaml.v3's alias-expansion limit applies to both decodes, so the cost stays bounded at twice today's. The file is root-owned local input. No new bound is needed.
- **Other ways a non-finite value could get in:** none. No numeric flag exists (fact 5) and the only environment variable is `PIMONITOR_API_KEY`. `Validate()` runs on the merged config, so `validateFinite()` covers every float source.

**Findings**
- Blocking: none.
- Non-blocking, for the Tester; no plan change needed. Measured: the input `x: &n 2.5\nprocesses_top_n: *n` fails the real decode on the unknown key `x`. For AC2's alias case, put the anchor on a known key (e.g. `processes_top_n: &n 2.5` with `alerts: {notify_max_retries: *n}`), or document that the test relies on `checkIntegerOptions` running before the real decode. Otherwise the test can pass for the wrong reason. Recommendation: fix now, in the test as written.

## Challenge notes

I found one minor gap and no major ones. The plan's factual claims match the code and yaml.v3 v3.0.1, the scope matches issue #200 (there is no numeric flag and no other YAML decode), and the approach is sound. I checked it by reading the code and by running the planned second decode against the real one with yaml.v3 v3.0.1.

**Checks against the plan:**
- **Facts 3, 6 and 7:** confirmed in `$GOMODCACHE/gopkg.in/yaml.v3@v3.0.1/decode.go:621-709` and `resolve.go:55-58`. The `int` case converts a resolved float64 with `int64(resolved)` after only a `<= math.MaxInt64` check, so `processes_top_n: -.inf` decodes to -9223372036854775808 with no error on amd64.
- **Fact 10 (the second decode sees what the real decode sees):** confirmed by experiment. I decoded the same input into a struct with two `*float64` fields (no `KnownFields`) and into a `KnownFields(true)` struct with `int` fields:

  | Input | Second decode | Real decode |
  | ----- | ------------- | ----------- |
  | `processes_top_n: 2.5`, `!!float 2.5`, alias `*n` to `&n 2.5`, `alerts: {<<: {notify_max_retries: 1.5}}`, merge inside an `alerts` block | the fraction | truncated, no error |
  | `!!float 5`, `1e1`, `0x10` | 5, 10, 16 | the same values |
  | duplicate `alerts` key | error | the same error |
  | multi-document input | first document | first document |

  Every case the second decode treats as an error, the real decode also rejects, so discarding that error loses nothing.
- **Order and messages:** `checkIntegerOptions` runs before the real decode, so AC1's text holds for `.nan`/`+.inf` on an `int` field, which would otherwise fail inside the decode (fact 7). Because `loadYAMLFile` (`internal/config/config.go:449-462`) runs before the `-version` short-circuit (`config.go:562`), the plan's -version behaviour (AC2 and the first consequence in record 0008) follows from the code. `validateFinite` running first in `Validate()` keeps NaN away from the ratio check at `config.go:350` and from the `>= 0` messages.
- **Error path (fact 9):** `cmd/pimonitor/main.go:27-30` and `:36` print `pimonitor: load config: <reason>` and exit 1.
- **Fact 8 (NaN breaks `GET /api/v1/config`):** `writeJSON` at `internal/httpapi/handlers.go:13-17` only logs the encode error after the Content-Type header is set.
- **Coverage of options:** the only `int` fields are `config.go:56` and `config.go:158`. The float list in AC4 matches the float64 fields at `config.go:24-33, 46, 59, 66, 88, 95-98, 143`. `load()` defines no numeric flag (`config.go:503-508`).
- **Need and scope:** `docs/areas/configuration.md:184-189` lists both behaviours as open questions, and the issue records the decision to reject them. No accepted record covers them, and nothing outside the issue rides along.

**Objections:**

1. `minor`: Record 0008's consequence ("A new integer option needs its field in the second decode, or a fraction is truncated again unnoticed") is left to memory. Instruction for the Tester: add one guard test in `internal/config/config_test.go`. It walks `Config` with `reflect`, recursing into the `Thresholds`, `Alerts` and `Webhook` structs, collects the `yaml` key path of every `int`-kind field, and asserts that this set equals the set of options `checkIntegerOptions` checks. Today that set is `processes_top_n` and `alerts.notify_max_retries`; state the comparison as set equality, not a hard-coded count. Instruction for the Dev: expose that set (for example an unexported list of option names next to the shadow struct) so the test can read it.
