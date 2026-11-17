# 0018: Adding a key to a v1 response is no shape change

- **Status:** Proposed
- **Date:** 2026-11-17
- **Area:** HTTP API
- **Source:** Issue #183
- **Supersedes:** —

## Context

`docs/CONTRIBUTING.md` (*REST API stability*), `AGENTS.md` (*Code style*) and `.squad/project.md` (*Guarantees*) call
every change to the JSON shape of a `/api/v1/...` response breaking, to be published as `/api/v2/...`. Yet v1 has
gained keys several times (`wireless`, `pihole`, `pmic_temperature`, `system.hostname`,
[0006](0006-publish-host-name-by-default.md)), and [HTTP API](../areas/http-api.md) lists the question as open.
Issue #183 adds three keys to `GET /api/v1/metrics/history` and one field to `GET /api/v1/openhab`; its author, the
repository owner, states that adding keys is additive and needs no v2. JSON clients in the project's integrations
(openHAB's HTTP binding with JSONPATH, `curl` with `jq`, the bundled dashboard) read the keys they name and ignore
the others.

## Options considered

1. **A new key in an existing object is additive and stays in v1; removing, renaming or retyping a key, changing
   its meaning or its presence rule, or changing a status code is a shape change** — matches what v1 already did
   and what clients tolerate; a client that rejects unknown keys would break, which no known client does.
2. **Every key change needs v2** — the literal reading of today's text; every small feature would fork the API, and
   the dashboard would have to follow each fork.
3. **Decide case by case in each issue** — keeps the open question open and leaves reviewers without a rule.

## Decision

A key added to an object of an existing `/api/v1/...` response, with its own presence rule documented in
[`API.md`](../API.md), is no shape change and stays in v1; any change to an existing key — its name, type, meaning or
when it is present — or to a status code is a shape change and needs `/api/v2/...` (option 1). The rule is stated in
[HTTP API](../areas/http-api.md#versioning-and-stability).

## Consequences

- Integrations must ignore keys they do not know; `API.md` says so.
- `CONTRIBUTING.md`, `AGENTS.md` with `.github/copilot-instructions.md`, and `.squad/project.md` state this rule
  instead of "any change to the JSON shape".
- An array element that gains a key (a point, a Thing) counts as an added key too; a point's `{ "t", "v" }` itself
  is unchanged by #183.
- Revisiting this means a v2 for every addition made under it.
