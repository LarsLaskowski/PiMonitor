# Full Code Review — 2026-07-17

Scope: complete review of the repository at commit `c3eac76` (branch
`main`), covering all Go packages (`cmd/`, `internal/`), the embedded web
frontend (`internal/web/assets/`), packaging (`packaging/`), CI/release
workflows, and documentation. Baseline: `go build ./...`, `go vet ./...`,
and `go test ./...` all pass cleanly.

Every finding was checked against the existing GitHub issue tracker
(33 issues, 12 open as of this review). Findings that are already tracked
are listed separately at the end with their issue numbers.

Severity scale: **High** (functional breakage or security impact in a
supported configuration) · **Medium** (real defect or footgun, limited
blast radius) · **Low** (polish, hardening, docs, efficiency).

---

## New findings (not covered by any existing issue)

> Update 2026-07-17: all 17 findings below have been filed as issues #58–#74 (F1 → #58 … F17 → #74).

### F1 — Setting `api_key` breaks the bundled dashboard entirely (filed as [#58](https://github.com/larslaskowski/pimonitor/issues/58))
**Severity: High · Bug · `internal/web/assets/app.js:131-135`, `internal/httpapi/server.go:84-87`**

All four API routes — including `GET /api/v1/config` — are wrapped in
`withAPIKey`, but the dashboard's `fetchJSON` sends no `X-Api-Key` /
`Authorization` header and has no way to obtain one. With `api_key` set,
every data fetch returns `401`, the header permanently shows
"Connection error", and all cards stay empty — while the static assets
themselves still load, so the page looks broken rather than locked.

This directly contradicts the documented guidance: `SECURITY.md` and
`docs/API.md` recommend setting `api_key` before exposing the API to other
systems, without mentioning that doing so disables the built-in dashboard.
Options: exempt the dashboard via a session/cookie mechanism, support a
key prompt persisted in `localStorage`, exempt only `/api/v1/config`, or at
minimum document the limitation prominently and make the dashboard render a
clear "API key required" state instead of "Connection error".

### F2 — `install.sh` never restarts a running service on upgrade (filed as [#59](https://github.com/larslaskowski/pimonitor/issues/59))
**Severity: Medium · Bug · `packaging/install.sh:86-91`**

The script advertises "Installs or upgrades PiMonitor" and replaces
`/usr/local/bin/pimonitor`, but only runs `systemctl enable --now
pimonitor.service`, which is a no-op when the unit is already active. After
an upgrade the old binary keeps running until an unrelated reboot/restart,
and the dashboard footer version silently disagrees with what was just
installed. Fix: `systemctl restart pimonitor.service` when the unit was
already active (and say so in the output).

### F3 — Unknown YAML config keys are silently ignored (filed as [#60](https://github.com/larslaskowski/pimonitor/issues/60))
**Severity: Medium · Robustness/Security · `internal/config/config.go:291-300`**

`loadYAMLFile` uses plain `yaml.Unmarshal`, so a typo like `api_kay:` or a
mis-indented `alerts:` block is silently dropped and the built-in default
(e.g. *no authentication*) applies without any warning. For a security-
relevant key this is a real footgun, and it undermines the stated goal of
`Validate()` ("a typo in an inert config is still caught at startup").
Fix: decode with `yaml.NewDecoder(...).KnownFields(true)` and fail fast on
unknown fields.

### F4 — Memory has no thresholds of its own: UI colors RAM with the *disk* thresholds, and the alert engine never evaluates memory at all (filed as [#61](https://github.com/larslaskowski/pimonitor/issues/61))
**Severity: Medium · Bug/Gap · `internal/web/assets/app.js:195-197`, `internal/alert/alert.go:182-212`, `internal/config/config.go:17-26`**

