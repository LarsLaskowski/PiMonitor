# Plan: Expose local openHAB status as Prometheus metrics

Source: Issue #182 | [spec.md](spec.md)
Tier: standard — a behavior change to `GET /metrics` that creates, changes or removes no security control (it reuses the existing label escaping and the existing `api_key` chain).
Security areas touched: HTTP surface (`internal/httpapi`, `GET /metrics`); openHAB API token (Thing counts that openHAB shows only to authenticated users now also reach `GET /metrics`)

## Problem / root cause

See [spec.md](spec.md), *Problem*.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | `GET /metrics` renders only the snapshot | `handlePrometheusMetrics`, `internal/httpapi/handlers.go:143-146`, calls `renderPrometheusMetrics(s.metrics.Snapshot())` | confirmed |
| 2 | The HTTP layer already has the openHAB status | `MetricsProvider.OpenHAB()`, `internal/httpapi/server.go:36`; `fakeMetrics.openhab` in `handlers_test.go` | confirmed |
| 3 | `openhab.enabled: false` reports `not_installed` | `Collector.OpenHAB`, `internal/collector/collector.go:745-750` | confirmed |
| 4 | `pending` carries only base fields | `OpenHABCollector.Status`, `internal/collector/openhab.go:213-222` | confirmed |
| 5 | Issue: `jvm.heap_max_bytes` exists "if available" | `OpenHABJVM` has only used and total (`openhab.go:92-95`); `docs/areas/openhab.md` says openHAB reports no maximum | refuted |
| 6 | Heap used and total are absent together when free exceeds total | `applySystemInfo`, `openhab.go:492-502` sets `JVM` only when free ≤ total; `docs/areas/openhab.md` says only "Heap used is absent" (doc defect) | confirmed |
| 7 | `by_status` counts only seven fixed statuses and holds only those seen; an empty Things list gives a non-nil `Things` with an empty map | `openHABThingStatuses` `openhab.go:134`; `summarizeThings` `openhab.go:506-516` | confirmed |
| 8 | Uptime comes from the process when one is found | `poll`, `openhab.go:254-283`; record 0015 | confirmed |
| 9 | `version` is non-empty only in state `ok`, valid UTF-8, bounded by the 8 MiB body limit | `pollREST` `openhab.go:438-451`; `json.Unmarshal` replaces invalid UTF-8 with U+FFFD; `openHABMaxBody` | confirmed |
| 10 | The existing escaping covers `\`, `"` and line feed | `labelValueReplacer`, `internal/httpapi/prometheus.go` | confirmed |
| 11 | `/metrics` and `/api/v1/openhab` share the `api: true` chain | `routeTable`, `internal/httpapi/routes.go:61-86` | confirmed |
| 12 | The test fake's zero `OpenHABStatus` has `State: ""` | `newTestServer`, `handlers_test.go` | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestRenderOpenHABMetrics_NoSeriesOutsideReportedStates_AC1` (subtests `not_installed`, `pending`, `""`, `bogus`, each with values set: empty output); `TestHandlePrometheusMetrics_OpenHABNotInstalledKeepsBody_AC1` (body equals `renderPrometheusMetrics` of the snapshot, byte for byte) | `internal/httpapi/prometheus_test.go`; `internal/httpapi/handlers_test.go` |
| AC2 | `TestRenderOpenHABMetrics_StateGauges_AC2` (`ok`+process 1/1, `ok` without process 1/0, `running_unreachable` 0/1, `installed_not_running` 0/0) | `internal/httpapi/prometheus_test.go` |
| AC3 | `TestRenderOpenHABMetrics_FullStatus_AC3` (every value series with its exact value, a byte value above 2^32 included); `TestRenderOpenHABMetrics_PartialStatus_AC3` (S2 default install, S3 unreachable, `JVM` nil: neither heap series) | `internal/httpapi/prometheus_test.go` |
| AC4 | `TestRenderOpenHABMetrics_ThingsSeries_AC4` (mixed counts: seven lines in E3 order with zeros; empty list: seven `0`; `Things` nil: none; a `by_status` key outside the seven: not written) | `internal/httpapi/prometheus_test.go` |
| AC5 | `TestRenderOpenHABMetrics_InfoSeries_AC5`: plain version; hostile input `"5.2\\4\"} 1\nfake 1"` (interpreted literal) expects the raw-string line `` `pimonitor_openhab_info{version="5.2\\4\"} 1\nfake 1"} 1` ``, so a lost backslash fails the `\\` replacement of `labelValueReplacer`; every case asserts exactly one line starting `pimonitor_openhab_info`, or none where not written; `strings.Repeat("9", 64)` written, `strings.Repeat("9", 65)` not; 64 `"` characters written (the cap counts bytes before escaping); empty version: none | `internal/httpapi/prometheus_test.go` |
| AC6 | `TestRenderOpenHABMetrics_HelpAndType_AC6` (full status: each family once, `# HELP` then `# TYPE <name> gauge` before its first sample, families in the order of *Approach*); `TestHandlePrometheusMetrics_OpenHABAfterExistingFamilies_AC6` (handler, state `ok`: the first `pimonitor_openhab_` line follows the last existing sample) | `internal/httpapi/prometheus_test.go`; `internal/httpapi/handlers_test.go` |

