---
name: squad-issue
description: Use when the user asks to fix a bug reported in a specific GitHub issue of this repository — not for an issue the user calls a feature or that carries a feature/enhancement label, which goes to squad-spec (.squad/routing.md, Bug or feature). Runs the squad pipeline — Lead plans and picks a tier, the Devil's Advocate challenges the plan, Security reviews the plan when a security area is touched and the diff of security-tier changes, Tester writes failing tests first, Dev implements until the Coverage gate passes, each member formats its files, Code Officer clears analyzer findings, Reviewer reviews, the PR is approved by checklist with the Lead deciding what is open — and opens a PR referencing the issue.
---

# Squad Issue

Fix a bug reported in a GitHub issue with the squad defined in `.squad/`. A feature runs through
`squad-spec` instead (`.squad/routing.md`, *Bug or feature*). You are the **orchestrator**: you launch the
members as subagents, pass their outputs on (they cannot talk to each other), enforce the tiers and loop
limits of [`.squad/routing.md`](../../../.squad/routing.md) and follow the procedures of
[`.squad/orchestration.md`](../../../.squad/orchestration.md) — the rules this procedure points to — and perform
every Git and GitHub action yourself, including follow-up issues the Lead decides on.

- **You never do a member's work.** Production code, tests and the documentation the plan assigns go to their
  owner (production code and documentation → `squad-dev`, tests → `squad-tester`, analyzer-only edits →
  `squad-code-officer`),
  whatever your own checks find. Besides read-only checks you write only:
  - `log.md`, through `squad-log.py` (step 1), and `tasks.md` check marks (features);
  - in `plan.md` (for a feature also `spec.md`): the *Challenge notes* and *Security notes* you append
    verbatim (*Plan challenge*) with `python3 .squad/tools/squad-notes.py <folder> challenge|security --agent-id <id>`
    (`--spec` for `spec.md`; `--stdin` with the report piped in outside Claude Code) and the exact edits such a
    note states for that document, a tier raise (*Tiers*) and an area added to *Security areas touched*
    (*Unlisted security areas*);
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
  with `--stdin` and the comment's log table): it prints the next step with its exact launch — agent, mode, model,
  fresh or continued (*Continuing a member*), hand-over and log command — or your own row, and the gates and review
  set the tier calls for. Follow it, continuing exactly the agent it names (*Continuing a member* in
  `.squad/orchestration.md`), and launching a new agent where it says fresh, a fix round too.
- **Scope:** a product PR touches neither the squad nor the instruction files outside their project blocks
  (*Scope of a product PR*, which `scope-check.py` enforces).
- **One build at a time:** launch the members that build or test one after another (*Concurrency*).
- **Commits:** commit and push as *Commits* and *The stop hook* say; never commit to `main`. Stage with plain
  `git add -A`: ignored paths are skipped anyway, and an exclusion pathspec for an ignored path makes
  `git add` fail and stage nothing.
- **GitHub:** write through the GitHub MCP tools (`mcp__github__*`: issues, comments, labels, pull requests);
  read as `.squad/team.md` (*Reading GitHub*) says.
- **Pull request:** invoking this skill is the user's approval for opening the PR in step 10, once step 9
  approved it (tiers `docs` and `patch`: once the latest review round is clean). It opens as a draft and is
  marked ready in step 11 (*Draft until green*).
- **Session model:** the orchestrating session re-reads its whole context on every turn and is often a run's largest
  cost; it runs on Sonnet with medium effort unless the user chose otherwise, and `session-usage.py` names its share.
  Gate output stays in the file `gate-stamp.py` names: read the summary it prints, not the whole file.
- **Product Manager:** the user is asked only when the Lead returns `RESULT: ESCALATE` (relay the question
  verbatim with its options and wait) or in the other cases *Escalation to the Product Manager* lists.

## Steps

