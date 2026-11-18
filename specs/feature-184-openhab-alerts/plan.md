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
| 13 | [0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md) is unreleased | `git ls-remote --tags origin`: v1.0.0–v1.2.0, none containing it | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestEngine_EvaluateOpenHAB_Down_AC1`; `TestOpenHABCollector_Poll_AlertSample_AC1` (states ok / not running / unreachable; E11: root 401 with token, owner check refused via `peerOwnerRecorder`, non-openHAB root) | `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go` |
| AC2 | `TestEngine_EvaluateOpenHAB_RSS_AC2` (warn, crit, back, invalid skipped); `TestOpenHABCollector_Poll_AlertSample_AC2` (no process, memory total 0) | same files |
| AC3 | `TestEngine_EvaluateOpenHAB_ThingsOffline_AC3`; `TestOpenHABCollector_Poll_AlertThings_AC3` (from `ohThingsBody` and `ohManyThingsBody`: disabled excluded; `UNKNOWN` excluded at start level 80, counted at 100 and when systeminfo is refused; 12 counted → value 12, list 10 by UID; Things refused → not valid) | same files |
| AC4 | `TestEngine_EvaluateOpenHAB_ZeroThresholdOff_AC4` (crit 0 → at most warn; warn 0 / crit 40 → `ok -> crit`, `crit -> ok` after below crit holds; both 0 → no state, also for Things) | `internal/alert/openhab_test.go` |
| AC5 | `TestEngine_EvaluateOpenHAB_WindowAndOrder_AC5` (uses `openHABFor`, not `forDur`; flap shorter than the window gives no event; event order within one sample; fast `Evaluate` leaves `openhab_*` states unchanged); `TestOpenHABCollector_Poll_AlertHook_AC5` (one call per `Poll`, `Timestamp` equals `collected_at`); `TestCollector_OpenHABPollsEvaluatedOnce_AC5` (history capacity 1, three polls without a fast tick → three evaluations) | `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go`; `internal/collector/collector_test.go` |
| AC6 | `TestEngine_EvaluateOpenHAB_NotInstalledPrunes_AC6` (cleared events with last value and list, states gone, fresh start after reinstall); `TestOpenHABCollector_Poll_AlertHookNotInstalled_AC6` (`Installed` false, timestamp from the clock); `TestNew_OpenHABAlertsOnlyWithAlertsEnabled_AC6` | `internal/alert/openhab_test.go`; `internal/collector/openhab_test.go`; `internal/collector/collector_test.go` |
| AC7 | `TestLoad_OpenHABAlertOptions_AC7` (defaults; file without the keys; every E13 text exact, also with `alerts.enabled: false`); `TestEngine_EvaluateOpenHAB_DefaultRSSSampleStaysOK_AC7` (`config.Default()` thresholds, 33.0 % held past the window) | `internal/config/config_test.go`; `internal/alert/openhab_test.go` |
| AC8 | `TestClientConfig_OpenHABThresholds_AC8`; `TestReport_JSONOpenHABThings_AC8` (`things` on every Things event, `[]` when empty, absent elsewhere; keys `uid`, `label`, `status`, `status_detail`) | `cmd/pimonitor/main_test.go`; `internal/alert/openhab_test.go` |
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
`Installed`: remove every `openhab_*` state, recording a `cleared` event to `ok` (last value, last list, `at` =
`Timestamp`) for each non-`ok` one; return. Otherwise, in this order: `openhab_down` with value 1/0 against cutoffs
1/1 (so only `crit`); `openhab_rss_percent` if `RSSValid` and not both thresholds 0; `openhab_things_offline` if
`ThingsValid` and not both 0. These use the existing debounce with `openHABFor` and a cutoff test of
`cutoff > 0 && value >= cutoff`; `evalMetric` gets the window and the cutoff rule as inputs (private; split as needed to
stay under gocognit 15). The Things state keeps its last list; every Things event gets a non-nil copy (`[]` when
empty); no stored slice is mutated afterwards, so `Report`'s shallow copy stays a point-in-time copy.

**Collector.** `summarizeThings` also receives the start level and derives the alert figures from the full Thing list:
counted = not `ONLINE`, not disabled, and not `UNKNOWN` while the start level is reported and below 100; the count and
the first 10 counted by UID as `alert.OpenHABThing`. These travel privately from `pollREST` to `Poll` (not in the served
status). `Poll` builds an `alert.OpenHABSample` after the time is stamped — `collected_at` from `onSample`, or `now()`
for `not_installed` — and calls a new private hook `onAlert func(alert.OpenHABSample)` holding no lock.
`Installed` = state not `not_installed`; `Down` = `installed_not_running` or `running_unreachable`; `RSSValid` =
process and percentage present; `ThingsValid` = Things call succeeded. `collector.New` sets `onAlert` only when the
engine exists; it calls `EvaluateOpenHAB` and hands the events to the notifier. The fast tick is unchanged.

**Notifier.** `eventView` gains `Things` (`json:"things,omitzero"`), copied from the event; `formatMessage` unchanged.

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
- Putting Thing labels into `message` so `.Message` templates name them: lost — a label's newline would reach
  plain-text bodies unescaped, and the existing message format would change; `.Things` serves templates.
- Coloring the card by raw thresholds: lost — the issue asks for the alert state (spec, *Out of scope*).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go` | five options, defaults, validation, `OpenHABAlertFor` |
| `internal/alert` | `alert.go` | `New` signature, `Event.Things`, `evalMetric` window and cutoff inputs |
| `internal/alert` | `openhab.go` (new) | `OpenHABThing`, `OpenHABSample`, metric names, `EvaluateOpenHAB`, pruning |
| `internal/alert` | `notify.go` | `eventView.Things` |
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

