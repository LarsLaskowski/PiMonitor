---
name: review-pr
description: Use when the user asks to review a PiMonitor pull request on GitHub. Checks out the PR, runs tests, reviews it with the pimonitor-reviewer subagent against this project's Go, security, and API-stability conventions, and posts the findings with an explicit verdict.
---

# Review PR

Use this skill to review a pull request on GitHub — someone else's, or your
own when you deliberately want a second opinion after it is open.

For a change that has not been pushed yet, do not use this skill: the
internal review loop in `create-pr` reviews the local branch before the pull
request exists, which is cheaper and does not fill the PR with comment
threads.

## Steps

1. Fetch and check out the PR (or read the diff directly if a checkout isn't
   necessary).
2. Delegate the review itself to the `pimonitor-reviewer` subagent
   (subagent_type `pimonitor-reviewer`, model `opus`). Give it the base ref,
   the head SHA, and the round number — round 1 for a first review, and for a
   re-review the previous round's findings plus the commits that were meant
   to fix them. The review checklist, the integration-surface sweep, the
   severity model and the round semantics all live in that agent's
   definition, so they stay identical whether the review runs before or after
   the push.
3. Post the result:
   - Inline comments for findings anchored to a line, otherwise one review
     comment.
   - **Only genuine findings.** No positive remarks, no confirmation that
     checklist items pass, no "looks good" filler, no style a linter catches.
   - Lead the review body with the verdict line the subagent produced
     (`APPROVE`, or the blocking/non-blocking counts), so the author can see
     whether anything is required of them without reading every thread.
   - Mark each finding `blocking` or `non-blocking` explicitly.
4. If the review produces no findings, post nothing beyond a short approving
   verdict — and if the previous round already said the same, post nothing at
   all.

## Keeping the loop finite

A pull request review can always produce one more finding. These rules make
it converge:

- **Round 1 reviews the whole diff. Every later round reviews only the
  delta**: does each fix resolve its finding, and did the fix commits break
  something — including in prose they wrote to fix a documentation finding?
  Never re-review untouched code; that is what turns three findings into
  four rounds.
- **Only blocking findings justify another round.** Non-blocking findings do
  not trigger a re-review — but once posted they are still the author's to
  resolve in this PR (see below). The cap is on rounds, not on findings.
- **Two consecutive rounds without a blocking finding means done.** Say so
  plainly instead of leaving the review open-ended.
- **At most two rounds on GitHub.** If blocking findings survive that, the
  change needs a decision from the author, not another review pass — say what
  is still blocking and stop. Stopping means no further *review* rounds; the
  posted findings themselves still get worked off.

## Answering findings on your own PR

Every point posted on the PR gets worked off in that same PR — whoever wrote
it, whatever its severity. Non-blocking is an ordering signal, not a licence
to skip: "we'll do it with the next change to this file" is not an answer,
because that change is not scheduled and the session that holds the context
will not exist then.

So each posted point ends in exactly one of three ways, before the PR
merges:

1. **Fixed** — push the fix, reply in one line, `Fixed in <sha>: <what
   changed>`. The reasoning belongs in the commit message, where it stays
   with the code; the reviewer verifies the commit, not the reply.
2. **Declined** — reply with the reason it stays as it is. A reason, not a
   deferral.
3. **Split out** — only for real work that genuinely does not belong in this
   PR: open the issue **now**, link it from the reply. "Follow-up issue"
   without an issue is not an outcome.

Resolve the thread once it is answered, whichever of the three it was. One
summary comment per round beats one essay per thread.
