# Plan: Do not let a delivered cleared event count for the webhook rate limit

Source: Issue #199
Tier: standard — a behavior change of the webhook rate limiter, which coalesces the alert engine's own events
against noise and is no guard against an attacker's input, so no security control is created, changed or removed.
Security areas touched: Outbound calls (webhook delivery, `internal/alert/notify.go`)

## Problem / root cause

`dispatch` (`internal/alert/notify.go:229-231`) calls `recordSent` after every successful delivery, whatever the
event's kind, and `recordSent` stamps the key (`notify.go:270`). `rateLimited` (`notify.go:242-248`) then compares
the next `fired` event against that stamp, so a delivered `cleared` event starts a window in which the next
`fired` event of the same metric and resource is dropped (`notify.go:219-222`).

Related defect, fixed with AC5: the comment on `NotifyMinIntervalSeconds` (`internal/config/config.go:60-63`) and
the example (`packaging/pimonitor.example.yaml:199-203`) claim the limit keeps a flapping alert from flooding a
destination; after this change the limiter no longer damps flapping at all (record 0005, *Consequences*), so
that claim goes too.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | A delivered `cleared` event starts the window | `notify.go:229-231` calls `recordSent` for any kind; `notify.go:270` stamps the key | confirmed |
| 2 | A `fired` event within the interval after that is dropped | `rateLimited` (`notify.go:242-248`) reads the key regardless of the stamping event's kind; `dispatch` drops on true (`notify.go:219-222`) | confirmed |
| 3 | It needs `alerts.for_seconds` plus one poll interval shorter than the interval | `docs/areas/alerts.md`, *Debounce*: the debounce applies in both directions; the refire's first sample above a cutoff is at least one poll after the `cleared` sample and is confirmed `for_seconds` later | confirmed |
| 4a | The example describes the limit per metric "to the same webhook URL" | `pimonitor.example.yaml:199-203`: "for the same metric … same webhook URL" | confirmed |
| 4b | The `config.go` comment describes it per metric | `config.go:60-63`: "per webhook … the same URL", no metric named at all; it is wrong all the same | refuted |
| 5 | The code limits per metric and resource, per webhook entry | key `metric + NUL + resource` (`notify.go:275-277`); one `webhookWorker` with its own `lastSent` per config entry, also for two entries with the same URL (`notify.go:110-137`) | confirmed |
| 6 | `cleared` events are never rate-limited | `notify.go:219` skips `rateLimited` for `KindCleared` | confirmed |
| 7 | Every severity decrease is `cleared`, `crit` → `warn` included | `internal/alert/alert.go:340-343` | confirmed |
| 8 | After the change the limiter coalesces only `fired` after `fired` without a `cleared` between (`ok` → `warn` → `crit`) | facts 7 and `docs/areas/alerts.md`, *Level transitions* | confirmed |
| 9 | `lastSent` is touched only on the webhook's worker goroutine, so `dispatch` may delete from it without a lock | `notify.go:92-95`, `notify.go:168-176` | confirmed |
| 10 | No existing test asserts that a `cleared` delivery limits a later firing | `internal/alert/notify_test.go:354-745` | confirmed |
| 11 | `dispatch` is synchronous: after it returns, the request was answered | `deliver` → `post` waits for the response (`notify.go:283-357`) | confirmed |
| 12 | The repository has no changelog | no `CHANGELOG*` in the repository root | confirmed |

## Acceptance criteria

AC1–AC5 are the issue's, verbatim; AC6 makes testable the issue's *Decision* sentence "A `fired` event after a
`cleared` event is always delivered" for a `cleared` delivery that failed.

- AC1: A `cleared` event does not start or extend the rate-limit window.
- AC2: A `fired` event that follows a delivered `cleared` for the same metric and resource within the interval is
  delivered.
- AC3: Repeated `fired` events for the same metric and resource within the interval are still dropped after the
  first delivery.
- AC4: `cleared` events are still never rate limited.
- AC5: The comments in `pimonitor.example.yaml` and `config.go` say "per metric and resource, per webhook".
- AC6: A `fired` event that follows a `cleared` event of the same metric and resource within the interval is
  delivered also when that `cleared` delivery failed (rejected or given up).

## Tests per criterion

All new tests are subtests of one new table test, TestNotifier_ClearedEndsRateLimitWindow, each case a sequence of
events and the expected delivered sequence.

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | TestNotifier_ClearedEndsRateLimitWindow: "AC1 cleared without earlier firing starts no window", "AC1 cleared does not extend the window" | `internal/alert/notify_test.go` |
| AC2 | TestNotifier_ClearedEndsRateLimitWindow: "AC2 firing after delivered cleared inside window is delivered" | `internal/alert/notify_test.go` |
| AC3 | TestNotifier_ClearedEndsRateLimitWindow: "AC3 repeated firing after a refire is dropped" | `internal/alert/notify_test.go` |
| AC4 | TestNotifier_ClearedEndsRateLimitWindow: "AC4 cleared after a dropped firing is delivered" | `internal/alert/notify_test.go` |
| AC5 | manual: Reviewer, step 8, reads the comment on NotifyMinIntervalSeconds in `internal/config/config.go` and the `notify_min_interval_seconds` comment in `packaging/pimonitor.example.yaml` on the reviewed head for the phrase and the wording under *Approach* | — |
| AC6 | TestNotifier_ClearedEndsRateLimitWindow: "AC6 firing after rejected cleared inside window is delivered" | `internal/alert/notify_test.go` |

