# Routing

The pipeline is the same for bug fixes and features; only the input differs (a bug reported in a GitHub
issue for `squad-issue`, a feature — described by the user or in a GitHub issue — plus `spec.md` for
`squad-spec`; see *Bug or feature*). The orchestrator — the session that runs the skill — launches the
members, passes documents between them (subagents cannot talk to each other directly), performs every Git and
GitHub action and records every step and launch in `log.md` (`squad-issue`, step 1). Step numbers below are the
ones the skills use.

The orchestrator's own procedures are in [`orchestration.md`](orchestration.md), which the members need not read;
*Outside authors*, *Squad lessons*, *Draft until green*, *Merge conflicts*, *Verbatim documentation fixes*, *An
artifact the Product Manager cannot build* and *Unlisted security areas*, named here, are its sections.

## Bug or feature

A GitHub issue is either a bug fix (`squad-issue`) or a feature (`squad-spec`). The orchestrator decides
which before step 1 — before any branch or work folder exists — and the first rule that applies wins:

1. **The user's request.** Calling it a feature ("implement feature #12", in any language) or invoking
   `squad-spec` makes it a feature; calling it a bug ("fix bug #12") or invoking `squad-issue` makes it a
   bug fix — whatever the issue's labels or text say.
2. **Label or issue type.** A label `feature` or `enhancement` (case-insensitive, also as the last part of
   a scoped label such as `type: feature` or `kind/enhancement`) or the GitHub issue type *Feature* makes
   it a feature; a label `bug` or the issue type *Bug* makes it a bug fix. Both kinds at once: ask the user.
3. **Otherwise** it is a bug fix with `squad-issue`. The issue text alone does not decide it: a feature
   request without label is labelled or called a feature by the user.

When the kind points to the other skill, switch to it now and say so in one line. The intake row in
`log.md` names the kind and what decided it (`feature: user request`, `feature: label enhancement`,
`bug: no feature label`).

An issue whose change needs values only the Product Manager can produce (measurements on a real system, figures from
a production host) runs with the skill its kind decides, but the Product Manager supplies the values before step 1;
the orchestrator asks for them (*Escalation to the Product Manager*) and creates no branch until they are in. The
Lead writes them, as given, into `measurements.md` in the work folder, which step 10 posts with the working record.

## Work folder

- Issue (bug fix): `specs/issue-<number>/` — `plan.md`, `log.md` (and `measurements.md`, *Bug or feature*)
- Feature: `specs/feature-<short-slug>/`, from an issue `specs/feature-<number>-<short-slug>/` — `spec.md`,
  `plan.md`, `tasks.md`, `log.md` (and `measurements.md`, *Bug or feature*)
- The work folder lives **only on the work branch**: it is committed and pushed after every step so a
  crashed session can resume, and in step 10 its content is posted as a "Squad working record" comment
  (on the issue, or on the PR for a feature without issue) and the folder is removed before the PR opens.
  `main` never contains `specs/` working records.
- What lasts is written elsewhere: the *why* in decision records (`docs/decisions/`), the *what* in the area
  documents (`docs/areas/`) the plan names under *Areas* (`AGENTS.md`).

## Scope of a product PR

An issue or feature PR changes the product and its documentation only. It never touches the squad or the
agent instructions: `.squad/` (`team.md`, `routing.md`, tools), `.claude/`, `CLAUDE.md`, and `AGENTS.md` and
`.github/copilot-instructions.md` outside their project blocks.
Squad lessons are filed in step 12 (*Squad lessons*), never fixed in the product PR.
`python3 .squad/tools/scope-check.py` reports any such file in step 7's stamp and the Reviewer's step 8; a finding
is blocking.

One exception: `.squad/stack.md`, `.squad/project.md` and the project blocks of `AGENTS.md` describe the
product, not the squad. `.squad/tools/squad_settings.py` is the repository's own settings file and a product PR may
change it too (a coverage exclusion, *Code a unit test cannot cover*; a threshold is never lowered). A product PR updates them when the change itself makes them untrue — a new build command,
a new security area, a new guarantee or coupling point — without an escalation, and the Reviewer treats a stale
entry there like stale documentation. A project block of `AGENTS.md` changes identically in
`.github/copilot-instructions.md` (`scope-check.py` compares them).

