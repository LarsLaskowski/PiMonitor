---
name: create-pr
description: Use when the user asks to open/create a pull request for PiMonitor changes on this branch. Runs local verification (build, vet, test, lint), then pushes the branch and opens a PR following this repo's pull request template.
---

# Create PR

Use this skill to prepare and open a pull request for changes made in this
repository.

## Steps

1. **Verify the working tree**: run `git status` and `git diff` to confirm
   what will be included. Do not include unrelated or uncommitted work the
   user didn't ask for.
2. **Verify tests exist** for what the diff changes. Per [`TESTS.md`](../../../docs/TESTS.md),
   tests are mandatory for new/changed behavior, not optional — if the diff adds or
   changes logic without a corresponding test, write one before proceeding (following
   `TESTS.md`'s naming, fixture, and table-driven conventions) rather than opening the PR
   without coverage.
3. **Run local verification** before pushing:
   - `go build ./...`
   - `go vet ./...`
   - `go test ./... -race -cover`
   - `golangci-lint run` if installed (skip with a note if not available in
     this environment, but do not silently skip `go vet`/`go test`)
   Fix any failures before proceeding — do not open a PR with failing checks.
4. **Commit** with a concise, imperative summary line and a body explaining
   *why* the change was made if not obvious from the diff. Follow the
   language rule: English only.
5. **Push** the branch: `git push -u origin <branch-name>`.
6. **Open the PR** using the repository's template at
   `.github/pull_request_template.md`. Fill in Description, Issues (link the
   related issue if one exists), Reviewer Notes, and Test Plan, and check off
   the checklist items that are actually true (don't check items you haven't
   verified) — including the REST API/configuration/packaging section when
   applicable, not just the General section.
7. Report the PR URL back to the user.

## Notes

- Do not add any Codex/Anthropic attribution to commits or PRs created via
  this skill: omit `Co-Authored-By: Codex ...` and `Codex-Session: ...`
  trailers from commit messages, and omit the "Generated with Codex"
  line and session link from the PR body.
- Never force-push over another contributor's commits without explicit
  confirmation.
- If the change touches `/api/v1/...` response shapes, `README.md`,
  `docs/API.md`, a documented design decision in `ARCHITECTURE.md`, or the
  systemd packaging in `packaging/`, make sure the corresponding
  documentation was updated as part of the same PR (see the template
  checklist). See [`CONTRIBUTING.md`](../../../docs/CONTRIBUTING.md) for the full
  workflow and stability policy this skill follows.
