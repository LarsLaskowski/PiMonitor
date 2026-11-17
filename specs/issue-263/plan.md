# Plan: Drop non-finite points when decoding history.bin

Source: Issue #263
Tier: standard — the decoder of `history.bin` changes behavior (it drops points it kept before), and that check only keeps malformed input from breaking the program: input handling, no security control (`.squad/routing.md`, *Tiers*).
Security areas touched: File writes: `<data_dir>/history.bin` (atomic write, bounded decode) in `internal/collector/persist.go`

## Problem / root cause

`decodeHistoryPoints` (`internal/collector/persist.go:292-299`) turns the stored value bits of every point into a
float64 with `math.Float64frombits` (line 297) and keeps it, whatever its bit pattern. A NaN or ±Inf point restored
by `loadHistory` (line 427) into the ring buffers reaches `json.Marshal` in `cachedHistory`
(`internal/httpapi/handlers.go:113`) and in `historyPayload` (line 88, `?since=`), which fail with
`UnsupportedValueError`; `handleHistory` then answers 500 "internal error" (lines 68-72) on every request until
the point leaves the window.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The decoder keeps any float bits, for every series kind | `persist.go:292-299`: one loop for all kinds, no check; `decodeSeries` (line 289) calls it for every kind | confirmed |
| 2 | `json.Marshal` fails on a NaN or ±Inf `float64` | `encoding/json` docs: "NaN and +/-Inf values will return an UnsupportedValueError"; `HistoryPoint.Value` is a plain `float64` (`types.go:264-267`), no custom marshaller (grep `MarshalJSON` in `internal/collector`: none) | confirmed |
| 3 | The failure fails the whole history response, full window and `?since=` | `handlers.go:68-72`, `88`, `113-118` | confirmed |
| 4 | Producing such a file needs write access to `data_dir` | `history.md` *Persistence*; `deployment.md` line 103: only the service user writes there | confirmed |
| 5 | The openHAB window of #183 keeps a bad point up to 168 h | `importHistory` trims openHAB series by `openhab.history_window_hours` (`persist.go`, `ohCutoff`); `history.md` *Restore on startup* | confirmed |
| 6 | `encodeHistory` writes NaN and ±Inf values as their bits, so a test can build the file with it | `persist.go` `encodeHistory`: `math.Float64bits(p.Value)`, no check | confirmed |
| 7 | Live recording of the history series takes finite values | thermal zone read with `strconv.ParseInt` (`temperature.go:69`); `usagePercent` clamps and guards a zero delta (`cpu.go:72-85`); rates divide only by `elapsed > 0` (`network.go:97`, `diskio.go:128`); `/proc/loadavg` is kernel-formatted | confirmed |
| 8 | A per-device series left without points is not restored; a scalar one restores empty | `importHistory` `importMap` skips `len(points) == 0`; scalar `Fill` takes the slice as is (`persist.go`) | confirmed |

The issue's claims (1–5) are confirmed. Related defect found on the way: `parseVcgencmdTemp`
(`temperature.go:251`) parses with `strconv.ParseFloat`, which accepts `nan`, `inf` and `infinity` in any case, so a
`temp=nan'C` output would put NaN into `gpu_temperature`/`pmic_temperature` of the snapshot and fail
`GET /api/v1/metrics`. Not a history series; proposed as a follow-up issue (*Out of scope*).

## Acceptance criteria

