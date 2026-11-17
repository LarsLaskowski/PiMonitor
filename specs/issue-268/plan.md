# Plan: writeJSON answers 200 with an empty body when encoding fails

Source: Issue #268
Tier: standard — the error branch of a shared response helper changes behavior (status, headers, body) and control flow, without creating, changing or removing a security control.
Security areas touched: HTTP surface (`internal/httpapi`: the response helper every JSON route but history uses)

## Problem / root cause

`writeJSON` (`internal/httpapi/handlers.go:13-18`) sets `Content-Type: application/json` and calls
`json.NewEncoder(w).Encode(v)`; on an error it only logs. `Encode` marshals into its own buffer and writes nothing when
marshalling fails, so the handler returns without writing a status and net/http answers `200` with an empty body.
Affected: the call sites at `handlers.go:42` (metrics), `:128` (alerts), `:135` (config), `:158` (processes), `:165`
(openHAB), `:175` (serverstats) and `routes.go:138` (the per-metric sub-resources).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Issue: on an encoding failure the client gets `200` with an empty body | `handlers.go:15-17` logs only; `encoding/json/stream.go:216-245` (and `v2_stream.go:136-156` under `goexperiment.jsonv2`) returns before `enc.w.Write` when marshalling fails, so nothing is written and the implicit status is `200` | confirmed |
| 2 | Issue: NaN is such a failure | `json.Marshal` rejects NaN/Inf with `UnsupportedValueError`; `TestHandleHistory_MarshalFailureReturns500` (`handlers_test.go:264`) relies on it | confirmed |
| 3 | `docs/areas/http-api.md:202-203`: the other endpoints "can leave a `200` response with a truncated body" | Fact 1: the body is empty, never truncated (the write is one call after a full marshal); a related doc defect, fixed under *Documentation updates* | refuted |
| 4 | The history endpoint already answers an encode failure with `500` `internal error` in v1 | `handlers.go:68-72`; `docs/areas/http-api.md:198` | confirmed |
| 5 | `http.Error` sets `Content-Type: text/plain; charset=utf-8`, `X-Content-Type-Options: nosniff`, keeps `Content-Encoding`, and writes the text plus `\n` | `net/http/server.go:2377-2395` (go1.27.4) | confirmed |
| 6 | Under gzip the error body is compressed like the history `500`, a consistent response | `withGzip` sets `Content-Encoding` before the handler (`middleware.go:202-219`); fact 5 keeps it | confirmed |
| 7 | `json.Marshal` escapes HTML like a default `Encoder`; `Encode` appends `\n` | `encode.go` `Marshal` uses `escapeHTML: true`; `stream.go:235` | confirmed |
| 8 | `withLogging` records the status a handler writes in server stats | `middleware.go:25`, `serverstats.go:70-82` | confirmed |
| 9 | The dashboard treats a non-OK status as a failure, as it does an unparsable `200` today | `internal/web/assets/app.js:193-198` | confirmed |
| 10 | 0018 is unreleased; record number 0022 is free on every remote branch | `git tag --contains` of its add commit is empty; `git ls-tree` over `origin/*` | confirmed |

## Acceptance criteria

- AC1: When the value of a JSON endpoint served by `writeJSON` cannot be encoded, the response has status `500`,
  `Content-Type: text/plain; charset=utf-8` and the body exactly `internal error\n` (no JSON bytes before it).
- AC2: That failure is logged at error level with the message `failed to encode JSON response` and an `error`
  attribute.
- AC3: That request is counted under `5xx`, not `2xx`, in `GET /api/v1/serverstats`.
- AC4: An encodable value is answered as before: status `200`, `Content-Type: application/json; charset=utf-8`, body
  byte-identical to `json.Marshal(v)` followed by `\n`.


## Tests per criterion

