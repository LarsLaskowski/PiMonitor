# Plan: <title>

Source: Issue #<number> | [spec.md](spec.md)
Tier: tests | trivial | standard | security  (tiers `docs` and `patch` use no plan.md) — <one-sentence justification>
Security areas touched: none | <areas>
<!-- Each security area of `.squad/project.md`, Docker/CI or build configuration and dependency the change
touches, or "none" — in every plan (`.squad/routing.md`, *Tiers*). Only a plan over its tier's PLAN_MAX_WORDS adds
the line "Plan size: <why the scope needs it>" here (`plan-check.py`). -->

## Problem / root cause

For a bug: the cause, with file and line. For a feature: one sentence pointing to `spec.md` — do not repeat it.

## Verified facts

Every factual claim the plan rests on — from the issue, the spec, the code, a library, the image — with its
evidence (file and line, a command and its output, the sample or document read) and its status. A claim no
one could check yet is `unverified`, and its evidence cell says `to verify:` with the measurement, its owner
and the step. `.squad/tools/plan-check.py` rejects a row without evidence or status.

| # | Claim | Evidence | Status |
| - | ----- | -------- | ------ |
| 1 | … | … | confirmed / refuted / unverified |

## Acceptance criteria

Bug fix only — a feature's criteria live in `spec.md` and are referred to by their id; do not copy them here.
A bug fix's criterion changed after step 2 gets an *Amendments* section at the end, as `spec.md`'s template says.

- AC1: ... (in a plan without production or test code, *Verification without tests* below says how it is
  verified instead)

A criterion that fixes an error text names the exact text the tests compare; one about a container image, or
tested by hand before step 9, names its verification as *Criteria verified only in CI* (`.squad/routing.md`) says.

## Tests per criterion

One row per acceptance criterion (of this plan, or of `spec.md` for a feature): the tests the Tester writes
first, named strictly by *Layout* in `.squad/stack.md`; each test carries its criterion id (*Criterion ids* in
`docs/UNIT_TESTS.md`). A criterion no unit test can verify names its kind and check instead: `CI: <the check>`,
`manual: <who, on which build>` or `after merge: <what, by whom>` (a linked verification issue, step 10).
`.squad/tools/plan-check.py` reports a criterion without a row; `--matrix` shows each one's result.

| AC | Test(s) | Test file |
| -- | ------- | --------- |
| AC1 | … | … |

A test or script that reads a repository document (`*.md` and the other files `.gitattributes` checks out with
CRLF) normalizes `\r\n` to `\n` first; the first such test also tests the normalizer with CRLF input
(`docs/UNIT_TESTS.md`). For a documented shell procedure, a list under this table names the test for each case
of *Operator environments and documented procedures* (`.squad/routing.md`), and the rows that can only run in
CI.

## Verification without tests

Only when the change touches no production or test code, in any tier: steps 4, 5 and the *Coverage gate*
are not applicable (*Changes without production or test code* in `.squad/routing.md`). Every acceptance
criterion is then verified here instead of by a test, one line each with its kind as above (`- AC1: CI: …`, `check:`
for a read-only check): where and by whom (workflow step, PR dry run, read-only check). A criterion verified here has
no *Tests per criterion* row, or one of the same kind or naming a test that exists already: a pointer such as
`check (below)` reads as a test name. Otherwise delete this section.

## Approach

<!-- New user-visible output: its layout, or "layout free for the Tester, approved in step 9" (squad-lead.md). -->

## Options considered

Each alternative weighed and why it lost — or "one option: <why the fix is the only reasonable one>"
(*Plan challenge* in `.squad/routing.md`). Tiers `standard` and `security` only.

## Affected projects and types

| Project | Type / file | Change |
| ------- | ----------- | ------ |

## Signatures (for the Dev's skeleton)

Every new or changed API member (one a test or another type calls), with its full signature in backticks — or
"none". Each backticked declaration here is one the Dev's skeleton adds (step 4), so a private member is described
in *Approach*, and a plan without API writes this section in plain prose. A member the change deletes stands on a
line of its own that starts with "Deleted:" or says it "is deleted" or "is removed": no declaration the skeleton
adds.

## Test files

The files of *Tests per criterion*, named strictly by the convention in *Layout* of
[`.squad/stack.md`](../../.squad/stack.md) and [UNIT_TESTS.md](../../docs/UNIT_TESTS.md) — no combined file, no
alternatives.

Existing test code that calls a changed signature (factories, helpers): the call sites and who adapts them
(`.squad/routing.md`, *Loop limits*) — or "none".

Files of the change no unit test can cover (a browser script without a test runner, a generated file), each
with the reason and the coverage exclusion that follows (`.squad/routing.md`, *Code a unit test cannot
cover*) — or "none".

## Areas

The area documents in `docs/areas/` this change touches (`docs/areas/README.md` lists them), and what changes in
each — or "none" (no change in behavior). A new area: name and scope for the index, a Lead decision. A change
in behavior updates its area document in this pull request.

## Documentation updates

`README.md` (configuration table, env vars), `docs/*.md` including the area documents and
`docs/ARCHITECTURE.md`, and what the PR template's checklist asks for (a changelog entry, say) — or "none": each file
and what changes in it, for the Dev (test documentation: the Tester).

## Architecture check

Which guarantees from `docs/ARCHITECTURE.md` are touched and how they are preserved.

## Security considerations

- [ ] Every byte or length limit on parsed input says whether it applies before or after decoding (a
  replacement such as U+FFFD can multiply the size of raw bytes) — or "no limits".
- [ ] Every exception that can reach a user-visible failure reason or message is listed, with the text
  that reaches the user (never a raw runtime exception message) — or "none".

## Decision records

- `docs/decisions/NNNN-title.md` (Proposed) — or "none: no decision beyond the obvious fix"

## Challenge

Left out when the plan is written. After a major Devil's Advocate objection the Lead adds each objection
and its answer (accepted — what changed; or rejected — why); minor objections alone arrive as *Challenge
notes* instead (*Plan challenge* in `.squad/routing.md`).

## Security review

Left out when the plan is written. After Security's plan review the Lead adds each `CHANGES_REQUIRED`
finding and how the revision resolves it, and the choices *Security notes* left open (*Plan challenge* in
`.squad/routing.md`).

## Out of scope / follow-ups
