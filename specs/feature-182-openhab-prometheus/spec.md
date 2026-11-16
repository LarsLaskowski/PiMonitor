# Spec: Expose local openHAB status as Prometheus metrics

Source: Issue #182
Status: Approved by Lead

## Problem

Operators who scrape PiMonitor into Prometheus/Grafana (`GET /metrics`) see the openHAB status of #181 only in
`GET /api/v1/openhab` and on the dashboard, which keep no history; JVM memory leaks grow over days and go unnoticed
without a long-term series or a second exporter. Motivation and metric table: the issue.

## Scenarios

Values are those of the stored status that `GET /api/v1/openhab` serves ([openHAB](../../docs/areas/openhab.md)).

- S1: Given openHAB answers every source (state `ok`, token set), when Prometheus scrapes `GET /metrics`, then it also
  gets `pimonitor_openhab_up 1`, `…_process_running 1`, every value series of AC3, the `things` series and
  `pimonitor_openhab_info{version="…"} 1`.
- S2: Given a default install without a token (`ok`; system info, Things and add-ons refused), then it gets `up 1`,
  `process_running 1`, resident bytes, uptime, Items and `info`, and no heap, start level, Things or add-on series.
- S3: Given the process runs but the REST root does not answer (`running_unreachable`), then it gets `up 0`,
  `process_running 1`, resident bytes and uptime only.
- S4: Given openHAB is installed but not running (`installed_not_running`), then it gets `up 0` and
  `process_running 0` only.
- S5: Given no openHAB (`not_installed`, also reported with `openhab.enabled: false`), then the output has no
  `pimonitor_openhab_` line, as before.
- S6: An operator finds `pimonitor_openhab_process_resident_bytes / pimonitor_memory_total_bytes` and
  `pimonitor_openhab_things{status!="ONLINE"} > 0` as examples in `docs/integrations/openhab.md`.

## Errors and edge cases

- E1: Before the first poll (`pending`) no openHAB series is written: `up 0` would be an outage that never happened.
- E2: A value openHAB did not deliver (refused or failed source, no `/proc/uptime`) has no series; never a `0`.
  Heap used and total come as a pair: when free exceeds total neither is written.
- E3: When the Things source answered, each of the seven counted statuses (`UNINITIALIZED`, `INITIALIZING`, `UNKNOWN`,
  `ONLINE`, `OFFLINE`, `REMOVING`, `REMOVED`, in this order) has a series, `0` when no Thing has it, so an emptied
  status drops to `0` instead of vanishing; other statuses count in none, as in `by_status`.
- E4: A hostile `version` (quotes, backslashes, line feed) is escaped as `\\`, `\"`, `\n`; the scrape still parses.
  A `version` longer than 64 bytes writes no `info` series, as if absent: whoever answers on `base_url` without a
  token chooses it, and every distinct value is a series Prometheus stores.
- E5: With `api_key` set, `GET /metrics` stays refused without the key, as today.

## Non-functional requirements

- N1: No new configuration.
- N2: At most one `info` and seven `things` series, the `version` label at most 64 bytes; no per-Thing series, no
  label besides `status` and `version`.
- N3: A scrape reads the stored status only; no request to openHAB.
- N4: Existing families are unchanged and come first; each openHAB family has one `# HELP`/`# TYPE … gauge` pair.

## Acceptance criteria

- AC1: While the openHAB state is none of `installed_not_running`, `running_unreachable` and `ok` (so `not_installed`,
  `pending`, or any other value, the empty one included), `GET /metrics` contains no line naming a
  `pimonitor_openhab_` metric, and the rest of its body is unchanged; `openhab.enabled: false` reports `not_installed` (S5, E1, N1, N4).
- AC2: In those three states, `pimonitor_openhab_up` is `1` when the state is `ok` and `0` otherwise, and
  `pimonitor_openhab_process_running` is `1` when the status has a process and `0` otherwise (S1–S4).
