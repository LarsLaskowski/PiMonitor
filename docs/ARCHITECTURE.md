# Architecture

<!-- project:begin architecture -->
{{TODO: What the system does, in two or three sentences.}}

## Components

{{TODO: The projects/packages/modules and what each owns. A diagram (Mermaid) of the main data flow.}}

## {{TODO: Main flow, e.g. request pipeline / sync pipeline}}

{{TODO: How data moves through the system, and the deliberate guarantees along the way — each guarantee also
listed in `.squad/project.md` (*Guarantees*) and linked to its decision record.}}

## Configuration

{{TODO: Where configuration comes from, required settings, defaults, validation at startup. The rules per area
are in [`areas/`](areas/README.md); this overview links them instead of repeating them.}}

## Security model

{{TODO: Authentication, secrets, trust boundaries. Kept in sync with `SECURITY.md` and the *Security areas* in
`.squad/project.md`.}}

## Deployment

{{TODO: How the software is built, packaged and released.}}
<!-- project:end architecture -->

## Development process

This repository is developed with Claude Code: `AGENTS.md` holds the rules every AI coding agent reads, the
skills live under `.claude/skills/` and the squad roles under `.claude/agents/`. Every pull request is reviewed
before it is opened by the read-only reviewer in `.claude/agents/squad-reviewer.md`. The squad skills
(`squad-issue`, `squad-spec`) wrap that review in a bounded pipeline described in
[`.squad/routing.md`](../.squad/routing.md): a Lead plans, picks the tier that decides how much of the pipeline
runs and owns every decision; a Devil's Advocate challenges a feature's spec and, when routing says so, the
plan; Security reviews the plan when a security area is touched; tests come first, the gates run before the
review, and every loop limit ends in a Lead decision — only one the Lead cannot make reaches the human. The stack's commands live in
[`.squad/stack.md`](../.squad/stack.md), the project's guarantees and attack surface in
[`.squad/project.md`](../.squad/project.md).

The reasoning behind individual choices is kept out of this document and recorded instead as decision
records in [`docs/decisions/`](decisions/README.md); this document describes how the system works and
links a record where a guarantee or flow is the result of one.
