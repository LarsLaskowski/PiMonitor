#!/usr/bin/env python3
"""Scope gate for a product change: what a squad or `create-pr` change may touch, checked against the diff
instead of remembered by a reviewer.

Checks, over every file changed since the merge base with BASE_REF (working tree and untracked files
included; BASE_REF from `.squad/tools/squad_settings.py`, default origin/main):

- no squad or instruction file is changed: `.squad/` except `stack.md`, `project.md` and, outside a fork,
  `tools/squad_settings.py`, `.claude/`, `CLAUDE.md` (`.squad/routing.md`, *Scope of a product PR*; squad lessons become issues);
- a marked file (`AGENTS.md` and its Copilot mirror `.github/copilot-instructions.md`, `docs/CONTRIBUTING.md`,
  `docs/ARCHITECTURE.md`, the bug report and pull request templates, the decision and area index) is changed only
  inside its `<!-- project:… -->` blocks; `AGENTS.md` and its mirror are neither added nor deleted, and a project
  block they share is the same in both (a pull request from a fork changes neither file at all);
- no changed Markdown file contains a control character (Unicode categories Cc, Cf, Zl, Zp other than tab
  and line break);
- with `--no-specs` (after squad step 10, and in every review after the PR is open): no working record
  under `specs/` is left in the diff, only `specs/README.md` and `specs/_template/` may change;
- with `--tier docs`: every changed file is product documentation as the `docs` tier defines it
  (`README.md`, `SECURITY.md`, `docs/` except `docs/decisions/` and `docs/areas/`, `.squad/stack.md`,
  `.squad/project.md`, the project blocks of `AGENTS.md` and its mirror, the bug report and pull request
  templates) or a working record under `specs/`;
- with `--tier patch`: the files outside `specs/` change by at most PATCH_MAX_LINES lines in all (added plus
  removed, an untracked file with every line; default 30, `squad_settings.py`), and no binary file changes;
- with `--tier tests`: every changed file outside `specs/` is a test path (`COVERAGE_TEST_PATHSPECS` of
  `squad_settings.py`, by default its `COVERAGE_EXCLUDES`) or `docs/UNIT_TESTS.md`.

When BASE_REF is not an ancestor of HEAD (a stale local copy in a fresh session, or a base that moved on), the
script prints a warning above the result: the file list may then include changes that are not this branch's, so
fetch the base (`git fetch origin main`) and merge it first. The warning does not change the exit code.

With `--pull-request NUMBER` (the fork-scope workflow, which never checks out the pull request) the changed files
and their contents come from the GitHub API instead of a working tree: `GITHUB_REPOSITORY` and `GITHUB_TOKEN`
from the environment, the file list of the pull request (a rename counts under both names), the text before at the
merge base and the text after at the head commit, in the head repository. Every check above applies except
`--tier`, which needs a working tree and is refused with it.

The script takes only these options, and `NUMBER` is an integer, so nothing user-supplied reaches git, the shell
or the filesystem; the API paths are quoted and the repositories checked against `owner/name`.

Usage, from the repository root:
    python3 .squad/tools/scope-check.py [--tier docs|patch|tests|trivial|standard|security] [--no-specs]
    python3 .squad/tools/scope-check.py --pull-request NUMBER [--no-specs]

Exit code 0 when the diff is in scope, 1 otherwise.
"""
import argparse
import http.client
import json
import os
import re
import subprocess
import sys
import urllib.parse

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from analyzer_common import BASE_REF, control_characters, git, merge_base, settings  # noqa: E402  (shared helpers next to this script)

SQUAD_FILES_ALLOWED = {".squad/stack.md", ".squad/project.md"}
# The repository's own settings (coverage exclusions, thresholds): a product PR may change them, a fork's never
# (the file is executed Python).
SETTINGS_FILE = ".squad/tools/squad_settings.py"
INSTRUCTION_FILES = {"CLAUDE.md"}
RULES, MIRROR = "AGENTS.md", ".github/copilot-instructions.md"
# The rules every agent reads: a product PR of the squad may change their project blocks (identically in both), a
# pull request from a fork never touches them.
RULE_FILES = (RULES, MIRROR)
IMPORT_LINE = re.compile(r"^@[^\s@]\S*[ \t]*\n", re.M)
MARKED = [RULES, MIRROR, "docs/CONTRIBUTING.md", "docs/ARCHITECTURE.md",
          ".github/ISSUE_TEMPLATE/bug_report.md",
          ".github/pull_request_template.md", "docs/decisions/README.md", "docs/areas/README.md"]
