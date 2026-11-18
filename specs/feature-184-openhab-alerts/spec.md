# Spec: Alert rules for a local openHAB

Source: Issue #184
Status: Draft

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
- S3: Given an `openhab.api_token` is set, when Things go offline and their count stays at or above
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

- E1: The Things call fails or is refused (the default install without an `api_token` refuses it): the Things rule
  is not evaluated for that poll; its state stays as it was, or is absent when it was never evaluated.
- E2: Start level below 100 (openHAB starting): Things in `UNKNOWN` are not counted. When the start level is not
  reported (system info failed or refused), `UNKNOWN` Things are counted.
- E3: A disabled Thing (`UNINITIALIZED` with detail `DISABLED`) is never counted.
- E4: No process found, or RAM total unknown (no RSS percentage): the RSS rule is not evaluated for that poll; its state
  stays as it was.
- E5: Detection goes from present to `not_installed` (openHAB uninstalled): every `openhab_*` state is removed; one
  that was `warn` or `crit` first produces a `cleared` event to `ok` with its last `value` (and, for the Things rule,
  its last list). Reinstalled, the rules start again at `ok` with fresh timers.
- E6: State `pending` (no poll finished yet), `openhab.enabled: false` or `alerts.enabled: false`: no `openhab_*`
  state or event.
- E7: A Thing is offline for less than the window (a bridge reconnect): no event. The window counts from the first
  poll on a side of a cutoff, so it is confirmed only by a later poll at least `alerts.openhab_for_seconds` after it;
  `0` confirms on the first poll.
- E8: Fast ticks between two openHAB polls never re-evaluate the last poll, so they neither shorten the window nor
  produce events.
- E9: More than 10 Things counted: `value` is the full count; the event lists the first 10 by UID.
- E10: A Thing label or UID with quotes, backslashes, newlines, control characters or markup: the default payload
  keeps valid JSON with the string escaped; it never goes into a webhook URL or a log line; a custom template renders
  it unchanged unless the template applies `json`.
- E11: An API token openHAB rejects makes the root call fail (state `running_unreachable`), so `openhab_down` fires.
- E12: `openhab.enabled: true` without any openHAB installed: state `installed_not_running`, so `openhab_down` fires.
- E13: Invalid options fail startup, naming the key: `thresholds.<key> must be >= 0 (got <v>)`,
  `thresholds.<warn key> (<v>) must be <= thresholds.<crit key> (<v>)` (only when both are non-zero),
  `thresholds.openhab_things_offline_warn must be a whole number (got 1.5)` (and `_crit`), and
  `alerts.openhab_for_seconds must be >= 0 (got <v>)`.

## Non-functional requirements

- N1: The feature adds at most three alert states and no request to openHAB; it never delays the fast or slow tick;
  an event carries at most 10 Things; the event list stays capped at 100.
- N2: New options and defaults: `thresholds.openhab_rss_warn_percent` 25, `thresholds.openhab_rss_crit_percent` 40,
  `thresholds.openhab_things_offline_warn` 1, `thresholds.openhab_things_offline_crit` 0 (off),
  `alerts.openhab_for_seconds` 300. A configuration without them keeps starting. After an upgrade with alerts on,
  the openHAB rules run with these defaults.
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
  level and no threshold (S1, E11, E12).
- AC2: `openhab_rss_percent` takes the process RSS as a percentage of RAM and moves between `ok`, `warn` and `crit`
  against `openhab_rss_warn_percent`/`_crit_percent` by the transition table of
  [Alerts](../../docs/areas/alerts.md); a poll without that percentage leaves it unchanged (S2, E4).
- AC3: `openhab_things_offline` takes the number of Things whose status is not `ONLINE`, leaving out disabled Things
  and, while the reported start level is below 100, `UNKNOWN` Things, and moves between levels against
  `openhab_things_offline_warn`/`_crit`; a poll whose Things call failed or was refused leaves it unchanged
  (S3, E1, E2, E3, E9).
