---
name: review-pr
description: Use when the user asks to review a PiMonitor pull request. Checks out the PR, runs tests, and reviews the diff against this project's Go, security, and API-stability conventions.
---

# Review PR

Use this skill to review a pull request against this repository's
conventions.

## Steps

1. Fetch and check out the PR (or read the diff directly if checkout isn't
   necessary for the review).
2. Run `go build ./...`, `go vet ./...`, and `go test ./...` against the PR
   branch. Note any failures.
3. Review the diff against this checklist:
   - **Error handling**: are errors from `/proc`, `/sys`, `os.Stat`, and
     `exec.Command` calls checked and handled gracefully (e.g. missing
     thermal zone on non-Pi hardware) rather than panicking?
   - **Resource leaks**: are opened files/readers closed (`defer f.Close()`),
     are goroutines/tickers properly stopped on shutdown?
   - **`/proc`/`/sys` parsing robustness**: does the parser handle malformed,
     truncated, or unexpected-format input without crashing? Is it covered
     by a unit test with fixture input?
   - **Command execution safety**: any `exec.Command` calls (`apt list
     --upgradable`, `vcgencmd measure_temp`) must use fixed argument lists —
     flag any string-concatenation into a shell command as a blocking issue.
   - **REST API stability**: does the change alter the JSON shape of an
     existing `/api/v1/...` response? If so, it should be a new API version
     rather than an in-place breaking change (see `CLAUDE.md`).
   - **Test coverage**: new parsers or handlers should have unit tests, not
     just the happy path.
   - **Language**: all new code, comments, and docs are in English.
   - **Unnecessary dependencies**: flag any new third-party dependency beyond
     `gopkg.in/yaml.v3` and ask if it's really justified over hand-rolling
     (see `CLAUDE.md` dependency philosophy).
4. Post the results as review comments on the PR (inline where possible,
   otherwise a single review comment). Only report genuine findings —
   concrete, actionable issues. Do not comment on things that are fine,
   pass the checklist, or work as expected; positive remarks and
   "looks good" filler add noise, not value. Don't nitpick style that a
   linter would already catch.
5. If the review produces no findings, do not post any comments.
