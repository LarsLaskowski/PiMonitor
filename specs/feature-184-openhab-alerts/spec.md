# Spec: Alert rules for a local openHAB

Source: Issue #184
Status: Approved by Lead

## Problem

An operator who runs openHAB on the Pi learns from the openHAB card ([openHAB](../../docs/areas/openhab.md)) that
openHAB stopped, that its JVM is eating the Pi's RAM or that a bridge Thing dropped off — but only while looking at
the dashboard. The alert engine ([Alerts](../../docs/areas/alerts.md)) evaluates only `cpu`, `temperature`, `memory`,
`swap` and `disk`, so none of this reaches `GET /api/v1/alerts` or a webhook. openHAB cannot report its own failure
when it is down or stuck, which is exactly when the operator needs to hear about it.

## Scenarios

- S1: Given openHAB is installed and alerts are on, when openHAB's process stops (card: `Not running`) or its REST API
  stops answering while the process runs (`Not reachable`) for `alerts.openhab_for_seconds`, then
  `GET /api/v1/alerts` reports `openhab_down` at `crit`, a `fired` event `ok -> crit` reaches the webhooks, and the
  dashboard shows a `crit` badge on the openHAB card and lists `openhab_down` in the banner. When openHAB answers
  again for that long, a `cleared` event `crit -> ok` follows and badge and banner go.
- S2: Given openHAB runs, when its process RSS stays at or above `thresholds.openhab_rss_warn_percent` (or
  `_crit_percent`) of the Pi's RAM for the debounce window, then `openhab_rss_percent` fires `warn` (or `crit`), the
  openHAB card shows the badge and its process-memory row takes the level's color; it clears the same way.
- S3: Given an openHAB API token is set (`openhab.api_token` or `PIMONITOR_OPENHAB_TOKEN`), when Things go offline and their count stays at or above
  `thresholds.openhab_things_offline_warn` (default 1) for the debounce window, then `openhab_things_offline` fires
  `warn` with the count as `value`; its event, in `GET /api/v1/alerts` and in the default webhook payload, lists the
  offline Things (UID, label, status, status detail), and the card's `Things offline` toggle takes the level's color.
- S4: Given a webhook with a custom `template`, when an `openhab_things_offline` event is delivered, then the template
  can name the offline Things through `.Things` (the ntfy example in `docs/integrations/openhab.md`).
- S5: Given the operator sets the new thresholds and `alerts.openhab_for_seconds`, when PiMonitor starts, then the
  rules use them, a threshold of `0` turns its cutoff off, and `GET /api/v1/config` reports the four new thresholds.
- S6: Given no openHAB is detected (`openhab.enabled: auto`), then no `openhab_*` state and no `openhab_*` event
  exists.

## Errors and edge cases

- E1: The Things call fails or is refused (the default install without a token, from `openhab.api_token` or
  `PIMONITOR_OPENHAB_TOKEN`, refuses it): the Things rule
  is not evaluated for that poll; its state stays as it was, or is absent when it was never evaluated.
- E2: Start level below 100 (openHAB starting): Things in `UNKNOWN` are not counted. When the start level is not
  reported (system info failed or refused), `UNKNOWN` Things are counted.
- E3: A disabled Thing (`UNINITIALIZED` with detail `DISABLED`) is never counted.
- E4: No process found, or RAM total unknown (no RSS percentage): the RSS rule is not evaluated for that poll; its state
  stays as it was.
- E5: Detection goes from present to `not_installed` (openHAB uninstalled): every `openhab_*` state is removed; one
  that was `warn` or `crit` first produces a `cleared` event to `ok` with its last `value` (and, for the Things rule,
  its last list), whose `at` is the time that poll finished (it has no `collected_at`). Reinstalled, the rules start again at `ok` with fresh timers.
- E6: State `pending` (no poll finished yet), `openhab.enabled: false` or `alerts.enabled: false`: no `openhab_*`
  state or event.
- E7: A Thing is offline for less than the window (a bridge reconnect): no event. The window counts from the first
  poll on a side of a cutoff, so it is confirmed only by a later poll at least `alerts.openhab_for_seconds` after it;
  `0` confirms on the first poll.
- E8: Fast ticks between two openHAB polls never re-evaluate the last poll, so they neither shorten the window nor
  produce events.
- E9: More than 10 Things counted: `value` is the full count of all Things of the poll; the event lists the first 10
  counted Things by UID. The card's `Things offline: <n>` keeps counting the Things the card lists (at most 10,
  `UNKNOWN` ones during startup included), so it can differ from the alert's `value`.
- E10: A Thing label or UID with quotes, backslashes, newlines, control characters or markup: the default payload
  stays valid JSON; a custom template renders it unchanged unless it applies `json`.
- E11: Any failure of the root call while the process runs — a timeout, a non-openHAB answer, a token refused with
  `401`/`403`, or a token connection closed by the owner check ([openHAB](../../docs/areas/openhab.md), *API
  token*) — is state `running_unreachable`, so `openhab_down` fires once it holds for the window.
