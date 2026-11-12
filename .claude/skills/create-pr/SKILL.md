---
name: create-pr
description: Use when the user asks to open/create a pull request for changes on this branch. Runs local verification (format, build, test), reviews the change with the squad-reviewer subagent, then pushes the branch and opens a PR following this repo's pull request template.
---

# Create PR

Use this skill to prepare and open a pull request for changes made in this
repository.

## Steps

1. **Verify the working tree**: run `git status --short --branch` and
   `git diff` to confirm what will be included, and confirm the `origin`
   remote exists. Do not include unrelated or uncommitted work the user
   didn't ask for. If there are no relevant local changes and no unpushed
   commits, stop and say so plainly.
2. **Create a branch** if you are still on `main` (or another base branch) —
   never commit directly to it. Derive a short kebab-case name from the work
   (e.g. `add-season-aggregates`, `fix-path-mapping`), or use the name the
   user supplied. If you are already on a feature branch, stay on it.
3. **Verify tests exist** for what the diff changes: logic the diff adds or
   changes without a test gets one now, following
   [`UNIT_TESTS.md`](../../../docs/UNIT_TESTS.md), before the PR opens.
4. **Run local verification** before pushing, from the repository root,
   with the commands from [`.squad/stack.md`](../../../.squad/stack.md):
   - *Restore* (if the stack has one) and *Format*
   - *Build* — it must finish without errors and without the warnings
     `stack.md` lists as forbidden
   - *Analyzer gate* — no analyzer diagnostic of any severity in a changed
     file; treat each as a failure
   - *Test with coverage* and *Coverage gate* — it must pass (the thresholds
     are in `.squad/tools/squad_settings.py`)
   - `python3 .squad/tools/config-check.py` when the diff touches `.claude/`,
     `AGENTS.md`, `CLAUDE.md` or `.github/copilot-instructions.md`
   Fix any failures before proceeding — do not open a PR with failing checks,
   unformatted code or outstanding analyzer diagnostics. This step is the gate
   before the PR; CI is not meant to find anything here.
5. **Commit** as `AGENTS.md` (*Commit messages and pull requests*) says,
   staging only the files that belong to this task. The *Sonar gate* from
   `.squad/stack.md` reads the CI code analysis of the draft pull request, so it
   runs in step 9.
6. **Run the internal review loop** (see below) and resolve what it finds.
   This happens *before* the push, so the pull request opens on a reviewed
   change instead of collecting review rounds afterwards.
7. **Push** the branch: `git push -u origin <branch-name>`.
8. **Open the PR as a draft** (`draft: true`) using the repository's template at
   `.github/pull_request_template.md`. The GitHub tools can rewrite an inline Markdown link in the body into
   a broken form: write record references as plain paths:
   - base branch `main`, unless the user explicitly requests a different base
   - title `[area] Description` per
     [`CONTRIBUTING.md`](../../../docs/CONTRIBUTING.md) — area is one of the
     areas CONTRIBUTING lists, capitalized, no period at the end, no issue number
     in the description, under 70 characters
   - fill in Description, Issues (link the related issue if one exists, with
     `Closes #<number>`), Reviewer Notes and Test Plan, and check off the
     checklist items that are actually true (don't check items you haven't
     verified) — including the unit-test, formatting, analyzer, coverage,
     documentation and dependency items, not just the general ones
   - wrap the body in a HEREDOC so the formatting survives
   - set one label on the PR (`issue_write`, method `update`, `labels`) for the release notes
     (`.github/release.yml`): `breaking-change` for a change that breaks existing users, else `enhancement` for a
     feature, `security` for a security fix, `bug` for a bug fix (with a linked issue: the matching label of the
     issue, else judge from the change); none for anything else (it lands under *Other
     changes*), `ignore-for-release` for a change that is not worth a release note, among them every change of the
     squad alone (only `.squad/`, `.claude/`, `AGENTS.md`, `CLAUDE.md`, `.github/copilot-instructions.md`)
   - read the body back right after creating or updating the PR
     (`pull_request_read`, method `get`) and compare its links and code spans
     with what you sent; a mangled one is fixed with `update_pull_request`
     before anything else
