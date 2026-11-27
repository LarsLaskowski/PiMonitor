# 0030: A wrong method on a registered route answers 405 in v1

- **Status:** Proposed
- **Date:** 2026-11-27
- **Area:** HTTP API
- **Source:** Issue #202
- **Supersedes:** —

## Context

Every route PiMonitor registers answers only `GET` and `HEAD`. A request with another method used to reach the
catch-all dashboard handler (`http.FileServerFS`, which serves every method), so `POST /api/v1/metrics` got `404`. That
told the client the endpoint did not exist. [HTTP API](../areas/http-api.md) listed this as an open question; the `404`
was never documented. Issue #202, opened by the repository owner, asks for `405` with `Allow: GET, HEAD`, through the
same middleware as other API responses. [0018](0018-new-key-in-v1-response-is-no-shape-change.md) calls a change to
the status of a documented response a shape change that needs `/api/v2/...`.

## Options considered

1. **Each registered route also takes its path for every other method and answers `405` through its own middleware**
   — the key check, in-flight limit, gzip and no-store apply as on `GET`. The dashboard keeps its method handling, and
   unknown paths keep their `404`.
2. **Restrict the dashboard handler to `GET`/`HEAD`, so that `http.ServeMux` answers `405` itself** — no new handler.
   But the mux's `405` bypasses `apiRoute` (no no-store, no key check), its body is `Method Not Allowed`, and the
   dashboard's method handling changes.
3. **A method check in front of the dashboard handler for the paths in `routeTable`** — keeps one handler. But it
   needs its own copy of each route's middleware, and the route set gets a second consumer.
4. **Keep the `404` and document it** — no code change. But a client keeps being told that an existing endpoint does
   not exist.
5. **Publish `405` only under `/api/v2/...`** — the literal reading of 0018. It forks every endpoint for a response
   that v1 never documented.

## Decision

A request with a method other than `GET` or `HEAD` to a registered route answers `405` with `Allow: GET, HEAD`, in v1,
through that route's own middleware (option 1). As with [0022](0022-unencodable-v1-response-answers-500.md), 0018's
status-code rule covers documented responses, and the `404` for a wrong method was none. The behavior is stated in
[HTTP API](../areas/http-api.md#limits-and-errors).

## Consequences

- A client that sends the wrong method learns that the endpoint exists and which methods it takes. Without a valid key,
  the client still gets `401` first, so nothing new is revealed.
- A route whose option is off stays unregistered and keeps its `404` for every method.
- A future route with a method other than `GET` would need its own `Allow` value. A test pins every route to `GET`.
- No CORS: `OPTIONS` gets the `405`.
