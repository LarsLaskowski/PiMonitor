# 0007: Log a webhook by its index and its scheme and host only

- **Status:** Proposed
- **Date:** 2026-11-13
- **Area:** Notifications
- **Source:** Issue #194
- **Supersedes:** —

## Context

For Slack, Discord, Home Assistant, ntfy and any URL with a `?token=` or `user:pass@`, the webhook URL is the
credential. The notifier logged that URL in full on every delivery problem, and the transport error it logged
beside it repeats it: `net/http` returns a `*url.Error` whose text is `Post "<URL>": …` (the client masks only a
password, as `user:***@`), and a URL `net/url` cannot parse comes back as `parse "<raw URL>": …`. Everyone who can
read the service journal, and every bug report a user pastes it into, received the secret, repeatedly while an
endpoint failed. The log still has to tell the operator which webhook failed and why.

## Options considered

1. **The index in `alerts.webhooks` plus the URL's scheme and host (with port); a `*url.Error` is rebuilt with that
   endpoint in place of its URL, and the inner error of a URL that does not parse is replaced by a fixed text** —
   names the webhook unambiguously (two Slack webhooks share a host) and keeps the diagnosis (which host, DNS or
   TLS failure); the host stays visible, which the inner transport errors (`dial tcp`, `lookup`, certificate
   names) show anyway.
2. **Index only** — no part of the URL at all, but the operator has to count list entries to find the host, and
   the inner transport errors still name the host, so it hides nothing more.
3. **Scheme and host only** — two webhooks on the same service cannot be told apart.
4. **The URL with userinfo, path and query replaced by a placeholder** — says a path exists, nothing more than
   option 1, and a hand-made rewrite of URL parts is easier to get wrong than building the string from scheme and
   host alone.
5. **Replace the configured URL inside the error text by string substitution** — misses the forms the client
   reports differently: the password masked as `***`, a redirect target, a URL that does not parse; and it fails
   open when the text differs by one byte.
6. **Keep the full URL at debug level** — the journal still receives it whenever someone debugs a failing
   webhook, which is exactly when the log is shared.

## Decision

Notifier log lines identify a webhook by its configuration index and by its URL's scheme and host, never by its
userinfo, path, query or fragment, also inside error texts (option 1). The fields and texts are in
[Notifications](../areas/notifications.md#what-is-logged).

## Consequences

- A service that puts its secret in the host name itself (a per-user subdomain) is not protected: the host is
  logged, and `net/http`'s inner errors name it in any case.
- A URL that does not parse is logged only as invalid, without the parser's detail (which can quote up to three
  bytes of the URL); the operator reads the URL in the configuration file.
- Text the endpoint itself chose (an unparsable redirect `Location`) still reaches the log as received; it is not
  the configured secret.
- Revisiting this means a new log field naming more of the URL, which needs the same leak tests.