## Tiers

In step 2 the Lead picks the tier on the change, not only on the paths it touches, and justifies it in
`plan.md` (`docs`, `patch`: in its result); in doubt about what the change does, the higher one (`docs` <
`patch` < `tests` < `trivial` < `standard` < `security`) — not in doubt about the definition, which the table settles. Security or the Reviewer may raise it at any point, never lower it. A raise to
`security` after step 2 adds only what is still ahead — Security's diff review in step 8, and its plan review
when step 3 has not run and step 4 has not started — and does not re-run the plan challenge; the orchestrator
records it in `plan.md` (with the area that made it) and launches the Lead only when it leaves a decision open.

Every `plan.md` names its **Security areas touched**: each security area of `.squad/project.md`, Docker/CI or build
configuration and dependency the change touches, or exactly "none" (never on tier `security`). Whenever it is not
"none", Security reviews the plan (step 3), whatever the tier. Security's diff review (step 8) is for tier
`security` and for `patch/security` (the `patch` row) alone; below it the Reviewer is the diff's security
review, and an area it finds touched but not listed goes to *Unlisted security areas*.

| Tier | When | Pipeline |
| ---- | ---- | -------- |
| `docs` | Issues, and a feature whose change is documentation only (`squad-spec` step 2a). The diff changes only product documentation: `README.md`, `SECURITY.md`, Markdown under `docs/` except `docs/decisions/` and `docs/areas/`, the project blocks of the marked files, and `.squad/stack.md` or `.squad/project.md` as documentation (below); any other file makes it `trivial` or higher (`scope-check.py --tier docs` checks it). | No `plan.md`: the Lead's result (tier, exact edits, acceptance criteria) is `result.md` in the work folder, written verbatim with `squad-notes.py <folder> result`, its `RESULT:` line the first `log.md` row; the orchestrator applies the edits; `scope-check.py --tier docs` and *Format check* pass; one Reviewer round reads the diff against `result.md` (no build) — a blocking finding is fixed by the orchestrator, checked and delta-reviewed the same way; the PR opens once the latest round is clean. Steps 3–7 and 9 are skipped. |
| `patch` | Issues only. The Lead can name the exact edits, and the diff stays within `PATCH_MAX_LINES` changed lines outside `specs/` (default 30, `squad_settings.py`): no change to production logic or control flow (an attribute, a literal, a condition rewritten equivalently, a configuration value, a script line), no new dependency, permission or schema change. An edit in a security area, a CI or build file makes it `patch/security`. `scope-check.py --tier patch` checks the line budget; the reviewer confirms that no logic changed and raises the tier otherwise. | As `docs`; the result also names, per edit, the test or check that covers it. An edit to production code without one gets the one test the Lead names, written once by `squad-tester` (the only extra launch). Step 7 runs only *Gates of a patch* (*Format* on the orchestrator's edits); the review round is `squad-reviewer`, or on `patch/security` `squad-security` in mode `diff` instead. No plan challenge, Security plan review, skeleton, tests first, Code Officer or step 9. |
| `tests` | The diff changes test code only — the test paths (`COVERAGE_TEST_PATHSPECS` in `squad_settings.py`, by default its `COVERAGE_EXCLUDES`) and `docs/UNIT_TESTS.md` — beyond what `patch` allows; any other file makes it `trivial` or higher (`scope-check.py --tier tests` checks it). | `plan.md` with the criteria and their tests; no plan challenge; step 3 only with *Security areas touched*; steps 4 and 5 skipped, step 6 is `squad-tester` in mode `coverage` writing the plan's tests (no Dev); step 7 runs the *test code only* row of *Gates of a patch* and `scope-check.py --tier tests`, with the Code Officer as usual; Reviewer and PR approval (step 9) still run. |
| `trivial` | Documentation that does not qualify as `docs`, code comments, log or UI wording, configuration defaults, or a documentation change that needs a decision record — no change to behavior or control flow | Steps 4 and 5 skipped (no tests-first), except step 5 of a task group whose first `tasks.md` row names the Tester rather than the Dev (`squad-next.py` names it); no plan challenge; step 3 only with *Security areas touched*; code check, Reviewer and PR approval (step 9) still run. Tests and coverage are still required if production code changes. |
| `standard` | A behavior change that creates, changes or removes no security control (see `security`) — also when it touches a security area, Docker/CI or build configuration, or adds or updates a dependency | Plan challenge in step 2 when *Plan challenge* calls for it; step 3 with *Security areas touched*, in parallel with the challenge. No Security diff review: the Reviewer raises the tier when the diff changes a security control after all |
| `security` | Creates, changes or removes a **security control**: authentication, authorization, sessions or CSRF protection, cryptography, the storage or handling of secrets and credentials, a permission, allow-list, sandbox or other guard that decides what an attacker may reach or do (a check that only keeps malformed or oversized input from breaking the program — a size bound, a parser's validation — is input handling: `standard` with the area under *Security areas touched*; a check that confines what a path, URL, redirect or query reaches, output escaping, or a rate limit against abuse is a guard, also inside a parser), the network exposure (a listener, a route reachable without login, a header or CORS policy), or the trust of the build and release (CI permissions and secrets, signing, provenance) — beyond what `patch/security` allows | Full pipeline; the plan challenge (*Plan challenge*) and the Security plan review run in parallel, answered by one Lead revision; Security reviews the diff in step 8 |

An issue about `.squad/stack.md` or `.squad/project.md` is a product PR and counts as documentation for the
`docs` tier — except an edit to *Security areas* or to a command the pipeline runs: `patch/security` within
that tier's budget, otherwise `standard` with `.squad/project.md` (*Security areas*) under *Security areas
touched*, and `security` when it removes or narrows a security area. A squad-maintenance PR (*Squad lessons*)
is only for lessons the squad itself raised.

### Gates of a patch

A `patch`'s step 7 runs the checks its files need and nothing else; the complete analysis, and for a `patch`
without production code the full suite and the overall coverage figure, are CI's job on the draft pull request
(*Draft until green*). `gate-stamp.py` runs every row that matches the diff, with the check the Lead's result names.

| The diff touches | Gates |
| ---------------- | ----- |
| documentation only (Markdown, the project blocks) | *Format check* |
| a shell script, a workflow or a Dockerfile | *Analyzer gate* with `--lint-only` (shellcheck, actionlint, hadolint and the shell rules on the changed files, no build), plus the check the Lead's result names for the edit (a pin check, `bash -n`, a dry run) |
| a configuration file or a data file the product reads (YAML, JSON, a catalog) | the check the Lead's result names (a loader test, a schema check), run as *Test (project)* when it is a test |
| test code only | *Format check*, *Analyzer gate*, *Test (project)* for the test project(s) of the changed code (`stack.md`) |
| production code | *Format check*, *Analyzer gate*, *Test with coverage* and the *Coverage gate* — not *Test (project)*: it writes no coverage report, and the gate reads the reports of the full run, the overall figure included |

A gate the row names that the stack does not have (`stack.md` lists no *Test (project)*) is replaced by the
next broader one (*Test*).

### Changes without production or test code

In any tier, a plan whose change touches no production or test code (a workflow, a Dockerfile, build or CI
configuration, documentation only) may declare steps 4 (*Skeleton*), 5 (*Tests first*) and the *Coverage
gate* **not applicable**; the tier and its other steps stay. The Lead then names in `plan.md`, under
*Verification without tests*, where and by whom each acceptance criterion is verified instead (a workflow
step, a PR dry run, a build of the image, a read-only check); the orchestrator logs the skipped steps with a
pointer to it. Step 6 is the Dev's edits, plus the Tester's share where *Verification without tests* or
*Documentation updates* names the Tester (test documentation, a dry-run artifact: *Dry-run artifacts*), in mode
`coverage` after the Dev; step 7 runs every gate but the *Coverage gate* (a branch of
Markdown files only: *Format check* alone, `gate-stamp.py`). The
Reviewer checks that the section exists, covers every criterion and that the diff really holds no production
or test code — otherwise the skip is blocking. If such code changes after all, steps 4–6 apply again.

### Dry-run artifacts

A criterion verified by a dry run on the branch — a planted failing test that must turn a gate or a CI job red, a
deliberate defect a check must catch — never reaches the merged change. Its owner writes the artifact as a patch file
outside the working tree (the scratchpad, a scratch worktree), checks it with `git apply --check` (`git-guard.py` allows a
member only the forms of `git apply` that read a patch) and hands back its path. The orchestrator applies it as one commit, runs or waits
for the check, reverts that commit with `git revert`, and logs both commits with the check's result; the Reviewer
confirms that the revert left nothing of it.

### Criteria verified only in CI

A criterion that only CI can verify in full — a container image the sandbox cannot build, a release build
on another platform — is still checked as far as the sandbox allows, before the review. A change that touches
an input the image build is sensitive to (*Image build check* in `.squad/stack.md` names them: project or
manifest files, content items, static web assets, the Dockerfile) runs the *Image build check*, which
reproduces the Dockerfile's own order (manifests, restore, then the sources, build without a restore); the
plan names it as the criterion's verification with the Dev as its owner. A build or publish from the full
working tree does not count: it restores with every source present. A red CI on such a criterion after that
check is worked in step 11.

A criterion tested by hand on a build before the PR approval (step 9) — a Product Manager's manual test of the
change, say — names in the plan the build it runs on: a local build or publish of the branch head, with the
command the project's documentation gives, or the CI-built artifact, for which the plan asks for an *Early draft
PR* (without one, no CI build of the change exists before step 10). When the plan asks the Product Manager to build
the artifact (an image for the target host, say), the spec or plan first asks whether they can build it there, and
the plan names the fallback: who builds it otherwise and how it reaches the target (`orchestration.md`, *An artifact
the Product Manager cannot build*). The manual test runs on the stamped head before the review set of step 8 (`squad-next.py` names it, logged as
`7 Manual test`), so a document it corrects is reviewed once.

### Operator environments and documented procedures

A criterion or document that depends on the operator's environment (network, DNS, access policy, console,
upload tools, what the target host has installed) is planned against that environment, not against the
session's:

- **Target host (install and deploy features).** The spec asks the Product Manager what the target host has
  and may have (tools such as `git`, network access to which hosts, DNS, the console it is used from, the
  order of steps the operator will paste) and records the answers as scenarios and acceptance criteria. The
  Devil's Advocate checks the plan against that list.
- **Third-party pages.** A document that leads the operator through another provider's page or console (a
  settings page, a dashboard) rests on labels and layouts that change without notice. The spec asks the Product
  Manager how far the document may rest on them — their exact labels, only the page's name and purpose, or a link
  to the provider's own guide — and records the answer as a non-functional requirement.
