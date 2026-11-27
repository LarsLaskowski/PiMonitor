# 0028: Available apt updates come from `apt-cache dumpavail` and the dpkg status file

- **Status:** Accepted
- **Date:** 2026-11-26
- **Area:** Metrics
- **Source:** Issue #118
- **Supersedes:** —

## Context

`updates` was parsed from `apt list --upgradable`, whose output `apt` declares unstable for scripts; a changed line
format skipped every line and reported `count: 0` as if the host were up to date, a fabricated reading
(`.squad/project.md`, *Guarantees*). The replacement must report exactly what `apt list --upgradable` reports, stay
unprivileged and read-only, add no dependency, and fail rather than report 0 when it cannot recognise its input.
In apt's source (`apt-private/private-cacheset.cc`), `apt list --upgradable` lists every package with an installed
version whose policy candidate is newer (`Upgradable()`, `apt-pkg/depcache.h`): holds and kept-back packages are
included, a candidate older than the installed version is not.

## Options considered

1. **`apt-cache dumpavail` plus `/var/lib/dpkg/status`, compared by the Debian version algorithm** — apt prints
   the policy candidate of every package in the `Packages` record format (`DumpAvail`, `cmdline/apt-cache.cc`), so
   pins, `Default-Release`, `NotAutomatic`, compressed lists and deb822 sources stay apt's job; the status file is
   dpkg's documented database. On the Trixie arm64 sample the comparison reproduces `apt list --upgradable` exactly
   (28 of 28). Costs a hand-rolled version comparison and a stream of about 90 MB per check.
2. **`apt-get -s upgrade` / `dist-upgrade`** — the scripting tool apt recommends, but its `Inst` lines are resolver
   output: `upgrade` leaves out held and kept-back packages, `dist-upgrade` leaves out held ones and adds new
   dependencies, so it reports a different set.
3. **Reading `/var/lib/apt/lists` directly** — no child process, but candidate selection (preferences, `apt.conf`,
   `NotAutomatic`, compression) would be reimplemented; every divergence is a fabricated reading.
4. **`apt-cache policy <names>`** — apt's candidates per package, but the installed package names would become
   arguments, against the fixed-argument-list rule.
5. **`pkgcache.bin`, libapt or python-apt** — an internal binary format, a cgo or Python dependency.
6. **Keep `apt list --upgradable`** — the problem of the issue.

## Decision

Option 1: the collector reads the installed set from the dpkg status file, streams `apt-cache dumpavail`, and
reports each installed package whose candidate version is newer. Any record it cannot recognise, a missing `dpkg`
record on either side, or a change of the apt or dpkg state during the check fails the check
([Metrics](../areas/metrics.md#available-apt-updates)).

## Consequences

- A future change of the `Packages` or status format fails the check visibly instead of reporting 0.
- Manual check (Product Manager, Trixie arm64 only): the API output equals `apt list --upgradable` (33 of 33);
  `apt-cache dumpavail` took 0.322 s, `apt list --upgradable` 0.547 s. No armv6 board, no Bookworm host and no hold
  or pin were available; those checks are waived as accepted risks: the Bookworm apt (2.6) and armhf were not
  sampled (the rule was read in apt's current source), holds and pins are covered by unit tests only.
- The 10 s bound stays unchanged on every architecture. `dumpavail` was faster than the `apt list --upgradable` it
  replaces on the one measured host and used about 3 % of the bound, so a board that misses 10 s would come close
  to it with the old source as well. A longer bound lost because the slow tick delays the fast tick by as long as it
  runs ([Metrics](../areas/metrics.md)); keeping `apt list --upgradable` on armv6 lost because it keeps the defect
  of the issue there and adds a second code path. Accepted risk: on armv6 neither `dumpavail` nor the streamed
  parse of its output was measured; a board that exceeds 10 s fails closed (failed check, last result kept, warning
  logged), never reports a wrong count. Revisit when such a failure is reported.
- The consistency check assumes apt gives a downloaded list the server's `Last-Modified` as mtime (so a newer list
  can look older) and moves finished lists in by `rename`; it therefore also compares the lists directory's mtime
  and the count and total size of list files, not only the newest mtime.
- Each check streams the whole `dumpavail` output (88 MB on the sample) instead of a short list.
- Revisit if apt offers a machine-readable upgradable list (a JSON output of `apt-get` or `apt-cache`).