- E13: Invalid options fail startup, naming the key: `thresholds.<key> must be a finite number` and
  `alerts.openhab_for_seconds must be a finite number` (NaN or infinite, checked first),
  `thresholds.<key> must be >= 0 (got <v>)`,
  `thresholds.<warn key> (<v>) must be <= thresholds.<crit key> (<v>)` (only when both are non-zero),
  `thresholds.openhab_things_offline_warn must be a whole number (got 1.5)` (and `_crit`), and
  `alerts.openhab_for_seconds must be >= 0 (got <v>)`.
- E14: Events are listed in the order they were produced. `at` rises within one metric; between an openHAB event
  and a fast-tick event it is not guaranteed to rise, since a poll and a fast tick finish independently.

## Non-functional requirements

- N1: The feature adds at most three alert states and no request to openHAB; it never delays the fast or slow tick;
  an event carries at most 10 Things; the event list stays capped at 100.
- N2: New options and defaults: `thresholds.openhab_rss_warn_percent` 50, `thresholds.openhab_rss_crit_percent` 75,
  `thresholds.openhab_things_offline_warn` 1, `thresholds.openhab_things_offline_crit` 0 (off),
  `alerts.openhab_for_seconds` 300. A configuration without them keeps starting. After an upgrade with alerts on,
  the openHAB rules run with these defaults, so the RSS defaults sit above the ratio of the repository's openHAB
  process sample (RSS 1,383,952 kB on a 4 GiB host, 33 %) and a healthy install of that size does not alert.
- N3: Thing data is untrusted input from openHAB: JSON-escaped in the default payload, never in a URL or log line,
  shown by the dashboard only as text. The `things` list of an event in `GET /api/v1/alerts` is governed by
  `api_key`, like `GET /api/v1/openhab`.
- N4: v1 compatibility ([0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md)): the new
  thresholds are new keys of `GET /api/v1/config`'s `thresholds`, `things` a new key of `openhab_things_offline`
  events only, and the new `metric` values new entries of `states` and `events`; no existing key or entry changes.
  The default payload's existing fields and `message` format are unchanged.
- N5: The existing thresholds keep their rule (a `0` is reached by every reading); only the four new ones treat `0`
  as off.

## Acceptance criteria

- AC1: `openhab_down` (no `resource`) has the value 1 for a poll in state `installed_not_running` or
  `running_unreachable` and 0 for `ok`; held for the window, 1 confirms `crit` and 0 confirms `ok`; it has no other
  level and no threshold (S1, E11).
- AC2: `openhab_rss_percent` takes the process RSS as a percentage of RAM and moves between `ok`, `warn` and `crit`
  against `openhab_rss_warn_percent`/`_crit_percent` by the transition table of
  [Alerts](../../docs/areas/alerts.md); a poll without that percentage leaves it unchanged (S2, E4).
- AC3: `openhab_things_offline` takes the number of Things whose status is not `ONLINE`, leaving out disabled Things
  and, while the reported start level is below 100, `UNKNOWN` Things, and moves between levels against
  `openhab_things_offline_warn`/`_crit`; a poll whose Things call failed or was refused leaves it unchanged
  (S3, E1, E2, E3, E9).
- AC4: For the four new thresholds a value of `0` is never reached: with only crit `0` the rule reaches at most
  `warn`; with only warn `0` it goes `ok -> crit` directly and `crit -> ok` once below crit is confirmed, never
  held at `crit` or `warn` by the off cutoff; with both `0` the metric has no state and no event (S5, N5).
- AC5: Each finished openHAB poll is evaluated exactly once, in the order the polls finished, also when the memory
  history drops waiting points; its `collected_at` is the sample time and `alerts.openhab_for_seconds` the window;
  fast ticks in between change no `openhab_*` state; within one poll the events come in the order `openhab_down`,
  `openhab_rss_percent`, `openhab_things_offline`; `events` stays in production order (E7, E8, E14, N1).
- AC6: There is no `openhab_*` state or event while the poll state is `pending` or `not_installed`, with
  `openhab.enabled: false` or `alerts.enabled: false`; a change to `not_installed` removes every `openhab_*` state,
  with a `cleared` event to `ok` for each one at `warn` or `crit`, at the time that poll finished (S6, E5, E6).
- AC7: The five new options have the defaults of N2 and are validated as E13 states, also while `alerts.enabled` is
  false; a configuration without them starts, and with it a poll of the repository's process sample (33 % of RAM)
  leaves `openhab_rss_percent` at `ok` (S5, E13, N2).
- AC8: `GET /api/v1/config` reports the four new keys in `thresholds`; `GET /api/v1/alerts` reports the `openhab_*`
  states and events, and every `openhab_things_offline` event carries `things`: up to 10 entries with `uid`, `label`,
  `status` and `status_detail`, sorted by UID, empty when none is counted (S3, S5, E9, N4).
