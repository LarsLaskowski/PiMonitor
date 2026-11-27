# Plan: Additive v1 changes, 405 for a wrong method, corrected 503 comment

Source: Issue #202 | [spec.md](spec.md)
Tier: standard — a new response on registered routes, and no security control is created, changed or removed: the
key check, the limit and the headers apply unchanged, and no route becomes reachable that was not before.
Security areas touched: HTTP surface (`internal/httpapi` routes and middleware chain)

## Problem / root cause

See [spec.md](spec.md), *Problem*. Root cause of the `404`: `mux.Handle("/", staticHandler)`
(`internal/httpapi/server.go:160-162`) matches every method. `route.register` (`routes.go`) installs only the
pattern `GET <path>`. So a `POST <path>` goes to `/`, where `http.FileServerFS` finds no asset.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The versioning rule of 0018 is stated in AGENTS.md, CONTRIBUTING.md, .squad/project.md and http-api.md; only the API.md introduction lacks it | AGENTS.md *Code style*; CONTRIBUTING.md:181-190; project.md:69-75; http-api.md *Versioning and stability*; API.md:7-9 | confirmed |
| 2 | `http.FileServerFS` serves every method (no method check) | `$GOROOT/src/net/http/fs.go`: line 430 only skips the body for HEAD; lines 565/586 concern conditional requests | confirmed |
| 3 | Every route in `routeTable` and every sub-resource uses `http.MethodGet`; a `GET` pattern also matches `HEAD` | routes.go (table and `metricsSubResource.route`); ServeMux doc | confirmed |
| 4 | `GET /p` and `/p` do not conflict: the method pattern matches a strict subset and wins for GET/HEAD; `/p` is more specific than `/` | `$GOROOT/src/net/http/server.go:2591-2601` | confirmed |
| 5 | `http.Error` keeps `Allow`, `Cache-Control` and `Content-Encoding`, sets `text/plain; charset=utf-8` and writes the message plus `\n` | server.go:2377-2395 (Go 1.27.4, `go.mod`) | confirmed |
| 6 | `withSecurityHeaders` and `withLogging` wrap the whole mux; serverstats buckets by `r.URL.Path` | server.go:166; middleware.go:20-25 | confirmed |
| 7 | No existing test sends a method other than GET/HEAD | `grep MethodPost\|"POST"` over `*_test.go`: no hit | confirmed |
| 8 | `internal/web` imports only the standard library, so `internal/httpapi` tests can use `web.Handler()` | embed.go imports | confirmed |
| 9 | The *Concurrency limit* wording in http-api.md is already correct; only the routes.go comment says "server busy" state | http-api.md *Concurrency limit*; routes.go comment above `/healthz` | confirmed |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | check: the Reviewer reads `docs/API.md` (introduction, *Errors*) against AC1, and the four rule documents against 0018 | — |
| AC2 | `TestRouteTable_WrongMethodAnswers405` (`AC2` subtests: every table route × `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS`, `get`; plus `POST /api/v1/metrics/history?since=bad`); `TestRouteTable_WrongMethodReadsNoBody` (`AC2 N1`); `TestRouteTable_EveryRouteIsGET` (`AC2`) | `internal/httpapi/routes_test.go` |
| AC3 | `TestRouteTable_GetAndHeadStillServed` (`AC3`), `TestRouteTable_UnroutedPathsStill404` (`AC3`: `/nope`, `/api/v1/metrics/`, `/api/v1/metrics/cpu/overall`, `/api/v1/nope`, each with GET and POST, and the three switched-off routes with POST), `TestRouteTable_DashboardMethodsUnchanged` (`AC3`: GET, HEAD, POST on `/` and `/app.js` → `200`) | `internal/httpapi/routes_test.go` |
| AC4 | `TestWrongMethod_GoesThroughAPIChain` (`AC4` subtests: key set and missing → `401` `unauthorized\n`; valid key → `405` with `Cache-Control: no-store` and `Vary` naming `Authorization` and `X-Api-Key`; `Accept-Encoding: gzip` → `Content-Encoding: gzip`, decoded body `method not allowed\n`; limit filled through `s.inFlight` → `503`; the four security headers), `TestWrongMethod_Healthz` (`AC4`: no key needed, no `Cache-Control`, security headers), `TestWrongMethod_CountedInServerStats` (`AC4`: `POST /api/v1/metrics` counts in that bucket as `4xx`) | `internal/httpapi/routes_test.go` |
| AC5 | check: `grep -n "server busy" internal/httpapi/routes.go` finds no comment naming a dashboard state; the Reviewer reads the new comment | — |