9. **Keep it a draft until green** (`.squad/orchestration.md`, *Draft until green*).
   Subscribe to the PR's activity (`subscribe_pr_activity` when available;
   otherwise check the results yourself before finishing). A merge conflict
   comes first (`.squad/orchestration.md`, *Merge conflicts*). Fix each failing
   check and every new issue `python3 .squad/tools/sonar-check.py pr <number>`
   lists, rerun step 4 on the fix (on merged files: the gates they need),
   commit once per round and push. Mark the PR ready for review only when
   *Draft until green* holds; otherwise leave it a draft and tell the user
   what blocks it.
10. Report the branch name, the PR URL and whether it is ready for review or
    still a draft (and why) back to the user.

## What the pull request says — and what it doesn't

The pull request documents **the change**, not how the change was produced
(`AGENTS.md`, *Related skills*): describe the finished state.

- Reviewer Notes tell a reviewer where to look and why the approach was
  chosen: the components touched, any guarantee from
  [`ARCHITECTURE.md`](../../../docs/ARCHITECTURE.md) the change comes near,
  and a smoke test if one is worth running.

## The internal review loop

The review happens here, in this session, against the local branch — not as
a round trip through pull request comments. Each pass is delegated to the
`squad-reviewer` subagent; its checklist and round semantics live in
`.claude/agents/squad-reviewer.md`.

1. **Pass 1** — launch `squad-reviewer` (subagent_type `squad-reviewer`).
   Tell it the base ref, the head to review, and that this is round 1. When
   the diff touches a security area as `AGENTS.md` (golden rules) defines it,
   launch `squad-security` in mode `diff` alongside it; its findings count like the reviewer's.
2. **Act on the verdict** (with Security alongside: both verdicts):
   - `APPROVE` (and Security's `APPROVED`) → done, go push.
   - Security's `CHANGES_REQUIRED` → its findings are blocking findings like the reviewer's.
   - Blocking findings → fix each one minimally and commit. Do not widen the
     change beyond what the finding requires.
   - Non-blocking findings → never a pass of their own. A small, in-scope fix
     goes in with the blocking fixes, so the next delta pass reads it; when
     no further pass is due, a fix that changes code, tests or `docs/` gets
     one delta pass. Otherwise open a GitHub issue for it **now** and link it
     under the PR's Next Steps.
3. **Pass n+1** — continue the reviewer's earlier launch
   (`.squad/orchestration.md`, *Continuing a member*) with the round number, the
   previous round's findings and the commits that fixed them; it reviews the
   delta only. When Security's findings were fixed, `squad-security` reviews
   that delta too.
4. **Stop** at the first pass that reports no blocking findings. Cap the loop
   at **three passes**: if blocking findings remain after the third, stop and
   report the open findings to the user — the change needs a decision, not
   another round.

## Findings that arrive after the push

A review posted on the open pull request — human, automated or `review-pr` —
is worked in this session, in this pull request (`AGENTS.md`, *Related
skills*); `review-pr` (*Answering findings on your own PR*) describes how.

## Notes

- Prefer non-interactive commands only.
- Do not amend existing commits unless the user explicitly asks.
- If a PR already exists for the branch, push the new commits and report the
  existing URL instead of opening a duplicate; a draft stays a draft until
  step 9 holds on the new head.
- If push or PR creation fails, stop and report the failure clearly instead
  of continuing as if it succeeded.
- Never force-push over another contributor's commits without explicit
  confirmation.
- If the change touches a guarantee, security area or integration-surface
  entry in [`.squad/project.md`](../../../.squad/project.md), a configuration
  key, or the Docker/CI setup, make
  sure the corresponding documentation — [`README.md`](../../../README.md),
  [`ARCHITECTURE.md`](../../../docs/ARCHITECTURE.md),
  [`SECURITY.md`](../../../SECURITY.md) — was updated in the same PR (see the
  template checklist). A change in behavior also updates its area document in
  [`docs/areas/`](../../../docs/areas/README.md), and a decision with lasting
  weight gets its record in [`docs/decisions/`](../../../docs/decisions/README.md),
  both in the same PR. See
  [`CONTRIBUTING.md`](../../../docs/CONTRIBUTING.md) for the full workflow and
  stability policy this skill follows.