- **Dry run.** The plan schedules a dry run of the documented steps by the Product Manager (or a stated
  stand-in) **before** the documentation is reviewed, and lists the hosts and software versions the run needs
  (for example the backend version with the API the procedure calls). Every statement of the document that no
  run has shown is a row of the plan's *Verified facts*, `unverified` with the dry run as its owner — never a
  marker in the document, which its readers keep; the dry run confirms or corrects each, and a statement it could
  not show is narrowed to what it showed or left out. When an external page cannot be reached from the session,
  the plan asks the Product Manager to confirm those statements in the earliest real run, not at the end.
- **Shell procedures.** For a documented shell procedure the plan states, before Security's plan review, what
  happens when (a) all lines are pasted at once, (b) one line at a time, (c) one line fails, (d) a variable
  is lost, and names the tests (unit and CI) that hold each answer; the Tester derives its cases from that
  list. A line that continues after a failed step, so that a half-finished state can still be run, is a
  defect of the design.
- **CI-only rows.** A script only CI can run (it needs systemd, root or a service the session lacks) has a
  local smoke substitute (*Writing tests* in `.squad/stack.md`); the plan lists the rows that can run in CI
  only, and the Tester runs the substitute before handing back.

### Pages the session cannot open

A link or statement of the change that rests on a page the session cannot open (the proxy refuses the host, the
repository cannot be attached) is confirmed by a person, never assumed. A plan whose documentation links or
describes third-party pages names who opens the ones the session cannot (the Product Manager or a stated stand-in)
and when: in the earliest real run when there is a dry run (*Operator environments and documented procedures*),
otherwise before step 9. The Reviewer reports each such link or statement of the diff as `unopened`, with the error
it got, instead of judging it; the orchestrator asks that person once for the round's list and logs the answer as a
Product Manager row, and a page that does not say what the change claims is a blocking finding for the Dev.

