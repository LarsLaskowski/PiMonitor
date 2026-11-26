# Plan: Read available apt updates from a stable source

Source: Issue #118 | [spec.md](spec.md)
Tier: standard — replaces the parser and the child process behind `updates` (a behavior change: unrecognised input now fails the check) without creating, changing or removing a security control; the fixed argument list and minimal environment stay as they are.
Security areas touched: Subprocesses (`internal/collector/updates.go`: `apt-cache dumpavail` replaces `apt list --upgradable`); Parsing of local input (new readers of `/var/lib/dpkg/status` and of `dumpavail` output)

## Problem / root cause

See [spec.md](spec.md), *Problem*. Today's skip of unmatched lines is `parseAptListUpgradable`
(`internal/collector/updates.go`, the `m == nil` branch).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | ------ | ------ |
| 1 | `apt list --upgradable` lists a package exactly when it has an installed version and its policy candidate is newer; no hold or kept-back filter | apt `main`: `apt-private/private-cacheset.cc` (`P.CurrentVer() && state.Upgradable()`), `apt-pkg/depcache.h` (`Status >= 1 && CandidateVer != NULL`), `depcache.cc` (`Status = Ver.CompareVer(Pkg.CurrentVer())`, candidate from `LocalPolicy->GetCandidateVer`); Bookworm's apt 2.6 by the manual check of AC1 | confirmed |
| 2 | `apt-cache dumpavail` prints, per package and architecture, the record of the policy candidate (else the installed version), from list files; for a candidate known only from the status file it prints that status record without `Status` | `cmdline/apt-cache.cc`, `DumpAvail` (`GetPolicy()->GetCandidateVer(P)`, `NotSource`, `Tag::Remove("Status")`), apt `main` | confirmed |
| 3 | apt treats a package as installed unless its status word is `not-installed` or `config-files`; the three Status words are validated against fixed lists | `apt-pkg/deb/deblistparser.cc`, `ParseStatus` (want: `unknown install hold deinstall purge`; flag: `ok reinstreq hold hold-reinstreq`; state: `not-installed config-files half-installed unpacked half-configured triggers-awaited triggers-pending installed`) | confirmed |
| 4 | On the Trixie arm64 sample, installed (status) ∩ newer candidate (dumpavail), `all` mapped to the native architecture and compared by the dpkg algorithm, equals `apt list --upgradable`: 28 of 28, none extra | Lead probe on the PM's samples (scratchpad, Python, not in the repository) | confirmed |
| 5 | Sample: 102,829 records, 88,662,148 bytes, longest line 75,649 bytes; every line is a field, a continuation or blank; all versions match `^([0-9]+:)?[0-9][A-Za-z0-9.+~-]*$` with no colon after the epoch; all names match `^[a-z0-9][a-z0-9.+-]+$`; architectures `all`, `arm64`, `armhf`; two `dpkg` records (arm64, armhf); 890 status records, all `install ok installed` | probe on the samples | confirmed |
| 6 | `apt-cache dumpavail` with empty lists prints nothing and exits 0 | run in this session (Ubuntu 24.04, apt 2.8.4, empty lists): 0 bytes, exit 0 | confirmed |
| 7 | `dumpavail` takes 0.325 s on the sample host | PM measurement (Trixie arm64); slower boards by the manual check of AC1 | confirmed |
| 8 | `/var/lib/dpkg/status` is readable inside `pimonitor.service` (`ProtectSystem=strict` only makes paths read-only; no `InaccessiblePaths`) | `packaging/pimonitor.service` lines 51–55; the run in the sandbox by the manual check | confirmed |
| 9 | The issue's claims about today's code (regex parsing, pinned locale, read-only collector) | `internal/collector/updates.go` (`docs/TESTS.md` is `docs/UNIT_TESTS.md`) | confirmed |
| 10 | Status records in `not-installed` state may lack `Version` and `Architecture` | to verify: not needed, the reader accepts both forms (hand-written fixture), Tester, step 5 | unverified |

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestFindUpgrades` (`AC1 Trixie arm64 sample matches apt list --upgradable`, `AC1 held package`, `AC1 foreign architecture installed`, `AC1 package installed for two architectures`, `AC1 all mapped to native architecture`, `AC1 candidate older than installed not reported`, `AC1 package in no list not reported`, `AC1 config-files and not-installed entries skipped`, `AC1 half-installed and unpacked count as installed`, `AC1 sorted by name then architecture`); `TestFindUpgrades_RealSample` (`AC1 …`, skips without `PIMONITOR_APT_SAMPLE_DIR`, reports counts only); `TestCompareDebianVersions` (`AC1 …` per dpkg vector: epoch, `~`, letters before non-letters, leading zeros, long digit runs, missing revision); `TestUpdatesCollector_Collect_PinsLocale` (adapted, `AC1`); manual: PM, see *Approach* | `internal/collector/updates_test.go` |
| AC2 | `TestFindUpgrades` (`AC2 no upgradable package`, `AC2 no dpkg record fails`, `AC2 dpkg record of a foreign architecture only fails`); `TestReadInstalled` (`AC2 no installed dpkg record fails`); `TestUpdatesCollector_Collect_EmptyDumpavailFails` (`AC2`) | `internal/collector/updates_test.go` |
| AC3 | `TestScanControlRecords` (`AC3 malformed line`, `AC3 duplicate field`, `AC3 continuation before a field`); `TestFindUpgrades` (`AC3 renamed fields fail`, `AC3 invalid version among valid records`, `AC3 truncated last record`, `AC3 duplicate package`); `TestReadInstalled` (`AC3 malformed Status`, `AC3 unknown status word`, `AC3 installed record without Version`); `TestParseDebianVersion` (`AC3 …` per invalid form: empty, letter first, bad character, colon after epoch, non-numeric or overflowing epoch, empty revision) | `internal/collector/updates_test.go` |
| AC4 | `TestUpdatesCollector_Collect_Failures` (`AC4 missing status file`, `AC4 missing apt-cache`, `AC4 apt-cache exits 1`, `AC4 timeout`, `AC4 output over the cap`, `AC4 line over 1 MiB`), each with its exact error text; `TestCollector_SlowTick_UpdatesFailureKeepsPreviousResult` (`AC4`: previous `Updates` and `checked_at` kept, warning `updates collection failed` logged; zero values before a first success) | `internal/collector/updates_test.go`, `internal/collector/collector_test.go` |
| AC5 | `TestUpdatesCollector_Collect_StateChangeFails` (`AC5`); `TestUpdatesCollector_Collect_Arguments` (`AC5`: stub records `dumpavail` as the only argument); `TestFindUpgrades_BoundedHeap` (`AC5`); `TestUpdatesCollector_Collect_StalenessDetection` (adapted, `AC5`); check: Reviewer — `updates.go` opens no file for writing, runs nothing but `apt-cache dumpavail`, takes no lock | `internal/collector/updates_test.go` |

## Approach

Early draft PR: no

**Collect** (signature unchanged), in this order: take `aptStateStamp` → open the status file and `readInstalled` →
start `apt-cache dumpavail` (fixed argument list, today's `cmd.Env` unchanged, `exec.CommandContext` with
`c.timeout`) and stream its stdout through `newCappedReader(stdout, c.maxOutput)` into `findUpgrades` → on any
parse error cancel the context, then `cmd.Wait` → take the stamp again; a differing stamp fails → build `Updates`
as today (count, packages, `CheckedAt`, cache age, stale). A zero `timeout` or `maxOutput` means the constants, so
struct literals in tests stay short.

**Readers.** `scanControlRecords` reads deb822 stanzas line by line with a `bufio.Scanner` whose buffer max is
`maxAptLineBytes` (1 MiB of raw bytes; no decoding happens). A line is a field (`Name: value`, name without space
or colon), a continuation (leading space or tab, after a field) or blank (ends a stanza); anything else, a
continuation before any field, or a duplicate field name fails. Only first-line values are kept, so one stanza costs
its field map. `readInstalled` validates each status record: `Package` (name regex of fact 5), `Status` (three words
from the lists of fact 3); a record in `not-installed` or `config-files` state is skipped, others need a valid
`Version` and `Architecture` (`^[a-z0-9][a-z0-9-]*$`) and go into the set keyed by name and architecture (`all` →
native). The native architecture is that of the installed `dpkg` record; none fails. `findUpgrades` validates every
`dumpavail` record the same way (Package, Version, Architecture required), fails on a duplicate key, keeps only
records whose key is installed, reports `name`, candidate `Version`, installed version, record `Architecture`
(`all` kept as `all`, as `apt list` prints it) when the candidate compares newer, and fails at the end unless a
`dpkg` record of the native architecture was seen (N7). The result is sorted by name, then architecture.

**Versions.** `parseDebianVersion`: epoch before the first colon (digits, fits `uint32`), revision after the last
hyphen (`[A-Za-z0-9.+~]`, non-empty when a hyphen is present), upstream non-empty, starting with a digit, from
`[A-Za-z0-9.+~-]`, no colon. `compareDebianVersions` compares epoch, then upstream and revision with dpkg's
`verrevcmp`: alternating non-digit runs (`~` before end before letters before other characters) and digit runs
compared after stripping leading zeros by length, then lexically (no integer overflow). To stay under `gocognit`
15 the alternation lives in `compareVersionPart`, with `compareNonDigits` and `compareDigits` per run.

**Bounds and their seam.** Child output: `cappedReader` returns `errOutputTooLarge` once more than `maxOutput`
bytes (256 MiB) arrive; the test sets `maxOutput` small. Lines: the scanner limit, tested with a 1 MiB + 1 line in
a status fixture. Memory: the installed set and the result only; `TestFindUpgrades_BoundedHeap` feeds a generated
64 MiB reader of not-installed records and reads `runtime.MemStats.HeapAlloc` in the generator's last `Read`,
which must stay below 16 MiB.

**Consistency (E5).** `aptStateStamp` holds the newest list-file mtime (shared with `aptCacheAge` through
`newestListModTime`) and the status file's mtime and size, as Unix nanoseconds; an unreadable part is zero. The
test's stub `apt-cache` rewrites the status file with a new mtime before printing.

**Error texts** (exact, compared by the tests; no input content, so nothing from a file reaches the log):
`read dpkg status: <err>`; `dpkg status: record N: <reason>` and `apt-cache dumpavail: record N: <reason>` (N from 1)
with reason one of `malformed line`, `duplicate field`, `missing Package`, `missing Version`, `missing Architecture`,
`missing Status`, `invalid Package`, `invalid Version`, `invalid Architecture`, `malformed Status`,
`duplicate package`; `dpkg status: line exceeds 1 MiB` and `apt-cache dumpavail: line exceeds 1 MiB`;
`dpkg status has no installed dpkg record`; `apt-cache dumpavail has no dpkg record for the native architecture`;
`apt-cache dumpavail output exceeds 256 MiB`; `run apt-cache dumpavail: <err>`;
`apt or dpkg state changed during the check`.

**Fixtures** (`internal/collector/testdata/apt/`, read by the tests): `trixie-arm64-status.txt` (the 28 upgradable
packages, about 20 others, `dpkg`), `trixie-arm64-dumpavail.txt` (their records, the armhf `dpkg` record and some
not-installed packages), `trixie-arm64-upgradable.txt` (the oracle lines) — whole records trimmed from the PM's
samples. The Tester checks every field for private values (host names, addresses, user names, keys; maintainer
addresses are public) and replaces any whole; held, foreign-architecture, rc and half-installed cases are
hand-written in the tests, derived from these records.

**Manual check (AC1, before step 9).** The PM builds the branch head (`make build-arm64`, `make build-arm` for
armhf), runs it as `pimonitor.service` on a Bookworm Pi (armhf if available) with one package on hold
(`apt-mark hold`) and one pin in `/etc/apt/preferences.d` that holds a package back, and compares
`GET /api/v1/metrics/updates` with `apt list --upgradable`: same names, versions, architectures and `count`, and the
check's duration in the journal context below 10 s. A mismatch is a blocking finding for the Dev.

## Options considered

The source: see [decision 0028](../../docs/decisions/0028-apt-updates-from-dumpavail-and-dpkg-status.md),
*Options considered* (six options; `apt-get -s`, reading the lists, `apt-cache policy`, binary cache/libraries,
status quo lost). Within option 1: the missing-`dpkg` signal against "at least one record" — one record proves no
list of the base repository was read; it lost.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| `internal/collector` | `updates.go` | rewritten: new reader, child process, bounds, stamp |
| `internal/collector` | `updates_test.go`, `collector_test.go`, `testdata/apt/` | tests and fixtures |

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
- `func scanControlRecords(r io.Reader, visit func(index int, fields map[string]string) error) error`
- `func readInstalled(r io.Reader) (installedSet, error)`
- `func findUpgrades(r io.Reader, installed installedSet) ([]PackageUpdate, error)`
- `func newCappedReader(r io.Reader, limit int64) io.Reader`
- `type stateStamp struct { listsNewest int64; statusModTime int64; statusSize int64 }`
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
  bounds, the failure cases and that a failure keeps the last result with its `checked_at` (zero: never checked).
- `docs/areas/deployment.md (+3 words)`: the child process list (line 86) names `apt-cache dumpavail`.

## Documentation updates

Dev, once the code is green:

- `docs/areas/metrics.md`, `docs/areas/deployment.md`: as *Areas*; `metrics.md` links decision 0028.
- `SECURITY.md` lines 41–42 (reads the dpkg status file and the apt cache via `apt-cache dumpavail`), 103, 107–108
  (`apt` → `apt-cache`).
- `README.md` line 98: the command named.
- `docs/ARCHITECTURE.md` lines 519 and 526: the read-only command is `apt-cache dumpavail`, linking decision 0028.
- `docs/CONTRIBUTING.md` line 170: the example shell-out.
- `packaging/pimonitor.service` line 38 and `packaging/pimonitor.example.yaml` line 23: comments only.
- `.squad/project.md (+4 words)`, *Subprocesses*: `(apt list --upgradable)` → `(apt-cache dumpavail)`; *Parsing of
  local input*: add `/var/lib/dpkg/status`, `apt-cache` output — exact text reported to the orchestrator.
- `.squad/stack.md` line 133 (+1 word): example fixture `apt-cache dumpavail` output and the dpkg status file — exact
  text reported to the orchestrator.
- `docs/UNIT_TESTS.md`: none (no new double). `docs/API.md`: none (shape and meaning unchanged); `docs/ARCHITECTURE.md`
  line 122 stays true.

## Architecture check

The unprivileged/root split holds: the service still only reads, `apt-cache` takes no lock and cannot write the
read-only cache. Stable v1 shapes: unchanged. No fabricated reading: strengthened (failure instead of 0). No new
dependency: standard library only.

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