Cases, with a 60 s interval, base a fixed time, and the delivered sequence expected:

- AC1, first case: cleared at base, fired at base+1s — both delivered; the worker's lastSent holds no key after
  the cleared.
- AC1, second case: fired at base, cleared at base+50s, fired at base+70s — all three delivered.
- AC2: fired at base, cleared at base+1s, fired at base+2s — all three delivered.
- AC3: fired at base, cleared at base+1s, fired at base+2s, fired (warn to crit) at base+3s — the last one dropped.
- AC4: fired at base, fired at base+1s (dropped), cleared at base+2s — the cleared delivered. The existing
  TestNotifier_ClearedBypassesRateLimit stays unchanged.
- AC6: the server answers 400 to bodies whose kind is cleared; fired at base, cleared at base+1s (rejected),
  fired at base+2s — the second fired delivered.

The tests call `dispatch` directly on `n.workers[0]` of a notifier built with NewNotifier (no Start), against
newCapturingServer or a handler that answers by the body's `kind`; since `dispatch` is synchronous (fact 11) the
delivered sequence is the channel's content after the last call, without sleeps. AC2, AC1's second case and AC6
fail on today's code; AC3 and AC4 pin the behavior that must survive.

## Approach

In `dispatch` (`internal/alert/notify.go`):

- A `cleared` event first deletes its key from the worker's lastSent, before rendering and delivery, so the
  outcome of its delivery does not matter (AC1, AC2, AC6). It still bypasses `rateLimited` (AC4).
- A `fired` event is checked by `rateLimited` as today (AC3).
- After a successful delivery, `recordSent` runs only for a `fired` event (AC1). Pruning stays in `recordSent`:
  entries are created only there, so the map stays bounded as before.

The doc comments of `webhookWorker.lastSent`, `dispatch`, `rateLimited` and `recordSent` are brought in line (the
last delivered `fired` event; a `cleared` event ends the window).

AC5: the comment on NotifyMinIntervalSeconds in `internal/config/config.go` reads:

    // NotifyMinIntervalSeconds rate-limits fired events per metric and
    // resource, per webhook: a fired event less than this many seconds after
    // the last delivered fired event of the same metric and resource to the
    // same webhook is dropped. A cleared event is never rate-limited and ends
    // the window, so the next fired event is delivered. Zero disables
    // rate-limiting.

and the comment above `notify_min_interval_seconds` in `packaging/pimonitor.example.yaml`:

    # Rate limit, per metric and resource, per webhook: a "fired" event less
    # than this many seconds after the last delivered "fired" event of the
    # same metric and resource to the same webhook is dropped. "cleared"
    # (recovery) events are never rate-limited and end the window, so the
    # next "fired" event is always sent. 0 disables it.

The phrase "per metric and resource, per webhook" is binding; the rest may be reworded by the Dev if it stays true.
No new user-visible output; the log lines are unchanged.

## Options considered

- Chosen: a `cleared` event that reaches the worker ends its window whatever its delivery outcome; only delivered
  `fired` events start one.
