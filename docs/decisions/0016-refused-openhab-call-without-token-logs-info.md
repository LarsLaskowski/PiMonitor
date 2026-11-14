# 0016: A refused openHAB call without a configured token is logged at info

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** openHAB
- **Source:** Issue #181
- **Supersedes:** 0001 (amends it: the openHAB sources use its log-once rule, with info instead of warning for a
  refused call while no token is configured)

## Context

[0001](0001-optional-sources-log-failures-once.md) logs an optional source's first failure as a warning. On a
default openHAB 5.2.4 install without an `api_token`, system info, Things and add-ons answer `401` on every poll
([0015](0015-openhab-values-from-real-5x-responses.md)): the expected state of an operator who never set a token,
not a fault. Three warnings at every start would teach operators to ignore openHAB warnings.

## Options considered

1. **Warning, as 0001 says** — three warnings per start on every default install.
2. **Not logged** — an operator who wonders why Things read "needs api_token" finds nothing in the journal.
3. **Info on the first refusal and when it changes, debug while it repeats, while no token is configured; a
   warning as 0001 says once a token is configured** — chosen.

## Decision

An openHAB source refused with `401` or `403` while no `api_token` is configured logs its first refusal at info,
otherwise the log-once rule of 0001 applies unchanged. The messages are in [openHAB](../areas/openhab.md).

## Consequences

- A token that openHAB rejects (revoked, mistyped) is still a warning.
- The dashboard and `GET /api/v1/openhab` carry the hint the log no longer shouts.
