# Plan: Drop the ineffective Persistent= from the apt timer and verify downloads

Source: Issue #204
Tier: security — `download-latest.sh` gains an integrity check on the release archive whose `install.sh` the operator then runs as root (trust of the release); in doubt the higher tier.
Security areas touched: Privilege separation (`packaging/pimonitor-apt-update.timer`, the root timer); release download integrity (`packaging/download-latest.sh`, not yet listed in `.squad/project.md`: added by this change)

## Problem / root cause

1. `packaging/pimonitor-apt-update.timer:7` sets `Persistent=true` beside `OnBootSec=10min` (line 5) and
   `OnUnitActiveSec=6h` (line 6); systemd applies `Persistent=` only to `OnCalendar=` timers, so the line is dead.
2. `packaging/download-latest.sh:73-77` downloads the archive and runs `tar xzf` on it without any integrity check,
   although every release publishes `checksums.txt` (`.goreleaser.yaml:37-38`).

## Verified facts

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | `Persistent=` only has an effect on `OnCalendar=` timers | `systemd/systemd` v255 `man/systemd.timer.xml:335-343`: "Note that this setting only has an effect on timers configured with OnCalendar=" | confirmed |
| 2 | The timer has `OnBootSec=10min`, `OnUnitActiveSec=6h`, `Persistent=true`, no `OnCalendar=` | `packaging/pimonitor-apt-update.timer:5-7` | confirmed |
| 3 | The script never checks the archive; `deployment.md` says so | `download-latest.sh:73-77`; `docs/areas/deployment.md` *Limits and errors*, last sentence of the `download-latest.sh` bullet | confirmed |
| 4 | Real `checksums.txt` format: one line per archive, `<64 lower-case hex>` two spaces `<file name>`, LF endings, no other files listed | `checksums.txt` of release v1.3.0 read with `od -c`: `bf6b049895294f4301c8d8188b7db2bc5bd88e37edfc4f6a6bf0e46c4d06d29a  pimonitor_1.3.0_linux_arm64.tar.gz` and `9ec3e35fd5fda91cef6d8fbc1e22c783a3cfe4f1b9496d6fe7564df16e004e73  pimonitor_1.3.0_linux_armv6.tar.gz` | confirmed |
| 5 | `sha256sum` output matches that format for the real archive | `sha256sum pimonitor_1.3.0_linux_armv6.tar.gz` of the downloaded v1.3.0 asset printed `9ec3e35f…e004e73` (GNU coreutils 9.4) | confirmed |
| 6 | The release builds exactly two archives, `linux_arm64` and `linux_armv6`; the script accepts exactly `arm64` and `armv6` | `.goreleaser.yaml:12-17`; v1.3.0 assets (`gh api …/releases/latest`): `checksums.txt` and the two archives; `download-latest.sh:36-43` | confirmed |
| 7 | `checksums.txt` lives at `https://github.com/<repo>/releases/download/<tag>/checksums.txt`, beside the archive | v1.3.0 `browser_download_url` of the asset | confirmed |
| 8 | Every file under `packaging/` ships in the release archive, so a test there would ship too | `.goreleaser.yaml:31-33` (`src: packaging/*`) | confirmed |
| 9 | A directory with only `_test.go` files passes `go build ./...`, `go vet ./...` and runs under `go test ./...` | scratch module experiment: build=0, vet=0, `ok x/b` | confirmed |
| 10 | golangci-lint accepts the test-only package without findings | to verify: Code Officer, *Analyzer gate* in step 7 | unverified |
| 11 | CI's `ubuntu-latest` runner has `bash`, `tar`, `gzip`, `awk`, `sha256sum` | to verify: the green `go test` job of the draft PR (step 11); the test fails, not skips, on Linux when a tool is missing | unverified |
| 12 | `systemd-analyze verify` checks the two units in the session | `systemd-analyze verify ./pimonitor-apt-update.timer ./pimonitor-apt-update.service` in `packaging/` printed nothing (systemd 255) | confirmed |
| 13 | No document besides `README.md` and `docs/areas/deployment.md` describes the script or `Persistent=` | `grep -rn "download-latest\|checksums\|Persistent"` outside `.git`, `specs/` | confirmed |
| 14 | `sha256sum` comes from `coreutils`, an Essential package, on Debian-family systems | `dpkg-query -W -f '${Essential}' coreutils` printed `yes`, `dpkg -S /usr/bin/sha256sum` printed `coreutils` (Ubuntu 24.04, coreutils 9.4) | confirmed |
| 15 | Raspberry Pi OS (Debian Bookworm based) ships `sha256sum` the same way | source: Product Manager on a Raspberry Pi — `dpkg -S /usr/bin/sha256sum` printed `coreutils: /usr/bin/sha256sum`, `dpkg-query -W -f '${Essential}' coreutils` printed `yes` | confirmed |