1. **Intake.** Read the issue in full, including comments and labels, and decide bug or feature first
   (*Bug or feature*): a feature switches to `squad-spec` now, before any branch or work folder exists. Note
   the reported environment (versions, image tag or release, host OS, configuration). A closed issue: stop
   and report that. Run `git fetch origin main --tags` (in a shallow clone, `git rev-parse
   --is-shallow-repository` prints `true`: `git fetch --unshallow --tags origin`; a stamp that checks the decision
   records refuses a shallow clone), then start from a clean working tree on a new branch off the latest `main`, e.g.
   `fix-issue-<number>-<short-slug>` (or the branch the session prescribes). A prescribed branch whose pull request
   is already merged is not reused: start a new branch off `main` without a force-push and name it in the intake
   row. Create `specs/issue-<number>/log.md` from `specs/_template/log.md`, commit and push it.

   **Several issues in one request.** By default each issue gets its own run, branch and pull request, one after
   another. Several may share one run: intake then starts with the lowest number (its work folder) and its row lists
   every issue; when the Lead's step-2 result does not cover them all in one tier `docs` or `patch` change and the
   user did not ask for one pull request, the others go back to runs of their own. A shared run's pull request
   carries one `Closes #<number>` per issue, and the working record goes to the first issue with a one-line pointer
   on each other. When the session prescribes a single branch but the issues
   need separate pull requests, ask the user before step 2 of the second issue. `gate-stamp.py --verify` and
   `squad-log.py` read the checked-out branch, `sonar-check.py pr` its `HEAD` unless `--head <sha>` is given: while one
   run waits on its pull request and the next works on another branch in the same clone, run the waiting one's checks
   and step-11 fixes in a worktree of its branch (`git worktree add ../<repository>-<number> <branch>`), and remove it
   once that pull request is merged. Issues that change the same lines, or one that needs another's change, either
   share one run or are stacked on purpose, decided at intake and named in the intake row: each branch starts from the
   previous one and its pull request targets it, the last pull request repeats every `Closes #<number>` (one merged
   into another branch closes nothing), and once the previous pull request is merged, `main` is merged into the next
   branch and its pull request retargeted to `main` before its own merge.

   **Logging.** Only you write `log.md`, one row per step and per launch, always with
   `python3 .squad/tools/squad-log.py issue-<number> "<step>" "<member>" "<result>"` — never by hand.
   - Write a launch's row as soon as its report is in, with `--agent-id <id>` (the agent id the Agent tool
     returned): the script reads the launch's figures from its transcript; never type a number yourself.
   - Log with the step cell and the continuation option (`--continued`, `--continued-from`) `squad-next.py`
     prints, and start the result with the member's `VERDICT:` or `RESULT:` line: the script reads them back.
     Every launch prompt quotes the *Verdict line* `squad-next.py` prints for it; a row logged without that line
     is corrected with `--replace-last <member> --step "<step>" --new-result "<line>, then the result"` when the
     report states it, never with a second row of the same launch.
   - Rows of the Orchestrator, the Product Manager and the User take no metric option; their member cells are
     exactly `Orchestrator`, `Product Manager` and `User`.
   - Outside Claude Code (no transcript) a launch is logged from its task notification instead; that path, and
     what each refusal means: `python3 .squad/tools/squad-log.py --help`.

   After every member's report, commit and push the files it left in the work folder.
2. **Plan.** Launch `squad-lead` in mode `plan` with the issue text and the work folder. It returns one of:
   - `RESULT: DONE` for tier **`docs`** or **`patch`**: record the result, with its tier, as the first plan row of
     `log.md` and run that tier's row of *Tiers* — apply the edits yourself; for `patch` launch `squad-tester` in
     mode `coverage` once when the Lead named a test to add; commit and stamp them (step 7) — then one review
     round (step 8) and step 10.
   - `RESULT: DONE` with `plan.md`: `squad-next.py` runs `plan-check.py` before anyone else reads it and after
     every Lead write, and sends findings back as *Loop limits* (Plan check) says. Then the steps the tier
     requires.
   - `RESULT: NO CHANGE` — as *Outcome "no change"*, and stop.
   - `RESULT: ESCALATE` — ask the user, then relaunch the Lead with the answer.

   **Plan challenge.** `squad-devils-advocate` in mode `plan` with the issue text and the work folder when *Plan
   challenge* calls for it, together with step 3 on the same plan commit when that runs, or the skip row. Route
   the verdicts as *Plan challenge* says. After a raised tier, continue with that tier's steps.
3. **Plan security review** whenever *Security areas touched* is not "none" (*Tiers*): `squad-security` in mode
   `plan`, in parallel with the plan challenge, and its loop as *Loop limits* (Plan ↔ Security) says; a revision
   made only for a major objection is confirmed by Security too.
4. **Skeleton** (only if the plan adds or changes API). Launch `squad-dev` in mode `skeleton` with the body the *Skeleton*
   section of `.squad/stack.md` names, quoted in the prompt: the planned signatures and the existing test call sites
   the plan assigns to the Dev. A plan that declares the change free of production and test code skips steps 4 and 5
   (*Changes without production or test code*).
