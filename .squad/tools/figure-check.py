#!/usr/bin/env python3
"""Stale-figure check: documentation that still states a value the change replaced.

A sentence in an untouched file that keeps a number, limit or default the diff made untrue is found only by a review,
at the cost of a fix round and a delta round. This script finds the candidates over the diff since the merge base with
BASE_REF (`.squad/tools/squad_settings.py`, default origin/main; working tree included):

1. The **old values** of every changed source file — production code and configuration, so anything except
   Markdown, `docs/`, `specs/`, `.squad/`, `.claude/`, the test paths (`COVERAGE_TEST_PATHSPECS`, by default
   `COVERAGE_EXCLUDES`) and `FIGURE_EXCLUDES` (lock files and SVG by default): the numbers (`512`, `0.75`, `1_000`,
   the `30` of `30s`, `8.0.1`) and the quoted constants (a token of three characters or more with a digit, one of
   `_ . / : @ -`, an inner capital or all capitals, such as `"APP_PORT"` or `"/api/v1"`) that its removed lines
   hold and its added lines no longer do. A value moved within a file is therefore no old value; one the file still
   holds on an unchanged line is reported with that note, since the documentation may state that other use. Dates
   (`2026-01-01`) are no numbers here.
2. Each old value is searched as a whole token (a number also with thousands separators, `1,000`, and with a unit
   after it, `512MB`) in the documentation: `README.md` and `SECURITY.md`, the text documents under `docs/`
   (`DOCUMENT_TYPES`; not the template-managed `docs/decisions/process.md` and `_template.md` files, not a decision
   record a fetched release tag contains, not a changelog, and only the project blocks of the marked `docs/CONTRIBUTING.md`,
   `docs/ARCHITECTURE.md`, `docs/decisions/README.md` and `docs/areas/README.md`), `.squad/project.md` and the
   project blocks of `AGENTS.md`.
3. Every hit is printed with file, line and text, the value, where the diff changed it and, when one line pair of
   the hunk shows it, the new value: a warning for the Dev or the Reviewer to confirm (the sentence states something
   else that still holds) or fix.

The values in `FIGURE_TRIVIAL` (`squad_settings.py`; by default the single digits, 10 and 100, which prose uses for
list items, steps and counts) are not searched.

The script takes no options, so nothing user-supplied reaches git, the shell or the filesystem.

Usage, from anywhere inside the repository:
    python3 .squad/tools/figure-check.py

Exit code 0, also with hits (they are warnings); 1 when the merge base cannot be found.
"""
import argparse
import os
import re
import subprocess
import sys
from dataclasses import dataclass, field

sys.dont_write_bytecode = True
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from analyzer_common import merge_base, settings  # noqa: E402  (shared helpers next to this script)

TRIVIAL = {str(value) for value in getattr(settings, "FIGURE_TRIVIAL", [*range(10), 10, 100])}
SOURCE_EXCLUDES = list(getattr(settings, "FIGURE_EXCLUDES", ["*.lock", "package-lock.json", "npm-shrinkwrap.json",
                                                             "pnpm-lock.yaml", "go.sum", "packages.lock.json", "*.svg"]))
TEST_PATHSPECS = list(getattr(settings, "COVERAGE_TEST_PATHSPECS", getattr(settings, "COVERAGE_EXCLUDES", [])))
# What is documentation, not a source: never read for old values.
NOT_SOURCES = ["*.md", "docs/", "specs/", ".squad/", ".claude/"]
DOCUMENTS = ["README.md", "SECURITY.md", "AGENTS.md", ".squad/project.md", "docs"]
# Text documents under docs/; images, SVG and generated HTML are no prose.
DOCUMENT_TYPES = (".md", ".markdown", ".txt", ".rst", ".adoc")
# Marked files: only their project blocks belong to the repository; the rest is template text.
MARKED = {"AGENTS.md", "docs/CONTRIBUTING.md", "docs/ARCHITECTURE.md", "docs/decisions/README.md", "docs/areas/README.md"}
# Template-managed documents, overwritten by every refresh.
MANAGED = {"docs/decisions/process.md", "docs/decisions/_template.md", "docs/areas/_template.md"}
DECISION_RECORD = re.compile(r"^docs/decisions/(\d{4}-[^/]+\.md)$")
CHANGELOG = re.compile(r"(?:^|/)CHANGELOG[^/]*$", re.IGNORECASE)
BLOCK_BEGIN = re.compile(r"<!-- project:begin [\w-]+ -->")
BLOCK_END = re.compile(r"<!-- project:end [\w-]+ -->")
HUNK = re.compile(r"^@@ -\d+(?:,(\d+))? \+(\d+)(?:,(\d+))? @@")
# A run of digits, underscores and dots; code_number() takes the number from it (no nested quantifiers: Sonar S8786).
CODE_RUN = re.compile(r"(?<![\w.])\d[\d_.]*")
QUOTED = re.compile(r"([\"'`])([^\"'`\s]{3,80})\1")
CONSTANT_CHARACTER = re.compile(r"[\d_./:@-]")
INNER_CAPITAL = re.compile(r"[a-z][A-Z]")
DATE = re.compile(r"\b\d{4}-\d{2}-\d{2}\b")
DOCUMENT_NUMBER = re.compile(r"(?<![\w.,])(?:\d{1,3}(?:,\d{3})+(?!\d)|\d+(?:\.\d+)*)")
STRING_EDGE = r"(?<![\w./-]){}(?![\w/-]|\.\w)"
SNIPPET = 100