DOCS_TIER = ["README.md", "SECURITY.md", ".squad/stack.md", ".squad/project.md", RULES, MIRROR,
             ".github/ISSUE_TEMPLATE/bug_report.md", ".github/pull_request_template.md"]
DOCS_TIER_DIRS = ["docs/"]
DOCS_TIER_EXCLUDED = ["docs/decisions/", "docs/areas/"]
PROJECT_BLOCK = re.compile(r"<!-- project:begin ([\w-]+) -->\n.*?<!-- project:end \1 -->\n?", re.S)
PATCH_MAX_LINES = getattr(settings, "PATCH_MAX_LINES", 30)
TEST_DOCUMENT = "docs/UNIT_TESTS.md"
GITHUB_API_HOST = "api.github.com"
REPOSITORY = re.compile(r"^[\w.-]+/[\w.-]+$")
FILES_PER_PAGE = 100
MAX_FILE_PAGES = 30  # the API lists at most 3000 files of a pull request


def changed_files(base):
    names = git("diff", "--name-only", base).splitlines()
    names += git("ls-files", "--others", "--exclude-standard").splitlines()
    return sorted({n.strip() for n in names if n.strip()})


def is_squad_file(path, fork=False):
    if path in SQUAD_FILES_ALLOWED or (path == SETTINGS_FILE and not fork):
        return False
    return path.startswith((".squad/", ".claude/")) or path in INSTRUCTION_FILES or (fork and path in RULE_FILES)


def is_working_record(path):
    return path.startswith("specs/") and path != "specs/README.md" and not path.startswith("specs/_template/")


def is_docs_tier(path):
    if path in DOCS_TIER or is_working_record(path):
        return True
    return (path.endswith(".md") and path.startswith(tuple(DOCS_TIER_DIRS))
            and not path.startswith(tuple(DOCS_TIER_EXCLUDED)))


def outside_blocks(text):
    return PROJECT_BLOCK.sub("", text.replace("\r\n", "\n"))


def read_text(path):
    with open(path, encoding="utf-8-sig", errors="replace") as handle:
        return handle.read()


class LocalSource:
    """The text of a file before (at the merge base) and after (in the working tree); None when it does not exist."""

    def __init__(self, base):
        self.base = base

    def before(self, path):
        result = subprocess.run(["git", "show", f"{self.base}:{path}"], capture_output=True, text=True,
                                encoding="utf-8", errors="replace", check=False)
        return result.stdout.lstrip("\ufeff") if result.returncode == 0 else None

    def after(self, path):
        return read_text(path) if os.path.isfile(path) else None


def api_get(path, token, raw=False):
    """The body of a GitHub API GET (the raw file text with `raw`), None on 404; any other failure ends the run."""
    accept = "application/vnd.github.raw+json" if raw else "application/vnd.github+json"
    connection = http.client.HTTPSConnection(GITHUB_API_HOST, timeout=30)
    try:
        connection.request("GET", path, headers={"Authorization": f"Bearer {token}", "Accept": accept,
                                                 "User-Agent": "scope-check", "X-GitHub-Api-Version": "2022-11-28"})
        response = connection.getresponse()
        body = response.read().decode("utf-8-sig", errors="replace")
    except OSError as error:
        sys.exit(f"GitHub API {path}: {error}")
    finally:
        connection.close()
    if response.status == 404:
        return None
    if response.status != 200:
        sys.exit(f"GitHub API {path}: HTTP {response.status}")
    return body


def api_json(path, token):
    body = api_get(path, token)
    if body is None:
        sys.exit(f"GitHub API {path}: not found")
    return json.loads(body)


def checked_repository(name):
    if not REPOSITORY.match(name):
        sys.exit(f"Unexpected repository name from the GitHub API: {name!r}")
    return name


