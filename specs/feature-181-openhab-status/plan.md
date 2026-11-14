# Plan: Show the local openHAB status in the dashboard

Source: Issue #181 | [spec.md](spec.md)
Tier: security — the change adds a guard on where outbound requests may go (loopback-only `base_url`, checked again
at connect time), stores and sends a new credential (`api_token`, `PIMONITOR_OPENHAB_TOKEN`) and adds an API route.
Security areas touched: Outbound calls (new openHAB HTTP client), API key and secret handling (new `api_token` and
its environment variable), HTTP surface (new route `GET /api/v1/openhab`), Dashboard XSS (openHAB text rendered in
`app.js`), Parsing of local input (`/proc/<pid>/cmdline`, `status`, `stat`, `/proc/uptime`, openHAB's JSON, the
YAML `openhab` block), Logging (new log lines that must never carry the token)

## Problem / root cause

Feature: see [spec.md](spec.md).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | openHAB 5.2.4 root `/rest/` answers `200` without a token, version in `runtimeInfo.version` | PM sample (`openhab-samples.md`, scratchpad) | confirmed |
| 2 | Without a token `/rest/systeminfo`, `/rest/things?summary=true`, `/rest/addons` answer `401` with `{"error":{"message":"Authentication required","http-code":401}}` | PM sample | confirmed |
| 3 | Anonymous `/rest/items?fields=name` answers `200` with an array of empty objects whose length is the Item count | PM sample (about 440 `{}`) | confirmed |
| 4 | System info is nested under `systemInfo`; `freeMemory`, `totalMemory` in bytes; `startLevel`; no maximum heap field | PM sample | confirmed |
| 5 | Unit of `systemInfo.uptime` (ms or s) | to verify: sample value 362335 fits both; Product Manager runs `ps -o etimes= -p <pid>` with a `/rest/systeminfo` call at the same moment, before step 5 of G2 (tasks row 1); unanswered by then: the fallback stays unimplemented and the Lead amends AC4 | unverified |
| 6 | Things carry `UID`, `label`, `statusInfo.status`, `statusInfo.statusDetail`, optional `statusInfo.description`; disabled = `UNINITIALIZED`/`DISABLED`; OFFLINE with detail `NONE` and the reason in `description` | PM sample | confirmed |
| 7 | `/rest/addons` is about 270 KB, chunked, 492 entries, 29 with `"installed":true` | `79cd39f0-addons.txt` (uploads), counted with `grep -o` | confirmed |
| 8 | The openHAB JVM's argv[0] is `/usr/bin/java`, an argument starts with `-Dopenhab.home=`; PPid 1, no `karaf` parent under systemd | PM sample `/proc/938/cmdline`, `status` | confirmed |
| 9 | Field names of openHAB 4.x match 5.2.4 | to verify: no 4.x sample exists; a 4.x user's report after merge; residual risk recorded in [0015](../../docs/decisions/0015-openhab-values-from-real-5x-responses.md) | unverified |
| 10 | `/proc/<pid>/stat` field 22 (start time) and `VmRSS` are already parsed | `internal/collector/process.go:38-82` (`parseProcPidStat`, `parseProcPidStatusRSS`) | confirmed |
| 11 | `netip.Addr.IsLoopback` unmaps IPv4-in-IPv6 and accepts all of 127.0.0.0/8 and `::1` | `$GOROOT/src/net/netip/netip.go:538-552` | confirmed |
| 12 | `CheckRedirect` returning `http.ErrUseLastResponse` stops at the 3xx response | `$GOROOT/src/net/http/client.go:73,496` | confirmed |
| 13 | A route that is not registered still has its `by_route` bucket | `internal/httpapi/routes.go:30-48` | confirmed |
| 14 | The unit allows reading other users' `/proc/<pid>/{cmdline,status,stat}` and loopback TCP (no `ProtectProc`, `AF_INET` allowed) | `packaging/pimonitor.service` | confirmed |
| 15 | Fast and slow ticks share one loop; a slow tick delays the fast tick | `internal/collector/collector.go:332-375`, `docs/areas/metrics.md` *Collection cadence* | confirmed |
| 16 | `v1.2.0` tags current `main`, so records 0001–0012 are released | `git ls-remote --tags origin` | confirmed |
| 17 | `net/http` decompresses a gzip response transparently, so a reader on `resp.Body` counts decoded bytes | `$GOROOT/src/net/http/transport.go` (`DisableCompression`, `gzipReader`) | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestOpenHABCollector_Poll_NotInstalled_AC1` | `internal/collector/openhab_test.go` |
| AC2 | `TestOpenHABCollector_Status_PendingBeforeFirstPoll_AC2`, `TestOpenHABCollector_Poll_States_AC2`, `TestOpenHABCollector_Poll_ReevaluatesDetection_AC2` | `internal/collector/openhab_test.go` |
| AC3 | `TestOpenHABCollector_Poll_Process_AC3` | `internal/collector/openhab_test.go` |
| AC4 | `TestOpenHABCollector_Poll_RESTValues_AC4`, `TestOpenHABCollector_Poll_Refused_AC4`, `TestOpenHABCollector_Poll_Uptime_AC4`, `TestOpenHABCollector_RealSamples_AC4` | `internal/collector/openhab_test.go` |
| AC5 | `TestOpenHABCollector_Poll_Things_AC5` | `internal/collector/openhab_test.go` |
| AC6 | `TestOpenHABCollector_Poll_Requests_AC6`, `TestOpenHABCollector_Poll_NonLoopbackDialRefused_AC6` | `internal/collector/openhab_test.go` |
| AC7 | `TestDefault_OpenHAB_AC7`, `TestValidate_OpenHAB_AC7`, `TestLoad_OpenHABPollIntervalFraction_AC7` | `internal/config/config_test.go` |
| AC8 | `TestLoad_OpenHABTokenEnv_AC8`, `TestOpenHABCollector_TokenNeverLogged_AC8`, `TestHandleOpenHAB_TokenNotServed_AC8`, `TestClientConfig_NoOpenHABToken_AC8` | `internal/config/config_test.go`, `internal/collector/openhab_test.go`, `internal/httpapi/handlers_test.go`, `cmd/pimonitor/main_test.go` |
| AC9 | `TestHandleOpenHAB_AC9`, `TestCollector_OpenHABDisabled_AC9`, `TestServerConfig_OpenHABEnabled_AC9` | `internal/httpapi/handlers_test.go`, `internal/collector/collector_test.go`, `cmd/pimonitor/main_test.go` |
| AC10 | `TestOpenHABCollector_Logging_AC10` | `internal/collector/openhab_test.go` |
| AC11 | `TestCollector_Run_OpenHABPollsAtStartup_AC11`, `TestCollector_Run_HungOpenHABDoesNotDelayFastTick_AC11`, `TestCollectorConfig_OpenHAB_AC11` | `internal/collector/collector_test.go`, `cmd/pimonitor/main_test.go` |
| AC12 | `TestAppJS_OpenHABPolling_AC12`, `TestAppJS_OpenHABCardVisibility_AC12` | `internal/web/openhab_test.go` |
| AC13 | manual: Product Manager, on a local `make build-arm64` of the branch head on their openHAB Pi, before step 9: card with and without `api_token`, with a wrong token, with openHAB stopped, and a Thing label containing `<b>x</b>` shown literally | — |

Cases the tests must hold (Tester):

- AC1: empty temp root, `enabled` auto; an `httptest` server counting requests stays at 0; a log buffer stays empty;
  the JSON has only `state`, `poll_interval_seconds`, `api_token_configured`.
- AC2: table over install path present/absent × process present/absent × root ok/failing × auto/true, plus no
  path, no process, root ok with `true`; a second poll after removing the path and the process gives `not_installed`.
- AC3: a fake `/proc` with the anonymized openHAB `java` cmdline, a `bash …/karaf` script, a `java` without
  `-Dopenhab.home=`, `vim /etc/openhab/x.items`, and two openHAB `java` PIDs (lowest wins); `memTotal` 0 omits
  the percent; all REST calls failing still reports the process.
- AC4: anonymized excerpts of samples 1–4 and 7 as inline constants; named and nameless Item arrays; per source:
  malformed JSON, wrong shape (object for array), missing fields, `free > total`; `401`/`403` listed under `refused`;
  `collected_at` from the injected clock. `TestOpenHABCollector_RealSamples_AC4` reads the PM's raw files from
  `PIMONITOR_OPENHAB_SAMPLE_DIR` (skipped when unset) and reports counts only. Uptime: from `stat`+`/proc/uptime`
  with a process; from `systemInfo.uptime` only without one (row 5).
- AC5: 12 OFFLINE Things in shuffled UID order, one disabled, one `UNKNOWN`, ONLINE ones: 10 listed in UID order,
  disabled not listed, `by_status` complete, `description` present only when given.
- AC6: a `302` to a second counting server (0 hits, value absent); a handler blocking until the request context ends
  with `Timeout` 50 ms (value absent, later calls still made); bodies of exactly 8 MiB (accepted) and 8 MiB + 1
  (absent), also gzip-encoded; `Authorization` present exactly with a token; `BaseURL` `http://192.0.2.1:9` fails
  without a connection attempt reaching the network (dial check).
