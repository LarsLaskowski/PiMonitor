---
name: fix-issue
description: Use when the user asks to fix a specific GitHub issue in PiMonitor. Reproduces the problem (ideally as a failing test), implements a minimal fix, and opens a PR referencing the issue.
---

# Fix Issue

Use this skill to resolve a reported GitHub issue in this repository.

## Steps

1. Read the issue in full, including comments — note the reported
   environment (Pi model, distribution, PiMonitor version) since bugs in the
   metric collectors are often hardware/OS-specific.
2. **Create a branch** for the fix off the latest default branch, e.g.
   `fix-issue-<number>-<short-slug>`. Do not commit directly to the default
   branch.
3. **Reproduce** the problem locally where possible:
   - For parser bugs (`internal/collector/*.go`), write a failing unit test
     using a fixture that captures the reported input (e.g. an actual
     `/proc/meminfo` or `apt list --upgradable` output from the issue).
   - For issues that only manifest on real Pi hardware (e.g. a thermal zone
     path that doesn't exist on some model), reproduce with the closest
     available fixture/mock and note in the PR that hardware verification is
     still needed.
4. Implement the **minimal** fix — do not refactor unrelated code or expand
   scope beyond what the issue describes.
5. Confirm the previously-failing test now passes, and run the full suite
   (`go build ./...`, `go vet ./...`, `go test ./...`, and `golangci-lint run`
   if available) to check for regressions.
6. **Commit** the fix with a concise, imperative summary line (English only)
   and a body explaining *why* the change was made if not obvious from the
   diff.
7. **Push** the branch (`git push -u origin <branch-name>`) and **open a PR**
   referencing the issue (`Closes #<number>`), following the `create-pr`
   skill's verification and template steps — do not skip the push/PR-creation
   steps even if verification already ran in step 5.
8. If the fix is not fully verifiable without physical Pi hardware, say so
   explicitly in the PR description rather than claiming full verification.