## Acceptance criteria

- AC1: `packaging/pimonitor-apt-update.timer` has no `Persistent=` line; `OnBootSec=10min` and `OnUnitActiveSec=6h`
  are unchanged and the units pass `systemd-analyze verify`.
- AC2: `download-latest.sh` downloads `checksums.txt` of the chosen release before the archive, verifies the archive
  with `sha256sum`, and on each failure below exits with status 1, writes exactly the given line(s) to standard
  error, and leaves neither the extracted directory nor the archive behind (`<archive>` is the archive's file name,
  `<version>` the tag, e.g. `v1.3.0`):
  - `checksums.txt` cannot be downloaded: `Could not download checksums.txt of PiMonitor <version>.` — the archive
    is not requested;
  - no usable entry (*Approach*, accepted forms): `No checksum for <archive> in checksums.txt of PiMonitor <version>.`
    — the archive is not requested;
  - the computed hash differs: `Checksum mismatch for <archive>: expected <expected>, got <actual>.` then
    `Removed <archive>; nothing was extracted.`;
  - the archive download fails (curl exits non-zero, a partial file possibly written):
    `Could not download <archive> of PiMonitor <version>.` — the partial file is removed;
  - `sha256sum` is not on the `PATH`: the existing `Required command not found: sha256sum` — nothing is requested.
- AC3: For `ARCH=arm64` and `ARCH=armv6`, with a matching entry, the script exits 0, extracts the directory and
  prints `Checksum of <archive> verified.` between the `Downloading …` and the `Extracting …` line.
- AC4: The smoke substitute runs in `go test ./...`: `bash -n` on the script and the AC2/AC3 runs against a scratch
  directory with a stub `curl`; `shellcheck` reports nothing (*Analyzer gate*).
- AC5: `README.md`, *1. Download and extract the release*, says that the script checks the archive against the
  release's `checksums.txt` with `sha256sum` and stops without extracting on a mismatch or a missing entry, that this
  detects a corrupted or incomplete download but does not prove who built the release, and that the manual commands
  below it do not check.

