# Spec: <title>

Source: Issue #<number> | the user's description
Status: Draft | Approved by Lead | Approved by Product Manager

<!-- What the feature does for its users, before anyone decides how to build it. Written by the Lead in mode
`spec`, challenged once by the Devil's Advocate, and from then on the only source of the acceptance criteria:
`plan.md` and `tasks.md` refer to them by id. `.squad/tools/plan-check.py` checks that every section is filled;
the guidance in these comments does not count. -->

## Problem

<!-- Who has the problem, what happens today and why it matters. No solution. -->

## Scenarios

<!-- What a user (or the system) does and sees, one numbered scenario per path, the usual path first, in the
words of the user interface, the API or the file the user reads: "S1: Given …, when …, then …". A small feature
(at most SMALL_FEATURE_CRITERIA criteria in `squad_settings.py`, no security area) may write "none" here and each
criterion as its own scenario. -->

- S1: …

## Errors and edge cases

<!-- Invalid or hostile input, a failing or slow dependency, a limit reached, an empty or a very large input, a
repeated or concurrent action — numbered, each with what the user sees (the exact text where a user reads it). -->

- E1: …

## Non-functional requirements

<!-- Limits and sizes, time and memory, security and privacy, compatibility (versions, platforms, existing data
and configuration), new configuration with its default, logs and metrics — numbered, one each: "N1: …", or
"none". For an installer or deployment feature also what the target host has and may have (tools, network access,
DNS, the console, the order of steps the operator pastes), and for a guide through third-party pages how far it may
rest on them, as the Product Manager answered (*Operator environments and documented procedures* in
`.squad/routing.md`). -->

- N1: …

## Acceptance criteria

<!-- The only list of criteria of this feature. Each is one testable statement and names the scenarios, edge
cases and non-functional requirements it covers, e.g. "AC1: an export of an empty library writes a file with the
header only (S1, E2)". No checkbox: the verification matrix of the working record shows what was verified. -->

- AC1: …

## Out of scope

<!-- What the feature deliberately does not do. A scenario, edge case or non-functional requirement listed here
by its id needs no criterion. -->

## Open questions

<!-- Each question the issue or description leaves open, with "Answered:" and the answer (from the code, the
documentation, an accepted decision record or the Product Manager) — or "none". A question without an answer
blocks the plan: the Lead escalates it. -->

## Area documents

<!-- The area documents in docs/areas/ that will describe this behavior once it is built, and what each will
say, in outline. This is what stays on main when the working record leaves. -->

<!-- Challenge: left out when the spec is written. After the spec challenge the Lead adds a "## Challenge"
section: each objection of the Devil's Advocate and the answer (accepted — what changed; or rejected — why). -->

<!-- Amendments: left out until a criterion changes after the Lead approved the spec. Then the Lead (mode
`decide`) adds a "## Amendments" section at the end: a table `| Criterion | Change | Reason | Step | Decided by |`,
one row per criterion it changes, adds, drops (`dropped: …`) or waives (`waived: …`, a criterion no one can
verify, only by the Product Manager), decided by `Lead` or `Product Manager`. -->
