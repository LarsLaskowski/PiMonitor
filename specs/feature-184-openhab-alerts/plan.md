# Plan: Alert rules for a local openHAB

Source: Issue #184 | [spec.md](spec.md)
Tier: standard — a behavior change (three new alert metrics, five options, a new event key) that creates, changes or removes no security control.
Security areas touched: Outbound calls (webhook payloads carry openHAB Thing UIDs and labels), openHAB API token (Thing data served by `GET /api/v1/alerts` and sent to webhooks), Dashboard XSS (`app.js` renders new alert states), Parsing of local input (new YAML options in `internal/config`)

## Problem / root cause

See [spec.md](spec.md), *Problem*.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The engine evaluates once per fast tick; openHAB is polled every 60 to 86400 s | `internal/collector/collector.go:712-735`, `internal/config/config.go:312`, [openHAB](../../docs/areas/openhab.md#cadence) | confirmed |
| 2 | A threshold of `0` is reached by every reading, so the issue's "zero disables, consistent with existing semantics" is wrong | `internal/alert/alert.go` `evalMetric` (`value >= warn`); [Alerts](../../docs/areas/alerts.md#raw-level-of-a-reading) | confirmed |
| 3 | `config.Thresholds` is the `thresholds` object of `GET /api/v1/config` | `cmd/pimonitor/main.go:132-139` | confirmed |
| 4 | A disabled Thing is `UNINITIALIZED` / `DISABLED`; `NotOnline` is capped at 10 and includes `UNKNOWN`; `ByStatus` has no disabled count | `internal/collector/openhab.go:569-588` | confirmed |
| 5 | systeminfo (start level) is requested before the Things | `internal/collector/openhab.go:502-530` | confirmed |
| 6 | A `not_installed` poll has no `collected_at` and never calls `onSample` | `openhab.go:299-305`, `Poll` at `openhab.go:243-253` | confirmed |
| 7 | The hand-over queue drops its oldest entries beyond `HistoryCapacity` | `collector.go:505-515` | confirmed |
| 8 | `Notifier.Notify` only sends on per-webhook channels and may be called from two goroutines | `internal/alert/notify.go:201-215` | confirmed |
| 9 | Go 1.27 (`go.mod`) supports the `omitzero` JSON option: a nil slice is omitted, an empty non-nil slice is `[]` | `go.mod` line 3; `encoding/json` since Go 1.24 | confirmed |
| 10 | The repository's process sample is 1,383,952 kB on a 4 GiB host, 33.0 % | `internal/collector/openhab_test.go:183-187, 478-479` | confirmed |
| 11 | Without a token, openHAB 5.2.4 refuses `/rest/things` and `/rest/systeminfo` | [0015](../../docs/decisions/0015-openhab-values-from-real-5x-responses.md), [openHAB](../../docs/areas/openhab.md) *Refused sources* | confirmed |
| 12 | A rejected token or a failed owner check fails the root call, giving `running_unreachable` while the process runs | `openhab.go:310-318` (any root error), `checkServerOwner` at `openhab.go:683`; the real answer to a bad token is not sampled, the fake server's 401 stands in | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestEngine_EvaluateOpenHAB_Down_AC1`; `TestOpenHABCollector_Poll_AlertSample_AC1` (states ok / not running / unreachable; E11: root 401 with token, non-openHAB root; `withAlways` without process → `Down`); `TestOpenHABCollector_Poll_AlertSampleOwnerCheck_AC1` (owner check refused via `peerOwnerRecorder` → `Down`) | `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go`; `internal/collector/openhab_linux_test.go` (Linux-only helper) |
| AC2 | `TestEngine_EvaluateOpenHAB_RSS_AC2` (warn, crit, back, invalid skipped); `TestOpenHABCollector_Poll_AlertSample_AC2` (no process, memory total 0) | same files |
| AC3 | `TestEngine_EvaluateOpenHAB_ThingsOffline_AC3`; `TestOpenHABCollector_Poll_AlertThings_AC3` (from `ohThingsBody` and `ohManyThingsBody`: disabled excluded; `ohManyThingsBody` gives value 15 at start level 100 and with systeminfo refused, 14 at start level 80, list `ohOfflineNodes(1, 10)` in all three; Things refused → not valid) | same files |
| AC4 | `TestEngine_EvaluateOpenHAB_ZeroThresholdOff_AC4` (crit 0 → at most warn; warn 0 / crit 40 → `ok -> crit`, `crit -> ok` after below crit holds; both 0 → no state, also for Things) | `internal/alert/openhab_test.go` |
| AC5 | `TestNotifier_RateLimitAcrossFeeds_AC5` (a fired `openhab_*` event stamped before an already delivered `cpu` event keeps its window: a second fired `openhab_*` event inside it is dropped); `TestEngine_EvaluateOpenHAB_WindowAndOrder_AC5` (uses `openHABFor`, not `forDur`; flap shorter than the window gives no event; event order within one sample; fast `Evaluate` leaves `openhab_*` states unchanged); `TestOpenHABCollector_Poll_AlertHook_AC5` (one call per `Poll`, `Timestamp` equals `collected_at`); `TestCollector_OpenHABPollsEvaluatedOnce_AC5` (history capacity 1, three polls without a fast tick → three evaluations) | `internal/alert/notify_test.go`; `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go`; `internal/collector/collector_test.go` |
| AC6 | `TestEngine_EvaluateOpenHAB_NotInstalledPrunes_AC6` (all three at warn/crit: cleared events in the order `openhab_down`, `openhab_rss_percent`, `openhab_things_offline`, with last value and list; states gone; fresh start after reinstall); `TestOpenHABCollector_Poll_AlertHookNotInstalled_AC6` (`Installed` false, timestamp from the clock); `TestNew_OpenHABAlertsOnlyWithAlertsEnabled_AC6` | `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go`; `internal/collector/collector_test.go` |
| AC7 | `TestLoad_OpenHABAlertOptions_AC7` (defaults; file without the keys; every E13 text exact, also with `alerts.enabled: false`); `TestEngine_EvaluateOpenHAB_DefaultRSSSampleStaysOK_AC7` (`config.Default()` thresholds, 33.0 % held past the window) | `internal/config/config_test.go`; `internal/alert/openhab_test.go` |
| AC8 | `TestClientConfig_OpenHABThresholds_AC8`; `TestOpenHABCollector_Poll_AlertThingsCapped_AC8` (a 300-byte label ending in multi-byte runes and a long UID come out at most 256 bytes, valid UTF-8, a prefix of the input; served `NotOnline` unchanged); `TestReport_JSONOpenHABThings_AC8` (`things` on every Things event, `[]` when empty, absent elsewhere; keys `uid`, `label`, `status`, `status_detail`) | `cmd/pimonitor/main_test.go`; `internal/collector/openhab_test.go`; `internal/alert/openhab_test.go` |
| AC9 | `TestRenderBody_OpenHABThings_AC9` (default payload `things`, label with `"`, `\\`, `\n`, `\u0000` and `</b>` decodes back unchanged; message `openhab_things_offline fired: ok -> warn (2.0)`; template `{{range .Things}}{{.UID}} {{.Label}}{{end}}`; `.Things` empty and no `things` key for `cpu`) | `internal/alert/notify_test.go` |
| AC10 | `TestAppJS_OpenHABAlertBadge_AC10`, `TestAppJS_OpenHABAlertHighlight_AC10` (source checks as in the file); manual: Product Manager, on the PR head on the Pi, before step 9 (stop openHAB for 5 min, see badge, banner and memory/Things colors, start it again) | `internal/web/alerts_test.go` |

## Approach

**Config.** `Thresholds` gains `OpenHABRSSWarnPercent`, `OpenHABRSSCritPercent` (float64) and
`OpenHABThingsOfflineWarn`, `OpenHABThingsOfflineCrit` (int), `Alerts` gains `OpenHABForSeconds`; defaults 50, 75, 1,
0, 300. The RSS keys and `alerts.openhab_for_seconds` join `validateFinite`; the two Things keys join
`checkIntegerOptions` (shadow `thresholds` block) and `integerOptionNames`. `Thresholds.validate` checks the two new
pairs with the existing texts, the warn ≤ crit check only when both are non-zero; `alerts.openhab_for_seconds must be
>= 0 (got <v>)` next to the `for_seconds` check. `Config.OpenHABAlertFor()` converts it; `main.go` maps it to
`collector.Config.OpenHABAlertFor`.

**Engine** (new file `internal/alert/openhab.go`). `EvaluateOpenHAB` takes the engine lock once per sample. Not
`Installed`: in the fixed order `openhab_down`, `openhab_rss_percent`, `openhab_things_offline` (never a map walk),
remove each existing state, recording a `cleared` event to `ok` (last value, last list, `at` = `Timestamp`) for a
non-`ok` one; return. Otherwise, in this order: `openhab_down` with value 1/0 against cutoffs
1/1 (so only `crit`); `openhab_rss_percent` if `RSSValid` and not both thresholds 0; `openhab_things_offline` if
`ThingsValid` and not both 0. These use the existing debounce with `openHABFor` and a cutoff test of
`cutoff > 0 && value >= cutoff`; `evalMetric` gets the window and the cutoff rule as inputs (private; split as needed to
stay under gocognit 15). The Things state keeps its last list; every Things event gets a non-nil copy (`[]` when
empty); no stored slice is mutated afterwards, so `Report`'s shallow copy stays a point-in-time copy.

**Collector.** `summarizeThings` also receives the start level and derives the alert figures from the full Thing list:
counted = not `ONLINE`, not disabled, and not `UNKNOWN` while the start level is reported and below 100; the count and
the first 10 counted by UID as `alert.OpenHABThing`, each of its four strings cut to at most 256 bytes at a UTF-8
rune boundary (private constant and helper in `openhab.go`; the served `NotOnline` keeps full strings). These travel privately from `pollREST` to `Poll` (not in the served
status). `Poll` builds an `alert.OpenHABSample` after the time is stamped — `collected_at` from `onSample`, or `now()`
for `not_installed` — and calls a new private hook `onAlert func(alert.OpenHABSample)` holding no lock.
`Installed` = state not `not_installed`; `Down` = `installed_not_running` or `running_unreachable`; `RSSValid` =
process and percentage present; `ThingsValid` = Things call succeeded. `collector.New` sets `onAlert` only when the
engine exists; it calls `EvaluateOpenHAB` and hands the events to the notifier. The fast tick is unchanged.

**Notifier.** `eventView` gains `Things` (`json:"things,omitzero"`), copied from the event; `formatMessage` unchanged.
`recordSent` relied on events reaching a worker in sample order, which two feeds break: an openHAB event can be enqueued
after a later-stamped fast-tick event. It now prunes only the entries of the event's own feed (`openhab_*` keys by an
openHAB event, the others by a fast-tick event); within one feed events arrive in sample order, so no pruned window can
still matter, and the doc comment says so. Delivery order per webhook is enqueue order across both feeds.

