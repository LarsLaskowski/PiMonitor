# 0033: Tests of the packaging scripts live in a test-only package internal/packaging

- **Status:** Proposed
- **Date:** 2026-11-28
- **Area:** —
- **Source:** Issue #204
- **Supersedes:** —

## Context

`.squad/stack.md` (*Writing tests*) asks for a local smoke substitute for a script only a real Pi can run, and Issue
#204 adds checksum logic to `packaging/download-latest.sh` that needs one. `docs/UNIT_TESTS.md` colocates every test
with its code, but `.goreleaser.yaml` copies every file under `packaging/` into the release archive, whose contents
`README.md` and the Deployment area list. CI runs `go test ./...` and no shell test runner.

## Options considered

1. **A Go test in a test-only package `internal/packaging`** that runs the script under `bash` with stub commands in a
   scratch directory — runs in CI with the existing `go test ./...`, uses only the standard library, ships nothing.
   Cost: an exception to colocation, and a directory without production code.
2. **A Go test next to the script in `packaging/`** — colocated. Cost: the test ships in every release archive unless
   the GoReleaser file list is narrowed, a release-build change that must then track every new packaging file.
3. **A self-test mode inside the script** — no new directory. Cost: test code and an extra switch in the script every
   operator runs.
4. **A shell test script with a new CI step** — no Go. Cost: a workflow change, and a second test runner beside
   `go test`.

## Decision

Option 1: tests of the scripts and units under `packaging/` live in `internal/packaging/`, one `_test.go` file per
file under test, in package `packaging`, and run with `go test ./...`.

## Consequences

- `docs/UNIT_TESTS.md` (*Where tests live*) and `.squad/stack.md` (*Layout*) name the exception.
- The tests of `download-latest.sh` need Linux with `bash`, `awk`, `tar`, `gzip`, `sha256sum`, `rm`, `ls`, `cat` and `uname`; elsewhere they skip. The timer test only reads the unit file and runs on every platform.
- No production Go code is added, so the coverage figures do not change.