5. **Tests first** (not for `trivial` or `tests`). Launch `squad-tester` in mode `tests-first`. Confirm yourself
   that the new tests compile and fail on the current code (unless the Tester justified why one cannot); the tests
   it hands back as **deferred** go into its log row as its `Deferred:` line (`Deferred: none` without one), which
   `squad-next.py` holds the coverage pass open on. A fix without a reproducing test is acceptable only when the bug
   genuinely needs a live external system — then the PR says so.
6. **Implement and cover.** For a change without production or test code this is the Dev's edits and the
   Tester's share the plan names (*Changes without production or test code*). Otherwise launch `squad-dev` in mode `implement` with the plan and
   the test names. If the Dev disputes a test, launch `squad-lead` in mode `decide`; the Tester changes a test only if the Lead says
   so. Run *Test with coverage* and the *Coverage gate* yourself. Launch `squad-tester` in mode `coverage`,
   with the reason, only when the gate fails, the Dev reports uncovered new lines, a resource bound of the
   plan has no test yet, a deferred test or test documentation is open (also when the gate passes) or a reviewer
   asks for a test;
   otherwise log why the pass is skipped. Repeat until the gate passes. Then, when the plan lists documentation
   updates not written yet, continue the Dev's `implement` agent for them (*Documentation* in `squad-dev.md`;
   test documentation: the Tester in mode `coverage`), logged as a step-6 continuation, and run *Test* again. Before
   step 7, `squad-next.py` runs `plan-check.py --matrix` and sends a `MISSING` criterion to `squad-tester` in mode
   `coverage`. Lines
   reported as not unit-testable go to `squad-lead` in mode `decide`.
7. **Code check.** `git fetch origin main` and merge `main` first when it moved (*Merge conflicts*). Commit, then
   run `python3 .squad/tools/gate-stamp.py <folder>` — in the background (Bash `run_in_background`) when it runs
   *Test with coverage*, which can outlast a foreground command's time limit — (`patch`: with `--check "<command>"` for the check the Lead's
   result names; `--project NAME=VALUE` for the test project of *Test (project)*). Route what it reports as the
   *Pipeline* row says: a `figure-check.py` hit to `squad-dev` in mode `fix`, with each hit; a *Format check*
   failure by *Format* and `git restore -- <file>` of every file it changed outside the change; analyzer findings to
   `squad-code-officer` with the base ref, its structural items to `squad-dev` (or `squad-tester`), a scope finding
   to its owner, a file it formatted outside the change restored. Commit and stamp again until it passes; a
   verbatim documentation fix is stamped with `--verbatim` (*Verbatim documentation fixes*). When the plan asks for
   an *Early draft PR*, open it once the stamp passes, as that section says (`create-pr` step 8 without its review
   loop), and subscribe to its activity.
8. **Review.** Launch the review set `squad-next.py` prints, in parallel, with the base ref, the stamp's code
   SHA, the work folder (tiers `docs` and `patch`: the first `log.md` row) and the stamp's gate output; tell
   them that later commits may only touch `specs/`, and avoid commits while the round runs (*The stop hook*). A
   report for another code head is a stale round (`squad-next.py` compares the SHA of the Reviewer's verdict line
   with the stamp). Then:
   - a tier raise: *Tiers*; an *unlisted security area*: *Unlisted security areas*; a fix marked `verbatim`:
     *Verbatim documentation fixes*; an `unopened` link: *Pages the session cannot open*;
   - blocking findings → their owner (`squad-dev` for code and documentation, `squad-tester`) → steps 6 and 7 again (tiers `docs` and
     `patch`: you fix them and stamp again) → **a delta round, mandatory**; at most 2 fix rounds, then
     `squad-lead` in mode `decide` (*Loop limits*);
   - non-blocking findings: *Non-blocking findings* — those without a `verbatim` fix go to `squad-lead` in
     mode `decide` now, before the round's fix round, and a fix it orders goes to its owner with the blocking ones.

   A reviewer or `squad-security` that stops on an API error (`529 Overloaded`; `429`: once the limit has reset) is relaunched
   unchanged, at most twice; after that report a blocker to the user. A safeguard stop is not relaunched (*Loop
   limits*, Safeguard stop).
9. **PR approval.** Run *PR approval* (`.squad/routing.md`): the checklist in full, then the record status
   and `decision-check.py`. Launch `squad-lead` in mode `approve-pr` — with the base ref, the gate output, the
   review outcome and the open points of *PR
   approval*, listed one by one (after the PR also the finished check-run list) — only when that section leaves
   a decision open; otherwise log "approved by checklist".
