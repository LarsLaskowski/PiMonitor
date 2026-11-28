# Plan: Refresh apt before the service starts, and repeat the apt check once after a state change

Source: Issue #292
Tier: standard — a behavior change of the apt check (a repeat after a state change) and a reorder of existing `install.sh` steps; neither creates, changes or removes a security control.
Security areas touched: Subprocesses (`internal/collector/updates.go` may run `apt-cache dumpavail` a second time per check; its arguments, environment and bounds stay unchanged); Privilege separation (`packaging/install.sh`, run as root: the same `systemctl` commands in a new order, no unit, user or permission change)

## Problem / root cause

`UpdatesCollector.Collect` (`internal/collector/updates.go:108-144`) takes a fingerprint before reading the dpkg
status file (line 120), streams `apt-cache dumpavail`, and returns `errors.New("apt or dpkg state changed during the
check")` when the fingerprint differs afterwards (lines 129-131). Nothing repeats the check: `slowTick`
(`internal/collector/collector.go:877-889`) logs `updates collection failed` and sets `check_status` to `failed`
until the next slow tick. `install.sh` restarts `pimonitor.service` (line 105) and then starts
`pimonitor-apt-update.service` (line 113) at once, while `Run` runs the first slow tick right after the first fast
tick (`collector.go:374-386`), so the first check overlaps `apt-get update` on every install and upgrade. The same
overlap can happen, rarely, when the 6-hourly timer runs during a slow tick.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | The check fails on any fingerprint difference and is not repeated | `updates.go:120-131`; `collector.go:877-889` | confirmed |
| 2 | `install.sh` runs `apt-get update` right after restarting the service | `packaging/install.sh:100-115`: restart of `pimonitor.service`, restart of the timer, then `systemctl start pimonitor-apt-update.service`; also on a first install (line 100, then 113). Whether the timer restart alone fires it (the issue's claim) does not matter, line 113 does | confirmed |
| 3 | The first check runs within about a second of the start | `Run`: `loadHistory`, `collectSysInfo`, `fastTick`, `slowTick` (`collector.go:365-386`); the issue's journal shows start of the refresh and the failure in the same second | confirmed |
| 4 | "Until the next slow tick, which can be hours away" | `updates_check_minutes` default 15, range 1 to 10080 (`docs/areas/configuration.md:87`): 15 min by default, hours only when configured | refuted |
| 5 | The 10 s bound covers one `Collect` call: one `context.WithTimeout` (line 117), the child killed at the deadline, plus `cmd.WaitDelay` 1 s (line 173) | `updates.go:109-118,173` | confirmed |
| 6 | The slow tick blocks the fast tick; the `/healthz` default bound budgets only fast-tick overhead (`3 × poll + 2 × WorstCaseTickOverhead`) | `collector.go:399-415`, `config.go:384-389`, `collector.go:182`; `docs/areas/metrics.md:26-28`; so the repeat must not lengthen the slow tick's worst case | confirmed |
| 7 | `runDumpavail` calls the shared `cancel` only on a parse error; a state-change failure comes only after a successful run, so the shared context is still live for a repeat | `updates.go:182-198` | confirmed |
| 8 | A timer-run refresh ends within the 2 s pause, so the repeat sees the new state (the issue's journal: `Starting` 11:23:19, `Finished` 11:23:20, 1 s resolution; the unit runs with `Nice=19`, `IOSchedulingClass=idle`, so a slow SD card can take longer) | to verify: the Product Manager, after merge, from the journal of a timer run that happens to overlap a check (not schedulable; the install case no longer rests on it, AC6) | unverified |
| 9 | The existing test `TestUpdatesCollector_Collect_StateChangeFails` breaks under the repeat: case 1 appends `# changed` to the status file, which the repeat's `readInstalled` rejects as `malformed line`; case 2's `mv` finds no source on the second run, so the repeat succeeds | `updates_test.go:986-1021`, `controlParser.line` (`updates.go:395-398`), read | confirmed |
| 10 | `errStateChanged` is a string constant of the test file, so the new sentinel needs another name | `updates_test.go:31` | confirmed |
| 11 | `systemctl start pimonitor-apt-update.service` (`Type=oneshot`) returns only after the run ends, also when the timer restart just before it already started one (the start job waits on the activating unit) | to verify: AC5, the Product Manager reads in the journal `Finished pimonitor-apt-update.service` before `pimonitor` starts (`packaging/pimonitor-apt-update.service:6`) | unverified |
| 12 | On an upgrade the old `pimonitor` process keeps running during the moved refresh; a failed check of the old process is replaced when the restart begins at `not_checked` | `install.sh:84-110`; *Available apt updates* (`check_status` not persisted) | confirmed |
| 13 | `install.sh` needs root (`EUID` check, line 20), so no test can run it in CI; no other doc states the step order than `docs/areas/deployment.md` steps 7-8 (README only lists what is installed) | `install.sh:20-23`; `README.md:211-216`, swept for the refresh | confirmed |

## Acceptance criteria

The error text compared is `apt or dpkg state changed during the check` (unchanged).

- AC1: When the state changes during the first attempt only, `Collect` waits `updatesRetryDelay` once, runs the
  check a second time and returns that attempt's result (no error, `Count` and `Packages` of the repeat,
  `CheckedAt` set); `apt-cache` runs exactly twice.
- AC2: When the state changes during the repeat too, `Collect` returns the state-change error and zero `Updates`;
  `apt-cache` runs exactly twice, never a third time.
- AC3: Any other failure of the first attempt (a failing or timed-out `apt-cache`, unrecognised input, an unreadable
  status file) is returned as today, without a pause and without a second run.
- AC4: The pause and the repeat run under the deadline the first attempt started: a cancellation or expired
  deadline during the pause ends `Collect` at once with the state-change error and no second run, and no second
  timeout is created for the repeat.
- AC5: After the release, an upgrade with `install.sh` on the reporter's Pi shows no `updates collection failed`
  in `journalctl -u pimonitor`, `Finished pimonitor-apt-update.service` before the new `pimonitor` process starts,
  and `updates.check_status` `ok` after the first check.
- AC6: `packaging/install.sh` runs `systemctl start pimonitor-apt-update.service` after enabling (and, when it was
  active, restarting) `pimonitor-apt-update.timer`, and before `systemctl enable --now pimonitor.service` and the
  restart of `pimonitor.service`; each of these five commands appears once, and a failed refresh still only warns.

## Tests per criterion

All stubs count their runs in a file (`echo x >> runs`) and the tests inject `retryAfter`, recording each duration;
no test waits on the real clock. A state change the repeat must accept is one the parsers still read (a new list
file, a touched list), never a line appended to the status file (fact 9).

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestUpdatesCollector_Collect_StateChangeRepeated` / `AC1 change during the first run only` (stub adds a list file on its first run only; `retryAfter` returns a closed channel; asserts 2 runs, one recorded duration equal to `updatesRetryDelay`, `Count == aptSampleUpgradeCount`) | `internal/collector/updates_test.go` |
| AC2 | `TestUpdatesCollector_Collect_StateChangeFails`, adapted: every case changes the state on every run (a new list file per run, a status change the parser accepts), `AC2` in each subtest name, asserts the error text, zero `Updates` and exactly 2 runs | `internal/collector/updates_test.go` |
| AC3 | `TestUpdatesCollector_Collect_OtherFailureNotRepeated` / `AC3 apt-cache exits 1`, `AC3 empty dumpavail output` (assert 1 run, no `retryAfter` call) | `internal/collector/updates_test.go` |
| AC4 | `TestUpdatesCollector_Collect_StateChangeRepeated` / `AC4 cancel during the pause` (`retryAfter` cancels the caller's context and returns a nil channel; asserts the state-change error, 1 run) and `AC4 deadline expires during the pause` (small `timeout`, as the existing `AC4 timeout` case does; `retryAfter` returns a channel that never fires; asserts the state-change error, 1 run); the single deadline: check, the Reviewer reads that `Collect` calls `context.WithTimeout` once, before the first attempt | `internal/collector/updates_test.go` |
| AC5 | after merge: the Product Manager upgrades the Pi with `install.sh` from the first release with the fix and checks the journal and `/api/v1/metrics/updates` (linked verification issue, step 10) | — |
| AC6 | `TestInstallScript_RefreshBeforeServiceStart_AC6`: reads `packaging/install.sh` (`\r\n` normalized as `timerLines` does), skips comment lines, asserts each of the five `systemctl` lines once and their order, and that the `start pimonitor-apt-update.service` line stays inside `if ! …; then` with the warning | `internal/packaging/install_test.go` |

Smoke substitute (`.squad/stack.md`, *Writing tests*): the script needs root and systemd (fact 13), so a stub run is
not possible; the Tester runs `bash -n packaging/install.sh` and the *Analyzer gate* runs `shellcheck` on it. CI-only
rows: none; what only a real Pi shows is AC5. Target host: nothing new beyond today's (systemd, `apt-get`, the
mirrors it reaches); *Shell procedures* (a)-(d) do not apply, `install.sh` is a script under `set -euo pipefail`,
not pasted lines.

## Approach

- `Collect` keeps its timeout and output defaults and its one `context.WithTimeout`. Its current body from the
  fingerprint on (lines 120-143) moves unchanged into the private `collectOnce(ctx context.Context, cancel
  context.CancelFunc, maxOutput int64) (Updates, error)`, which returns `errAptStateChanged` on the fingerprint
  difference.
- `Collect` calls `collectOnce`; on an error that is not `errors.Is(err, errAptStateChanged)`, or on success, it
  returns the result as is. Otherwise it waits in `select` on `ctx.Done()` (return the first error, no repeat) and
  on `wait(updatesRetryDelay)`, where `wait` is `c.retryAfter` or `time.After` when that is nil, then returns a
  second `collectOnce` with the same `ctx`, `cancel` and `maxOutput`: its result or error, whatever it is.
- Bound: the pause and the repeat count against the one 10 s deadline, so the worst case of a slow tick stays 10 s
  plus `WaitDelay` (fact 6): a repeat follows only an attempt that finished before the deadline, and a pause cut by
  the deadline returns. Seam for the tests: `retryAfter` and the existing `timeout` field.
- `updatesTimeout`'s comment says it bounds one check, its repeat included. No new log line; the warning of
  `slowTick` is unchanged. No `/api/v1` shape change.
- `packaging/install.sh` (Dev): after `systemctl daemon-reload`, enable `pimonitor-apt-update.timer` (restart it
  when it was active), then run the blocking refresh with its warning as today, then enable and start
  `pimonitor.service` (restart it when it was active). The `*_WAS_ACTIVE` checks stay before `daemon-reload`. The
  echo lines may be reworded; the one before the refresh says that `pimonitor.service` (re)starts only after it and that an interrupted run is completed with `sudo systemctl restart pimonitor.service`.
- New user-visible output: the installer's echo lines only, layout free for the Dev, approved in step 9.

Early draft PR: no

## Options considered

The seven options of [0037](../../docs/decisions/0037-apt-state-change-repeats-check-once.md): refreshing in
`install.sh` before the service starts (chosen, with the repeat: it ends the install overlap whatever the refresh
takes, the repeat covers the timer); the reorder alone (lost: the timer overlap keeps failing for a slow interval);
a paused single repeat under the one deadline (chosen); an immediate repeat (the issue's proposal: lost, the repeat would mostly fall
inside the same refresh); a fresh 10 s bound for the repeat (lost: doubles the slow tick's worst-case delay of the
fast tick); repeating until stable (lost: unbounded runs per tick); an extra early slow tick (lost: `failed` still
shows until it runs, a third schedule in `Run`); also repeating a failed attempt whose state changed (lost: no such
failure observed, and it would repeat genuine format errors).

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `updates.go`: `UpdatesCollector`, `Collect`, new `collectOnce`, `errAptStateChanged`, `updatesRetryDelay` | repeat once after a state change |
| `internal/collector` | `updates_test.go` | new and adapted tests (Tester) |
| `packaging` | `install.sh` | refresh before the service starts (Dev) |
| `internal/packaging` | `install_test.go` | new order test (Tester) |

## Signatures (for the Dev's skeleton)

- `var errAptStateChanged = errors.New("apt or dpkg state changed during the check")`
- `const updatesRetryDelay = 2 * time.Second`
- New field of `UpdatesCollector`: `retryAfter func(time.Duration) <-chan time.Time` (nil: `time.After`), set by tests.
- `func (c *UpdatesCollector) Collect(ctx context.Context) (Updates, error)` — unchanged signature.

`NewUpdatesCollector` leaves `retryAfter` nil; the skeleton holds the fields and declarations for real, so no test
is deferred.

## Test files

- `internal/collector/updates_test.go`
- `internal/packaging/install_test.go`

Existing test code that calls a changed signature: none (no signature changes). The Tester adapts
`TestUpdatesCollector_Collect_StateChangeFails` in step 5 (fact 9: its inputs are handled differently now).
`collector_test.go`'s `UpdatesCollector{aptCachePath: "/nonexistent/apt-cache", …}` fails before any fingerprint
comparison and stays as is.

Files of the change no unit test can cover: none.

## Areas

- `docs/areas/metrics.md (+45 words)` (3366 words now) — *Available apt updates*: the 10 s bound is for the whole check, the repeat
  included; a change of the status file or the lists fails the check only when it shows again in the one repeat
  that follows a 2 s pause, linked to 0037.
- `docs/areas/deployment.md (+35 words)` (1620 words now) — *`install.sh`*
  steps 7-8: reload, enable/restart the timer, run the refresh and wait (so the service's first check does not
  overlap it), then enable/restart `pimonitor.service`; 0037 added under *Related decisions*.

## Documentation updates

- `docs/areas/metrics.md` (Dev) — as under *Areas*: the first bullet's "a **10 s** timeout" and the sentence of the
  third bullet on a change while the check ran.
- `docs/areas/deployment.md` (Dev) — as under *Areas*, steps 7 and 8 rewritten in the new order.
- `docs/decisions/0037-apt-state-change-repeats-check-once.md` (Lead, done).
- No changelog file exists; the PR carries its release-notes label (bug fix). `docs/API.md`, `README.md`,
  `docs/ARCHITECTURE.md`, `SECURITY.md`: no change (swept for `dumpavail`, `state change`, `10 s`, the slow tick and
  the installer's refresh, fact 13).

## Architecture check

The fail-closed guarantee (no fabricated reading, `.squad/project.md`) holds: a count is only reported from an
attempt whose fingerprint did not change. The slow tick's worst-case duration and so the `/healthz` behavior stay
as they are (fact 6). The privilege split holds: `install.sh` runs the same root-only commands as today, in a new
order; `pimonitor.service` stays unprivileged. `install.sh` still never overwrites the config. No route or v1 shape
changes.

## Security considerations

- [x] No limits: the 256 MiB and 1 MiB caps apply per attempt as today; at most two `apt-cache` children per check,
  one at a time.
- [x] No new failure reason reaches a user: the texts logged by `slowTick` are those of today.
- [x] A refresh that hangs now delays the start of the new binary instead of running after it; an interrupted run
  leaves the old process running with the new binary on disk until the next restart (0037, *Consequences*).

## Decision records

- `docs/decisions/0037-apt-state-change-repeats-check-once.md` (Proposed), amends 0028.

## Challenge

- **Major 1 (reorder `install.sh`)** — accepted. The refresh now runs before `pimonitor.service` is (re)started
  (AC6, *Approach*), so the install and upgrade case no longer rests on the 2 s pause (fact 8 now concerns only the
  timer); the repeat stays for the 6-hourly timer overlap. Added: the option and its consequences in 0037, the
  test, `docs/areas/deployment.md`, facts 11-13, the security area *Privilege separation*, and the target-host and
  smoke-substitute statement under *Tests per criterion*.
- **Minor 1 (expired deadline)** — accepted: subtest `AC4 deadline expires during the pause`.
- **Minor 2 (AC5 not a guarantee)** — accepted in the reorder's form: 0037 says the install case rests on the order
  and the oneshot start waiting (fact 11), and that the repeat does not make a timer overlap certain to pass on a
  slow host.

## Security review

- `APPROVED`, no findings, on the plan before this revision. This revision adds the area *Privilege separation*
  (`install.sh`), which that review did not see.

## Out of scope / follow-ups

- Option 6 of 0037 (repeating a failed attempt whose state changed): a follow-up only if a journal shows an
  `apt-cache` failure coinciding with the refresh.

## Challenge notes

Devil's Advocate, plan challenge for specs/issue-292 (plan: /home/user/PiMonitor/specs/issue-292/plan.md, decision: /home/user/PiMonitor/docs/decisions/0037-apt-state-change-repeats-check-once.md). There is one major objection and two minor ones.

I checked the plan's factual claims against the code and they hold:
- **Facts 1, 5, 6 and 7:** `updates.go:108-144` and `updates.go:159-198`; `slowTick` at `collector.go:877-889`; the order in `Run` at `collector.go:365-386`.
- **Fact 2:** `packaging/install.sh:100-115`.
- **Facts 9 and 10:** `updates_test.go:986-1021` and line 31. The "# changed" line is rejected as `malformed line` by `controlParser.line` (`updates.go:388-398`).
- **No other caller:** `slowTick` (`collector.go:878`) is the only caller of `UpdatesCollector.Collect`.

On fact 2: `pimonitor-apt-update.timer` has `OnUnitActiveSec=6h`. So restarting the timer can also fire the refresh at once if the last run was more than 6 h ago. The plan is right that line 113 is enough on its own.

### Major

**1. The fix for the reported scenario depends on a timing nobody has checked, and a fix that does not depend on timing was not considered.**

The case the issue reports, an upgrade with `install.sh`, is fixed only if `apt-get update` finishes within the 2 s pause. The plan itself marks this as unverified (fact 8), and AC5 can only check it after the release.

- **Why this is likely to fail on some hosts:** the refresh runs with `Nice=19` and `IOSchedulingClass=idle` (`packaging/pimonitor-apt-update.service`). On a slow SD card, a busy board, a slow mirror or a host with many sources it can easily take longer than 2 s.
- **The margin is thin even on the reporter's host.** The failure is logged at 11:23:19 and `Finished` at 11:23:20, at 1 s resolution. So the refresh may end up to about 2 s after the failure, and the repeat starts exactly 2 s after it.
- **The alternative not considered:** reorder `packaging/install.sh` so the refresh runs before the service's first check. The order would be: restart the timer, then the blocking `systemctl start pimonitor-apt-update.service`, then `restart`/`enable --now` of `pimonitor.service`. It is a `Type=oneshot` unit, so `systemctl start` waits until the refresh ends. It also joins a run the timer restart already started. The overlap is then gone on every install and upgrade, whatever the refresh takes. Today, lines 100 and 105 start the service before line 113.
- **Neither document considers it.** 0037's six options and the plan's *Options considered* leave this out. Yet it fixes the reported case without lengthening the slow tick at all.
- **What to change:**
  - Add the reorder as an option in 0037.
  - Either adopt it, alone or together with the retry (the retry still covers the 6-hourly timer overlap), or record why it loses.
  - If adopted, the plan grows by: the `install.sh` change, `internal/packaging` tests for the order, the `docs/areas/deployment.md` *install.sh* step list (lines 37-60), and the target-host list that *Operator environments* in `.squad/routing.md` requires.

### Minor

**1. AC4's test covers only a cancelled context, not an expired deadline.** AC4 promises that "a cancellation or expired deadline during the pause" ends `Collect` with the state-change error and no second run. The only test listed is `AC4 cancel during the pause`. The Tester should add a subtest, e.g. `AC4 deadline expires during the pause`. It sets the existing `timeout` field small, and `retryAfter` returns a channel that never fires. It asserts the state-change error and exactly one `apt-cache` run, without waiting on the real clock beyond that small timeout. The plan's error text in the AC2 assertions stays as it is.

**2. 0037's *Consequences* should say plainly that the retry does not make the install-path fix certain.** The current text says a refresh longer than the pause "still leaves `failed` until the next slow tick". It should also name the `install.sh` upgrade as the case where this applies when the host is slow, so the Product Manager's AC5 check is not read as a guarantee for all hosts. If major 1 adopts the reorder, this point goes away.

## Security notes

Security plan review, round 2 (delta confirmation): specs/issue-292/plan.md and docs/decisions/0037-apt-state-change-repeats-check-once.md at 62d6f9b, branch claude/intelligent-mayer-7yggb1

I approve the revision; it has one non-blocking finding. The repeat-once in `internal/collector/updates.go` is unchanged since my round-1 approval, so I reviewed only the `install.sh` reorder and the new *Privilege separation* area.

**Tier.** `standard` still holds, and *Security areas touched* is now complete. The `systemctl` commands and the `*_WAS_ACTIVE` checks stay the same. `packaging/pimonitor-apt-update.service` (root, its hardening and capability set), `pimonitor-apt-update.timer` and `pimonitor.service` (unprivileged) are untouched, and no user, permission or config handling changes (plan *Approach* and *Architecture check*). Moving lines only changes timing, and no control decides anything differently. The binary is still installed early (`install.sh:56`), as it is today.

**The four delta points:**
- **Refresh failure handling:** a failed refresh still only warns, because the `if ! systemctl start pimonitor-apt-update.service; then … warning` stays (`install.sh:113-115`, pinned by AC6). The service is still enabled or restarted after a failed refresh, so the new binary still takes effect. A held apt lists lock makes `apt-get update` fail rather than wait, which takes this same warn path.
- **Timer order:** enabling the timer before the blocking start is correct for the order guarantee. If `enable --now` fires the oneshot at once (its `OnBootSec=10min` has long passed), `systemctl start` merges with that running start job and waits for it to finish. Either way the service starts after the refresh. I did not run this: no systemd here. I took it from systemd's documented job semantics; it bears on the fix working, not on a security property.
- **Hanging refresh and interrupted installer:** the plan and 0037 *Consequences* accept both. For `Type=oneshot`, systemd disables the start timeout by default (documented in systemd.service; man page not installed here, so not checked), so a slow or stuck refresh can block the installer for as long as `apt-get`'s own network timeouts allow. Re-running `install.sh` recovers: `SERVICE_WAS_ACTIVE=1` leads to a restart. See the finding.
- **New `set -e` abort point:** a failing `systemctl enable --now pimonitor-apt-update.timer` now aborts before `pimonitor.service` is enabled or restarted; today it aborts after. This is the same "old process, new binary" outcome as an interrupt, and it is visible because "Done." never prints. Nothing to change beyond the finding.

**AC6 test:** it reads `packaging/install.sh` as text only and runs nothing as root. There is no security concern.

**Finding:**

1. [NON-BLOCKING] Plan *Approach*, `packaging/install.sh` bullet ("The echo lines may be reworded; one says the refresh runs before the service starts").
   - **Defect:** with the reorder, an upgrade only takes effect after a refresh that has no time bound (oneshot). A release that ships a security fix stays inactive while the refresh runs. An operator who interrupts the installer with Ctrl-C leaves the old, possibly vulnerable process running. Interrupting `systemctl start` does not cancel the systemd job, so the refresh finishes in the background, but the service is never restarted. Nothing on screen tells them so.
   - **Smallest fix:** make the echo before the refresh also say that the new version only starts after it, and how to apply it if the run is interrupted. Plan wording to replace that sentence of the bullet: "The echo lines may be reworded; the one before the refresh says that `pimonitor.service` (re)starts only after it and that an interrupted run is completed with `sudo systemctl restart pimonitor.service`."
   - Recommendation: fix now