Every request of AC1–AC4 goes through `s.Handler().ServeHTTP` (the full middleware chain), never through `writeJSON`
or a handler method called directly.

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestWriteJSON_EncodeFailureReturns500_AC1`, table with `t.Run("AC1 GET /api/v1/metrics")`, `t.Run("AC1 GET /api/v1/metrics/cpu")` (both call sites: `fakeMetrics.snapshot.CPU.OverallPercent = math.NaN()`) and `t.Run("AC1 GET /api/v1/metrics gzip")` (request with `Accept-Encoding: gzip`; asserts `500`, `Content-Encoding: gzip` and a decompressed body of `internal error\n`) | `internal/httpapi/handlers_test.go` |
| AC2 | `TestWriteJSON_EncodeFailureLogged_AC2` (server built with `New(fm, Config{}, nil, slog.New(slog.NewTextHandler(&buf, …)))`, as `middleware_test.go:75-76`; asserts `level=ERROR`, the message and `error=` in `buf`) | `internal/httpapi/handlers_test.go` |
| AC3 | `TestWriteJSON_EncodeFailureCountedAs5xx_AC3` (a NaN `GET /api/v1/metrics`, then `GET /api/v1/serverstats` with an encodable state; `5xx` is 1 and `2xx` is 0: a request is recorded after its handler returns) | `internal/httpapi/handlers_test.go` |
| AC4 | `TestWriteJSON_SuccessBodyUnchanged_AC4` (`GET /api/v1/metrics` with `newTestServer`; body equals `json.Marshal(fm.snapshot)` plus `\n`) | `internal/httpapi/handlers_test.go` |

## Approach

`writeJSON` keeps its signature. It encodes with `json.Marshal(v)` first. On an error it logs as today
(`log("failed to encode JSON response", "error", err)`), answers `http.Error(w, "internal error",
http.StatusInternalServerError)` and returns — the same text and status as `handleHistory` (`handlers.go:70-71`).
Only on success it sets `Content-Type: application/json; charset=utf-8` and writes the bytes with a trailing `\n`
appended (`_, _ = w.Write(...)`, as `handleHistory` does), so success bodies stay byte-identical (fact 7). Setting the
JSON content type only on success means `http.Error` alone decides the error's headers. The `encoding/json` import
stays (`json.Marshal`). No call site changes.

The error body is no new user-visible layout: it is the existing `internal error` text of the history endpoint.

## Options considered

1. **Keep `json.NewEncoder(w).Encode` and call `http.Error` when it fails** — works today (fact 1), but rests on
   `Encode` writing nothing on failure, which its documentation does not promise; lost against an explicit buffer the
   function owns.
2. **`bytes.Buffer` plus `Encoder`** — same bytes as `Marshal` plus `\n` with more code; lost on simplicity.
3. **A JSON error body (`{"error": …}`)** — every error of the API is plain text from `http.Error` (`401`, `503`,
   `400`, history `500`); a second error format for one case lost.
4. **Publish the fix under `/api/v2/...`** (the issue asks to decide it; 0018 calls a status-code change a shape
   change) — lost: see [0022](../../docs/decisions/0022-unencodable-v1-response-answers-500.md); no documented response
   changes status, and 0018 now names that scope.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/httpapi` | `handlers.go`, `writeJSON` | marshal first, `500` on failure |
| `internal/httpapi` | `handlers_test.go` | four new tests |

## Signatures (for the Dev's skeleton)

None: `writeJSON` keeps its unexported signature, and no other member is added or changed. No skeleton file needs
rewriting; the Tester's tests compile against the unchanged code and fail on AC1–AC3.

## Test files

`internal/httpapi/handlers_test.go`.

Existing test code that calls a changed signature: none. Existing tests kept unchanged: the JSON endpoint tests decode
bodies or trim the newline (`routes_test.go:191`, `handlers_test.go:1181`) and use encodable values, which AC4 keeps
identical.

Files of the change no unit test can cover: none.

## Areas

- HTTP API (`docs/areas/http-api.md`): the encode-failure row covers every JSON endpoint; the "truncated body" bullet
  goes; the versioning paragraph names the documented response and links 0022; *Related decisions* lists 0022.

## Documentation updates

By the Dev once the code is green, except where an owner reports text for the orchestrator to write in. Where the
rule's wording is fixed below, it is exact, so the five statements of the rule stay in step.

- `docs/areas/http-api.md`, *Limits and errors*: the row "History response could not be encoded" becomes "A JSON
  response could not be encoded (any `/api/v1/...` JSON endpoint)", same status `500` and body text; delete the
  bullet at lines 202-203 ("The other JSON endpoints write their body as they encode it …").
- `docs/areas/http-api.md`, *Versioning and stability* (lines 22-23): "or changing a status code is a shape change and
  needs `/api/v2/...`." reads "or changing the status code of a documented response is a shape change and needs
  `/api/v2/...`; a `500` for a response that cannot be encoded is none
  ([0022](../decisions/0022-unencodable-v1-response-answers-500.md))."
- `docs/areas/http-api.md`, *Related decisions*: add 0022 at the top.
- `docs/API.md`: a short `## Errors` section before `## Endpoints`: any `/api/v1/...` JSON endpoint answers `500
  Internal Server Error` with the plain-text body `internal error` when its response cannot be encoded; clients treat
  it as a server error.