- AC7: the forms under *Approach*, *URL guard*, each accepted or rejected with the exact error text; `enabled`
  `auto`, `AUTO`, `True`, `"false"`, `FALSE` accepted, `yes`, `no`, `on`, `off`, `1`, `0`, `""` rejected; interval
  59, 60, 86400, 86401; `60.5` in a file.
- AC8: token from the environment wins when non-empty, an empty variable keeps the file's, `unsetEnv` called; a
  sentinel token never appears in the log buffer over failing, refused (with token), timed-out and redirected
  polls, nor in `GET /api/v1/openhab`, nor in `clientConfig` JSON.
- AC9: `200` with the fake's status JSON; `401` without the key; `Cache-Control: no-store`; `404` when
  `OpenHABEnabled` is false; `by_route` holds `/api/v1/openhab` in both cases; `enabled: false` makes no request
  and reads no install path.
- AC10: first failure warning, repeat debug, recovery info; `401` without token info, with token warning;
  `not_installed` logs nothing.
- AC11: a blocking openHAB handler signals entry; the snapshot's timestamp becomes non-zero while it is still
  blocked; `Status()` leaves `pending` after the first poll; `collectorConfig` maps the interval.
- AC12: static checks of `app.js`: `pollOpenHAB` with an in-flight guard reset in `finally`, its timer cleared in
  `stopPolling`, called in the visibility handler and in `reloadAll`, card id `openhab` in `CARD_DEFS`, the card
  hidden for `not_installed`, `pending` and `404`.

