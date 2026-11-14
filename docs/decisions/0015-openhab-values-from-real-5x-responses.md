# 0015: openHAB values are built from real openHAB 5.2.4 responses; uptime from the process

- **Status:** Proposed
- **Date:** 2026-11-14
- **Area:** openHAB
- **Source:** Issue #181
- **Supersedes:** —

## Context

The issue asks to verify openHAB's field names against 4.x and 5.x. The Product Manager supplied real responses of
openHAB 5.2.4 (apt package, Pi 5), with and without an API token; they stay outside the repository, and the tests
use anonymized excerpts. They show: system info nested under `systemInfo` with `freeMemory` and `totalMemory` in
bytes and no maximum heap; `uptime` of unknown unit; without a token, system info, Things and add-ons answer `401`
while the root call and the Item list answer, the Item list as nameless empty objects; offline Things with status
detail `NONE` and the reason in `description`; an add-on list of about 270 KB, sent chunked. No 4.x sample exists.

## Options considered

1. **Build from openHAB's documentation** — field names, units and the token rules would be guesses.
2. **Build from the 5.2.4 responses, leave out what they do not show** — chosen.
3. **Take uptime from `systemInfo.uptime`** — its unit is unverified (milliseconds or seconds).
4. **Take uptime from the process start time, `systemInfo.uptime` only without a process** — chosen: the
   process start time has a known unit and needs no token.

## Decision

The parsers follow the 5.2.4 responses: heap used is `totalMemory − freeMemory`, the heap maximum is not reported,
Items are counted by array length whatever the entries hold, and a Thing's `description` is reported beside its
status detail. Uptime comes from the process start time; `systemInfo.uptime` is used only when no process is found,
in the unit the Product Manager's measurement shows. The resulting fields are in [openHAB](../areas/openhab.md).

## Consequences

- An openHAB 4.x that differs from 5.2.4 shows the affected values as absent, never wrong: every field is
  optional. A 4.x report reopens this record.
- Until the unit of `systemInfo.uptime` is measured, uptime without a visible process is left out.
- A real sample read through an environment variable lets a maintainer check the parsers against a live install
  without committing it.
