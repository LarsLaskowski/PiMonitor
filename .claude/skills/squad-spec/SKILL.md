---
name: squad-spec
description: Use when the user wants to develop a new feature in this repository spec-driven with the squad — from a description, or from a GitHub issue the user calls a feature (implement feature 12) or that carries a feature/enhancement label (.squad/routing.md, Bug or feature). The Lead writes the spec first and the Devil's Advocate challenges it before any plan exists; then the Lead writes plan and tasks against the spec's acceptance criteria and picks a tier, Security reviews the plan when a security area is touched and the diff of security-tier changes, Tester writes failing tests first, Dev implements until the Coverage gate passes, each member formats its files, Code Officer clears analyzer findings, Reviewer reviews, the PR is approved by checklist with the Lead deciding what is open, then a PR is opened.
---

# Squad Spec

Build a feature with the squad defined in `.squad/`, **spec first**: what the feature does for its users is
written, challenged and settled in `spec.md` before anyone plans how to build it; `plan.md` and `tasks.md` refer to
its acceptance criteria by id. You are the
**orchestrator**, with the same role and rules as in the `squad-issue` skill (its introduction) — read it
first. Steps 1–3 below replace its steps 1–3 (step 2 in two parts, 2a *Spec* and 2b *Plan*: use those step
cells in `log.md`); steps 4–12 follow `squad-issue` with the changes listed after them.

## Steps 1–3

1. **Intake.** With a GitHub issue, read it in full, including comments and labels, and decide bug or feature
   (`.squad/routing.md`, *Bug or feature*) — the user calling it a feature or invoking this skill settles
   it; a bug fix switches to `squad-issue` now. A closed issue: stop and report that. A run that waits for
   an outside author's agreement or the Product Manager's spec approval continues on its work branch
   (`.squad/orchestration.md`, *Outside authors*, *Spec approval*). Run
   `git fetch origin main --tags` (unshallowing a shallow clone as `squad-issue` step 1 says), create the branch off the latest `main` (or the branch the session
   prescribes, unless its pull request is merged: `squad-issue` step 1) and the work folder
   `specs/feature-<short-slug>/` (from an issue
   `specs/feature-<number>-<short-slug>/`) with `log.md` from `specs/_template/log.md`; commit and push. Log
   as `squad-issue` step 1 says.
2a. **Spec.** Launch `squad-lead` in mode `spec` with the issue text (or the user's description) and the work
   folder. It writes `spec.md` from `specs/_template/spec.md` and returns `RESULT: DONE`, `RESULT: NO CHANGE`
   (the feature exists already or contradicts an accepted decision: with an issue, as *Outcome "no change"*;
   otherwise report it to the user) or `RESULT: ESCALATE` (log its row, relay the question verbatim, then continue the
   Lead with the answer; a round logged late takes `squad-log.py --round N`). `squad-next.py` runs `plan-check.py --spec` after every Lead write and sends findings back as
   *Loop limits* (Plan check) says, and its size warning once: the Lead shortens the spec, or keeps it and gives the
   reason in its result, which you log. A small feature's spec (mode `spec`
   in `squad-lead.md`) runs the same steps. A feature whose change is documentation only gets `RESULT: DONE` with
   tier `docs` and no `spec.md`: run the `docs` route of `squad-issue` step 2 instead of the rest of steps 2–3
   (the working record is `log.md` alone).

   **Spec challenge.** Launch `squad-devils-advocate` in mode `spec` with the issue text and the work folder,
   for every feature. Minor objections only: append them verbatim to `spec.md` as a *Challenge notes*
   section (`squad-notes.py <folder> challenge --spec --agent-id <id>`); the Lead answers them while it plans. A
   major objection: continue `squad-lead` in mode `revise`
   on the spec with all objections; it answers each in the spec's *Challenge* section and may narrow the
   scope, escalate or return `RESULT: NO CHANGE`. One round, no veto (*Loop limits*). An issue by an outside
   author: its spec is agreed on the issue now (*Outside authors*).

   **Spec approval.** Before step 2b, run *Spec approval* (`.squad/orchestration.md`) with the value of
   `SPEC_APPROVAL` in `.squad/tools/squad_settings.py`; where it calls for the Product Manager, step 2b starts
   only after the answer.
2b. **Plan.** Continue the Lead's last launch in mode `plan` with the work folder (*Continuing a member*): it
   closes the spec (answers its *Challenge notes*, sets its status) and writes `plan.md` and `tasks.md`. A
   feature is never `patch` (`docs`: step 2a). `squad-next.py` runs `plan-check.py` after every Lead write, before
   anyone else reads the plan; findings as *Loop limits* says. Then the **plan challenge** with
   `squad-devils-advocate` in mode `plan` when *Plan challenge* calls for it (the verdict `plan-check.py`
   prints), routed as `squad-issue` step 2 says.
3. **Plan security review** whenever *Security areas touched* is not "none": `squad-security` in mode `plan`
   reviews `spec.md` and `plan.md` together, in parallel with the plan challenge, and the loop of
   `squad-issue` step 3 applies.

## Steps 4–12: as in `squad-issue`, with these changes

- **Steps 4–6** run per task group — the *Group* column of `tasks.md`, in table order; tick tasks off as they
  are done. The Tester's tests first are the tests *Tests per criterion* names for the group's
  criteria. `squad-next.py` prints each group's launches; the step cell names the group (`4 Skeleton (G2)`). The
  documentation rows are the Dev's, done once the last group's *Coverage gate* passes (`squad-issue`
  step 6; *Documentation* in `squad-dev.md`).
- **Code-free changes:** as *Changes without production or test code*; *Verification without tests* then
  covers each criterion of the spec.
- **Amendments** (steps 3–11): a Lead decision that changes, adds or drops a criterion amends the spec (mode
  `decide` in `squad-lead.md`); run `plan-check.py <folder>` on it before any member reads it, and hand later
  launches the amended spec.
- **Step 10:** the working record (the verification matrix of the spec's criteria, `spec.md`, `plan.md`,
  `tasks.md`, `measurements.md` when the run has one, `log.md`; on the `docs` route `log.md` alone) is posted to the feature request issue if one exists
  (`Closes #<n>` in the PR), otherwise as the first comment on the PR right after opening it; every log row and
  amend after it goes through `squad-log.py … --stdin` on that comment's log table. Any document but the
  verification matrix may go as a permalink to the commit before the removal, with a summary, as `squad-issue` step 10 says.