## Approach

**Configuration** (`internal/config`): a nested `openhab` block (`OpenHAB` struct). `enabled` is a string compared
case-insensitively against `auto`/`true`/`false`; `Mode()` returns it lower-cased. `poll_interval_seconds` is an
`int` (60–86400) added to `integerOptionNames` and the shadow struct of `checkIntegerOptions`
([0008](../../docs/decisions/0008-reject-non-finite-and-fractional-config-numbers.md)). `api_token` must hold no
whitespace or control character. `OpenHABTokenEnvVar` is read in `load` exactly like `APIKeyEnvVar`
(non-empty overrides the file, unset after reading); no flag. `Validate` calls a private `validate` on the block.
Error texts (exact, the URL and token never echoed):

- `openhab.enabled must be auto, true or false (got %q)`
- `openhab.base_url must be an http URL with a loopback host (127.0.0.0/8, ::1 or localhost) and no user, path, query or fragment`
- `openhab.poll_interval_seconds must be between 60 and 86400 (got %v)`
- `openhab.api_token must not contain whitespace or control characters`

**URL guard** ([0013](../../docs/decisions/0013-openhab-requests-only-to-loopback-http.md)), forms `url.Parse`
accepts and the guard's answer: scheme after `url.Parse` lower-casing must be `http` (`https`, `ftp`, none,
opaque `http:127.0.0.1` rejected); `User` must be nil (`http://u:p@127.0.0.1`, `http://127.0.0.1:80@evil` rejected);
`Path` empty or `/`, `RawQuery`, `Fragment` empty and `ForceQuery` false; port empty or 1–65535; host either
`localhost` (case-insensitive, no trailing dot) or a `netip.ParseAddr` literal without zone that `IsLoopback`
(accepts `127.x.y.z`, `::1`, `::ffff:127.0.0.1`; rejects `127.1`, `2130706433`, `0x7f.0.0.1`, `0127.0.0.1`,
`127.0.0.1.nip.io`, `::1%lo`, `192.168.1.2`, empty host). A backslash or space in the authority makes `url.Parse`
fail, which is rejected with the same text. At connect time a `net.Dialer.Control` rejects any address whose IP is
not loopback (covers `localhost` mapped elsewhere); the transport has `Proxy: nil`; `CheckRedirect` returns
`http.ErrUseLastResponse`, so a 3xx is a failed call.

