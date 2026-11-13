# Project

What the squad needs to know about this project that is not stack-specific. Read by the Lead, the Devil's
Advocate, Security, the Tester and the Reviewer. Not template-managed: `adopt-template` creates it once
and never overwrites it. A product PR updates it when the change makes an entry untrue
(`.squad/routing.md`, *Scope of a product PR*).

Every entry points: it names files, types or endpoints and links the document that says more. Behavior, values,
limits and sequences are written once, in the area documents (`docs/areas/README.md`), and the reasons in the
decision records; a copy here goes stale with the next change. `config-check.py` warns when this file grows past
`PROJECT_MAX_WORDS` words (`.squad/tools/squad_settings.py`).

## Security areas

A change that touches one of these lists it under *Security areas touched* in its plan, so Security reviews the
plan (`.squad/routing.md`, *Tiers*, which also says when a change is tier `security`). One line per area: what is
protected, the concrete types, files or endpoints, and a link to the area document that states its rules
(`- Session tokens: issued in src/Auth/, checked by every /api route — [Sessions](../docs/areas/sessions.md)`); no
limits, patterns or sequences.

- Privilege separation: `pimonitor.service` runs unprivileged (only the `video` group), only
  `pimonitor-apt-update.service`/`.timer` runs as root; `packaging/` units and `install.sh` —
  [Deployment](../docs/areas/deployment.md),
  [Threat model](../SECURITY.md#threat-model--design-notes)
- API key: read in `internal/config/config.go` (`api_key`, `PIMONITOR_API_KEY`, unset after reading, `-api-key`
  flag), compared in constant time by `withAPIKey` (`internal/httpapi/middleware.go`), kept by the dashboard in
  `localStorage`; config file mode set by `install.sh` — [Authentication](../docs/areas/http-api.md#authentication),
  [Threat model](../SECURITY.md#threat-model--design-notes)
- Subprocesses: `exec.CommandContext` with fixed argument lists and an explicit minimal environment in
  `internal/collector/updates.go` (`apt list --upgradable`) and `internal/collector/vcgencmd.go` —
  [Metrics](../docs/areas/metrics.md), [Threat model](../SECURITY.md#threat-model--design-notes)
- HTTP surface: routes and middleware in `internal/httpapi` (security headers, gzip, no-store, `withMaxInFlight`,
  optional TLS via `tls_cert`/`tls_key`) — [HTTP API](../docs/areas/http-api.md),
  [Concurrency limit](../docs/areas/http-api.md#concurrency-limit)
- Dashboard XSS: `internal/web/assets/app.js` builds DOM nodes without `.innerHTML`, guarded by
  `internal/web/xss_test.go`; CSP in `internal/httpapi` —
  [Dashboard](../docs/areas/dashboard.md)
- Parsing of local input: `/proc`, `/sys`, `/etc/os-release`, `/etc/pihole/versions` and command output in
  `internal/collector`; the YAML config in `internal/config` —
  [Metrics](../docs/areas/metrics.md),
  [Configuration](../docs/areas/configuration.md)
- File writes: `<data_dir>/history.bin` (atomic write, bounded decode) in `internal/collector/persist.go` —
  [History](../docs/areas/history.md)
- Outbound calls: webhook delivery (`text/template` bodies with the `json` helper, retries, per-webhook queues) in
  `internal/alert/notify.go` — [Notifications](../docs/areas/notifications.md)
- Logging: `slog` calls across `internal/` and `cmd/pimonitor`; no call logs the API key; the notifier names a
  webhook by its index in `alerts.webhooks` and by its URL's scheme and host, and redacts the URL in `*url.Error`
  texts, so it never logs the URL's userinfo, path, query or fragment itself — except where the endpoint echoes
  them back in a malformed response, which `net/http` quotes (`internal/alert/notify.go`); see
  [Notifications](../docs/areas/notifications.md#what-is-logged). Keep secrets out of new log fields
  (`.squad/stack.md`, *Logger arguments*)

## Guarantees

Deliberate behavior that must not change without the Product Manager. One line each: the guarantee in a few words,
a link to its decision record where it was a real choice, and a link to the area document that states it.

- The main service never needs root; only the apt-cache timer runs privileged —
  [Deployment](../docs/areas/deployment.md)
- `/api/v1/...` response shapes are stable; a shape change adds `/api/v2/...` — [API.md](../docs/API.md),
  [Versioning](../docs/areas/http-api.md#versioning-and-stability)
- A missing hardware source yields an empty/zero value and a logged warning, never a crash or a fabricated
  reading — [Metrics](../docs/areas/metrics.md); where the code falls short of this today is listed under *Open
  questions* there
- `gopkg.in/yaml.v3` is the only runtime dependency; `/proc`/`/sys` parsing stays hand-rolled —
  [Stability policy](../docs/CONTRIBUTING.md#stability-policy)
- A hung or failing webhook never stalls collection, and `cleared` events bypass the rate limiter —
  [Notifications](../docs/areas/notifications.md)
- `history.bin` is written atomically, a corrupt file never stops startup, and a layout change bumps
  `historyVersion` instead of migrating — [History](../docs/areas/history.md)
- `install.sh` upgrades in place and never overwrites an existing `/etc/pimonitor/config.yaml` —
  [Deployment](../docs/areas/deployment.md)

## Integration surface

What the Reviewer checks when the diff introduces or changes a thing of this kind: every place that must
change with it. For anything else the diff adds, grep the whole repository — `docs/`, `README.md` and
`SECURITY.md` included — for the new identifier and check that every statement naming it is still true.

**A new or changed configuration option** touches:
- the struct field and `Default()` in `internal/config/config.go`, plus `Validate()` if it has a valid range
- `serverConfig()` / `clientConfig()` in `cmd/pimonitor/main.go` if the value must reach the HTTP layer
- `docs/API.md` if `clientConfig()` exposes it through `GET /api/v1/config` (a documented v1 response shape)
- `packaging/pimonitor.example.yaml` and the option's row in `docs/areas/configuration.md`
- the Configuration section of `README.md` if it is an option worth highlighting there
- the tests in `internal/config/config_test.go` that pin defaults and validation

**A new HTTP route** touches:
- its entry in `routeTable` (or `metricsSubResources`) in `internal/httpapi/routes.go`, from which `New()` registers it,
  `routeBucket()` classifies it and `newServerStats()` creates its `by_route` counter
- the `by_route` key list and example in `docs/API.md`, and the route table in
  `docs/areas/http-api.md`
- if its entry sets `api: true` (the `apiRoute` chain), it inherits `withNoStore`, `withMaxInFlight`, `withGzip` and `withAPIKey`: the
  Authentication, Compression, Caching and Rate limiting sections of `docs/API.md`, the concurrency-limit
  sentence in `SECURITY.md` and the `defaultMaxInFlight`/`inFlight` comments in `server.go` must still match
- an `httptest` test through `s.Handler().ServeHTTP` for its status, headers and auth gating

**A new or changed collector field** touches:
- `Snapshot` in `internal/collector/types.go` and the JSON shape in `docs/API.md` (a v1 shape change is a new API
  version), and its source, unit and failure behavior in `docs/areas/metrics.md`
- `internal/collector/persist.go` if the series is persisted (a layout change bumps `historyVersion`)
- `internal/alert` if a threshold applies to it
- `internal/httpapi/prometheus.go` and the `GET /metrics` section of `docs/API.md` if it is exported there
- `internal/web/assets/*.js` if the dashboard displays it
- a fixture test for its parser, including malformed and missing input

**A new shell-out** touches:
- a fixed argument list and an explicit `cmd.Env` like `updates.go` / `vcgencmd.go`
- the list of commands and files the service reads in `SECURITY.md` (*Threat Model*)
- the systemd unit if it needs a new group or path

## Test doubles

The hand-written fakes and stubs the tests reuse (no mocking library unless `docs/UNIT_TESTS.md` says
otherwise), one line each: the double, where it lives and what it stands in for; how it behaves is read in its code.

- `fakeMetrics` and `newTestServer` (`internal/httpapi/handlers_test.go`) — the `MetricsProvider` behind a real
  `Server`
- `writeTempFile`, `fakeProcess`/`writeFakeProcess`, `writeFakeVcgencmd` (`internal/collector/testhelpers_test.go`)
  — fixture files, a fake `/proc/<pid>` tree and a stub `vcgencmd` script
- `writeThermalZone`, `writeHwmonChip`/`writeHwmonChannel`, `writeCPUFreqCore`, `writeMountsFixture`,
  `writeWirelessFixture` (the matching `internal/collector/*_test.go`) — fake `/sys` and `/proc` files
- `feed` (`internal/alert/alert_test.go`) — drives the alert engine with explicit, increasing timestamps
- `httptest.NewServer` handlers in `internal/alert/notify_test.go` — the webhook endpoints

## Analyzer exclusions

- `.golangci.yml` excludes `gocognit` (the stand-in for SonarQube `go:S3776`) for `Collect` of `DiskIOCollector`,
  `NetworkCollector`, `PiholeCollector` and `ProcessCollector` and for the tests `TestParseMounts_DecodesOctalEscapes`
  and `TestParseVcgencmdTemp`, for which SonarQube reports no `go:S3776` (`.squad/stack.md`, *Analyzer gate*).