class PullRequestSource:
    """The files of a pull request and their text before (merge base, base repository) and after (head commit,
    head repository), read through the GitHub API; the pull request is never checked out."""

    def __init__(self, repository, number, token):
        self.token = token
        self.number = number
        self.repository = checked_repository(repository)
        pull = api_json(f"/repos/{self.repository}/pulls/{number}", token)
        head_repository = (pull["head"].get("repo") or {}).get("full_name") or self.repository
        self.head_repository = checked_repository(head_repository)
        self.head_sha = pull["head"]["sha"]
        compare = api_json(f"/repos/{self.repository}/compare/{pull['base']['sha']}...{self.head_sha}", token)
        self.merge_base = compare["merge_base_commit"]["sha"]

    def changed_files(self):
        names = set()
        for page in range(1, MAX_FILE_PAGES + 1):
            rows = api_json(f"/repos/{self.repository}/pulls/{self.number}/files?per_page={FILES_PER_PAGE}"
                            f"&page={page}", self.token)
            for row in rows:
                names.update(name for name in (row["filename"], row.get("previous_filename")) if name)
            if len(rows) < FILES_PER_PAGE:
                return sorted(names)
        sys.exit(f"Pull request {self.number} lists more than {FILES_PER_PAGE * MAX_FILE_PAGES} files")

    def contents(self, repository, path, ref):
        return api_get(f"/repos/{repository}/contents/{urllib.parse.quote(path, safe='/')}?ref={ref}", self.token,
                       raw=True)

    def before(self, path):
        return self.contents(self.repository, path, self.merge_base)

    def after(self, path):
        return self.contents(self.head_repository, path, self.head_sha)


def marked_file_changed_outside_blocks(path, source):
    before, after = source.before(path), source.after(path)
    if before is None or after is None:
        return False  # added or deleted: a different kind of finding, not an edit outside the blocks
    return outside_blocks(before) != outside_blocks(after)


def control_character_error(path, source):
    text = source.after(path) if path.endswith(".md") else None
    found = control_characters(text) if text else None
    return f"{path}: control characters {', '.join(found)} (write them as text escapes)" if found else None


def rule_file_added_or_deleted(path, source):
    return path in RULE_FILES and (source.before(path) is None or source.after(path) is None)


def file_errors(path, source, tier, no_specs, fork=False):
    """Every scope finding for one changed file."""
    findings = [
        (is_squad_file(path, fork), f"{path}: squad or instruction file changed in a product change "
                                    "(file a squad issue instead, .squad/orchestration.md *Squad lessons*)"),
        (not fork and rule_file_added_or_deleted(path, source),
         f"{path}: instruction file added or deleted in a product change"),
        (path in MARKED and marked_file_changed_outside_blocks(path, source),
         f"{path}: changed outside its <!-- project:… --> blocks (the rest is template-managed)"),
        (no_specs and is_working_record(path),
         f"{path}: working record still in the diff (squad step 10 removes specs/<folder>/)"),
        (tier == "docs" and not is_docs_tier(path),
         f"{path}: not product documentation - tier docs allows no such file (raise the tier)"),
    ]
    errors = [message for applies, message in findings if applies]
    control = control_character_error(path, source)
    return errors + ([control] if control else [])


def patch_errors(base):
    """The `patch` tier's budget: lines added plus removed outside `specs/` (an untracked file counts every line),
    and no binary file."""
    errors, lines = [], 0
    for row in git("diff", "--numstat", base).splitlines():
        added, removed, path = row.split("\t", 2)
        if is_working_record(path):
            continue
        if added == "-":
            errors.append(f"{path}: binary file changed - tier patch allows only text edits (raise the tier)")
            continue
        lines += int(added) + int(removed)
    for path in git("ls-files", "--others", "--exclude-standard").splitlines():
        if path.strip() and not is_working_record(path) and os.path.isfile(path):
            lines += len(read_text(path).splitlines())
    if lines > PATCH_MAX_LINES:
        errors.append(f"{lines} changed lines outside specs/: tier patch allows at most {PATCH_MAX_LINES} "
                      "(raise the tier)")
    return errors


