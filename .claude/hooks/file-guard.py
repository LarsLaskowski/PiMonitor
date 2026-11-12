#!/usr/bin/env python3
"""PreToolUse hook for the squad subagents: a member writes only the files it owns (`.squad/team.md`, *Files*).

`.claude/settings.json` declares it for `Write`, `Edit`, `MultiEdit` and `NotebookEdit`, next to `git-guard.py` for
`Bash` (see there why the project settings and not the members' front matter), so it runs before every file write of
the session and of its subagents. It decides only for a squad member (the hook input's `agent_type` is `squad-…`), so
the orchestrator and other agents write as before, and only for a file inside the repository's working tree
(`CLAUDE_PROJECT_DIR`, else the hook input's `cwd`): a scratch worktree or directory outside it stays free for
experiments. A Git working tree nested in it (a worktree under `.claude/worktrees/`) is judged by the paths inside that
tree. It reads the hook input JSON from stdin and denies, by `agent_type`:

- every member: a file under `.git/` or `.claude/`, and one under `.squad/` other than `.squad/stack.md` for
  `squad-code-officer` (its *Analyzer gate* notes);
- `squad-lead`: a file outside `specs/` and `docs/decisions/`;
- `squad-tester`: a file outside the test paths, the Markdown files under their directories (a fixture README) and
  `docs/UNIT_TESTS.md`. The test paths are `COVERAGE_TEST_PATHSPECS` in `.squad/tools/squad_settings.py`, by default
  its `COVERAGE_EXCLUDES` (as `coverage-check.py` reads them), matched as Git pathspecs: a path, a directory, or a
  wildcard whose `*` also matches `/`, with `:(exclude)` / `:!` exclusions; a test path's directory is an inclusion
  up to its last `/`, matched against the folders above the file (`tests` of `tests/*.cs`, `src/**/*.Tests` of
  `src/**/*.Tests/*.cs`; `*_test.go` names none);
- `squad-devils-advocate`, `squad-security` and `squad-reviewer`: every file (they are read-only).

`squad-dev`, `squad-code-officer` and any other `squad-*` agent write everything else. A denied call answers
`{"hookSpecificOutput": {"permissionDecision": "deny", …}}` with the reason, so the member reports the need to the
orchestrator instead. Anything else, including input the hook cannot parse and a Tester's write while
`squad_settings.py` cannot be loaded, is left to the normal permission flow (exit 0, no decision).

The guard prevents accidents; it is not a security boundary. A member's Bash still writes any file (a redirect,
`sed -i`, an interpreter), and the rules in `.squad/team.md` bind there all the same.
"""
import fnmatch
import importlib.util
import json
import os
import sys

sys.dont_write_bytecode = True  # loading squad_settings.py must leave no __pycache__ in the working tree

MEMBER_PREFIX = "squad-"
FILE_TOOLS = {"Write", "Edit", "MultiEdit", "NotebookEdit"}
PROTECTED = {".git", ".claude", ".squad"}
# The protected files a member owns all the same.
OWN_PROTECTED = {"squad-code-officer": {".squad/stack.md"}}
READ_ONLY = {"squad-devils-advocate", "squad-security", "squad-reviewer"}
LEAD = "squad-lead"
LEAD_PATHS = ("specs/", "docs/decisions/")
TESTER = "squad-tester"
TEST_DOCUMENT = "docs/UNIT_TESTS.md"
SETTINGS = ".squad/tools/squad_settings.py"
SHORT_MAGIC = {"!": "exclude", "^": "exclude", "/": "top"}


def same_path(a, b):
    return os.path.normcase(a) == os.path.normcase(b)


def project_root(data):
    """The working tree the guard watches: CLAUDE_PROJECT_DIR, else the session's `cwd` from the hook input."""
    root = os.environ.get("CLAUDE_PROJECT_DIR") or data.get("cwd")
    return os.path.realpath(root) if isinstance(root, str) and root else None


def tree_path(root, path):
    """(working tree, path inside it with `/` separators) of a file below root, where a Git working tree nested in
    root holds its own files; None for a file outside root."""
    path = os.path.realpath(os.path.join(root, path))
    try:
        inside = same_path(os.path.commonpath([root, path]), root) and not same_path(path, root)
    except ValueError:  # another drive
        return None
    if not inside:
        return None
    tree = os.path.dirname(path)
    while not same_path(tree, root) and not os.path.exists(os.path.join(tree, ".git")):
        tree = os.path.dirname(tree)
    return tree, os.path.relpath(path, tree).replace(os.sep, "/")


def split_magic(pathspec):
    """(magic words, pattern) of a Git pathspec: `:(exclude,glob)p`, the short forms `:!p`, `:^p`, `:/p`, or `p`."""
    if pathspec.startswith(":("):
        words, _, pattern = pathspec[2:].partition(")")
        return {word.strip() for word in words.split(",")}, pattern
    if not pathspec.startswith(":"):
        return set(), pathspec
    rest, magic = pathspec[1:], set()
    while rest[:1] in SHORT_MAGIC:
        magic.add(SHORT_MAGIC[rest[0]])
        rest = rest[1:]
    return magic, rest.removeprefix(":")


