---
name: squad-issue
description: Use when the user asks to fix a bug reported in a specific GitHub issue of this repository — not for an issue the user calls a feature or that carries a feature/enhancement label, which goes to squad-spec (.squad/routing.md, Bug or feature). Runs the squad pipeline — Lead plans and picks a tier, the Devil's Advocate challenges the plan, Security reviews the plan when a security area is touched and the diff of security-tier changes, Tester writes failing tests first, Dev implements until the Coverage gate passes, each member formats its files, Code Officer clears analyzer findings, Reviewer reviews, the PR is approved by checklist with the Lead deciding what is open — and opens a PR referencing the issue.
---

# Squad Issue

Fix a bug reported in a GitHub issue with the squad defined in `.squad/`. You are the **orchestrator** the first
paragraph of [`.squad/routing.md`](../../../.squad/routing.md) describes: its tiers and loop limits and the procedures
of [`.squad/orchestration.md`](../../../.squad/orchestration.md) are the rules this procedure points to, and you
create the follow-up issues the Lead decides on.

- **You never do a member's work.** Production code, tests and the documentation the plan assigns go to their
  owner (production code and documentation → `squad-dev`, tests → `squad-tester`, analyzer-only edits →
  `squad-code-officer`), whatever your own checks find. Besides read-only checks you write only:
  - `log.md`, through `squad-log.py` (step 1), and `tasks.md` check marks (features);
  - in `plan.md` (for a feature also `spec.md`): the *Challenge notes* and *Security notes* you append
    verbatim (*Plan challenge*) with the `squad-notes.py` command `squad-next.py` prints (`--stdin` with the
    report piped in outside Claude Code) and the exact edits such a note states for that document, a tier raise
    (*Tiers*) and an area added to *Security areas touched* (*Unlisted security areas*);
  - the Lead's exact edits on tiers `docs` and `patch` (*Tiers*), a fix marked `verbatim` (*Verbatim
    documentation fixes*), *Format* on the files of the change (step 7), merge commits (*Merge conflicts*),
    and the record status of step 9 (*PR approval*);
  - the exact text of a `.squad/stack.md` or `.squad/project.md` edit its owning member reports (`.squad/team.md`,
    *Files*), and the commit and revert of a dry-run patch (*Dry-run artifacts*).
