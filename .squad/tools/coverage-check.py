#!/usr/bin/env python3
"""Coverage gate for the squad: coverage of new/changed production lines (like SonarQube's "coverage on new code":
covered lines plus covered branch conditions over coverable lines plus conditions, the conditions read from a
Cobertura `condition-coverage` or an lcov `BRDA` record; a Go coverprofile has none, and COVERAGE_CONDITIONS = False in
`squad_settings.py` counts lines only) and overall line coverage, merged from all coverage reports
(one per test project) of the latest test run; *Test with coverage* clears the results directory first. A report
older than the last change of a tracked production or test file is refused: a merge, a formatter or an edit since
the test run has moved the lines it counts.

Supported report formats (set COVERAGE_FORMAT in `.squad/tools/squad_settings.py`; a repository with
several stack profiles lists one `(format, glob)` pair per report kind in COVERAGE_REPORTS instead, and
the hits of all of them are merged):
- `cobertura` — e.g. coverlet's `coverage.cobertura.xml` (.NET), or any Cobertura XML
- `lcov`      — `lcov.info` (Node: c8, Node's test runner, Jest, Vitest, ...)
- `go`        — a `go test -coverprofile` file

The base (BASE_REF, default origin/main), the report location, the production-code paths and the thresholds
(COVERAGE_THRESHOLD for new/changed lines, COVERAGE_OVERALL_THRESHOLD for the whole code base, both default
80) are fixed in `squad_settings.py`, not taken from the command line, so nothing user-supplied reaches git
or the filesystem.

Usage, from the repository root, after *Test with coverage* from `.squad/stack.md`:
    python3 .squad/tools/coverage-check.py

Exit code 0 when both values reach their threshold, 1 otherwise; an overall value measured over zero lines
fails too, since it would meet any threshold without checking anything. When the diff contains neither production
nor test code, the overall value is only reported: such a change cannot make coverage worse, so a gap that
already exists on the base does not fail it. A diff that only changes or deletes tests is gated, because
it can lower overall coverage.
"""
import argparse
import glob
import os
import re
import subprocess
import sys
import xml.etree.ElementTree as ET

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import squad_settings as settings  # noqa: E402  (per-repository settings next to this script)
from analyzer_common import changed_line_map  # noqa: E402  (shared helpers next to this script)

BASE_REF = getattr(settings, "BASE_REF", "origin/main")


def merge_base():
    """The merge base of BASE_REF and HEAD; a missing base ref is a clear error, not a traceback."""
    result = subprocess.run(["git", "merge-base", BASE_REF, "HEAD"], capture_output=True, text=True, check=False)
    if result.returncode != 0:
        sys.exit(f"Cannot find the merge base with {BASE_REF}: fetch it (git fetch origin <branch>) or set BASE_REF "
                 "in .squad/tools/squad_settings.py to the base branch of this repository")
    return result.stdout.strip()


def repo_path(path):
    """Normalize a report path to a repository-relative path with forward slashes."""
    return os.path.relpath(os.path.abspath(path)).replace(os.sep, "/")


def changed_lines():
    """Return {repo-relative path: set(line numbers)} of lines added or changed since the merge base with
    origin/main (working tree included) in the production paths from squad_settings."""
    pathspecs = list(settings.COVERAGE_PATHSPECS) + [f":(exclude){p}" for p in settings.COVERAGE_EXCLUDES]
    return changed_line_map(merge_base(), pathspecs)


def tests_pathspecs():
    """COVERAGE_TEST_PATHSPECS, by default the COVERAGE_EXCLUDES of squad_settings."""
    return getattr(settings, "COVERAGE_TEST_PATHSPECS", settings.COVERAGE_EXCLUDES)


def changed_test_files():
    """Return the repo-relative test files changed since the merge base with origin/main (working tree
    included): the paths in tests_pathspecs()."""
    pathspecs = tests_pathspecs()
    if not pathspecs:
        return []
    return subprocess.run(
        ["git", "diff", "--name-only", merge_base(), "--", *pathspecs],
        capture_output=True, text=True, check=True).stdout.splitlines()