def pathspec_matches(rel, pattern):
    """Git's default pathspec match: the path itself, a directory above it, or a wildcard whose `*` also matches `/`
    (a `**/` also matches no directory, as in a `:(glob)` pathspec)."""
    pattern = pattern.removeprefix("./").rstrip("/")
    if not pattern or rel == pattern or rel.startswith(pattern + "/"):
        return True
    return any(fnmatch.fnmatchcase(rel, variant) for variant in {pattern, pattern.replace("**/", "")})


def is_test_path(rel, pathspecs):
    """Whether the pathspecs select the path, as `git diff -- <pathspecs>` would: no exclusion matches it, and an
    inclusion does (or there are only exclusions)."""
    parsed = [split_magic(pathspec) for pathspec in pathspecs]
    includes = [pattern for magic, pattern in parsed if "exclude" not in magic]
    excludes = [pattern for magic, pattern in parsed if "exclude" in magic]
    if not parsed or any(pathspec_matches(rel, pattern) for pattern in excludes):
        return False
    return not includes or any(pathspec_matches(rel, pattern) for pattern in includes)


def test_document(rel, pathspecs):
    """Whether `rel` is a Markdown file under the directory of an included test path (a fixture README under
    `tests/` for `tests/*.cs`, under `src/App.Tests/` for `src/**/*.Tests/*.cs`); an inclusion without a directory
    (`*_test.go`) gives none, and an exclusion that matches the file wins."""
    if not rel.lower().endswith(".md"):
        return False
    parsed = [split_magic(pathspec) for pathspec in pathspecs]
    if any(pathspec_matches(rel, pattern) for magic, pattern in parsed if "exclude" in magic):
        return False
    parents = [rel.rsplit("/", depth)[0] for depth in range(1, rel.count("/") + 1)]
    for magic, pattern in parsed:
        pattern = pattern.removeprefix("./").rstrip("/")
        directory = pattern.rsplit("/", 1)[0] if "/" in pattern else ""
        if "exclude" not in magic and directory and any(
                fnmatch.fnmatchcase(parent, variant) for parent in parents
                for variant in {directory, directory.replace("**/", "")}):
            return True
    return False


def tester_pathspecs(tree):
    """COVERAGE_TEST_PATHSPECS of the tree's squad_settings.py, by default its COVERAGE_EXCLUDES; None when the file
    cannot be loaded or names neither."""
    try:
        spec = importlib.util.spec_from_file_location("squad_settings_of_the_tree", os.path.join(tree, SETTINGS))
        settings = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(settings)
    except Exception:  # noqa: BLE001  (a missing or broken settings file leaves the decision to the permission flow)
        return None
    pathspecs = getattr(settings, "COVERAGE_TEST_PATHSPECS", getattr(settings, "COVERAGE_EXCLUDES", None))
    return [str(pathspec) for pathspec in pathspecs] if isinstance(pathspecs, (list, tuple)) else None


def tester_verdict(rel, tree):
    if rel == TEST_DOCUMENT:
        return None
    pathspecs = tester_pathspecs(tree)
    if pathspecs is None or is_test_path(rel, pathspecs) or test_document(rel, pathspecs):
        return None
    return (f"the Tester writes only the test paths ({', '.join(pathspecs) or 'none'}: COVERAGE_TEST_PATHSPECS in "
            f"{SETTINGS}, by default its COVERAGE_EXCLUDES), the Markdown files under their directories and "
            f"{TEST_DOCUMENT}; .squad/project.md (*Test doubles*): report the rows verbatim, the orchestrator writes "
            "them")


def ownership_verdict(agent, rel, tree):
    """Why `agent` may not write the file at `rel` of the working tree `tree`, or None when it may."""
    if agent in READ_ONLY:
        return "a read-only member writes no file"
    top = rel.split("/", 1)[0].lower()
    if top in PROTECTED and rel not in OWN_PROTECTED.get(agent, ()):
        return f"no member writes under {top}/"
    if agent == LEAD and not rel.startswith(LEAD_PATHS):
        return "the Lead writes only under specs/ and docs/decisions/"
    return tester_verdict(rel, tree) if agent == TESTER else None


def written_path(data):
    """The file a Write, Edit, MultiEdit or NotebookEdit call writes, or None."""
    tool_input = data.get("tool_input")
    if data.get("tool_name") not in FILE_TOOLS or not isinstance(tool_input, dict):
        return None
    path = tool_input.get("file_path") or tool_input.get("notebook_path")
    return path if isinstance(path, str) and path else None


def deny_reason(data):
    """The reason to deny the file write described by the hook input, or None when it may run."""
    if not isinstance(data, dict):
        return None
    agent, path, root = data.get("agent_type"), written_path(data), project_root(data)
    if not isinstance(agent, str) or not agent.startswith(MEMBER_PREFIX) or path is None or root is None:
        return None  # the orchestrator or another agent, or no file write
    located = tree_path(root, path)
    if located is None:
        return None  # outside the working tree: a scratch worktree or directory
    why = ownership_verdict(agent, located[1], located[0])
    if why is None:
        return None
    return (f"{agent} may not write {located[1]}: {why} (.squad/team.md, Files). Report the change you need to the "
            "orchestrator instead.")


def main():
    """Exit code 0 either way: a denial is the JSON decision on stdout, not an error."""
    try:
        data = json.load(sys.stdin)
    except ValueError:
        return
    reason = deny_reason(data)
    if reason is not None:
        print(json.dumps({"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
                                                 "permissionDecisionReason": reason}}))


if __name__ == "__main__":
    main()