**Dashboard.** `index.html`: `<span class="alert-badge hidden" id="badge-openhab"></span>` in the openHAB card's `h2`,
like Filesystems. `app.js`: `ALERT_BADGE_IDS` maps the three metrics to `badge-openhab`; `renderAlerts` keeps the
latest per-metric levels and calls a new `applyOpenHABAlertLevels`, which `renderOpenHAB` also calls after rebuilding:
it adds `metric-warn`/`metric-crit` to the process-memory value and the `Things offline` summary for a non-`ok` level of
`openhab_rss_percent`/`openhab_things_offline`, and nothing for `ok`. The banner shows the raw metric names, as today.
Text only through `textContent`/`classList`.

## Options considered

- Evaluation on the fast tick (repeated or from the queue) and a shared `alerts.for_seconds`:
  [0024](../../docs/decisions/0024-openhab-alerts-evaluated-per-poll.md).
- `0` off for all thresholds, or a large Things crit default:
  [0023](../../docs/decisions/0023-openhab-alert-thresholds-zero-is-off.md).
- One alert per Thing (issue, *Alternatives*): [0026](../../docs/decisions/0026-openhab-things-alert-is-one-count.md).
- A v2 or separate endpoint for the new metrics: [0025](../../docs/decisions/0025-new-metric-in-v1-alert-lists-is-no-shape-change.md).
- Thing labels in `message`: lost — a label's newline would reach plain-text bodies, and the format would change.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go` | five options, defaults, validation, `OpenHABAlertFor` |
| `internal/alert` | `alert.go` | `New` signature, `Event.Things`, `evalMetric` window and cutoff inputs |
| `internal/alert` | `openhab.go` (new) | `OpenHABThing`, `OpenHABSample`, metric names, `EvaluateOpenHAB`, pruning |
| `internal/alert` | `notify.go` | `eventView.Things`, per-feed pruning in `recordSent` |
| `internal/collector` | `openhab.go` | counted Things, alert sample, `onAlert` hook |
| `internal/collector` | `collector.go` | `Config.OpenHABAlertFor`, `alert.New` call, hook wiring |
| `cmd/pimonitor` | `main.go` | map `OpenHABAlertFor` |
| `internal/web/assets` | `index.html`, `app.js` | badge, highlight |