- **Hand each member only what it needs:** the plan (for a feature: its task rows and `spec.md`), the test
  names, the base ref, the code SHA and the gate output for the head. Not the log, not the issue thread (the
  Lead and the Devil's Advocate read the issue themselves), not earlier reports in full — one line on what the
  previous member handed back is enough.
- **Next step:** after every row of `log.md`, run `python3 .squad/tools/squad-next.py <folder>` (after step 10
  with `--stdin` and the comment's log table): it prints the next step — its exact launch (agent, mode, model, fresh
  or continued, hand-over, verdict line, log command) or your own row — and the gates and review set due. Follow
  it: continue exactly the agent it names, and launch a new one where it says fresh, a fix round too.
- **Scope:** as *Scope of a product PR* says, which `scope-check.py` enforces.
- **One build at a time:** launch the members that build or test one after another (*Concurrency*).
- **Commits:** commit and push as *Commits* and *The stop hook* say; never commit to `main`. Stage with plain
  `git add -A`: an exclusion pathspec for an ignored path makes `git add` fail and stage nothing.
- **GitHub:** write through the GitHub MCP tools (`mcp__github__*`: issues, comments, labels, pull requests);
  read as `.squad/team.md` (*Reading GitHub*) says.
- **Pull request:** invoking this skill is the user's approval for opening the PR in step 10, once step 9
  approved it (tiers `docs` and `patch`: once the latest review round is clean).
- **Session model:** the orchestrating session runs on Sonnet with medium effort unless the user chose otherwise,
  and `session-usage.py` names its share. Gate output stays in the file `gate-stamp.py` names: read the summary it
  prints, not the whole file.
- **Product Manager:** the user is asked only when the Lead returns `RESULT: ESCALATE` (relay the question
  verbatim with its options and wait) or in the other cases *Escalation to the Product Manager* lists.

## Steps

1. **Intake.** Read the issue in full, including comments and labels, and decide bug or feature first (*Bug or
   feature*): a feature switches to `squad-spec`, a verification issue stops here. A closed issue: stop and report
   that. Note the reported environment (versions, image tag or release, host OS, configuration). Run
   `git fetch origin main --tags` (`git fetch --unshallow --tags origin` when `git rev-parse --is-shallow-repository`
   prints `true`: a stamp that checks the decision records refuses a shallow clone), then start from a clean working
   tree on a new branch off the latest `main`, e.g. `fix-issue-<number>-<short-slug>`, or the branch the session
   prescribes — unless its pull request is already merged: then a new branch off `main`, without a force-push, named
   in the intake row. Create `specs/issue-<number>/log.md` from `specs/_template/log.md`, commit and push it.

   **Several issues in one request.** By default each issue gets its own run, branch and pull request, one after
   another. Several may share one run: intake starts with the lowest number (its work folder), and its row lists every
   issue; unless the Lead's step-2 result covers them all in one tier `docs` or `patch` change or the user asked for
   one pull request, the others go back to runs of their own. A shared run's pull request carries one
   `Closes #<number>` per issue; its working record goes to the first issue, with a one-line pointer on each other.
   When the session prescribes a single branch but the issues need separate pull requests, ask the user before step 2
   of the second issue. While one run waits on its pull request and the next works on another branch in the same
   clone, run the waiting one's checks and step-11 fixes in a worktree of its branch
   (`git worktree add ../<repository>-<number> <branch>`, removed once that pull request is merged):
   `gate-stamp.py --verify` and `squad-log.py` read the checked-out branch, `sonar-check.py pr` its `HEAD` unless
   `--head <sha>` is given. Issues that change the same lines, or one that needs another's change, share one run or
   are stacked, decided at intake and named in the intake row: each branch starts from the previous one and its pull
   request targets it; the last pull request repeats every `Closes #<number>` (one merged into another branch closes
   nothing); once the previous pull request is merged, merge `main` into the next branch and retarget its pull request
   to `main` before its own merge.

   **Logging.** Only you write `log.md`, one row per step and per launch, always with
   `python3 .squad/tools/squad-log.py issue-<number> "<step>" "<member>" "<result>"` — never by hand:
   - log a launch as soon as its report is in, with the command `squad-next.py` printed for it (step cell,
     `--agent-id <id>` the Agent tool returned, continuation option): the script reads the launch's figures from its
     transcript; never type a number yourself;
   - start the result with the member's `VERDICT:` or `RESULT:` line, which the script reads back; a row without it
     is corrected as `squad-next.py` then prints (`--replace-last`), never with a second row of the same launch;
   - rows of the Orchestrator, the Product Manager and the User take no metric option; their member cells are
     exactly `Orchestrator`, `Product Manager` and `User`;
   - outside Claude Code (no transcript) a launch is logged from its task notification instead; that path, and
     what each refusal means: `python3 .squad/tools/squad-log.py --help`.

   After every member's report, commit and push the files it left in the work folder.
2. **Plan.** Launch `squad-lead` in mode `plan` with the issue text and the work folder. It returns one of:
   - `RESULT: DONE` for tier **`docs`** or **`patch`**: log it, with its tier, as the first plan row and run that
     tier's row of *Tiers* (the Tester's one test in mode `coverage`; stamp as step 7, review as step 8), then step 10.
   - `RESULT: DONE` with `plan.md`: the steps its tier requires; `squad-next.py` runs `plan-check.py` after every
     Lead write, before anyone else reads the plan, and sends findings back as *Loop limits* (Plan check) says.
   - `RESULT: NO CHANGE` — as *Outcome "no change"*, and stop.
   - `RESULT: ESCALATE` — ask the user, then relaunch the Lead with the answer.

   **Plan challenge.** `squad-devils-advocate` in mode `plan` with the issue text and the work folder when *Plan
   challenge* calls for it, or the skip row. Route the verdicts as *Plan challenge* says. After a raised tier,
   continue with that tier's steps.
