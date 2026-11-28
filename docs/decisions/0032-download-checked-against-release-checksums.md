# 0032: download-latest.sh checks the archive against the release's checksums.txt before extracting it

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** Deployment
- **Source:** Issue #204
- **Supersedes:** —

## Context

`download-latest.sh` fetches the latest release archive over HTTPS and extracts it; the operator then runs the
archive's `install.sh` as root. Every release also publishes `checksums.txt` (GoReleaser, one `<sha256>  <file>` line
per archive), which the script ignored, so a truncated or corrupted download, or an asset that does not belong to the
release, was extracted unnoticed. Issue #204 decided that the script verifies the archive and aborts on a mismatch.
`checksums.txt` comes from the same release over the same channel as the archive, so the check proves integrity
against that release, not who built it.

## Options considered

1. **Fetch `checksums.txt` before the archive, keep it in memory, compare the computed `sha256sum` with the one entry
   for the archive, remove the archive on a mismatch** — a missing or unusable entry stops the run before the archive
   is downloaded, no extra file is written into the operator's directory, and no unverified archive is left behind for
   a manual `tar xzf`. Cost: a corrupt download cannot be inspected afterwards.
2. **Download both files, then `sha256sum --check`** — the stock tool decides the match. Cost: `checksums.txt` lands
   in the current directory (or a temp file with cleanup), `--ignore-missing` would pass a release without the entry
   unless handled separately, and the tool's own output is the operator's error message.
3. **Keep a mismatched archive for inspection** — helps a debugging operator. Cost: the file the check rejected stays
   next to the instructions to extract and install.
4. **Verify a signature (cosign, GPG) instead** — would prove origin. Cost: the release publishes no signature today;
   a signing setup is a release-trust change of its own, out of the issue's scope.

## Decision

Option 1: the script fails closed — no valid entry, no download of `checksums.txt` or a different hash ends it with
status 1 before anything is extracted, and a mismatched archive is removed. The rules and messages are in
[Deployment](../areas/deployment.md#download-latestsh).

## Consequences

- `sha256sum` becomes a required command of the script (coreutils, present on Raspberry Pi OS).
- The check detects corrupted, incomplete or mismatched downloads only; a compromised release or GitHub account
  publishes matching checksums. Signing the release would have to come first to change that.
- The documentation says so, so the check is not read as an authenticity guarantee.