## Signatures (for the Dev's skeleton)

Package `alert`:

- `func New(thresholds config.Thresholds, forDur, openHABFor time.Duration) *Engine` (changed)
- `const MetricOpenHABDown = "openhab_down"`
- `const MetricOpenHABRSSPercent = "openhab_rss_percent"`
- `const MetricOpenHABThingsOffline = "openhab_things_offline"`
- `type OpenHABThing struct` with the string fields UID, Label, Status and StatusDetail, JSON keys uid, label, status
  and status_detail
- `type OpenHABSample struct` with Timestamp (time.Time), Installed and Down (bool), RSSPercent (float64), RSSValid
  (bool), ThingsOffline (int), ThingsValid (bool), Things ([]OpenHABThing)
- `func (e *Engine) EvaluateOpenHAB(s OpenHABSample) []Event`
- `Event` gains the field Things ([]OpenHABThing), JSON key things with the omitzero option

Package `config`:

- `Thresholds` gains OpenHABRSSWarnPercent and OpenHABRSSCritPercent (float64), OpenHABThingsOfflineWarn and
  OpenHABThingsOfflineCrit (int), YAML and JSON keys openhab_rss_warn_percent, openhab_rss_crit_percent,
  openhab_things_offline_warn, openhab_things_offline_crit