### Code a unit test cannot cover

A file of the change that no unit test can cover — a browser script the stack has no test runner for, a
generated file, process startup glue — is found in the plan, not after the PR: the Lead lists such files in
`plan.md` with the reason, the Tester confirms or contests the list in its report, and the Lead's decision
is a decision record. Such a file is excluded from the coverage **measure** only, in every place that
computes it (`sonar.coverage.exclusions` for the CI code analysis, and `COVERAGE_EXCLUDES` in
`.squad/tools/squad_settings.py` where `COVERAGE_PATHSPECS` includes the file — the product PR edits that file
itself; *Coverage exclusions* in `.squad/stack.md` names where the repository sets them) — never from the analysis (`sonar.exclusions`, a
suppression): its rules still apply, and those no local gate reports arrive in step 11.

### Early draft PR

CI runs on pull requests only, so a PR opened in step 10 comes too late for a change whose **CI-only criteria** or
manual test on the CI-built artifact (*Criteria verified only in CI*) or **dry run** (*Operator environments and
documented procedures*) needs a CI result before the review. For such a change the plan says so, and the orchestrator
opens the PR as a draft right after step 7 (`create-pr` step 8, `specs/` still in the diff, no working record comment
yet), then waits for CI and reads `sonar-check.py pr <n>` before step 8; findings are worked as in step 11. The *Sonar
gate* alone is no reason for an early PR: every push to an open PR runs all of CI again, and step 11 reads the analysis
anyway. The dry run runs on that head, before the documentation is reviewed. Step 9 additionally requires the green CI
run, the dry-run result and the Reviewer's round on the same head; step 10 then only posts the working record, removes
`specs/` and marks nothing ready (*Draft until green*).