The Tester builds statuses from `collector.OpenHABStatus` literals; the handler tests set `fakeMetrics.openhab`.
Spec scenario S6 is documentation, checked by the Reviewer.

## Approach

- In `internal/httpapi/prometheus.go` add `renderOpenHABMetrics(status collector.OpenHABStatus) []byte`. It returns
  nil unless `status.State` is `collector.OpenHABStateNotRunning`, `OpenHABStateUnreachable` or `OpenHABStateOK`.
  Split it into small writers (state gauges, value gauges, Things, info) so each stays under `gocognit` 15.
- `handlePrometheusMetrics` writes `renderPrometheusMetrics(s.metrics.Snapshot())` followed by
  `renderOpenHABMetrics(s.metrics.OpenHAB())` as one body (append, then one `Write`). `renderPrometheusMetrics` and
  its tests stay unchanged. The scrape reads the stored status only (N3).
- Families, unlabeled unless noted, in this order, each with `writeGaugeHeader` and these HELP texts (layout fixed
  here, no other text):
  1. `pimonitor_openhab_up` — `Whether the openHAB REST API answered (1) or not (0).` — always (in the three states).
  2. `pimonitor_openhab_process_running` — `Whether the openHAB Java process was found (1) or not (0).` — always.
  3. `pimonitor_openhab_process_resident_bytes` — `Resident memory of the openHAB process in bytes.` — `Process != nil`.
  4. `pimonitor_openhab_jvm_heap_used_bytes` — `openHAB JVM heap used in bytes.` — `JVM != nil`.
  5. `pimonitor_openhab_jvm_heap_total_bytes` — `openHAB JVM heap total in bytes.` — `JVM != nil`.
  6. `pimonitor_openhab_uptime_seconds` — `Seconds since the openHAB process started.` — `UptimeSeconds != nil`.
  7. `pimonitor_openhab_start_level` — `openHAB start level.` — `StartLevel != nil`.
  8. `pimonitor_openhab_things{status}` — `Number of openHAB Things per status.` — `Things != nil`; seven
     samples in the order of `collector`'s status list, `0` for a missing key ([0017](../../docs/decisions/0017-openhab-thing-statuses-exported-as-fixed-set.md)).
  9. `pimonitor_openhab_items` — `Number of openHAB Items.` — `ItemsCount != nil`.
  10. `pimonitor_openhab_addons_installed` — `Number of installed openHAB add-ons.` — `AddonsInstalled != nil`.
  11. `pimonitor_openhab_info{version}` — `openHAB version; the value is always 1.` — `Version != ""` and
      `len(Version) <= openHABMaxVersionLabel`, an unexported constant `64` in `prometheus.go` (bytes, before escaping).
- The seven statuses: `openHABThingStatuses` is unexported in `collector`. Export it as a function (*Signatures*)
  so the order lives in one place; the summarizer keeps using it.

## Options considered

1. **Add an `OpenHABStatus` parameter to `renderPrometheusMetrics`** — one function, but every existing call in
   `prometheus_test.go` (about ten) changes for no behavior of its own. Lost to option 2.
2. **A separate `renderOpenHABMetrics`, joined by the handler** — chosen: existing renderer and tests untouched,
   the openHAB part testable alone; the join is covered by the two handler tests.