Every test of AC2–AC4 builds its server with `newTestServerWithDashboard(t, cfg)`: `New(fm, cfg, h, nil)` with
`h` from `web.Handler()`, the production dashboard handler (no test double). The Tester adds it to `routes_test.go`,
with the snapshot `newTestServer` uses. The body test passes a `Body` whose `Read` counts its calls and wants `0`.
The AC3 tests pass on today's code: they guard what must not change. Per step 5 they are deferred to step 6.

## Approach

- `routes.go`: `route.register` registers, after its `r.pattern()` handler, a second handler on the bare
  `r.path`. That handler is `writeMethodNotAllowed`, wrapped in `s.apiRoute` when `r.api`, unwrapped otherwise (as
  `/healthz`). A disabled route registers neither. Because of fact 4, `GET`/`HEAD` keep their handler, and any other
  method token (lower case and `CONNECT` with a path included) gets the bare pattern.
- `writeMethodNotAllowed(w, _)` sets `Allow` to the constant `allowGetHead = "GET, HEAD"` and calls
  `http.Error(w, "method not allowed", http.StatusMethodNotAllowed)`. The body bytes are `method not allowed\n`,
  the same `http.Error` form as `unauthorized\n`. The handler does not touch `r.Body`.
- The `routeTable` comment: "The static dashboard shell is left unwrapped for the same reason: the page must still
  load while the API sheds load." The "server busy" state is no longer mentioned.
- No new user-visible output beyond the fixed status, header and body above.

Early draft PR: no

## Options considered

The options are in [0030](../../docs/decisions/0030-wrong-method-answers-405-in-v1.md):

- Restricting the dashboard to `GET /` loses: the mux's own `405` skips `apiRoute`, and AC3 forbids changing
  the dashboard.