- `Alerts` gains OpenHABForSeconds (float64), YAML key openhab_for_seconds
- `func (c Config) OpenHABAlertFor() time.Duration`

Package `collector`: `Config` gains OpenHABAlertFor (time.Duration).

Every name follows *Writing code* and the linter configuration; no function takes more than three parameters.

## Test files

`internal/alert/openhab_test.go` (new), `internal/alert/notify_test.go`, `internal/config/config_test.go`,
`internal/collector/openhab_test.go`, `internal/collector/collector_test.go`, `cmd/pimonitor/main_test.go`,
`internal/web/alerts_test.go`.

Call sites of the changed `alert.New`: the 13 calls in `internal/alert/alert_test.go` (Tester, adding `0`);
`internal/collector/collector.go:294` (Dev). `TestCollectorConfig_MapsFields` and `TestClientConfig_ThresholdsRoundTrip`
in `cmd/pimonitor/main_test.go` set every field and get the new ones added (Tester). `TestOpenHABCollector_Poll_Things_AC5`
keeps its inputs and expectations: the served `NotOnline` is unchanged.

Files no unit test can cover: `internal/web/assets/app.js` and `index.html` (no JS test runner), already excluded from
the coverage measure; no new exclusion.

## Areas

- [Alerts](../../docs/areas/alerts.md): *When the engine runs* (the openHAB feed, per poll, its window; with
  `openhab.enabled: true` on a host without openHAB, `openhab_down` stays at `crit`); the 256-byte cut of Thing strings; *Evaluated
  metrics* table gains the three rows and the "No other metric" sentence names them; *Raw level* gets the `0`-is-off
  exception; event order within an openHAB poll and the `at` ordering (E14); the `things` list; pruning on
  `not_installed`; *Failed readings* for the openHAB metrics; related decisions 0023, 0024, 0026.
