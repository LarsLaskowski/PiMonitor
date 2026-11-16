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
| AC5 | `TestRenderOpenHABMetrics_InfoSeries_AC5` (plain version; hostile `"5.2\\4\"} 1\nfake 1"` expects `pimonitor_openhab_info{version="5.2\\\\4\\\"} 1\\nfake 1"} 1` as one line — written as Go escapes, so a lost backslash fails the `\\` replacement of `labelValueReplacer`; empty version: none) | `internal/httpapi/prometheus_test.go` |
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
  11. `pimonitor_openhab_info{version}` — `openHAB version; the value is always 1.` — `Version != ""`.
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
  statuses); `pimonitor_openhab_up 1` and one `things` line in the example. *`GET /api/v1/openhab`*: one sentence
  linking the Prometheus rows.
- `docs/areas/http-api.md`, *Prometheus endpoint*: "current snapshot" sentence as above; the label list gains
  `status` and `version`; the families left out gain the openHAB ones (outside the three states, absent field);
  *Related decisions* lists 0017.
- `docs/areas/openhab.md`: a *Prometheus* bullet under *Sources and values* (which values, absent means no series,
  link to `API.md`); in the systeminfo row "Heap used and total are absent when free exceeds total" (fact 6);
  *Limits and errors*: `GET /metrics` serves the Thing counts per status under the same `api_key`.
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

- [x] No new limits: `version` is bounded by the existing 8 MiB limit after decompression, before JSON decoding;
  label output uses the existing escaping.
- [x] No error reaches a user: rendering cannot fail.
- `status` labels come from the fixed list only; Thing UIDs and labels never reach `/metrics`.

## Decision records

- `docs/decisions/0017-openhab-thing-statuses-exported-as-fixed-set.md` (Proposed)

## Out of scope / follow-ups

Spec, *Out of scope*.

## Challenge notes

Devil's Advocate, mode `plan`, feature #182 (specs/feature-182-openhab-prometheus)

The plan holds up. I checked its verified facts against the code and they are accurate:
- `handlePrometheusMetrics` renders only the snapshot (`internal/httpapi/handlers.go:143-146`).
- `Collector.OpenHAB` returns `not_installed` when the poll is off (`internal/collector/collector.go:745-750`).
- `pending` carries only the base fields (`internal/collector/openhab.go:213-222`).
- `OpenHABJVM` has no heap maximum (`openhab.go:92-95`).
- `applySystemInfo` sets `JVM` only when free ≤ total (`openhab.go:492-502`).
- `summarizeThings` counts only the seven fixed statuses and returns a non-nil, empty `ByStatus` for an empty list (`openhab.go:134`, `506-516`).
- Uptime from `/proc` overrides the systeminfo value (`openhab.go:254-283`).
- `version` is set only after the root call succeeds (`pollREST`, `openhab.go:438-451`).
- `labelValueReplacer` escapes `\`, `"` and line feed (`internal/httpapi/prometheus.go`).
- `/metrics` and `/api/v1/openhab` both go through the `api: true` chain (`internal/httpapi/routes.go:66-85`).

Other checks:
- **Need:** the change is not already in the code.
- **Scope:** decision record 0017 exists. The documentation list covers every file that lists the Prometheus families: `docs/API.md`, `docs/areas/http-api.md`, `docs/integrations/openhab.md`, `README.md`, `SECURITY.md`, `docs/ARCHITECTURE.md`, and `.squad/project.md` via the orchestrator.
- **Tier:** `standard` fits. The feature reuses the existing escaping and the existing `api_key` chain without changing either, and *Security areas touched* is filled in, so Security's plan review runs.
- **Alternatives:** option 2 (a separate renderer joined by the handler) is the less invasive choice.
- **Target host / sample:** not applicable; there is no installer and no new parser.

Objections:

1. minor — Tester: the AC5 hostile-version expectation in *Tests per criterion* is written half as a Go escaped string. Its inner backslashes are escaped but the quotes around the label value are not, so copying it into an interpreted string literal won't compile, and copying it into a raw string gives the wrong bytes. Write the expected line as a raw-string literal of the exact output: `` `pimonitor_openhab_info{version="5.2\\4\"} 1\nfake 1"} 1` ``. I checked this against `labelValueReplacer` (`internal/httpapi/prometheus.go`) for the input `"5.2\\4\"} 1\nfake 1"`. Also assert that the output has exactly one line starting with `pimonitor_openhab_info`. Why minor: the intent is clear and only the test literal changes.

## Security notes

Security plan review: specs/feature-182-openhab-prometheus (spec.md and plan.md), mode `plan`

Files read: /home/user/PiMonitor/specs/feature-182-openhab-prometheus/spec.md, /home/user/PiMonitor/specs/feature-182-openhab-prometheus/plan.md, /home/user/PiMonitor/.squad/project.md (Security areas), /home/user/PiMonitor/.squad/routing.md (Tiers), /home/user/PiMonitor/SECURITY.md, /home/user/PiMonitor/docs/areas/openhab.md, /home/user/PiMonitor/internal/httpapi/prometheus.go, /home/user/PiMonitor/internal/httpapi/handlers.go, /home/user/PiMonitor/internal/httpapi/routes.go, /home/user/PiMonitor/internal/collector/openhab.go, /home/user/PiMonitor/internal/collector/collector.go, /home/user/PiMonitor/internal/config/config.go.

