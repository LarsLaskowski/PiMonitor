# Plan: Reject non-finite values in /proc load average, uptime and wireless parsers

Source: Issue #267
Tier: standard — each parser gains a new rejection branch (a control-flow change, so not `patch`) that only keeps malformed local input from breaking the JSON encoding: input handling, no security control.
Security areas touched: Parsing of local input (`/proc` parsers in `internal/collector`)

## Problem / root cause

`strconv.ParseFloat` decodes `nan`, `inf` and `infinity` (any case, `inf`/`infinity` with an optional sign) with
a nil error, and the three parsers accept whatever it returns without a finiteness check:
`internal/collector/loadavg.go:19`, `:23`, `:27` (`parseLoadAvg`), `internal/collector/uptime.go:19`
(`parseUptime`) and `internal/collector/wireless.go:51`, `:55` (`parseWireless`). A NaN or ±Inf reaches the
snapshot (`fastTick`, `collector.go:661-691`) and, for the load average, the `load1`/`load5`/`load15` history
(`collector.go:701-703`); `encoding/json` refuses to encode a non-finite float, so `GET /api/v1/metrics` (and
`/api/v1/history` for a load point) fails as described in #265. Hardening only: the kernel writes plain
decimals.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The three parsers call `strconv.ParseFloat` at the lines the issue names, with no finiteness check | `loadavg.go:19-30`, `uptime.go:19-22`, `wireless.go:51-58` read | confirmed |
| 2 | `ParseFloat` accepts `nan`, `NaN`, `inf`, `+Inf`, `-Infinity` with a nil error; a signed NaN (`+nan`, `-nan`) and a trailing dot (`nan.`, the kernel's "updated" marker form) are syntax errors; a decimal or hex overflow returns ±Inf **with** `ErrRange` and is already rejected | Go 1.27.4 `src/internal/strconv/atof.go`: `special()` lines 48-78 (sign falls through to the `inf` case only), `ParseFloat` line 721 (`n != len(s)` → `ErrSyntax`) | confirmed |
| 3 | So the only inputs that reach the parsers' success path non-finite are the unsigned NaN spellings and the (signed) `inf`/`infinity` spellings without trailing dot; each is at most 9 bytes | fact 2 | confirmed |
| 4 | A failed load reading leaves `Load` at `LoadAverage{}` in the snapshot, logs a warning every tick and adds no history point | `collector.go:613-616`, `:667`, `:701-703`; `docs/areas/history.md` *Recording* | confirmed |
| 5 | A failed uptime reading leaves `UptimeSeconds` at 0 and logs a warning every tick | `collector.go:649-652`, `:663` | confirmed |
| 6 | A `parseWireless` error fails the whole reading: nil (field omitted), reported through `wirelessFailures` (logged once) | `wireless.go:51-58`, `collector.go:643-644`, `:691` | confirmed |
| 7 | A NaN `link`/`level` is not mistaken for the null-stats placeholder (`link == 0` is false for NaN), so it reaches the result today | `wireless.go:59`, `:81-83` | confirmed |
| 8 | `GET /metrics` (Prometheus) exports none of these three values, alerts read none of them | `internal/httpapi/prometheus.go:129-141` read, grep of `internal/alert` | confirmed |
| 9 | The only other `ParseFloat` outside these files, `qualityIsZero` (`internal/httpapi/middleware.go:190`), is not affected: NaN/Inf compare unequal to 0, so such a qvalue keeps compressing (no related defect) | read | confirmed |
| 10 | #269 rejects non-finite vcgencmd temperatures with `math.IsNaN(v) \|\| math.IsInf(v, 0)` right after `ParseFloat`, error text "non-finite …" | `temperature.go:254-261` | confirmed |

## Acceptance criteria

- AC1: `parseLoadAvg` returns `LoadAverage{}` and an error when any of its three fields parses as NaN or ±Inf;
  the error text is `parse <name>: non-finite value "<field>"` with `<name>` the existing `load1`/`load5`/`load15`
  (e.g. `parse load5: non-finite value "nan"`).
- AC2: `parseUptime` returns 0 and the error `parse uptime: non-finite value "<field>"` when the first field parses
  as NaN or ±Inf.
- AC3: `parseWireless` returns nil and an error when the `link` or `level` column of any interface line parses as
  NaN or ±Inf, also when another interface's line is valid; the text is
  `parse link quality for "<iface>": non-finite value "<field>"` or
  `parse signal level for "<iface>": non-finite value "<field>"`.
- AC4: a fast tick reading such values from all three files leaves `Load` at `LoadAverage{}`, `UptimeSeconds` at
  0 and `Wireless` nil, records no `load1`/`load5`/`load15` history point, and both the snapshot and the history
  encode as JSON without error.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | TestParseLoadAvg_NonFinite_AC1: table over the field position (load1, load5, load15) × the forms `nan`, `NaN`, `inf`, `+Inf`, `-Infinity`, the other two fields valid; asserts zero result and exact error text | internal/collector/loadavg_test.go |
| AC2 | TestParseUptime_NonFinite_AC2: the same five forms as first field (`<form> 100.00`); asserts 0 and exact error text | internal/collector/uptime_test.go |
| AC3 | TestParseWireless_NonFinite_AC3: cases `nan` in `link`, `-Infinity` in `level`, `Inf` in `link` of `wlan1` after a valid `wlan0` line; asserts nil result and exact error text | internal/collector/wireless_test.go |
| AC4 | TestCollector_FastTick_NonFiniteProcValues_AC4: `newOptionalSourceEnv`, with `c.loadAvg` and `c.uptime` pointed at temp files (`nan 0.50 0.50 1/1 1`, `inf 1.00`) and the wireless file holding a `nan` link; one `e.tick(1)`; asserts the snapshot fields, empty `Load1`/`Load5`/`Load15` in `c.History()`, and nil errors of `json.Marshal` on snapshot and history | internal/collector/collector_test.go |

## Approach

In each parser, directly after each existing `ParseFloat` error check, add the check #269 uses:
`if math.IsNaN(v) || math.IsInf(v, 0)` → return the zero result and
`fmt.Errorf("parse <name>: non-finite value %q", fields[i])` (wireless: the existing `for %q` prefix with `name`).
Five checks: three in `parseLoadAvg`, one in `parseUptime`, two in `parseWireless` (before the null-stats test).
Each file imports `math`. Extend each parser's doc comment by one sentence: a value `ParseFloat` decodes as NaN or
±Inf is rejected, because it would make the JSON encoding of the snapshot fail. `Collect` methods, callers and
logging stay as they are: the new error takes the existing failure path of each reading (facts 4-6).

No new user-visible output beyond the error texts fixed above (they appear in the existing warning log lines).

## Options considered

- **One private helper** (`parseFiniteFloat(s) (float64, error)`) used by all five call sites: lost — it needs a
  home file and its own test file (`one test file per source file`, `docs/UNIT_TESTS.md`) or sits in an unrelated
  parser's file, for a two-condition check; inline checks mirror #269 and keep each parser readable on its own.
- **Skip only the wireless interface with a non-finite value** and keep the others: lost — every other malformed
  `link`/`level` field already fails the whole reading (`wireless.go:51-58`, `TestParseWireless_MalformedLine`),
  and the issue asks to treat the value as unparsable input; a second rule for one kind of garbage would make
  the malformed-file behavior depend on which garbage it is.
- **Clamp to 0** instead of failing: lost — fabricates a reading, against "No reading is invented"
  (`docs/areas/metrics.md`, *Limits and errors*), and records a false 0 load point in the history.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/collector | loadavg.go (`parseLoadAvg`) | three finiteness checks, doc comment |
| internal/collector | uptime.go (`parseUptime`) | one finiteness check, doc comment |
| internal/collector | wireless.go (`parseWireless`) | two finiteness checks, doc comment |

## Signatures (for the Dev's skeleton)

None: no new or changed API member. The three private parsers keep their signatures; the skeleton step has
nothing to add.

## Test files

internal/collector/loadavg_test.go, internal/collector/uptime_test.go, internal/collector/wireless_test.go,
internal/collector/collector_test.go.

Existing test code that calls a changed signature: none. Existing tests kept unchanged: `TestParseLoadAvg_Malformed`,
`TestParseUptime_Malformed`, `TestParseWireless_MalformedLine` — none of their inputs is a non-finite spelling.

Files no unit test can cover: none.

## Areas

`docs/areas/metrics.md` and `docs/areas/history.md`: a non-finite value is a failed reading (see below).

## Documentation updates

All by the Dev, once the code is green:

- `docs/areas/metrics.md`: the `load_average` bullet (*CPU and load*), the `wireless` bullet (*Network and
  wireless*) and the `uptime_seconds` bullet (*System identity and uptime*) each say that a value that is not a
  finite number (`nan`, `inf`) fails that tick's reading; under *Limits and errors* the `wireless` parenthesis
  ("a missing, unreadable or malformed `/proc/net/wireless`") names a non-finite value as malformed.
- `docs/areas/history.md`, *Recording*: "`load1`, `load5` and `load15` when the load average cannot" → also when
  it holds a value that is not a finite number.
- `docs/API.md`, `GET /api/v1/metrics` *Notes*: the `uptime_seconds` note says it reads 0 after a failed
  `/proc/uptime` read (a non-finite value included); a new note in the style of the `/proc/meminfo` one says
  every `load_average` figure reads 0 after a failed `/proc/loadavg` read (a non-finite value included), in v1
  indistinguishable from a real 0; the `wireless` note adds that the field is also omitted when
  `/proc/net/wireless` cannot be read or holds a malformed or non-finite value. Presence rules of keys do not
  change (no `v2`).
- `docs/ARCHITECTURE.md`, `README.md`: none (sweep: no statement on parser input validity of these files).
  No changelog file exists in the repository.

## Architecture check

Preserves "a missing source yields an empty/zero value and a logged warning, never a fabricated reading" and the
v1 shape: keys stay present (`load_average`, `uptime_seconds`) or omitted (`wireless`) under their documented
rules. No guarantee of `.squad/project.md` is weakened.

## Security considerations

- [x] No limits: the error quotes a field that, to reach the new branch, is one of the special spellings of at
  most 9 ASCII bytes (facts 2-3), so the log line gains no unbounded input.
- [x] Exceptions reaching a user-visible message: none — the error reaches only the existing warning log lines;
  the API serves the zero/omitted value.

## Decision records

- none: no decision beyond the obvious fix, following #269.

## Out of scope / follow-ups

- Other `/proc` and `/sys` parsers decode integers (`ParseUint`/`ParseInt`), which have no non-finite values.