- Only stop recording `cleared` deliveries (the issue's first *Decision* sentence alone): lost — fired, cleared,
  fired within one interval still drops the refire inside the first firing's window, so AC2 fails.
- End the window only on a delivered `cleared` (AC2's wording alone): lost — after a failed `cleared` the refire is
  dropped, against the issue's "always delivered", and the limiter's state would depend on the outcome of an event
  it never limits.

Reasons in record 0005.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| internal/alert | `notify.go`: dispatch, recordSent call, doc comments | cleared ends the window; only fired records |
| internal/alert | `notify_test.go` | new table test (Tester) |
| internal/config | `config.go`: comment on NotifyMinIntervalSeconds | AC5 wording |
| packaging | `pimonitor.example.yaml`: comment | AC5 wording |

## Signatures (for the Dev's skeleton)

None: the change is inside private members of the notifier; no skeleton step.

## Test files

- `internal/alert/notify_test.go`

Existing test code that calls a changed signature: none.

Files of the change no unit test can cover: none (the YAML and Go comments are checked under AC5).

## Areas

- Notifications (`docs/areas/notifications.md`): *Rate limit* states that only a delivered `fired` event starts a
  window, that a `cleared` event ends the window of its metric and resource whatever its delivery outcome, and
  that what remains coalesced is a `fired` event after another `fired` event; *Related decisions* links 0005;
  both *Open questions* entries are removed (the section then says "None.").

## Documentation updates

Owner: Dev, written against the code once green.

- `docs/areas/notifications.md`: as under *Areas*. In *Rate limit*, bullet 2 loses "whether that delivery was a
  `fired` or a `cleared` event" and says "the last successful delivery of a `fired` event"; bullet 4 ("Only
  successful deliveries count") is narrowed to `fired` deliveries; a new bullet says a `cleared` event ends the
  window (also when its delivery fails) and that a flapping alert is therefore bounded by the debounce only.
- `docs/ARCHITECTURE.md`, line 240 ("The per-`(metric, resource)` rate limiter coalesces repeated firings."):
  add that a `cleared` event ends the window of its pair, linking `decisions/0005-cleared-event-ends-rate-limit-window.md`.
- `internal/config/config.go`, `packaging/pimonitor.example.yaml`: AC5 (above).
- Sweep done: `README.md` ("basic rate-limiting"), `docs/areas/configuration.md` (row "Rate-limit window"),
  `docs/areas/alerts.md`, `docs/API.md`, `SECURITY.md` (its rate limiting is the HTTP API's) and
  `.squad/project.md` (guarantee "`cleared` events bypass the rate limiter") stay true; no changelog exists
  (fact 12).

## Architecture check

The guarantee "a hung or failing webhook never stalls collection, and `cleared` events bypass the rate limiter"
holds: delivery stays on the worker, and `cleared` still skips `rateLimited`. The only state change is a map
delete on the worker goroutine that owns the map (fact 9).

## Security considerations

- [x] No limits on parsed input: the change parses nothing. The rate limit's resource bound holds: lastSent gains
  entries only from delivered `fired` events, each of which prunes expired entries, and a `cleared` event only
  removes one. The outbound request rate per metric and resource is bounded by the debounce and poll interval,
  not by the limiter, for an alert flapping between `fired` and `cleared` (record 0005).
- [x] No error reaches a user-visible message; log lines and fields are unchanged.

## Decision records

- `docs/decisions/0005-cleared-event-ends-rate-limit-window.md` (Proposed)

## Out of scope / follow-ups

- A `cleared` event dropped at a full queue never reaches the worker and ends no window (record 0005); no change.

## Challenge notes

Devil's Advocate, mode plan, issue #199 (/home/user/PiMonitor/specs/issue-199/plan.md)

The plan holds up. I checked these against the code:
- Root cause (facts 1, 2, 6). In /home/user/PiMonitor/internal/alert/notify.go, `dispatch` checks the rate limit only for `fired` events (line 219) and calls `recordSent` after any successful delivery (lines 229-231). `recordSent` stamps the key at line 270.
- AC2, AC6 and the second AC1 case fail on today's code, so the tests catch the defect. Today a delivered `cleared` at base+50s puts the `fired` at base+70s inside the window. A rejected `cleared` leaves base's window in place, so the `fired` at base+2s is dropped. The second AC1 case and AC2 also rule out option 2, and AC6 rules out option 3.
- No sibling path is missed. `eventReaches` (notify.go:435-441) filters on the higher of From and To. A webhook with min_level crit receives every `fired` that reaches crit, and also the `cleared` that left crit before it (crit→warn or crit→ok, both `cleared` per alert.go:340-343). A filtered webhook therefore never sees a refire without the `cleared` that ends its window.
- No existing test breaks. TestNotifier_ClearedBypassesRateLimit (notify_test.go:394) sends fired then cleared and asserts both arrive, which still holds. TestNotifier_RateLimitsPerWebhook sends fired then fired, which is still coalesced.
- Calling `dispatch` directly works for the tests: `deliver`/`post` is synchronous, and with maxRetries 0 a 400 is a permanent error, so there is no retry. newCapturingServer's channel holds 16 requests, which is enough.

Objections:

1. minor: In /home/user/PiMonitor/docs/ARCHITECTURE.md line 240, the sentence after the one the plan edits ("Each successful delivery drops the entries whose window has already run out", line 240-241) becomes false: after the change only a successful `fired` delivery prunes, because pruning stays in `recordSent`. Dev: when adding the `cleared` sentence there, change "Each successful delivery" to "Each successful delivery of a `fired` event".

2. minor: Two comments still say the limiter keeps a flapping alert from flooding a webhook, which record 0005 (*Consequences*) now says is untrue. One is the doc comment on `rateLimited` in /home/user/PiMonitor/internal/alert/notify.go (line 237, "so a fast-flapping metric can't flood a webhook"). The other is the comment in `dispatch` at notify.go:218 ("The limiter only coalesces repeated firings"). Dev: remove the flapping/flood claim from `rateLimited`'s comment and say it coalesces a `fired` event after a delivered `fired` event with no `cleared` between. Then grep notify.go for "flap" and "flood" and leave no match.
