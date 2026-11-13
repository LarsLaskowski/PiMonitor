# Plan: Webhook notifier logs the full webhook URL, including embedded tokens

Source: Issue #194
Tier: security — the change alters how the notifier handles a credential (the webhook URL) in its log output, a security control under *storage or handling of secrets and credentials*.
Security areas touched: Logging (`slog` calls of `internal/alert/notify.go`), Outbound calls (webhook delivery in `internal/alert/notify.go`)

## Problem / root cause

`internal/alert/notify.go` passes the configured URL `w.wh.url` / `wh.url` as the `"url"` attribute of all six
log calls — queue full (line 206), rate-limited (226), render failed (231), rejected (300–301), giving up
(304–305), retry (308–309) — and passes the delivery error unchanged as `"error"` in the last three. That error
comes from `post` (line 344): `http.NewRequestWithContext` (line 348) returns the `*url.Error` of `url.Parse`
with the raw URL, and `n.client.Do` (line 354) returns a `*url.Error` whose text is `Post "<URL>": …`. So the
URL's userinfo, path, query and fragment reach the log twice.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | Six log calls carry the full URL as `"url"` (issue gives lines 206, 221, 226, 295, 299, 303) | `notify.go` lines 206, 226, 231, 301, 305, 309; the issue's line numbers are off by up to 6, the set of calls is right | confirmed |
| 2 | The `error` field of the three `deliver` calls leaks the URL through `*url.Error` | `post` returns the errors of `http.NewRequestWithContext` (request.go 912–914: raw `url.Parse` error, Op `parse`, URL = raw string) and `Client.Do` (client.go 634–648 `uerr`); `url.Error.Error()` is `%s %q: %s` (url.go 40) | confirmed |
| 3 | The client masks only the password: userinfo user name, path, query and fragment stay in `*url.Error.URL` | `stripPassword` (client.go 1064–1070) replaces the password by `***`, nothing else | confirmed |
| 4 | The Op of a POST's `*url.Error` is `Post` | `urlErrorOp` (client.go 546) | confirmed |
| 5 | A RoundTripper error reaches `*url.Error.Err` unwrapped when the client has no `Timeout` | `send` (client.go 266–280) returns `err` as is; `Notifier.client` is `&http.Client{}` (`notify.go` 142) | confirmed |
| 6 | Inner transport errors carry no part of the configured URL that the notifier puts there, but they can quote bytes the endpoint sends: an unparsable redirect `Location` and a malformed status line (`malformed HTTP response "<line>"`, `malformed HTTP status code "<code>"`), so an endpoint that echoes the request line puts the configured path and query into the error text even after redaction | `client.go` 665; `badStringError` (request.go 97) used by response.go 174, 181, 185; echo case measured by Security in the step-3 review; the original claim ("at most host and port") is refuted | confirmed |
| 7 | Parse errors of `net/url` can quote up to three bytes of the URL (`EscapeError`), one host byte (`InvalidHostError`) or the port | url.go 69–77, 119, 128, 140, 149, 562, 631 | confirmed |
| 8 | `.squad/project.md` (*Security areas*, *Logging*) states that the notifier logs the configured webhook URL | `.squad/project.md` line 46–48 | confirmed |
| 9 | `docs/areas/notifications.md` documents the `url` field and that the URL is logged in full | notifications.md lines 131–142 | confirmed |
| 10 | No other code logs a webhook URL; config validation errors name the index, not the URL | grep of `"url"` and `Webhooks` in `cmd/` and `internal/`; `config.go` 402–411; template errors use the name `webhook[<i>]` (`notify.go` 125) | confirmed |
| 11 | The bug report template asks users to paste the journal | `.github/ISSUE_TEMPLATE/bug_report.md` line 35 (it already asks to redact tokens) | confirmed |

## Acceptance criteria

Sentinel URL for AC1–AC3: a URL whose userinfo user name and password, path, query and fragment each contain one
sentinel string (for example `https://SNTL:SNTL@hooks.example:8443/services/T0/B0/SNTL?token=SNTL#SNTL`).