3. **Plan security review** whenever *Security areas touched* is not "none" (*Tiers*): `squad-security` in mode
   `plan`, in parallel with the plan challenge, and its loop as *Loop limits* (Plan ↔ Security) says; a revision
   made only for a major objection is confirmed by Security too.
4. **Skeleton** (only if the plan adds or changes API). Launch `squad-dev` in mode `skeleton` with the body the
   *Skeleton* section of `.squad/stack.md` names, quoted in the prompt: the planned signatures and the existing test
   call sites the plan assigns to the Dev; a planned file the skeleton left unchanged goes back to the Dev
   (`squad-next.py` names it). A plan that declares the change free of production and test code skips steps 4 and 5
   (*Changes without production or test code*).
5. **Tests first** (not for `trivial` or `tests`). Launch `squad-tester` in mode `tests-first`. Confirm yourself
   that the new tests compile and fail on the current code (unless the Tester justified why one cannot); the tests
   it hands back as **deferred** go into its log row as `squad-next.py` says. A fix without a reproducing test is
   acceptable only when the bug genuinely needs a live external system — then the PR says so.
6. **Implement and cover.** For a change without production or test code this is the Dev's edits and the
   Tester's share the plan names (*Changes without production or test code*). Otherwise launch `squad-dev` in mode
   `implement` with the plan and the test names. If the Dev disputes a test, launch `squad-lead` in mode `decide` (a
   fresh agent: `squad-next.py`'s step-6 output names the launch); the Tester changes a test only if the Lead says so.
   Run *Test with coverage* and the *Coverage gate* yourself. Launch `squad-tester` in mode `coverage`, with the
   reason, only for what the *Pipeline* row 6 names or open test documentation (also when the gate passes); otherwise
   log why the pass is skipped. Repeat until the gate passes. Then continue the Dev's `implement` agent for the
   plan's documentation updates not written yet (*Documentation* in `squad-dev.md`; test documentation: the Tester in
   mode `coverage`), logged as a step-6 continuation, and run *Test* again. Lines or branch conditions reported as
   not unit-testable go to `squad-lead` in mode `decide`, also when the gate passes; `squad-next.py` launches a `FIX:` it orders.
7. **Code check.** `git fetch origin main` and merge `main` first when it moved (*Merge conflicts*). Commit, then
   run `python3 .squad/tools/gate-stamp.py <folder>` as `squad-next.py`'s *Gates due* lines say (in the background,
   `--check`, `--project`). Route what it reports as the *Pipeline* row says (a `figure-check.py` hit to `squad-dev`
   in mode `fix`, analyzer findings to `squad-code-officer` with the base ref, a scope finding to its owner). Commit
   and stamp again until it passes; a verbatim documentation fix is stamped with `--verbatim` (*Verbatim
   documentation fixes*). An *Early draft PR* the plan asks for opens once the stamp passes, as that section and
   `squad-next.py` say.
8. **Review.** Launch the review set `squad-next.py` prints, in parallel, with the base ref, the stamp's code SHA,
   the work folder (tiers `docs` and `patch`: `result.md`) and the stamp's gate output; tell them that later commits
   may only touch `specs/`, and avoid commits while the round runs (*The stop hook*). Then:
   - a tier raise: *Tiers*; an *unlisted security area*: *Unlisted security areas*; a fix marked `verbatim`:
     *Verbatim documentation fixes*; an `unopened` link: *Pages the session cannot open*;
   - blocking findings → their owner → steps 6 and 7 again (tiers `docs` and `patch`: you fix them and stamp
     again) → **a delta round, mandatory**, within *Loop limits*;
   - non-blocking findings: *Non-blocking findings*.

   A reviewer or `squad-security` that stops on an API error (`529 Overloaded`; `429`: once the limit has reset) is
   relaunched unchanged, at most twice; after that report a blocker to the user. A safeguard stop is not relaunched
   (*Loop limits*, Safeguard stop).
9. **PR approval.** Run *PR approval* (`.squad/routing.md`). A `squad-lead` launch in mode `approve-pr` gets the
   base ref, the gate output, the review outcome and the open points one by one (after the PR also the finished
   check-run list).
