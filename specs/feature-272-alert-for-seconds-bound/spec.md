# Spec: Upper bound for alerts.for_seconds and alerts.openhab_for_seconds

Source: Issue #272
Status: Draft

## Problem

An operator who sets `alerts.for_seconds` or `alerts.openhab_for_seconds` to a huge value (a typo such as `1e12`)
gets no startup error: `Config.Validate()` checks only `>= 0` and finiteness (`internal/config/config.go`), and
`AlertFor()` / `OpenHABAlertFor()` convert the seconds to a `time.Duration` with no upper bound. A product above
`math.MaxInt64` nanoseconds (about 292 years) converts to a platform-dependent value; on amd64 it is negative, and
the engine's `now.Sub(since) >= window` (`internal/alert/alert.go`) then holds on the first sample, so the debounce
the operator asked for silently becomes none: every spike fires.

## Scenarios

none (small feature: each criterion is its own scenario).

## Errors and edge cases

- E1: A value above the bound (`alerts.for_seconds: 1e12`) ends startup with
  `pimonitor: load config: alerts.for_seconds must be <= 604800 (got 1e+12)`, exit status 1, before any port opens.
- E2: The same for `alerts.openhab_for_seconds`:
  `pimonitor: load config: alerts.openhab_for_seconds must be <= 604800 (got <value>)`, also while
  `alerts.enabled` or `openhab.enabled` is `false`.
- E3: A value equal to the bound (`604800`) is valid and gives a debounce window of exactly 7 days.
- E4: Negative, NaN and infinite values keep their current messages
  (`… must be >= 0 (got <value>)`, `… must be a finite number`); the finiteness check still runs first.

## Non-functional requirements

- N1: Compatibility: a configuration that is valid today and holds both keys at or below the bound stays valid with
  the same window; only values above the bound, which today are accepted, now fail startup.
- N2: No new configuration option; the bound is fixed in code.

## Acceptance criteria

- AC1: `alerts.for_seconds` above 604800 fails `Config.Validate()` with
  `alerts.for_seconds must be <= 604800 (got <value>)`; `604800` passes and `AlertFor()` returns 168 h (E1, E3, E4,
  N1).
- AC2: `alerts.openhab_for_seconds` above 604800 fails `Config.Validate()` with
  `alerts.openhab_for_seconds must be <= 604800 (got <value>)`, also with `alerts.enabled: false`; `604800` passes and
  `OpenHABAlertFor()` returns 168 h (E2, E3, E4, N1).
- AC3: `pimonitor` started with a config file holding `alerts.for_seconds: 1e12` writes the one line of E1 to
  standard error and exits with status 1 (E1, N2).

## Out of scope

- The same unbounded float-to-`time.Duration` conversion of `poll_interval_seconds`, `updates_check_minutes`,
  `updates_stale_threshold_minutes`, `healthz_max_staleness_seconds`, `history_window_minutes`
  (`internal/config/config.go`), `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds` and
  `alerts.webhooks[].timeout_seconds` (`internal/alert/notify.go`): a related defect, proposed as a follow-up issue.
- A bound tied to another option (for example a multiple of `openhab.poll_interval_seconds`).

## Open questions

- Q1: Which upper bound, and one for both keys? Proposed: 604800 s (7 days) for both — far below the overflow,
  above `openhab.poll_interval_seconds`' maximum of 86400 so `alerts.openhab_for_seconds` can still span several
  polls, and equal to the `openhab.history_window_hours` maximum (168 h). Not answered: escalated to the Product
  Manager, since it decides which of today's accepted configurations stop starting.
- Q2: Keep the existing `>= 0` message and add a separate `<=` message, or one `must be between 0 and <bound>` text
  for both? Answered: keep the existing documented negative message unchanged (`docs/areas/configuration.md`) and add
  the `<=` message, so no documented text changes.

## Area documents

- `docs/areas/configuration.md`: the *Valid values* of both keys in the `alerts` table read "0 to 604800; `0` fires
  on the first crossing"; *Validation and startup failure* adds the `alerts.for_seconds must be <= 604800 (got
  <value>)` and `alerts.openhab_for_seconds must be <= 604800 (got <value>)` texts beside the existing `>= 0` ones.
- `packaging/pimonitor.example.yaml` (not an area document): the comments of both keys name the maximum.