**Tier and listed areas.** `standard` is correct. The change reuses the existing `api_key` chain: `/metrics` and `/api/v1/openhab` are both `api: true` in `routes.go:61-86`. It reuses the existing `labelValueReplacer` and adds no route, listener or credential handling. The Thing counts per status are a subset of what `GET /api/v1/openhab` already serves under the same key. *Security areas touched* lists both affected areas (HTTP surface, openHAB API token), and the plan's doc updates to SECURITY.md and `.squad/project.md` cover the new exposure. I found no unlisted area.

**Checks that hold:**
- **Escaping.** The `status` label comes only from the fixed list of seven, so the only free-text label is `version`. The existing replacer escapes `\`, `"` and LF, which are exactly the characters the 0.0.4 text format requires escaped. A raw CR or NUL passes through, which the format allows inside a quoted label value.
- **UTF-8 measurement (fact 9).** Fact 9 says JSON decoding turns invalid UTF-8 in `version` into valid UTF-8, and the label's validity depends on that, so I measured it. I wrote a Go program at /tmp/claude-0/-home-user-PiMonitor/896d997e-3cf5-5d85-b755-590a08a9626a/scratchpad/m/main.go. It decodes `{"version":"5.2\xff\xfe\u0000\r\n\"x\\"}` with `encoding/json` and applies the same `strings.NewReplacer` as `prometheus.go:208`. Command: `go run .` in that folder. Output:
  `err=<nil> valid=true decoded="5.2��\x00\r\n\"x\\" escaped="5.2��\x00\r\\n\\\"x\\\\"`
  Fact 9 holds: invalid bytes become U+FFFD, and LF, quote and backslash are escaped.
- **Concurrency (N3).** `OpenHABCollector.Status` (`openhab.go:213-222`) returns the stored status under RLock. Each poll builds a new status and a new `ByStatus` map, so reading it during a scrape is the same as the JSON handler does today.
- **Missing readings.** Pending, unknown and empty states write nothing (E1, AC1), so no reading is fabricated.

**Findings**

1. [BLOCKING] The `version` label has no size bound that suits a series label, and its source is untrusted on a default install.
   - **Plan passages.** *Security considerations* says "`version` is bounded by the existing 8 MiB limit". The spec's *Open questions* says "Is `version` bounded? Answered: only by the 8 MiB limit of the root call". N2 counts "at most one `info`" series.
   - **What the code allows.** Without a token nothing checks who answers the REST call (`docs/areas/openhab.md:89`, "Without a token nothing is checked"). `poll` (`openhab.go:254-283`) sets state `ok` whenever the root call returns a non-empty `runtimeInfo.version` (`openhab.go:438-451`). So when openHAB is installed but down, any local user who binds its port chooses `version`: up to 8 MiB, and a new value on every poll (minimum interval 60 s, `config.go:197-200`).
   - **What is new.** Today that string only reaches the JSON and the dashboard (`textContent`), neither of which keeps it. With this change it becomes a Prometheus label. Prometheus keeps every distinct label value as its own series in its head block and index, and sets no default `label_value_length_limit`. The attacker can therefore add a new series of up to 8 MiB (up to 16 MiB once escaped) every minute, roughly 1 GB per 2-hour head block. Each scrape also renders and sends that body from the Pi.
   - **Why the plan misses it.** "At most one `info` series" holds per scrape, not over time. The 8 MiB limit bounds a parse step, not a stored label.
   - **Smallest fix.** Add a length cap to `renderOpenHABMetrics`:
     - Write `pimonitor_openhab_info` only when `len(status.Version)` is at most a fixed limit, e.g. 64 bytes; real versions such as `5.2.4` or `5.2.0-SNAPSHOT` are well below that.
     - A longer version writes no `info` series, as if it were absent (E2 semantics, so no fabricated or truncated value).
     - State the cap in E4/AC5/N2 and in the *Open questions* answer, and replace the plan's *Security considerations* bullet with it. Document it in `docs/API.md` and `docs/areas/http-api.md`.
     - Add AC5 test cases at exactly the limit (written) and one byte over (not written).
   - **Effect on tier.** A size bound is input handling, so the tier stays `standard`. Churn of short versions at one per poll remains possible, which matches any other version label and needs no further control.

2. [NON-BLOCKING] Note in the documentation that the openHAB series can come from an impostor.
   - **Problem.** Without a token, `up`, `items`, `addons_installed` and `things` can come from a process that is not openHAB, as the JSON already can. Operators who alert on these series in Prometheus should know.
   - **Smallest fix.** Add one sentence to the *Limits and errors* bullet the plan already adds to `docs/areas/openhab.md`: "Without an API token the REST values, and so their Prometheus series, come from whatever process answers on `base_url`."
   - **Recommendation:** fix now in this pull request; it is one sentence in a document the plan already edits.
