# Plan: Log missing optional sources once and query GPU/PMIC temperature independently

Source: Issue #197
Tier: standard — changes collector behavior (what is logged, when GPU/PMIC are read) without creating, changing or removing a security control.
Security areas touched: Logging (new log-once calls with error texts in `internal/collector/collector.go`); Parsing of local input (missing-source results of the thermal, wireless, hwmon and cpufreq collectors); Subprocesses (`vcgencmd measure_temp` now also runs on a host without a readable thermal zone; `vcgencmd.go` itself unchanged)

## Problem / root cause

1. `internal/collector/collector.go:510-512` logs a temperature error with `c.log.Warn` on every fast tick; a
   missing zone returns the constant error "no CPU thermal zone detected" (`temperature.go:152`), so the same line
   repeats every tick. Lines 501-503, 533-536 and 538-541 do the same for cpufreq, wireless and hwmon, but those
   collectors return no error for a missing source: `WirelessCollector.Collect` maps `ENOENT` to `(nil, nil)`
   (`wireless.go:106-109`), `readHwmonSensors` and `CPUFreqCollector.Collect` return an empty result for a glob
   without matches (glob at `hwmon.go:45`, `cpufreq.go:39`), and `ThrottledCollector.Collect` maps
   `errVcgencmdUnavailable` to `(nil, nil)` (`throttled.go:58-61`). The GPU/PMIC errors are dropped inside
   `TemperatureCollector.Collect` (`temperature.go:175-192`). Only `throttledFailures` and `piholeFailures` use
   `failureLog` (`collector.go:205-208`).
2. `TemperatureCollector.Collect` returns early when no zone is detected or the read fails
   (`temperature.go:151-168`), before the vcgencmd readings at lines 175-191.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | A missing thermal zone is warned on every fast tick | `collector.go:510-512`; error text constant, `temperature.go:152` | confirmed |
| 2 | Missing `vcgencmd`, `/proc/net/wireless`, hwmon or cpufreq is not logged | `throttled.go:58-61`, `temperature.go:175-191`, `wireless.go:106-109`, `hwmon.go:45`, `cpufreq.go:39` | confirmed |
| 3 | Only `throttled` and Pi-hole use `failureLog` | `collector.go:514`, `collector.go:742` | confirmed |
| 4 | GPU/PMIC are attempted only after a successful zone read | `temperature.go:151-168` return before 175 | confirmed |
| 5 | `docs/API.md` names only `vcgencmd` availability as the condition for the two fields | `docs/API.md:294-303`, `:796-797`; they already state the fixed behavior, so no edit | confirmed |
| 6 | `failureLog.observe` warns on first/changed error, debugs repeats, logs `<msg>: recovered` at INFO | `failurelog.go:27-48`, `failurelog_test.go` | confirmed |
| 7 | A fast tick makes at most three vcgencmd calls; still true after the change | `collector.go:150-169`; the change only drops the zone precondition | confirmed |
| 8 | `collectFastTickSamples` is at gocognit 15 today; replacing `if err != nil { Warn }` blocks by `observe` calls lowers it | `collector.go:491-548` counted by reading, `.golangci.yml` | confirmed |
| 9 | `AGENTS.md`, `.squad/project.md` and `docs/ARCHITECTURE.md:96-101` stay true ("a logged warning"; details in Metrics) | read | confirmed |

No hardware report: the issue comes from the area-document review. What only a Pi can show: the journal of a
Pi 3 (PMIC unsupported, AC5) and of a Pi without wireless — optional smoke test in the PR's Reviewer Notes.

## Acceptance criteria

Messages are the `msg` values the tests match as `msg="<text>"` (the quotes keep `temperature collection failed`
apart from `gpu temperature collection failed`). "Logged once" means: two or more ticks with the same error give
exactly one WARN line and one DEBUG line per further tick; the first tick after recovery gives exactly one INFO line
`<message>: recovered`.

- AC1: A missing thermal zone is logged once under `temperature collection failed`, and recovers with
  `temperature collection failed: recovered` when a zone appears.
- AC2: These missing sources are logged once and recover the same way, each under its message: `vcgencmd` not
  found (`gpu temperature collection failed`, `pmic temperature collection failed` and
  `throttled state collection failed`, one each); `/proc/net/wireless` absent (`wireless collection failed`); no
  hwmon chip directory with `hwmon_enabled` on (`hwmon sensor collection failed`); no cpufreq directory
  (`cpu frequency collection failed`). With `hwmon_enabled` off nothing is logged for hwmon. The snapshot fields
  stay omitted while their source is missing.
