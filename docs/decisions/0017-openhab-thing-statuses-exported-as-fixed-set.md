# 0017: openHAB Thing counts are exported to Prometheus as the fixed set of seven statuses

- **Status:** Accepted
- **Date:** 2026-11-16
- **Area:** HTTP API
- **Source:** Issue #182
- **Supersedes:** —

## Context

Issue #182 exports the openHAB status of `GET /api/v1/openhab` on `GET /metrics`, and asks for the Thing counts as
"one series per status present". `by_status` in the JSON holds only the statuses some Thing has, but it counts only
seven fixed statuses (`openHABThingStatuses` in `internal/collector/openhab.go`); other statuses count in none. The
issue also forbids a fake `0` for a value that was not collected
([0010](0010-missing-reading-left-out-of-prometheus.md) states the same rule for CPU and memory). A Prometheus series
that stops being written turns stale and disappears from graphs, so a status whose last Thing recovers would show a
gap rather than a drop to zero.

## Options considered

1. **Only the statuses present, as in `by_status`** — mirrors the JSON and the issue's wording; a status that empties
   vanishes instead of reading `0`, and graphs and `sum()` over time show gaps.
2. **All seven statuses, `0` for an empty one, whenever the Things source answered** — a counted `0` is a real
   reading, not a fabricated one; the series set is constant (seven) and the cardinality bound unchanged.

## Decision

`pimonitor_openhab_things` carries one series for each of the seven counted statuses, in a fixed order, whenever the
Things source answered, and none when it did not. Behavior: [HTTP API](../areas/http-api.md#prometheus-endpoint-metrics)
and the metric table in [`API.md`](../API.md#get-metrics-prometheus).

## Consequences

- An emptied status drops to `0` in Grafana; an alert on `status!="ONLINE"` resolves instead of going stale.
- A status openHAB adds in a later release is not exported until the list in `openhab.go` gains it, as in the JSON.
- Seven series are written even on an install with few Things; the cost is constant and small.