- AC3: `pimonitor_openhab_process_resident_bytes`, `…_jvm_heap_used_bytes`, `…_jvm_heap_total_bytes`,
  `…_uptime_seconds`, `…_start_level`, `…_items` and `…_addons_installed` are each written, unlabeled, exactly when the
  matching field of `GET /api/v1/openhab` (`process.rss_bytes`, `jvm.heap_used_bytes`, `jvm.heap_total_bytes`,
  `uptime_seconds`, `start_level`, `items_count`, `addons_installed`) of the stored status is present, with its value;
  the scrape itself polls nothing (S1–S4, E2, N3).
- AC4: When the Things source answered (`things` present in the status, also with total 0), `pimonitor_openhab_things` has exactly seven series, one per counted status in
  the order of E3, each with its count from `by_status` or `0`; without `things` it has none (S1, S2, E3, N2).
- AC5: When the status has a version of at most 64 bytes, exactly one `pimonitor_openhab_info{version="<version>"} 1`
  line is written with the label escaped as in E4; without a version, or with a longer one, it is not written (S1, S3, E4, N2).
- AC6: Each openHAB family that has a series is preceded once by `# HELP` and `# TYPE <name> gauge`, after the
  existing families, and none is written twice in one scrape (N4).

## Out of scope

- `pimonitor_openhab_jvm_heap_max_bytes`: openHAB reports no heap maximum ([openHAB](../../docs/areas/openhab.md)),
  and this feature collects nothing new.
- Per-Thing series, the Things total (`sum()` of the seven), the RSS percentage (a PromQL division) and `refused`.
- Changes to `GET /api/v1/openhab`, the poll or `/metrics` authentication (E5).
- S6 is documentation, verified in review.

## Open questions

- Which states write series? Answered: not `pending` either (E1); an `up 0` there is a fabricated reading
  ([0010](../../docs/decisions/0010-missing-reading-left-out-of-prometheus.md)). Uptime comes from the process
  ([0015](../../docs/decisions/0015-openhab-values-from-real-5x-responses.md)), so S3 includes it.
- All seven statuses or only those present? Answered: all seven (E3). `by_status` counts only the fixed list
  `openHABThingStatuses` (`internal/collector/openhab.go`), so a `0` is a real reading; this refines the issue's
  "one series per status present". Only those seven names reach the `status` label.
- Is `version` bounded? Answered: the label is, at 64 bytes before escaping (E4, Security review 1); the JSON keeps
  the 8 MiB limit of the root call. JSON decoding makes it valid UTF-8.
- Does `/metrics` widen who sees Thing data? Answered: no; same `api_key` chain (`internal/httpapi/routes.go`), and
  counts only.

## Area documents

- `docs/areas/http-api.md`, *Prometheus endpoint*: labels `status` and `version`; the openHAB families among those
  left out without a reading; the 64-byte cap on `version`.
- `docs/areas/openhab.md`: the status is also exported on `GET /metrics`; a missing value has no series.
- `docs/API.md`, *`GET /metrics`*: the new rows with source field and when each is omitted.
- `docs/integrations/openhab.md`, *Prometheus alternative*: the examples of S6.

## Challenge

The Devil's Advocate found the three deviations from the issue sound (all seven statuses, nothing while `pending`, no
heap maximum) and raised four minor objections.

1. AC4 contradicted E3 for an empty Things list. Accepted: AC4 now applies whenever `things` is present, total 0
   included; the Tester covers an empty list (seven `0` series) and a nil `Things` (none).
2. E2 hid that the heap series go together. Accepted: E2 says so; AC3 already follows the `jvm` object, which is
   absent as a whole when free exceeds total. The plan corrects the same wording in `docs/areas/openhab.md`.
3. The state rule listed only the excluded states, so the zero `OpenHABStatus` of the test fake would emit series.
   Accepted: AC1 and AC2 name the three states that write series; any other value, the empty one included, writes
   none, with a test for `State: ""`.
4. The S6 alert keeps firing for a disabled Thing (counted as `UNINITIALIZED`). Accepted: the integration guide adds
   a note under the example saying so and how to exclude that status.

## Amendments

| Criterion | Change | Reason | Step | Decided by |
| --------- | ------ | ------ | ---- | ---------- |
| AC5 | `info` written only for a version of at most 64 bytes, exactly one line | Security plan review, finding 1: an attacker-chosen version as an unbounded Prometheus label | 3 | Lead |
