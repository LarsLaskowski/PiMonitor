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
2. **Run local verification** before pushing:
   - `go build ./...`
   - `go vet ./...`
   - `go test ./...`
   - `golangci-lint run` if installed (skip with a note if not available in
     this environment, but do not silently skip `go vet`/`go test`)
   Fix any failures before proceeding — do not open a PR with failing checks.
3. **Commit** with a concise, imperative summary line and a body explaining
   *why* the change was made if not obvious from the diff. Follow the
   language rule: English only.
4. **Push** the branch: `git push -u origin <branch-name>`.
5. **Open the PR** using the repository's template at
   `.github/pull_request_template.md`. Fill in the Summary, link the related
   issue if one exists, and check off the checklist items that are actually
   true (don't check items you haven't verified).
6. Report the PR URL back to the user.

## Notes

- Never force-push over another contributor's commits without explicit
  confirmation.
- If the change touches `/api/v1/...` response shapes, `README.md`,
  `docs/API.md`, or the systemd packaging in `packaging/`, make sure the
  corresponding documentation was updated as part of the same PR (see the
  template checklist).