- AC9: The default webhook payload of an `openhab_things_offline` event carries the same `things` array, with a hostile
  label JSON-escaped, and its `message` follows the existing format (`openhab_things_offline fired: ok -> warn (2.0)`);
  a custom template sees `.Things` with `.UID`, `.Label`, `.Status`, `.StatusDetail`, empty for other metrics
  (S3, S4, E10, N3).
- AC10: The dashboard lights a badge with the worst `openhab_*` level on the openHAB card, lists the metrics in the
  banner, and colors the card's process-memory row by the `openhab_rss_percent` level and its `Things offline` toggle
  by the `openhab_things_offline` level while that level is not `ok`. The card's existing warning state (at once on
  `Not running`, `Not reachable` or a listed Thing) and its `Things offline: <n>` stay as they are, beside the
  debounced badge (S1, S2, S3, E9).

## Out of scope

- One alert per Thing (unbounded state, noisy; the issue rejects it).
- Alerts on the JVM heap, add-ons, Items or the openHAB version.
- Exporting alert states to `GET /metrics`.
- Coloring openHAB values by the raw thresholds (the card follows the debounced alert only), and showing an event's
  Thing list on the dashboard.
- A change to the `0` rule of the existing thresholds (N5).

## Open questions

- Does `0` disable a rule "consistent with existing semantics", as the issue says? Answered: no — the claim is
  refuted by [Alerts](../../docs/areas/alerts.md) (*Raw level of a reading*) and `Engine.evalMetric` in
  `internal/alert/alert.go` (`value >= warn`): today `0` is reached by every reading. The issue needs `0` as off
  for the Things crit default, so the four new thresholds treat `0` as off and the existing ones are unchanged
  (N5); the plan records the choice.
- Are new `metric` values in `GET /api/v1/alerts` and `things` on events v1-compatible? Answered: the issue
  (repository owner) asks for them in v1; they add entries and a key, change no existing one, and the
  documented rule "do not assume every metric is always present" already holds — see N4; the plan records
  whether [0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md) (unreleased) covers new entries.
- Debounce against stale samples? Answered: evaluated per poll with `alerts.openhab_for_seconds`, default 300 as the
  issue proposes (AC5).
- `UNKNOWN` Things when the start level is unknown? Answered: counted (E2), since nothing says openHAB is starting.
- Things rule without an API token? Answered: openHAB 5.2.4 refuses the Things call without a token
  ([openHAB](../../docs/areas/openhab.md), *Refused sources*), so the rule needs `openhab.api_token` or
  `PIMONITOR_OPENHAB_TOKEN` (E1); the documentation says so.
- RSS defaults? Answered: the issue's 25/40 are examples ("e.g."); 33 % on the repository's 4 GiB sample would warn
  permanently, so the defaults are 50/75 (N2). The general `memory` alert (80/95) still covers the whole Pi.

## Area documents

- [Alerts](../../docs/areas/alerts.md): the three openHAB metrics in *Evaluated metrics*, their values and levels,
  the `0`-is-off rule of their thresholds, per-poll evaluation with `alerts.openhab_for_seconds`, event order, the
  `things` list, removal on `not_installed`.
- [Notifications](../../docs/areas/notifications.md): the new `metric` values, the `things` field of the default
  payload and `.Things` in templates.
- [Configuration](../../docs/areas/configuration.md): the five options with defaults, ranges and error texts.
- [Dashboard](../../docs/areas/dashboard.md): the openHAB card's badge and highlighted rows.
- [openHAB](../../docs/areas/openhab.md): a pointer to the alert rules.

## Challenge

- MAJOR 1 (RSS defaults fire on a healthy install): accepted — defaults 50/75 (N2), above the 33 % of the process
  sample (`writeOHJava`, `internal/collector/openhab_test.go`), which AC7 requires to stay `ok`. The issue's 25/40 are
  examples, so no Product Manager question.
- 2 (count and list not from today's poll result): accepted — E9 says count and list come from all Things of the poll
  and that the card's `Things offline: <n>` may differ; AC10 keeps the card's count. How the poll carries the full set
  is the plan's.
- 3 (time base of E5, event order): accepted — E5 and AC6 give the time the `not_installed` poll finished; E14 and AC5
  keep `events` in production order, with `at` rising only within one metric.
- 4 (bounded hand-over queue): accepted — AC5 requires every poll evaluated once, also when the memory history drops
  waiting points; the plan puts the evaluation outside that queue.
- 5 (finite check): accepted — E13 names both texts.
- 6 (warn `0`, crit non-zero): accepted — AC4 states `ok -> crit` and `crit -> ok`, with the off cutoff never holding
  a level; the plan names its test.
- 7 (E11 unproven): accepted — E11 reworded to any failure of the root call, refused token and owner
  check included; tested with the fake server.
- 8 (immediate card warning): accepted — AC10 keeps it.
- 9 (`PIMONITOR_OPENHAB_TOKEN`): accepted — S3 and E1 name both.