**Collector** (`internal/collector/openhab.go`, [0014](../../docs/decisions/0014-openhab-poll-runs-on-its-own-loop.md),
[0015](../../docs/decisions/0015-openhab-values-from-real-5x-responses.md)): `OpenHABCollector` built by
`Collector.New` when `cfg.OpenHAB.Enabled`; `Collector.Run` starts its `Run` in a goroutine before the first fast
tick and waits for it before returning. One `Poll`:

1. Detection: install paths (`Root` + `/usr/share/openhab`, `/etc/openhab`, `/var/lib/openhab`, `os.Stat`
   succeeds); process: every numeric `Root/proc/<pid>`, `cmdline` split at NUL, base name of argv[0] is `java` and
   an argument starts with `-Dopenhab.home=`; lowest PID. With `auto`, neither found → `not_installed`, nothing else
   runs, nothing is logged.
2. Process values: `VmRSS` and start time via the existing parsers; uptime = `/proc/uptime` − start ticks / 100,
   floored, never negative; percent = 100 × RSS / `memTotal()` when that is above 0.
3. REST, in order root, `systeminfo`, `things?summary=true`, `items?fields=name`, `addons`, each with its own
   `Timeout` (5 s) context, body read through `io.LimitReader` of 8 MiB + 1 byte (decoded bytes) and failed when
   longer. Root succeeds only on `200` with a JSON object whose `runtimeInfo.version` is non-empty; when it fails the
   other REST calls are skipped (their values absent, not refused, not logged). A `401`/`403` adds the source name
   (`root`, `systeminfo`, `things`, `items`, `addons`) to `refused`. Items are counted by decoding into
   `[]struct{}`; add-ons count `installed == true`; heap used = `totalMemory − freeMemory`, JVM absent when free
   exceeds total.
4. State per spec N2; `collected_at` from `Now`; the result replaces the status under the collector's own mutex.

Logging: one `failureLog` per source (`openhab process`, `openhab root`, `openhab systeminfo`, `openhab things`,
`openhab items`, `openhab addons`); a refused call while no token is configured goes through a new private
`failureLog.observeAt(log, msg, err, level)` with `slog.LevelInfo`
([0016](../../docs/decisions/0016-refused-openhab-call-without-token-logs-info.md)); `observe` becomes
`observeAt(…, slog.LevelWarn)`. Error values carry the HTTP status or the `net/http` error, never a response body
or header. The process source fails with `no openHAB process found` when installed but not found.

**HTTP**: `routeTable` entry `GET /api/v1/openhab`, `api: true`, `enabled: cfg.OpenHABEnabled`; the private
handler `handleOpenHAB` writes `s.metrics.OpenHAB()` with `writeJSON`.

**Response shape** (`docs/API.md`): `state`, `poll_interval_seconds`, `api_token_configured` always; then, each
omitted when absent: `collected_at`, `version`, `start_level`, `uptime_seconds`, `process` {`pid`, `rss_bytes`,
`rss_percent_of_ram`}, `jvm` {`heap_used_bytes`, `heap_total_bytes`}, `things` {`total`, `by_status`, `not_online`
[{`uid`, `label`, `status`, `status_detail`, `description`}]} with `not_online` always an array, `items_count`,
`addons_installed`, `refused` (sorted in the call order above, omitted when empty).

