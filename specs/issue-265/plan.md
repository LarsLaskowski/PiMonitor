# Plan: Reject non-finite vcgencmd temperatures

Source: Issue #265
Tier: standard — a new validation in a parser changes which vcgencmd answers become a reading (a behavior change), and it is input handling, not a security control.
Security areas touched: Parsing of local input (command output in `internal/collector`, here `vcgencmd measure_temp`)

## Problem / root cause

`parseVcgencmdTemp` (`internal/collector/temperature.go:243-256`) returns whatever `strconv.ParseFloat` decodes at
line 251. `ParseFloat` accepts the tokens `NaN`, `Inf` and `Infinity` (signed, any case) without an error, so
`temp=nan'C` becomes a NaN reading. `CollectGPU`/`CollectPMIC` (lines 176-212) put it into the snapshot
(`collector.go:619-621`, `670-671`), and `GET /api/v1/metrics` (`handlers.go:41-42` → `writeJSON`, line 15) then fails
to encode the whole snapshot: `encoding/json` rejects NaN/±Inf with an `UnsupportedValueError`, `Encoder.Encode`
writes nothing, and the client gets a 200 with an empty body while the error is logged.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | `parseVcgencmdTemp` uses `strconv.ParseFloat` with no finiteness check (issue) | `temperature.go:251-255` | confirmed |
| 2 | `ParseFloat` accepts `NaN`, `Inf`, `Infinity`, optionally signed, case-insensitively, as special values without error | Go 1.27 source `src/strconv/number.go:90`, `src/internal/strconv/atof.go:48` (`special`) | confirmed |
| 3 | An overflowing literal (`1e999`) already fails with `strconv.ErrRange` and is classed `errVcgencmdUnsupportedOutput` | `temperature.go:252-253`; test case "AC2 out-of-range number", `temperature_test.go:561-565` | confirmed |
| 4 | A NaN reading would reach `gpu_temperature`/`pmic_temperature` of the snapshot (issue) | `collector.go:619-621`, `670-671`; `types.go:241-242` | confirmed |
| 5 | NaN/±Inf make `GET /api/v1/metrics` fail to encode (issue: "make `json.Marshal` fail") | `handlers.go:15` uses `json.NewEncoder(w).Encode`; Go `encoding/json/encode.go:572-573` raises `UnsupportedValueError`, `stream.go:224-227` returns before writing; the call is `Encoder.Encode`, not `json.Marshal`, with the same failure: empty 200 body plus an error log | confirmed |
| 6 | These values reach the snapshot, not the history series (issue) | no `GPUTemperature`/`PMICTemperature` in `history*.go`, `persist.go` or `internal/alert` (grep) | confirmed |
| 7 | An `errVcgencmdUnsupportedOutput` from `measure_temp pmic` latches the PMIC reading off for the process's life; from `measure_temp` (GPU) it does not | `temperature.go:198-209` (latch only in `CollectPMIC`), `176-185` | confirmed |
| 8 | The Prometheus writer would print a NaN as the `NaN` token rather than fail | `prometheus.go:190-208`, `formatFloat` at `prometheus.go:312-317` | confirmed |

## Acceptance criteria

- AC1: `parseVcgencmdTemp` returns `0` and an error that wraps `errVcgencmdUnsupportedOutput` for every non-finite
  value `ParseFloat` accepts after the `temp=` prefix: `temp=nan'C`, `temp=NaN'C`, `temp=inf'C`, `temp=+Inf'C`,
  `temp=-Infinity'C`. The error text is `vcgencmd: output is not a temperature reading: non-finite vcgencmd temp
  "<output>"`, the output bounded as by `sanitizeVcgencmdDetail` — for `temp=nan'C` exactly
  `vcgencmd: output is not a temperature reading: non-finite vcgencmd temp "temp=nan'C"`. Finite readings and the
  existing error cases are unchanged.
- AC2: `CollectGPU` and `CollectPMIC` against a vcgencmd that answers `temp=nan'C` return a nil reading and an error
  wrapping `errVcgencmdUnsupportedOutput`; for PMIC the error latches (vcgencmd invoked once across three calls),
  for GPU it does not (invoked on every call).
- AC3: after a fast tick against a vcgencmd answering `temp=nan'C` to both `measure_temp` and `measure_temp pmic`,
  the snapshot has neither `GPUTemperature` nor `PMICTemperature`, and `json.Marshal` of the snapshot succeeds.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestParseVcgencmdTemp_NonFinite_AC1` (table, one subtest per spelling, exact text for `temp=nan'C`) | `internal/collector/temperature_test.go` |
| AC2 | `TestTemperatureCollector_CollectGPUAndPMIC_NonFinite_AC2` (fake vcgencmd via `writeFakeVcgencmd`, invocation log as in `TestTemperatureCollector_Collect_PMICUnsupportedLatches`) | `internal/collector/temperature_test.go` |
| AC3 | `TestCollector_FastTick_NonFiniteVcgencmdTemp_AC3` (`newOptionalSourceEnv`, a fake vcgencmd answering `throttled=0x0` to `get_throttled` and `temp=nan'C` otherwise) | `internal/collector/collector_test.go` |

## Approach

In `parseVcgencmdTemp`, after a successful `ParseFloat`, return
`fmt.Errorf("%w: non-finite vcgencmd temp %q", errVcgencmdUnsupportedOutput, sanitizeVcgencmdDetail([]byte(output)))`
when `math.IsNaN(celsius) || math.IsInf(celsius, 0)` (add the `math` import). The check is on the decoded value,
not on the tokens, so every spelling of fact 2 is covered, and the existing `ErrRange` path of fact 3 stays as it
is. Update the comments of `errVcgencmdUnsupportedOutput` and `parseVcgencmdTemp` to name the non-finite case.
No new user-visible output: the error reaches only the log through the existing failure loggers.

