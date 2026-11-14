#!/usr/bin/env python3
"""Name the label of a squad lesson from the files its fix changes (`.squad/orchestration.md`, *Squad lessons*).

A lesson is filed where it can be fixed. A refresh overwrites a template-managed file, so a lesson about one goes to
the template repository (`squad-upstream`); a lesson whose fix changes only this repository's own files stays here
(`squad`). The script decides it from the paths, so the label is never a judgement call:

- **managed** (`MANAGED`, `.squad/orchestration.md`, *Template-managed files*): `CLAUDE.md`, `.squad/team.md`,
  `.squad/routing.md`, `.squad/orchestration.md`, the tools of `.squad/tools/` the template ships (`TOOLS`; a
  repository's own tool there, such as a coverage converter, is its own), a profile's shared
  rule files (`.squad/*-rules.*`), `.claude/agents/squad-*.md`, `.claude/hooks/`, the template's skills, the feature
  request template, the `_template.md` files of `docs/decisions/` and `docs/areas/`, `docs/decisions/process.md`,
  `specs/README.md` and `specs/_template/` — `squad-upstream`;
- **marked** (`MARKED`): `AGENTS.md` (and `.github/copilot-instructions.md`, generated from it), `docs/CONTRIBUTING.md`,
  `docs/ARCHITECTURE.md`, the bug report and pull request templates, the `README.md` of `docs/decisions/` and
  `docs/areas/`, and `.editorconfig` with its template blocks (`squad:dotnet-rules`, `squad:sonar-rules`) —
  `squad-upstream` for the template text, `squad` for a `<!-- project:… -->` block (or the rest of `.editorconfig`);
  the path is followed by `:project` or `:template` to say which (`AGENTS.md:project`);
- everything else — seeded files (`.squad/stack.md`, `.squad/project.md`, `squad_settings.py`, `docs/UNIT_TESTS.md`,
  the profile's CI and tool configuration) and the repository's own code and documents — `squad`.

Paths of both kinds make two issues, one per label, the lesson split between them. The script prints the label (or
both), the title form and the reason per path; exit code 0, or 2 for a marked file without `:project` or `:template`.
`tools/template-check.py` of the template repository checks `MANAGED` and `MARKED` against what `apply-template`
writes, so the two cannot drift.

Usage, from the repository root:
    python3 .squad/tools/lesson-label.py <path>[:project|:template] [<path> ...] [--issue N]
"""
import argparse
import fnmatch
import re
import subprocess
import sys

UPSTREAM = "squad-upstream"
LOCAL = "squad"
# The template's skills (core/.claude/skills/ of the template repository).
SKILLS = ("create-pr", "decision-consolidate", "review-pr", "squad-issue", "squad-spec")
# The tools the template writes to .squad/tools/ (core and every profile; `analyzer-check-<profile>.py` in a repository
# with several profiles); any other file there is the repository's own. tools/template-check.py keeps the list exact.
TOOLS = (
    ".gitignore", "analyzer-check.py", "analyzer-check-*.py", "analyzer_common.py", "config-check.py",
    "coverage-check.py", "decision-check.py", "dotnet-rules.py", "dotnet_config.py", "dotnet_major.py",
    "figure-check.py", "gate-stamp.py", "lesson-label.py", "plan-check.py", "scope-check.py", "session-usage.py",
    "sonar-check.py", "sonar-rules.py", "squad-log.py", "squad-next.py", "squad-notes.py",
)
# Template-managed files, overwritten by every refresh (glob patterns; `*` stays within one folder).
MANAGED = (
    "CLAUDE.md", ".squad/team.md", ".squad/routing.md", ".squad/orchestration.md",
    *(f".squad/tools/{tool}" for tool in TOOLS),
    ".squad/*-rules.*", ".claude/agents/squad-*.md", ".claude/hooks/*",
    *(f".claude/skills/{skill}/*" for skill in SKILLS),
    ".github/ISSUE_TEMPLATE/feature_request.md", "docs/decisions/_template.md", "docs/areas/_template.md",
    "docs/decisions/process.md", "specs/README.md", "specs/_template/*",
)
# Template text around the repository's own blocks (`<!-- project:… -->`, the rest of `.editorconfig`).
MARKED = ("AGENTS.md", ".github/copilot-instructions.md", "docs/CONTRIBUTING.md", "docs/ARCHITECTURE.md",
          ".github/ISSUE_TEMPLATE/bug_report.md", ".github/pull_request_template.md", "docs/decisions/README.md",
          "docs/areas/README.md", ".editorconfig")