- AC3: With `vcgencmd` answering and no readable thermal zone, a fast tick reports `gpu_temperature` and
  `pmic_temperature`, and `TemperatureValid` is false.
- AC4: With every optional source missing or failing in the same tick, the fast tick completes without a panic
  and still reports the other sources (memory, uptime).
- AC5: When `vcgencmd measure_temp pmic` answers with something other than a temperature, the PMIC reading is
  logged once at WARN under `pmic temperature collection failed`, `measure_temp pmic` runs only once over several
  ticks, and no `recovered` line follows.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCollector_FastTick_OptionalSourceLoggedOnce`, subtest "AC1 missing thermal zone" | `internal/collector/collector_test.go` |
| AC2 | `TestCollector_FastTick_OptionalSourceLoggedOnce`, subtests "AC2 vcgencmd", "AC2 wireless", "AC2 hwmon", "AC2 cpufreq", "AC2 hwmon disabled logs nothing"; `TestThrottledCollector_Collect_NoVcgencmd` (adapted: now an error); `TestWirelessCollector_Collect_NoWirelessFile` (adapted); `TestReadHwmonSensors_NoHwmonDirectory` (adapted); `TestCPUFreqCollector_Collect_NoDriver` (adapted); `TestTemperatureCollector_CollectGPU_NoVcgencmd` | `collector_test.go`, `throttled_test.go`, `wireless_test.go`, `hwmon_test.go`, `cpufreq_test.go`, `temperature_test.go` (all `internal/collector/`) |
| AC3 | `TestTemperatureCollector_CollectGPUAndPMIC_WithoutThermalZone_AC3`; `TestCollector_FastTick_GPUAndPMICWithoutThermalZone_AC3` | `internal/collector/temperature_test.go`, `internal/collector/collector_test.go` |
| AC4 | `TestCollector_FastTick_AllOptionalSourcesMissing_AC4` (may pass on the skeleton: then deferred to step 6) | `internal/collector/collector_test.go` |
| AC5 | `TestCollector_FastTick_PMICUnsupportedLoggedOnce_AC5` | `internal/collector/collector_test.go` |

Test notes for the Tester: every collector-level test sets **all** optional sources to fixtures
(`TemperatureCollector{zoneGlob: …}`, a `vcgencmdRunner`, `WirelessCollector{path: …}`, `HwmonCollector{glob: …}`,
`CPUFreqCollector{glob: …}`) so the CI host's real `/sys` does not add lines, and uses `newCapturingLogger` /
`countLogLines`. Recovery of `vcgencmd` without touching runner internals: start with
`vcgencmdRunner{now: clock, lastDetect: t0}`, then `t.Setenv("PATH", dir)` with a fake named `vcgencmd`
(`writeFakeVcgencmd`) and advance the clock past `detectRetryInterval`; the thermal zone recovers the same way
(`TemperatureCollector{now: clock, lastZoneDetect: t0}`, write the zone, advance the clock). The same runner goes
into `c.temp` and `c.throttled`, as `fakeVcgencmdCollector` does.

## Approach

- `TemperatureCollector` is split into three independent reads. `Collect()` reads only the thermal zone (the
  re-detection logic unchanged). `CollectGPU(ctx)` and `CollectPMIC(ctx)` each run their vcgencmd call whether or
  not a zone exists: nil runner → `(nil, nil)` (disabled); runner without `vcgencmd` → `(nil, err)` with
  `errVcgencmdUnavailable`; exec or parse failure → `(nil, err)`. The private latch `pmicUnsupported bool` becomes
  `pmicUnsupported error`: set to the unsupported-output error the first time, and returned unchanged on every
  later call without running vcgencmd, so `failureLog` sees an identical error (DEBUG) and never a success (no
  false "recovered"; AC5).
- `ThrottledCollector.Collect` returns `(nil, err)` for `errVcgencmdUnavailable` instead of `(nil, nil)`; nil
  runner stays `(nil, nil)`.
- `WirelessCollector.Collect` drops the `ENOENT` special case: an absent file is `read <path>: …` like any read
  error. `readHwmonSensors` returns `no hwmon chips found matching <glob>` and `CPUFreqCollector.Collect` returns
  `no cpufreq directories found matching <glob>` when the glob matches nothing. All three still return a nil slice
  with the error, so the snapshot fields stay omitted. A chip without channels or a core missing a file keeps
  being skipped without an error.
- `Collector` gets one `failureLog` per optional source next to `throttledFailures` and `piholeFailures`:
  thermal zone, GPU, PMIC, wireless, hwmon, cpufreq. `collectFastTickSamples` calls `Collect`, `CollectGPU`,
  `CollectPMIC` one after another and passes every optional source's error to its `observe` with the message of
  AC1/AC2; the mandatory sources keep `c.log.Warn`. `TemperatureValid` and the alert sample still follow the zone
  error only.
- Doc comments that state the old behavior (`TemperatureCollector`, `Collect`, `ThrottledCollector`,
  `WirelessCollector.Collect`, `HwmonCollector.Collect`, `CPUFreqCollector.Collect`, `fastTickSamples`) are
  updated by the Dev. Log output: layout fixed by `failureLog`; no new user-visible output otherwise.

## Options considered

- **One `vcgencmd` availability line instead of three per-reading lines** — lost: the collector would need the
  shared runner (today a local in `New`, replaced per collector in tests) and would have to divert "not available"
  away from the three readings; a reading-specific failure still needs its own line. Recorded in 0001.
- **`Collect(ctx)` returning a struct with a value and an error per reading** instead of three methods — lost:
  three methods make the independence of AC3 structural (no early return can skip a reading) and match one
  `observe` per source; the struct would still need every field read by the caller.
- **Treating an empty result (no interfaces, no channels) as "missing"** — lost: a present source with nothing
  to report (wireless file without interfaces, chip without temperature channels) is not a missing source, and
  would warn on healthy hosts.
- **Warn once at startup only, or per tick** — lost, see 0001.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `temperature.go` | `Collect` zone only; new `CollectGPU`, `CollectPMIC`; latch holds its error |
| `internal/collector` | `throttled.go` | missing `vcgencmd` is an error |
| `internal/collector` | `wireless.go`, `hwmon.go`, `cpufreq.go` | missing source is an error |
| `internal/collector` | `collector.go` | six new `failureLog` fields, `collectFastTickSamples` wiring |

## Signatures (for the Dev's skeleton)

- Changed: `func (c *TemperatureCollector) Collect() (Temperature, error)` (was `Collect(ctx context.Context) (Temperature, *GPUTemperature, *PMICTemperature, error)`)
- New: `func (c *TemperatureCollector) CollectGPU(ctx context.Context) (*GPUTemperature, error)`
- New: `func (c *TemperatureCollector) CollectPMIC(ctx context.Context) (*PMICTemperature, error)`

`ThrottledCollector.Collect`, `WirelessCollector.Collect`, `HwmonCollector.Collect` and `CPUFreqCollector.Collect`
keep their signatures. The skeleton rewrites `temperature.go` (the three methods; the old body moves into the
stubs only as the stub rules allow) and the call in `collector.go:509`.

## Test files

`internal/collector/collector_test.go`, `temperature_test.go`, `throttled_test.go`, `wireless_test.go`,
`hwmon_test.go`, `cpufreq_test.go`.

Existing test call sites of the changed signature: `temperature_test.go` lines 122, 148, 231, 273, 312, 333, 350,
367, 376, 382, 404, 420, 426 (`c.Collect(ctx)` with four results) and 282, 317 (`c.pmicUnsupported` as bool) — the
**Dev** adapts them mechanically in step 4 (`Collect()` for the zone, `CollectGPU`/`CollectPMIC` for the readings,
`c.pmicUnsupported != nil`), no assertion touched. Existing tests whose expected result changes (no signature
change): `TestThrottledCollector_Collect_NoVcgencmd`, `TestWirelessCollector_Collect_NoWirelessFile`,
`TestReadHwmonSensors_NoHwmonDirectory`, `TestCPUFreqCollector_Collect_NoDriver` — the **Tester** adapts them in
step 5 as AC2 tests. `TestCollector_FastTick_ThrottledFailureLoggedOnce` and `fakeVcgencmdCollector` keep working
unchanged.

Files no unit test can cover: none.

## Areas

Metrics (`docs/areas/metrics.md`): GPU/PMIC no longer depend on the zone; which failures are logged once.

## Documentation updates

All by the Dev, against the code as built:

- `docs/areas/metrics.md`, *Temperature, sensors and throttling*: replace "Both are only attempted after the
  thermal zone was read successfully in the same tick. … a failure is silent …" with: both are attempted on every
  fast tick whether or not a thermal zone was read; a failure affects neither the other reading nor `temperature`
  and is logged as stated under *Limits and errors*.
- `docs/areas/metrics.md`, *Limits and errors*: take `temperature` and `wireless` out of the every-tick list; the
  log-once bullet lists `temperature`, `gpu_temperature`, `pmic_temperature`, `throttled`, `wireless`, `sensors`
  (only with `hwmon_enabled`), `cpu_frequency` and `pihole`, says that a missing `vcgencmd` is reported once by each
  of the three readings, and that a board without PMIC sensor logs one warning and no recovery; the "Not logged at
  all" bullet keeps only a single mountpoint, a single hwmon channel or cpufreq core, a single process and system
  identity values. Add 0001 under *Related decisions*. Remove the first two *Open questions* (the `/proc/stat` one
  stays).
- `docs/API.md`: no edit — `:294-303` and `:796-797` already state `vcgencmd` as the only condition (fact 5).
- Sweep result, no edit needed: `docs/ARCHITECTURE.md:96-109`, `docs/areas/http-api.md:142`,
  `docs/areas/dashboard.md:97`, `docs/areas/alerts.md:122`, `README.md:17-18`, `SECURITY.md`, `AGENTS.md` and
  `.squad/project.md` (*Guarantees*: "a logged warning" stays true; the *Open questions* pointer stays valid
  because one remains). No changelog file exists.

## Architecture check

Per-source isolation (`docs/ARCHITECTURE.md`, fast tick) is kept and extended: GPU/PMIC no longer depend on the
zone. "Never a crash, never a fabricated reading" unchanged (AC4; missing fields stay omitted). The /healthz
budget `WorstCaseTickOverhead` (three vcgencmd calls) is unchanged; on a host with `vcgencmd` and no zone a tick
now makes those calls, within the budget.

## Security considerations

- [x] No limits: no new byte or length limit; `vcgencmd` failure text stays bounded at 200 bytes, one line
  (`vcgencmd.go`, unchanged).
- [x] No exception reaches a user-visible message: the new error texts go to the journal only; they carry
  `/proc`/`/sys` paths and `vcgencmd` output, no secret (no name matches *Logger arguments* in `.squad/stack.md`).

## Decision records

- `docs/decisions/0001-optional-sources-log-failures-once.md` (Proposed)

## Out of scope / follow-ups

- The `/proc/stat` seven-counter crash: #195.
- Every-tick warnings of the mandatory sources (`cpu`, `memory`, `disks`, …): unchanged by the issue's decision.

## Challenge notes

Devil's Advocate, minor objections (no major):

1. The `temperature_test.go` adaptations are not purely mechanical. Bind the existing `err == nil` checks to `Collect()` (the zone) only. In `TestTemperatureCollector_Collect_PMICUnsupportedLatches` assert that `CollectPMIC` returns an error matching `errors.Is(err, errVcgencmdUnsupportedOutput)` on every call and keep the check that `measure_temp pmic` runs once. In `TestTemperatureCollector_Collect_PMICTransientFailureKeepsRetrying` assert that the error does not match `errVcgencmdUnsupportedOutput`. In `TestTemperatureCollector_Collect_VcgencmdExecFails` assert that `CollectGPU` and `CollectPMIC` return a non-nil error with nil readings. Do not discard the new errors with `_`.
2. A cpufreq source that exists but has no readable core, and hwmon chips present but with no readable channel, stay silent (the field is omitted, nothing is logged). Keep this behavior; in the "Not logged at all" bullet of `docs/areas/metrics.md` say it explicitly: "cpufreq directories present but no core readable, hwmon chips present but no channel readable".
3. Decision record 0001 does not place `updates` (apt) and process collection on either side of its split. In *Decision* add one sentence: `updates` and process collection keep logging every failure, since they are not hardware sources.

## Security notes

Security, non-blocking: the claim in *Security considerations* that `vcgencmd` failure text stays bounded at 200 bytes holds only for the exec-failure path (`sanitizeVcgencmdDetail`, `vcgencmd.go`, unchanged). The parse-failure text of `measure_temp` carries vcgencmd's stdout `%q`-escaped and unbounded (`temperature.go:228,234`), as `get_throttled` already does (`throttled.go:77,84`). The PMIC latch keeps that error for the life of the process. Bounding the parse-failure text is general hardening outside this change; the Lead decides on a linked issue.