- AC4: For the four new thresholds a value of `0` is never reached: with only crit `0` the rule reaches at most
  `warn`, with both `0` the metric has no state and no event (S5, N5).
- AC5: Each finished openHAB poll is evaluated exactly once, with its `collected_at` as the sample time and
  `alerts.openhab_for_seconds` as the window; fast ticks in between change no `openhab_*` state; within one poll the
  events come in the order `openhab_down`, `openhab_rss_percent`, `openhab_things_offline` (E7, E8, N1).
- AC6: There is no `openhab_*` state or event while the poll state is `pending` or `not_installed`, with
  `openhab.enabled: false` or `alerts.enabled: false`; a change to `not_installed` removes every `openhab_*` state,
  with a `cleared` event to `ok` for each one at `warn` or `crit` (S6, E5, E6).
- AC7: The five new options have the defaults of N2 and are validated as E13 states, also while `alerts.enabled` is
  false; a configuration without them starts (S5, E13, N2).
- AC8: `GET /api/v1/config` reports the four new keys in `thresholds`; `GET /api/v1/alerts` reports the `openhab_*`
  states and events, and every `openhab_things_offline` event carries `things`: up to 10 entries with `uid`, `label`,
  `status` and `status_detail`, sorted by UID, empty when none is counted (S3, S5, E9, N4).
- AC9: The default webhook payload of an `openhab_things_offline` event carries the same `things` array, with a hostile
  label JSON-escaped, and its `message` follows the existing format (`openhab_things_offline fired: ok -> warn (2.0)`);
  a custom template sees `.Things` with `.UID`, `.Label`, `.Status`, `.StatusDetail`, empty for other metrics
  (S3, S4, E10, N3).
- AC10: The dashboard lights a badge with the worst `openhab_*` level on the openHAB card, lists the metrics in the
  banner, and colors the card's process-memory row by the `openhab_rss_percent` level and its `Things offline` toggle
  by the `openhab_things_offline` level while that level is not `ok` (S1, S2, S3).

## Out of scope

- One alert per Thing (unbounded state, noisy; the issue rejects it).
- Alerts on the JVM heap, add-ons, Items or the openHAB version.
- Exporting alert states to `GET /metrics`, persisting alert state across restarts.
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
- Debounce against stale samples? Answered: the engine runs on the fast tick today (`fastTick` in
  `internal/collector/collector.go`) while openHAB polls every 60 s or more; the rules are evaluated per poll with
  their own window `alerts.openhab_for_seconds`, default 300 as the issue proposes (AC5).
- `UNKNOWN` Things when the start level is unknown? Answered: counted (E2), since nothing says openHAB is starting.
- RSS rule while no process is found? Answered: not evaluated, like a failed reading (E4); `openhab_down` reports the
  outage.
- Things rule without an API token? Answered: openHAB 5.2.4 refuses the Things call without a token
  ([openHAB](../../docs/areas/openhab.md), *Refused sources*), so the rule needs `openhab.api_token` (E1); the
  documentation says so.

## Area documents

- [Alerts](../../docs/areas/alerts.md): the three openHAB metrics in *Evaluated metrics*, their values and levels,
  the `0`-is-off rule of their thresholds, per-poll evaluation with `alerts.openhab_for_seconds`, event order, the
  `things` list, removal on `not_installed`.
- [Notifications](../../docs/areas/notifications.md): the new `metric` values, the `things` field of the default
  payload and `.Things` in templates.
- [Configuration](../../docs/areas/configuration.md): the five options with defaults, ranges and error texts.
- [Dashboard](../../docs/areas/dashboard.md): the openHAB card's badge and highlighted rows.
- [openHAB](../../docs/areas/openhab.md): a pointer to the alert rules. Besides: `docs/API.md` (`/api/v1/alerts`,
  `/api/v1/config`), `packaging/pimonitor.example.yaml`, `docs/integrations/openhab.md` (ntfy example).