@dataclass
class Hunk:
    path: str
    line: int
    removed: list = field(default_factory=list)
    added: list = field(default_factory=list)


@dataclass
class Change:
    """Where the diff dropped an old value: file, line, the new value when one line pair shows it, and whether the
    file still holds the value on another line."""
    path: str
    line: int
    new: str | None = None
    still: bool = False


def git(*args):
    return subprocess.run(["git", "-c", "core.quotePath=false", *args], capture_output=True, text=True,
                          encoding="utf-8", errors="replace", check=True).stdout


def exclusion(pathspec):
    """The pathspec as a Git exclusion; a pathspec that already excludes something is left out (it only narrows)."""
    if pathspec.startswith(":("):
        return None if "exclude" in pathspec.split(")", 1)[0] else ":(exclude," + pathspec[2:]
    if pathspec.startswith((":!", ":^")):
        return None
    return f":(exclude){pathspec}"


def source_diff(base):
    """`git diff -U0` of the source files since the merge base, working tree included; a renamed file is compared
    with its old version, not read as deleted and added."""
    excluded = [exclusion(p) for p in NOT_SOURCES + TEST_PATHSPECS + SOURCE_EXCLUDES]
    return git("diff", "-U0", "-M", "--no-ext-diff", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", base, "--",
               ".", *[p for p in excluded if p])


def read_header(line, hunks, state):
    """A line outside a hunk: the file names, or a hunk header that starts the next hunk."""
    if line.startswith(("--- a/", "+++ b/")):
        state["path"] = line[6:].rstrip("\t")  # Git ends a name with a space in a tab
    else:
        match = HUNK.match(line)
        if match and state["path"]:
            state["old"] = int(match.group(1) or "1")
            state["new"] = int(match.group(3) or "1")
            hunks.append(Hunk(state["path"], max(int(match.group(2)), 1)))


def parse_diff(text):
    """The hunks of a `git diff -U0`: file, first line on the new side, removed and added lines."""
    hunks, state = [], {"path": None, "old": 0, "new": 0}
    for line in text.splitlines():
        if not (state["old"] or state["new"]):
            read_header(line, hunks, state)
        elif line.startswith("-"):
            hunks[-1].removed.append(line[1:])
            state["old"] -= 1
        elif line.startswith("+"):
            hunks[-1].added.append(line[1:])
            state["new"] -= 1
    return hunks


def is_constant(token):
    """A quoted token that reads like a value or a key rather than a word of prose."""
    return bool(CONSTANT_CHARACTER.search(token) or INNER_CAPITAL.search(token)) or (token.isupper() and len(token) >= 3)


def code_number(run):
    """The number a CODE_RUN starts with: digit groups joined by single underscores (`1_000`), then by single dots
    (`8.0.1`); the rest of the run (`1..3`, a trailing dot, `1.5_0`) is no part of it."""
    integer, *fractions = run.split(".")
    groups = integer.split("_")
    if "" in groups:
        return "_".join(groups[:groups.index("")])
    number = integer
    for fraction in fractions:
        digits = fraction.split("_", 1)[0]
        if not digits:
            break
        number += "." + digits
        if digits != fraction:
            break
    return number


def is_number(value):
    """Whether the value is one number as code writes it (CODE_RUN, code_number)."""
    return CODE_RUN.fullmatch(value) is not None and code_number(value) == value


def values(lines):
    """The numbers and quoted constants in the lines, trivial values left out."""
    found = set()
    for line in lines:
        found.update(code_number(run).replace("_", "") for run in CODE_RUN.findall(DATE.sub(" ", line)))
        found.update(token for _, token in QUOTED.findall(line) if is_constant(token))
    return found - TRIVIAL


def same_kind(found, value):
    """The values of `found` that are numbers when `value` is one, else the constants."""
    number = is_number(value)
    return {other for other in found if is_number(other) == number}


def replacement(before, after, value):
    """The value that replaced `value` from one removed line to its added line, when it is the only one of its kind."""
    old, new = values([before]) - values([after]), values([after]) - values([before])
    replaced = same_kind(new, value)
    return next(iter(replaced)) if same_kind(old, value) == {value} and len(replaced) == 1 else None


def locate(hunk, value):
    """Where the hunk dropped `value` (its line on the new side) and, when a line pair shows it, the new value."""
    index = next(i for i, line in enumerate(hunk.removed) if value in values([line]))
    line = hunk.line + min(index, max(len(hunk.added) - 1, 0))
    paired = len(hunk.removed) == len(hunk.added)
    return Change(hunk.path, line, replacement(hunk.removed[index], hunk.added[index], value) if paired else None)