def report_sources():
    """The (format, glob) pairs to read: COVERAGE_REPORTS when set (several stack profiles), otherwise the
    single COVERAGE_FORMAT / COVERAGE_REPORT_GLOB."""
    pairs = getattr(settings, "COVERAGE_REPORTS", None)
    if pairs is None:
        pairs = [(settings.COVERAGE_FORMAT, settings.COVERAGE_REPORT_GLOB)]
    for report_format, _ in pairs:
        if report_format not in LOADERS:
            sys.exit(f"Unknown coverage format '{report_format}' in squad_settings.py")
    return list(pairs)


def run_reports(pattern):
    """Every report the glob matches: one per test project of the latest run. *Test with coverage* clears the
    results directory first, so no report of an earlier run can be among them."""
    reports = sorted(glob.glob(pattern, recursive=True))
    if not reports:
        sys.exit(f"No coverage report matches {pattern} - the report was never written or "
                 "has been deleted; run *Test with coverage* from .squad/stack.md first, then this gate again")
    return reports


def refuse_stale_reports():
    """Exit when a report is older than the newest change of a tracked production or test file: its line numbers
    belong to the files as they were before that change, so it would count changed lines wrongly."""
    reports = [report for _, pattern in report_sources() for report in run_reports(pattern)]
    oldest = min(reports, key=os.path.getmtime)
    tests = tests_pathspecs()
    files = production_files() | (tracked_files(tests) if tests else set())
    newest = max(((os.path.getmtime(path), path) for path in files if os.path.isfile(path)), default=None)
    if newest and newest[0] > os.path.getmtime(oldest):
        sys.exit(f"{oldest} is older than the last change of {newest[1]}: it does not measure the files as they "
                 "are now (a merge, a formatter or an edit since the test run). Run *Test with coverage* from "
                 ".squad/stack.md again, then this gate.")


def add_hit(hits, path, number, count):
    lines = hits.setdefault(path, {})
    lines[number] = max(lines.get(number, 0), count)


def cobertura_classes(report):
    """(repo-relative path, class element) of every class of a Cobertura report."""
    # Local report from the repo's own test run; the tool stays stdlib-only, so no defusedxml.
    root = ET.parse(report).getroot()  # noqa: S314  # nosec B314
    sources = [s.text.rstrip("/\\") for s in root.iter("source") if s.text]
    for cls in root.iter("class"):
        filename = cls.get("filename")
        for src in sources:
            candidate = os.path.join(src, filename)
            if os.path.exists(candidate):
                filename = candidate
                break
        yield repo_path(filename), cls


def load_cobertura(report):
    hits = {}
    for path, cls in cobertura_classes(report):
        for ln in cls.iter("line"):
            add_hit(hits, path, int(ln.get("number")), int(ln.get("hits")))
    return hits


CONDITION_COVERAGE = re.compile(r"\((\d+)/(\d+)\)")


def add_conditions(conditions, path, number, covered, total):
    lines = conditions.setdefault(path, {})
    known = lines.get(number, (0, 0))
    lines[number] = (max(known[0], covered), max(known[1], total))


def cobertura_conditions(report):
    """{path: {line: (covered, total)}} of the branch conditions (`condition-coverage="50% (1/2)"`)."""
    conditions = {}
    for path, cls in cobertura_classes(report):
        for ln in cls.iter("line"):
            found = CONDITION_COVERAGE.search(ln.get("condition-coverage") or "")
            if found:
                add_conditions(conditions, path, int(ln.get("number")), int(found.group(1)), int(found.group(2)))
    return conditions


