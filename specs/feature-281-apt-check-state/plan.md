# Plan: Signal a never-run or failing apt updates check

Source: Issue #281 | [spec.md](spec.md)
Tier: standard — a behavior change of the snapshot, `/metrics` and the dashboard that creates, changes or removes no security control.
Security areas touched: Dashboard XSS (`internal/web/assets/app.js` renders new texts)

## Problem / root cause

See [spec.md](spec.md), *Problem*.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | A failed check keeps the whole last result (`count`, `packages`, `cache_age_seconds`, `stale`, `checked_at`) | `internal/collector/collector.go:877-885` assigns `c.latest.Updates` only on success | confirmed |
| 2 | Before the first success `Updates` is the zero value | `collector.go` `New`: `latest: Snapshot{Disks: …, DiskIO: …}`; nothing persists `Updates` (`persist.go`) | confirmed |
| 3 | The server answers before the first check finishes | `cmd/pimonitor/main.go:67,73`: `coll.Run` and `ListenAndServe` run concurrently; `Run` does `fastTick` then `slowTick` (`collector.go:374-386`) | confirmed |
| 4 | `pimonitor_updates_pending` is always written | `internal/httpapi/prometheus.go:262-265` | confirmed |
| 5 | `writePiholeMetrics` omits its family when Pi-hole is not installed | `prometheus.go:271-274` | confirmed |
| 6 | The issue: the snapshot is `/api/v1/snapshot` (it is `GET /api/v1/metrics`; `updates` also at `/api/v1/metrics/updates`) | `internal/httpapi/routes.go:61,123` | refuted |
| 7 | The dashboard shows `Checked <time>` for the zero time and never clears the line | `app.js:298-300`: guard `if (snap.updates.checked_at)`; Go encodes the zero `time.Time` as `"0001-01-01T00:00:00Z"` | confirmed |
| 8 | An open package list re-renders from `snap.updates.packages` on every poll | `app.js:303-311` | confirmed |
| 9 | `updates` feeds no alert rule | `docs/areas/alerts.md:47` | confirmed |
| 10 | A JS test runner is not part of the repository; dashboard behavior is guarded by source scans of the embedded `app.js` | `docs/UNIT_TESTS.md` (*Static-asset guard tests*), `internal/web/pihole_test.go` | confirmed |
| 11 | `pihole_test.go:59` pins the line `setText('updates-count', String(snap.updates.count));`, which this change rewrites | `internal/web/pihole_test.go:59-61` | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestCollector_SlowTick_UpdatesCheckStatus`: subtests `AC1 not_checked before any check` (fresh `New`, `Updates` otherwise zero), `AC1 ok after a successful check` (stub prints the fixture; result as today), `AC1 failed after a success keeps the result` (stub `exit 1`; count, packages, `checked_at`, cache age and stale of the first check), `AC1 failed before any success keeps the zero values`, `AC1 ok again after a failure` (new result, new `checked_at`) | `internal/collector/collector_test.go` |
| AC1 | `TestHandleMetrics_UpdatesCheckStatus`: subtests `AC1 key in /api/v1/metrics` and `AC1 key in /api/v1/metrics/updates`, a fake snapshot with `CheckStatus` `failed` and a count of 3: decoded `updates.check_status` is `"failed"`, `count` is 3 | `internal/httpapi/handlers_test.go` |
| AC2 | `TestCollector_SlowTick_UpdatesStatusMatchesResult`: `AC2 snapshot never mixes status and result` — slow ticks alternating success and failure while a goroutine reads `Snapshot()`; every read satisfies `ok` ⇒ non-zero `CheckedAt`, `not_checked` ⇒ zero `CheckedAt` (run under `-race`) | `internal/collector/collector_test.go` |
| AC3 | `TestRenderPrometheusMetrics_UpdatesPendingOnlyWhenOK`: subtests `AC3 ok writes the family` (count 3: HELP, TYPE, `pimonitor_updates_pending 3`), `AC3 ok writes a real 0`, `AC3 not_checked omits the family`, `AC3 failed omits the family` (count 3; no `pimonitor_updates_pending` substring at all) | `internal/httpapi/prometheus_test.go` |
| AC4 | `TestAppJS_UpdatesCardFollowsCheckStatus`: source scan of `renderUpdates` in the embedded `app.js` (*Approach*): subtests `AC4 count only when ok`, `AC4 state texts` (`Not checked yet`, `Check failed`, `Last successful check `), `AC4 zero checked_at shows no line` (the line is set on every render, no `if (…checked_at)` guard left), `AC4 stale and button only when ok`, `AC4 open list closes outside ok`; the `.innerHTML` ban stays `xss_test.go`'s | `internal/web/updates_test.go` |

## Approach

**Collector.** `Updates` gets `CheckStatus` (*Signatures*). `New` starts `latest.Updates` as
`Updates{CheckStatus: UpdatesCheckNotChecked}`. In `slowTick`, on success the returned value gets
`CheckStatus = UpdatesCheckOK` before the one locked assignment; on failure `slowTick` takes the write lock and sets
only `c.latest.Updates.CheckStatus = UpdatesCheckFailed`, then logs the warning as today. Status and result therefore
change under one lock (AC2). `updates.go` (a security-area file) is not changed.

**Prometheus.** `writeUpdatesMetrics` returns without writing anything while `updates.CheckStatus !=
collector.UpdatesCheckOK`; otherwise unchanged.

**Dashboard.** The `// Updates` block of the render function becomes a call `renderUpdates(snap.updates)`, a new
function beside `renderPihole`, using `setText` and `classList.toggle` only. With `ok = updates.check_status === 'ok'`:

| Element | `ok` | `not_checked` | `failed` (and any other value) |
| ------- | ---- | ------------- | ------------------------------ |
| `updates-count` | `String(updates.count)` | `Not checked yet` | `Check failed` |
| `updates-checked` | `Checked <time>` | empty | `Last successful check <time>` when `checked_at` is not the zero time, else empty |
| `updates-stale` | shown when `stale` | hidden | hidden |
| `updates-show` | shown when packages, text as today | hidden | hidden |
| open `updates-modal` | re-rendered | closed (`.close()`) | closed |

`<time>` is `new Date(checked_at).toLocaleTimeString()` as today. A zero time is one whose `Date.parse` is `NaN` or
`<= 0` (the Go zero time parses to a large negative number), in a small helper `aptCheckedLine(updates)` that returns
the line's text, so `renderUpdates` stays well below SonarQube's cognitive-complexity limit. `latestPackages` is
`[]` outside `ok`. The Pi-hole section is not touched. The new card texts are fixed by AC4; no further layout.

Early draft PR: no

## Options considered

The key design and the `/metrics` rule are in [decision 0029](../../docs/decisions/0029-apt-check-status-key-and-prometheus-gap.md)
(two flags, openHAB's names, keeping the count in `/metrics` after a failure, a new family, a nullable `count`).
For where the status is set: in `UpdatesCollector.Collect` (returning `ok` with the result) lost because `Collect`
cannot set `failed` without the snapshot, so `slowTick` would still own half of it, and because it would change a
security-area file for no gain.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `types.go` | `Updates.CheckStatus`, the three constants |
| `internal/collector` | `collector.go` | initial status in `New`, status in `slowTick` |
| `internal/httpapi` | `prometheus.go` | `writeUpdatesMetrics` guard |
| `internal/web` | `assets/app.js` | `renderUpdates`, `aptCheckedLine` |

## Signatures (for the Dev's skeleton)

A new field of the collector's updates type, after its check-time field, in `internal/collector/types.go`:

- ``CheckStatus string `json:"check_status"` ``

New untyped string constants in the same file, each with a doc comment, in the style of the openHAB state set:

- `const UpdatesCheckOK = "ok"`
- `const UpdatesCheckNotChecked = "not_checked"`
- `const UpdatesCheckFailed = "failed"`

The skeleton holds them for real; the logic of the collector, the Prometheus renderer and the dashboard script is
step 6.

## Test files

- `internal/collector/collector_test.go`, `internal/httpapi/handlers_test.go`, `internal/httpapi/prometheus_test.go`,
  `internal/web/updates_test.go` (new).

Existing test code affected (Tester adapts): `fullSnapshot()` in `internal/httpapi/prometheus_test.go` sets
`CheckStatus: collector.UpdatesCheckOK`, since the tests at its lines 70, 96, 184 and 234 expect
`pimonitor_updates_pending 3`; `TestAppJS_PiholeRenderedSeparatelyFromAptCount` (`internal/web/pihole_test.go:59`)
pins the new `updates-count` line of `renderUpdates`, still the apt-only `updates.count`. The routes test snapshot
(`routes_test.go:133`) compares sub-resource to full snapshot and stays unchanged. The existing
`TestCollector_SlowTick_UpdatesFailureKeepsPreviousResult` stays: no input it uses is handled differently.

Files no unit test can cover: `internal/web/assets/app.js` — no JS runner; guarded by the source scans above. It is
outside `COVERAGE_PATHSPECS` (`*.go`), so no exclusion is needed.

## Areas

- `docs/areas/metrics.md` (3262 words, +70 words): *Available apt updates*, last bullet — the three check states,
  when each holds, set together with the result, a running check keeps the previous state, restart begins at
  `not_checked`; existing keys keep the last success.
- `docs/areas/http-api.md` (2555 words, +15 words): *Prometheus endpoint*, the "left out" bullet — the updates family
  while the latest apt check has not succeeded.
- `docs/areas/dashboard.md` (3145 words, +60 words): the Updates card bullet — per state as the *Approach* table.

## Documentation updates

All by the Dev, once the code is green:

- the three area documents above;
- `docs/API.md`: `"check_status": "ok"` in the `updates` example (after `checked_at`); a note bullet beside
  `updates.stale`: always present, the three values, the other keys keep the last success, and a pointer to
  Metrics; the `updates_pending` row: "Omitted entirely while `updates.check_status` is not `ok`";
- `docs/integrations/openhab.md`: a `Type string : updatesCheck` channel on `$.updates.check_status` beside the
  `updates` channel (line 69) and its item next to line 116; at the json_exporter example (lines 289-291) a note that
  `count` means something only while `check_status` is `ok`, and that `/metrics` leaves the count out instead;
- `README.md`: the *Available apt updates* feature line adds "and whether the last check succeeded";
- `docs/ARCHITECTURE.md`: none (the slow-tick paragraph names no state; swept for "updates", "apt");
- decision 0029 is linked from the metrics bullet. No changelog file exists; the release-notes label is the PR's.

## Architecture check

The guarantee "never a fabricated reading" is strengthened (no `0` sample without a check). v1 shapes stay stable:
one new key with its presence rule, no existing key changed (decision 0018). Slow-tick isolation is unchanged.

## Security considerations

- [x] no limits: nothing new is parsed.
- [x] none: no error text reaches a user; the failure reason stays in the log only.
- The new dashboard texts are constants set with `textContent`; `xss_test.go` keeps guarding `.innerHTML`.

## Decision records

- `docs/decisions/0029-apt-check-status-key-and-prometheus-gap.md` (Proposed)

## Out of scope / follow-ups

See [spec.md](spec.md), *Out of scope*.

## Challenge notes

Devil's Advocate, plan mode: specs/feature-281-apt-check-state/plan.md (checked against spec.md, tasks.md and issue #281)

The plan holds up. I checked its claims against the code and found nothing wrong:
- **Collector:** `collector.go:877-885` writes `c.latest.Updates` only on success. `collector.go:308` is the only place `latest` is built, and nothing assigns the whole snapshot later, so starting at `not_checked` in `New` is enough.
- **Prometheus:** `prometheus.go:262-265` writes the family every time, and `writePiholeMetrics` at `prometheus.go:271-274` gives the pattern to copy.
- **Dashboard:** `app.js:297-311` has the `checked_at` guard and re-renders the open dialog on every poll. No other part of `app.js` reads `snap.updates`; I grepped for it.
- **Tests to adapt:** `fullSnapshot()` sets `Updates{Count: 3}` (`prometheus_test.go:36`), and the tests at lines 70, 96, 184 and 234 depend on it, so the planned adaptation is needed and complete. `pihole_test.go:59` pins the line that changes. `routes_test.go` has no exact-JSON test for `/api/v1/metrics/updates`, so no other pinned body breaks.
- **Need:** `updates` feeds no alert rule (`docs/areas/alerts.md:47`). Decision 0029 is in the index as Proposed. No accepted record makes this a NO CHANGE.
- **Alternatives:** setting the status in `slowTick` rather than in `updates.go` is the less invasive option, and it leaves the security-area file alone.

Two minor objections:

1. **Minor: the AC2 test would pass on one wrong build.** As planned, the race test checks only "`ok` ⇒ non-zero `CheckedAt`" and "`not_checked` ⇒ zero `CheckedAt`". Suppose a build assigns the value from `Collect`, whose `CheckStatus` is `""` because `updates.go:134-138` never sets it, and sets `ok` under a second lock. A reader could then see `""` with a new `CheckedAt`, and both checks would still pass. Tester: in `TestCollector_SlowTick_UpdatesStatusMatchesResult`, also assert on every read that `CheckStatus` is one of `UpdatesCheckOK`, `UpdatesCheckNotChecked` or `UpdatesCheckFailed`, and never empty. Dev: set `CheckStatus = UpdatesCheckOK` on the local `updates` before the single locked assignment, as the *Approach* says, never in a separate locked section.

2. **Minor: closing the dialog sends focus to a hidden element.** `wireModalFocusReturn` (`app.js:525-531`) refocuses `modalReturnFocus` on `close`. That is the `updates-show` button, which `renderUpdates` hides outside `ok` with `.hidden { display: none !important; }` (`style.css:801-803`). A focus call on a `display: none` element fails silently, so keyboard focus drops to the page. Dev: in `renderUpdates`, close the dialog only when `dialog.open` is true. If the button is hidden, move focus to the Updates card or another visible element after `close()`, or accept the drop and say so in `dashboard.md`. No new criterion is needed.