Format sample: the `temp=NN.N'C` form is unchanged and the existing tests use hand-typed lines of it; no firmware
is known to print a non-finite value, so the AC1 inputs are synthetic by nature — no real sample exists or is needed.

## Options considered

- Chosen: class a non-finite value as `errVcgencmdUnsupportedOutput`, as the issue proposes. It matches how
  `temp=1e999'C` (±Inf via `ErrRange`) is already classed, so `inf` and `1e999` behave alike.
- A separate, non-latching error for non-finite values, so a PMIC sensor that once printed NaN is retried: lost —
  it would treat `temp=inf'C` and `temp=1e999'C` differently, and a firmware that answers a temperature request
  with NaN is not a transient exec failure but a sensor that does not deliver readings, the case the latch is for.
- Sanitizing non-finite floats at the JSON layer (`writeJSON`): lost — it would hide the bad reading as a fabricated
  value or null instead of omitting it, against "never a fabricated reading" (`AGENTS.md`), and would not stop the
  NaN in the Prometheus output (fact 8).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/collector | `temperature.go` (`parseVcgencmdTemp`, comments) | finiteness check |
| internal/collector | `temperature_test.go`, `collector_test.go` | new tests |

## Signatures (for the Dev's skeleton)

None: no new or changed API member; only the body of the unexported parser changes. No skeleton.

## Test files

- `internal/collector/temperature_test.go`
- `internal/collector/collector_test.go`

Existing test code that calls a changed signature: none. Existing tests kept unchanged: `TestParseVcgencmdTemp` and
`TestParseVcgencmdTemp_ErrorText` have no non-finite input (`temp=1e999'C` still fails in `ParseFloat` first).

Files of the change no unit test can cover: none.

## Areas

- `docs/areas/metrics.md`, *Temperature, sensors and throttling*: a non-finite value counts as "anything other than a
  temperature".

## Documentation updates

Owned by the Dev, written once the code is green:

- `docs/areas/metrics.md` (bullets at lines 67-75): `gpu_temperature`/`pmic_temperature` are present only when the
  call returned a finite temperature; a non-finite answer (`temp=nan'C`, `temp=inf'C`) is output other than a
  temperature, so it latches the PMIC reading off like an error line, and fails the GPU reading for that tick.
- `docs/API.md` (lines 300-310): `gpu_temperature` is present only if `vcgencmd` responded with a finite temperature;
  same for `pmic_temperature`.

Sweep: `docs/ARCHITECTURE.md` (line 99, collectors return a typed value plus an error) and the Prometheus table of
`docs/API.md` (lines 911-912, "like `gpu_temperature`") stay true unchanged. No changelog exists in the repository.

## Architecture check

Strengthens "a missing hardware source yields an empty value, never a fabricated reading". The presence rule of
`gpu_temperature`/`pmic_temperature` in `v1` is not changed in a way a client could observe: a non-finite answer
made the whole response empty before; decision 0018 is not touched.

## Security considerations

- [x] No limits: the check bounds nothing; the quoted output keeps the existing 200-byte bound of
  `sanitizeVcgencmdDetail`, applied to the raw bytes before quoting ([0002](../../docs/decisions/0002-bound-vcgencmd-output-in-errors.md)).
- [x] The new error reaches only the logs (`gpu temperature collection failed` / `pmic temperature collection
  failed`, with the AC1 text); no user-visible failure reason.

## Decision records

- none: no decision beyond the obvious fix — the non-finite value joins the existing unsupported-output class, as
  the out-of-range value already does.

## Out of scope / follow-ups

Related defects found while checking (proposed issues for the orchestrator):

- "[Hardening] Reject non-finite values in /proc load average, uptime and wireless parsers": `loadavg.go:19-27`,
  `uptime.go:19` and `wireless.go:51-55` use `strconv.ParseFloat` without a finiteness check; a NaN load average
  would break `GET /api/v1/metrics` the same way. The kernel formats these as decimals, so it is hardening only.
- "[HTTP API] writeJSON answers 200 with an empty body when encoding fails": `handlers.go:13-18` logs the
  encode error but the client gets an empty 200; a 500 would tell clients the response failed.

## Challenge notes

The plan holds up. I checked it against the code and found nothing to object to.

- **Facts 1, 3, 4, 6 and 7 hold:**
  - `parseVcgencmdTemp` has no finiteness check (`/home/user/PiMonitor/internal/collector/temperature.go:243-256`).
  - The overflowing-literal error path stays as it is.
  - The values go only into `c.latest` (`collector.go:670-671`). The other readers are `prometheus.go:129-130` and JSON. Nothing in history, persistence or alerts reads them.
  - The PMIC latch is only in `CollectPMIC` (`temperature.go:198-209`).
- **AC1 error text is right:** `sanitizeVcgencmdDetail` (`/home/user/PiMonitor/internal/collector/vcgencmd.go:137-147`) only collapses whitespace and truncates past the bound. `%q` does not escape `'`, so the exact text given for `temp=nan'C` is what the code will produce.
- **No other vcgencmd parser has the same defect:** `throttled.go:77` uses `ParseUint`, which cannot return NaN or Inf. The `/proc` parsers that use `ParseFloat` (`loadavg.go`, `uptime.go`, `wireless.go`) are already listed as follow-ups.
- **AC3 is buildable and catches the defect:** `newOptionalSourceEnv` (`collector_test.go:1050`) and the fake vcgencmd pattern of `installVcgencmd` support it. Without the fix, `json.Marshal` of the snapshot would fail.
- **The fix is the simplest one:** it reuses the existing error class, and the alternatives the plan rejected were rightly rejected.
