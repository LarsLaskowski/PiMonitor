# 0013: openHAB requests go only to a loopback address over plain HTTP

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** openHAB
- **Source:** Issue #181
- **Supersedes:** —

## Context

Issue #181 makes PiMonitor an HTTP client for the first time outside webhooks: it polls openHAB's REST API, with an
optional openHAB API token, from `openhab.base_url`. The feature is "openHAB on this Pi". A free `base_url` would
turn the service into a request relay towards any host its network reaches and send the token wherever the URL
points. For Pi-hole the project avoided network access and a password in its configuration altogether
([`ARCHITECTURE.md`](../ARCHITECTURE.md)); for openHAB the values the issue asks for (Things, heap, add-ons) exist
only behind its REST API. openHAB serves REST over HTTP on its HTTP port; its HTTPS port (8443) uses a
self-signed certificate.

## Options considered

1. **Any `base_url`** — supports a remote openHAB, which the issue puts out of scope, and makes PiMonitor a relay
   that sends the token to whatever host the URL names.
2. **A loopback host checked at startup only** — `localhost` is resolved later through `/etc/hosts`; the check and
   the connection could disagree.
3. **A loopback host checked at startup, a loopback address checked again when the connection is made, plain HTTP,
   no redirects** — chosen.
4. **Also allow `https`, with certificate verification** — every call to openHAB's self-signed port would fail.
5. **Also allow `https` without verification** — a disabled verification is the pattern every review flags, and on
   loopback it protects nothing that plain HTTP does not.

## Decision

`openhab.base_url` must be an `http` URL whose host is a loopback IP literal or `localhost`, and every connection
is checked again against a loopback address when it is made; redirects are never followed and no proxy is used.
The accepted forms and the error texts are in [openHAB](../areas/openhab.md).

## Consequences

- The token never leaves the machine, and a redirect cannot carry it elsewhere.
- A remote openHAB and openHAB's HTTPS port are not supported; changing that means a new record and a guard for
  the remote case (#181 *Out of scope*).
- A `localhost` that `/etc/hosts` maps elsewhere fails each call instead of reaching that host.