- AC1: For each of the six notifier log calls (queue full, rate-limited, render failed, rejected, giving up after
  retries, retry), a webhook configured with the sentinel URL at index 1 of `alerts.webhooks` produces the call's log record
  (found by its message and `webhook` = 1), which has no `url` attribute and carries `webhook` = `1` (a JSON number)
  and `endpoint` = `https://hooks.example:8443`; and the sentinel appears nowhere in the whole captured log output of
  the test (webhook 0, where configured, has a URL without the sentinel).
- AC2: When the HTTP client fails with a transport error whose own text holds no URL, the `error` attribute of the retry and giving-up records (a
  transport error is always retried, so it never reaches the rejected call) is the `*url.Error` text with the endpoint in place of the URL: for a transport that fails
  with `stub transport failure` exactly `Post "https://hooks.example:8443": stub transport failure`, sentinel
  absent.
- AC3: When the request cannot be built because the configured URL does not parse, the record's `endpoint` is
  exactly `[invalid URL]` and its `error` is exactly `parse "[invalid URL]": invalid URL`, sentinel absent — for an
  invalid percent escape directly before the sentinel in the path (`…/%SNTL…`, whose parser error would quote
  `%SN`) and for a DEL byte (U+007F) in the
  path.
- AC4: `endpoint` is `<scheme>://<host>` with the port when the URL has one, and `[invalid URL]` whenever the
  parsed URL has an empty scheme or an empty host: `https://u:p@hooks.example/x?t=1#f` → `https://hooks.example`;
  `http://[::1]:8080/x` → `http://[::1]:8080`; `hooks.example/x/SNTL` (no scheme) → `[invalid URL]`;
  `https:SNTL@hooks.example/x` (opaque) → `[invalid URL]`; `https:///SNTL` (empty host) → `[invalid URL]`;
  `//hooks.example/SNTL` (no scheme) → `[invalid URL]`; the empty string → `[invalid URL]`.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestNotifier_LogRedactsWebhookURL`, one subtest per log call (`AC1 queue full`, `AC1 rate-limited`, `AC1 render failed`, `AC1 rejected`, `AC1 giving up`, `AC1 retry`) | `internal/alert/notify_test.go` |
| AC2 | `TestNotifier_LogRedactsTransportError` (`AC2 …` subtests for the retry and the giving-up record) | `internal/alert/notify_test.go` |
| AC3 | `TestNotifier_LogRedactsUnparsableURL` (`AC3 invalid escape`, `AC3 control character`) | `internal/alert/notify_test.go` |
| AC4 | `TestNotifier_LogEndpointForms` (table, `AC4 …` subtests named by form, never containing the sentinel) | `internal/alert/notify_test.go` |

How the tests drive the calls, without real network: a `*slog.Logger` with `slog.NewJSONHandler` writing to a
buffer (guarded by a mutex if a test starts the workers) is passed to `NewNotifier`; each record is decoded and
checked. The HTTP side is a stub `http.RoundTripper` set on the existing `n.client` field (status 400 for
rejected, 500 for giving up and retry, an error for AC2), so the sentinel host is never resolved. Rate-limited,
render failed (a template `{{.Nope}}`), rejected, giving up and retry call the existing `n.dispatch` directly on
`n.workers[1]`; queue full calls `Notify` with 257 events on a notifier that was not started. AC4 uses the
render-failed path, which needs no HTTP at all. Every test asserts that the expected record exists before the
leak check, so a missing record cannot pass. The sentinel is kept out of every subtest name
(`docs/UNIT_TESTS.md`, *Leak, error-text and timing tests*); the DEL byte is written as the Go escape `\x7f` and
the test asserts the URL contains it.

## Approach

In `internal/alert/notify.go`:

- `webhook` gains two private fields, `index int` (the position in `cfg.Webhooks`) and `endpoint string`
  (computed once in `NewNotifier` from `w.URL`). `url` stays: `post` still needs it.
- A private helper `redactURL(raw string) string` parses `raw` with `url.Parse`; on an error, or when the result's
  `Scheme` or `Host` is empty, it returns the constant `[invalid URL]`; otherwise `Scheme + "://" + Host`
  (`Host` includes the port and IPv6 brackets). Userinfo, path, raw query, opaque part and fragment are never
  read into the result.
- A private helper `redactError(err error) string`: when `errors.As` finds a `*url.Error`, it returns the text of
  a new `url.Error` with the same `Op`, `URL` = `redactURL(ue.URL)` and, when `Op` is `parse`, `Err` =
  `errors.New("invalid URL")`, else the original `ue.Err`; any other error returns `err.Error()`. Rebuilding from
  the `*url.Error` alone drops any wrapper text around it (none today: `post` returns it unwrapped).
- The six log calls replace `"url", <url>` with `"webhook", <index>, "endpoint", <endpoint>`, in that position
  (directly after the message). The three `deliver` calls log `"error", redactError(err)`; the render-failed call
  keeps `"error", err` (a template or JSON error carries no URL, fact 10). Messages, levels and the other
  attributes stay as they are.
- Names: `index`, `endpoint`, `redactURL`, `redactError` contain none of the names `go/clear-text-logging`
  treats as sensitive (`.squad/stack.md`, *Logger arguments*); `redact` additionally marks a value as safe.

Accepted forms of the guard (the URL forms `url.Parse` and the client produce, and what `endpoint` shows):
absolute `scheme://[userinfo@]host[:port]/path?query#fragment` → scheme and host; userinfo with a password the
client masked (`user:***@host`) → scheme and host; IPv6 literal with port → bracketed host and port; no scheme
(`host/path`, `//host/path`), opaque (`scheme:rest`), empty host (`scheme:///path`), empty string, any string
`url.Parse` rejects (control byte, bad escape, bad host byte, bad port) → `[invalid URL]`; a redirect's
`*url.Error` carries the redirect target's URL → its scheme and host. A secret in the host name itself is logged,
and bytes an endpoint echoes back in a malformed response stay in the cause as `net/http` quotes them (decision
record, *Consequences*).