def tests_errors(base, files):
    """The `tests` tier: every changed file outside `specs/` is a test path or the test document."""
    pathspecs = [str(p) for p in getattr(settings, "COVERAGE_TEST_PATHSPECS", getattr(settings, "COVERAGE_EXCLUDES", []))]
    tests = set()
    if pathspecs:
        tests = set(git("diff", "--name-only", base, "--", *pathspecs).splitlines())
        tests |= set(git("ls-files", "--others", "--exclude-standard", "--", *pathspecs).splitlines())
    return [f"{path}: not test code - tier tests allows only the test paths ({', '.join(pathspecs) or 'none set'}: "
            "COVERAGE_TEST_PATHSPECS in squad_settings.py) and docs/UNIT_TESTS.md (raise the tier)"
            for path in files if path not in tests and path != TEST_DOCUMENT and not is_working_record(path)]


TIER_CHECKS = {"patch": lambda base, files: patch_errors(base), "tests": tests_errors}


def project_blocks(text):
    """{block name: content} of a text's project blocks, Claude Code import lines left out (the Copilot mirror has
    none)."""
    return {match.group(1): IMPORT_LINE.sub("", match.group(0)) for match in PROJECT_BLOCK.finditer(
        (text or "").replace("\r\n", "\n"))}


def rule_block_errors(files, source):
    """A project block changed in AGENTS.md or its Copilot mirror is the same in both."""
    if not any(path in RULE_FILES for path in files):
        return []
    texts = [source.after(path) for path in RULE_FILES]
    if None in texts:
        return []  # a deleted file is its own finding
    rules, mirror = (project_blocks(text) for text in texts)
    return [f"{RULE_FILES[0]} and {RULE_FILES[1]} differ in the project block '{name}' - make the same change in "
            "both" for name in sorted(set(rules) | set(mirror)) if rules.get(name) != mirror.get(name)]


def check(files, base, tier, no_specs, source=None):
    fork = source is not None  # the pull-request mode checks a fork's pull request
    source = source or LocalSource(base)
    errors = [error for path in files for error in file_errors(path, source, tier, no_specs, fork)]
    errors += [] if fork else rule_block_errors(files, source)
    return errors + (TIER_CHECKS[tier](base, files) if tier in TIER_CHECKS else [])


def base_is_stale():
    """True when BASE_REF is not an ancestor of HEAD, so the diff against the merge base may show foreign files."""
    result = subprocess.run(["git", "merge-base", "--is-ancestor", BASE_REF, "HEAD"], capture_output=True, check=False)
    return result.returncode == 1


def pull_request_source(number):
    repository, token = os.environ.get("GITHUB_REPOSITORY"), os.environ.get("GITHUB_TOKEN")
    if not repository or not token:
        sys.exit("--pull-request needs GITHUB_REPOSITORY and GITHUB_TOKEN in the environment")
    return PullRequestSource(repository, number, token)


def printable(text):
    """The text with every control character escaped (`\\n`, `\\x1b`, `\\u2028`): a file name of a fork pull request
    may hold a newline, and a line it starts with `::` would run as a GitHub Actions workflow command (#361)."""
    return "".join(char if char.isprintable() or char == " " else repr(char)[1:-1] for char in text)


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--tier", choices=["docs", "patch", "tests", "trivial", "standard", "security"])
    parser.add_argument("--no-specs", action="store_true", help="no working record may be left under specs/")
    parser.add_argument("--pull-request", type=int, metavar="NUMBER",
                        help="check this pull request through the GitHub API instead of the working tree")
    args = parser.parse_args()
    if args.pull_request is not None and args.tier:
        parser.error("--tier needs a working tree and cannot be combined with --pull-request")
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    if args.pull_request is not None:
        source = pull_request_source(args.pull_request)
        base, files = source.merge_base, source.changed_files()
    else:
        source = None
        base = merge_base()
        files = changed_files(base)
    errors = check(files, base, args.tier, args.no_specs, source)
    if source is None and base_is_stale():
        print(f"WARNING: {BASE_REF} is not an ancestor of HEAD: fetch it (git fetch origin main) and merge it, or "
              "the changed files below may include changes of the base branch")
    for error in errors:
        print(f"ERROR: {printable(error)}")
    print(f"\nChecked {len(files)} changed files: {'PASS' if not errors else 'FAIL'}")
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main())
