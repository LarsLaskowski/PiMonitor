# Process decisions

How this repository works with its AI squad, and why. These are decisions of the squad template (the repository
`.squad/template.json` names), not of this product: the template's `adopt-template` skill overwrites this file on
every refresh, so it takes no record number, has no row in the [index](README.md) and is never superseded here.
A change to one of these decisions is made in the template repository and arrives with the next refresh; a local
edit is lost. `.squad/tools/decision-check.py` does not check this file.

## Squad working records stay off main, and product PRs never change the squad

**Context.** The squad writes a plan and a log per issue or feature under `specs/`. Kept on `main`, these records
accumulate without being read again, while the lasting reasoning already lives in decision records. Squad lessons
fixed inside a product pull request mix two unrelated changes and are easy to lose in a later template refresh.

**Options considered.**

1. **Keep `specs/` on `main`** — full history in the repository; grows with every change and duplicates the
   decision records.
2. **Working records only on the work branch** — posted as a "Squad working record" comment on the issue (or the
   pull request) before the pull request opens, then removed; `main` keeps only `specs/README.md` and the templates.

**Decision.** Option 2. In addition, an issue or feature pull request never changes the squad or the agent
instructions (`.squad/routing.md`, *Scope of a product PR*), and a lesson about the squad becomes an issue in this
repository, labelled by where it is fixed: in the template repository for a template-managed file, in a
squad-maintenance pull request here for project knowledge (`.squad/orchestration.md`, *Squad lessons*).

**Consequences.**

- `main` stays free of per-change working records; the issue comment keeps them findable.
- A crashed session can still resume, because the work folder is committed on the work branch.
- Squad fixes need their own pull request, even when they are small.

## Squash-merge pull requests

**Context.** The squad commits and pushes after every pipeline step to secure its work, and the internal process
(plan revisions, review rounds) must not appear in the history of `main`. Rewriting a pushed branch to tidy it up
would need a force-push, which requires explicit approval.

**Options considered.**

1. **Rebase merges, commit only at curated milestones** — linear, granular history on `main`; work between
   milestones is not secured, and review fixes have to be folded into earlier commits.
2. **Squash and merge** — one commit per pull request on `main`, built from its title and description; branch
   commits can be frequent and unpolished; per-commit granularity inside a pull request is lost on `main`.

**Decision.** Option 2. Pull requests are merged with *Squash and merge*. The title (`[area] Description`) becomes
the commit subject on `main`, the description its body. Branches are kept current by merging `main` into them
instead of rebasing.

**Consequences.**

- The title and description of a pull request are the permanent record of a change and are written for that purpose.
- Branch commits may name pipeline steps; they never reach `main`.
- The repository settings allow only *Squash and merge* (or at least default to it).

## Quality gates before the pull request

**Context.** Formatting slips, analyzer findings and missing tests that only surface in CI or in the code analysis
of the pull request cost an extra fix-and-push round each, and every round costs reviewer attention. The squad has a
Code Officer whose sole job is to leave nothing of that kind behind, and the `create-pr` skill runs the same checks
outside the squad.

**Options considered.**

1. **Keep everything in CI** — a safety net for every contributor, but findings keep arriving late.
2. **Check locally and in CI** — earliest feedback plus a safety net; some checks run twice.
3. **Check locally first, CI as the system of record** — format, analyzer gate and coverage gate run before the
   push (`.squad/stack.md`); CI keeps the build, the tests and the code analysis.

**Decision.** Option 3. Before a push: *Format*, the *Analyzer gate* (no diagnostic of any severity in a changed
file) and the *Coverage gate* (new or changed production code with the branch conditions its report records, line coverage overall,
at least the thresholds
in `.squad/tools/squad_settings.py`) from `.squad/stack.md` pass. CI still builds, tests and runs the code analysis
(e.g. SonarQube Cloud), which stays the system of record for the quality gate. Squad and agent tooling (`.squad/**`,
`.claude/**`) is excluded from the coverage measure in CI; it is developer tooling, not production code.

**Consequences.**

- Findings appear while coding; the Code Officer clears them before the review.
- A push that skips the local gates is only caught by what CI still checks. The pull request template's checklist
  and the `create-pr` skill require the local gates; adding a CI step back is the remedy if unchecked code starts
  reaching `main`.
- The local analyzers may differ from the CI quality profile, and some checks (duplication, security hotspots,
  taint analysis) only run in CI; such findings arrive after the pull request is opened.
- The coverage rule applies to new or changed lines, so it does not force retroactive tests on old code.