- `docs/CONTRIBUTING.md`, project block `stability` (lines 183-187), Dev: "or to a status code are breaking changes"
  reads "or to the status code of a documented response are breaking changes"; after "is not a breaking change
  ([decision 0018](decisions/0018-new-key-in-v1-response-is-no-shape-change.md))." add the sentence "Neither is a
  `500` for a response that cannot be encoded
  ([decision 0022](decisions/0022-unencodable-v1-response-answers-500.md))."
- `AGENTS.md` and `.github/copilot-instructions.md`, project block `code-style` (`AGENTS.md:140-143`), identical in
  both (`config-check.py`); the Dev reports the text, the orchestrator writes it in (instruction files): "or to a
  status code is a breaking change" reads "or to the status code of a documented response is a breaking change", and
  "is not breaking ([decision 0018](/docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md))." reads "is not
  breaking ([decision 0018](/docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md)), nor is a `500` for a
  response that cannot be encoded ([decision 0022](/docs/decisions/0022-unencodable-v1-response-answers-500.md))."
- `.squad/project.md`, *Guarantees*, the `/api/v1/...` line (lines 67-70); the Dev reports the text, the orchestrator
  writes it in: "or to a status code adds `/api/v2/...`; a new key in a response object, with its presence rule
  documented in API.md, stays in v1" reads "or to the status code of a documented response adds `/api/v2/...`; a new
  key in a response object, with its presence rule documented in API.md, stays in v1, and so does a `500` for a
  response that cannot be encoded", and the link list gains
  `[0022](../docs/decisions/0022-unencodable-v1-response-answers-500.md)` after the 0018 link. The Product Manager
  approved this edit of the guarantee (answer (a) to the Lead's escalation, 2026-11-17).

Sweep: the error statuses are enumerated only in the *Limits and errors* table of `http-api.md`. The status-code rule
("status code" searched in `AGENTS.md`, `.github/copilot-instructions.md`, `.squad/project.md`, `docs/`, `README.md`)
is stated in `AGENTS.md:140-141`, `.github/copilot-instructions.md:139-140`, `docs/CONTRIBUTING.md:183-184`,
`.squad/project.md:67-68`, `docs/areas/http-api.md:22-23` and the records 0018 (edited in place by the Lead) and 0022,
each covered above; `docs/API.md:7-9` speaks only of response shape and stays. `README.md`, `SECURITY.md` and
`docs/ARCHITECTURE.md` name neither. No changelog exists; the PR template asks for `docs/API.md` (above).

## Architecture check

No flow of `docs/ARCHITECTURE.md` changes: every route keeps its middleware chain; no `ARCHITECTURE.md` update. The
v1 stability guarantee of `.squad/project.md` gets its scope named (the status of documented responses), which keeps
every documented response's status and body and admits the `500` this fix adds; the Product Manager approved this change of
the guarantee's text (answer (a), 2026-11-17).

## Security considerations

- [x] No limits.
- [x] The error that reaches the client is the fixed text `internal error`; the encoder's error message goes only to
  the log, never into the response.

## Decision records

- `docs/decisions/0022-unencodable-v1-response-answers-500.md` (Proposed)
- `docs/decisions/0018-new-key-in-v1-response-is-no-shape-change.md` (unreleased, edited in place: the Decision names
  the documented response and links 0022)

## Challenge

Devil's Advocate, plan round (one major, three minor objections):

- **Major 1 — 0022 narrows 0018 without changing it; the rule stays unqualified in four places.** Accepted. 0018's
  Decision now reads "or to the status code of a documented response" with a link to 0022 (unreleased: no tag
  contains it); 0022's Decision says it states the scope of 0018's rule. *Documentation updates* qualifies the rule
  in `AGENTS.md` with `.github/copilot-instructions.md`, `docs/CONTRIBUTING.md`, `.squad/project.md` and
  `docs/areas/http-api.md` with exact wording, and the sweep lists every statement. The `.squad/project.md` line is a
  guarantee; the Product Manager approved its edit (answer (a)).
- **Minor 1 — a status-only probe does see the change.** Accepted: 0022 Option 1 and *Consequences* say that such a
  check now reports the failure, which is intended.
- **Minor 2 — no gzip case.** Accepted: AC1's table gains `t.Run("AC1 GET /api/v1/metrics gzip")`.
- **Minor 3 — tests through the full chain.** Accepted: *Tests per criterion* requires `s.Handler().ServeHTTP` for
  every request of AC1–AC4.

## Security review

Security, plan round: `APPROVED`, one non-blocking note.

- **AC1 not tested under gzip.** Taken (the recommended fix): the same gzip row as Minor 2, asserting `500`,
  `Content-Encoding: gzip` and the decompressed body `internal error\n`.

## Out of scope / follow-ups

- Where a NaN in a snapshot, alert report or process list would come from is not investigated here; this change makes
  such a failure visible.
