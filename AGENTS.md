# AGENTS.md

Project guidance for AI coding agents working in this repository. This one file holds the rules for all of
them: Codex, Kimi Code and GitHub Copilot's coding agent read it directly, Claude Code through the
`@AGENTS.md` import in [`CLAUDE.md`](/CLAUDE.md), and GitHub Copilot's code review and chat through
[`.github/copilot-instructions.md`](/.github/copilot-instructions.md), which the template's `adopt-template`
skill generates from it: everything from the first `##` heading on is identical in both files (Claude Code's
`@` import lines aside), so a change to it is made in both, and `.squad/tools/config-check.py` fails when they
differ. This file is a summary; the binding, detailed references are [`ARCHITECTURE.md`](/docs/ARCHITECTURE.md)
(how the system is put together and why), [`CONTRIBUTING.md`](/docs/CONTRIBUTING.md) (workflow, PR
conventions, versioning), [`UNIT_TESTS.md`](/docs/UNIT_TESTS.md) (test conventions — **unit tests are
mandatory for new code**) and [`.squad/stack.md`](/.squad/stack.md) (toolchain and commands). Read them before
making a non-trivial change; when this file and one of them appear to disagree, treat that as a sync bug to
fix, not as license to pick either one.

## What this project is

<!-- project:begin overview -->
PiMonitor is a lightweight system-monitoring dashboard for the Raspberry Pi. A single Go binary collects system
metrics (CPU usage and load average, CPU temperature, RAM/swap, filesystem usage, network throughput, kernel
version, distribution, Pi model, available apt updates), serves a self-contained web dashboard, and exposes a
versioned REST API (`/api/v1/...`, contract in [`docs/API.md`](/docs/API.md)) for third-party integration such as
openHAB. It runs as an unprivileged systemd service on the Pi; a separate, root-privileged systemd timer refreshes
the apt package cache so the main service itself never needs elevated privileges. See
[`README.md`](/README.md) for the feature list and installation, and [`ARCHITECTURE.md`](/docs/ARCHITECTURE.md)
for how it fits together.
<!-- project:end overview -->

## Golden rules

- **Never commit or push to `main`** — no one, not even with approval. Every change goes through a
  separate branch and a pull request.
- **Commits and pushes to a feature branch are always allowed** without asking (squad members never commit
  or push: `.squad/team.md`, *Git and GitHub*): commit finished work and push it to the current feature
  branch (creating that branch off `main` if needed), so nothing is lost when a session ends. Force-pushing
  or otherwise rewriting published history, deleting branches, and creating tags (a `v*` tag may trigger a
  release) still need explicit user approval.
- **Pull requests are only opened by the squad or by the user.** The `squad-issue` and `squad-spec`
  skills open a PR once it is approved (`.squad/routing.md`, *PR approval*); outside the squad, a PR is
  opened only when the user explicitly asks for one (e.g. by running the `create-pr` skill). Never open a PR
  on your own initiative.
- **A change made outside the squad that touches a security area** — one listed in
  [`.squad/project.md`](/.squad/project.md) (*Security areas*), a CI workflow (any change to it: permissions,
  secrets, triggers, the actions it uses), Docker or build configuration, or a dependency — is reviewed before it is merged: `create-pr` adds
  Security's diff review to its review loop, and a pull request opened another way (by hand, from a plain
  session) gets a `review-pr` run whose findings are worked in that pull request.
- Run *Format* from [`.squad/stack.md`](/.squad/stack.md) after editing code and before building; CI is
  not meant to find formatting issues. In the squad skills the member that changed the files runs it on
  them before handing back (`.squad/team.md`, *Shared rules*).
- A changed file may not carry **any analyzer diagnostic of any severity**, including info-level ones that
  never show up as build warnings but that the CI code analysis (e.g. SonarQube Cloud) reports. Check with
  the *Analyzer gate* from `.squad/stack.md` and fix every finding before considering the work done (in
  the squad skills, the Code Officer owns this).