10. **Pull request.** With an *Early draft PR* the open PR is updated as below instead of opened. With a
    `plan.md`, run `python3 .squad/tools/plan-check.py <folder> --matrix` (a `MISSING` row sends the change back
    to step 6) and fill in its results: each `to record` from the log (the manual test, the check), each `after
    merge` with a linked verification issue you create now, each `pending` CI row once the PR's checks have
    finished (step 11). Post the matrix, not collapsed, then
    `plan.md` (none on tiers `docs` and `patch`), `measurements.md` when the run has one, and `log.md` as one
    comment on the issue, headed "Squad working record", each inside a collapsed `<details>` block — or, for any
    document except the matrix (the comment tool takes its body as typed text, so a long document would be re-typed),
    a permalink to the last commit that contains it
    (`https://github.com/<owner>/<repo>/blob/<sha>/specs/issue-<number>/plan.md`) plus a summary (for `plan.md`:
    tier, criteria, decisions and challenge outcome). A `log.md` that does not fit the comment (GitHub takes 65,536
    characters) must be linked this way; a linked `log.md` makes the comment's log table start with the step-10 row:
    `squad-log.py` and `squad-next.py` with `--stdin` read the earlier rows from that commit. Then `git rm -r specs/issue-<number>/`, confirm with
    `python3 .squad/tools/scope-check.py --no-specs`, commit ("Remove squad working record") and push. Later
    log rows go into that comment: pipe its log table into `squad-log.py … --stdin` and paste the log it prints
    back (nothing is restored into the repository). Open the PR as `create-pr` step 8 says, without its review loop; a PR the
    session already has for the branch (`list_pull_requests` with `head: "<owner>:<branch>"`) gets its title
    and body updated instead (`update_pull_request`, `draft: true`). The body describes the bug, the fix and
    the reproducing test (or that the fix is not verifiable without a real external system), `Closes
    #<number>` under Issues and the decision records, the matrix as *Test Plan*; Next Steps lists **only linked
    GitHub issues** (create them now, the verification issues included) or "None".
11. **After the PR.** Subscribe to the PR's activity right away (`subscribe_pr_activity`; without it, check CI
    and the code analysis yourself before finishing) — a session that ends with an unwatched PR has not
    completed this step. Stay with the PR until *Draft until green* holds, then mark it ready. Route:
    - a merge conflict → *Merge conflicts*, before anything else;
    - code-analysis findings (`sonar-check.py pr <number>` lists them) → `squad-code-officer` (structural
      ones → `squad-dev`);
    - failing build or tests → `squad-dev` (test defects → `squad-tester`);
    - review comments (human, automated, `review-pr`) → `squad-dev`, worked in this PR, blocking or not.

    Each fix goes through steps 7–8 again (the stamp with `--stdin`, a delta review), logged at step 11 like every
    launch (`squad-next.py --launch squad-<role> <mode>` prints the exact launch), within the limit of *Loop limits*,
    and gets the re-approval of step 9. The work folder is gone: give the Reviewer, Security and the Lead
    the plan (tiers `docs` and `patch`: the first log row; for a feature also `spec.md`, `tasks.md`) from the
    working-record comment or `git show
    <commit-before-removal>:specs/<folder>/<file>`, and edit that comment in one batch. Never skip, disable or
    weaken a test to get green.
12. **Wrap-up (mandatory, also when the PR was merged or closed before it was marked ready: *Draft until green*).**
    Collect what this run taught about the squad itself — an unclear or
    contradictory rule, a misbehaving tool, an agent that could not be launched, a step you had to improvise —
    each with the role it concerns and a concrete proposal, and check your log for the lesson candidates of *Squad
    lessons* (`squad-log.py … --summary` lists those it finds while the lessons row is missing). File the lessons
    and log the lessons row as *Squad lessons* says, and link them from the working-record comment. Run
    `python3 .squad/tools/squad-log.py issue-<number> --summary` (`--stdin` with the comment's log table) until it
    exits 0, completing what another exit code names (`--help`): launch rows without usage, commits after the PR
    without a step-11 delta review or verbatim row, the lessons row, a passing gate stamp (step 7). Append the tables
    exactly as printed (a run without them is not wrapped up) to the comment under "Squad run metrics", then the
    output of `python3 .squad/tools/session-usage.py` as printed under "Session usage". Report the branch, the PR URL, the tier, the lesson issues (or "no lessons"),
    the metrics tables and any escalation or Lead decision to the user.

## Notes

- The PR documents the change, not how it was produced (`AGENTS.md`, *Related skills*).
- Text you write or copy verbatim (notes, verbatim fixes, a plan table) keeps its escapes: `.squad/team.md`,
  *Escapes in files*.
- Prefer non-interactive commands only. If push or PR creation fails, stop and report it.
- Never close the issue manually; `Closes #<number>` closes it on merge.
