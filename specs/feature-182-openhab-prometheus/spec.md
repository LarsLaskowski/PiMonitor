# Spec: Expose local openHAB status as Prometheus metrics

Source: Issue #182
Status: Draft

## Problem

Operators who scrape PiMonitor into Prometheus/Grafana (`GET /metrics`, `prometheus_enabled: true`) see the local
openHAB status only in `GET /api/v1/openhab` and on the dashboard card (#181), which keep no history. JVM memory leaks
in openHAB grow over days, so without a long-term series they go unnoticed, or the operator has to set up a second
exporter. The motivation and the proposed metric table are in the issue; this spec states what `GET /metrics` shows.

## Scenarios

The openHAB status is the one `GET /api/v1/openhab` serves at the time of the scrape ([openHAB](../../docs/areas/openhab.md),
*Detection and states*, *Sources and values*); the scrape triggers no collection of its own.

- S1: Given openHAB runs and every source answers (state `ok`, a token configured), when Prometheus scrapes
  `GET /metrics`, then besides the existing families it gets `pimonitor_openhab_up 1`,
  `pimonitor_openhab_process_running 1`, `pimonitor_openhab_process_resident_bytes`,
  `pimonitor_openhab_jvm_heap_used_bytes`, `pimonitor_openhab_jvm_heap_total_bytes`,
  `pimonitor_openhab_uptime_seconds`, `pimonitor_openhab_start_level`, `pimonitor_openhab_things{status="…"}`,
  `pimonitor_openhab_items`, `pimonitor_openhab_addons_installed` and `pimonitor_openhab_info{version="…"} 1`,
  each value equal to the matching field of `GET /api/v1/openhab`.
- S2: Given a default openHAB install without an API token (state `ok`; system info, Things and add-ons refused),
  when Prometheus scrapes, then it gets `up 1`, `process_running 1`, the resident bytes, the uptime, the Item count and
  the `info` series, and no heap, start level, Things or add-on series.
- S3: Given the openHAB process runs but its REST root does not answer (`running_unreachable`), when Prometheus
  scrapes, then it gets `up 0`, `process_running 1`, the resident bytes and the uptime, and no other openHAB series.
- S4: Given openHAB is installed but not running (`installed_not_running`), when Prometheus scrapes, then it gets
  `up 0` and `process_running 0` and no other openHAB series.
- S5: Given a host without openHAB (`not_installed`, which `openhab.enabled: false` also reports), when Prometheus
  scrapes, then the output contains no line that names a `pimonitor_openhab_` metric, exactly as before this feature.
- S6: Given an operator graphs `pimonitor_openhab_process_resident_bytes / pimonitor_memory_total_bytes` or alerts
  on `pimonitor_openhab_things{status!="ONLINE"} > 0`, when they read `docs/integrations/openhab.md`, then they find
  both queries as examples.

## Errors and edge cases

- E1: Before the first openHAB poll has finished (`pending`, a few seconds after start), no `pimonitor_openhab_`
  series is written: an `up 0` would read as an outage that never happened.
- E2: A value openHAB did not deliver (a refused or failed source, heap used absent because free exceeds total, no
  `/proc/uptime`) has no series in that scrape; it is never written as `0`.
- E3: The Things source answered: every one of the seven counted statuses (`UNINITIALIZED`, `INITIALIZING`,
  `UNKNOWN`, `ONLINE`, `OFFLINE`, `REMOVING`, `REMOVED`) gets its series, with `0` for a status no Thing has, in that
  order, so a status that empties drops to `0` in Grafana instead of vanishing. With no Things at all the seven
  series read `0`. A Thing whose status is not one of the seven counts in no series, as in `by_status` of the JSON.
- E4: A hostile or odd `version` string (quotes, backslashes, a line feed, non-ASCII text) appears in the `info`
  label with `\`, `"` and line feed escaped as `\\`, `\"` and `\n`; the scrape stays one line per sample and parses.
- E5: `api_key` is set: `GET /metrics` without the key is refused exactly as today, so the openHAB values (Thing
  counts included) are as protected as in `GET /api/v1/openhab`.

## Non-functional requirements

- N1: No new configuration: the series follow `prometheus_enabled` and `openhab.enabled` as they are.
- N2: Bounded cardinality: at most one `info` series and seven `things` series per scrape; no per-Thing series and no
  label other than `status` and `version`.
- N3: A scrape reads the stored openHAB status only; it sends no request to openHAB and reads no file.
- N4: The existing families keep their names, labels, values and order; the openHAB families follow them, each
  preceded once by its `# HELP` and `# TYPE … gauge` lines.

## Acceptance criteria

- AC1: While the openHAB state is `not_installed` or `pending`, `GET /metrics` contains no line naming a
  `pimonitor_openhab_` metric, and the rest of its body is unchanged; `openhab.enabled: false` reports `not_installed` (S5, E1, N1, N4).
- AC2: In every other state, `pimonitor_openhab_up` is `1` when the state is `ok` and `0` otherwise, and
  `pimonitor_openhab_process_running` is `1` when the status has a process and `0` otherwise (S1–S4).
- AC3: `pimonitor_openhab_process_resident_bytes`, `…_jvm_heap_used_bytes`, `…_jvm_heap_total_bytes`,
  `…_uptime_seconds`, `…_start_level`, `…_items` and `…_addons_installed` are each written, unlabeled, exactly when the
  matching field of `GET /api/v1/openhab` (`process.rss_bytes`, `jvm.heap_used_bytes`, `jvm.heap_total_bytes`,
  `uptime_seconds`, `start_level`, `items_count`, `addons_installed`) of the stored status is present, with its value;
  the scrape itself polls nothing (S1–S4, E2, N3).
- AC4: When the status has Things, `pimonitor_openhab_things` has exactly seven series, one per counted status in
  the order of E3, each with its count from `by_status` or `0`; without Things it has none (S1, S2, E3, N2).
- AC5: When the status has a version, `pimonitor_openhab_info{version="<version>"} 1` is written with the label
  escaped as in E4; without a version it is not written (S1, S3, E4, N2).
- AC6: Each openHAB family that has a series is preceded once by `# HELP` and `# TYPE <name> gauge`, after the
  existing families, and none is written twice in one scrape (N4).

## Out of scope

- `pimonitor_openhab_jvm_heap_max_bytes` from the issue's table: openHAB reports no heap maximum, so #181 collects
  none ([openHAB](../../docs/areas/openhab.md), *Sources and values*), and this feature does no new collection.
- Per-Thing series (`thing_uid` label), the Things total, the RSS percentage of RAM and the `refused` list: the total
  is the `sum()` of the seven, the percentage a PromQL division (S6), the rest stays in the JSON API.
- A change to `GET /api/v1/openhab`, to the openHAB poll or to `/metrics` authentication (E5 is today's behavior).
- S6 is documentation, verified in review, and needs no criterion.

## Open questions

- Which states write series? Answered: the issue names only `not_installed`; `pending` is left out as well (E1),
  because before the first poll nothing is known and an `up 0` would be a fabricated reading — the rule of the
  issue and of [0010](../../docs/decisions/0010-missing-reading-left-out-of-prometheus.md).
- Does the uptime count as a process series in S3? Answered: yes; since #181 the uptime comes from the process
  ([0015](../../docs/decisions/0015-openhab-values-from-real-5x-responses.md)), so the issue's test "partial snapshot
  gives only the process series plus `up 0`" includes it.
- Only the statuses present, or all seven? Answered: from the code and the issue's own rule, `by_status` counts only
  the seven fixed statuses (`openHABThingStatuses`, `internal/collector/openhab.go`), so a `0` for a counted status
  that no Thing has is a real reading, not a fake one; writing it keeps a series from disappearing when the last
  OFFLINE Thing recovers (E3). The issue's "one series per status present" is refined to this.
- Is the `status` label untrusted? Answered: no; only the seven fixed names reach it. It is escaped like every label.
- Is the `version` label bounded? Answered: only by the 8 MiB response limit of the root call, as in the JSON;
  JSON decoding makes it valid UTF-8, and it is escaped (E4). No cap of its own.
- Does `/metrics` widen who sees Thing data? Answered: no; both routes sit behind the same `api_key` chain
  (`internal/httpapi/routes.go`), and `/metrics` carries counts only, no UID or label (E5).

## Area documents

- `docs/areas/http-api.md`, *Prometheus endpoint*: the labels `status` and `version` join the label list, and the
  openHAB families join the families left out without a reading (state `not_installed` or `pending`, a source that
  delivered nothing).
- `docs/areas/openhab.md`: a short paragraph that the status is also exported on `GET /metrics`, which values, and
  that a missing value has no series; *Not here* links `API.md` for the names.
- `docs/API.md`, *`GET /metrics`*: the new rows in the metrics table, with source field and when each is omitted.
- `docs/integrations/openhab.md`, *Prometheus alternative*: the two PromQL examples of S6.

## Challenge notes

I challenged the spec once (mode `spec`) against the code and docs. All three places where the Lead departs from the issue's wording hold up. I found four minor problems and nothing that would make the feature build the wrong thing.

**The three departures checked:**
- **Write all seven Thing statuses, with 0 when empty — sound.** `summarizeThings` counts only the fixed list `openHABThingStatuses` (`/home/user/PiMonitor/internal/collector/openhab.go:134`, `:511-512`). So a 0 for one of those statuses is a real count, not an invented one, and it stops a series from vanishing when the last OFFLINE Thing recovers.
- **Write no series while `pending` — sound.** Before the first poll, `Status()` returns only the base fields with state `pending` (`openhab.go:216-219`). Nothing has been measured yet, so `up 0` would be an invented reading. That breaks the issue's own "no fake zero" rule and decision record 0010.
- **Leave `heap_max` out — sound.** The fields the code reads from `systeminfo` have no maximum (`openhab.go:415-422`, `:499-501`). Decision record 0015 (lines 14 and 28) and `docs/areas/openhab.md:45` both say openHAB 5.2.4 reports no heap maximum, and this feature collects nothing new.

**Objections (all minor)**

1. **minor — AC4 contradicts E3 when there are no Things.** E3 says "With no Things at all the seven series read `0`". AC4 says "without Things it has none". When `/rest/things` returns an empty list, `summarizeThings` still returns a non-nil `Things` with an empty `by_status` (`openhab.go:507`), so that answer is a real reading. A Dev who checks `Things.Total > 0` would pass a literal reading of AC4 and still break E3.
   *Instruction:* read AC4's condition as "the Things source answered (`things` present in `GET /api/v1/openhab`, even with total 0)". The Tester adds a case with an empty Things list that expects all seven series at `0`, and a case with `Things == nil` that expects no `things` series.

2. **minor — E2's heap example hides that both heap series go together.** When free exceeds total, `applySystemInfo` leaves the whole `JVM` struct nil (`openhab.go:499-501`). So `jvm.heap_total_bytes` is missing as well as heap used, even though total was reported. `docs/areas/openhab.md:45` says only "Heap used is absent", which is misleading in the same way.
   *Instruction:* the Tester expects neither `pimonitor_openhab_jvm_heap_used_bytes` nor `…_jvm_heap_total_bytes` when `JVM` is nil. The Dev writes the two series together from `status.JVM != nil`. The `openhab.md` paragraph says "heap used and total are absent when free exceeds total".

3. **minor — the state rule only lists the states that are excluded.** AC1 and AC2 say "every other state" writes `up`/`process_running`. So a status whose `State` is anything else, including the empty string, would write `up 0`. The httpapi test fake returns a zero `OpenHABStatus` by default (`fakeMetrics.openhab`, `/home/user/PiMonitor/internal/httpapi/handlers_test.go:42-56`). Its `State` is `""`, so every existing `/metrics` test would start emitting openHAB lines.
   *Instruction:* the Dev writes openHAB series only for the states `installed_not_running`, `running_unreachable` and `ok`, and writes none for any other value. The Tester adds one case with `State: ""` that expects no `pimonitor_openhab_` line.

4. **minor — the S6 alert example fires forever when a Thing is disabled.** A disabled Thing is counted under `UNINITIALIZED`, but the dashboard card does not warn about it (`docs/areas/openhab.md`, *Not-ONLINE list*). So `pimonitor_openhab_things{status!="ONLINE"} > 0` stays firing on any install with a disabled Thing.
   *Instruction:* in `docs/integrations/openhab.md`, put a one-line note under the example. It should say that disabled Things count as `UNINITIALIZED` and keep the alert firing, and that operators can exclude that status if they disable Things on purpose.