10. **Pull request.** With a `plan.md`, run `python3 .squad/tools/plan-check.py <folder> --matrix` and fill in its
    results: each `to record` from the log (the manual test, the check), each `after merge` with a linked verification
    issue you create now, each `pending` CI row once the PR's checks have finished (step 11). Post as one comment on
    the issue, headed "Squad working record", the matrix, not collapsed, then `plan.md` (tiers `docs` and `patch`:
    `result.md`), `measurements.md` when the run has one and `log.md`, each in a collapsed `<details>` block — or, for
    any document but the matrix, a permalink to the last commit that contains it
    (`https://github.com/<owner>/<repo>/blob/<sha>/specs/issue-<number>/plan.md`) with a summary (for `plan.md`: tier,
    criteria, decisions and challenge outcome). A `log.md` too long for the comment (GitHub takes 65,536 characters)
    must be linked; the comment's log table then starts with the step-10 row, and `--stdin` reads the earlier rows
    from that commit. Then remove the work folder as `squad-next.py` prints (`git rm -r`, `scope-check.py --no-specs`,
    commit "Remove squad working record", push). Later log rows go into that comment: pipe its log table into
    `squad-log.py … --stdin` and paste the log it prints back (nothing is restored into the repository). Open the PR
    as `create-pr` step 8 says, without its review loop; a PR the session already has for the branch
    (`list_pull_requests` with `head: "<owner>:<branch>"`) gets its title and body updated instead
    (`update_pull_request`, `draft: true`). The body describes the bug, the fix and the reproducing test (or that the
    fix is not verifiable without a real external system), `Closes #<number>` under Issues and the decision records,
    the matrix as *Test Plan*; Next Steps lists **only linked GitHub issues** (create them now, the verification
    issues included) or "None".
11. **After the PR.** Subscribe to the PR's activity at once (`subscribe_pr_activity`; without it, check CI and the
    code analysis yourself before finishing): a session that ends with an unwatched PR has not completed this step.
    Stay with the PR until *Draft until green* holds, then mark it ready. Route a merge conflict first (*Merge
    conflicts*); code-analysis findings (`sonar-check.py pr <number>`) to `squad-code-officer`, structural ones to
    `squad-dev`; a failing build or test to `squad-dev`, test defects to `squad-tester`; review comments (human,
    automated, `review-pr`) to `squad-dev`, worked in this PR, blocking or not. Each fix goes through steps 7–8 again
    (the stamp with `--stdin`, a delta review) within *Loop limits*, logged at step 11 with the launch
    `squad-next.py --launch squad-<role> <mode>` prints, and gets the re-approval of step 9. The work folder is gone:
    hand the Reviewer, Security and the Lead the plan (tiers `docs` and `patch`: `result.md`; a feature also
    `spec.md`, `tasks.md`) from the working-record comment or
    `git show <commit-before-removal>:specs/<folder>/<file>`, and edit that comment in one batch. Never skip, disable
    or weaken a test to get green.
12. **Wrap-up (mandatory, also when the PR was merged or closed before it was marked ready: *Draft until green*).**
    Collect what this run taught about the squad itself — an unclear or contradictory rule, a misbehaving tool, an
    agent that could not be launched, a step you had to improvise — each with the role it concerns and a concrete
    proposal; file and log the lessons as *Squad lessons* says and link them from the working-record comment. Run
    `python3 .squad/tools/squad-log.py issue-<number> --summary` (`--stdin` with the comment's log table) until it
    exits 0, completing what another exit code names (`--help`); append its tables, then the output of
    `python3 .squad/tools/session-usage.py`, exactly as printed to the comment under "Squad run metrics" and "Session
    usage" (a run without them is not wrapped up). Report the branch, the PR URL, the tier, the lesson issues (or "no
    lessons"), the metrics tables and any escalation or Lead decision to the user.

## Notes

- The PR documents the change, not how it was produced (`AGENTS.md`, *Related skills*).
- Text you write or copy verbatim (notes, verbatim fixes, a plan table) keeps its escapes: `.squad/team.md`,
  *Escapes in files*.
- Prefer non-interactive commands. If push or PR creation fails, stop and report it.
- Never close the issue manually; `Closes #<number>` closes it on merge.
