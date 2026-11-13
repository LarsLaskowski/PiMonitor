# Plan: Show the host name in the dashboard header and browser tab

Source: Issue #179 | [spec.md](spec.md)
Tier: security — the change adds a disclosure opt-out (`hostname_enabled`) for a value published on an API that is
unauthenticated by default, and a new sink for server text in the dashboard whose text-only insertion is the guard
against markup injection (output escaping counts as a security control, `.squad/routing.md`, *Tiers*).
Security areas touched: Dashboard XSS (`internal/web/assets/app.js`, `index.html`); Parsing of local input
(`/proc/sys/kernel/hostname` in `internal/collector`, the new YAML option in `internal/config`)

## Problem / root cause

Feature: see [spec.md](spec.md), *Problem*.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | No host name exists in the API, the collector or the dashboard today | `grep -rni hostname` over `internal`, `cmd`, `docs`: only `ProtectHostname` in `docs/areas/deployment.md:77` | confirmed |
| 2 | `SystemInfo` has four keys, none `omitempty` | `internal/collector/types.go:159-164` | confirmed |
| 3 | `SystemInfo` is built once at start; toggles blank fields after reading | `collectSysInfo`, `internal/collector/collector.go:447-464`, called only from `Run` (`:331`) and tests | confirmed |
| 4 | Sysinfo readers take their path as a struct field | `SysInfoCollector`, `internal/collector/sysinfo.go` (fields set in `NewSysInfoCollector`) | confirmed |
| 5 | No per-metric sub-resource serves `system` | `metricsSubResources`, `internal/httpapi/routes.go:110-118` | confirmed |
| 6 | Additive optional keys were added to `v1` before | `wireless` (a24fc0c, #160), `pihole` (ff34c70); `docs/API.md:7-9` excludes only breaking changes | confirmed |
| 7 | The collector config is built inline in `run()`, untested | `cmd/pimonitor/main.go:51-70`; `cmd/pimonitor/main_test.go` tests only `serverConfig`, `clientConfig`, `warnIfNotifierInert`, `awaitCollector` | confirmed |
| 8 | The README *Configuration* section lists no options; the issue's request to add the toggle there has no place to land | `README.md:467-481` (the issue claims the toggle belongs there) | refuted |
| 9 | The issue's test conventions file is `docs/TESTS.md` | the file is `docs/UNIT_TESTS.md` (issue claim) | refuted |
| 10 | The kernel host name is at most 64 bytes, and an unset name reads `(none)` | Linux `include/uapi/linux/utsname.h` (`__NEW_UTS_LEN 64`), `init/Kconfig` (`DEFAULT_HOSTNAME` default `"(none)"`) | confirmed |
| 11 | The service sees the host's name under its hardening | to verify: after merge, the Product Manager checks on a Pi with the installed unit that the header shows the Pi's name (verification issue, step 10); read so far: `packaging/pimonitor.service:51,75` has `ProtectKernelTunables=true` (read-only `/proc/sys`) and `ProtectHostname=true` (own UTS namespace initialized from the host's), no `ProcSubset=`/`ProtectProc=` | unverified |
| 12 | The dashboard has no JS test runner; client behavior is guarded by static scans of the assets | `docs/UNIT_TESTS.md` (test type 3), `internal/web/pihole_test.go`, `polling_test.go` | confirmed |
| 13 | JS, HTML and CSS are outside the coverage measure in both places | `COVERAGE_PATHSPECS = ["*.go"]` (`squad_settings.py`); `sonar.coverage.exclusions=internal/web/assets/**` | confirmed |
| 14 | A failed poll and a `401` leave the page as it is; only `renderMetrics` writes snapshot data | `pollMetrics`, `internal/web/assets/app.js:1155-1173` | confirmed |
| 15 | `.hidden` is `display: none !important`, and the page has no inline style (CSP) | `internal/web/assets/style.css:726-728`; `docs/areas/dashboard.md`, *Rendering* | confirmed |
| 16 | `metrics.md` says undeterminable system identity values are not logged | `docs/areas/metrics.md:194-196` (the host name becomes the exception) | confirmed |
| 17 | `encoding/json` replaces invalid UTF-8 with U+FFFD and escapes `<`, `>`, `&` | Go `encoding/json` documentation (`Marshal`) | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestReadHostname`: "AC1 trailing newline trimmed", "AC1 surrounding whitespace trimmed", "AC1 original case kept" | `internal/collector/sysinfo_test.go` |
| AC1 | `TestCollector_CollectSysInfo_Hostname`: "AC1 enabled copies the fixture name into System.Hostname", "AC1 a fast tick does not re-read the name" (fixture rewritten after `collectSysInfo`, then `fastTick`: value unchanged) | `internal/collector/collector_test.go` |
| AC1 | `TestHandleMetrics_Hostname`: "AC1 system.hostname present and the four existing system keys unchanged" (decoded into `map[string]any`) | `internal/httpapi/handlers_test.go` |
| AC2 | `TestReadHostname`: "AC2 empty file", "AC2 whitespace only", "AC2 kernel placeholder (none)", "AC2 missing file returns an error" | `internal/collector/sysinfo_test.go` |
| AC2 | `TestCollector_CollectSysInfo_Hostname`: "AC2 unreadable source logs one warning" (after `collectSysInfo` and two `fastTick`s the log holds `could not read host name` exactly once, `Hostname` empty), "AC2 empty source logs nothing" | `internal/collector/collector_test.go` |
| AC2 | `TestHandleMetrics_Hostname`: "AC2 key omitted when the host name is empty" | `internal/httpapi/handlers_test.go` |
| AC3 | `TestDefault_HostnameEnabled`: "AC3 defaults to true"; `TestLoad_HostnameEnabledFromYAML`: "AC3 false is parsed", "AC3 a file without the key keeps true" | `internal/config/config_test.go` |
| AC3 | `TestCollector_CollectSysInfo_Hostname`: "AC3 disabled leaves Hostname empty for a readable source", "AC3 disabled logs nothing for a missing source" | `internal/collector/collector_test.go` |
| AC3 | `TestCollectorConfig_MapsFields`: "AC3 hostname_enabled false reaches the collector", "AC3 default true reaches the collector" (plus every other field `run()` mapped before) | `cmd/pimonitor/main_test.go` |
| AC4 | `TestIndexHTML_HostnameInHeader`: "AC4 hidden host name element directly after the brand"; `TestAppJS_HostnameRenderedPerSnapshot`: "AC4 header element toggled and filled", "AC4 rendered only from renderMetrics"; `TestStyleCSS_HostnameStyle`: "AC4 muted normal-weight style"; manual: Product Manager, local `make build` of the branch head (below) | `internal/web/hostname_test.go` |
| AC5 | `TestAppJS_HostnameRenderedPerSnapshot`: "AC5 tab title with and without a host name"; manual (below) | `internal/web/hostname_test.go` |
| AC6 | `TestIndexHTML_HostnameRowFirstInSystemCard`: "AC6 hidden Hostname row is the card's first row"; `TestAppJS_HostnameRenderedPerSnapshot`: "AC6 row toggled and filled" | `internal/web/hostname_test.go` |
| AC7 | `TestAppJS_HostnameOnlyAsText`: "AC7 renderHostname uses no HTML sink"; the existing `internal/web/xss_test.go` passes unchanged; manual case 4 (below) | `internal/web/hostname_test.go` |
| AC8 | `TestStyleCSS_HostnameStyle`: "AC8 name truncates with an ellipsis", "AC8 brand column may shrink"; manual case 3 (below) | `internal/web/hostname_test.go` |

**Manual test** (step 7, `7 Manual test`, by the Product Manager, on a local build of the branch head: `make build`,
then `bin/pimonitor -config <copy of packaging/pimonitor.example.yaml>` on a Linux machine, browser at
`http://localhost:8080`, desktop width and a 375 px device in the browser's responsive mode):

1. Default config: header `PiMonitor (<name>)` with the name muted and normal weight, tab `<name> – PiMonitor`,
   `Hostname` first row of the System card (AC4, AC5, AC6).
2. `hostname_enabled: false`: header, tab title and card as on `main` (AC3–AC6).
3. A 64-character name: `sudo unshare --uts sh -c 'hostname <64 characters>; exec bin/pimonitor -config …'`; at
   375 px the name ends in `…`, hovering shows the full name, the buttons stay on the header's row (AC8).
4. The name `<b>pi</b>` the same way: shown literally in header, tooltip, card and tab (AC7).

**After merge:** the Product Manager confirms fact 11 on a Pi with the installed unit (verification issue, step 10).

## Approach

**Collector.** `sysinfo.go` gets the constant `hostnamePath = "/proc/sys/kernel/hostname"`, a `hostnamePath` field
on `SysInfoCollector` set in `NewSysInfoCollector`, the function `readHostname` (*Signatures*) and the unexported
method `hostname() (string, error)` returning `readHostname(c.hostnamePath)`. `readHostname` reads the file
(`os.ReadFile`; a read error is returned wrapped, with `""`), trims surrounding whitespace with `strings.TrimSpace`,
and returns `""` with a nil error for an empty result or `(none)`. `Collect()` is unchanged: it does not read the
host name, so an opted-out source is never read. `collectSysInfo` reads it only when `c.cfg.HostnameEnabled`; on an
error it logs `c.log.Warn("could not read host name", "error", err)` and leaves the field empty. `collectSysInfo`
runs once per `Run`, so the warning comes once per process start.

**Config.** `HostnameEnabled` (`yaml:"hostname_enabled"`) after `PiModelEnabled`, `true` in `Default()`; no
`Validate()` rule.

**Wiring.** The `collector.Config` literal in `run()` moves unchanged into `collectorConfig` (*Signatures*),
together with the `PersistPath` assignment, and gains `HostnameEnabled: cfg.HostnameEnabled`; `run()` calls it.

**Dashboard.** Exact markup the static tests read:

- `index.html`, the header's first `<div>` gets `class="header-brand"`; the `<h1>` becomes
  `<h1>🖥️ PiMonitor<span class="header-hostname hidden" id="header-hostname">(<span class="header-hostname-name" id="header-hostname-name"></span>)</span></h1>`
  — no whitespace between `PiMonitor` and the span, so a hidden span leaves the header as today.
- `#card-system`, directly after its `<h2>`:
  `<div class="kv-row hidden" id="sys-hostname-row"><span class="kv-key">Hostname</span><span class="kv-val" id="sys-hostname"></span></div>`.
- `app.js`, a new function beside `renderMetrics`, and in `renderMetrics`, in the `// System` block before
  `setText('sys-kernel', …)`, the call `renderHostname(snap.system.hostname || '');` — its only caller, so a failed
  poll or a `401` changes nothing:

  ```js
  function renderHostname(name) {
    document.title = name ? name + ' – PiMonitor' : 'PiMonitor';
    document.getElementById('header-hostname').classList.toggle('hidden', !name);
    setText('header-hostname-name', name);
    document.getElementById('header-hostname-name').title = name;
    document.getElementById('sys-hostname-row').classList.toggle('hidden', !name);
    setText('sys-hostname', name);
  }
  ```
- `style.css`: `.header-brand { min-width: 0; }`; `.header-hostname` with `font-weight: normal`,
  `color: var(--muted)`, `margin-left: 0.35em`; `.header-hostname-name` with `overflow: hidden`,
  `text-overflow: ellipsis`, `white-space: nowrap`. Any further layout property needed for the truncation to take
  effect (`display`, `min-width`, `max-width`, flex on `h1`) is the Dev's, checked by manual case 3. The System card
  row wraps through the existing `.kv-val { overflow-wrap: anywhere }`.

The static tests compare these strings literally (whitespace inside a line as written here; the en dash is U+2013,
written as the character).

## Options considered

1. **The standard library's host name call** instead of the file — same value on Linux, no fixture path
   ([0006](../../docs/decisions/0006-publish-host-name-by-default.md)).
2. **Read in `Collect()` and blank afterwards** (the distribution/model pattern) — would read an opted-out source
   and log its failure (spec, *Challenge* 3).
3. **An empty `hostname` key like its siblings** instead of omitting it — rejected in 0006.
4. **Opt-in instead of opt-out** — rejected in 0006.
5. **Re-reading the name every tick** — `ProtectHostname=true` freezes it; out of scope in the spec.
6. **Leave the collector config inline in `run()`** — the toggle's wiring, the privacy opt-out, would be the one
   untested step; extracting `collectorConfig` costs one function.
7. **Upper case, a centered name, the System card only** (issue, *Alternatives Considered*) — rejected there: width
   on phones, a collapsing flex row, and no help telling tabs apart.
8. **Expose `hostname_enabled` in `GET /api/v1/config`** — the key's presence already tells the dashboard; a v1
   config shape change for nothing.
9. **A dashboard element created by script instead of static markup** — static hidden markup is what the static
   tests can pin, and it follows the Pi-hole section's pattern (`pihole_test.go`).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `types.go` | `SystemInfo.Hostname` |
| `internal/collector` | `sysinfo.go` | path constant and field, `readHostname`, `hostname()` |
| `internal/collector` | `collector.go` | `Config.HostnameEnabled`; `collectSysInfo` reads and logs |
| `internal/config` | `config.go` | `HostnameEnabled`, default `true` |
| `cmd/pimonitor` | `main.go` | `collectorConfig` extracted and used by `run()` |
| `internal/web/assets` | `index.html`, `app.js`, `style.css` | header element, card row, `renderHostname`, styles |

## Signatures (for the Dev's skeleton)

- `internal/collector/types.go`, in SystemInfo after CPUModel: ``Hostname string `json:"hostname,omitempty"` ``
- `internal/collector/collector.go`, in Config after PiModelEnabled: `HostnameEnabled bool`
- `internal/config/config.go`, in Config after PiModelEnabled: ``HostnameEnabled bool `yaml:"hostname_enabled"` ``
- `internal/collector/sysinfo.go`: `func readHostname(path string) (string, error)` (called by the tests)
- `cmd/pimonitor/main.go`: `func collectorConfig(cfg config.Config, notifier *alert.Notifier) collector.Config`
  (called by the tests)

Names checked against .squad/stack.md (*Writing code*, *Logger arguments*) and .golangci.yml: initialism-free, no
stutter, doc comments on the exported fields; the log call carries only the error.

## Test files

`internal/collector/sysinfo_test.go`, `internal/collector/collector_test.go`, `internal/config/config_test.go`,
`internal/httpapi/handlers_test.go`, `cmd/pimonitor/main_test.go` (existing) and `internal/web/hostname_test.go`
(new; `internal/web` names its asset scans by topic, as `pihole_test.go`). Collector tests set `c.sysInfo` to a
`SysInfoCollector` with a `t.TempDir()` path and use `newCapturingLogger` (`failurelog_test.go`).

Existing test code that calls a changed signature: none. Kept unchanged, with no input a new criterion handles
differently: `TestSysInfoCollector_Collect` (`Collect` does not read the host name),
`TestCollector_CollectSysInfo_TogglesDistroAndPiModel` and `newTestCollector` (`HostnameEnabled` zero: not read),
`TestHandleMetrics` (`newTestServer` has no host name; decodes into the struct).

Files no unit test can cover: `internal/web/assets/index.html`, `app.js`, `style.css` — no JS test runner (fact
12); already outside the coverage measure in both places (fact 13), so no new exclusion and no record.

## Areas

- Metrics: `system.hostname`, and the host name as the one system identity value whose failure is logged.
- Configuration: the `hostname_enabled` row.
- Dashboard: header, tab title, System card row, truncation, failures.

## Documentation updates

All by the Dev, once the code is green:

- `docs/areas/metrics.md`, *System identity and uptime*: a bullet for `system.hostname` — `/proc/sys/kernel/hostname`
  trimmed of surrounding whitespace, read once at startup (a later rename shows after a restart), **omitted** (not
  empty, unlike the other `system` keys) when unreadable, empty, only whitespace or `(none)`, and when
  `hostname_enabled` is off, in which case the file is not read; link 0006. *Failures*: "logged ... once at
  startup" gains the host name, and "system identity values that cannot be determined" becomes "other system
  identity values".
- `docs/areas/configuration.md`: row after `pi_model_enabled`: `hostname_enabled` | bool | `true` | — | Whether the
  host name is read and published ([Metrics](metrics.md)).
- `docs/areas/dashboard.md`, *Rendering*: host name added to the text-node list; new bullets: header name in
  parentheses after the brand, muted, original case, cut with an ellipsis and full name as tooltip; tab title
  `<host name> – PiMonitor` or `PiMonitor`; `Hostname` first row of the System card, hidden without a name; all
  evaluated on every snapshot. *Failures*: a failed poll and a `401` keep the host name and title.
- `docs/API.md`: `"hostname": "raspberrypi"` in the example `system` object; a note that it is the host name read at
  startup, omitted when unknown or when `hostname_enabled` is off; the zero-values note after process start
  (`docs/API.md:347-350`) adds that `system.hostname` may be briefly absent.
- `packaging/pimonitor.example.yaml`: `hostname_enabled: true` after `pi_model_enabled`, with a comment that the
  host name appears in the dashboard and the API, which needs no key unless `api_key` is set.
- `README.md`, *Features*: "System identity - host name, kernel version, OS distribution, Raspberry Pi model".
- `AGENTS.md` (project block `overview`) and `.github/copilot-instructions.md`, the same sentence in both
  (`config-check.py`): "(... kernel version," becomes "(... host name, kernel version,". If the file guard denies
  the edit, the Dev reports the exact text and the orchestrator writes it.
- `docs/ARCHITECTURE.md`, `SECURITY.md`, `.squad/project.md`: none — the sweep found no enumeration the change
  extends (`/proc` is already listed as read; no list of `system` keys or toggles).

## Architecture check

No guarantee of `.squad/project.md` is weakened: `v1` gains an optional key only (fact 6); the source is a `/proc`
file, hand-rolled, no dependency; an unreadable source yields an absent value and a warning, never a crash; the
service gains no privilege (the file is world-readable). `docs/ARCHITECTURE.md` needs no change.

## Security considerations

- [x] No limits: the kernel bounds the file to 64 bytes plus a newline (fact 10); no server-side length cap is
  added, and the dashboard's truncation is presentation only.
- [x] No exception reaches a user: a read error only goes to the log (`could not read host name`, with the
  `os.ReadFile` error and its path); the API omits the key.
- Untrusted text: the name is set only by root on the host, but is treated as untrusted: JSON-encoded by
  `encoding/json` (fact 17) and inserted only through `textContent`, `document.title` and the `title` property
  (AC7). Control or bidi characters are not filtered: they are displayed as text, which spoofs nothing beyond what
  the host's own root could write.
- Disclosure: published by default on an API that needs no key unless `api_key` is set; opt-out not reading the
  source at all (AC3, [0006](../../docs/decisions/0006-publish-host-name-by-default.md)).

## Decision records

- `docs/decisions/0006-publish-host-name-by-default.md` (Proposed), indexed.

## Out of scope / follow-ups

As the spec's *Out of scope*; no follow-up issue.

## Security notes

Security plan review: feature 179 (host name), round 1, mode `plan`

I approve the plan. There are no blocking findings and one non-blocking finding.

Files read: `specs/feature-179-hostname/spec.md`, `plan.md`, `.squad/project.md` (*Security areas*), `internal/web/assets/app.js` (`setText` at :136-139, the System block at :277-280, the three `innerHTML` uses), `internal/web/assets/index.html` (header, `#card-system`), `internal/web/xss_test.go`, `internal/httpapi/middleware.go` (CSP), `internal/collector/collector.go` (`collectSysInfo`, `Run`), `internal/collector/sysinfo.go`, `packaging/pimonitor.service`, `docs/decisions/0006-publish-host-name-by-default.md`, and `.squad/stack.md` (*Logger arguments*).

**Tier and areas**
- The `security` tier is right. The plan adds a disclosure opt-out and a new sink for server text in the dashboard.
- *Security areas touched* lists Dashboard XSS and Parsing of local input, which matches what the plan changes.
- No other area is affected:
  - No new log field holds secrets.
  - There are no subprocesses, file writes or outbound calls.
  - The HTTP surface does not change: there is no new route, and the `system` key has no sub-resource (`metricsSubResources`).
  - Privileges do not change: `/proc/sys/kernel/hostname` is world-readable, and the unit's `ProtectKernelTunables=true` keeps it readable.

**Dashboard XSS**
- The proposed `renderHostname` writes the name only through `textContent` (via `setText`), `document.title` and the `title` property. None of these parses HTML.
- The static markup in `index.html` has no inline style, so the CSP (`style-src 'self'`) is unaffected.
- The single caller in `renderMetrics` matches how the other system fields are accessed. `snap.system.kernel_version` at :278 already relies on `snap.system` being present.
- Not filtering bidi or control characters is acceptable. Only root on the host can set the name, and it is displayed as text.

**Parsing of local input**
- `readHostname` does `os.ReadFile` on a constant path, then `TrimSpace`, then maps empty or `(none)` to no name.
- The kernel limits the source to 65 bytes, so there is no unbounded allocation.
- No parser has bypass forms to enumerate: the value is used as one opaque string.
- The new YAML bool needs no validation rule. A file without the key keeps the default.
- With `hostname_enabled: false` the file is never read, because the read moves out of `Collect()` into `collectSysInfo` behind the flag. That makes the opt-out real rather than cosmetic, and AC3 plus `TestCollectorConfig_MapsFields` pin it end to end.

**Logger arguments**
- The planned call `c.log.Warn("could not read host name", "error", err)` passes only `err`, whose text is the `os.ReadFile` path error.
- No argument name matches the sensitive-name list, and the host name itself is never logged.

**Measurements**
- No security property here depends on measured or library behavior. The XSS guard is the text-only DOM API, not `encoding/json` escaping (fact 17 is defense in depth only).
- Fact 11 (the service sees the host's name under `ProtectHostname`) is about whether the feature works, not about security. Leaving it to the Product Manager's after-merge check is acceptable.
- I ran no probes.

**Findings**

1. Non-blocking: the AC7 static test is underspecified (`plan.md`, *Tests per criterion*, AC7: `TestAppJS_HostnameOnlyAsText` "AC7 renderHostname uses no HTML sink").
   - The plan does not say which sinks the test rejects, and the existing `xss_test.go` only scans for `innerHTML`.
   - Proposed denylist, every HTML-parsing or script sink the browser offers: `innerHTML`, `outerHTML`, `insertAdjacentHTML`, `document.write`/`writeln`, `createContextualFragment`, `DOMParser`/`parseFromString`, `setHTMLUnsafe`, `srcdoc`, `setAttribute(` (which could set `on*`/`href`), and `eval`/`Function(`.
   - A denylist goes stale, so the stronger form is an allowlist. Extract the body of `function renderHostname` and assert that every write in it is one of `setText(`, `document.title =`, `.title = name`, or `.classList.toggle(`. Assert also that it contains none of the sinks above.
   - Recommendation: fix now. The Tester writes this test in step 5 anyway, so the Lead only has to add the sentence to the AC7 row.

## Challenge notes

I found nothing major in the plan for specs/feature-179-hostname. The plan's facts that I checked hold, and it covers every place the change touches. There are two minor objections.

**Facts checked:**
- `collectSysInfo` is at `internal/collector/collector.go:447-464`, and `Run` (`:331`) is its only production caller, so the warning really comes once per start.
- `SystemInfo` is at `types.go:159-164`.
- The collector config is built inline in `cmd/pimonitor/main.go:51-70`, and `main.go:72` is the only production `collector.New`. So the zero-value `HostnameEnabled` in the test helpers is safe.
- Nothing else reads `SystemInfo` field by field. The only other reader is `app.js:277-280`, so no Prometheus or webhook output changes.
- The unit has no `ProcSubset=` or `ProtectProc=` (`packaging/pimonitor.service:44-75`).
- `install.sh:70-76` never overwrites an existing config, and config loading uses `KnownFields(true)` (`config.go:451`). Old config files simply get the default, as N1 says.
- No existing test pins the `<h1>` or the `<title>` markup, and `xss_test.go` only scans for `innerHTML`.

**Minor objections** (written for the plan's *Challenge notes*):

1. **Truncation (AC8).** `overflow: hidden` and `text-overflow: ellipsis` do nothing on an inline `<span>`. As written, `.header-hostname-name` and the scan "AC8 name truncates with an ellipsis" would pass while nothing gets cut. `header h1` is a plain block (`internal/web/assets/style.css:59-62`).
   - Dev: give the name element a `display` that is not inline, and constrain its width. For example, make `header h1` and `.header-hostname` flex rows with `min-width: 0` and the name `flex: 0 1 auto; min-width: 0`.
   - Dev: keep the closing `)` visible.
   - Dev: check in manual case 2 that a hidden name still leaves the header looking as it does today.
   - Tester: also make the AC8 scan require a non-inline `display` (and `min-width: 0`) on the truncating element.
   - This is minor because manual case 3 would catch it and the plan already gives these properties to the Dev.

2. **Deployment document.** The `ProtectHostname` row in `docs/areas/deployment.md:77` says only "the hostname cannot be changed". The new behavior in S5 comes from this directive. Dev: extend that row's *Effect* cell to say the service keeps the host name it had at start, so `system.hostname` shows a rename only after a restart. Link it to `metrics.md`.

**Files:**
- /home/user/PiMonitor/specs/feature-179-hostname/plan.md
- /home/user/PiMonitor/internal/web/assets/style.css
- /home/user/PiMonitor/docs/areas/deployment.md