3. **Only the statuses present** (the issue's wording) — lost; see [0017](../../docs/decisions/0017-openhab-thing-statuses-exported-as-fixed-set.md).
4. **Per-Thing series** and **a separate exporter** (issue, *Alternatives*) — rejected there: unbounded, untrusted
   labels; extra setup without the OS view of RSS.
5. **Copy the status list into `httpapi`** — two lists drift apart when openHAB adds a status; lost to the export.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `openhab.go` | `OpenHABThingStatuses()` exported accessor for the fixed list |
| `internal/httpapi` | `prometheus.go` | `renderOpenHABMetrics` and its writers |
| `internal/httpapi` | `handlers.go` | `handlePrometheusMetrics` appends the openHAB part; doc comment names it |

## Signatures (for the Dev's skeleton)

- `func OpenHABThingStatuses() []string` in `internal/collector/openhab.go` — returns a copy of the seven counted
  statuses in their fixed order; doc comment starts with the name.

`renderOpenHABMetrics` and its writers are unexported (described in *Approach*). Every name above passes the
analyzer rules of the stack's *Writing code* and the golangci configuration: no stutter, no parameters.

## Test files

- `internal/httpapi/prometheus_test.go`, `internal/httpapi/handlers_test.go` (new tests only);
  `internal/collector/openhab_test.go` gets `TestOpenHABThingStatuses` (order and copy semantics), owner Tester.
- Existing call sites of a changed signature: none (`renderPrometheusMetrics` and `handlePrometheusMetrics` keep
  theirs). Existing `/metrics` tests keep passing unchanged: their fake status has `State: ""` (fact 12).
- Files no unit test can cover: none.

## Areas

- HTTP API (`docs/areas/http-api.md`): `/metrics` also carries the openHAB status.
- openHAB (`docs/areas/openhab.md`): the status is exported on `/metrics`.

## Documentation updates

All by the Dev, once the code is green:

- `docs/API.md`, *`GET /metrics`*: first sentence "the current snapshot and the openHAB status"; the eleven rows of
  *Approach* in the metrics table, with source field, label and when omitted (state rule, absent field, seven
  statuses, `info` only for a version of at most 64 bytes); `pimonitor_openhab_up 1` and one `things` line in the example. *`GET /api/v1/openhab`*: one sentence
  linking the Prometheus rows.
- `docs/areas/http-api.md`, *Prometheus endpoint*: "current snapshot" sentence as above; the label list gains
  `status` and `version`; the families left out gain the openHAB ones (outside the three states, absent field, `info` for a
  version over 64 bytes);
  *Related decisions* lists 0017.
- `docs/areas/openhab.md`: a *Prometheus* bullet under *Sources and values* (which values, absent means no series,
  link to `API.md`); in the systeminfo row "Heap used and total are absent when free exceeds total" (fact 6);
  *Limits and errors*: `GET /metrics` serves the Thing counts per status under the same `api_key`, and "Without an API
  token the REST values, and so their Prometheus series, come from whatever process answers on `base_url`."
- `docs/integrations/openhab.md`, *Prometheus alternative*: the two PromQL examples of spec S6 and a note that a
  disabled Thing counts as `UNINITIALIZED` and keeps the alert firing, with `status!~"ONLINE|UNINITIALIZED"` as the
  exclusion.
- `README.md`, Prometheus feature bullet: "the current snapshot and the local openHAB status".
- `SECURITY.md`, openHAB token bullet: after the Thing UIDs sentence, "`GET /metrics` serves their counts per status".
- `docs/ARCHITECTURE.md`, openHAB poll paragraph: "`GET /api/v1/openhab` and `GET /metrics` serve the last finished
  result".
- `.squad/project.md`, *openHAB API token* line: the Dev reports this text for the orchestrator to write — after "so
  `api_key` governs who reads it" add "; `GET /metrics` serves the Thing counts per status under the same key".

## Architecture check

No guarantee weakened: no fabricated reading (absent values and pre-poll states write no series), `/api/v1`
shapes unchanged, no dependency, no new route; the scrape reads stored state, so the openHAB loop stays off the
request path (0014).

## Security considerations

- [x] The `version` label is capped at 64 bytes of the decoded string, before escaping; a longer one writes no
  `info` series. The 8 MiB limit of the root call applies after decompression, before JSON decoding. Label output
  uses the existing escaping.
- [x] No error reaches a user: rendering cannot fail.
- `status` labels come from the fixed list only; Thing UIDs and labels never reach `/metrics`.

## Decision records

- `docs/decisions/0017-openhab-thing-statuses-exported-as-fixed-set.md` (Proposed)

## Out of scope / follow-ups

Spec, *Out of scope*.

## Challenge

1. minor (Devil's Advocate) — the AC5 hostile-version expectation was half escaped. Accepted: *Tests per criterion*
   gives the input as an interpreted literal and the expected line as a raw string, checked against
   `labelValueReplacer`, and every AC5 case asserts exactly one (or no) `pimonitor_openhab_info` line.

## Security review

1. BLOCKING — the `version` label had no size bound fit for a stored series label, and without a token any local
   process answering on `base_url` chooses it. Resolved: `info` is written only for a version of at most 64 bytes
   (unexported constant `openHABMaxVersionLabel` in `prometheus.go`), none otherwise, never a truncated value. AC5
   amended (spec *Amendments*); E4, N2 and the open question updated; boundary cases of 64 and 65 bytes and a 64-byte
   all-quote version added to the AC5 test; the cap documented in `docs/API.md` and `docs/areas/http-api.md`. Tier
   stays `standard`: a size bound is input handling. Accepted forms re-checked: any decoded string of 1 to 64 bytes
   is written escaped (backslash, quote and LF escaped; CR, NUL and U+FFFD pass, which the format allows inside a
   quoted value); an empty one or one over 64 bytes writes nothing. Churn of short versions, one per poll, remains,
   as for any version label.
2. NON-BLOCKING — impostor sentence. Accepted: added to the `docs/areas/openhab.md` edit under *Documentation updates*.
