---
name: fix-issue
description: Use when the user asks to fix a specific GitHub issue in PiMonitor. Reproduces the problem (ideally as a failing test), implements a minimal fix, reviews it with the pimonitor-reviewer subagent, and opens a PR referencing the issue.
---

# Fix Issue

Use this skill to resolve a reported GitHub issue in this repository.

## Steps

1. Read the issue in full, including comments — note the reported
   environment (Pi model, distribution, PiMonitor version) since bugs in the
   metric collectors are often hardware/OS-specific.
2. **Create a branch** for the fix off the latest default branch, e.g.
   `fix-issue-<number>-<short-slug>`. Do not commit directly to the default
   branch.
3. **Reproduce** the problem locally where possible, following
   [`TESTS.md`](../../../docs/TESTS.md)'s conventions (fixture-based, no real
   `/proc`/`/sys` access, `Test<Subject>_<Scenario>` naming):
   - For parser bugs (`internal/collector/*.go`), write a failing unit test
     using a fixture that captures the reported input (e.g. an actual
     `/proc/meminfo` or `apt list --upgradable` output from the issue).
   - For issues that only manifest on real Pi hardware (e.g. a thermal zone
     path that doesn't exist on some model), reproduce with the closest
     available fixture/mock and note in the PR that hardware verification is
     still needed.
   A fix without a reproducing test is not acceptable except in the genuine
   hardware-only case above — tests are mandatory for this repository, see
   `TESTS.md`.
4. Implement the **minimal** fix — do not refactor unrelated code or expand
   scope beyond what the issue describes.
5. Confirm the previously-failing test now passes, and run the full suite
   (`go build ./...`, `go vet ./...`, `go test ./... -race -cover`, and
   `golangci-lint run` if available) to check for regressions.
6. **Commit** the fix with a concise, imperative summary line (English only)
   and a body explaining *why* the change was made if not obvious from the
   diff.
7. **Review the fix before it leaves this session**: run the internal review
   loop from the `create-pr` skill — one or more `pimonitor-reviewer`
   subagent passes (Opus, fresh context) against the local branch, fixing
   blocking findings and re-running the reviewer on the delta until a pass
   comes back clean, capped at three passes. Non-blocking findings do not buy
   another pass, but they are still resolved before the push: fixed now while
   the branch is local, or filed as an issue if genuinely out of scope. This
   is what keeps the review out of the pull request comments, so do not skip
   it and do not defer it to a separate review session.
8. **Push** the branch (`git push -u origin <branch-name>`) and **open a PR**
   referencing the issue (`Closes #<number>`), following the `create-pr`
   skill's verification and template steps — do not skip the push/PR-creation
   steps even if verification already ran in step 5. Describe the fixed
   state, not the review that got you there (see "The loop stays invisible"
   below).
9. If the fix is not fully verifiable without physical Pi hardware, say so
   explicitly in the PR description rather than claiming full verification.

## The loop stays invisible

The review loop is working material and stays in this session. The pull
request documents the **finished state** — the issue's cause, the fix, the
components it touches, the guarantees it had to preserve, and the smoke test
that shows the reported symptom is gone. It does not document the way there.

Nothing in the PR body, the commit messages, or any PR comment mentions that
a review ran, how many passes it took, what it found, its verdicts or
severities, or the `pimonitor-reviewer` subagent. Reviewer Notes say what a
reviewer needs in order to review this fix — the affected collector, route
or config key, the reported environment it has to keep working on, where to
look first, and how to smoke-test it on a Pi. Next Steps is for genuine
follow-up work with issue links, never a parking lot for review findings.

## Every posted review point gets resolved in this PR

Once the PR is open, any point posted as a review comment — from a human, a
bot, or the `review-pr` skill, blocking or non-blocking — is work for this
pull request. There is no deferring to "the next change that touches this
file": it is not scheduled, and the session with the context is gone by
then. Each posted point ends in one of three ways, all inside this PR: fixed
and pushed with a one-line `Fixed in <sha>: …` reply, declined with a
reason, or split into an issue **created now** and linked from the reply —
then resolve the thread. The round caps limit re-reviews, not the number of
findings worked off. See `create-pr`'s section of the same name for the full
rule.

## Notes

- As with `create-pr`, do not add Claude/Anthropic attribution to commits or
  PRs: no `Co-Authored-By: Claude ...` / `Claude-Session: ...` commit
  trailers, and no "Generated with Claude Code" line or session link in the
  PR body.
