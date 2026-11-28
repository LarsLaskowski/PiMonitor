# 0037: A changed apt or dpkg state repeats the apt check once, within the same 10 s bound

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Metrics
- **Source:** Issue #292
- **Supersedes:** 0028 (amends it: a change of the apt or dpkg state during the check fails the check only when the
  one repeat sees a change too)

## Context

The apt check compares a fingerprint of the apt lists and the dpkg status file before and after `apt-cache
dumpavail` and fails on any difference ([0028](0028-apt-updates-from-dumpavail-and-dpkg-status.md)). `install.sh`
restarts `pimonitor.service` and then runs `pimonitor-apt-update.service` (`apt-get update`) at once, so the
service's first check overlaps the refresh and `updates.check_status` reads `failed`
([0029](0029-apt-check-status-key-and-prometheus-gap.md)) until the next slow tick, 15 minutes later by default.
The slow tick runs in the same goroutine as the fast tick and delays it by as long as it runs
([Metrics](../areas/metrics.md#collection-cadence)); 0028 kept the check's 10 s bound for that reason. The
fail-closed rule (no count from a state that changed under the check) stays.

## Options considered

1. **Repeat the check once after a short pause, only after a state change, under the check's one 10 s deadline** —
   the repeat starts after the refresh that the journal of the issue shows finishing within about a second, and
   the worst case of a slow tick stays what it is today. Costs a pause that delays the fast tick by up to 2 s more
   when the state changed, and a refresh that outlasts the pause still fails the check.
2. **Repeat at once, without a pause** (the issue's proposal) — no added delay, but `dumpavail` took 0.3 s on the
   measured host, so the repeat would mostly fall inside the same `apt-get update` and fail again.
3. **Each attempt with its own 10 s bound** — a slow board keeps its full budget for the repeat, but a slow tick
   could then delay the fast tick by twice as long as today, against the reason 0028 kept the bound.
4. **Repeat until the state stays unchanged, within the bound** — covers a longer refresh, but runs `apt-cache`
   an unbounded number of times per tick for one more case the bound mostly cuts off anyway.
5. **Schedule an extra slow tick soon after a state-change failure** — never delays the fast tick, but
   `check_status` still shows `failed` until it runs, and it adds a third schedule to `Run` that also re-runs the
   Pi-hole and process collection.
6. **Also repeat a failed attempt (a failing `apt-cache`, unrecognised output) when the state changed during it**
   — would cover an `apt-cache` that trips over lists replaced while it reads, but no such failure has been
   observed, and the repeat would then also cover genuine format errors that happen to coincide with a refresh.

## Decision

Option 1: when an attempt ends with the state-change failure, the check waits a fixed pause and runs once more,
both under the deadline the first attempt started; any other failure, a second state change, or the deadline or a
shutdown during the pause fails the check as before ([Metrics](../areas/metrics.md#available-apt-updates)).

## Consequences

- An upgrade with `install.sh` on the reporter's host is expected to show `check_status: ok` right away; this rests
  on the refresh finishing within the pause and is checked once on a real upgrade after the release.
- A refresh that takes longer than the pause (a slow mirror, many sources) still leaves `failed` until the next slow
  tick; revisit with option 5 when that is reported.
- On a board where one `dumpavail` takes most of the 10 s, the repeat runs out of time and fails closed with the
  timeout; such a board was already at risk under 0028.
- A successful repeat is not logged; only a failure that persists logs `updates collection failed`.
