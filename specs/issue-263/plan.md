# Plan: Drop non-finite points when decoding history.bin

Source: Issue #263
Tier: standard — the decoder of `history.bin` changes behavior (it drops points it kept before), and that check only keeps malformed input from breaking the program: input handling, no security control (`.squad/routing.md`, *Tiers*).
Security areas touched: File writes: `<data_dir>/history.bin` (atomic write, bounded decode) in `internal/collector/persist.go`

## Problem / root cause

`decodeHistoryPoints` (`internal/collector/persist.go:292-299`) keeps every point whatever its fields hold: the
value bits become a float64 with `math.Float64frombits` (line 297), the timestamp any int64 with `time.UnixMilli`
(lines 295, 297). A point with a NaN or ±Inf value, or a timestamp whose year lies outside 0–9999, restored by
`loadHistory` (line 427) reaches `json.Marshal` in `cachedHistory` (`internal/httpapi/handlers.go:113`) and in
`historyPayload` (line 88, `?since=`), which fail; `handleHistory` then answers 500 "internal error" (lines
68-72) on every request until the point leaves the ring buffer — a far-future timestamp never leaves the window.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The decoder keeps any value bits and any timestamp, for every series kind | `persist.go:292-299`: one loop for all kinds, no check; `decodeSeries` (line 289) calls it for every kind | confirmed |
| 2 | `json.Marshal` fails on a NaN or ±Inf `float64` | `encoding/json` docs: "NaN and +/-Inf values will return an UnsupportedValueError"; `HistoryPoint.Value` is a plain `float64` (`types.go:264-267`), no custom marshaller (grep `MarshalJSON` in `internal/collector`: none) | confirmed |
| 3 | `json.Marshal` fails on a `time.Time` whose year, in its own location, is outside 0–9999 | `$(go env GOROOT)/src/time/format_rfc3339.go:62-80` `appendStrictRFC3339`: formats in `t`'s location, errors "year outside of range [0,9999]" unless the year is four digits; `time.go:1591` `MarshalJSON` calls it; Security's run: `math.MaxInt64` ms and year 10000 fail | confirmed |
| 4 | Restored timestamps carry the local location | `time.UnixMilli` returns local time (Go docs); the guard therefore checks `Year()` of that value, the year `MarshalJSON` formats | confirmed |
| 5 | The failure fails the whole history response, full window and `?since=` | `handlers.go:68-72`, `88`, `113-118` | confirmed |
| 6 | Producing such a file needs write access to `data_dir` | `history.md` *Persistence*; `deployment.md` line 103: only the service user writes there | confirmed |
| 7 | The openHAB window of #183 keeps a bad point up to 168 h; a far-future point is never trimmed | `importHistory` drops only points before the cutoff (`!p.Timestamp.Before(cutoff)`, `persist.go:447`, `481`) | confirmed |
| 8 | `encodeHistory` writes any value and timestamp, so a test can build the file with it | `persist.go` `encodeHistory`: `math.Float64bits`, `UnixMilli`, no check | confirmed |
| 9 | Live recording of the history series takes finite values and current timestamps | thermal zone via `strconv.ParseInt` (`temperature.go:69`); `usagePercent` guards a zero delta (`cpu.go:72-85`); rates divide only by `elapsed > 0` (`network.go:97`, `diskio.go:128`); `/proc/loadavg` is kernel-formatted; timestamps from the tick clock | confirmed |
| 10 | A per-device series left without points is not restored; a scalar one restores empty | `importHistory` `importMap` skips `len(points) == 0`; scalar `Fill` takes the slice as is | confirmed |

The issue's claims are confirmed; Security found the timestamp case (fact 3), now in scope. Related defect:
`parseVcgencmdTemp` (`temperature.go:251`) accepts `nan`/`inf` through `strconv.ParseFloat`, which would fail
`GET /api/v1/metrics` (snapshot, not history): follow-up issue (*Out of scope*).

## Acceptance criteria