def lcov_conditions(report):
    """{path: {line: (covered, total)}} of the branches (`BRDA:<line>,<block>,<branch>,<taken>`, `-` never run)."""
    counts, path = {}, None
    with open(report, encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if line.startswith("SF:"):
                path = repo_path(line[3:])
            elif line.startswith("BRDA:") and path:
                parts = line[5:].split(",")
                key = (path, int(parts[0]))
                covered, total = counts.get(key, (0, 0))
                counts[key] = (covered + (parts[3] not in ("-", "0")), total + 1)
            elif line == "end_of_record":
                path = None
    conditions = {}
    for (name, number), (covered, total) in counts.items():
        add_conditions(conditions, name, number, covered, total)
    return conditions


def load_lcov(report):
    hits, path = {}, None
    with open(report, encoding="utf-8") as handle:
        for raw in handle:
            line = raw.strip()
            if line.startswith("SF:"):
                path = repo_path(line[3:])
            elif line.startswith("DA:") and path:
                number, count = line[3:].split(",")[:2]
                add_hit(hits, path, int(number), int(float(count)))
            elif line == "end_of_record":
                path = None
    return hits


def go_module():
    with open("go.mod", encoding="utf-8") as handle:
        for line in handle:
            if line.startswith("module "):
                return line.split()[1].strip()
    sys.exit("go.mod has no module line")


def load_go(report):
    module, hits = go_module(), {}
    block = re.compile(r"^(.+):(\d+)\.\d+,(\d+)\.\d+ (\d+) (\d+)$")
    with open(report, encoding="utf-8") as handle:
        for raw in handle:
            match = block.match(raw.strip())
            if not match:
                continue
            name, start, end, statements, count = match.groups()
            if int(statements) == 0:
                continue
            path = name[len(module) + 1:] if name.startswith(module + "/") else name
            for number in range(int(start), int(end) + 1):
                add_hit(hits, path, number, int(count))
    return hits


LOADERS = {"cobertura": load_cobertura, "lcov": load_lcov, "go": load_go}
CONDITION_LOADERS = {"cobertura": cobertura_conditions, "lcov": lcov_conditions}


def merged_hits():
    """Merge every report matching the globs of COVERAGE_REPORTS into one map of hits per file and line."""
    hits, merged = {}, 0
    for report_format, pattern in report_sources():
        reports = run_reports(pattern)
        merged += len(reports)
        for report in reports:
            for path, lines in LOADERS[report_format](report).items():
                for number, count in lines.items():
                    add_hit(hits, path, number, count)
    print(f"Coverage reports merged: {merged}")
    return hits


def merged_conditions():
    """The branch conditions of every report, merged like the hits; none with COVERAGE_CONDITIONS = False."""
    conditions = {}
    if not getattr(settings, "COVERAGE_CONDITIONS", True):
        return conditions
    for report_format, pattern in report_sources():
        loader = CONDITION_LOADERS.get(report_format)
        for report in run_reports(pattern) if loader else []:
            for path, lines in loader(report).items():
                for number, (covered, total) in lines.items():
                    add_conditions(conditions, path, number, covered, total)
    return conditions


def tracked_files(pathspecs, *options):
    """The files `git ls-files <options> -- <pathspecs>` lists."""
    return set(subprocess.run(["git", "ls-files", *options, "--", *pathspecs],
                              capture_output=True, text=True, check=True).stdout.splitlines())


def production_files(*options):
    """The production files `git ls-files <options>` lists: COVERAGE_PATHSPECS minus COVERAGE_EXCLUDES. The
    excludes are subtracted from a second listing, not passed as `:(exclude)` pathspecs: `git ls-files`
    (2.43) drops every tracked file for an exclude that matches none of them (`:(exclude)*.d.ts` with no
    declaration file under `src/`), which left the overall value at 0/0 lines."""
    files = tracked_files(settings.COVERAGE_PATHSPECS, *options)
    if settings.COVERAGE_EXCLUDES:
        files -= tracked_files(settings.COVERAGE_EXCLUDES, *options)
    return files


def overall_coverage(hits):
    """Return (percent, hit lines, coverable lines) over the tracked production files."""
    tracked = production_files()
    production = {path: lines for path, lines in hits.items() if path in tracked}
    total = sum(len(lines) for lines in production.values())
    total_hit = sum(1 for lines in production.values() for count in lines.values() if count > 0)
    return (total_hit / total * 100 if total else 100.0), total_hit, total


def file_coverage(lines, file_hits, file_conditions):
    """([hit lines, coverable lines, covered conditions, conditions], its rate and counts, what it leaves uncovered)
    of a file's changed lines, the two texts for the gate's report line."""
    relevant = [n for n in lines if n in file_hits]
    hit = sum(1 for n in relevant if file_hits[n] > 0)
    branches = [file_conditions[n] for n in relevant if n in file_conditions]
    met, possible = sum(c for c, _ in branches), sum(t for _, t in branches)
    missed = sorted(n for n in relevant if file_hits[n] == 0)
    partial = sorted(n for n in relevant if n in file_conditions and file_conditions[n][0] < file_conditions[n][1])
    rate = f"{(hit + met) / (len(relevant) + possible) * 100:5.1f}%" if relevant else "  n/a "
    summary = f"{rate}  {hit}/{len(relevant)}" + (f" + {met}/{possible} conditions" if possible else "")
    gaps = (f"  uncovered: {missed}" if missed else "") + (f"  conditions not covered: {partial}" if partial else "")
    return [hit, len(relevant), met, possible], summary, gaps


def new_code_coverage(hits, conditions=None):
    """Print the changed production files and return (percent, hit lines, coverable lines, covered conditions,
    conditions): the percent of lines and branch conditions together, as SonarQube computes coverage on new code."""
    totals = [0, 0, 0, 0]  # hit lines, coverable lines, covered conditions, conditions
    print("Changed production files (coverable changed lines, branch conditions on them):")
    for path, lines in sorted(changed_lines().items()):
        counts, summary, gaps = file_coverage(lines, hits.get(path, {}), (conditions or {}).get(path, {}))
        totals = [total + count for total, count in zip(totals, counts, strict=True)]
        print(f"  {summary}  {path}{gaps}")
    covered, coverable = totals[0] + totals[2], totals[1] + totals[3]
    return (covered / coverable * 100 if coverable else 100.0), *totals


def warn_untracked():
    """Name the untracked production files: neither `git diff` nor `git ls-files` sees them, so the gate
    would silently leave them out of both values and could fail at a false low percentage."""
    untracked = production_files("--others", "--exclude-standard")
    if untracked:
        print("WARNING: untracked production files ignored by this gate (stage them with `git add` first):")
        for path in sorted(untracked):
            print(f"  {path}")
        print()


def main():
    argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter).parse_args()
    threshold = float(getattr(settings, "COVERAGE_THRESHOLD", 80))
    overall_threshold = float(getattr(settings, "COVERAGE_OVERALL_THRESHOLD", threshold))

    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    warn_untracked()
    refuse_stale_reports()
    hits = merged_hits()
    overall, total_hit, total = overall_coverage(hits)
    changed = changed_lines()
    new_code, covered, coverable, met, possible = new_code_coverage(hits, merged_conditions())

    branches = f", {met}/{possible} conditions" if possible else ""
    print(f"\nNew/changed code: {new_code:.1f}% ({covered}/{coverable} lines{branches})")
    print(f"Overall:          {overall:.1f}% ({total_hit}/{total} lines)")
    gated = bool(changed) or bool(changed_test_files())
    if not gated:
        print("No production or test code changed: overall coverage is reported, not gated (the change cannot lower it).")
    unmeasured = total == 0
    if unmeasured:
        print("Overall coverage measured no line: no report line belongs to a tracked production file, so the "
              "overall threshold checks nothing - compare the report's paths with COVERAGE_PATHSPECS and "
              "COVERAGE_EXCLUDES in .squad/tools/squad_settings.py")
    ok = not gated or (not unmeasured and new_code >= threshold and overall >= overall_threshold)
    print(f"Thresholds {threshold:.0f}% on new/changed code, {overall_threshold:.0f}% overall: {'PASS' if ok else 'FAIL'}")
    return 0 if ok else 1

if __name__ == "__main__":
    sys.exit(main())
