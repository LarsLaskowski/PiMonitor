# 0029: The apt check's state is a v1 key, and `/metrics` leaves the count out until a check succeeds

- **Status:** Accepted
- **Date:** 2026-11-27
- **Area:** Metrics
- **Source:** Issue #281
- **Supersedes:** —

## Context

Before the first successful apt check `updates.count` and `pimonitor_updates_pending` read 0, and after a failed
check every `updates` key keeps the last successful result ([0028](0028-apt-updates-from-dumpavail-and-dpkg-status.md)),
so neither JSON clients, the dashboard nor a Prometheus scraper can tell "no updates" from "no current result" — a
fabricated reading (`.squad/project.md`, *Guarantees*). The existing keys' values may not change in v1
([0018](0018-new-key-in-v1-response-is-no-shape-change.md)); `/metrics` already leaves out a family whose latest
collection did not succeed ([0010](0010-missing-reading-left-out-of-prometheus.md)).

## Options considered

1. **One new key `updates.check_status` (`ok`, `not_checked`, `failed`), existing keys unchanged, and
   `pimonitor_updates_pending` written only while it is `ok`** — additive in v1, one value per state, and the
   Prometheus side follows the documented "no reading, no family" rule; a transient failure leaves a gap of one slow
   interval.
2. **Two booleans (`never_checked`, `last_check_failed`)** — the states exclude each other, so two flags allow an
   impossible pair.
3. **openHAB's vocabulary (`state`, `pending`)** — `pending` reads as "updates pending" in this object, and `state`
   as the state of the updates, not of the check.
4. **Keep the last count in `/metrics` after a failure, omit it only before the first success** (the issue's
   proposal for `/metrics`) — the kept sample looks current, the problem the issue names; the JSON still has it.
5. **A new Prometheus family for the check (status or last-success time)** — no scraper asked for it; the log and
   the JSON key already name the failure.
6. **`count` as `null` or omitted while there is no result** — changes an existing key's type and presence rule,
   which needs `/api/v2/...`.

## Decision

Option 1: the `updates` object carries `check_status`, set together with the result on every finished check, and
the Prometheus renderer writes `pimonitor_updates_pending` only while it is `ok`. The rules are in
[Metrics](../areas/metrics.md#available-apt-updates) and [HTTP API](../areas/http-api.md#prometheus-endpoint-metrics).

## Consequences

- A scrape before the first check or after a failed one has no `pimonitor_updates_pending` sample; queries see a gap.
- JSON clients that read only `count` still see the kept or zero value; they need `check_status` to tell them apart
  (`docs/integrations/openhab.md` says so). The json_exporter example still rebuilds a count from `count`.
- The state is not persisted: a restart begins at `not_checked`.