- New or changed production code needs unit tests until the *Coverage gate* in `.squad/stack.md` passes:
  **at least 80 % line coverage** on new or changed lines and overall (the thresholds live in
  `.squad/tools/squad_settings.py`, see [`UNIT_TESTS.md`](/docs/UNIT_TESTS.md#code-coverage)).
<!-- stack:begin golden-rules -->
- `gofmt` formats everything; `go vet` and **golangci-lint** must report nothing new, and `govulncheck`
  must stay clean.
- Add dependencies with `go get`, run `go mod tidy`, and commit `go.mod` and `go.sum` together.
<!-- stack:end golden-rules -->

## Commit messages and pull requests

- Commit subject: one summary of **no more than 80 characters**, no trailing period, not in the first
  person. A body only where the subject does not explain the change; pull requests are squash-merged, so
  branch commits never reach `main` and may simply name the pipeline step.
- Pull request title `[area] Description` (areas in `docs/CONTRIBUTING.md`), description of **3–5
  sentences** on what changed and why: they become the commit on `main`.
- Everything written to the repository or to GitHub — code, comments, documentation, commit messages, PR
  titles and descriptions, review comments — is in **English**, regardless of the conversation's language.

## Commands

<!-- stack:begin commands -->
```bash
go mod download
gofmt -w .
go build ./...
go vet ./...
go test ./... -race -coverprofile=coverage.out
python3 .squad/tools/analyzer-check.py                      # analyzer gate (vet + golangci-lint)
python3 .squad/tools/coverage-check.py                      # coverage gate
```
<!-- stack:end commands -->

All commands, with what each one checks, are listed in [`.squad/stack.md`](/.squad/stack.md).

## Architecture

<!-- project:begin architecture -->
- `cmd/pimonitor` — the binary: flag parsing, config loading, wiring of collector, alerts and HTTP server.
- `internal/collector` — hand-rolled `/proc`, `/sys`, `vcgencmd`, apt and Pi-hole parsers, the snapshot type, ring-buffer
  history and its persistence (`persist.go`).
- `internal/alert` — threshold/debounce engine and the webhook notifier (queue, retry, rate limit).
- `internal/httpapi` — the `net/http` server: `/api/v1/...` routes, middleware (API key, gzip, no-store,
  in-flight limit, security headers), per-route server stats and the Prometheus endpoint.
- `internal/web` — the embedded dashboard assets (`assets/`: vanilla HTML/CSS/JS, no build step).
- `internal/config` — YAML config: defaults, parsing, validation.
- `packaging/` — `install.sh`, the systemd units (`pimonitor.service`, `pimonitor-apt-update.service`/`.timer`)
  and `pimonitor.example.yaml`.
<!-- project:end architecture -->

## Project configuration

<!-- stack:begin configuration -->
- **Go** version and module path from `go.mod`; tools (`govulncheck`) as `tool` dependencies in `go.mod`.
- **golangci-lint** configured in `.golangci.yml`.
<!-- stack:end configuration -->
<!-- project:begin configuration -->
- Runtime configuration is a YAML file (`/etc/pimonitor/config.yaml` on the Pi, `packaging/pimonitor.example.yaml`
  as the documented example), parsed with `gopkg.in/yaml.v3` — the only accepted runtime dependency. Options,
  defaults and validation: [`ARCHITECTURE.md`](/docs/ARCHITECTURE.md#configuration-internalconfig) and the
  Configuration section of [`README.md`](/README.md#configuration).
- `make build` / `make build-arm64` / `make build-arm` (`GOARM=6` for Pi Zero/1; arm64 for Pi 3/4/5 64-bit OS) and
  `make run` (runs against `packaging/pimonitor.example.yaml`) are the developer entry points; the squad's commands
  are in [`.squad/stack.md`](/.squad/stack.md). Releases are built by GoReleaser (`.goreleaser.yaml`).
<!-- project:end configuration -->

## Code style

<!-- stack:begin code-style -->
`gofmt` formatting; short lower-case package names; doc comments on exported identifiers starting with
the identifier's name; errors returned and wrapped with `%w`, never ignored; no `panic` in library code;
`context.Context` first for anything that does I/O or blocks.
<!-- stack:end code-style -->
<!-- project:begin code-style -->
- Prefer the Go standard library over third-party dependencies. `/proc`- and `/sys`-parsing is hand-rolled rather
  than using `gopsutil`, to keep binary size and dependency surface small on constrained hardware
  ([`CONTRIBUTING.md`](/docs/CONTRIBUTING.md#stability-policy)); a new dependency needs a justification against
  hand-rolling it.
- Every metric parser under `internal/collector` takes its source path or input as a field or parameter, so it is
  unit-testable against fixture strings without real `/proc`/`/sys` access.
- A missing hardware source (no thermal zone, no `vcgencmd`, non-Linux platform) yields an empty/zero value and a
  logged warning, never a crash and never a fabricated reading.
- `exec.Command` only with fixed argument lists; external input is never interpolated into a command.
- `internal/web/assets/app.js` never assigns non-empty content to `.innerHTML` (enforced by
  `internal/web/xss_test.go`); DOM nodes are built with `document.createElement` + `textContent`.
- A change to the JSON shape of an existing `/api/v1/...` response is a breaking change: add `/api/v2/...` instead
  of changing `v1` in place, and update [`docs/API.md`](/docs/API.md).
<!-- project:end code-style -->

## Testing

<!-- stack:begin testing -->
**Unit tests are mandatory for newly written code.** Standard `testing` package, colocated `_test.go`
files, table-driven tests with `t.Run`, `t.Helper()` in helpers, `t.TempDir()` for files, failure messages
that state got and want, the id of a squad acceptance criterion in the name of its test (`t.Run("AC3 …")`;
`docs/UNIT_TESTS.md`, *Criterion ids*); no real network or clock (the narrow exceptions, such as a time bound tested as the
ratio of two durations, never an absolute one, are in `docs/UNIT_TESTS.md`).
<!-- stack:end testing -->
Full conventions, including the project's test doubles and the checklist to run before committing a new
test, are in [`UNIT_TESTS.md`](/docs/UNIT_TESTS.md).

## Related skills

Project-specific workflow skills live under `.claude/skills/` and run in Claude Code:

- `create-pr` — verify (format, build, tests, analyzer and coverage gates), review the change locally,
  then open a PR following [`.github/pull_request_template.md`](/.github/pull_request_template.md).
- `squad-issue` — fix a bug reported in a GitHub issue with the squad: the Lead plans and picks a tier
  (`docs` / `patch` / `tests` / `trivial` / `standard` / `security`), the Devil's Advocate challenges a plan once when
  `.squad/routing.md` calls for it, Security reviews the plan whenever a security area is touched and the
  diff of a change to a security control, the Tester writes failing tests first, the Dev implements until the
  *Coverage gate* passes, the Code Officer clears analyzer diagnostics, the Reviewer reviews the diff, and
  once the PR is approved by checklist (the Lead decides what is open) a PR referencing the issue opens.
- `squad-spec` — the same squad pipeline for a new feature, spec first: the Lead writes `spec.md` (scenarios,
  edge cases, the acceptance criteria) and the Devil's Advocate challenges it before `plan.md` and `tasks.md`
  are written against it, in a working folder under `specs/`; also for an issue the user calls a feature or that is
  labelled `feature`/`enhancement` (`.squad/routing.md`, *Bug or feature*).
- `decision-consolidate` — merge unreleased decision records (Superseded chains, records of one decision) into
  one record each, split a record that holds several decisions, delete the obsolete ones, lift behavior that sits
  in records into the area documents and fix links and index; released records stay untouched.
- `review-pr` — review an open pull request against this project's stack, analyzer, security and
  unit-test conventions, and post the findings with an explicit verdict.

Every review — before the push in `create-pr` and the squad skills, on an open pull request in `review-pr` —
runs the read-only subagent `.claude/agents/squad-reviewer.md`, which holds the checklist, the severity model
and the round semantics. The squad's members (`.squad/team.md`) run as subagents under
`.claude/agents/squad-*.md`; a hook denies them Git and GitHub writes, so only the orchestrating session
commits, pushes and posts. The pipeline, its limits and escalations are in [`.squad/routing.md`](/.squad/routing.md),
the project's guarantees, security areas and integration surface in [`.squad/project.md`](/.squad/project.md).
Working records stay under `specs/` on the work branch. A product PR never changes the squad or these
instructions (`.squad/stack.md`, `.squad/project.md`, `.squad/tools/squad_settings.py` and the project blocks of this file excepted); squad lessons become `squad` or `squad-upstream` issues in this repository (`.squad/orchestration.md`, *Squad
lessons*). The user acts as Product Manager and is asked only in the cases `.squad/routing.md` lists (*Escalation to
the Product Manager*), a feature's spec approval among them.

The reasoning behind code decisions — why something was built the way it was — is recorded by the Lead
as one decision record per decision in [`docs/decisions/`](/docs/decisions/README.md) (unreleased records are
edited in place, released ones are append-only and superseded), not in `ARCHITECTURE.md`. What holds today —
formats, limits, error behavior, guarantees — is written once in the area document of the change in
[`docs/areas/`](/docs/areas/README.md), which the same pull request updates. Read the relevant records and area
documents before changing code they cover, and do not contradict an accepted record without changing it
(unreleased) or superseding it (released).

Two rules these skills enforce that are easy to get wrong:

- **A pull request documents the change, not how it was produced.** The internal review loop — its
  pass count, its findings, the commits that resolved them — never appears in the PR title, body or
  commit messages.
- **A finding posted as a review comment gets worked in that pull request**, blocking or not, now, in
  that pull request — never deferred to "the next change that touches this code". If it
  really should not be fixed here, reply with the reason or open a linked issue now — then resolve the thread.

## Pull requests, contributing and architecture

Follow [`CONTRIBUTING.md`](/docs/CONTRIBUTING.md) for branch/PR naming (`[area] Description`), the PR
checklist in [`.github/pull_request_template.md`](/.github/pull_request_template.md), and the
stability policy. Consult [`ARCHITECTURE.md`](/docs/ARCHITECTURE.md) before changing the behavior it
describes — the guarantees listed in [`.squad/project.md`](/.squad/project.md) are deliberate, not
incidental behavior.