def old_values(hunks):
    """{old value: [Change]} of the diff: per file, the values its removed lines hold and its added lines do not."""
    by_file = {}
    for hunk in hunks:
        by_file.setdefault(hunk.path, []).append(hunk)
    changes = {}
    for path, file_hunks in by_file.items():
        kept = values(line for hunk in file_hunks for line in hunk.added)
        current = current_values(path)
        for hunk in file_hunks:
            for value in sorted(values(hunk.removed) - kept):
                change = locate(hunk, value)
                change.still = value in current
                changes.setdefault(value, []).append(change)
    return changes


def current_values(path):
    """The values a source file holds now (none for a deleted file)."""
    if not os.path.isfile(path):
        return set()
    with open(path, encoding="utf-8", errors="replace") as handle:
        return values(handle.read().splitlines())


def released_records():
    """The decision records a release tag contains (by file name): append-only, so they keep what was true then."""
    names = set()
    for tag in git("tag", "--list", "v*").split():
        listing = subprocess.run(["git", "ls-tree", "-z", "--name-only", f"{tag}:docs/decisions"], capture_output=True,
                                 text=True, encoding="utf-8", errors="replace", check=False).stdout
        names.update(name for name in listing.split("\0") if name)
    return names


def is_document(path, released):
    """Whether a file under the searched paths is the repository's own documentation."""
    record = DECISION_RECORD.match(path)
    if path.startswith("docs/") and not path.lower().endswith(DOCUMENT_TYPES):
        return False
    return path not in MANAGED and not CHANGELOG.search(path) and not (record and record.group(1) in released)


def documents():
    """The documentation files to search, tracked or new."""
    released = released_records()
    listed = git("ls-files", "-z", "--cached", "--others", "--exclude-standard", "--", *DOCUMENTS).split("\0")
    return [path for path in sorted(set(listed)) if path and os.path.isfile(path) and is_document(path, released)]


def owned_lines(path, text):
    """(line number, line) of what the repository owns in a document: all of it, or a marked file's project blocks."""
    lines = list(enumerate(text.splitlines(), 1))
    if path not in MARKED:
        return lines
    owned, inside = [], False
    for number, line in lines:
        if BLOCK_BEGIN.search(line) or BLOCK_END.search(line):
            inside = BLOCK_BEGIN.search(line) is not None
        elif inside:
            owned.append((number, line))
    return owned


def read_document(path):
    """The text of a document, or None for a binary file."""
    with open(path, encoding="utf-8-sig", errors="replace") as handle:
        text = handle.read()
    return None if "\0" in text else text


def matches(line, numbers, strings):
    """The old values a documentation line states: numbers (with or without thousands separators) and constants."""
    found = [token.replace(",", "") for token in DOCUMENT_NUMBER.findall(DATE.sub(" ", line))]
    hits = [value for value in dict.fromkeys(found) if value in numbers]
    return hits + [value for value, pattern in strings if value in line and pattern.search(line)]


def find_hits(changes):
    """(document, line number, line, value) for every documentation line that states an old value."""
    numbers = {value for value in changes if is_number(value)}
    strings = [(value, re.compile(STRING_EDGE.format(re.escape(value)))) for value in sorted(changes)
               if value not in numbers]
    hits = []
    for path in documents():
        text = read_document(path)
        for number, line in owned_lines(path, text) if text else []:
            hits += [(path, number, line, value) for value in matches(line, numbers, strings)]
    return hits


def place(change):
    """Where a value changed, with the new value and whether the file still holds the old one elsewhere."""
    notes = [f"now {change.new}"] if change.new else []
    notes += ["still used elsewhere in that file"] if change.still else []
    return f"{change.path}:{change.line}" + (f" ({'; '.join(notes)})" if notes else "")


def describe(path, number, line, value, changes):
    places = ", ".join(place(change) for change in changes[value][:3])
    more = f" and {len(changes[value]) - 3} more" if len(changes[value]) > 3 else ""
    text = " ".join(line.split())
    shown = text if len(text) <= SNIPPET else text[:SNIPPET].rsplit(" ", 1)[0] + " …"
    return (f"WARNING: {path}:{number}: states {value}, which the diff changed in {places}{more} - confirm the "
            f"sentence still holds or fix it: \"{shown}\"")


def main():
    argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter).parse_args()
    sys.stdout.reconfigure(errors="replace")  # documentation text on a console that cannot show every character
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    hunks = parse_diff(source_diff(merge_base()))
    changes = old_values(hunks)
    hits = find_hits(changes) if changes else []
    for path, number, line, value in hits:
        print(describe(path, number, line, value, changes))
    files = len({hunk.path for hunk in hunks})
    print(f"\nChecked {len(changes)} changed values from {files} source files: {len(hits)} documentation lines to "
          "confirm or fix")
    return 0


if __name__ == "__main__":
    sys.exit(main())