## Pipeline

| # | Step | Owner | Exit condition |
| - | ---- | ----- | -------------- |
| 1 | Intake | Orchestrator | Branch off `main`, work folder and `log.md` created (rows appended with `squad-log.py`), committed and pushed |
| 2 | Plan | Lead, Devil's Advocate | Features first: `spec.md`, `plan-check.py --spec` clean, one spec challenge and, where *Spec approval* in `orchestration.md` calls for it, the Product Manager's approval (`squad-spec` step 2a). Then `plan.md` from `specs/_template/plan.md` (features: and `tasks.md`), decision records `Proposed` and indexed, `plan-check.py` clean before anyone else reads it — or the `docs`/`patch` result (*Tiers*), or outcome **no change**. One plan challenge when *Plan challenge* calls for it, routed as that section says |
| 3 | Plan security review | Security | Whenever *Security areas touched* is not "none" (always on tier `security`), in parallel with the plan challenge. `APPROVED` → 4; `CHANGES_REQUIRED` → Lead revision and Security delta confirmation (*Loop limits*) |
| 4 | Skeleton | Dev | Only when the plan adds or changes API: compile-only signatures built as *Skeleton* in `.squad/stack.md` describes, *Build* passes |
| 5 | Tests first | Tester | Only the tests the plan names for its acceptance criteria (one or a few per criterion); they compile and **fail** on the current code or the skeleton. A plan-named test that cannot fail yet is *deferred* to step 6; other guards, pins of existing behavior, determinism and bound tests are not written |
| 6 | Implementation + coverage | Dev, Tester | All tests green; *Coverage gate* passes; then the plan's doc updates, area documents included, written by the Dev against the code as built (`squad-dev.md`, *Documentation*). The Tester's coverage pass adds tests only for uncovered lines, a resource bound the plan names, a deferred or missing plan-named test, or a test a reviewer asks for |
| 7 | Code check | Orchestrator, Code Officer | `main` merged first when it moved (*Merge conflicts*). The orchestrator commits and runs `gate-stamp.py`, which runs the tier's gates (*Format check*, *Analyzer gate*, *Test with coverage*, *Coverage gate*, `scope-check.py`; `patch`: *Gates of a patch*) and, in tiers `trivial`, `standard` and `security`, `figure-check.py`, which is no gate, and logs them as a stamp with the head SHA: a gate result counts only as that stamp, never as a member's report. The *Sonar gate* runs in step 11 on the draft PR. A `figure-check.py` hit (documentation that still states a value the diff changed) that the Dev's last report does not confirm goes back to the Dev, the others reach the Reviewer with the gate output. The orchestrator clears a *Format check* failure itself (*Format*, then `git restore` of every file outside the change); analyzer diagnostics and style findings that need reading launch the Code Officer (structural items go to the Dev or Tester), then the stamp runs again. Exit: a passing stamp on the head |
| 8 | Review | Reviewer (+ Security's diff review on `security`, and on an unlisted security area) | They get the code SHA and the gate output of the stamp of step 7 (they do not re-run the gates); later commits during the round may touch only `specs/` and do not make it stale. No blocking findings → 9; blocking → owner fixes (Dev: code and documentation, Tester: tests), back to 6, then a mandatory delta round — except a verbatim documentation fix (*Verbatim documentation fixes*) |
| 9 | PR approval | Orchestrator, Lead when needed | *PR approval* below, and what *Early draft PR* adds |
| 10 | Pull request | Orchestrator | Working record posted as comment, `specs/<folder>/` removed and `scope-check.py --no-specs` clean, PR opened **as a draft** (*Draft until green*; merged later with *Squash and merge*) — or, after an *Early draft PR*, only the record and the removal |
| 11 | After the PR | Dev, Code Officer, Reviewer | *Draft until green* holds and the PR is marked ready for review; review comments worked |
| 12 | Wrap-up | Orchestrator | Squad lessons checked against the candidates and filed, and the lessons row logged (*Squad lessons*); the run metrics of `squad-log.py --summary` (exit 0), then the output of `session-usage.py` headed "Session usage", appended to the working record exactly as printed and reported to the user — never a screenshot or a figure typed from `/usage` |

### PR approval

The orchestrator first runs this checklist in full:

- the latest review round is clean and covers every change to production code, tests and `docs/` since it
  ran — only `specs/` bookkeeping and that round's verbatim documentation fixes may follow it; a due delta
  round runs first;
- `gate-stamp.py --verify` passes after `git fetch origin main`: a passing stamp on the head's code, which
  contains `main` — otherwise `main` is merged first (*Merge conflicts*) and the stamp runs again;
- the area documents and documentation updates the plan names are in the diff;
- every link or statement a review round reported `unopened` is confirmed (*Pages the session cannot open*).

Then it sets each `Proposed` decision record of this change and its index row to `Accepted`, runs
`decision-check.py`, commits and stamps that commit with `gate-stamp.py --verbatim` (bookkeeping, like `log.md`: no
delta round). The Lead is launched in mode `approve-pr` only when a
decision is open: a plan deviation in a member's report, a Lead decision recorded during steps 3–8 (loop
limit, dispute, accepted gap, finding accepted unfixed), or a change to `docs/ARCHITECTURE.md` or
`.squad/project.md` that the plan does not name or the Reviewer did not find consistent. The orchestrator lists
these points one by one, and they are all the Lead decides there (`squad-lead.md`, mode `approve-pr`);
non-blocking findings are decided in step 8 (*Non-blocking findings*). Otherwise "approved by checklist" is
logged → 10.

