---
name: create-pr
description: Use when the user asks to open/create a pull request for PiMonitor changes on this branch. Runs local verification (build, vet, test, lint), reviews the change with the pimonitor-reviewer subagent, then pushes the branch and opens a PR following this repo's pull request template.
---

# Create PR

Use this skill to prepare and open a pull request for changes made in this
repository.

## Steps

1. **Verify the working tree**: run `git status` and `git diff` to confirm
   what will be included. Do not include unrelated or uncommitted work the
   user didn't ask for.
2. **Verify tests exist** for what the diff changes. Per [`TESTS.md`](../../../docs/TESTS.md),
   tests are mandatory for new/changed behavior, not optional — if the diff adds or
   changes logic without a corresponding test, write one before proceeding (following
   `TESTS.md`'s naming, fixture, and table-driven conventions) rather than opening the PR
   without coverage.
3. **Run local verification** before pushing:
   - `go build ./...`
   - `go vet ./...`
   - `go test ./... -race -cover`
   - `golangci-lint run` if installed (skip with a note if not available in
     this environment, but do not silently skip `go vet`/`go test`)
   Fix any failures before proceeding — do not open a PR with failing checks.
4. **Commit** with a concise, imperative summary line and a body explaining
   *why* the change was made if not obvious from the diff. Follow the
   language rule: English only.
5. **Run the internal review loop** (see below) and resolve what it finds.
   This happens *before* the push, so the pull request opens on a reviewed
   change instead of collecting review rounds afterwards.
6. **Push** the branch: `git push -u origin <branch-name>`.
7. **Open the PR** using the repository's template at
   `.github/pull_request_template.md`. Fill in Description, Issues (link the
   related issue if one exists), Reviewer Notes, and Test Plan, and check off
   the checklist items that are actually true (don't check items you haven't
   verified) — including the REST API/configuration/packaging section when
   applicable, not just the General section. In Reviewer Notes, record how
   many internal review passes ran and which commits resolved their
   findings; put any accepted non-blocking findings under Next Steps.
8. Report the PR URL back to the user.

## The internal review loop

The review happens here, in this session, against the local branch — not as
a round trip through pull request comments. Each pass is delegated to the
`pimonitor-reviewer` subagent, which runs on Opus with a fresh context and
the repository's full review checklist.

1. **Pass 1** — launch `pimonitor-reviewer` (subagent_type
   `pimonitor-reviewer`, model `opus`). Tell it the base ref, the head to
   review, and that this is round 1.
2. **Act on the verdict**:
   - `APPROVE` → done, go push.
   - Blocking findings → fix each one minimally and commit. Do not widen the
     change beyond what the finding requires.
   - Non-blocking findings → **do not open another round for them**. Fix one
     only if it is trivial and already in scope; otherwise carry it into the
     PR's Next Steps or propose a follow-up issue, and say so.
3. **Pass n+1** — launch a fresh `pimonitor-reviewer` and give it the round
   number, the previous round's findings, and the commits that fixed them.
   It reviews the delta only, per its own instructions.
4. **Stop** at the first pass that reports no blocking findings. Cap the loop
   at **three passes**: if blocking findings remain after the third, stop and
   report the open findings to the user rather than continuing to iterate —
   at that point the change needs a decision, not another round.

Two rules keep this loop finite, and they are the point of the whole
arrangement:

- **Later passes review the delta, never the whole diff again.** A fresh full
  review of unchanged code always finds something new.
- **Only blocking findings start a new pass.** Non-blocking findings are
  recorded, not iterated on.

## Notes

- Do not add any Claude/Anthropic attribution to commits or PRs created via
  this skill: omit `Co-Authored-By: Claude ...` and `Claude-Session: ...`
  trailers from commit messages, and omit the "Generated with Claude Code"
  line and session link from the PR body.
- Never force-push over another contributor's commits without explicit
  confirmation.
- If the change touches `/api/v1/...` response shapes, `README.md`,
  `docs/API.md`, a documented design decision in `ARCHITECTURE.md`, or the
  systemd packaging in `packaging/`, make sure the corresponding
  documentation was updated as part of the same PR (see the template
  checklist). See [`CONTRIBUTING.md`](../../../docs/CONTRIBUTING.md) for the full
  workflow and stability policy this skill follows.