- [Notifications](../../docs/areas/notifications.md): `metric` row lists the new values; new `things` row with the
  256-byte cut; `.Things` in *Custom templates*; Thing labels are untrusted and leave the Pi to the webhook; delivery
  order per webhook is enqueue order, and the openHAB events come from a second feed (*Queueing and ordering*).
- [Configuration](../../docs/areas/configuration.md): four `thresholds` rows with unit and the `0`/warn ≤ crit
  exception in the section text; `alerts.openhab_for_seconds` row; the integer-option sentence names the two new keys; the `openhab.enabled` and
  `alerts.openhab_for_seconds` rows say that `true` without openHAB keeps `openhab_down` at `crit`.
- [Dashboard](../../docs/areas/dashboard.md): *Alerts* badge list gains the openHAB card; the openHAB card paragraph
  gets the highlight and that its warning state and `Things offline: <n>` stay as they are.
- [openHAB](../../docs/areas/openhab.md): one line under *Cadence* linking the alert rules.
- [HTTP API](../../docs/areas/http-api.md#versioning-and-stability): the rule of 0025.

## Documentation updates

All by the Dev, once the code is green:

- `docs/API.md`: `GET /api/v1/alerts` example and notes (new `metric` values, `things` with its presence rule and the
  256-byte cut of each string,
  clients ignore unknown metrics, per-poll evaluation); `GET /api/v1/config` example with the four keys.
- `packaging/pimonitor.example.yaml`: the four thresholds and `openhab_for_seconds`, each with a comment (`0` turns
  it off; Things need an API token); the `openhab.enabled` comment says `true` without openHAB keeps `openhab_down`
  at `crit`.
- `docs/integrations/openhab.md`: a section "Alerts on openHAB itself" after "The other direction": the three
  metrics, the token for the Things rule, and an ntfy webhook with `content_type: text/plain` and a template that
  ranges over `.Things`.
- `README.md`: the *Threshold alerts* feature bullet names the openHAB rules.
- `SECURITY.md` (line 67): Thing UIDs and labels also appear in `GET /api/v1/alerts` events and in webhook payloads;
  then append verbatim: "After an upgrade with an `openhab.api_token` set and alerts on, every webhook whose
  `min_level` admits `warn` receives the UIDs and labels of up to 10 offline Things in `openhab_things_offline`
  events; set both `thresholds.openhab_things_offline_warn` and `thresholds.openhab_things_offline_crit` to `0` to stop
  them."
- `docs/ARCHITECTURE.md`: the diagram gains `OH`-poll → `Alerts` (`Collector -->|openHAB poll| Alerts`); *Alert engine*
  says it is also fed from the openHAB poll's goroutine, linking 0024.
- `.squad/project.md` (exact text, written in by the orchestrator): in *openHAB API token*, after "so `api_key`
  governs who reads it;" insert " the `things` of `openhab_things_offline` events in `GET /api/v1/alerts` carry up to
  10 of those UIDs and labels under the same key, and the webhook payloads send them to the configured endpoints;";
  in *Outbound calls*, after "per-webhook queues" insert ", whose default payload carries openHAB Thing UIDs and
  labels".

## Architecture check

Guarantees of `.squad/project.md`: the main service stays unprivileged; v1 shapes change only additively (0018,
0025); a missing source yields no fabricated reading (an absent RSS or Things value is not evaluated); a hung webhook
never stalls collection (the poll goroutine only enqueues; `cleared` still bypasses the rate limiter). No new
dependency, request, file write or route.

## Security considerations

- [x] The Things body stays bounded at 8 MiB after decompression; the new cut applies to the decoded strings: each
  of the four strings of an alert Thing at most 256 bytes, an event at most 10 Things, so the engine retains at most
  about 1 MiB of Thing text (100 events × 10 × 4 × 256 B), each queued event at most 10 KiB.
- [x] No new error reaches a user except the startup texts of E13, which name the key and its value only.
- Thing strings are untrusted: JSON-encoded by `encoding/json` in the default payload, never in a URL, never logged
  (the queue-full warning logs metric and resource only), rendered by the dashboard only through `textContent`.

## Decision records

- [0023](../../docs/decisions/0023-openhab-alert-thresholds-zero-is-off.md) (Proposed)
- [0024](../../docs/decisions/0024-openhab-alerts-evaluated-per-poll.md) (Proposed)
- [0025](../../docs/decisions/0025-new-metric-in-v1-alert-lists-is-no-shape-change.md) (Proposed)
- [0026](../../docs/decisions/0026-openhab-things-alert-is-one-count.md) (Proposed)

## Out of scope / follow-ups

See [spec.md](spec.md), *Out of scope*.

Next Steps — linked issue for the orchestrator to open:
- Title: "Upper bound for alerts.for_seconds and alerts.openhab_for_seconds". Body: "`Config.AlertFor()` and
  `OpenHABAlertFor()` (`internal/config/config.go`) convert float seconds to a `time.Duration` with no upper bound; an
  out-of-range value (for example `1e12`) gives a platform-dependent result, on amd64 a negative window that confirms
  on the first sample. Validate both keys against an upper bound with an error text naming the key, in Configuration
  and the example config. Found in the Security plan review of #184."

## Challenge

Devil's Advocate, mode plan: no major objection, five minor ones.

- 1 (AC3 count for `ohManyThingsBody`): accepted — AC3 row asserts 15 at start level 100 and with systeminfo refused,
  14 at 80, list node01–node10; the rule is unchanged.
- 2 (owner-check case needs the Linux-only helper): accepted — `TestOpenHABCollector_Poll_AlertSampleOwnerCheck_AC1` in
  `openhab_linux_test.go`.
- 3 (prune order): accepted — *Approach* fixes the order `openhab_down`, `openhab_rss_percent`,
  `openhab_things_offline`; the AC6 test asserts it with three active states.
- 4 (rate-limit pruning assumes sample order): accepted, with a different fix — pruning against the largest `At` seen
  is still unsafe (a later event of the pruned key may carry a smaller `At`), so `recordSent` prunes per feed, where
  sample order holds; comment corrected, `TestNotifier_RateLimitAcrossFeeds_AC5`, and Notifications states enqueue
  order (*Approach*, *Areas*).
- 5 (`openhab.enabled: true` without openHAB): accepted — documented in Alerts, Configuration and the example config;
  the `withAlways` case is in the AC1 test.

## Security review

1. [BLOCKING] Byte bound on Thing data: resolved — the collector cuts each of the four strings of an alert Thing to
   256 bytes at a rune boundary (*Approach*); spec N1 and AC8 state it, Alerts, Notifications and `API.md` document it,
   and `TestOpenHABCollector_Poll_AlertThingsCapped_AC8` pins it. Worst case about 1 MiB (*Security considerations*).
2. [NON-BLOCKING] Upgrade starts sending Thing names off-host: fixed now — Security's sentence is appended verbatim to
   the `SECURITY.md` bullet (*Documentation updates*).
3. [NON-BLOCKING] No upper bound on `alerts.openhab_for_seconds`: a gap shared with `alerts.for_seconds`, left to the
   linked issue under *Out of scope / follow-ups*; this change copies the existing validation.