`renderBar('mem-bar', …, t.disk_warn_percent, t.disk_crit_percent, …)`
color-codes the RAM bar using the disk cutoffs (80/95 by default), which is
coincidental rather than intended semantics. Consistently, `Thresholds` has
no memory pair and `alert.Sample`/`Engine.Evaluate` skip RAM entirely, so a
box sitting at 99 % memory used never fires an alert while swap, disk, CPU
and temperature all do. Adding `memory_warn_percent`/`memory_crit_percent`
(config + engine + UI) would close the gap; note the API impact is additive
(new state metric `memory`), not a breaking change.

### F5 — Webhook rate limiter records a delivery *before* it succeeds (filed as [#62](https://github.com/larslaskowski/pimonitor/issues/62))
**Severity: Low · Bug · `internal/alert/notify.go:199-209`**

`rateLimited` stamps `lastSent[key] = ev.At` at check time, before
`deliver` runs. If that delivery then fails all retries, the next firing of
the same metric within `notify_min_interval_seconds` is still suppressed as
a "repeat" — even though nothing was ever delivered. Stamping only after a
successful `post` (or clearing the stamp on give-up) would make the limiter
count deliveries, not attempts.

### F6 — Webhook delivery retries non-retryable 4xx responses and serializes all deliveries through one worker (filed as [#63](https://github.com/larslaskowski/pimonitor/issues/63))
**Severity: Low · Robustness/Efficiency · `internal/alert/notify.go:214-232`, `128-141`**

`deliver` treats every non-2xx alike, so a permanent `400`/`404`/`410`
still burns the full retry/backoff budget (default ~7 s, worst case much
longer with user-configured timeouts). Combined with the single serial
worker, one dead webhook head-of-line-blocks every other webhook and every
queued event for up to `(retries+1)×timeout + backoffs` per event. Skipping
retries for 4xx (except 408/429) and/or delivering per-webhook concurrently
would bound the damage. (The queue-drop safety valve already exists, so
this is degradation, not loss of collection.)

### F7 — A legitimate 0.0 °C reading renders as "n/a", and the API cannot distinguish 0 °C from "sensor failed" (filed as [#64](https://github.com/larslaskowski/pimonitor/issues/64))
**Severity: Low · Bug · `internal/web/assets/app.js:184-190`, `internal/collector/types.go:118-134`**

The dashboard uses a falsy check (`snap.temperature.celsius`), so an exact
0 °C — plausible for a Pi in an unheated enclosure — shows "n/a". The root
cause is in the API shape: `Snapshot.Temperature` is always marshaled, and a
failed collection yields `{zone:"", celsius:0}`, so `0` is overloaded.
A pointer field (`*Temperature`, omitted on failure like `gpu_temperature`)
would fix it cleanly but is a v1-breaking change; within v1, the frontend
could at least key off `temperature.zone !== ""` instead of the value.

### F8 — No upper bound on history capacity: a misconfigured window/interval can allocate huge buffers (filed as [#65](https://github.com/larslaskowski/pimonitor/issues/65))
**Severity: Low · Robustness · `internal/config/config.go:167-176`, `Validate()`**

`Validate` enforces only `> 0`. `history_window_minutes: 525600` with
`poll_interval_seconds: 0.05` yields ~630 M points per series, allocated
eagerly by `NewRingBuffer` for the 7 scalar series (≈ several GiB) — an
instant OOM on a Pi from a config typo. A sanity cap on
`HistoryCapacity()` (or on the window/interval ratio) in `Validate` would
fail fast with a clear message instead.

### F9 — `vcgencmd` is spawned twice per fast tick (filed as [#66](https://github.com/larslaskowski/pimonitor/issues/66))
**Severity: Low · Efficiency · `internal/collector/temperature.go:195-204`, `internal/collector/throttled.go:90-96`**

`TemperatureCollector` (`measure_temp`) and `ThrottledCollector`
(`get_throttled`) each fork+exec `vcgencmd` on every 5-second tick — two
subprocesses per tick on the hot path, each with its own 5 s timeout that
counts against the tick budget. The two collectors also duplicate the
lazy-redetect logic verbatim. A small shared vcgencmd runner (one detect,
one helper) would halve the exec rate and remove the duplication.

