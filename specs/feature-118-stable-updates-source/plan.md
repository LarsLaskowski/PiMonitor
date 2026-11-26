# Plan: Read available apt updates from a stable source

Source: Issue #118 | [spec.md](spec.md)
Tier: standard — replaces the parser and the child process behind `updates` (a behavior change: unrecognised input now fails the check) without creating, changing or removing a security control; the fixed argument list and minimal environment stay as they are.
Security areas touched: Subprocesses (`internal/collector/updates.go`: `apt-cache dumpavail` replaces `apt list --upgradable`); Parsing of local input (new readers of `/var/lib/dpkg/status` and of `dumpavail` output)

## Problem / root cause

[spec.md](spec.md), *Problem*; the skip is the `m == nil` branch of `parseAptListUpgradable`.

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | ------ | ------ |
| 1 | `apt list --upgradable` = installed and policy candidate newer, holds and kept-back included; `dumpavail` prints the policy candidate per package and architecture; apt counts every status state but `not-installed` and `config-files` as installed (Status word lists in *Approach*) | apt `main` source, files and functions in [0028](../../docs/decisions/0028-apt-updates-from-dumpavail-and-dpkg-status.md) *Context*; Bookworm's apt 2.6 by the manual check | confirmed |
| 2 | The approach reproduces `apt list --upgradable` on the Trixie arm64 sample: 28 of 28, none extra | Lead probe on the PM's samples (scratchpad, not in the repository) | confirmed |
| 3 | Sample: 102,829 records, 88,662,148 bytes, longest line 75,649 bytes, both files end with a blank line; every line a field, continuation or blank; versions match `^([0-9]+:)?[0-9][A-Za-z0-9.+~-]*$`, names `^[a-z0-9][a-z0-9.+-]+$`; architectures `all`, `arm64`, `armhf`; two `dpkg` records; 890 status records, all `install ok installed` | probe on the samples | confirmed |
| 4 | `apt-cache dumpavail` with empty lists prints nothing and exits 0 | this session, apt 2.8.4 | confirmed |
| 5 | `dumpavail` takes 0.325 s on the sample host | PM (Trixie arm64); armv6 by the manual check | confirmed |
| 6 | `/var/lib/dpkg/status` is readable in `pimonitor.service` (no `InaccessiblePaths`) | `packaging/pimonitor.service` lines 51–55 | confirmed |
| 7 | apt sets a list's mtime from `Last-Modified`; `rename` sets the directory mtime | to verify: not checkable from the samples; documented assumption in 0028, covered by the stamp's count, size and directory mtime | unverified |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestFindUpgrades` (`AC1 Trixie arm64 sample matches apt list --upgradable`, `AC1 held package`, `AC1 foreign architecture installed`, `AC1 package installed for two architectures`, `AC1 all mapped to native architecture`, `AC1 candidate older than installed not reported`, `AC1 package in no list not reported` (absent from the dumpavail fixture), `AC1 status record of the installed candidate parsed, not reported` (Status removed, `Conffiles:` with an empty value and continuation lines), `AC1 architecture changes all to native`, `AC1 architecture changes native to all`, `AC1 config-files and not-installed entries skipped`, `AC1 half-installed and unpacked count as installed`, `AC1 sorted by name then architecture`); `TestFindUpgrades_RealSample` (`AC1 …`, skips without `PIMONITOR_APT_SAMPLE_DIR`, reports counts only); `TestCompareDebianVersions` (`AC1 …` per dpkg vector: epoch, `~`, letters before non-letters, leading zeros, long digit runs, missing revision); `TestUpdatesCollector_Collect_PinsLocale` (adapted, `AC1`); manual: PM, see *Approach* | `internal/collector/updates_test.go` |
| AC2 | `TestFindUpgrades` (`AC2 no upgradable package`, `AC2 no dpkg record fails`, `AC2 dpkg record of a foreign architecture only fails`); `TestReadInstalled` (`AC2 no installed dpkg record fails`); `TestUpdatesCollector_Collect_EmptyDumpavailFails` (`AC2`) | `internal/collector/updates_test.go` |
| AC3 | `TestScanControlRecords` (`AC3 malformed line`, `AC3 duplicate field`, `AC3 continuation before a field`, `AC3 truncated record`; and `AC1 empty field value accepted`); `TestFindUpgrades` (`AC3 renamed fields fail`, `AC3 invalid version among valid records`, `AC3 truncated last record`, `AC3 duplicate package`); `TestReadInstalled` (`AC3 malformed Status`, `AC3 unknown status word`, `AC3 installed record without Version`); `TestParseDebianVersion` (`AC3 …` per invalid form: empty, letter first, bad character, colon after epoch, non-numeric or overflowing epoch, empty revision) | `internal/collector/updates_test.go` |
| AC4 | `TestUpdatesCollector_Collect_Failures` (`AC4 missing status file`, `AC4 missing apt-cache`, `AC4 apt-cache exits 1`, `AC4 timeout`, `AC4 output over the cap`, `AC4 line over 1 MiB`), each with its exact error text; the stubs of `AC4 apt-cache exits 1` and `AC4 timeout` first print the complete valid dumpavail fixture (then exit 1, or sleep past a 50 ms `timeout`); `TestCollector_SlowTick_UpdatesFailureKeepsPreviousResult` (`AC4`: previous `Updates` and `checked_at` kept, warning `updates collection failed` logged; zero values before a first success) | `internal/collector/updates_test.go`, `internal/collector/collector_test.go` |
| AC5 | `TestUpdatesCollector_Collect_StateChangeFails` (`AC5 status file rewritten`, `AC5 list file replaced by one with an older mtime`, `AC5 list file added`); `TestUpdatesCollector_Collect_Arguments` (`AC5`: stub records `dumpavail` as the only argument); `TestFindUpgrades_BoundedHeap` (`AC5`); `TestUpdatesCollector_Collect_StalenessDetection` (adapted, `AC5`); check: Reviewer — `updates.go` opens no file for writing, runs nothing but `apt-cache dumpavail`, takes no lock | `internal/collector/updates_test.go` |

## Approach

Early draft PR: no

**Collect** (signature unchanged), in this order: take `aptStateStamp` → open the status file and `readInstalled` →
start `apt-cache dumpavail` (fixed argument list, today's `cmd.Env` unchanged, `exec.CommandContext` with
`c.timeout`) and stream its stdout through `newCappedReader(stdout, c.maxOutput)` into `findUpgrades` → on any
parse error cancel the context; always call `cmd.Wait`, and a non-nil `Wait` error or `ctx.Err()` fails the check
even when `findUpgrades` returned without error → take the stamp again; a differing stamp fails → build `Updates`
as today (count, packages, `CheckedAt`, cache age, stale). A zero `timeout` or `maxOutput` means the constants, so
struct literals in tests stay short.

**Readers.** `scanControlRecords` reads deb822 stanzas line by line with a `bufio.Scanner` whose buffer max is
`maxAptLineBytes` (1 MiB of raw bytes; no decoding happens). A line is a field (`Name:` and a value that may be
empty, name without space or colon), a continuation (leading space or tab, after a field) or blank (ends a stanza);
anything else, a continuation before any field, a second `Package`, `Version`, `Architecture` or `Status` in one
stanza, or input ending inside a stanza (no closing blank line; both samples end with one) fails. Only those four
first-line values are kept, in one reused `controlRecord` value (no map, no per-stanza allocation besides the four
strings); every other field is checked for syntax only. `readInstalled` validates each status record: `Package` (name regex of fact 3), `Status` (want `unknown install hold deinstall purge`, flag `ok reinstreq hold hold-reinstreq`,
state `not-installed config-files half-installed unpacked half-configured triggers-awaited triggers-pending installed`); a record in `not-installed` or `config-files` state is skipped, others need a valid
`Version` and `Architecture` (`^[a-z0-9][a-z0-9-]*$`) and go into the set keyed by name and architecture (`all` →
native). The native architecture is that of the installed `dpkg` record; none fails. `findUpgrades` validates every
`dumpavail` record the same way (Package, Version, Architecture required), builds its lookup key with the same
`all` → native mapping, fails on a duplicate key, keeps only records whose key is installed, reports `name`, candidate `Version`, installed version, record `Architecture`
(the candidate record's raw value, `all` kept as `all`, as `apt list` prints it) when the candidate compares newer, and fails at the end unless a
`dpkg` record of the native architecture was seen (N7). The result is sorted by name, then architecture.

**Versions.** `parseDebianVersion`: epoch before the first colon (digits, fits `uint32`), revision after the last
hyphen (`[A-Za-z0-9.+~]`, non-empty when a hyphen is present), upstream non-empty, starting with a digit, from
`[A-Za-z0-9.+~-]`, no colon. `compareDebianVersions` compares epoch, then upstream and revision with dpkg's
`verrevcmp`: alternating non-digit runs (`~` before end before letters before other characters) and digit runs
compared after stripping leading zeros by length, then lexically (no integer overflow). To stay under `gocognit`
15 the alternation lives in `compareVersionPart`, with `compareNonDigits` and `compareDigits` per run.

**Bounds and their seam.** Child output: `cappedReader` returns `errOutputTooLarge` once more than `maxOutput`
bytes (256 MiB) arrive; the test sets `maxOutput` small. Lines: the scanner limit, tested with a 1 MiB + 1 line in
a status fixture. Memory: the installed set and the result only; `TestFindUpgrades_BoundedHeap` feeds a reader
that repeats one pre-built not-installed record up to 64 MiB and in its last `Read` calls
`runtime.GC()` and then reads `runtime.MemStats.HeapAlloc`, which must stay below 8 MiB, an eighth of the input.

**Consistency (E5).** `aptStateStamp` holds the mtime of the `/var/lib/apt/lists` directory, the number and total
size of its files, the newest list-file mtime (shared with `aptCacheAge` through `newestListModTime`) and the status
file's mtime and size, as Unix nanoseconds and bytes; an unreadable part is zero. Assumption (documented in 0028, not
checked on the sample host): apt sets a downloaded list's mtime from the server's `Last-Modified`, so a replaced list
can carry an older mtime; `apt-get update` moves finished lists in by `rename`, which sets the directory's mtime to
local time. The tests' stub `apt-cache` changes the state before printing: rewrites the status file, renames a list
file with an older mtime over an existing one, or adds a list file.

**Error texts** (exact, compared by the tests; no input content, so nothing from a file reaches the log):
`read dpkg status: <err>`; `dpkg status: record N: <reason>` and `apt-cache dumpavail: record N: <reason>` (N from 1)
with reason one of `malformed line`, `duplicate field`, `missing Package`, `missing Version`, `missing Architecture`,
`missing Status`, `invalid Package`, `invalid Version`, `invalid Architecture`, `malformed Status`,
`duplicate package`, `truncated record`; `dpkg status: line exceeds 1 MiB` and `apt-cache dumpavail: line exceeds 1 MiB`;
`dpkg status has no installed dpkg record`; `apt-cache dumpavail has no dpkg record for the native architecture`;
`apt-cache dumpavail output exceeds 256 MiB`; `run apt-cache dumpavail: <err>`;
`apt or dpkg state changed during the check`.

**Fixtures** (`internal/collector/testdata/apt/`, read by the tests): `trixie-arm64-status.txt` (the 28 upgradable
packages, about 20 others, `dpkg`), `trixie-arm64-dumpavail.txt` (their records, the armhf `dpkg` record and some
not-installed packages, one status-style record of an installed candidate as m3 of *Challenge* says; packages only in
the status file are left out), `trixie-arm64-upgradable.txt` (the oracle lines) — whole records trimmed from the PM's
samples. The Tester checks every field for private values (host names, addresses, user names, keys; maintainer
addresses are public) and replaces any whole; held, foreign-architecture, rc and half-installed cases are
hand-written in the tests, derived from these records.

**Manual check (AC1, before step 9).** The PM builds the branch head (`make build-arm64`; `make build-arm` for
armv6/armhf), runs it as `pimonitor.service` on a Bookworm Pi with one package on hold (`apt-mark hold`) and one pin
in `/etc/apt/preferences.d` that holds a package back, and compares `GET /api/v1/metrics/updates` with
`apt list --upgradable`: same names, versions, architectures and `count`. On an armv6 board (Pi Zero/1, `make
build-arm`) the PM also runs, as the `pimonitor` user, `time apt-cache dumpavail | wc -c` and, as the baseline,
`time apt list --upgradable`, and records both times in `log.md`; the journal must show no `updates collection
failed`. A mismatch is a blocking finding for the Dev. **Fallback:** if `dumpavail` takes more than 5 s (half the
bound) on armv6, or no armv6 board is available, the Lead decides in mode `decide` with the numbers: amend N4 to a
longer bound for this check alone (it delays the fast tick by as long, `docs/areas/metrics.md`), or escalate keeping
`apt list --upgradable` on armv6; the decision goes into 0028.

## Options considered

See [0028](../../docs/decisions/0028-apt-updates-from-dumpavail-and-dpkg-status.md), *Options considered*. For N7,
"at least one record" lost to the `dpkg` record: one record does not prove the base repository's lists were read.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `updates.go`; tests and `testdata/apt/` | rewritten; tests and fixtures |

## Signatures (for the Dev's skeleton)

In `internal/collector/updates.go`:

- `const dpkgStatusPath = "/var/lib/dpkg/status"`, `const maxAptOutputBytes int64 = 256 << 20`, `const maxAptLineBytes = 1 << 20`, `const updatesTimeout = 10 * time.Second`
- `type UpdatesCollector struct { aptCachePath string; statusPath string; listsDir string; staleThreshold time.Duration; timeout time.Duration; maxOutput int64; now func() time.Time }` (field `aptPath` is removed)
- `func NewUpdatesCollector(staleThreshold time.Duration) *UpdatesCollector` (unchanged; sets `aptCachePath: "apt-cache"`, `statusPath: dpkgStatusPath`)
- `func (c *UpdatesCollector) Collect(ctx context.Context) (Updates, error)` (unchanged)
- `type pkgKey struct { name string; arch string }`
- `type installedSet struct { native string; versions map[pkgKey]debianVersion }`
- `type debianVersion struct { raw string; epoch uint32; upstream string; revision string }`
- `func parseDebianVersion(v string) (debianVersion, error)`
- `func compareDebianVersions(a, b debianVersion) int`
- `func compareVersionPart(a, b string) int`
- `type controlRecord struct { index int; pkg string; version string; arch string; status string }`
- `func scanControlRecords(r io.Reader, visit func(rec controlRecord) error) error`
- `func readInstalled(r io.Reader) (installedSet, error)`
- `func findUpgrades(r io.Reader, installed installedSet) ([]PackageUpdate, error)`
- `func newCappedReader(r io.Reader, limit int64) io.Reader`
- `type stateStamp struct { listsDirModTime int64; listsCount int; listsSize int64; listsNewest int64; statusModTime int64; statusSize int64 }`
- `func aptStateStamp(listsDir, statusPath string) stateStamp`
- `func newestListModTime(listsDir string) (time.Time, error)`
- `var errOutputTooLarge error` (its text is in *Approach*, *Error texts*)
- `func aptCacheAge(listsDir string, now time.Time) (time.Duration, error)` (unchanged signature, body uses `newestListModTime`)
- Deleted: `parseAptListUpgradable` and `packageUpdateLine`.

The per-run helpers of the version comparison and the capped reader's type are private (*Approach*). The skeleton
holds nothing for real; the constructor already exists.

## Test files

`internal/collector/updates_test.go`, `internal/collector/collector_test.go`, fixtures under
`internal/collector/testdata/apt/`.

Existing call sites: `TestParseAptListUpgradable*` are deleted (Tester); `TestUpdatesCollector_Collect_StalenessDetection`
and `TestUpdatesCollector_Collect_PinsLocale` move to `aptCachePath`, a status fixture and a stub printing a
dumpavail fixture (their `true` stub now fails, AC2) — Tester; `collector_test.go` lines 590–593, 1050, 1069:
`aptPath` → `aptCachePath` (failure intended, comment updated) — Tester. `writeFakeVcgencmd` serves as the stub
`apt-cache`; no new test double.

Files no unit test can cover: none.

## Areas

- `docs/areas/metrics.md (+150 words)`: *Available apt updates* rewritten — the two sources, what counts as an update
  (installed, candidate newer; holds and kept-back included), the `all` mapping, sorting, the `dpkg` signal, the
  bounds, the failure cases (line 176: `apt` → `apt-cache`, its parse failures included) and that a failure keeps
  the last result with its `checked_at` (zero: never checked).
- `docs/areas/deployment.md (+3 words)`: the child process list (line 86) names `apt-cache dumpavail`.

## Documentation updates

Dev, once the code is green:

- `docs/areas/metrics.md`, `docs/areas/deployment.md`: as *Areas*; `metrics.md` links decision 0028.
- `SECURITY.md` lines 41–42 (reads the dpkg status file and the apt cache via `apt-cache dumpavail`), 103, 107–108
  and 127 (`apt` → `apt-cache`).
- `README.md` line 98: the command named.
- `docs/ARCHITECTURE.md` lines 519 and 526: the read-only command is `apt-cache dumpavail`, linking decision 0028.
- `docs/CONTRIBUTING.md` line 170: the example shell-out.
- `packaging/pimonitor.service` lines 38 and 65 and `packaging/pimonitor.example.yaml` line 23: comments only (`apt` → `apt-cache`).
- `.squad/project.md (+4 words)`, *Subprocesses*: `(apt list --upgradable)` → `(apt-cache dumpavail)`; *Parsing of
  local input*: add `/var/lib/dpkg/status`, `apt-cache` output — exact text reported to the orchestrator.
- `.squad/stack.md` line 133 (+1 word): example fixture `apt-cache dumpavail` output and the dpkg status file — exact
  text reported to the orchestrator.
- `docs/UNIT_TESTS.md`, `docs/API.md`: none.

## Architecture check

Unprivileged/root split, stable v1 shapes and no new dependency hold; the no-fabricated-reading guarantee is
strengthened (failure instead of 0).

## Security considerations

- [x] Limits: 1 MiB per line and 256 MiB per child output, both on raw bytes (nothing is decoded); 10 s per check.
- [x] Failure reasons reaching the log: the texts in *Approach*, plus wrapped `os` and `exec` errors (`read dpkg status:`, `run apt-cache dumpavail:`), which carry a path or exit status but no input content.

## Decision records

- `docs/decisions/0028-apt-updates-from-dumpavail-and-dpkg-status.md` (Proposed)

## Out of scope / follow-ups

Follow-up issue for the orchestrator to create — title "Signal a never-run or failing apt updates check in the API,
/metrics and the dashboard"; body: "Before the first successful check `pimonitor_updates_pending` reports 0 and the
dashboard shows 0; after a failed check the kept result looks current except for `checked_at`. Propose omitting the
gauge before the first success, as `writePiholeMetrics` does, and a documented new key under decision 0018."

## Challenge

- M1 (10 s bound unverified on armv6): accepted. The manual check now includes an armv6 board (`make build-arm`)
  with `dumpavail` and `apt list --upgradable` timed and logged, and names the fallback (more than 5 s, or no
  board: Lead `decide` between a longer bound for this check and keeping `apt list` on armv6). The reader keeps
  only the four needed values in one reused `controlRecord` (no per-stanza map); duplicates are detected for those
  four fields, other fields are checked for syntax only — a duplicate of an unused field cannot change the result.
- M2 (newest mtime misses a replaced older list): accepted. The stamp adds the lists directory's mtime and the count
  and total size of list files; the `Last-Modified` behavior is a documented assumption in 0028 (the samples carry
  no `ls` output). Tests added: a list file replaced by an older-mtime one, a list file added.
- m1 (missed mentions): accepted; `SECURITY.md` line 127, `packaging/pimonitor.service` line 65 and
  `docs/areas/metrics.md` line 176 are in *Documentation updates* / *Areas*.
- m2 (`all` in dumpavail lookup): accepted; both readers map `all` to native for the key, the raw candidate value is
  reported, and tests cover both directions of an architecture change.
- m3 (DumpAvail edge records): accepted; packages only in the status file are absent from the dumpavail fixture, a
  status-style record (no `Status`, `Conffiles:` with an empty value and continuations) must parse and not be
  reported, and the grammar accepts empty values.
- m4 (heap test): accepted; `runtime.GC()` before `ReadMemStats`, a cheap repeated 64 MiB input, bound 8 MiB.

## Security review

- S1 (Wait and truncation): accepted; `cmd.Wait` always runs and its error or `ctx.Err()` fails the check,
  `truncated record` is an error text, and the stubs of `AC4 apt-cache exits 1` and `AC4 timeout` first print the
  complete valid fixture.
- S2 (heap read): accepted; the generator calls `runtime.GC()` before reading `HeapAlloc` (*Bounds and their seam*).