A fix the Lead orders goes back to step 6 or 8 and counts against the review loop; any edit beyond the
record status — `docs/ARCHITECTURE.md` included — has an owner and gets a delta round. Once that round is clean, the
checklist runs again; the Lead's `approve-pr` agent is continued only for a point the fix leaves open, and a point it
settled is not listed again. `NOT APPROVED` without a fix means the Lead decides or escalates. After the PR, a re-approval runs the same checklist once CI on the
new head has finished and passes.

## Plan challenge

The Devil's Advocate challenges every plan of tier `security` (also one without production code: CI permissions,
secrets, a workflow) and a plan of tier `standard` only when it changes production code **and** either names an
alternative under *Options considered* or adds a new decision record — extending an existing record alone does
not count; never a plan of tier `docs`, `patch`, `tests` or `trivial`. The same holds for a feature, whose spec it
has already challenged. `plan-check.py` prints the verdict with its reason (`Plan challenge: due — …` or `not due —
…`), and `squad-next.py` acts on it. It runs at most once, has no veto, and a rejected objection is not raised
again. Each objection is ranked `major` (the plan would build the wrong thing or miss the defect) or
`minor`, so the orchestrator routes without judgment:

- **Only minor objections** (when step 3 runs: and Security approved): no Lead launch; the orchestrator
  appends them verbatim to `plan.md` as *Challenge notes* (bookkeeping, like `log.md`), with `squad-notes.py`, which
  copies them from the member's report, never retyped. Dev and Tester follow them
  or report a deviation; the Reviewer checks them in round 1.
