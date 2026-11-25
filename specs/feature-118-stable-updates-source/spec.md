# Spec: Read available apt updates from a stable source

Source: Issue #118
Status: Draft

## Problem

Operators read available apt updates in `updates` (API, dashboard, `pimonitor_updates_pending`). PiMonitor parses
the human-readable output of `apt list --upgradable`, which `apt` declares unstable for scripts (issue #118). A
future format change breaks the count silently: unrecognised lines are skipped, so the host looks up to date with
`count: 0` — a fabricated reading ([Metrics](../../docs/areas/metrics.md#limits-and-errors)), and the operator
misses security updates.

## Scenarios

- S1: Given upgradable packages in the apt cache, when the slow tick checks, then `updates` lists exactly what
  `apt list --upgradable` lists on that host (`name`, `new_version`, `old_version`, `arch`), `count` their number, in
  any locale.
- S2: Given a host whose apt cache lists no upgradable package, when the slow tick checks, then `count` is 0 and
  `packages` is absent, as today — only on a positive signal of a complete result (N7).
- S3: Given update data PiMonitor cannot recognise (a changed entry format, an unknown structure, no positive signal
  of N7), when the slow tick checks, then the check fails and the warning `updates collection failed` is logged with
  the reason. After a first success, `updates` keeps the last successful result, and its `checked_at` stays at the time
  of that success; before the first success it keeps the zero values with the zero `checked_at`, as today. The
  failure never itself produces `count: 0`.
- S4: In every case `cache_age_seconds`, `stale` and the JSON shape of `updates` stay as today
  ([Metrics](../../docs/areas/metrics.md#available-apt-updates)).

## Errors and edge cases

- E1: The update source is missing (a host without apt), unreadable, exceeds the time bound or a size cap (N4): failed check as in S3.
- E2: Each of these is in `packages` exactly when `apt list --upgradable` on the same host lists it, with the same
  versions and architecture (the plan says per case how its source handles it): a held package; a kept-back one; a
  candidate chosen by `/etc/apt/preferences(.d)` or `APT::Default-Release`; a `NotAutomatic`(+`ButAutomaticUpgrades`)
  repository; a foreign architecture (`dpkg --add-architecture`) or several installed architectures; a status entry
  not installed (`rc`, half-installed); an obsolete or local package in no list; a candidate older than the installed
  version; deb822 `.sources` or `.list` repositories; compressed lists (`Acquire::GzipIndexes`). A case the source
  cannot read within N2 (`.lz4`, `.xz`) is a failed check, never a skipped repository.
- E3: An entry the reader does not recognise, a malformed or truncated entry (one cut off, a field missing): the whole
  check fails, never a list with that entry dropped, never a fabricated version.
- E4: A very large apt cache (tens of thousands of packages): the check stays within N4.
- E5: Another process holds the apt or dpkg lock or changes the state during the check (an `apt-get update` by the
  root timer replacing list files, an operator's `apt upgrade`): the check neither waits for nor takes the lock; its
  result is one consistent snapshot of the state or a failed check, never a mix of old and new lists.

## Non-functional requirements

- N1: Unprivileged and read-only, inside today's `pimonitor.service` sandbox ([Deployment](../../docs/areas/deployment.md)):
  no new group, capability or writable path, no write to apt or dpkg state, no cache refresh, no network.
- N2: No new dependency: standard library only (`.squad/project.md`, *Guarantees*).
- N3: Allowed sources: the state files dpkg and apt document (`/var/lib/dpkg/status`, the `Packages` and `Release`
  files in `/var/lib/apt/lists`, apt's preferences and configuration), or the output of `apt-get` or `apt-cache`,
  the tools apt(8) names for scripts. Never the output of the `apt` binary. A child process keeps a fixed argument
  list and an explicit minimal environment with a pinned locale (`SECURITY.md`, *Threat model*), so the result does
  not depend on the operator's locale or environment.
- N4: Bounds: a check ends within 10 s, as today. A child process's output beyond 4 MiB, or a single line beyond
  1 MiB in any file or output the check reads, fails the check. Files are read as a stream, never whole into
  memory; the check holds at most the installed-package set and the result.
- N5: Compatibility: the `/api/v1` shape and meaning of `updates`, `pimonitor_updates_pending`, the dashboard and the
  configuration (`updates_check_minutes`, `updates_stale_threshold_minutes`) are unchanged; no new option.
- N6: Supported targets: the apt versions of the current Raspberry Pi OS releases, Bookworm (Debian 12) and Trixie
  (Debian 13), both 32-bit (`armhf`) and 64-bit (`arm64`).
- N7: A successful check rests on a positive structural signal of a complete result that the plan names for its
  source: for command output, the summary or marker line the tool always prints; for state files, a dpkg status
  file with at least one well-formed installed-package record, and every list file a configured repository needs
  present and readable.

## Acceptance criteria

- AC1: For the real update data of each supported release (N6) with upgradable packages, including the cases of E2,
  the check reports the same packages, versions and architectures and the same `count` that `apt list --upgradable`
  reports on that host, and the result does not depend on the locale (S1, E2, N3, N6).
- AC2: For update data that carries the positive signal of N7 and lists no upgradable package, `count` is 0 and
  `packages` is absent; the same data without that signal fails the check (S2, N7).
- AC3: Update data in the new source's syntax in which only the entry format changed, so that no entry matches,
  fails the check instead of reporting 0; so does data with one unrecognised, malformed or truncated entry among
  valid ones (S3, E3).
- AC4: A missing or unreadable source, a check not done within 10 s, a child output beyond 4 MiB or a line beyond
  1 MiB fails the check with an error; the collector then logs `updates collection failed` and keeps the previous
  `updates` with its `checked_at`, the zero values before the first success (S3, E1, E4, N4).
- AC5: The check reads only the sources N3 allows, needs no privilege, group or path beyond today's
  `pimonitor.service`, writes nothing, takes no apt or dpkg lock, adds no dependency, and returns one consistent
  snapshot or a failed check when the state changes during the check; `cache_age_seconds`, `stale` and the JSON shape
  of `updates` are unchanged (N1, N2, N3, N5, E5, S4).

## Out of scope

- Refreshing the apt cache or changing its timer.
- Security-update classification, changelogs, phased updates or any new key in `updates`.
- Pi-hole updates (`pihole`).
- A signal for "never checked" or "last check failed" beyond `checked_at` (S3): `pimonitor_updates_pending` reports 0
  before the first success and the dashboard shows that 0, as today; a follow-up issue proposes such a signal.

## Open questions

- Which source replaces `apt list --upgradable`? Answered: a design choice for the plan (*Options considered*) among
  those N3 allows, bound by AC1 and AC5; `apt-get -s upgrade` omits held and kept-back packages and `dist-upgrade`
  omits held ones and adds new dependencies, so the plan verifies its option against E2 with a real sample.
- What counts as an update, on which releases? Answered: what `apt list --upgradable` reports today (E2), keeping the
  v1 meaning, on the releases of N6 (the README names no release).
- Where does the real data for AC1 come from? Answered: in mode `plan` the Lead asks the Product Manager for samples
  from a Bookworm and a Trixie host — at least a held, kept-back, pinned and foreign-architecture package and the other
  E2 cases its source handles, with `apt list --upgradable` of the same host.

## Area documents

- `docs/areas/metrics.md`, *Available apt updates*: the source, E2's set, N7's signal, N4's bounds, failure keeping
  the last result, `checked_at` as the last success (zero: never checked); the `apt list` line format leaves it.
- `docs/areas/deployment.md`, *`pimonitor.service`*: the child processes and paths the service reads.

## Challenge

- M1 (no definition of "recognised"): accepted. N7 requires a positive structural signal per source; AC2 fails data
  without it; AC3 adds a fixture in the new source's syntax with only the entry format changed, which must fail, and
  E3/AC3 make one unrecognised entry fail the whole check.
- M2 ("count never drops to 0" false after a restart): accepted, scope narrowed. Verified: `writeUpdatesMetrics`
  (`internal/httpapi/prometheus.go`) emits `pimonitor_updates_pending` unconditionally, `app.js` shows
  `String(snap.updates.count)`, and `slowTick` (`internal/collector/collector.go`) keeps the previous `Updates` on
  error with its `stale` unchanged. S3/AC4 now promise only that a failure itself never produces 0 and that
  `checked_at` marks the last success (zero: never checked), which the metrics area document will state. A new signal
  (omitting the gauge before the first success, a new key) changes what existing consumers see and is not needed to
  replace the source: it goes to *Out of scope* with a proposed follow-up issue — title "Signal a never-run or
  failing apt updates check in the API, /metrics and the dashboard", body: before the first successful check
  `pimonitor_updates_pending` reports 0 and the dashboard shows 0; after a failed check the kept result looks current
  except for `checked_at`; propose omitting the gauge before the first success (as `writePiholeMetrics` does) and a
  documented new key under decision 0018.
- M3 (memory bound unmeasurable): accepted. N4 now caps a child's output at 4 MiB and any line at 1 MiB (both fail
  the check, tested by AC4), requires streaming reads of files and bounds held state by the installed set plus the
  result; the per-entry wording is gone.
- m1 (missing E2 cases): accepted; every listed case is in E2 and in the sample request, the oracle stays
  `apt list --upgradable` on the same host, and the plan says per case how its source handles it. Compression the
  standard library cannot read is a failed check (N2).
- m2 (`apt-get -s` and held/kept-back packages): accepted as a constraint on the plan's option choice (*Open
  questions*), with sample rows for a held and a kept-back package requested.
- m3 (AC4 too permissive; concurrent `apt-get update`): accepted. N3 names the allowed sources; E5/AC5 require one
  consistent snapshot or a failed check while the lists change.