### F10 — Config comment promises the load gauges use the CPU thresholds; the code hardcodes 70 %/100 % of core count (filed as [#67](https://github.com/larslaskowski/pimonitor/issues/67))
**Severity: Low · Docs/Code mismatch · `packaging/pimonitor.example.yaml:60-61`, `internal/web/assets/app.js:486-491`**

`pimonitor.example.yaml` says the thresholds section drives "the
load-average gauge scale, which uses the CPU thresholds relative to core
count", but `renderGauge` uses fixed `cores×0.7` / `cores×1.0`. Either use
`cpu_warn_percent`/`cpu_crit_percent` relative to core count as documented,
or fix the comment.

### F11 — `-api-key` CLI flag exposes the secret in the process list (filed as [#68](https://github.com/larslaskowski/pimonitor/issues/68))
**Severity: Low · Security note · `internal/config/config.go:318`, `docs/API.md`, `README.md:278`**

Any local user can read `/proc/<pid>/cmdline`, so passing the key via
`-api-key` leaks it (unlike the 640-permission config file, which the
installer gets right). Worth a warning in README/API.md that the flag is
for development only, and/or supporting an environment variable instead.

### F12 — `go.mod` marks `gopkg.in/yaml.v3` as `// indirect` although it is a direct dependency (filed as [#69](https://github.com/larslaskowski/pimonitor/issues/69))
**Severity: Low · Hygiene · `go.mod:5`**

`internal/config` imports it directly; the comment is stale. `go mod tidy`
fixes it. (CI would not catch this today; a tidy-check step would.)

### F13 — `disks` is `null` (not `[]`) before the first tick, contradicting API.md (filed as [#70](https://github.com/larslaskowski/pimonitor/issues/70))
**Severity: Low · Docs/API nit · `internal/collector/types.go:130`, `docs/API.md:148-151`**

API.md says fields may briefly read as "empty arrays", but a nil
`[]Disk`/`[]NetworkInterface` marshals as `null` (`network` at least has
`omitempty`; `disks` does not). Strict JSON consumers iterating `disks`
will trip on `null`. Either initialize the slices, add `omitempty` for
consistency, or fix the docs sentence.

### F14 — Embedded static assets are served with no cache validators or `Cache-Control` (filed as [#71](https://github.com/larslaskowski/pimonitor/issues/71))
**Severity: Low · Efficiency · `internal/web/embed.go:15-21`**

Files from `embed.FS` have a zero ModTime, so `http.FileServerFS` emits no
`Last-Modified`/`ETag`, and no `Cache-Control` is set — every page load
refetches all assets. Harmless on a LAN, but a small
`Cache-Control: no-cache` plus content-hash or version-derived `ETag`
would give correct, cheap revalidation (and avoids stale-JS confusion after
upgrades if any intermediary caches heuristically).

### F15 — Webhooks configured while `alerts.enabled: false` silently never fire (filed as [#72](https://github.com/larslaskowski/pimonitor/issues/72))
**Severity: Low · Config UX · `cmd/pimonitor/main.go:46-64`, `internal/config/config.go:237-259`**