- **Non-blocking findings of Security's `APPROVED` plan review** go verbatim into a *Security notes* section
  the same way — except a note that leaves the plan a choice ("document it, or reject"): the Lead decides it
  in mode `revise` before the skeleton, in one launch with any major objection.
- **A note or minor objection whose fix is an exact edit of the challenged document itself** (`plan.md`,
  `spec.md`: the sentence as it stands and as it should read) is applied by the orchestrator when it appends the
  note, the way of *Verbatim documentation fixes* (`orchestration.md`; `specs/` only, logged, no launch): Dev and
  Tester may not edit those documents, so nobody else would. A fix that needs judgment, or one that changes an
  acceptance criterion, is routed as above.
- **A major objection:** the Lead, in mode `revise` with all objections, answers each one in the plan's
  *Challenge* section, the minor ones included.

When step 3 runs, the challenge and Security's plan review run **in parallel** on the same plan commit; one
Lead revision answers the major objections and the `CHANGES_REQUIRED` findings together, followed by one
Security delta confirmation.

## Outcome "no change"

If the Lead concludes in step 2 (also after answering the Devil's Advocate) that no code change is needed —
duplicate, cannot be reproduced, works as designed (e.g. covered by an accepted decision record), or out of
scope — it returns `RESULT: NO CHANGE` with a proposed issue comment, written for the issue's author (`squad-lead.md`,
*Output format*: none that thanks the repository owner who posts it). The orchestrator shows the comment to
the Product Manager and posts it only after confirmation (a public statement on the issue), with the log
appended as a collapsed "Squad working record" block. It removes the work folder with a commit and pushes; no
PR is opened, the branch stays without one, and the user is told so.

## Loop limits

- **Plan check (step 2):** `plan-check.py` runs after every Lead write; its findings go back to the Lead
  (mode `revise`, the findings only, no challenge round; a plan challenge or Security plan review logged before it
  and not yet answered is answered by the same revision) at most twice per document; findings left after that
  go to the Lead in mode `decide`, which fixes them or escalates. A finding is never waved through.
- **Spec challenge (step 2, features):** one Devil's Advocate round, no veto, handled like the plan challenge
  (minor objections into the spec's *Challenge notes*, which the Lead answers while it plans).
- **Plan challenge (step 2):** at most one round, no veto (*Plan challenge*).
- **Plan ↔ Security (steps 2–3):** at most 2 rejections; when step 3 runs, the first revision answers the plan
  challenge and the first `CHANGES_REQUIRED` together. After the 2nd `CHANGES_REQUIRED` the Lead
  decides: narrow the scope, split into separate issues, escalate to the Product Manager, or — when the
  remaining defect is a pure wording defect, a defect confined to the plan's resource section (limits,
  bounds, sizes) whose fix changes no scope, or a defect whose **complete fix** (the exact statements or edits)
  Security supplied, where the fix changes no scope and no acceptance criterion (for a defect in a claim about
  how code, a library or a server behaves, only once Security has **measured** the failure) — accept and fix it
  without an escalation, followed by exactly one Security delta confirmation of the fix (a further rejection
  then goes to the Product Manager).
- **Review ↔ Dev (steps 6–8):** review pass 1 is a full review; at most **2 further fix-and-review
  rounds**, each reviewing only the delta; blocking findings still open then go to the Lead (accept with
  justification, split into a follow-up issue, abort, or escalate). A round counts per review round that
  returned blocking findings, not per launch: the Dev's fix and a test or test double the Tester repairs
  because of it are one fix round, followed by one delta review; a defect found in a test counts the same.
- **Existing tests affected by a signature change:** the plan lists the existing test call sites of a
  changed signature (factories, helpers) and who adapts them. The **Dev** adapts them in step 4 when the old
  signature goes away (so *Build* passes before the Tester starts), and later when a call site stops
  compiling only because of a signature or field change the Dev made itself — mechanically, no assertion touched, listed in
  its report; these are the only cases in which the Dev edits test code. The **Tester** moves them in step 5
  when old and new signature coexist, and checks the Dev's edits in its coverage step.