**Dashboard** (`app.js`, `index.html`, `style.css`): card `card-openhab`, `CARD_DEFS` id `openhab`, label `openHAB`,
last in the default order, hidden (`hidden` attribute) unless the latest answer was `200` with a state other than
`not_installed`/`pending`. `pollOpenHAB` with `openhabInFlight`; timer `openhabTimer` at `max(60,
poll_interval_seconds)` s, restarted when the reported interval changes, cleared in `stopPolling`; `404` hides the
card and clears the timer until reload; `401` opens the API key dialog like the other polls; other failures leave
the card as it is. Layout, top to bottom: state line (`Running · <version>`, `Starting (start level N)`, `Not
running`, `Not reachable`); rows `Process memory` (`<bytes> (<x.y> % of RAM)`), `JVM heap` (`used / total` with a
bar), `Uptime`, `Items`, `Add-ons`; `Things` summary `<n> ONLINE · <n> OFFLINE · …` (ONLINE first, the rest by
name); then the not-ONLINE list, one entry per Thing: label, UID muted, `STATUS – description` (or status detail
when no description). An absent value reads `n/a`; a refused source's values read `needs api_token` (no token)
or `api_token refused`. Warning state: the card gets the existing `warn` color class. All openHAB text through
`textContent`.

**Seams**: `OpenHABConfig.Root` (file system root for install paths and `/proc`), `Timeout`, `Now`;
the bound of one request at a time per poll is observed through the `httptest` handler's concurrent-request
counter (AC6 test asserts it never exceeds 1).

## Options considered

1. **Poll in the slow tick or as a third ticker on the collector loop** — a hung openHAB would delay the fast tick
   by up to 25 s ([0014](../../docs/decisions/0014-openhab-poll-runs-on-its-own-loop.md)).
2. **Any `base_url`; `https` with or without verification** — rejected in
   [0013](../../docs/decisions/0013-openhab-requests-only-to-loopback-http.md).
