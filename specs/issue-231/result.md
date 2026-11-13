Issue #231 is tier `patch`: one documentation edit, no `plan.md`, no decision record. The issue's claim is partly wrong. Go drops a proxy value only when it cannot parse it at all, not every value that is malformed. The edit below is worded to match what the code really does.

**Tier: `patch`.** The only change is one bullet in `docs/areas/notifications.md`. Files under `docs/areas/` are excluded from tier `docs` (`.squad/routing.md`, *Tiers*), so this goes to `patch`. It adds about 4 changed lines against `PATCH_MAX_LINES` = 30, with no production logic, dependency, permission or schema change.
- The text describes the security area *Outbound calls* (webhook delivery in `internal/alert/notify.go`, whose area document is notifications.md). It does not touch the code, CI or build files, and it creates, changes or removes no control, so I picked `patch` rather than `patch/security`. The Reviewer may raise it.
- Security areas touched: none.

**Verified facts**
1. **The notifier uses the default transport, so proxies come from the environment — confirmed.** `internal/alert/notify.go:147` builds `&http.Client{}` with a nil Transport. That means `http.DefaultTransport`, whose proxy setting is `ProxyFromEnvironment` (GOROOT `src/net/http/transport.go:48`).
2. **A value that fails to parse is dropped and the request goes direct — confirmed for the values parsing really rejects.** In GOROOT `src/vendor/golang.org/x/net/http/httpproxy/proxy.go`, `config.init` (lines 208–213) keeps the parsed URL only when `parseProxy` returns no error and throws the error away. The proxy is then nil, and `proxyForURL` (lines 137–138) returns nil, nil, which means a direct connection.
3. **"With no error" — confirmed.** The parse error is discarded in `init`. The notifier logs nothing about the proxy.
4. **Every malformed value is dropped — refuted.** `parseProxy` (lines 147–165) first parses the value as it is. If that fails or gives no scheme or host, it retries with `http://` in front and uses that result when it parses. An error comes back only when both attempts fail.
   - I settled this with a scratch experiment (`/tmp/claude-0/-home-user-PiMonitor/4c35554a-7bf8-5381-a84e-033dcaef62e9/scratchpad/px/main.go`, using `httpproxy.Config.ProxyFunc` against an https URL).
   - Dropped (proxy is nil): `%zz`, `http://proxy.lan:3128/%zz`, `http://us%zzer@proxy:3128`, and a value containing `\x7f`.
   - Used as a mangled proxy address instead of being dropped: `http://proxy.lan:31x8` becomes `http://http://proxy.lan:31x8`, `http://proxy lan:3128` becomes `http://http://proxy%20lan:3128`, `proxy.lan:31x8` becomes scheme `proxy.lan`, and `http://[::1` becomes `http://http://[::1`.
5. **Which variable applies — confirmed.** `HTTPS_PROXY` is used for `https` webhook URLs and `HTTP_PROXY` for `http` ones (`proxyForURL`, lines 129–132).
- Related point, not a defect: the doc comment of `ProxyFromEnvironment` (transport.go:511) says "An error is returned if the value is a different form". The implementation never returns that error. This is Go's behavior, so there is nothing to file here.

**Edit.** In `/home/user/PiMonitor/docs/areas/notifications.md`, section *Request*, replace the third bullet:

```
- HTTPS certificates are verified against the system's trust store; there is no option to skip verification. The
  standard proxy environment variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`) of the service are honored.
```
with
```
- HTTPS certificates are verified against the system's trust store; there is no option to skip verification. The
  standard proxy environment variables (`HTTPS_PROXY`, `HTTP_PROXY`, `NO_PROXY`) of the service are honored.
  A proxy value that cannot be parsed as a URL, not even with `http://` put in front (for example one with an
  invalid `%` escape or a control character), is ignored without an error or a log line, and the webhook is then
  contacted directly; any other malformed value is used as the proxy address as it was read.
```

**Acceptance criteria**
- AC1: The *Request* section of `docs/areas/notifications.md` says three things:
  - a proxy value that cannot be parsed even with `http://` put in front is ignored, with no error and no log line;
  - delivery then goes directly to the webhook;
  - other malformed values are not ignored.
- AC2: No other file outside `specs/` changes.

**Check per edit.** The edit is documentation only, so no test is needed. The gate is *Format check* (`test -z "$(gofmt -l .)"`) plus `scope-check.py --tier patch`. The Reviewer verifies AC1 by reading the text against `httpproxy` `parseProxy`/`config.init` (GOROOT `src/vendor/golang.org/x/net/http/httpproxy/proxy.go:147–213`), and AC2 from the diff.

**Documentation sweep.** I searched README.md, SECURITY.md, docs/ (including ARCHITECTURE.md, API.md and decisions), packaging/ and .squad/project.md for proxy statements. Every other hit is about a reverse proxy in front of the Pi, so nothing else changes. There is no changelog, and the PR template's checklist asks for no other document for this change.

**Options considered.** One option: the issue asks for documentation only. Changing the notifier to fail closed or to log a bad proxy would be a behavior change that the issue does not ask for.

RESULT: DONE