- AC1: `decodeHistoryPoints` drops every point whose value bits are a NaN (exponent all ones, mantissa non-zero, either sign — quiet, signalling, any payload) or ±Inf, and keeps every other point with its timestamp and value, in file order; finite edge values (±`math.MaxFloat64`, the smallest subnormal, -0) are kept.
- AC2: for every series kind of the format (the seven scalar kinds, the five per-device kinds with a key, the three openHAB kinds), `decodeHistory` of a file whose series holds finite points around a NaN, a +Inf and a -Inf point returns that series with exactly the finite points, and no error.
- AC3: a collector that restores such a file with `loadHistory` holds only finite points, `json.Marshal` of its `History()` succeeds, and a per-device series whose points were all non-finite is not restored.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestDecodeHistoryPoints_DropsNonFiniteValues_AC1` (table: each non-finite bit pattern between two finite points; one row of finite edge values, all kept) | `internal/collector/persist_test.go` |
| AC2 | `TestDecodeHistory_DropsNonFinitePointsPerKind_AC2` (table over the 15 kinds, one `t.Run` per kind) | `internal/collector/persist_test.go` |
| AC3 | `TestCollector_LoadHistory_DropsNonFinitePoints_AC3` | `internal/collector/persist_test.go` |

AC1 builds `raw` with `binary.LittleEndian.AppendUint64` from the bit patterns `0x7FF8000000000001`,
`0x7FF0000000000001`, `0xFFF8000000000000`, `0x7FFFFFFFFFFFFFFF`, `0x7FF0000000000000`, `0xFFF0000000000000`; the
kept row uses `0x7FEFFFFFFFFFFFFF`, `0xFFEFFFFFFFFFFFFF`, `0x0000000000000001`, `0x8000000000000000`, compared by
bits. AC2 and AC3 write the file with `encodeHistory` / `writeHistoryFile` from `math.NaN()`, `math.Inf(1)`,
`math.Inf(-1)`; AC3 uses `newOHPersistCollector` so the openHAB series are restored too.

## Approach

In `decodeHistoryPoints`, allocate `make([]HistoryPoint, 0, n)` (the bound stays `n`, checked before) and append
a point only when `!math.IsNaN(v) && !math.IsInf(v, 0)`; signature unchanged. `math.IsNaN` and `math.IsInf(v, 0)`
together cover every bit pattern with an all-ones exponent, which are exactly the non-finite ones. No log line
(record 0021). Nothing else changes: the file is still valid, the limits and their order stay.

## Options considered

Listed with why they lost in [0021](../../docs/decisions/0021-non-finite-history-point-dropped-on-restore.md):
reject the whole file as invalid; filter at serve time in `internal/httpapi`; replace the value with 0 or the
previous value.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `persist.go`, `decodeHistoryPoints` | skip non-finite points |
| `internal/collector` | `persist_test.go` | three tests |
| `docs` | `areas/history.md`, `decisions/0021-…` | rule, record |

## Signatures (for the Dev's skeleton)

None: no new or changed API member. The private function decodeHistoryPoints keeps its signature; no skeleton
file is rewritten.

## Test files

- `internal/collector/persist_test.go`

Existing test code that calls a changed signature: none. Kept tests: the round-trip and limit tests use finite
fixtures only, so no new criterion handles their input differently.

Files no unit test can cover: none.

## Areas

- History (`docs/areas/history.md`): *Restore on startup* gains the drop rule.

## Documentation updates

- `docs/areas/history.md`, *Restore on startup* (Dev): a bullet that a point whose value is NaN or infinite is
  dropped while the file is read, the rest of its series and of the file are kept, and a series left without
  points follows the empty-series rule of the bullet above; add 0021 to *Related decisions*.
- Sweep: `docs/areas/history.md` *Limits and errors* lists what makes a file invalid — unchanged, since a
  non-finite value does not; `docs/ARCHITECTURE.md` line 171-173 ("decoding enforces hard limits") stays true;
  `docs/API.md` and `http-api.md` promise nothing about non-finite history values; README *History* section
  (lines 418-430) does not describe decode rules. No other hit; no changelog file exists, and the PR template asks
  for nothing else here.

## Architecture check

No guarantee of `.squad/project.md` is weakened: the restore still never fabricates a reading (a dropped point is
a gap, as in decision 0003), the bounded decode is unchanged, the v1 shape is unchanged.

## Security considerations

- [x] Limits: unchanged; the point count bound is checked on the declared count before allocation, the drop only
  shortens the result.
- [x] Exceptions reaching a user: none new; the change removes the 500 this input caused.

## Decision records

- `docs/decisions/0021-non-finite-history-point-dropped-on-restore.md` (Proposed)

## Out of scope / follow-ups

- Proposed issue — title: "[Hardening] Reject non-finite vcgencmd temperatures". Body: `parseVcgencmdTemp`
  (`internal/collector/temperature.go`) uses `strconv.ParseFloat`, which accepts `nan`/`inf`; a `temp=nan'C`
  output would put NaN into `gpu_temperature` or `pmic_temperature` and make `json.Marshal` fail
  `GET /api/v1/metrics`. Proposed: treat a non-finite value as unsupported output (`errVcgencmdUnsupportedOutput`),
  with a unit test.

## Challenge notes

Mode plan, issue #263, work folder /home/user/PiMonitor/specs/issue-263 (plan.md).

I checked the plan against the code and it holds. The root cause, the 15 series kinds and the restore rules all match:

- **Root cause:** `decodeHistoryPoints` stores `math.Float64frombits` with no check (`internal/collector/persist.go:292-299`). `decodeSeries` (line 289) calls it for every kind.
- **Series kinds:** there are 15 (`persist.go:49-65`): 7 scalar, 5 per-device and 3 openHAB. AC2's table matches.
- **Per-device restore:** `importMap` skips a series with `len(points) == 0` (`persist.go` importHistory). AC3's "per-device series not restored" therefore follows once the decoder returns an empty slice.
- **Scalar and openHAB restore:** these series go through `Fill` as they are (`persist.go:450-456`, `483-485`).
- **Live sources:** the openHAB series are pushed from integer conversions (`collector.go:521-527`). The only `ParseFloat` feeding a history series is `/proc/loadavg` (`loadavg.go:19-27`). This agrees with fact 7 and with the scope of record 0021.
- **Need:** the change is real and still needed. Nothing filters non-finite values today, and no accepted decision record covers this.
- **Scope:** filtering at decode time is the narrowest fix. The vcgencmd sibling is correctly left as a follow-up, because it affects `/api/v1/metrics`, not the history series.

Objections:

1. minor (AC3 could pass without the drop working). Instruction for the Tester: in `TestCollector_LoadHistory_DropsNonFinitePoints_AC3`, give every fixture point a timestamp inside both windows (the `HistoryWindow` passed to `newOHPersistCollector` and `openhab.history_window_hours`). Make the per-device series that is meant to disappear hold only non-finite points, and give a second per-device series finite points around non-finite ones. Then assert that `History()` holds exactly those finite points for each restored series, not just that `json.Marshal` succeeds. A fixture whose points fall outside the window would make the "not restored" and "Marshal succeeds" checks pass because of trimming, even on a build without the fix.
