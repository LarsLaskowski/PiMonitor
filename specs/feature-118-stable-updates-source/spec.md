# Spec: Read available apt updates from a stable source

Source: Issue #118
Status: Draft

## Problem

Operators read the number and list of available apt updates in `updates` (`GET /api/v1/metrics`,
`GET /api/v1/metrics/updates`, the dashboard, `pimonitor_updates_pending` in `GET /metrics`). PiMonitor gets them by
parsing the human-readable output of `apt list --upgradable` (`internal/collector/updates.go`), an interface `apt`
itself declares unstable for scripts. Issue #100 pinned the locale, but a future `apt` release that changes the line
format still breaks the count, and silently: a line the parser does not recognise is skipped, so a changed format
reports `count: 0` and an empty package list as if the host were up to date — a fabricated reading, which the project
promises never to give ([Metrics](../../docs/areas/metrics.md#limits-and-errors)). The operator who trusts
"0 updates" misses security updates.

## Scenarios

- S1: Given a host with upgradable packages in its apt cache, when the slow tick checks for updates, then `updates`
  lists exactly the packages `apt list --upgradable` lists on that host, each with `name`, `new_version`,
  `old_version` and `arch` as today, and `count` is their number — on every supported release, whatever the
  operator's locale.
- S2: Given a host whose apt cache lists no upgradable package, when the slow tick checks, then `count` is 0 and
  `packages` is absent, as today.
- S3: Given an apt release whose update data PiMonitor cannot recognise (a changed format, an unknown structure), when
  the slow tick checks, then the check fails: `updates` keeps the last successful result, the warning
  `updates collection failed` is logged with the reason, and `count` never drops to 0 because of it.
- S4: Given any of the above, then `cache_age_seconds`, `stale` and `checked_at` behave as today
  ([Metrics](../../docs/areas/metrics.md#available-apt-updates)), and the JSON shape of `updates` is unchanged.

## Errors and edge cases

- E1: The update source is missing, unreadable or exceeds the time bound (N4): failed check as in S3, the last result
  kept; before the first success `updates` holds zero values, as today.
- E2: A package on hold, one that `apt upgrade` would keep back (it needs a new dependency or a removal), one whose
  candidate is chosen by an apt pin, or one with several architectures installed: it is in `packages` exactly when
  `apt list --upgradable` on the same host lists it, with the same versions and architecture.
- E3: A partly malformed or truncated source (one entry cut off, a field missing): failed check, never a list with the
  broken entry silently dropped, never a fabricated version.
- E4: A very large apt cache (several repositories, tens of thousands of available packages): the check still
  completes within N4.
- E5: Another process holds the apt or dpkg lock (an `apt-get update` by the root timer, an operator's
  `apt upgrade`): the check neither waits for nor takes the lock; it reports what the cache holds or fails as in E1.
- E6: A host without apt (off-Pi development): failed check as today, no crash.

## Non-functional requirements

- N1: Unprivileged and read-only: the check runs as the `pimonitor` user inside today's `pimonitor.service` sandbox,
  without new groups, capabilities or writable paths; it never writes to the apt or dpkg state, never refreshes the
  cache and never contacts the network ([Deployment](../../docs/areas/deployment.md)).
- N2: No new dependency: standard library only (`.squad/project.md`, *Guarantees*).
- N3: Independent of the operator's locale and environment: a child process, if any, keeps a fixed argument list and
  an explicit minimal environment (`SECURITY.md`, *Threat model*).
- N4: A check ends within 10 s, as today; its memory does not grow with the size of the apt cache beyond what one
  package entry needs.
- N5: Compatibility: the `/api/v1` shape and meaning of `updates`, `pimonitor_updates_pending`, the dashboard and the
  configuration (`updates_check_minutes`, `updates_stale_threshold_minutes`) are unchanged; no new option.
- N6: Supported targets: the apt versions of the current Raspberry Pi OS releases, Bookworm (Debian 12) and Trixie
  (Debian 13), both 32-bit (`armhf`) and 64-bit (`arm64`).

## Acceptance criteria

- AC1: For the real update data of a supported release (N6) with upgradable packages, including the cases of E2, the
  check reports the same packages, versions and architectures and the same `count` that `apt list --upgradable`
  reports on that host, and the result does not depend on the locale (S1, E2, N3, N6).
- AC2: For update data that lists no upgradable package, `count` is 0 and `packages` is absent (S2).
- AC3: Update data that cannot be recognised, is malformed or truncated, is missing, or is not available within 10 s
  makes the check fail with an error; the collector then keeps the last successful `updates` and logs
  `updates collection failed` (S3, E1, E3, E6, N4).
- AC4: The check uses no human-readable output of `apt` (the `apt` binary), needs no privilege, group or path beyond
  today's `pimonitor.service`, writes nothing, takes no apt or dpkg lock and adds no dependency (N1, N2, E5).
- AC5: `cache_age_seconds`, `stale`, `checked_at` and the JSON shape of `updates` are unchanged, and a large cache is
  checked within the bound of AC3 with memory per entry, not per cache (S4, N4, N5, E4).

## Out of scope

- Refreshing the apt cache, or any change to `pimonitor-apt-update.service`/`.timer`.
- Security-update classification, changelogs, phased updates or any new key in `updates`.
- Pi-hole updates (`pihole`), which do not come from apt.

## Open questions

- Which source replaces `apt list --upgradable` — `apt-get --just-print`/`-s` (the interface apt documents as stable
  for scripts), reading `/var/lib/dpkg/status` with the lists in `/var/lib/apt/lists`, or another? Answered: a design
  choice for the plan (*Options considered*), bound by AC1 and AC4; the issue leaves it open.
- What counts as an update? Answered: exactly what `apt list --upgradable` reports today on the same host (E2), so the
  number operators see does not change meaning (`.squad/project.md`, *Guarantees*: stable v1 meaning).
- Which apt releases must be supported? Answered: N6; the README names Raspberry Pi OS without a release, and Bookworm
  and Trixie are the releases in use.
- Where does the real update data for AC1 come from? Answered: the plan names the samples it needs (a Bookworm and a
  Trixie host, covering E2) once the source is chosen, and asks the Product Manager for them in mode `plan`
  (`.squad/routing.md`, *Escalation to the Product Manager*).

## Area documents

- `docs/areas/metrics.md`, *Available apt updates*: the new source, what counts as an update (the set of E2), the
  10 s bound, and that unrecognisable, malformed or missing data is a failed check that keeps the last result; the
  `apt list --upgradable` line format leaves it.
- `docs/areas/deployment.md`, *`pimonitor.service`*: the child processes the service starts and the paths it reads,
  in line with the new source.