No new exported name breaks a rule of *Writing code* or the golangci-lint configuration; no function takes more than
three parameters.

## Test files

`internal/alert/openhab_test.go` (new), `internal/alert/notify_test.go`, `internal/config/config_test.go`,
`internal/collector/openhab_test.go`, `internal/collector/collector_test.go`, `cmd/pimonitor/main_test.go`,
`internal/web/alerts_test.go`.

Call sites of the changed `alert.New`: the 13 calls in `internal/alert/alert_test.go` (Tester, adding `0`);
`internal/collector/collector.go:294` (Dev). `TestCollectorConfig_MapsFields` and `TestClientConfig_ThresholdsRoundTrip`
in `cmd/pimonitor/main_test.go` set every field and get the new ones added (Tester). `TestOpenHABCollector_Poll_Things_AC5`
keeps its inputs and expectations: the served `NotOnline` is unchanged.

Files no unit test can cover: `internal/web/assets/app.js` and `index.html` — no JS test runner; already excluded by
`sonar.coverage.exclusions`, and `COVERAGE_PATHSPECS` measures only `*.go`. No new exclusion.

## Areas

- [Alerts](../../docs/areas/alerts.md): *When the engine runs* (the openHAB feed, per poll, its window); *Evaluated
  metrics* table gains the three rows and the "No other metric" sentence names them; *Raw level* gets the `0`-is-off
  exception; event order within an openHAB poll and the `at` ordering (E14); the `things` list; pruning on
  `not_installed`; *Failed readings* for the openHAB metrics; related decisions 0023, 0024, 0026.
- [Notifications](../../docs/areas/notifications.md): `metric` row lists the new values; new `things` row; `.Things`
  in *Custom templates*; Thing labels are untrusted and leave the Pi to the webhook.
- [Configuration](../../docs/areas/configuration.md): four `thresholds` rows with unit and the `0`/warn ≤ crit
  exception in the section text; `alerts.openhab_for_seconds` row; the integer-option sentence names the two new keys.
- [Dashboard](../../docs/areas/dashboard.md): *Alerts* badge list gains the openHAB card; the openHAB card paragraph
  gets the highlight and that its warning state and `Things offline: <n>` stay as they are.