The notifier is built and its worker started, but with the engine disabled
no events are ever produced. A user who configured webhooks but flipped
`enabled: false` (or vice versa) gets no hint. A startup warning ("webhooks
configured but alerts disabled") would catch this in the journal.

### F16 — CI actions and golangci-lint are unpinned (`version: latest`) (filed as [#73](https://github.com/larslaskowski/pimonitor/issues/73))
**Severity: Low · Supply chain/Reproducibility · `.github/workflows/ci.yml:33-35`, `release.yml`**

`golangci-lint-action@v6` with `version: latest` means lint rules change
under the project without a commit, and workflow actions are pinned only by
major tag rather than SHA. Pinning the linter version (and optionally
action SHAs) makes CI reproducible and tamper-evident.

### F17 — `pimonitor-apt-update.service` runs as root with no sandboxing (filed as [#74](https://github.com/larslaskowski/pimonitor/issues/74))
**Severity: Low · Hardening · `packaging/pimonitor-apt-update.service`**

The main service got extensive hardening (#22/#52), but the root-privileged
timer unit has none. `apt-get update` genuinely needs root and network, but
cheap directives still apply: `ProtectHome=true`, `PrivateTmp=true`,
`ProtectKernelModules=true`, `NoNewPrivileges=` is not possible with apt's
sandbox user drop, but e.g. `CapabilityBoundingSet` trimming and
`ProtectSystem=full` with the apt paths in `ReadWritePaths` are worth
evaluating. Also consider `Nice=`/`IOSchedulingClass=idle` so the 6-hourly
refresh doesn't compete with workloads on slow SD cards.

---

## Findings already tracked by open issues (verified still present)

| Finding | Where verified | Existing issue |
|---|---|---|
| JSON responses (esp. `/api/v1/metrics/history`, ~1 MB+ at defaults) served uncompressed; note the fixed 10 s `WriteTimeout` (`server.go:96`) compounds this on slow links | `internal/httpapi/handlers.go:8-13` | **#23** (open) |
| Per-device history maps `diskHist`/`rxHist`/`txHist` only ever grow; churning mountpoints/interfaces (containers, USB, VPN) leak ring buffers and bloat history responses and the persist file | `internal/collector/collector.go:333-355`, `persist.go:355-368` | **#24** (open) |
| `GET /healthz` is a static `ok`; a stalled collector goroutine is invisible to systemd/uptime monitors | `internal/httpapi/handlers.go:15-18` | **#42** (open) |
| Dashboard never surfaces alert states/events; `app.js` has no `/api/v1/alerts` poll although the engine and API exist | `internal/web/assets/app.js` (no reference to `alerts`) | **#11** (open) |
| Access logging is hardwired to debug level with no toggle, and the server exposes no self-metrics | `internal/httpapi/middleware.go:13-25` | **#43** (open) |
| No built-in TLS; API key travels in cleartext unless a reverse proxy terminates TLS | `internal/httpapi/server.go:109-112` | **#41** (open) |

No finding in this review duplicates a **closed** issue: spot-checks of the
closed security/robustness issues (#18 constant-time key compare — fixed in
`middleware.go:58-60`; #19 XSS — frontend uses `textContent` throughout,
covered by `xss_test.go`; #20 config permissions — `install.sh:63-77` uses
640/750; #25 config validation; #26 statfs timeout; #27 Bavail + mountpoint
dedup; #28 sensor re-detection) confirmed all of them remain properly fixed.

---

## Positive observations (no action needed)

- Every `/proc`,`/sys` parser is fixture-testable and defensive; error paths
  degrade to empty values off-Pi, matching the stated design.
- The alert engine's per-boundary debounce (`warnSince`/`critSince` tracked
  independently) correctly handles values flapping around the crit cutoff,
  and invalid-sample handling prevents bogus zeros from clearing alerts.
- The persistence format has explicit decode limits (`maxSeries`,
  `maxKeyLen`, `maxPointsPerSeries`), a magic/version check, trailing-data
  rejection, and atomic writes — a corrupt or malicious `history.bin`
  cannot cause large allocations or partial state.
- `statfs` hang protection (timeout + per-mountpoint cooldown) and the
  network-FS exclusion list are careful and well documented.
- systemd hardening of the main service is thorough and its non-obvious
  trade-offs (`PrivateDevices`, `ProtectClock`, `SystemCallErrorNumber`)
  are explained in comments.
- HTTP security headers, constant-time API-key comparison, and the strict
  self-contained CSP are all in place and tested.