PART = re.compile(r":(project|template)$")
REMOTE = re.compile(r"github\.com[:/](?P<name>[^/\s]+/[^/\s]+?)(?:\.git)?/?$")


def normalized(path):
    """The repository-relative path with forward slashes and without a leading `./` or `/`."""
    path = path.strip().replace("\\", "/")
    while path.startswith(("./", "/")):
        path = path[2:] if path.startswith("./") else path[1:]
    return path


def kind(path):
    """`managed`, `marked` or `seeded` (every file the repository owns) for a repository-relative path."""
    path = normalized(path)
    if any(fnmatch.fnmatchcase(path, pattern) and path.count("/") == pattern.count("/") for pattern in MANAGED):
        return "managed"
    return "marked" if path in MARKED else "seeded"


def label(argument):
    """(label or None, the reason) for one argument: a path, a marked one followed by `:project` or `:template`."""
    part = PART.search(argument)
    path = normalized(PART.sub("", argument))
    found = kind(path)
    if found == "managed":
        return UPSTREAM, f"{path}: managed — overwritten by every refresh, fixed in the template repository"
    if found == "marked" and part is None:
        return None, (f"{path}: marked — add :template for its template text or :project for a `<!-- project:… -->` "
                      "block (in .editorconfig: the squad blocks are template text)")
    if found == "marked":
        where = "the template text" if part.group(1) == "template" else "a project block"
        return (UPSTREAM if part.group(1) == "template" else LOCAL), f"{path}: marked, {where}"
    return LOCAL, f"{path}: this repository's own (seeded or project file), fixed here"


def repository():
    """`owner/name` of the `origin` remote on GitHub, or a placeholder."""
    done = subprocess.run(["git", "remote", "get-url", "origin"], capture_output=True, text=True, check=False)
    found = REMOTE.search(done.stdout.strip()) if done.returncode == 0 else None
    return found.group("name").split("/")[-1] if found else "<this repository>"


def report(arguments, issue):
    """The lines to print and the exit code."""
    results = [label(argument) for argument in arguments]
    lines = [f"  {reason}" for _, reason in results]
    if any(found is None for found, _ in results):
        return ["Cannot decide the label:"] + lines, 2
    labels = {found for found, _ in results}
    run = f"#{issue}" if issue else "#<issue>"
    titles = {UPSTREAM: f"`[Squad] <lesson> (from {repository()}{run})`, linking the run",
              LOCAL: "`[Squad] <lesson>`, linking the run"}
    head = [f"Label: {next(iter(labels))}"] if len(labels) == 1 else [
        f"Labels: {UPSTREAM} and {LOCAL} — two issues, the lesson split by the paths below (each issue names the other)"]
    return head + [f"Title ({name}): {titles[name]}" for name in (UPSTREAM, LOCAL) if name in labels] + \
        ["Paths:"] + lines, 0


def main(argv=None):
    parser = argparse.ArgumentParser(description="The label of a squad lesson (squad-upstream or squad) from the "
                                                 "files its fix changes.")
    parser.add_argument("paths", nargs="+", help="repository-relative path; a marked file with :project or :template")
    parser.add_argument("--issue", type=int, help="the run's issue number, for the title")
    args = parser.parse_args(argv)
    lines, code = report(args.paths, args.issue)
    print("\n".join(lines))
    return code


if __name__ == "__main__":
    sys.exit(main())
