# 0037: install.sh refreshes apt before it starts the service, and a changed apt state repeats the check once

- **Status:** Accepted
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
The same overlap can happen, rarely, when the 6-hourly timer runs during a check. The refresh runs with `Nice=19`
and idle I/O priority, so its length varies with the SD card and the mirrors. The slow tick runs in the same
goroutine as the fast tick and delays it by as long as it runs ([Metrics](../areas/metrics.md#collection-cadence));
0028 kept the check's 10 s bound for that reason. The fail-closed rule (no count from a state that changed under the
check) stays.

## Options considered

1. **`install.sh` runs its blocking refresh before it (re)starts `pimonitor.service`** — the service's first check
   starts after the refresh, however long that takes, on every install and upgrade; no Go change. Leaves the timer
   overlap, and a hanging refresh now delays the start of the new binary.
2. **Repeat the check once after a short pause, only after a state change, under the check's one 10 s deadline** —
   covers a timer overlap whose refresh ends within the pause, and the worst case of a slow tick stays what it is
   today. Costs a pause that delays the fast tick by up to 2 s more when the state changed; a longer refresh still
   fails the check.
3. **Repeat at once, without a pause** (the issue's proposal) — no added delay, but `dumpavail` took 0.3 s on the
   measured host, so the repeat would mostly fall inside the same `apt-get update` and fail again.
4. **Each attempt with its own 10 s bound** — a slow board keeps its full budget for the repeat, but a slow tick
   could then delay the fast tick by twice as long as today, against the reason 0028 kept the bound.
5. **Repeat until the state stays unchanged, within the bound** — covers a longer refresh, but runs `apt-cache`
   an unbounded number of times per tick for one more case the bound mostly cuts off anyway.
6. **Schedule an extra slow tick soon after a state-change failure** — never delays the fast tick, but
   `check_status` still shows `failed` until it runs, and it adds a third schedule to `Run` that also re-runs the
   Pi-hole and process collection.
7. **Also repeat a failed attempt (a failing `apt-cache`, unrecognised output) when the state changed during it**
   — would cover an `apt-cache` that trips over lists replaced while it reads, but no such failure has been
   observed, and the repeat would then also cover genuine format errors that happen to coincide with a refresh.

## Decision

Options 1 and 2: `install.sh` enables the timer and waits for its refresh before it enables or restarts
`pimonitor.service` ([Deployment](../areas/deployment.md#installsh)). When an attempt of the check ends with the
state-change failure, the check waits a fixed pause and runs once more, both under the deadline the first attempt
started; any other failure, a second state change, or the deadline or a shutdown during the pause fails the check as
before ([Metrics](../areas/metrics.md#available-apt-updates)).

## Consequences

- An install or upgrade no longer overlaps its own refresh, whatever the refresh takes; this rests on
  `systemctl start` of the oneshot unit waiting for a run the timer restart may already have started, and is checked
  once on a real upgrade after the release.
- The repeat does not make a timer overlap certain to pass: a refresh that outlasts the 2 s pause (a slow SD card, a
  slow mirror, many sources) still leaves `failed` until the next slow tick; revisit with option 6 when that is
  reported.
- On an upgrade the old process runs on during the refresh; a hanging refresh delays the new binary's start, and an
  interrupted installer leaves the old process running with the new binary on disk until the next restart. The
  installer says so before the refresh and names the recovery, `sudo systemctl restart pimonitor.service`; a re-run
  of `install.sh` recovers too.
- On a board where one `dumpavail` takes most of the 10 s, the repeat runs out of time and fails closed with the
  timeout; such a board was already at risk under 0028.
- A successful repeat is not logged; only a failure that persists logs `updates collection failed`.
