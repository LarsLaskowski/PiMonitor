# Spec: Additive v1 changes, 405 for a wrong method, corrected 503 comment

Source: Issue #202
Status: Draft

## Problem

The issue ([#202](https://github.com/LarsLaskowski/PiMonitor/issues/202)) raises three points from the open questions
of [HTTP API](../../docs/areas/http-api.md#open-questions):

1. The versioning rule: already settled on `main` by
   [0018](../../docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md) and stated in every document
   the issue names. Only the `docs/API.md` introduction does not yet say that keys may be added and that clients
   must ignore unknown keys.
2. A request to an API route with a method other than `GET`/`HEAD` gets `404`. The catch-all `/` dashboard
   handler (`http.FileServerFS`, `internal/web/embed.go`) matches every method, so `http.ServeMux` sends a `POST
   /api/v1/metrics` to it instead of answering `405`, and the file server finds no such asset. A client that
   sends the wrong method is told the endpoint does not exist.
3. The comment on `routeTable` (`internal/httpapi/routes.go`) says the dashboard shell stays outside the
   concurrency limit "to render its 'server busy' state". The dashboard has no such state: a `503` shows the same
   "Connection error" as any other failure ([Dashboard](../../docs/areas/dashboard.md)).

## Scenarios

- S1: Given a running PiMonitor, when a client sends `POST /api/v1/metrics` (or `PUT`, `DELETE`, `PATCH`,
  `OPTIONS`, any other method but `GET`/`HEAD`), then it gets `405 Method Not Allowed`, the header
  `Allow: GET, HEAD` and the plain-text body `method not allowed`.
- S2: Given `api_key` is set, when a client sends `POST /api/v1/metrics` without a valid key, then it gets `401`
  `unauthorized`, as a `GET` would. With a valid key it gets the `405` of S1. The `405` carries
  `Cache-Control: no-store`, the `Vary` values and the security headers of every API response, and it is
  gzip-compressed when the client accepts gzip.
- S3: When a client sends `POST /healthz`, then it gets the `405` of S1, with the security headers. Like every
  `/healthz` response it needs no key and gets no caching headers.
- S4: When `prometheus_enabled` is true and a client sends `POST /metrics`, then it gets the `405` of S2.
- S5: When a client sends `GET` or `HEAD` to any route, then the answer is the same as today.
- S6: When a client sends any method to a path that is not a registered route, then it gets the same answer as
  today. That covers an unknown path, `/api/v1/metrics/` and a route its option switches off: `404` from the
  dashboard asset handler. The dashboard at `/` and its assets also answer every method as today.
- S7: A developer reading the `routeTable` comment in `routes.go` learns that the dashboard shell stays outside
  the concurrency limit so the page still loads under load. The comment no longer mentions a "server busy" state.
- S8: An integrator reading the `docs/API.md` introduction learns the versioning rule of 0018, and reads the
  `405` under *Errors*.

## Errors and edge cases

- E1: A method name in another letter case (`get`, `Post`) is not `GET`/`HEAD`, because methods are
  case-sensitive. It gets the `405` of S1 on a registered route.
- E2: More than 16 API-chain requests in progress: a wrong-method request to an API-chain route counts against the
  limit like any other, and beyond it gets `503` `server busy` instead of `405`.
- E3: A wrong-method request whose query would be rejected (`POST /api/v1/metrics/history?since=bad`) gets `405`,
  not `400`.
- E4: A `405` is counted in `GET /api/v1/serverstats` under its route's bucket in the `4xx` class. With
  `access_log_enabled` it is logged like any other request: method, path, `405`.

## Non-functional requirements

- N1: The server reads no request body to answer a `405`, and the `405` body contains no request input.
- N2: No new configuration option. No route, method or response other than the `405` changes.
- N3: `GET /api/v1/...` clients see no change. The new `405` takes the place of an undocumented `404` that the
  open question in *HTTP API* left undecided, so no documented v1 response changes.

## Acceptance criteria

- AC1: `docs/API.md` states in its introduction that a v1 response may gain keys, that clients must ignore keys
  they do not know, and that removing, renaming or retyping a key, or changing its meaning or presence, needs
  `/api/v2/...`. `AGENTS.md`, `docs/CONTRIBUTING.md`, `.squad/project.md` and `docs/areas/http-api.md` keep
  stating that same rule, recorded in decision 0018. The `## Errors` section of `docs/API.md` documents the `405`:
  the routes that answer it (every `/api/v1/...` route, `/metrics` when enabled, `/healthz`), `Allow: GET, HEAD`, the
  body `method not allowed`, that on an API-chain route the key check (`401`) and the in-flight limit (`503`) come
  first, and that unknown paths keep their `404` (S8).
- AC2: On every registered route (each `/api/v1/...` route, `/metrics` when enabled, `/healthz`), every method
  except `GET` and `HEAD` gets `405` with `Allow: GET, HEAD` and the plain-text body `method not allowed`. The tests
  cover at least `POST`, `PUT`, `DELETE`, `PATCH`, `OPTIONS` and one lower-case method. The request body is never
  read: a test sends a body whose `Read` records each call and asserts no call, and that the `405` body is exactly
  `method not allowed`, with no request input (S1, S3, S4, E1, E3, N1).
- AC3: `GET` and `HEAD` on every route answer as before. Any method on an unknown path, on a path that is not in
  the route table (`/api/v1/metrics/`) or on a route switched off by its option still gets `404`. The dashboard at
  `/` and its assets answer every method as before (S5, S6, N2, N3).
- AC4: A `405` on an API-chain route goes through the whole API chain. Without a valid key, when `api_key` is set,
  the answer is `401`. Beyond the in-flight limit it is `503`. The `405` itself carries `Cache-Control: no-store`
  and the `Vary` values, and it is gzip-compressed when the client accepts gzip. A `405` on `/healthz` gets the same
  middleware as a `/healthz` response. Every `405` carries the security headers and is counted in the server
  statistics under its route's bucket (S2, S3, E2, E4).

The tests of AC2, AC3 and AC4 build the server with a non-nil static handler that behaves like production's: it
answers every method on `/` and `404` on an unknown path, like `http.FileServerFS`. With a nil handler, Go's
`ServeMux` already answers `405` and `Allow`, and the tests would pass on today's code.
- AC5: The comment on `routeTable` in `internal/httpapi/routes.go` says the dashboard shell stays outside the
  concurrency limit so the page still loads under load. It no longer mentions a "server busy" state (S7).

## Out of scope

- No CORS: `OPTIONS` gets the `405`, not a preflight answer.
- No new dashboard behavior for `503`.
- No change to the method handling of the dashboard asset handler.
- No rewording of the versioning rule outside `docs/API.md` (done with 0018).

## Open questions

- Does a wrong-method request on an API-chain route have to pass the key check first? Answered: from the issue's
  AC4 ("passes through the same chain as other API responses"): yes, the whole chain. An unauthenticated client
  gets `401`, as for `GET`, which already tells a registered route from an unknown one.
- Is replacing the `404` with `405` a v1 shape change under 0018? Answered: no. The `404` for a wrong method was
  never documented; *HTTP API* listed it as an open question. The issue owner decided to return `405` in v1. The
  plan records this in a decision record.
- What is the body text? Answered: by the Lead. It is `method not allowed`, lower-case and fixed, like the existing error
  bodies `unauthorized`, `server busy` and `internal error`.

## Area documents

- `docs/areas/http-api.md`: *Routes* says every route answers `GET` and `HEAD`, and every other method gets `405`
  with `Allow: GET, HEAD` through the route's own middleware. *Limits and errors* gains a `405` row. The `404` row
  keeps unknown paths and switched-off routes. *Concurrency limit* already gives the right reason ("keep
  answering while the API sheds load") and stays. Both remaining open questions are removed, and the new
  decision record goes under *Related decisions*.
- `docs/API.md` (not an area document, the contract): the introduction as AC1 says, and a `405` entry in `## Errors`
  beside the `500`.
- `docs/areas/dashboard.md`: unchanged. The asset handler keeps its method handling.

## Challenge

- 1 (major): no criterion made `docs/API.md` *Errors* document the `405`. Accepted: AC1 now requires that entry,
  with its routes, `Allow`, body, the `401`/`503` order and the unchanged `404`. *Area documents* lists `docs/API.md`.
- 2 (minor): with a nil static handler the tests would pass on today's code. Accepted: a paragraph below the
  criteria requires a non-nil static handler that behaves like production's. AC2 fixes the exact body, and AC4
  already fixes `401` before `405`.
- 3 (minor): N1 was not testable. Accepted: AC2 now names the test, a request body whose `Read` records each call
  (no call allowed), plus the exact body with no request input.