New user-visible output: the log lines' attribute set changes as above; the message texts do not change.

## Options considered

1. Index plus scheme and host, `*url.Error` rebuilt, parse detail dropped — chosen.
2. Index only — lost: the operator must count list entries to find the host, and inner transport errors name the
   host anyway, so nothing more is hidden.
3. Scheme and host only (the issue's first example) — lost: two webhooks on one service (two Slack channels)
   cannot be told apart.
4. URL with userinfo, path and query replaced by a placeholder (the issue's second example) — lost: no more
   information than option 1, and a rewrite of URL parts is easier to get wrong than a string built from two
   fields.
5. String substitution of the configured URL inside the error text — lost: misses the password-masked form, a
   redirect target and an unparsable URL, and fails open on any byte difference.
6. Keep the inner parse error of an unparsable URL — lost: `EscapeError` quotes up to three bytes of the path,
   query-adjacent or userinfo text (fact 7); the operator reads the URL in the configuration instead.
7. Full URL only at debug level — lost: the log is shared precisely while a failing webhook is debugged.
8. Also filter the configured path and query out of the inner error text (an echoing endpoint) — lost: option 5
   on free text the endpoint chose, fails open the same way, against the party that holds the secret already
   (*Security review*, finding 1).

The reasons are in [0007](../../docs/decisions/0007-log-webhook-by-index-and-host.md).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/alert` | `webhook`, `NewNotifier`, `Notify`, `dispatch`, `deliver` in `notify.go` | two private fields, two private helpers, six log calls |
| `internal/alert` | `notify_test.go` | four new tests |

## Signatures (for the Dev's skeleton)

None: the change adds no exported member and changes no signature. The private fields and helpers are described in
*Approach*; the tests reach them only through `NewNotifier`, `Notify`, `dispatch` and the `client` and `workers`
fields that exist today, so no skeleton is needed and the tests compile against the current code.

## Test files

`internal/alert/notify_test.go` (tests of `notify.go`).

Existing test code that calls a changed signature: none. No existing test asserts on the `url` attribute or
on log output (they pass a nil logger).

Files of the change no unit test can cover: none.

## Areas

- Notifications (`docs/areas/notifications.md`): *What is logged* changes — the fields and the redaction rules.

## Documentation updates

All by the Dev, against the code as built:

- `docs/areas/notifications.md`, *What is logged*: in the table, `url` becomes `webhook`, `endpoint` in all six
  rows; replace the bullet "The webhook URL is logged in full…" by bullets stating: `webhook` is the index in
  `alerts.webhooks`; `endpoint` is scheme and host (with port), or `[invalid URL]`; the notifier never logs the
  URL's userinfo, path, query or fragment, also not in the URL part of an error text; a transport error reads
  `Post "<endpoint>": <cause>` and an unparsable URL `parse "[invalid URL]": invalid URL`. Two exceptions, stated
  as such: the host name is logged (a secret in a host name is not protected), and the `<cause>` quotes bytes the
  endpoint itself sends as received — an unparsable redirect `Location`, a malformed status line — so an endpoint
  that echoes the request line puts the configured path and query into the log.
  Add [0007](../../docs/decisions/0007-log-webhook-by-index-and-host.md) under *Related decisions*. Under *Limits
  and errors*, first bullet: a malformed URL's failure is logged with `endpoint` `[invalid URL]`.
- `.squad/project.md`, *Security areas*, *Logging* line: the Dev reports the exact text, the orchestrator writes
  it in. Content: no call logs the API key; the notifier names a webhook by its index and scheme and host and
  redacts the URL in `*url.Error` texts, so it never logs the URL's userinfo, path, query or fragment itself —
  except where the endpoint echoes them back in a malformed response, which is quoted (`internal/alert/notify.go`) —
  link [Notifications](../docs/areas/notifications.md#what-is-logged); keep secrets out of new log fields
  (`.squad/stack.md`, *Logger arguments*).
- `SECURITY.md`, *Threat Model / Design Notes*: one bullet — webhook URLs often are the webhook's credential;
  the full URLs live only in the configuration file (mode `640 root:pimonitor`); the service log names a webhook
  by its index and the URL's scheme and host, and the notifier never logs userinfo, path, query or fragment itself,
  error texts included; not covered: a service that puts its secret in the host name, and an endpoint that echoes
  the request line back in a malformed response, which `net/http` quotes in the error text.

Sweep: `docs/ARCHITECTURE.md` (notifier section, lines 224–256) states no logged field; `README.md` and
`docs/areas/configuration.md` say nothing about logged URLs; the PR template asks for no changelog (the
repository has none). No further hits.

## Architecture check

No guarantee of `.squad/project.md` changes: delivery, queueing, retries and the rate limiter are untouched, and
the change only alters attributes of existing log calls. `docs/ARCHITECTURE.md` needs no update (it describes no
log fields). The *Logging* security-area line is updated as listed above.

## Security considerations

- [x] No limits: the change parses only the configured URL (once at startup) and URLs from `*url.Error` values.
- [x] Every exception that can reach a user-visible failure reason: none reaches a user; log text only, as in
  *Approach* — `Post "<endpoint>": <cause>` for transport errors, `parse "[invalid URL]": invalid URL` for an
  unparsable URL, `webhook returned status <n>` for a status error, the template or JSON error for render
  failures.

## Decision records

- `docs/decisions/0007-log-webhook-by-index-and-host.md` (Proposed)

## Out of scope / follow-ups

- Validating the URL at startup (failing fast on an unparsable URL) would change configuration behavior; not
  requested here.
- Text the endpoint sends (an unparsable redirect `Location`, a malformed status line, also when it echoes the
  request) stays in the error text as received; documented, not filtered (*Security review*, finding 1).
- `net/http` quotes an invalid `HTTPS_PROXY`/`HTTP_PROXY` value, credentials included, in the transport error. A
  service environment secret, not the webhook URL: linked follow-up issue, title "Notifier error log can quote
  proxy credentials from an invalid HTTPS_PROXY value", body: "When the service's `HTTPS_PROXY`/`HTTP_PROXY` holds
  a value `net/http` cannot parse, the webhook delivery error logged by `internal/alert/notify.go` quotes it, with
  any `user:password@`. Decide whether to redact it in `redactError` or document it in `SECURITY.md`. Found in the
  Security plan review of #194."

## Challenge

Devil's Advocate, two minor objections, both accepted:

1. A record-only leak check passes while another record leaks — accepted: AC1 now requires the expected record
   to exist and the sentinel to be absent from the whole captured output; webhook 0 gets a URL without the
   sentinel. AC2 and AC3 tests do the same.
2. Rate-limited setup and the JSON number type — accepted: the `AC1 rate-limited` subtest sets
   `NotifyMinIntervalSeconds` > 0, gets a first fired event delivered on `n.workers[1]` with a 200 from the stub
   (or seeds `w.lastSent`), then dispatches a second fired event of the same metric inside the interval; the
   decoded `webhook` value is compared as a number (float64 after `json.Unmarshal` into `any`).

## Security review

1. (blocking) Fact 6 was wrong: `net/http` quotes response bytes (`malformed HTTP response`, `malformed HTTP
   status code`), so an echoing endpoint puts the configured path and query into the error text. Decision: keep
   the code design and document it (Security's recommendation). A guard in `redactError` would have to find the
   configured path and query inside free text the endpoint chose, which is the string substitution option 5
   rejects (fails open on any encoding difference), for an endpoint that already holds the secret. Changed: fact 6
   (refuted original claim, corrected), the *Documentation updates* wording for `notifications.md`, `SECURITY.md`
   and `.squad/project.md` (scoped to "the notifier never logs …", echo named beside the host-name exception), AC2
   narrowed to a cause without a URL, record 0007 *Decision* and *Consequences*.
2. (non-blocking) Invalid proxy values quoted with credentials — out of scope (an environment secret, not the
   webhook URL); linked follow-up issue under *Out of scope / follow-ups*.
3. (non-blocking) Option "keep the parser's detail" missing from 0007 — added there as option 7 (it was option 6
   of this plan already).

## Security notes

Security review, mode plan, second round: confirming the Lead's revision of specs/issue-194

**Verdict: approved.** All three findings from my first round are resolved, and the revision introduces no new security defect. I read the diff of commit b7da5a4 ("Revise plan for issue 194"), which changes /home/user/PiMonitor/specs/issue-194/plan.md and /home/user/PiMonitor/docs/decisions/0007-log-webhook-by-index-and-host.md. I ran nothing new: the revision only changes wording and adds no code-level claim beyond what my round-1 run already covered.

**Finding 1 (blocking, wrong fact 6): resolved.**
- **Fact 6** now names both cases where the error text quotes what the endpoint sent back: an unparsable redirect `Location` and a malformed status line or response. It says an endpoint that echoes the request line puts the configured path and query into the log. It cites my round-1 echo measurement and states the original claim was wrong.
- **Documentation updates** now say "the notifier never logs …" instead of "never logged" in all three places:
  - In `docs/areas/notifications.md`, the echoed-response case is a stated exception beside the host-name exception.
  - In the `.squad/project.md` *Logging* line, the echo case is named.
  - In `SECURITY.md`, both cases are listed under "not covered".
- **Decision record 0007**: *Decision* has the same narrower wording, and the *Consequences* bullet names both malformed-response texts and the echo leak.
- **Approach**: the description of what the guard covers now includes the echo case.
- **AC2** is narrowed to a transport error whose own text holds no URL, which is correct.
- **Choice made**: the Lead documents the gap instead of filtering it in code (new option 8). That was my recommended fix.

**Finding 2 (non-blocking, proxy credentials): resolved.** The follow-up issue is planned under *Out of scope / follow-ups*, with its exact title and body.

**Finding 3 (non-blocking, option missing from 0007): resolved.** Option 7 of 0007 now covers keeping the parser's detail.

**New in the revision:** nothing blocking. One cosmetic point: the option numbers differ between the plan (6 parse detail, 7 debug level, 8 filter echoed text) and 0007 (6 filter, 7 parse detail, 8 debug level). The plan says the reasons are in 0007 but does not refer to the options by number there, so nothing breaks. The Lead can renumber the plan to match 0007 or leave it. This is not a security matter.