- **Dev ↔ Tester disagreements:** if the Dev believes a step-5 test's expectation is wrong, the Lead decides
  (the test is not changed silently). A defect the Dev reproduces without changing the test (a failing fixture
  or helper, not a disputed expectation) goes to the Tester as a fix round once the orchestrator has verified
  the reproduction. Not counted against a loop limit.
- **Code check needs a structural change** (or breaks build/tests): the Code Officer's edit is reverted
  and the item goes to the Dev (or Tester for tests), then the code check runs again. Not counted
  against a loop limit.
- **Coverage gate fails:** Dev and Tester iterate; lines no unit test can cover go to the Lead, whose
  decision is recorded in `log.md`. A whole such file that the plan did not list (*Code a unit test cannot
  cover*) is a plan defect the Lead records with the decision.
- **After the PR (step 11):** CI or quality-gate failures and review comments are fixed on the same
  branch through steps 7–8 again (delta review), at most 2 fix rounds per failure; then the Lead decides.
- **New scope on the open PR (step 11):** a Product Manager request on the open pull request that adds or
  changes behavior beyond the spec's acceptance criteria (not a criterion the change misses) goes to the Lead
  before any edit. The Lead decides, recorded in `log.md`: a follow-up issue (the default), or this change grows,
  and then the spec gets the new criteria as amendments (mode `decide` in `squad-lead.md`) and the plan a new
  task group, which runs steps 3–8 like the others
  (plan challenge and Security plan review as the tier asks) before step 9 approves again.
- **Safeguard stop (any member, any step):** an API `invalid_request` of category `cyber` is not an API error:
  relaunching unchanged loops, and rephrasing the request is a workaround. The orchestrator escalates to the
  Product Manager at once, naming the finding the member had not reviewed as unverified.

## Escalation to the Product Manager

The Lead escalates only when it cannot decide responsibly on its own: an ambiguous requirement (a reader of an
external format without a real sample in the test data or on the issue is one — it asks for the sample, or for the
confirmation to build from the description with the residual risk recorded in the decision record; a sample the
PM supplies stays outside the repository, the Tester builds an anonymized fixture, a test that reads the sample
from an environment variable checks the fixture against it, and only counts are reported), a product
decision (behavior change visible to users, breaking a guarantee in `docs/ARCHITECTURE.md` or an accepted decision
record), or a loop limit where none of its options is clearly right. The escalation is one concise question with
the options and the Lead's recommendation. Follow-up issues the Lead decides on are created by the orchestrator and
listed in the PR under Next Steps.

Besides the Lead's escalation, the orchestrator involves the Product Manager only where one of these rules says so:
the kind of an issue labelled both bug and feature and the values only the Product Manager can produce (*Bug or
feature*); the approval of a feature's spec (*Spec
approval* in `orchestration.md`); the public issue comment of *Outcome "no change"* and of *Outside authors*; a
download a member was denied (*A denied measurement* in `orchestration.md`); a Security rejection of a fix the Lead
accepted, and a safeguard stop (*Loop limits*); the dry run and the confirmation of statements no run has shown
(*Operator environments and documented procedures*); the pages the session cannot open (*Pages the session cannot
open*); a manual test before step 9 (*Criteria verified only in CI*); a pull request the user marked ready before
*Draft until green* holds (`orchestration.md`). A request the Product Manager makes on the open pull request goes to
the Lead first (*Loop limits*, New scope on the open PR).

## Non-blocking findings

Fixed in the same change or opened as a linked GitHub issue now — never deferred to "a later change".

- A non-blocking finding with a verbatim documentation fix goes the way of *Verbatim documentation fixes*.
- A non-blocking finding that only asks for what the approved plan already orders (the Reviewer quotes the plan's
  sentence and tags it `plan`) goes to the sentence's owner with the round's fixes, without a Lead decision.
- Every other non-blocking finding — the Reviewer's or Security's, one that touches code, or one the
  orchestrator would turn into a follow-up issue — is decided by the Lead in step 8, in mode `decide` (one
  launch for all of a round's findings), before that round's fix round: a fix it orders goes to its owner
  together with the round's blocking findings, and one delta round covers both; a linked issue is opened by
  the orchestrator. Step 9 does not take them up again.
