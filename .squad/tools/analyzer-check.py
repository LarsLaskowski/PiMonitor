#!/usr/bin/env python3
"""Analyzer gate (Go profile): `go vet ./...` must pass for the whole module, and golangci-lint must report
no issue anywhere in a file changed since the merge base with origin/main (`--whole-files`; working tree
and untracked files included). The per-linter caps are switched off so one run prints every finding.

The base (BASE_REF in `.squad/tools/squad_settings.py`, default origin/main) and the commands are fixed
here: the script accepts one flag, `--lint-only` (the lint pass over changed shell scripts, workflows and
Dockerfiles without vet and golangci-lint, for a `patch` that touches no Go — `.squad/routing.md`, *Gates of
a patch*), and no value, so nothing user-supplied reaches the shell, git or the filesystem.

Usage, from the repository root:
    python3 .squad/tools/analyzer-check.py [--lint-only]

Exit code 0 when both pass, 1 otherwise.
"""
import os
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from analyzer_common import BASE_REF, merge_base, lint_check, lint_only_requested, lint_only_main  # noqa: E402  (shared helpers next to this script)

STEPS = [
    ("go vet", ["go", "vet", "./..."]),
    ("golangci-lint (changed files)", ["golangci-lint", "run", "--new-from-merge-base=" + BASE_REF, "--whole-files",
                                     "--max-issues-per-linter=0", "--max-same-issues=0", "./..."]),
]


def main():
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    if lint_only_requested():
        return lint_only_main()
    merge_base()  # a clear error when the base ref is missing, before golangci-lint reports a confusing one
    failed = False
    for name, command in STEPS:
        result = subprocess.run(command, capture_output=True, text=True, check=False)
        output = (result.stdout + result.stderr).strip()
        if output:
            print(output[-6000:])
        print(f"{name}: {'PASS' if result.returncode == 0 else 'FAIL'}\n")
        failed = failed or result.returncode != 0
    failed = not lint_check() or failed
    print("PASS" if not failed else "FAIL")
    return 0 if not failed else 1


if __name__ == "__main__":
    sys.exit(main())
