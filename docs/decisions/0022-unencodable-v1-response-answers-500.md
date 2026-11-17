# 0022: A v1 response that cannot be encoded answers 500 in v1

- **Status:** Proposed
- **Date:** 2026-11-17
- **Area:** HTTP API
- **Source:** Issue #268
- **Supersedes:** —

## Context

Every JSON endpoint but the history one encodes its value with `writeJSON` (`internal/httpapi/handlers.go`), which
only logs an encoding failure: the client gets `200` with an empty body, which is no JSON and no documented response
of [`API.md`](../API.md). `GET /api/v1/metrics/history` already answers such a failure with `500`. Issue #268, opened
by the repository owner, asks for the same `500` on the other endpoints. [0018](0018-new-key-in-v1-response-is-no-shape-change.md)
calls a change to a status code a shape change that needs `/api/v2/...`, which raises whether this fix may stay in v1.

## Options considered

1. **Answer `500` in v1** — every documented response keeps its status and body; only a failure that so far produced
   an undocumented, unparsable `200` changes, as the history endpoint already does in v1. No client can use an empty
   `200` as data: each JSON client fails on it already, the bundled dashboard included; only a status-only probe
   counted it as success.
2. **Publish the fix as `/api/v2/...` and keep v1 as it is** — the literal reading of 0018; it forks every endpoint
   for a bug fix and leaves v1 clients with a response they cannot tell from success.

## Decision

An encoding failure on any `/api/v1/...` JSON endpoint is answered with `500` and the plain-text error the history
endpoint already uses, in v1 (option 1). This states the scope of 0018's status-code rule, which 0018 now names: it
covers the status of documented responses, not a status for a failure that had no valid response. The resulting
behavior is stated in [HTTP API](../areas/http-api.md#limits-and-errors).

## Consequences

- Clients see a server error they can retry or report instead of a `200` they cannot parse; `GET /api/v1/serverstats`
  counts the request as `5xx`.
- A check that reads only the status of a JSON endpoint (an uptime probe on `GET /api/v1/metrics`) now reports the
  failure as down, where it saw up before; this is intended.
- The rule's wording in `AGENTS.md` with `.github/copilot-instructions.md`, `CONTRIBUTING.md`, `.squad/project.md`
  and [HTTP API](../areas/http-api.md#versioning-and-stability) names the documented response and links this record.
- A later change that alters the status of a documented response still needs `/api/v2/...` (0018).