- AC1: `decodeHistoryPoints` drops every point whose value bits are a NaN (exponent all ones, mantissa non-zero, either sign — quiet, signalling, any payload) or ±Inf, or whose timestamp, as `time.UnixMilli` decodes it, has a year outside 0–9999; it keeps every other point with its timestamp and value, in file order. Finite edge values (±`math.MaxFloat64`, the smallest subnormal, -0) and the first and last millisecond of years 0 and 9999 are kept.
- AC2: for every series kind of the format (the seven scalar kinds, the five per-device kinds with a key, the three openHAB kinds), `decodeHistory` of a file whose series holds valid points around a NaN, a +Inf, a -Inf point and a point of year 10000 returns that series with exactly the valid points, and no error.
- AC3: a collector that restores such a file with `loadHistory` holds exactly the valid points in each restored series, `json.Marshal` of its `History()` succeeds, and a per-device series whose points were all dropped is not restored.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestDecodeHistoryPoints_DropsUnencodablePoints_AC1` (table: each dropped value bit pattern and each dropped timestamp between two valid points; rows of kept edge values and boundary timestamps) | `internal/collector/persist_test.go` |
| AC2 | `TestDecodeHistory_DropsUnencodablePointsPerKind_AC2` (table over the 15 kinds, one `t.Run` per kind) | `internal/collector/persist_test.go` |
| AC3 | `TestCollector_LoadHistory_DropsUnencodablePoints_AC3` | `internal/collector/persist_test.go` |

AC1 builds `raw` with `binary.LittleEndian.AppendUint64`. Dropped values: `0x7FF8000000000001`,
`0x7FF0000000000001`, `0xFFF8000000000000`, `0x7FFFFFFFFFFFFFFF`, `0x7FF0000000000000`, `0xFFF0000000000000`; kept:
`0x7FEFFFFFFFFFFFFF`, `0xFFEFFFFFFFFFFFFF`, `0x0000000000000001`, `0x8000000000000000`, compared by bits. Timestamps
are computed in `time.Local`, the location `time.UnixMilli` decodes into (fact 4), so the boundaries hold in any
zone: dropped `math.MaxInt64`, `math.MinInt64`, `time.Date(10000, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()` and
`time.Date(0, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli() - 1`; kept, one millisecond inside each:
`time.Date(0, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()` and `time.Date(10000, 1, 1, 0, 0, 0, 0,
time.Local).UnixMilli() - 1`. AC2 and AC3 write the file
with `encodeHistory` / `writeHistoryFile` from `math.NaN()`, `math.Inf(±1)` and `time.Date(10000, 1, 1, …)`.

AC3 (Challenge 1): `newOHPersistCollector`, every valid point inside both the history window and
`openhab.history_window_hours`; one per-device series of only dropped points, a second per-device series with
valid points around dropped ones, and the scalar and openHAB series as in AC2; the test asserts each restored
series of `History()` equals exactly its valid points, then that `json.Marshal` succeeds.

## Approach

In `decodeHistoryPoints`, allocate `make([]HistoryPoint, 0, n)` (the bound stays `n`, checked before) and append
a point only when `!math.IsNaN(v) && !math.IsInf(v, 0)` and the year of `time.UnixMilli(ms)` is within 0–9999;
signature unchanged. `math.IsNaN` and `math.IsInf(v, 0)` cover every bit pattern with an all-ones exponent; the
year check is the one `MarshalJSON` applies, on the same value. No log line (record 0021). The file stays valid, the
limits and their order stay.

## Options considered

Listed with why they lost in [0021](../../docs/decisions/0021-non-finite-history-point-dropped-on-restore.md):
reject the whole file as invalid; filter at serve time in `internal/httpapi`; replace the value with 0 or the
previous value; leave the timestamp to a follow-up issue.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `persist.go`, `decodeHistoryPoints` | skip unencodable points |
| `internal/collector` | `persist_test.go` | three tests |
| `docs` | `areas/history.md`, `decisions/0021-…` | rule, record |

## Signatures (for the Dev's skeleton)

None: no new or changed API member. The private function decodeHistoryPoints keeps its signature; no skeleton
file is rewritten.

## Test files

- `internal/collector/persist_test.go`

Existing test code that calls a changed signature: none. Kept tests: the round-trip and limit tests use finite
values and current timestamps, so no new criterion handles their input differently.

Files no unit test can cover: none.

## Areas

- History (`docs/areas/history.md`): *Restore on startup* gains the drop rule.

## Documentation updates

- `docs/areas/history.md`, *Restore on startup* (Dev): a bullet that a point whose value is NaN or infinite, or
  whose timestamp lies outside the years 0–9999, is dropped while the file is read; the rest of its series and
  of the file are kept, and a series left without points follows the empty-series rule of the bullet above; add
  0021 to *Related decisions*.
- Sweep: `docs/areas/history.md` *Limits and errors* lists what makes a file invalid — unchanged, since such a
  point does not; `docs/ARCHITECTURE.md` line 171-173 ("decoding enforces hard limits") stays true;
  `docs/API.md` and `http-api.md` promise nothing about these values; README *History* section (lines 418-430)
  does not describe decode rules. No changelog file exists, and the PR template asks for nothing else here.

## Architecture check

No guarantee of `.squad/project.md` is weakened: the restore still never fabricates a reading (a dropped point is
a gap, as in decision 0003), the bounded decode is unchanged, the v1 shape is unchanged.

## Security considerations

- [x] Limits: unchanged; the point count bound is checked on the declared count before allocation, the drop only
  shortens the result.
- [x] Exceptions reaching a user: none new; the change removes the 500 this input caused.

## Decision records

- `docs/decisions/0021-non-finite-history-point-dropped-on-restore.md` (Proposed)

## Challenge

1. minor — AC3 could pass by trimming instead of by the drop. Accepted: *Tests per criterion* fixes AC3's fixture
   (every valid point inside both windows, a per-device series of only dropped points and one with valid points
   around dropped ones) and its assertion (each restored series equals exactly its valid points); AC3 now says
   "holds exactly the valid points".

## Security review

1. BLOCKING — the timestamp is a second unencodable field (`time.UnixMilli` of any int64; `MarshalJSON` fails
   outside years 0–9999; a far-future point is never trimmed). Accepted, fixed here: same function, same
   root cause. The guard checks `Year()` of the decoded time, which is the year `MarshalJSON` formats; the
   proposed UTC millisecond bounds were not taken, because the decoded time is local and a zone offset moves the
   year boundary (fact 4). AC1–AC3, the AC1 boundary rows (computed in `time.Local`), record 0021 and the
   history.md bullet are reworded. Accepted forms re-checked: value — every all-ones-exponent pattern is dropped,
   every other is finite and encodes; timestamp — `appendStrictRFC3339` has two failure cases, the year (guarded)
   and a zone offset of 24 h or more, which `time.Local` from tzdata never has (offsets stay below 24 h), so it
   is not guarded. Out-of-order timestamps (`dropPrefix` assumes sorted input) encode fine and leave by capacity;
   not addressed.
2. NON-BLOCKING — the drop is silent. Decided: leave as decided in 0021 (file only writable with `data_dir`
   access, the history is disposable, a dropped point reads as a gap like a failed reading); no follow-up issue.

## Out of scope / follow-ups

- Proposed issue — title: "[Hardening] Reject non-finite vcgencmd temperatures". Body: `parseVcgencmdTemp`
  (`internal/collector/temperature.go`) uses `strconv.ParseFloat`, which accepts `nan`/`inf`; a `temp=nan'C`
  output would put NaN into `gpu_temperature` or `pmic_temperature` and make `json.Marshal` fail
  `GET /api/v1/metrics`. Proposed: treat a non-finite value as unsupported output (`errVcgencmdUnsupportedOutput`),
  with a unit test.