- [openHAB](../../docs/areas/openhab.md): one line under *Cadence* linking the alert rules.
- [HTTP API](../../docs/areas/http-api.md#versioning-and-stability): the rule of 0025.

## Documentation updates

All by the Dev, once the code is green:

- `docs/API.md`: `GET /api/v1/alerts` example and notes (new `metric` values, `things` with its presence rule,
  clients ignore unknown metrics, per-poll evaluation); `GET /api/v1/config` example with the four keys.
- `packaging/pimonitor.example.yaml`: the four thresholds and `openhab_for_seconds`, each with a comment (`0` turns
  it off; Things need an API token).
- `docs/integrations/openhab.md`: a section "Alerts on openHAB itself" after "The other direction": the three
  metrics, the token for the Things rule, and an ntfy webhook with `content_type: text/plain` and a template that
  ranges over `.Things`.
- `README.md`: the *Threshold alerts* feature bullet names the openHAB rules.
- `SECURITY.md` (line 67): Thing UIDs and labels also appear in `GET /api/v1/alerts` events and in webhook payloads.
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

- [x] No limits on parsed input change; the Things body is still bounded at 8 MiB after decompression; an event holds
  at most 10 Things.
- [x] No new error reaches a user except the startup texts of E13, which name the key and its value only.
- Thing strings are untrusted: JSON-encoded by `encoding/json` in the default payload, never in a URL, never logged
  (the queue-full warning logs metric and resource only), rendered by the dashboard only through `textContent`.

## Decision records

- [0023](../../docs/decisions/0023-openhab-alert-thresholds-zero-is-off.md) (Proposed)
- [0024](../../docs/decisions/0024-openhab-alerts-evaluated-per-poll.md) (Proposed)
- [0025](../../docs/decisions/0025-new-metric-in-v1-alert-lists-is-no-shape-change.md) (Proposed)
- [0026](../../docs/decisions/0026-openhab-things-alert-is-one-count.md) (Proposed)

## Out of scope / follow-ups

See [spec.md](spec.md), *Out of scope*. No follow-up issue.

## Challenge notes

Devil's Advocate, mode plan: specs/feature-184-openhab-alerts (/home/user/PiMonitor/specs/feature-184-openhab-alerts/plan.md, spec.md, tasks.md), issue #184

The plan holds up. I checked its verified facts against the code and they hold:
- `evalMetric` uses `value >= warn` (internal/alert/alert.go:245).
- The hand-over queue drops its oldest entries (internal/collector/collector.go:505-515).
- A `not_installed` poll returns `sampled=false` with no `collected_at` (internal/collector/openhab.go:299-305).
- Any root failure while the process runs gives `running_unreachable` (openhab.go:310-318).
- systeminfo is fetched before the Things (openhab.go:502-530).
- `summarizeThings` lists non-ONLINE, non-disabled Things, capped at 10 (openhab.go:569-588).
- There are exactly 13 `alert.New` calls in alert_test.go plus collector.go:294.
- The Go version is 1.27.4, so `omitzero` is available.
- On shutdown, `Run` joins the openHAB goroutine before `notifier.Stop` (deferred functions run last-in, first-out: collector.go:363-380). So the new feed never calls `Notify` after the workers have stopped.
- The AC4 transitions follow from `nextLevel` (alert.go:284-309) with the plan's `cutoff > 0 && value >= cutoff` rule. When warn is off, `warnAbove` stays false since the first poll, so a confirmed `crit -> ok` goes straight to `ok`.

Tier `standard` fits: no security control changes. Need, scope and the alternatives are sound. I found no major objection, only five minor ones.

**Minor objections (each written as an instruction):**

1. **minor: the AC3 count in the plan is wrong for the fixture it names.** Plan, *Tests per criterion* AC3, says "12 counted → value 12" for `ohManyThingsBody`. The fixture (internal/collector/openhab_test.go:774-795) holds 12 OFFLINE Things plus one `UNKNOWN`, one with the status `<script>` and one `UNINITIALIZED`/`HANDLER_MISSING_ERROR`. All of them are counted under the plan's rule (not `ONLINE`, not disabled). The default systeminfo body reports `startLevel` 100 (openhab_test.go:32).
   - Tester: assert value 15 at start level 100 and when systeminfo is refused, and 14 at start level 80.
   - In all three cases the list is node01 to node10 by UID (`ohOfflineNodes(1, 10)`).
   - Dev: do not change the rule to match 12.

2. **minor: the E11 owner-check case cannot go in the file the plan names.** `peerOwnerRecorder` is defined in internal/collector/openhab_linux_test.go:87-105, which has `//go:build linux` (line 1). Plan AC1 puts the case in internal/collector/openhab_test.go. Tester: put the owner-check case of `TestOpenHABCollector_Poll_AlertSample_AC1` in openhab_linux_test.go, or in a Linux-only test beside the existing owner-check table at openhab_linux_test.go:148-154.

3. **minor: the cleared events on `not_installed` must come in a fixed order.** The plan's pruning ("remove every `openhab_*` state, recording a `cleared` event … for each non-`ok` one") would, if written like `pruneDisks`, walk the `e.states` map (alert.go:321) and produce the events in random order. AC5 fixes the order within a poll only for evaluation.
   - Dev: prune in the fixed order `openhab_down`, `openhab_rss_percent`, `openhab_things_offline`.
   - Tester: assert that order in `TestEngine_EvaluateOpenHAB_NotInstalledPrunes_AC6`, with more than one state at warn or crit.

4. **minor: the notifier's ordering assumption breaks with the second feed.** The plan's *Architecture check* and fact 8 cover only non-blocking enqueueing. `recordSent`'s doc comment (internal/alert/notify.go:266-272) justifies pruning other keys' rate-limit entries with "events reach a worker in sample order". With two feeds, an openHAB event stamped at time A can be enqueued after a fast-tick event stamped later. That fast-tick event's `recordSent` can then drop the openHAB key's entry early, so a fired `openhab_*` event inside its rate-limit window gets through. Dev:
   - Make the pruning safe against out-of-order `At`. For example, prune against the largest `At` the worker has seen, not `ev.At`, or prune only entries older than both.
   - Correct the comment.
   - Add a notify_test.go case for an older-stamped event arriving after a newer one.
   - Say in [Notifications] that delivery order across the two feeds is enqueue order.

5. **minor: `openhab.enabled: true` on a host without openHAB will keep `openhab_down` at `crit`.** With `Always` set, `poll` never returns `not_installed` (openhab.go:300). With no process and a failed root call the state is `installed_not_running` (openhab.go:310-318; docs/areas/openhab.md:32 confirms the state is "never `not_installed`" with `true`). So `openhab_down` fires `crit` and stays there. The spec covers only `auto` (S6). Dev: state this in [Alerts], in the `openhab.enabled` / `alerts.openhab_for_seconds` rows of [Configuration], and in the comment in packaging/pimonitor.example.yaml. Tester: optionally add a case to `TestOpenHABCollector_Poll_AlertSample_AC1` using `withAlways` (openhab_test.go:189) with no process, expecting `Down` true.

## Security notes

Security plan review for feature 184 (openHAB alert rules). I read `specs/feature-184-openhab-alerts/spec.md` and `plan.md` at `ae7e88a`, on branch claude/squad-feature-184-7jqtun. One blocking finding: the plan's memory bound for Thing data does not hold. There are two non-blocking findings.

**Tier and listed areas**
- `standard` is the right tier. No control is created, changed or removed:
  - `api_key` already guards `GET /api/v1/alerts` (`internal/httpapi/routes.go:63`, `api: true`), the same as `GET /api/v1/openhab`. N3 is true.
  - The default webhook payload still goes through `encoding/json`.
  - The owner check and the bearer-token path are unchanged.
- The four listed areas (Outbound calls, openHAB API token, Dashboard XSS, Parsing of local input) are complete:
  - The Logging area is not touched: the only log call in the new path is the existing queue-full warning, which logs metric and resource only (`internal/alert/notify.go:210`).
  - The HTTP surface gets no new route.
- The dashboard side is safe:
  - The badge and banner get only engine levels and metric names, set through `textContent`/`className` (`app.js:405-455`).
  - No Thing string is rendered.
  - The plan's `applyOpenHABAlertLevels` uses fixed `metric-warn`/`metric-crit` classes.
- Leaving `description` out of `alert.OpenHABThing` is correct: it carries binding error texts.

**Measurement I ran**
Plan fact 9 and AC8/AC9 (`omitzero`, escaping). In `/tmp/claude-0/-home-user-PiMonitor/955b6d88-c31d-5aa5-8bda-5584324fbbba/scratchpad/oz`, `go run .` (go1.24.7 linux/amd64) printed:
```
{"metric":"cpu"}
{"metric":"x","things":[]}
{"metric":"x","things":[{"uid":"a\"\n\u0000</b>"}]}
```
A nil slice is omitted, an empty slice gives `[]`, and a hostile label is escaped. The claim holds.

**Findings**

1. [BLOCKING] The memory bound for Thing data counts entries, not bytes.
   - The claim: N1 and the plan's *Security considerations* ("an event holds at most 10 Things", "event list stays capped at 100") present this as the bound on retained Thing data.
   - Why it does not hold: no field of a Thing is length-limited. Only the whole body is capped at 8 MiB (`internal/collector/openhab.go:124`, `:631`). `summarizeThings` copies `UID`, `Label`, `Status` and `StatusDetail` unchanged (`openhab.go:584-585`).
   - What retains it today: one poll's list, in the served status.
   - What retains it after this plan:
     - the Things state's last list;
     - every one of up to 100 events (`internal/alert/alert.go:27`, `:354`);
     - up to 256 queued events per webhook (`notify.go:39`, sharing the strings);
     - a rendered POST body per delivery, possibly to an internet endpoint.
   - Worst case: one label of about 8 MiB in each of 100 Things events is about 800 MiB retained on a Pi. Each transition needs a poll at least 60 s apart, so filling the list takes about 100 minutes.
   - Who can cause it: an openHAB admin, or a binding that takes a label from a discovered device. A spoofed server cannot, since the token and owner check are required.
   - Smallest fix: in the collector, where `alert.OpenHABThing` is built, cut each of the four strings to a fixed maximum (for example 256 bytes, at a UTF-8 rune boundary).
     - State the cap in *Alerts* and *Notifications*, and in the `things` notes of `docs/API.md`.
     - Add a Tester test under AC3 or AC8 that pins it: a label longer than the cap comes out at the cap length and still valid UTF-8.
   - The worst case then drops to about 1 MiB (100 × 10 × 4 × 256 B). The served `NotOnline` of `GET /api/v1/openhab` stays unchanged.

2. [NON-BLOCKING] After an upgrade, existing webhooks start sending Thing names off-host without the operator changing anything.
   - Defaults are warn 1 and alerts on (N2). With a token set, every existing webhook whose `min_level` admits `warn` starts receiving up to 10 Thing UIDs and labels per event. UIDs often contain IP or MAC addresses (for example `network:pingdevice:192_168_1_10`).
   - The receiver may be a public ntfy topic, which anyone who knows its name can read.
   - The plan's SECURITY.md edit (line 67) names the new places the data appears, but not that it starts on upgrade, nor how to stop it.
   - Recommendation: fix now by appending to that SECURITY.md bullet, verbatim:
     "After an upgrade with an `openhab.api_token` set and alerts on, every webhook whose `min_level` admits `warn`
     receives the UIDs and labels of up to 10 offline Things in `openhab_things_offline` events; set both
     `thresholds.openhab_things_offline_warn` and `thresholds.openhab_things_offline_crit` to `0` to stop them."

3. [NON-BLOCKING] `alerts.openhab_for_seconds` has no upper limit.
   - `OpenHABAlertFor()` copies `AlertFor()` (`internal/config/config.go:320-321`), which converts float seconds to a Duration with no upper bound. When the value is out of range, the result of that conversion depends on the platform.
   - Measured on amd64 (`go run . 1e12` in the scratchpad folder above): `-2562047h47m16.854775808s`. A negative window means a reading is confirmed on the first poll instead of never.
   - arm64 was not run: the binary builds, but running it needs an arm64 host or qemu.
   - `alerts.for_seconds` already has the same gap, so this is a copied validation gap, not a new control.
   - Recommendation: a linked issue covering both keys (an upper bound with an error text), not this change.
