# Spec: Signal a never-run or failing apt updates check

Source: Issue #281
Status: Draft

## Problem

An operator who reads the apt update count from the dashboard, from `GET /api/v1/metrics` (the snapshot; the issue
calls it `/api/v1/snapshot`, a route that does not exist) or `GET /api/v1/metrics/updates`, or who scrapes
`pimonitor_updates_pending` from `GET /metrics`, cannot tell "no updates" from "no check result":

- Before the first successful check (from the start of the service until the first slow tick finishes, or forever on
  a host where every check fails) `updates.count` is 0 and `pimonitor_updates_pending` is written as `0`, a
  fabricated reading against the project's guarantee. The dashboard shows `0`, and because the JSON zero time
  `0001-01-01T00:00:00Z` is a non-empty string, it also shows a meaningless `Checked <time>` line.
- After a failed check every key keeps the last successful result ([Metrics](../../docs/areas/metrics.md#available-apt-updates)),
  which looks current everywhere; only `checked_at` (the time of the last success) hints at the failure.

## Scenarios

- S1: Given the latest apt check succeeded, when a client reads `updates`, then it carries `"check_status": "ok"`
  beside the unchanged `count`, `packages`, `cache_age_seconds`, `stale` and `checked_at`; `/metrics` writes
  `pimonitor_updates_pending <count>`; the Updates card shows the count, `Checked <time>`, the stale marker when
  `stale` is true and the package-list button when there are packages — as today.
- S2: Given no check has finished since the service started, when a client reads `updates`, then it carries
  `"check_status": "not_checked"` with today's zero values; `/metrics` has no `pimonitor_updates_pending` family (no
  `# HELP`, `# TYPE` or sample line); the Updates card shows `Not checked yet` instead of the count, no `Checked`
  line, no stale marker and no package-list button.
- S3: Given the latest check failed after an earlier success, when a client reads `updates`, then it carries
  `"check_status": "failed"` and still the last successful result in every other key; `/metrics` has no
  `pimonitor_updates_pending` family; the Updates card shows `Check failed` instead of the count, the line
  `Last successful check <time>` from `checked_at`, no stale marker and no package-list button.
- S4: Given every check since start failed, when a client reads `updates`, then it carries
  `"check_status": "failed"` with the zero values (`checked_at` the zero time); `/metrics` has no
  `pimonitor_updates_pending` family; the Updates card shows `Check failed` and no second line.
- S5: Given a check succeeds after failed ones, then S1 holds again with the new result.

## Errors and edge cases

- E1: A check that is still running keeps the status of the previous check (`not_checked` before the first one
  finishes); a status changes only when a check finishes.
- E2: One response never mixes two checks: `check_status` and the other `updates` keys come from the same state
  (no `ok` with the zero `checked_at`, no `not_checked` with a non-zero `checked_at`).
- E3: Every failure that fails the check today (the list in [Metrics](../../docs/areas/metrics.md#available-apt-updates),
  a timed-out or missing `apt-cache` included) gives `failed`; the warning `updates collection failed` is still logged
  as today.
- E4: A service restart starts at `not_checked` again; the status is not persisted.
- E5: The card's `<time>` is formatted as today's `Checked <time>` (browser-local time of day).

## Non-functional requirements

- N1: Compatibility: `check_status` is a new key of the `updates` object in `GET /api/v1/metrics` and
  `GET /api/v1/metrics/updates`, always present, with exactly the values `ok`, `not_checked` and `failed`
  ([decision 0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md)). Every existing key keeps
  its name, type, value and presence rule, so no `/api/v2/...` is needed.
- N2: `/metrics` leaves `pimonitor_updates_pending` out whenever the latest check has not succeeded, as the rule
  "a family whose source has no reading is left out" ([HTTP API](../../docs/areas/http-api.md#prometheus-endpoint-metrics))
  already does for the temperature, CPU and memory families; no new Prometheus family is added.
- N3: The dashboard renders the new texts with `textContent` only (Dashboard XSS security area).
- N4: No new option, log line, command or file read.

## Acceptance criteria

- AC1: `updates.check_status` in `GET /api/v1/metrics` and `GET /api/v1/metrics/updates` is `ok` after a successful
  latest check, `not_checked` before any check has finished, and `failed` after a failed latest check, while every
  other `updates` key keeps the value it has today in each case (S1, S2, S3, S4, S5, E1, E3, E4, N1).
- AC2: Within one snapshot, `check_status` and the other `updates` keys are set together, so no response carries
  `ok` with the zero `checked_at` or `not_checked` with a non-zero one (E2).
- AC3: `GET /metrics` writes the `pimonitor_updates_pending` family (with its `# HELP` and `# TYPE` lines) exactly
  when `check_status` is `ok`, with the value of `count` (S1, S2, S3, S4, N2).
- AC4: The Updates card shows, for `ok`, the count, `Checked <time>`, the stale marker when `stale` is true and the
  package-list button when there are packages; for `not_checked`, `Not checked yet` and nothing else; for `failed`,
  `Check failed`, and `Last successful check <time>` only when `checked_at` is not the zero time — never the count,
  the stale marker or the button outside `ok`, built without `.innerHTML` (S1, S2, S3, S4, S5, E5, N3).

## Out of scope

- An alert or webhook on a failed or missing check, and a Prometheus family for the check status or its time.
- Fewer warnings for repeated failures, and a failure's reason in the API.
- Any change to how the check works ([decision 0028](../../docs/decisions/0028-apt-updates-from-dumpavail-and-dpkg-status.md))
  and to the Pi-hole section of the card. N4 needs no criterion of its own.

## Open questions

- Should `/metrics` also signal a failed check (the issue's title), or only a never-run one (its proposal)? Answered:
  both — [HTTP API](../../docs/areas/http-api.md#prometheus-endpoint-metrics) leaves a family out while the latest
  collection of its source has not succeeded ([decision 0010](../../docs/decisions/0010-missing-reading-left-out-of-prometheus.md));
  a kept count is the issue's "looks current" problem.
- Does the dashboard show the last count after a failure? Answered: no; the issue asks to show the state "instead of
  the count". The last success's time stays visible (S3).
- One key or two (a flag for "never" and one for "failed")? Answered: one key with three values, since the states
  exclude each other; `checked_at` already tells a failure before any success (S4) from one after it (S3).

## Area documents

- `docs/areas/metrics.md`, *Available apt updates*: the three check states, when each holds, that a running check
  keeps the previous state and that a restart begins at `not_checked`.
- `docs/areas/http-api.md`, *Prometheus endpoint*: `pimonitor_updates_pending` joins the list of families left out
  while the latest check has not succeeded.
- `docs/areas/dashboard.md`: the Updates card per check state.
- Outside `docs/areas/`: `docs/API.md` (the key, its values and presence rule in the example and notes, the
  `updates_pending` row) and the apt line of `README.md`'s feature list.
