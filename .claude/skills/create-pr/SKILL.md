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
   applicable, not just the General section. Write all of it as a
   description of the finished change, never of the review loop that
   produced it — see "The loop stays invisible" below.
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
   - Non-blocking findings → **do not open another round for them**, but do
     not park them in the PR either. The branch is still local and the
     context that found them is still here, so the cheap outcome is to fix
     them now. If one is genuinely out of scope, open an issue for it before
     pushing. Either way they are resolved here, not carried into the PR
     text as leftovers.
3. **Pass n+1** — launch a fresh `pimonitor-reviewer` and give it the round
   number, the previous round's findings, and the commits that fixed them.
   It reviews the delta only, per its own instructions.
4. **Stop** at the first pass that reports no blocking findings. Cap the loop
   at **three passes**: if blocking findings remain after the third, stop and
   report the open findings to the user rather than continuing to iterate —
   at that point the change needs a decision, not another round.
5. **Cover your last fixes.** Fixes you make after the final pass — including
   fixes for its non-blocking findings — are themselves unreviewed. If the
   three-pass budget still has a pass left, spend it on them as a delta
   review. If it does not, say so when you report, and push anyway rather
   than starting a fourth pass.

Two rules keep this loop finite, and they are the point of the whole
arrangement:

- **Later passes review the delta, never the whole diff again.** A fresh full
  review of unchanged code always finds something new.
- **Only blocking findings start a new pass.** Non-blocking findings are
  still resolved — fixed now, or filed as an issue before the push — they
  just do not buy another pass.

The cap is on *passes*, not on findings. A single pass may resolve any
number of them.

## The loop stays invisible

This section is about the **internal loop above** — the passes that run in
this session before the push. That loop is working material and does not
travel with the change. Review comments posted on the pull request once it
is open, and the replies to them, are a different thing: they are public
review, governed by the next section, and nothing here forbids them.

The pull request documents the **finished state**: what the change does,
which components it touches, which guarantees it had to preserve, and how to
smoke-test it. It does not document the way there. So nothing you write when
opening the PR — the body, and the commit messages on the branch — mentions:

- that an internal review ran, or how many passes it took
- its findings, their verdicts or severities, or which commit resolved
  which one
- the `pimonitor-reviewer` subagent, or internal review rounds

Commit messages still explain *why* the change is what it is, as always —
they just explain it in terms of the change, never in terms of a finding
that prompted it.

A reviewer opening the PR gets the change, not its history — the loop's
value was in fixing the code, and that value is already in the diff.

So fill the template like this:

- **Description** — the problem and what the change does about it.
- **Reviewer Notes** — the components the diff touches (routes, config keys,
  collectors, packaging), the guarantees it had to keep intact (`/api/v1`
  response shapes, privilege separation, the unprivileged/privileged service
  split), where to look first, and the smoke test: the concrete steps to
  exercise the change on a Pi.
- **Test Plan** — the tests that cover the change and anything that could
  only be verified against real hardware.
- **Next Steps** — genuine follow-up work, with issue links. Never a parking
  lot for review findings; see the next section for where those go.

## Every posted review point gets resolved in this PR

Once a point exists as a review comment on the pull request — from a human,
a bot, or the `review-pr` skill, blocking or non-blocking alike — it is work
for **this** pull request. Severity decides the order it gets handled in, not
whether it gets handled.

There is no deferring to "the next change that touches this file". That
change is not scheduled, and the session holding the context is gone long
before it happens. A posted point therefore has exactly three outcomes, all
of them reached while this PR is open:

1. **Fixed** — implement it, push it, reply `Fixed in <sha>: <what
   changed>`, resolve the thread.
2. **Declined** — reply with the reason it stays as it is, resolve the
   thread. A reason, not a deferral: "this is intentional because …", not
   "later".
3. **Split out** — only when it is real work that genuinely does not belong
   in this PR: create the issue **now**, link it from the reply, resolve the
   thread. A promise of a follow-up issue without a created issue is not an
   outcome.

The round caps still apply: at most three internal passes here, and at most
two rounds on GitHub per `review-pr`. They limit how often the change is
*re-reviewed* — not how many findings get worked off. Arriving at the cap
with open posted points is not "done"; it means fixing, declining, or filing
them and saying so.

## Notes

- Never force-push over another contributor's commits without explicit
  confirmation.
- If the change touches `/api/v1/...` response shapes, `README.md`,
  `docs/API.md`, a documented design decision in `ARCHITECTURE.md`, or the
  systemd packaging in `packaging/`, make sure the corresponding
  documentation was updated as part of the same PR (see the template
  checklist). See [`CONTRIBUTING.md`](../../../docs/CONTRIBUTING.md) for the full
  workflow and stability policy this skill follows.