## Tests per criterion

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | `TestAptUpdateTimer_Schedule_AC1`; check: Dev runs `systemd-analyze verify` on both units in `packaging/` | `internal/packaging/pimonitor_apt_update_timer_test.go` |
| AC2 | `TestDownloadLatest_ChecksumsUnavailable_AC2`, `TestDownloadLatest_NoUsableEntry_AC2` (table), `TestDownloadLatest_ChecksumMismatch_AC2`, `TestDownloadLatest_ArchiveDownloadFails_AC2`, `TestDownloadLatest_MissingSha256sum_AC2` | `internal/packaging/download_latest_test.go` |
| AC3 | `TestDownloadLatest_VerifiedArchive_AC3` (cases `arm64`, `armv6`); check: Dev runs the changed script against the live latest release with `ARCH=arm64` and `ARCH=armv6`, each in a new empty scratch directory | `internal/packaging/download_latest_test.go` |
| AC4 | `TestDownloadLatest_Syntax_AC4` (`bash -n`; deferred to step 6, it cannot fail on today's script); CI: the `go test` job runs the AC2/AC3 tests; check: `shellcheck` in the *Analyzer gate* stamp | `internal/packaging/download_latest_test.go` |
| AC5 | check: Reviewer reads the README paragraph against AC5 | — |

Test cases:

- `TestAptUpdateTimer_Schedule_AC1` reads `../../packaging/pimonitor-apt-update.timer` and asserts: no line starts
  with `Persistent=`, exactly one `OnBootSec=10min` and one `OnUnitActiveSec=6h` line in `[Timer]`. Fails today.
- `TestDownloadLatest_NoUsableEntry_AC2`, each case with `ARCH=arm64`, version `v1.3.0`: only the `armv6` line; the
  `arm64` line of another version (`pimonitor_1.2.0_linux_arm64.tar.gz`); the entry with CRLF endings; the entry in
  binary form `<hash> *<archive>`; the entry twice (same hash); a 63-character hash; an upper-case hash; the BSD form
  `SHA256 (<archive>) = <hash>`; an empty file. Each asserts AC2's line, exit 1, and that the stub log holds no
  archive request.
- `TestDownloadLatest_ChecksumMismatch_AC2` serves the **real v1.3.0 `checksums.txt`** (fact 4, an inline constant;
  public release data, nothing to anonymize) with a generated archive whose hash differs; asserts the two lines with
  `expected bf6b0498…d29a` (full hash) and the generated archive's hash, exit 1, no directory, no archive file.
- `TestDownloadLatest_ChecksumsUnavailable_AC2`: the stub has no `checksums.txt` (exit 22 like `curl -f`).
- `TestDownloadLatest_ArchiveDownloadFails_AC2`: a valid entry, and the stub writes the first half of the archive to
  the `-o` file, then exits 18 (curl's partial-file code); asserts AC2's line, exit 1, no archive file, no directory.
- `TestDownloadLatest_MissingSha256sum_AC2`: the tool directory lacks `sha256sum`; the stub log is empty.
- `TestDownloadLatest_VerifiedArchive_AC3`: a generated `checksums.txt` in the real format with both archives; asserts
  exit 0, `<dir>/pimonitor` extracted, the archive kept, the stdout line order of AC3, and the stub log order
  release API, `checksums.txt`, archive — all `https://`.

Shell procedures (*Operator environments and documented procedures*): the README's three script lines and the
manual snippet are unchanged, so their paste behavior is too. The script runs under `set -euo pipefail`; every
failing step of the new check ends it before `tar` runs, which the AC2 tests hold. Rows only CI can run: none; only a
real Pi shows `uname -m` detection plus `install.sh` end to end (Reviewer Notes of the PR, unchanged code).

## Approach

Early draft PR: no

**Timer.** Delete line 7 `Persistent=true`; nothing else in the file changes.

**`download-latest.sh`**, in this order (existing steps unchanged):

1. Required commands become `curl tar sha256sum` (same loop and message).
2. After the "target directory already exists" check and before `Downloading …`:
   `checksums=$("${CURL[@]}" "<release download base>/checksums.txt")` inside an `if !`; on failure AC2's first line
   and `exit 1`. Held in a variable like `release_json`, no file is written.
3. `expected=$(awk -v name="$archive" '$2 == name { print $1 }' <<<"$checksums")` — a here-string, not a pipe, so
   no `SIGPIPE` under `pipefail`. Then `[[ "$expected" =~ ^[0-9a-f]{64}$ ]]` or AC2's second line and `exit 1`.
4. Download the archive inside `if ! "${CURL[@]}" -o "$archive" "$url"; then`: `rm -f -- "$archive"`, AC2's
   download line, `exit 1` — so no partial, unverified file stays behind (decision 0032).
5. `actual=$(sha256sum "$archive")`, `actual=${actual%% *}`; on `"$actual" != "$expected"`: the mismatch line,
   `rm -f -- "$archive"`, the `Removed …` line, `exit 1`. Otherwise `printf 'Checksum of %s verified.\n'` to stdout.
6. Extract as today. All new error lines go to standard error.

Accepted forms of a `checksums.txt` line (awk's default field splitting on blanks and tabs): exactly one line whose
second field equals the archive name and whose first field is 64 lower-case hex digits is used — GoReleaser's
`<hash>  <name>` and also single-space, tab-separated or indented variants, or a line with trailing fields, since
only the hash is taken and it is still compared with the computed one. Rejected, so fail closed with "No checksum":
no such line; two or more such lines (the joined value fails the pattern); `*<name>` binary marker; CRLF (`<name>\r`);
upper-case or wrong-length hex; BSD `SHA256 (name) = hash`; a backslash-escaped name line. The archive name contains
no backslash (`pimonitor_<version>_linux_<arch>.tar.gz`), so `awk -v` escape processing does not alter it. No form can
make a different archive pass: acceptance always ends in an equality test against `sha256sum` of the downloaded file.

**Tests** (`internal/packaging/`, package `packaging`, decision 0033; skip with a reason when `runtime.GOOS` is not
`linux`, fail when a tool is missing on Linux):

- `newScriptEnv(t, tools ...string)`: a `t.TempDir()` tool directory with symlinks to the real `bash`, `awk`, `tar`,
  `gzip`, `sha256sum`, `rm`, `ls`, `cat`, `uname` found by `exec.LookPath` (minus the ones a case drops), a work
  directory and a served-files root; the script runs as `exec.CommandContext(ctx, <abs bash>, <abs script>)` with an
  explicit `Env` (`PATH`=tool dir, `HOME`, `ARCH`, `STUB_ROOT`, `STUB_LOG`) and `Dir`=work directory, under a
  context timeout.
- `writeStubCurl`: a bash script named `curl` in the tool directory that ignores the flags, takes the last argument as
  the URL, appends it to `$STUB_LOG`, exits 1 for a non-`https://` URL, serves `$STUB_ROOT/<URL without https://>` to
  the `-o` file or stdout, and exits 22 silently when the file is missing. A served file with a sibling
  `<file>.partial` is cut to half its length and the stub then exits 18.
- `writeReleaseArchive`: builds `pimonitor_<v>_linux_<arch>.tar.gz` with `archive/tar` and `compress/gzip`, holding
  `<dir>/pimonitor`, and returns its SHA-256 (`crypto/sha256`). The API answer is a minimal JSON with a `"tag_name":
  "v1.3.0",` line.

Layout of the new output: AC2 and AC3 fix every line; nothing else is printed.

## Options considered

- **Verification design** (decision 0032): checksums in memory and fetched first, compare, remove on mismatch — chosen;
  `sha256sum --check` on two downloaded files (lost: an extra file, `--ignore-missing` passes a missing entry, tool
  output as the message); keep the bad archive (lost: an unverified archive stays next to install instructions);
  signature verification (lost: releases are unsigned, a release-trust change of its own).
- **Timer**: remove the line — chosen (the issue's decision); switch to `OnCalendar=` with `Persistent=true` (lost: the
  issue keeps the schedule "10 minutes after boot, then every 6 hours").
- **Where the smoke test lives** (decision 0033): test-only package `internal/packaging` — chosen; colocated in
  `packaging/` (lost: ships in the archive or narrows the GoReleaser file list); self-test mode in the script (lost:
  test code in the operator's script); shell test plus CI step (lost: workflow change, second runner).
- **README manual snippet**: add a `sha256sum` step to it (lost: not asked, and a pasted block continues to `tar` after a
  failed check — the half-finished state *Shell procedures* forbids); say it does not check — chosen.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |
| packaging | `pimonitor-apt-update.timer` | `Persistent=true` removed |
| packaging | `download-latest.sh` | checksum download, lookup, verification, `sha256sum` required |
| tests | `internal/packaging/download_latest_test.go`, `internal/packaging/pimonitor_apt_update_timer_test.go` | new |

## Signatures (for the Dev's skeleton)

None: no Go API changes; the test helpers are private to the test files (*Approach*). No skeleton (step 4).

## Test files

- `internal/packaging/download_latest_test.go`
- `internal/packaging/pimonitor_apt_update_timer_test.go`

Existing test code that calls a changed signature: none.

Files of the change no unit test can cover: none — the shell script is exercised by the tests above; it is not in
`COVERAGE_PATHSPECS` (`*.go`), so no exclusion follows.

## Areas

- `docs/areas/deployment.md` (+70 words): *`download-latest.sh`* — fetches `checksums.txt` first, the verification and
  what it does not prove, removal on mismatch, "Requires `curl`, `tar` and `sha256sum`"; *`.timer`* bullet — the timer
  sets no `Persistent=` (it would apply only to `OnCalendar=`), so a run missed while the Pi was off is not caught up
  beyond the boot run; *Limits and errors* — status 1 also for a missing `sha256sum`, an unavailable `checksums.txt`,
  no usable entry, a failed archive download and a mismatch (the archive removed in both), the "does not verify" sentence dropped; *Related decisions* —
  0032; *Open questions* — both removed, section left as "None."

## Documentation updates

Written by the Dev after the code is green, unless named otherwise:

- `docs/areas/deployment.md`: as *Areas*.
- `README.md`, *1. Download and extract the release*: the paragraph after the script's three lines gains AC5's
  content; the snippet and the `ls` listing stay.
- `docs/UNIT_TESTS.md`, *Where tests live* (Tester): the colocation bullet names the exception — tests of the files
  under `packaging/` live in `internal/packaging/` (everything under `packaging/` ships in the release archive),
  linking decision 0033. *Test doubles* (Tester, `docs/UNIT_TESTS.md (Test doubles +30 words)`): one line for
  `newScriptEnv`, `writeStubCurl`, `writeReleaseArchive`.
- `.squad/stack.md`, *Layout*, the *Tests* bullet (Tester reports the text, orchestrator writes it): the same
  exception in one sentence.
- `.squad/project.md` (+35 words), *Security areas* (Dev reports the text, orchestrator writes it): a line "Release
  download: `packaging/download-latest.sh` fetches the archive whose `install.sh` the operator runs as root — HTTPS
  only, checked against the release's `checksums.txt` — [Deployment](../docs/areas/deployment.md)".
- `AGENTS.md` and `.github/copilot-instructions.md`, project block *architecture* (Tester reports the text,
  orchestrator writes it identically in both): a line ``internal/packaging` — tests of the `packaging/` scripts and
  units (test files only).``
- `docs/ARCHITECTURE.md`, `SECURITY.md`, `docs/CONTRIBUTING.md`: no change (fact 13; CONTRIBUTING already says
  GoReleaser writes `checksums.txt`). No changelog file exists; the release-notes label is the orchestrator's.

## Architecture check

The privilege split is untouched: the timer still runs only `pimonitor-apt-update.service` as root on the same
schedule (fact 1: the removed line had no effect), and `download-latest.sh` still needs no root. No guarantee of
`.squad/project.md` changes; the archive layout stays (the tests are outside `packaging/`).

## Security considerations

- [x] Limits: none on input. `checksums.txt` is read into memory without a size cap, like the API answer today, from
  the same HTTPS origin as the archive; the script parses only the one matching line.
- [x] User-visible failure texts: exactly AC2's lines; curl's own `curl: (22) …` line may precede the first one
  (real curl with `-S`), no other runtime message reaches the user from the new code.
- The check gives integrity against the release, not authenticity (decision 0032): README and the area document say so.
- The new code introduces no new command interpolation: `$archive` is passed as an `awk -v` value and as a quoted
  argument; `rm -f --` guards a leading dash.

## Decision records

- `docs/decisions/0032-download-checked-against-release-checksums.md` (Proposed)
- `docs/decisions/0033-packaging-script-tests-in-internal-packaging.md` (Proposed)

## Out of scope / follow-ups

- Signing releases (cosign/GPG) and verifying the signature — no follow-up planned unless the Product Manager asks.
- An existing `/var/lib/systemd/timers/stamp-pimonitor-apt-update.timer` on upgraded Pis, if systemd wrote one, stays
  as a harmless leftover; no cleanup step is added to `install.sh`.

## Challenge

- DA minor 1 (target host has `sha256sum`): accepted — *Verified facts* rows 14 and 15; row 15 is unverified,
  owned by the Product Manager's check on a Pi before step 9. The area document's "Requires `curl`, `tar` and
  `sha256sum`" stays, since it states the script's requirement, whatever the check shows.

## Security review

- NON-BLOCKING, partial archive after a failed download (`fix now`): taken as Security's smallest fix, not the
  narrowing of decision 0032 — the record's promise "no unverified archive is left behind" is the point of the check,
  and the fix is three lines. Changed: *Approach* step 4, AC2's new download-failure line,
  `TestDownloadLatest_ArchiveDownloadFails_AC2` and the stub's `.partial` mode, decision 0032's decision sentence.

## Challenge notes

Devil's Advocate, plan mode, specs/issue-204 (/home/user/PiMonitor/specs/issue-204/plan.md)

I checked the plan's claims myself and they hold:
- `Persistent=true` is at /home/user/PiMonitor/packaging/pimonitor-apt-update.timer:7, next to `OnBootSec`/`OnUnitActiveSec` (lines 5-6), and there is no `OnCalendar=`.
- /home/user/PiMonitor/packaging/download-latest.sh:75-79 downloads and runs `tar xzf` with no check.
- The required-command loop is at line 46. It runs before the release API request (line 53), so the `sha256sum`-missing case ("nothing is requested") is reachable.
- /home/user/PiMonitor/.goreleaser.yaml:12-17 and 37-38 build two archives and publish `checksums.txt`. Lines 31-33 ship everything under `packaging/`, which is why the test-only `internal/packaging` location (decision 0033) is justified.
- shellcheck is part of the *Analyzer gate* (/home/user/PiMonitor/.squad/tools/analyzer_common.py:106), so AC4's shellcheck row is enforced.
- CI's `go test` jobs run on `ubuntu-latest` (/home/user/PiMonitor/.github/workflows/ci.yml:18, 45).
- `.golangci.yml` enables no gosec, so `exec.CommandContext` with a looked-up bash path raises no G204 finding.
- The test names fit *Criterion ids* (/home/user/PiMonitor/docs/UNIT_TESTS.md:154-160).
- The mismatch test uses a real `checksums.txt` sample (v1.3.0), so the *Sample* rule is met.
- Need: the issue records the decision, and no accepted record covers it.
- Scope: no sibling download path is missed. The only other download is the README manual snippet (/home/user/PiMonitor/README.md:155-158), which the plan deliberately documents as unchecked, with the reason.
- The lookup design fails closed in every form. Duplicate lines join with a newline and fail the `^…{64}$` match. CRLF lines fail the `$2 == name` comparison. Every accepted entry still has to equal the computed `sha256sum`.

Objections:

1. minor: Add a *Verified facts* row for the target host. Why it matters: the plan makes `sha256sum` a hard requirement on the Pi but never shows that Raspberry Pi OS has it, which *Operator environments and documented procedures* (/home/user/PiMonitor/.squad/routing.md:166-173) asks for. Instruction to the Dev: record that `sha256sum` comes from `coreutils` (Essential: yes on Raspberry Pi OS Lite, Bookworm and later), with the source you checked. Update `docs/areas/deployment.md`'s "Requires `curl`, `tar` and `sha256sum`" line only to match what that check shows.

The plan holds up apart from this one minor addition.

## Security notes

Security review, mode `plan`: specs/issue-204/plan.md, with decisions 0032 and 0033 (/home/user/PiMonitor/docs/decisions/0032-download-checked-against-release-checksums.md, /home/user/PiMonitor/docs/decisions/0033-packaging-script-tests-in-internal-packaging.md)

**Tier and areas.** The plan has tier `security`, which is correct. It changes how far an operator can trust the archive whose `install.sh` then runs as root. It lists both areas it touches: Privilege separation (the root apt timer) and the new Release download area, which the plan adds to `.squad/project.md`. I found no other area it touches. It changes no CI workflow, Docker or build configuration and adds no dependency (the tests use only the standard library). The plan states correctly that the check proves integrity against the release, not authenticity, and both the README (AC5) and the area document will say so.

**Timer.** Deleting `Persistent=true` (packaging/pimonitor-apt-update.timer:7) leaves the same unit running as root on the same `OnBootSec`/`OnUnitActiveSec` schedule. The privilege split holds whether or not that line ever had an effect, so it does not depend on fact 1. I tried to check fact 1 myself: `gh api -H "Accept: application/vnd.github.raw" "repos/systemd/systemd/contents/man/systemd.timer.xml?ref=v255"` returned `GitHub access to this repository is not enabled for this session ... (HTTP 403)`, and the local man pages are minimized away. So my own check was not run; it needs read access to systemd/systemd. This is not blocking, because no security property rests on it.

**Bypass forms of the checksum lookup.** I ran the planned lookup myself: the awk command from *Approach* step 3 plus `[[ "$expected" =~ ^[0-9a-f]{64}$ ]]`, with the archive name `pimonitor_1.3.0_linux_arm64.tar.gz`. The script is /tmp/claude-0/-home-user-PiMonitor/0cf353e4-7165-5ab5-94a5-7943a45a2166/scratchpad/t.sh. It ran under `mawk`, which is `/usr/bin/awk` here (via /etc/alternatives), the same default as on Debian and Raspberry Pi OS; `gawk` is not installed. Results:

- **Accepted:** the GoReleaser form `<hash>  <name>`, and an indented, tab-separated line with a trailing field.
- **Rejected:** CRLF; the `*<name>` binary form; the same entry twice (the result has an embedded newline, and the match is anchored to the whole string); an upper-case hash; the BSD form `SHA256 (<name>) = <hash>`; an empty file; a backslash-escaped line; a BOM before the line; `./<name>`; separators `\v` and `\f`.

This matches the plan's list of accepted and rejected forms, and every rejection fails closed. A crafted `tag_name` containing a backslash would also fail closed in two ways:
- `awk -v` would rewrite the name, so no line matches.
- GNU `sha256sum` prefixes its output with `\` for such a name (`sha256sum 'a\b'` printed `\2d7116…4881  a\\b`), so the comparison fails.

Every accepted form still ends in an equality test against the hash of the downloaded file, so no form lets a different archive pass. The test table in the plan covers these classes.

**Resource bound.** The plan claims no resource bound. `checksums.txt` is read into memory without a cap, the same as the API answer today and from the same HTTPS-only origin.

**Command injection.** The plan adds no command interpolation: the archive name is passed as an `awk -v` value and as quoted arguments, and the removal uses `rm -f --`.

**Findings**

[NON-BLOCKING] A failed archive download leaves an unverified partial archive behind. This contradicts decision 0032 ("no unverified archive is left behind for a manual `tar xzf`") and the design of AC2.
- **Where:** *Approach* step 4 keeps `"${CURL[@]}" -o "$archive" "$url"` (packaging/download-latest.sh:76) unchanged.
- **What happens:** under `set -e`, a transfer that breaks off after the retries ends the script with the partial file curl wrote still in the directory. The README and the script's output then tell the operator to extract it and run `install.sh` as root. The script does nothing new to that file; only the claim in the decision goes beyond the plan.
- **Smallest fix:** in step 4, run `if ! "${CURL[@]}" -o "$archive" "$url"; then rm -f -- "$archive"; printf 'Could not download %s of PiMonitor %s.\n' "$archive" "$version" >&2; exit 1; fi`, add that line to AC2's list, and add a stub case (`curl` writes a partial file, then exits 18) to `TestDownloadLatest_*_AC2`.
- **Alternative:** narrow decision 0032 to the mismatch case.

Recommendation: fix now
