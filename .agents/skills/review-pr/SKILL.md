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
2. Run `go build ./...`, `go vet ./...`, and `go test ./... -race -cover`
   against the PR branch. Note any failures.
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
   - **Privilege separation**: does the change keep `pimonitor.service`
     unprivileged (no new dependency on root-only files/commands)? See
     `ARCHITECTURE.md` and `SECURITY.md`.
   - **REST API stability**: does the change alter the JSON shape of an
     existing `/api/v1/...` response? If so, it should be a new API version
     rather than an in-place breaking change (see `ARCHITECTURE.md`).
   - **Test coverage**: new or changed logic must have tests — this is a hard
     requirement in this repository, not just a nice-to-have. Check the diff
     against [`TESTS.md`](../../../docs/TESTS.md): fixture-based (no real
     `/proc`/`/sys` access), `Test<Subject>_<Scenario>` naming, table-driven
     for multiple scenarios, no third-party test/assertion/mocking library.
     Flag a missing test on new/changed behavior as a blocking issue, not a
     suggestion.
   - **Language**: all new code, comments, and docs are in English.
   - **Unnecessary dependencies**: flag any new third-party dependency beyond
     `gopkg.in/yaml.v3` and ask if it's really justified over hand-rolling
     (see `CONTRIBUTING.md` dependency philosophy).
4. Post review comments (or a summary if inline commenting isn't available)
   focused on concrete, actionable issues — don't nitpick style that a
   linter would already catch.
5. If everything checks out, say so explicitly rather than staying silent.