- A method guard in front of `/` loses: it duplicates the route set and needs its own chain.
- Keeping the `404` loses to the issue's decision.
- A v2-only `405` loses: the old `404` was undocumented, as with 0022.
- A method check inside each handler loses: it repeats the check in every handler, the generated sub-resources
  included. That check is what the mux pattern already does.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/httpapi` | `routes.go`: `route.register`, new `writeMethodNotAllowed`, const `allowGetHead`, `routeTable` comment | 405 registration, comment |
| `internal/httpapi` | `routes_test.go` | new tests and helper |

## Signatures (for the Dev's skeleton)

No exported API. The private package function `writeMethodNotAllowed(w http.ResponseWriter, _ *http.Request)` and
the const `allowGetHead` are described in *Approach*. Its two parameters satisfy the analyzer's limits, and its name
follows the package's `writeJSON` style. The skeleton holds the registration in `route.register` for real (it is
one line per branch), so no test is deferred for want of it. `server.go` is final: it is unchanged.

## Test files

`internal/httpapi/routes_test.go` (the tests cover `routes.go`).

Existing test code that calls a changed signature: none (fact 7). `TestRouteTable_EveryRouteIsRegistered` and
`TestRouteTable_DisabledRoutesAreNotRegistered` send `GET` and are kept unchanged.

Files of the change no unit test can cover: none.

## Areas

- `docs/areas/http-api.md` (-40 words; 2565 now). *Routes* gets a bullet: every other method on a registered route
  gets `405` with `Allow: GET, HEAD` and the body `method not allowed`, through the route's own middleware.
  *Limits and errors* gets a `405` row (wrong method on a registered route; on API-chain routes after `401`/`503`).
  The `404` row is unchanged. The two open questions are removed, and the section says "none". *Related decisions*
  links 0030.

## Documentation updates

All are owned by the Dev and written in step 6, against the code as built:

- `docs/API.md`, introduction: v1 responses may gain keys, clients must ignore keys they do not know, and removing,
  renaming or retyping a key, or changing its meaning or presence, needs `/api/v2/...`, linking
  [HTTP API](../../docs/areas/http-api.md#versioning-and-stability).
- `docs/API.md`, *Errors*: the `405` entry as spec AC1 lists it.
- `docs/areas/http-api.md`: as *Areas*.
- `docs/ARCHITECTURE.md` around lines 299-315 (the `routeTable` paragraph): `register` also installs the bare path
  answering `405` through the route's own layer, linking 0030; "(or the mux's `404` fallback)" stays true.
- Unchanged, after a sweep for `404`, `method` and `GET and HEAD`: `AGENTS.md`, `.github/copilot-instructions.md`,
  `docs/CONTRIBUTING.md`, `.squad/project.md` (fact 1; spec *Out of scope*), `README.md`, `SECURITY.md`, and
  `docs/areas/dashboard.md`, whose `404` wording is about assets.
- PR template: *REST API* box `docs/API.md` (above). No changelog file exists; the release notes come from the
  `enhancement` label.

## Architecture check

The guarantees of `.squad/project.md` are kept:

- v1 shapes stay stable: no documented response changes (0030).
- The API key still gates every API-chain route, the wrong-method path included.
- `/healthz` and the dashboard stay outside the limit.
- The security headers are on every response.

`docs/ARCHITECTURE.md` gets the one paragraph above.

## Security considerations

- [x] No limits: no input is parsed. The handler never reads `r.Body`, and the body is fixed.
- [x] No exception reaches a user: the body is the constant `method not allowed`.
- On an API-chain route, a wrong method passes `withAPIKey` before the `405`. Without a key it gets `401`, so the
  `405` tells an unauthenticated client nothing that `GET`'s `401` versus `404` does not already tell.
- `/healthz` answers `405` unauthenticated, like its `GET`. There is no CORS: `OPTIONS` is no preflight.

## Decision records

- `docs/decisions/0030-wrong-method-answers-405-in-v1.md` (Proposed, indexed)

## Out of scope / follow-ups

The spec's *Out of scope*. No follow-up issue.

## Challenge notes

The plan holds up. The root cause and all nine verified facts match the code. I found no major objection and two minor ones that the Tester can follow without a plan change.

**What I checked against the code**
- **Root cause:** confirmed. `route.register` installs only `r.pattern()`, which is `GET <path>` (`internal/httpapi/routes.go`, `pattern`/`register`). `mux.Handle("/", staticHandler)` is at `internal/httpapi/server.go:160-162`. `web.Handler()` wraps `http.FileServerFS` and has no method check (`internal/web/embed.go`).
- **Fact 5:** confirmed on Go 1.27.4. `http.Error` deletes only `Content-Length` and keeps `Content-Encoding`, citing go.dev/issue/66343 (`$GOROOT/src/net/http/server.go:2377-2395`). So the gzip assertion in AC4 is sound. It is the same path the existing `unauthorized` 401 takes inside `withGzip`.
- **Middleware order:** `apiRoute` is `withNoStore(withMaxInFlight(withGzip(withAPIKey(h))))` (`server.go`, `apiRoute`). A full in-flight limit therefore gives 503 before the key check and before the 405. The 405 row ("after 401/503") and the AC4 subtests match this.
- **Fact 7:** confirmed. No `*_test.go` sends a method other than GET/HEAD, and no Go file mentions 405 or `MethodNotAllowed`. No existing test pins the old 404.
- **Fact 8:** confirmed. `cmd/pimonitor/main.go:55,60` wires `web.Handler()` the way the planned `newTestServerWithDashboard` would. `internal/web` imports only the standard library.
- **Need, alternatives and tier:** all hold. The 405 is the issue owner's decision. Option 1 is the only option that keeps `apiRoute` without duplicating the route set. `standard` fits: no security control changes, and no route becomes reachable without the key.

**Objections**
1. (minor) **The server statistics assertion asks for a field that does not exist.** It is the plan's *Tests per criterion* row for AC4, `TestWrongMethod_CountedInServerStats`, which says "counts in that bucket as `4xx`". `serverStats` has no status class per route: `record` increments the global `status4xx` and the route counter separately (`internal/httpapi/serverstats.go`, `record`, and `serverStatsSnapshot`: `by_status_class` is global, `by_route` is a plain count).
   *Instruction for the Tester:* assert that `by_route["/api/v1/metrics"]` and `by_status_class["4xx"]` each grow after one `POST /api/v1/metrics`. Do not look for a status class inside the route's bucket.
2. (minor) **The AC2 and AC4 tests must turn on the three optional routes.** AC2 says "every table route", but the `/metrics`, `/api/v1/processes` and `/api/v1/openhab` entries are only registered when `PrometheusEnabled`, `ProcessesEnabled` and `OpenHABEnabled` are set (`routes.go`, the `enabled` predicates). With any of them off, that route answers 404 and the loop would have to skip it. `/metrics` is explicitly in scope (issue AC2, spec S4).
   *Instruction for the Tester:* build the AC2 server (and the AC4 `/metrics` case) with all three options set to true. Keep the three switched-off routes with POST → 404 in AC3, which is a separate config.