3. **`enabled: false` answering `{"state":"not_installed"}`** — the existing rule is that a disabled route is not
   registered ([HTTP API](../../docs/areas/http-api.md#routes)).
4. **The openHAB interval in `GET /api/v1/config`** — changes an existing v1 shape, an open question of the HTTP
   API area; carried in the new response instead.
5. **`503` before the first poll** — rejected for `pending` (spec *Challenge* 1, 0014).
6. **Browser fetches openHAB directly** — rejected in the issue (CSP, token in the browser).
7. **Streaming JSON decode to bound memory below the 8 MiB body cap** — more code for a cap that already bounds a
   poll; one poll at a time, so the peak is one body.
8. **Take uptime from `systemInfo.uptime`** — unit unverified (0015).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/config` | `config.go` | `OpenHAB` block, defaults, validation, env var |
| `internal/collector` | `openhab.go` (new) | `OpenHABCollector`, status types, detection, HTTP client, parsers |
| `internal/collector` | `collector.go` | `Config.OpenHAB`, construction, `Run` start/join, `OpenHAB()` |
| `internal/collector` | `failurelog.go` | private `observeAt` |
| `internal/httpapi` | `server.go`, `routes.go`, `handlers.go` | provider method, `Config.OpenHABEnabled`, route, handler |
| `cmd/pimonitor` | `main.go` | `collectorConfig`, `serverConfig` mapping |
| `internal/web/assets` | `app.js`, `index.html`, `style.css` | card, poll, layout entry |

## Signatures (for the Dev's skeleton)

`internal/config/config.go`:

- `type OpenHAB struct { Enabled string; BaseURL string; APIToken string; PollIntervalSeconds int }`, YAML tags
  enabled, base_url, api_token, poll_interval_seconds
- field `OpenHAB OpenHAB` on `Config`, YAML tag openhab; Default sets auto, http://127.0.0.1:8080, empty, 60
- `const OpenHABTokenEnvVar = "PIMONITOR_OPENHAB_TOKEN"`
- `func (o OpenHAB) Mode() string`
- `func (o OpenHAB) PollInterval() time.Duration`

`internal/collector/openhab.go`:

- `const OpenHABStatePending = "pending"`, `OpenHABStateNotInstalled = "not_installed"`, `OpenHABStateNotRunning = "installed_not_running"`, `OpenHABStateUnreachable = "running_unreachable"`, `OpenHABStateOK = "ok"`
- `type OpenHABConfig struct { Enabled bool; Always bool; BaseURL string; Token string; Interval time.Duration; Root string; Timeout time.Duration }` plus the
  field Now, a func() time.Time (nil means time.Now)
- `type OpenHABStatus struct { State string; PollIntervalSeconds int; APITokenConfigured bool; CollectedAt *time.Time; Version string; StartLevel *int; UptimeSeconds *uint64; Process *OpenHABProcess; JVM *OpenHABJVM; Things *OpenHABThings; ItemsCount *int; AddonsInstalled *int; Refused []string }` (JSON tags as in Response shape)
- `type OpenHABProcess struct { PID int; RSSBytes uint64; RSSPercentOfRAM *float64 }`
- `type OpenHABJVM struct { HeapUsedBytes uint64; HeapTotalBytes uint64 }`
- `type OpenHABThings struct { Total int; ByStatus map[string]int; NotOnline []OpenHABThing }`
- `type OpenHABThing struct { UID string; Label string; Status string; StatusDetail string; Description string }`
- `func NewOpenHABCollector(cfg OpenHABConfig, memTotal func() uint64, log *slog.Logger) *OpenHABCollector`
- `func (c *OpenHABCollector) Run(ctx context.Context)`
- `func (c *OpenHABCollector) Poll(ctx context.Context)`
- `func (c *OpenHABCollector) Status() OpenHABStatus`

`internal/collector/collector.go`:

- field `OpenHAB OpenHABConfig` on `Config`
- `func (c *Collector) OpenHAB() OpenHABStatus`

`internal/httpapi`:

- `OpenHAB() collector.OpenHABStatus` added to `MetricsProvider` (`server.go`)
- field `OpenHABEnabled bool` on `Config` (`server.go`)

Names checked against .squad/stack.md (Writing code, Logger arguments): none contains a name of the CodeQL list
(APIToken, Token and APITokenConfigured contain no apikey, secret or password); log calls pass no token value.
Files the skeleton rewrites: config.go, collector.go, failurelog.go, server.go, routes.go, handlers.go, main.go
(read; the edits are additive).

## Test files

`internal/collector/openhab_test.go` (new), `internal/collector/collector_test.go`, `internal/config/config_test.go`,
`internal/httpapi/handlers_test.go`, `cmd/pimonitor/main_test.go`, `internal/web/openhab_test.go` (new; web tests
are per dashboard topic, as `pihole_test.go`).

Existing call sites: `fakeMetrics` (`internal/httpapi/handlers_test.go:19-40`) gains `OpenHAB()` — Dev, step 4,
mechanically (the interface change breaks the build). `TestRouteTable_EveryRouteIsRegistered`
(`routes_test.go:52`) sets `OpenHABEnabled: true` and `cardDomIDs` (`internal/web/layout_test.go:51`) gains
`card-openhab` — Tester, step 5. `TestServerConfig_MapsFields` and `TestCollectorConfig_MapsFields`
(`main_test.go`) gain the new fields — Tester, step 5. No other test has input a criterion handles differently.

Files no unit test can cover: `internal/web/assets/app.js`, `index.html`, `style.css` — browser assets without a
test runner (*Layout* in `.squad/stack.md`); static checks in `internal/web/openhab_test.go` and the manual test of
AC13 instead; no coverage exclusion follows, as `COVERAGE_PATHSPECS` holds only `*.go`.

## Areas

New area **openHAB** (`docs/areas/openhab.md`): scope "detection of a local openHAB, its states, the values and
their sources, the poll cadence, request limits and the loopback guard, logging, the token"; not here: response
shape (`API.md`), the card (Dashboard), option ranges (Configuration). Configuration, HTTP API, Dashboard and
Deployment change as listed below.

## Documentation updates

All by the Dev, against the built code, in the last group; the area document and its index row first, because
`decision-check.py` fails on records 0013–0016 (area `openHAB`, links to `openhab.md`) until they exist.

- `docs/areas/openhab.md` (new) and its row in `docs/areas/README.md`; *Related decisions* 0013–0016.
- `docs/areas/configuration.md`: precedence gains `PIMONITOR_OPENHAB_TOKEN`; option rows for the `openhab` block.
- `docs/areas/http-api.md`: route table row (registered when `openhab.enabled` is not `false`, API chain yes).
- `docs/areas/dashboard.md`: "reads four endpoints … uses no other" becomes five; the openHAB poll, pause and `404`
  rule; *Rendering* openHAB text; card list and layout ids gain `openhab`.
- `docs/areas/deployment.md`: `PIMONITOR_OPENHAB_TOKEN` in the optional `EnvironmentFile`.
- `docs/areas/metrics.md` *Collection cadence*: one sentence that the openHAB poll runs outside the two ticks, link
  to openHAB.
- `docs/API.md`: section `GET /api/v1/openhab` with every field; `by_route` key list and example of
  `GET /api/v1/serverstats`.
- `docs/ARCHITECTURE.md`: diagram (openHAB REST → Collector), collector list and the "two independent tickers"
  paragraph (a third loop, link 0014).
- `SECURITY.md` threat model: loopback-only outbound HTTP, the token (file mode, env var, never logged or served),
  the openHAB process files read, `hidepid` degrades gracefully.
- `README.md` *Configuration*: `PIMONITOR_OPENHAB_TOKEN`; feature list gains the openHAB card.
- `docs/integrations/openhab.md`: the reverse direction; which calls need an API token (rows 2–3).
- `packaging/pimonitor.example.yaml`: the `openhab` block with comments; `packaging/pimonitor.service`: hardening
  comment names the openHAB reads and loopback HTTP, and the commented `EnvironmentFile` line mentions the token.
- `AGENTS.md` and `.github/copilot-instructions.md` project blocks *overview* and *architecture*: openHAB status and
  parser (identical text in both).
- `.squad/project.md` — the Dev reports the exact text, the orchestrator writes it: *Security areas* "Outbound
  calls" gains the openHAB client (`internal/collector/openhab.go`, loopback guard); a line for the openHAB token;
  *Test doubles* gains the openHAB `httptest` server and fake `/proc` helpers of `openhab_test.go`.

## Architecture check

Guarantees of `.squad/project.md`: the service stays unprivileged (no unit directive changes); v1 shapes keep
(new route; `by_route` gains a key, as every new route did); a missing source yields absent values and a logged
line, never a fabricated reading; no new dependency; webhook, history and installer guarantees untouched. The
ARCHITECTURE statement that the collector avoids network access for Pi-hole stays true; openHAB is a deliberate,
loopback-only exception recorded in 0013.

## Security considerations

- [x] Limits: 8 MiB per openHAB response, after gzip decoding (row 17); `cmdline` and `status` read whole (kernel
  files, bounded by the kernel); the YAML file as today.
- [x] User-visible failure reasons: none — the API reports only state names and `refused` source names; startup
  errors are the four fixed texts above; log lines carry `net/http` errors (URL without userinfo, which the guard
  rejects) and HTTP status codes, never bodies or headers.

## Decision records

- `docs/decisions/0013-openhab-requests-only-to-loopback-http.md` (Proposed)
- `docs/decisions/0014-openhab-poll-runs-on-its-own-loop.md` (Proposed)
- `docs/decisions/0015-openhab-values-from-real-5x-responses.md` (Proposed)
- `docs/decisions/0016-refused-openhab-call-without-token-logs-info.md` (Proposed)

## Out of scope / follow-ups

#182 (Prometheus), #183 (sparkline), #184 (alerts); spec *Out of scope*.
