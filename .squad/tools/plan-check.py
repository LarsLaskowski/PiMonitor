#!/usr/bin/env python3
"""Plan gate: the structure of a squad work folder checked by a script instead of remembered by a reviewer.

Run by the orchestrator after every Lead launch that writes or revises the work folder (step 2), before any
other member reads it; a finding goes back to the Lead (mode `revise`, the findings only), without a challenge
round (*Loop limits* in `.squad/routing.md`).

Checks, in the work folder `specs/<folder>/`:

- **Feature** (`spec.md` exists): every section of the spec template is filled — *Problem*, *Scenarios*,
  *Errors and edge cases*, *Non-functional requirements*, *Acceptance criteria*, *Out of scope*, *Open
  questions*, *Area documents* ("none" counts as filled, the template's guidance comments do not); there is at
  least one scenario `S<n>` and one criterion written `- AC<n>: …` (a leftover checkbox, `- [ ] AC<n>: …`, is
  accepted and means nothing), and a line that names a criterion in
  another form is reported; *Non-functional requirements* says "none" or numbers them `- N<n>: …`; every
  scenario, edge case `E<n>` and non-functional requirement is named by a criterion or listed under *Out of
  scope*; a small feature (at most `SMALL_FEATURE_CRITERIA` criteria, default 3, and a plan without security
  areas) may write *Scenarios* as "none", its criteria being its scenarios; every open question is answered
  (`Answered:`) or the section says "none"; `plan.md` defines no
  criterion of its own; every criterion of the spec has a row in the plan's *Tests per criterion* and appears in
  the *Criteria* column of `tasks.md` (every table of it), and neither names a criterion the spec lacks; every
  task row names its *Group* (the unit steps 4–6 run per, with one agent per member and mode), and a group's rows are
  consecutive, except the rows of group `PM`: tasks of the Product Manager and the Lead (a probe, a survey run, a
  fact correction), which steps 4–6 skip and only they may own; when the plan's *Signatures* lists any API, every
  group but `PM` has a *Skeleton* row (its *Task* cell starts with "Skeleton") or a row saying "no new API", a
  group of documentation rows alone excepted (no row is
  the Lead's outside `specs/` and `docs/decisions/`), and every name in
  *Signatures* (the identifier of each backticked declaration) appears in a skeleton row of some group. A
  *Signatures* section that starts with "none" or states that the change adds no API ("no new API", "no public API
  changes", "no skeleton") lists none, whatever else it puts in backticks.
- **Bug fix** (no `spec.md`): `plan.md` defines its acceptance criteria, and each has a row in *Tests per
  criterion* (tier `trivial`, which writes no tests first, needs none).
- **Amendments** (a criterion changed after approval, in the file that defines the criteria): the *Amendments*
  table has the columns Criterion, Change, Reason, Step and Decided by, every cell filled; each row names a
  criterion the file defines, or one it no longer defines with a change that starts with `dropped`; it is decided
  by the Lead or the Product Manager, a change that starts with `waived` (a criterion no one can verify) by the
  Product Manager. A dropped criterion needs no test or task, and naming it there is no finding; a waived one
  needs no test.
- **Both:** the *Tier* line names exactly one tier, and the *Security areas touched* line says "none" or lists
  them (it decides the Security plan review); on tiers `standard` and `security` *Options considered* is filled
  (the alternatives, or `one option: <why>`); *Verified facts* has at least
  one row, and every row has evidence and a status (`confirmed`, `refuted`, or `unverified` with the measurement
  that settles it, starting with `to verify:`); no template placeholder is left; no Markdown file of the work
  folder contains a control character (`scope-check.py`'s check, here before any member reads the folder); a
  criterion that *Tests per criterion* and *Verification without tests* verify as different kinds is reported (a
  "none" cell on tier `trivial` yields to the line); in a plan with *Verification without tests*, which writes no
  test, a *Tests per criterion* cell that names no kind names only tests the test files already hold, so a pointer
  such as `check (below)` is reported instead of read as a test name. A
  plan longer than its tier's `PLAN_MAX_WORDS` (`squad_settings.py`, default `tests` and `trivial` 800,
  `standard` 2500, `security` 4000; HTML comments, lines kept verbatim from `specs/_template/plan.md` and the
  orchestrator-appended *Challenge notes* and *Security notes* sections do not count) is a finding: a long plan
  costs every
  member that reads it, so the Lead moves settled material (answered challenge entries, facts verified in earlier
  rounds) into the decision record, or into *Documentation updates* for the Dev. A plan whose scope really needs
  more says why in a `Plan size:` line under the header; then up to twice the limit is a warning that quotes the
  reason, and beyond that the scope is split. Once the plan is approved (`squad-next.py` says so when the log has a
  row of step 4 or later) its size is a warning only: an approved plan is not reopened for its size, also not when a
  merge of `main` brought a lower limit (`.squad/orchestration.md`, *Merge conflicts*).

After a run with a `plan.md` the trigger of *Plan challenge* (`.squad/routing.md`) is printed with its reason,
`Plan challenge: due — …` or `Plan challenge: not due — …`: due on tier `security`, and on tier `standard` when
the plan has production code (no *Verification without tests*) and *Options considered* names an alternative
(anything but `one option: …`) or *Decision records* names a record that BASE_REF (`squad_settings.py`) lacks — an
extended record alone does not count.

Ids may be written as ranges (`AC1–AC3`, `S2-S4`, `N1-N2`); backticks and emphasis around a cell are ignored. A criterion
without a test, in a plan whose *Verification without tests* section names it, counts as covered.

With `--matrix` the verification matrix of the work folder is printed, one row per criterion: its kind of
verification and its tests or check, from the plan's *Tests per criterion* and *Verification without tests* (a
*Test(s)* cell or a line that starts with `CI:`, `manual:`, `after merge:` or `check:` names that kind, any other
cell names tests, an empty or "none" cell on tier `trivial` leaves the criterion to the review round unless a
*Verification without tests* line names it), or `dropped` and `waived` from *Amendments*; and its result on the code
head (the last commit outside `specs/`). A unit criterion's tests are looked up in the test files
(`COVERAGE_TEST_PATHSPECS` of `squad_settings.py`, by default its `COVERAGE_EXCLUDES`, and the files the plan names):
a file that names a test the plan gives, or that carries the criterion id (`docs/UNIT_TESTS.md`, *Criterion ids*)
and is a file the criterion's row names or one the branch changes against its merge base with BASE_REF (uncommitted
and untracked files included), makes it `pass`, since the matrix runs on a head the gates of step 7 passed on; an id
in a test file the branch leaves alone is an earlier change's test and counts for nothing. No such file makes it
`MISSING` and the exit code 1. A *Verification without tests* line of another kind than the criterion's *Tests per
criterion* row is printed as a `CONFLICT` row below it, also exit code 1. Every other kind prints the result still
to fill in (`squad-issue`, step 10).

With `--spec` only `spec.md` is checked: after the Lead's mode `spec`, before the spec challenge. A spec longer
than `SPEC_BASE_WORDS` plus `SPEC_WORDS_PER_CRITERION` per acceptance criterion, at most `SPEC_MAX_WORDS`
(`squad_settings.py`, default 300, 200 and 2500; the template's guidance comments do not count) is reported as a
warning: every later launch reads it, so the Lead sizes it to the feature.

Usage, from the repository root:
    python3 .squad/tools/plan-check.py <work folder name, e.g. feature-12-export or issue-34> [--spec | --matrix]

Exit code 0 when the work folder passes, 1 otherwise.
"""
import itertools
import os
import re
import subprocess
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
import squad_settings as settings  # noqa: E402  (per-repository settings next to this script)
from analyzer_common import BASE_REF, control_characters  # noqa: E402  (shared helpers next to this script)

TIERS = ("tests", "trivial", "standard", "security")
PLAN_LIMITS = {"tests": 800, "trivial": 800, "standard": 2500, "security": 4000}
# Per tier; one number (an older settings file) applies to every tier.
PLAN_MAX_WORDS = getattr(settings, "PLAN_MAX_WORDS", PLAN_LIMITS)
NOTES_HEADING = re.compile(r"^(#{2,6})[ \t]+(?:Challenge|Security) notes\b", re.I)
PLAN_SIZE = re.compile(r"^Plan size:[ \t]*(\S.*)", re.M)
SPEC_MAX_WORDS = getattr(settings, "SPEC_MAX_WORDS", 2500)
SPEC_BASE_WORDS = getattr(settings, "SPEC_BASE_WORDS", 300)
SPEC_WORDS_PER_CRITERION = getattr(settings, "SPEC_WORDS_PER_CRITERION", 200)
SMALL_FEATURE_CRITERIA = getattr(settings, "SMALL_FEATURE_CRITERIA", 3)
CHALLENGED_TIERS = ("standard", "security")
ONE_OPTION = re.compile(r"^\W*one option\b", re.I)
RECORD = re.compile(r"docs/decisions/\d{4}-[\w.-]+\.md")
TIER_LINE = re.compile(r"^Tier:\s*`?(\w+)`?([^\n]*)", re.M)
STATUSES = ("confirmed", "refuted", "unverified")
CRITERIA_SECTION = "Acceptance criteria"
WITHOUT_TESTS = "Verification without tests"
SPEC_SECTIONS = ["Problem", "Scenarios", "Errors and edge cases", "Non-functional requirements",
                 CRITERIA_SECTION, "Out of scope", "Open questions", "Area documents"]
CRITERION = re.compile(r"^\s*- (?:\[[ xX]\] )?(AC\d+):", re.M)
CASE = re.compile(r"^\s*- (?:\*\*)?([SEN]\d+)(?:\*\*)?:", re.M)
# A range repeats the prefix (`AC1\u2013AC3`, `S2 .. S4`) or joins the numbers without spaces (`AC1-3`), so
# that "E1 \u2013 10 s at most" stays one id and a number.
RANGE = re.compile(r"\b(AC|S|E|N)(\d+)(?:\s*(?:[-\u2013\u2014]|\.\.)\s*\1(\d+)|[-\u2013](\d+))\b")
ID = re.compile(r"\b(AC|S|E|N)(\d+)\b")
ELLIPSIS = r"(?:\.\.\.|\u2026)"
PLACEHOLDERS = [re.compile(p) for p in (r"<title>", r"Issue #<number>", r"<one-sentence justification>",
                                         r"NNNN-title", r"one option: <why", rf"\bAC\d+: {ELLIPSIS}\s*$",
                                         rf"^\s*- [SEN]\d+: {ELLIPSIS}\s*$", rf"^\|.*\|\s*{ELLIPSIS}\s*\|")]
EMPTY_CELLS = {"", "-", "\u2013", "\u2014", "...", "\u2026", "none", "n/a", "tbd", "todo"}
COMMENT = re.compile(r"(```.*?```|`[^`\n]*`)|<!--.*?-->", re.S)  # a code span or fenced block is kept whole
EMPHASIS = "`*_"
SPEC = "spec.md"
PLAN = "plan.md"
SPECS_PREFIX = "specs/"
TEMPLATE_PLAN = os.path.join("specs", "_template", PLAN)
LEAD_FILES = (SPECS_PREFIX, "docs/decisions/")
NONE = re.compile(r"^\s*(?:[-*]\s+)?none\.?\s*$", re.I)
SKELETON_ROW = re.compile(r"^skeleton\b", re.I)
# The group of the tasks steps 4–6 skip: the Product Manager's and the Lead's (squad-next.py runs no member for it).
NO_CODE_GROUP = "PM"
NO_CODE_OWNERS = {"product manager", "lead"}
NO_NEW_API = re.compile(r"\bno new API\b", re.I)
# The words that may stand between "no" and "API" in a *Signatures* section that states the change adds no API.
API_MODIFIERS = {"new", "changed", "public", "internal", "exported", "or", "and"}
NO_API_WORDS = {"api", "apis", "skeleton"}
SPAN = re.compile(r"`([^`\n]+)`")
# The receiver of a Go method (`func (o *Options) Kernel() string`): its parentheses are no call.
GO_RECEIVER = re.compile(r"^\s*func\s*\([^)]*\)\s*")
# A line of *Signatures* that states its member is deleted ("- Deleted: `f()`", "`f()` is removed"): no declaration
# the skeleton adds. A description that only uses the word ("removes the expired rows") states no deletion.
REMOVED_LABEL = re.compile(r"^\s*(?:[-*]\s+)?(?:deleted|removed|dropped)\b", re.I)
REMOVED_STATEMENT = re.compile(r"\b(?:is|are|gets?|was|were|be)\s+(?:deleted|removed|dropped)\b", re.I)
DECLARATION_KEYWORDS = {"func", "def", "fn", "public", "private", "protected", "internal", "static", "const",
                        "readonly", "final", "async", "export", "function", "type", "class", "struct", "record",
                        "interface", "enum", "var", "let", "val", "abstract", "override"}
TOP_ITEM = re.compile(r"^(?:[-*]|\d+[.)])\s+")
AMENDMENT_COLUMNS = ("criterion", "change", "reason", "step", "decided by")
PRODUCT_MANAGER = "product manager"
DECIDERS = ("lead", PRODUCT_MANAGER)
DROPPED = "dropped"
WAIVED = "waived"
AFTER_MERGE = "after merge"
KINDS = {"ci": "CI", "manual": "manual", AFTER_MERGE: AFTER_MERGE, "check": "check"}
KIND = re.compile(r"^(CI|manual|after merge|check)\s*:\s*", re.I)
# The ids a line starts with; the digits in the separators keep a range's end (`AC1-3`).
LEADING_IDS = re.compile(r"^[\s*-]*(?:\**AC\d+\**[\s,.\u2013\u2014\d-]*)+:?\s*")
QUOTE_PATHS = "core.quotePath=false"
RESULTS = {"CI": "pending: the pull request's CI (step 11)", "manual": "to record: the manual test's result",
           "check": "to record: the check's result", AFTER_MERGE: f"{AFTER_MERGE}: the linked verification issue",
           "review": "pass: the clean review round", DROPPED: "dropped (*Amendments*)",
           WAIVED: "waived by the Product Manager (*Amendments*)"}


def strip_comments(text):
    """The text without its HTML comments; a comment opener inside a code span or fenced block is no comment."""
    return COMMENT.sub(lambda match: match.group(1) or "", text)


def read(path, errors):
    if not os.path.isfile(path):
        return None
    try:
        with open(path, encoding="utf-8-sig") as handle:
            return handle.read()
    except UnicodeDecodeError:
        errors.append(f"{os.path.basename(path)}: not UTF-8")
        return ""


def resolve_folder(root, folder):
    """The work folder under `specs/`, found in the directory listing so the argument never becomes a path."""
    wanted = folder.strip().replace("\\", "/").rstrip("/")
    wanted = wanted.removeprefix(SPECS_PREFIX)
    specs = os.path.join(root, "specs")
    if not os.path.isdir(specs):
        return None
    for name in sorted(os.listdir(specs)):
        if name == wanted and name not in ("_template", ".", "..") and os.path.isdir(os.path.join(specs, name)):
            return os.path.join(specs, name)
    return None


def ids(text, kind=None):
    """Every id the text names (`AC1`, `S2`, `E3`, `N4`), ranges expanded; only those of `kind` when given."""
    found = set()
    for prefix, low, spaced, joined in RANGE.findall(text):
        high = spaced or joined
        found.update(f"{prefix}{n}" for n in range(int(low), int(high) + 1) if int(high) - int(low) < 100)
    found.update(f"{prefix}{number}" for prefix, number in ID.findall(text))
    return {i for i in found if kind is None or re.fullmatch(rf"{kind}\d+", i)}


def sections(text):
    """{heading: body} for every `## ` section, without the template's guidance comments."""
    result, current = {}, None
    for line in strip_comments(text).splitlines():
        if line.startswith("## "):
            current = line[3:].strip()
            result[current] = []
        elif current is not None:
            result[current].append(line)
    return {name: "\n".join(lines).strip() for name, lines in result.items()}


def cell(value):
    return value.strip().strip(EMPHASIS).strip()


def split_row(line, keep=False):
    """The cells of a table row, without their emphasis; with `keep` only trimmed, so a cell of code spans (`` `A`,
    `B` ``) keeps its backticks (Censio#137)."""
    return [c.strip() if keep else cell(c) for c in re.split(r"(?<!\\)\|", line.strip().strip("|"))]


def tables(body, keep=False):
    """Every Markdown table in the text, as (header cells, data rows); `keep` as for `split_row`."""
    found, header, rows = [], None, []
    for line in (body or "").splitlines():
        if not line.strip().startswith("|"):
            if header is not None:
                found.append((header, rows))
            header, rows = None, []
            continue
        cells = split_row(line, keep)
        if all(set(c) <= set("-: ") for c in cells):
            continue
        if header is None:
            header = cells
        else:
            rows.append(cells)
    if header is not None:
        found.append((header, rows))
    return found


def column(header, *names):
    """The index of the first header cell that is one of the names (case-insensitive), or None."""
    for index, name in enumerate(header):
        if name.lower() in names:
            return index
    return None


def empty(value):
    return cell(value).lower() in EMPTY_CELLS


def check_placeholders(name, text, errors):
    for line in strip_comments(text).splitlines():
        if any(pattern.search(line) for pattern in PLACEHOLDERS):
            errors.append(f"{name}: template placeholder left: {line.strip()}")


def check_open_questions(questions, errors):
    """Every top-level question is answered somewhere in its item (continuation lines and sub-items included)."""
    if NONE.match(questions or ""):
        return
    if questions and not any(TOP_ITEM.match(line) for line in questions.splitlines()):
        if "Answered:" not in questions:
            errors.append("spec.md: *Open questions* holds a question without `Answered:` (or write \"none\")")
        return
    items, current = [], None
    for line in questions.splitlines():
        if TOP_ITEM.match(line):
            current = [line]
            items.append(current)
        elif current is not None and line.strip():
            current.append(line)
    for item in items:
        if not NONE.match(item[0]) and "Answered:" not in "\n".join(item):
            errors.append(f"spec.md: open question without `Answered:`: {item[0].strip()}")


def check_requirements(requirements, errors):
    """*Non-functional requirements* says "none" or numbers each requirement `- N<n>: …`, so that a limit, a timeout,
    a compatibility promise or a new setting is traced like a scenario."""
    if not requirements or NONE.match(requirements):
        return
    lines = [line for line in requirements.splitlines() if TOP_ITEM.match(line) and not CASE.match(line)]
    for line in lines:
        errors.append(f"spec.md: non-functional requirement not written `- N<n>: …`: {line.strip()}")
    if not lines and not CASE.search(requirements):
        errors.append("spec.md: *Non-functional requirements* names no `- N1: …` (or write \"none\")")


def check_spec(spec, errors):
    """The spec's sections; returns its criterion ids and the criteria its *Amendments* drop or waive."""
    parts = sections(spec)
    for section in SPEC_SECTIONS:
        if not parts.get(section):
            errors.append(f"spec.md: section *{section}* is missing or empty (write \"none\" when nothing applies)")
    block = parts.get(CRITERIA_SECTION, "")
    criteria = CRITERION.findall(block)
    if not criteria:
        errors.append("spec.md: no acceptance criterion written `- AC1: …` under *Acceptance criteria*")
    if len(criteria) != len(set(criteria)):
        errors.append("spec.md: an acceptance criterion id is used twice")
    for line in block.splitlines():
        if re.match(r"^[-*\d]", line) and ids(line, "AC") and not CRITERION.match(line):
            errors.append(f"spec.md: criterion not written `- AC<n>: …`: {line.strip()}")
    requirements = parts.get("Non-functional requirements", "")
    check_requirements(requirements, errors)
    cases = [case for name in ("Scenarios", "Errors and edge cases") for case in CASE.findall(parts.get(name, ""))]
    cases += CASE.findall(requirements)
    if not any(case.startswith("S") for case in cases):
        check_small_spec(parts.get("Scenarios", ""), len(criteria), errors)
    covered = ids(block) | ids(parts.get("Out of scope", ""))
    for case in cases:
        if case not in covered:
            errors.append(f"spec.md: {case} is named by no acceptance criterion and not listed under *Out of scope*")
    check_open_questions(parts.get("Open questions", ""), errors)
    return set(criteria), check_amendments(SPEC, parts, set(criteria), errors)


def check_small_spec(scenarios, count, errors):
    """A spec without scenarios: only a small feature's, which says "none" and lets its criteria be its scenarios."""
    if not NONE.match(scenarios):
        errors.append("spec.md: no scenario written `- S1: …` under *Scenarios*")
    elif count > SMALL_FEATURE_CRITERIA:
        errors.append(f"spec.md: *Scenarios* says \"none\", which only a small feature may (at most "
                      f"{SMALL_FEATURE_CRITERIA} criteria, SMALL_FEATURE_CRITERIA): write the scenarios")


def small_spec(spec):
    return bool(NONE.match(sections(spec).get("Scenarios", "")))


def check_amendment_row(name, cells, criteria, settled, errors):
    """One row of *Amendments*: (criterion, change, reason, step, decided by)."""
    shown = f"| {' | '.join(cells)} |"
    if any(empty(value) for value in cells):
        errors.append(f"{name}: *Amendments* row with an empty cell: {shown}")
        return
    named, change = ids(cells[0], "AC"), cells[1].lower()
    if not named:
        errors.append(f"{name}: *Amendments* row names no criterion: {shown}")
    if not cells[4].lower().startswith(DECIDERS):
        errors.append(f"{name}: *Amendments* row not decided by the Lead or the Product Manager: {shown}")
    kind = next((k for k in (DROPPED, WAIVED) if change.startswith(k)), None)
    if kind == WAIVED and not cells[4].lower().startswith(PRODUCT_MANAGER):
        errors.append(f"{name}: *Amendments* waives a criterion the Product Manager did not waive: {shown}")
    if kind:
        settled.update(dict.fromkeys(named, kind))
    for criterion in sorted(named):
        errors.extend(amended_criterion_errors(name, criterion, kind, criteria))


def amended_criterion_errors(name, criterion, kind, criteria):
    """A dropped criterion is no longer defined; any other amended one still is."""
    if kind == DROPPED and criterion in criteria:
        return [f"{name}: *Amendments* drops {criterion}, which is still a criterion"]
    if kind != DROPPED and criterion not in criteria:
        return [f"{name}: *Amendments* changes {criterion}, which no criterion defines (a dropped one says "
                "`dropped`)"]
    return []


def check_amendments(name, parts, criteria, errors):
    """The *Amendments* table of the file that defines the criteria (a criterion changed after approval); returns
    {criterion: `dropped` or `waived`} for the criteria it drops or waives."""
    body = parts.get("Amendments")
    settled = {}
    if body is None:
        return settled
    found = tables(body)
    if not found:
        errors.append(f"{name}: *Amendments* holds no table (Criterion, Change, Reason, Step, Decided by)")
    for header, rows in found:
        index = [column(header, wanted) for wanted in AMENDMENT_COLUMNS]
        if None in index:
            errors.append(f"{name}: the *Amendments* table needs the columns Criterion, Change, Reason, Step and "
                          "Decided by")
            continue
        for cells in amendment_rows(rows, index):
            check_amendment_row(name, cells, criteria, settled, errors)
    return settled


def amendment_rows(rows, index):
    """The cells of each *Amendments* row in the order of AMENDMENT_COLUMNS, a missing cell empty."""
    return [[row[i] if i < len(row) else "" for i in index] for row in rows]


def check_tests(plan_parts, criteria, tier, errors, settled=None):
    named = set()
    for header, rows in tables(plan_parts.get("Tests per criterion")):
        test = column(header, "test(s)", "tests", "test")
        test = 1 if test is None else test
        for row in rows:
            named |= ids(row[0], "AC")
            if len(row) <= test or (empty(row[test]) and tier != "trivial"):
                errors.append(f"plan.md: *Tests per criterion* row without a test name: | {' | '.join(row)} |")
    if tier == "trivial":
        return
    settled = settled or {}
    without_tests = ids(plan_parts.get(WITHOUT_TESTS, ""), "AC")
    for criterion in sorted(criteria - named - without_tests - set(settled)):
        errors.append(f"plan.md: {criterion} has no row in *Tests per criterion* (nor in *Verification without tests*)")
    for criterion in sorted(named - criteria - set(settled)):
        errors.append(f"plan.md: *Tests per criterion* names {criterion}, which no acceptance criterion defines")


def check_facts(plan_parts, errors):
    rows = [row for _, table_rows in tables(plan_parts.get("Verified facts")) for row in table_rows]
    if not rows:
        errors.append("plan.md: *Verified facts* is missing or has no row (claim, evidence, status)")
    for row in rows:
        if len(row) < 3:
            errors.append(f"plan.md: *Verified facts* row needs claim, evidence and status: | {' | '.join(row)} |")
            continue
        claim, evidence, status = row[-3], row[-2], row[-1].lower()
        if empty(evidence):
            errors.append(f"plan.md: claim without evidence in *Verified facts*: {claim}")
        if status not in STATUSES:
            errors.append(f"plan.md: *Verified facts* status must be one of {', '.join(STATUSES)}: {claim}")
        elif status == "unverified" and not evidence.lower().startswith("to verify:"):
            errors.append(f"plan.md: unverified claim without `to verify:` and its measurement: {claim}")


def check_groups(header, rows, errors):
    """Every task row names its group, and a group's rows are consecutive: steps 4–6 run one group at a time, as
    one launch per member, in table order."""
    index = column(header, "group")
    if index is None:
        errors.append("tasks.md: a task table without a *Group* column (steps 4–6 run per task group)")
        return
    owner = column(header, "owner")
    seen, previous = set(), None
    for row in rows:
        group = row[index] if len(row) > index else ""
        if empty(group):
            errors.append(f"tasks.md: task without a group: | {' | '.join(row)} | (a task of the Product Manager or "
                          f"the Lead alone goes to group {NO_CODE_GROUP})")
            continue
        if group == NO_CODE_GROUP:
            check_no_code_row(row, owner, errors)
            continue
        if group != previous and group in seen:
            errors.append(f"tasks.md: the rows of group {group} are not consecutive")
        seen.add(group)
        previous = group


def owners(text):
    """The owners an *Owner* cell names (`Product Manager, Lead` gives both), lower-case."""
    return {name.strip().lower() for name in re.split(r"[,/&]|\band\b", cell(text)) if name.strip()}


def check_no_code_row(row, owner, errors):
    """A row of group `PM` is owned by the Product Manager or the Lead alone: steps 4–6 launch no member for it."""
    names = owners(cell_at(row, owner))
    if not names or names - NO_CODE_OWNERS:
        errors.append(f"tasks.md: row {row[0]} of group {NO_CODE_GROUP} is owned by {cell_at(row, owner) or 'no one'}: "
                      "that group holds only tasks of the Product Manager and the Lead, which steps 4–6 skip")


def signatures_section(plan):
    parts = sections(plan)
    return next((body for name, body in parts.items() if name.startswith("Signatures")), "")


def states_no_api(text):
    """True when the prose of a *Signatures* section (its code spans left out) starts with "none" or says "no API",
    "no new API", "no public API changes", "no skeleton": only API_MODIFIERS may stand between "no" and "API", so "no
    other API changes" beside a listed member is no such statement."""
    words = re.findall(r"[a-z]+", SPAN.sub(" ", text).lower())
    if words[:1] == ["none"] and words[1:2] != ["of"]:
        return True
    return any(word == "no" and next(itertools.dropwhile(API_MODIFIERS.__contains__, words[index + 1:]), "")
               in NO_API_WORDS for index, word in enumerate(words))


def removed_member(prose):
    """True when the prose of a *Signatures* line states that its member is deleted or removed."""
    return bool(REMOVED_LABEL.search(prose) or REMOVED_STATEMENT.search(prose))


def signature_names(body):
    """The identifier of every backticked declaration in the plan's *Signatures* section (`Options.Kernel` and the
    Go method `func (o *Options) Kernel() string` give `Kernel`); a section that says "none" or states that the change
    adds no API gives none, also when it names private members in backticks; a line that states its member is deleted
    or removed is left out."""
    names, text = [], strip_comments(body or "")
    if states_no_api(text):
        return names
    kept = [line for line in text.splitlines() if not removed_member(SPAN.sub(" ", line))]
    for span in SPAN.findall("\n".join(kept)):
        span = GO_RECEIVER.sub("", span)
        call = re.search(r"([A-Za-z_]\w*)\s*\(", span)
        words = [w for w in re.findall(r"[A-Za-z_][\w.]*", span) if w not in DECLARATION_KEYWORDS]
        name = words[0].split(".")[-1] if words else ""
        if call:
            name = call.group(1)
        if name and name not in names:
            names.append(name)
    return names


def file_list(cell_text):
    """The paths a *Files* cell names; a note in parentheses and words without a `/` or `.` are left out."""
    text = re.sub(r"\([^)]*\)", " ", cell_text.replace("`", ""))
    return [f for f in re.split(r"[,\s]+", text) if "/" in f or "." in f.strip(".")]


def documentation_only(cell_text):
    """True when every file a *Files* cell names is documentation (Markdown or a README)."""
    files = file_list(cell_text)
    return bool(files) and all(f.lower().endswith(".md") or f.rsplit("/", 1)[-1].upper().startswith("README")
                               for f in files)


def check_lead_row(row, owner, paths, errors):
    """A task row of the Lead names only its own files (`specs/`, `docs/decisions/`)."""
    if owner is None or len(row) <= owner or row[owner].lower() != "lead":
        return
    if not all(path.startswith(LEAD_FILES) for path in file_list(paths)):
        errors.append(f"tasks.md: row {row[0]} is owned by the Lead, which writes only plans and decision records: "
                      "documentation rows are the Dev's (test documentation: the Tester's)")


def group_states(header, rows, errors):
    """{group: (skeleton or "no new API" stated, documentation rows alone)} and the text of the skeleton rows;
    a row the Lead owns outside its own files (`specs/`, `docs/decisions/`) is reported."""
    group, task = column(header, "group"), column(header, "task")
    owner, files = column(header, "owner"), column(header, "files")
    states, skeleton = {}, []
    for row in rows:
        if group is None or task is None or len(row) <= max(group, task):
            continue
        text = " ".join(row)
        is_skeleton = bool(SKELETON_ROW.match(row[task]))
        paths = row[files] if files is not None and len(row) > files else ""
        check_lead_row(row, owner, paths, errors)
        stated, docs_only = states.get(row[group], (False, True))
        states[row[group]] = (stated or is_skeleton or bool(NO_NEW_API.search(text)),
                              docs_only and documentation_only(paths))
        if is_skeleton:
            skeleton.append(text)
    return states, "\n".join(skeleton)


def check_skeleton(header, rows, signatures, errors):
    """With API in the plan's *Signatures*, every group has a skeleton row or says "no new API" (a group of
    documentation rows alone needs neither), and every signature name appears in a skeleton row. Returns the text
    of the skeleton rows."""
    states, skeleton = group_states(header, rows, errors)
    for key, (stated, docs_only) in states.items():
        if signatures and not (stated or docs_only or empty(key) or key == NO_CODE_GROUP):
            errors.append(f"tasks.md: group {key} has no *Skeleton* row and does not say \"no new API\" "
                          "(the plan's *Signatures* lists API)")
    return skeleton


def check_tasks(tasks, criteria, errors, signatures=(), dropped=frozenset()):
    named, found_column, skeleton = set(), False, ""
    for header, rows in tables(tasks):
        index = column(header, "criteria", "criterion", "tests (ac)", "ac")
        if index is None:
            continue
        found_column = True
        named |= {i for row in rows if len(row) > index for i in ids(row[index], "AC")}
        check_groups(header, rows, errors)
        skeleton += check_skeleton(header, rows, signatures, errors)
    for name in signatures:
        if not re.search(rf"(?<!\w){re.escape(name)}(?!\w)", skeleton):
            errors.append(f"tasks.md: the plan's signature `{name}` is named by no skeleton row of any group")
    if not found_column:
        errors.append("tasks.md: no table with a *Criteria* column")
    for criterion in sorted(criteria - named):
        errors.append(f"tasks.md: {criterion} is covered by no task")
    for criterion in sorted(named - criteria - dropped):
        errors.append(f"tasks.md: names {criterion}, which spec.md does not define")


def plan_tier(plan):
    """The tier the *Tier* line names, or None."""
    tier = TIER_LINE.search(plan)
    return tier.group(1) if tier else None


def areas_value(plan):
    areas = re.search(r"^Security areas touched:(.*)", plan, re.M)
    return cell(areas.group(1)) if areas else None


def check_security_areas(plan, errors):
    """The line that decides the Security plan review: "none", or the areas — never empty or left as the template."""
    value = areas_value(plan) or ""
    if NONE.match(value):
        if plan_tier(plan) == "security":
            errors.append("plan.md: tier `security` changes a security control, so *Security areas touched* "
                          "cannot be \"none\": name the area (add it to .squad/project.md if the list lacks it)")
        return
    if (empty(value) or re.match(r"(?:none|no|nothing)\b", value, re.I) or "<areas>" in value):
        errors.append("plan.md: write the line `Security areas touched: none` (exactly) or `Security areas "
                      "touched: <the areas>` (it decides the Security plan review)")


def check_plan(plan, spec, errors):
    """plan.md against the tier, the facts and the criteria; returns the criteria of the work folder and the ones
    its *Amendments* drop or waive."""
    parts = sections(plan)
    tier = TIER_LINE.search(plan)
    name = tier.group(1) if tier else None
    if name not in TIERS or tier.group(2).lstrip().startswith("|"):
        errors.append(f"plan.md: the *Tier* line must name exactly one of {', '.join(TIERS)}")
    check_security_areas(plan, errors)
    if name in CHALLENGED_TIERS and not parts.get("Options considered"):
        errors.append("plan.md: *Options considered* is missing or empty: each alternative weighed and why it lost, "
                      "or `one option: <why>` (it decides the plan challenge)")
    own = set(CRITERION.findall(parts.get(CRITERIA_SECTION, "")))
    if spec is None:
        criteria = own
        if not criteria:
            errors.append("plan.md: no acceptance criterion (`- AC1: …`) under *Acceptance criteria*")
        settled = check_amendments(PLAN, parts, criteria, errors)
    else:
        criteria, settled = check_spec(spec, errors)
        for criterion in sorted(own):
            errors.append(f"plan.md: defines {criterion}; a feature's criteria live in spec.md only")
        if small_spec(spec) and not NONE.match(areas_value(plan) or "none"):
            errors.append("spec.md: *Scenarios* says \"none\", which a feature that touches a security area may "
                          "not: write the scenarios")
    check_tests(parts, criteria, name, errors, settled)
    check_verification_kinds(parts, errors)
    check_facts(parts, errors)
    return criteria, settled


def check_verification_kinds(parts, errors):
    """A criterion verified two ways is reported, since the matrix keeps only the *Tests per criterion* row; in a plan
    without production or test code, which writes no test, a *Tests per criterion* cell that names no kind names
    tests the test files already hold — `check (below)` would read as a test name."""
    conflicts = verification_conflicts(parts)
    for criterion, row, line in conflicts:
        errors.append(f"plan.md: {criterion} is verified as {row[0]} under *Tests per criterion* ({row[1]}) and as "
                      f"{line[0]} under *Verification without tests* ({line[1]}): keep one")
    if parts.get(WITHOUT_TESTS):
        check_existing_tests(parts, {conflict[0] for conflict in conflicts}, errors)


def check_existing_tests(parts, reported, errors):
    """In a plan without production or test code, every test a *Tests per criterion* cell names exists already
    (criteria `reported` as verified two ways left out)."""
    units = {criterion: verification for criterion, verification in test_rows(parts).items()
             if verification[0] == "unit" and criterion not in reported}
    sources = test_sources({path for _, _, _, paths in units.values() for path in paths}) if units else {}
    for criterion in sorted(units, key=criterion_number):
        _, text, names, _ = units[criterion]
        absent = [name for name in names if not any(name in source for source in sources.values())]
        if absent:
            errors.append(f"plan.md: {criterion}: the *Tests per criterion* cell `{text}` names no kind (`CI:`, "
                          f"`manual:`, `after merge:`, `check:`), and no test file holds {', '.join(absent)}, while the "
                          "plan writes no test (*Verification without tests*): write its kind, or verify it under "
                          "*Verification without tests* alone")


def criterion_number(criterion):
    return int(criterion[2:])


def check_spec_only(folder):
    """Findings for `spec.md` alone (step 2, before the plan is written)."""
    errors = []
    spec = read(os.path.join(folder, SPEC), errors)
    if spec is None:
        return ["spec.md is missing"], []
    criteria, _ = check_spec(spec, errors)
    check_placeholders(SPEC, spec, errors)
    check_control_characters(folder, errors)
    words = len(strip_comments(spec).split())
    limit = min(SPEC_MAX_WORDS, SPEC_BASE_WORDS + SPEC_WORDS_PER_CRITERION * len(criteria))
    if words > limit:
        return errors, [f"spec.md has {words} words, over {limit} for {len(criteria)} criteria (SPEC_BASE_WORDS "
                        f"{SPEC_BASE_WORDS} + SPEC_WORDS_PER_CRITERION {SPEC_WORDS_PER_CRITERION} per criterion, at "
                        f"most SPEC_MAX_WORDS {SPEC_MAX_WORDS}), and every later launch reads it: "
                        "keep one scenario per behavior a user can tell apart and only the edge cases that change "
                        "what the user sees, and link the issue, an area document or a record instead of restating "
                        "it"]
    return errors, []


def check_control_characters(folder, errors):
    """No Markdown file of the work folder carries a control character: `scope-check.py` would reject it only in
    the docs step, after every member has read the plan."""
    for name in sorted(os.listdir(folder)):
        path = os.path.join(folder, name)
        if name.endswith(".md") and os.path.isfile(path):
            found = control_characters(read(path, []) or "")
            if found:
                errors.append(f"{name}: control characters {', '.join(found)} (write them as text escapes)")


def check(folder, approved=False):
    """All findings for the work folder, and the warnings; `approved`: the run is past the plan (step 4 or later), so
    the plan's size is a warning only."""
    errors, warnings = [], []
    check_control_characters(folder, errors)
    plan = read(os.path.join(folder, PLAN), errors)
    spec = read(os.path.join(folder, SPEC), errors)
    if plan is None:
        return ["plan.md is missing (tiers docs and patch write none: there is nothing to check)"], warnings
    criteria, settled = check_plan(plan, spec, errors)
    dropped = {criterion for criterion, kind in settled.items() if kind == DROPPED}
    for name, text in ((PLAN, plan), (SPEC, spec)):
        if text is not None:
            check_placeholders(name, text, errors)
    if spec is not None:
        tasks = read(os.path.join(folder, "tasks.md"), errors)
        if tasks is None:
            errors.append("tasks.md is missing")
        else:
            check_tasks(tasks, criteria, errors, signature_names(signatures_section(plan)), dropped)
            check_placeholders("tasks.md", tasks, errors)
    check_plan_size(plan, errors, warnings, approved)
    return errors, warnings


def template_lines():
    """The non-empty lines of the repository's plan template, stripped."""
    text = read(TEMPLATE_PLAN, []) if os.path.isfile(TEMPLATE_PLAN) else None
    return {line.strip() for line in (text or "").splitlines() if line.strip()}


def counted_words(plan):
    """The Lead's own words in the plan: not its HTML comments, not a line kept verbatim from the template (a
    heading, a table header, guidance), and not the *Challenge notes* and *Security notes* sections the orchestrator
    appends verbatim after the Lead has sized the plan: the Lead cannot shorten them."""
    words, skip_level, template = 0, None, template_lines()
    for line in strip_comments(plan).splitlines():
        heading = re.match(r"^(#{1,6})[ \t]", line)
        if heading and skip_level is not None and len(heading.group(1)) <= skip_level:
            skip_level = None
        notes = NOTES_HEADING.match(line)
        if notes and skip_level is None:
            skip_level = len(notes.group(1))
        if skip_level is None and line.strip() not in template:
            words += len(line.split())
    return words


def plan_limit(tier):
    """The word limit of a plan of the tier: PLAN_MAX_WORDS per tier (a tier it lacks takes the default), or one
    number for every tier; the highest when the *Tier* line names no tier (its own finding)."""
    limits = dict(PLAN_LIMITS)
    limits.update(PLAN_MAX_WORDS if isinstance(PLAN_MAX_WORDS, dict) else dict.fromkeys(TIERS, PLAN_MAX_WORDS))
    return limits.get(tier, max(limits.values()))


def check_plan_size(plan, errors, warnings, approved=False):
    """A plan within its tier's PLAN_MAX_WORDS, or up to twice that with a `Plan size:` line that says why; an approved
    plan's size is a warning, since a limit a later merge brought cannot reopen it."""
    words, tier = counted_words(plan), plan_tier(plan)
    limit = plan_limit(tier)
    if words <= limit:
        return
    match = PLAN_SIZE.search(plan)
    reason = match.group(1).strip() if match and not empty(cell(match.group(1))) else None
    shown = f"PLAN_MAX_WORDS {limit} for tier {tier}"
    if approved:
        warnings.append(f"plan.md has {words} words ({shown}), approved: the limit applies while the plan is written, "
                        "not to the approved plan")
    elif words > 2 * limit:
        errors.append(f"plan.md has {words} words, over twice {shown}: split the scope into separate issues the plan "
                      "can describe within the limit")
    elif reason and "<" not in reason:
        warnings.append(f"plan.md has {words} words ({shown}), justified: {reason}")
    else:
        errors.append(f"plan.md has {words} words ({shown}), and every member reads it: size it to the change, "
                      "move settled material (answered challenge entries, facts verified in earlier rounds) into the "
                      "decision record or into Documentation updates for the Dev and cut what the code, the spec or a "
                      "record already says; "
                      "a scope that really needs more says why in a `Plan size:` line under the header")


def git_output(*args):
    """The output of a read-only git command, or "" when git fails or is missing."""
    try:
        result = subprocess.run(["git", *args], capture_output=True, text=True, check=False)
    except OSError:
        return ""
    return result.stdout if result.returncode == 0 else ""


def test_names(text):
    """The test names of a *Test(s)* cell: its code spans when it has any (prose around them is no name), else its
    comma-separated parts; a qualified name (`ExportTests.Writes`, a Go subtest `TestParse/empty`) by the name a test
    file declares, and a shortened one (`…ArchiveUsed`, `ReaderLoad...`) by the part that is written out."""
    spans = re.findall(r"`([^`\n]+)`", text)
    parts = spans if spans else re.split(r"[,;]", text)
    names = []
    for part in parts:
        name = re.sub(r"\(\)$", "", part.strip().strip("`'\"").strip())
        name = name.replace("\u2026", "").replace("...", "").strip()
        if name and " " not in name:
            name = name.split("/")[0].rsplit(".", 1)[-1]
        if name and not (spans and " " in name):
            names.append(name)
    return names


def row_verification(text, paths):
    """(kind, tests or check, test names, test files) of one *Tests per criterion* cell, its code spans kept."""
    plain = cell(text)
    kind = KIND.match(plain)
    if kind:
        return KINDS[kind.group(1).lower()], plain[kind.end():].strip(), [], paths
    if empty(plain) or plain.lower().startswith("none"):
        return "review", "none (no tests first)", [], paths
    return "unit", text, test_names(text), paths


def cell_at(row, index):
    return row[index] if index is not None and len(row) > index else ""


def test_rows(parts):
    """{criterion: (kind, tests or check, test names, test files)} of the plan's *Tests per criterion*."""
    planned = {}
    for header, rows in tables(parts.get("Tests per criterion"), keep=True):
        header = [cell(name) for name in header]
        test = column(header, "test(s)", "tests", "test")
        test = 1 if test is None else test
        files = column(header, "test file", "test files", "file", "files")
        for row in rows:
            verification = row_verification(cell_at(row, test), file_list(cell_at(row, files)))
            planned.update(dict.fromkeys(ids(row[0], "AC"), verification))
    return planned


def without_tests_rows(parts):
    """[(criterion, (kind, check, no test names, no test files))] of the plan's *Verification without tests*: the
    criteria a line starts with (`- AC1, AC2: …`), or every one it names when it starts with none."""
    found = []
    for line in parts.get(WITHOUT_TESTS, "").splitlines():
        lead = LEADING_IDS.match(line)
        named = ids(lead.group(0), "AC") if lead else set()
        verification = without_tests_verification(LEADING_IDS.sub("", line).strip())
        found += [(criterion, verification) for criterion in sorted(named or ids(line, "AC"), key=criterion_number)]
    return found


def planned_verifications(parts):
    """{criterion: (kind, tests or check, test names, test files)}: its *Tests per criterion* row, else its first
    *Verification without tests* line; a row that names no test ("none" on tier `trivial`) yields to such a line."""
    rows = test_rows(parts)
    planned = {criterion: row for criterion, row in rows.items() if row[0] != "review"}
    for criterion, verification in without_tests_rows(parts):
        planned.setdefault(criterion, verification)
    for criterion, row in rows.items():
        planned.setdefault(criterion, row)
    return planned


def verification_conflicts(parts):
    """[(criterion, its *Tests per criterion* verification, a *Verification without tests* one of another kind)]."""
    rows = test_rows(parts)
    return [(criterion, rows[criterion], line) for criterion, line in without_tests_rows(parts)
            if criterion in rows and rows[criterion][0] not in ("review", line[0])]


def without_tests_verification(text):
    """(kind, check, no test names, no test files) of a *Verification without tests* line; `check` by default."""
    kind = KIND.match(text)
    if kind:
        return KINDS[kind.group(1).lower()], text[kind.end():], [], []
    return "check", text, [], []


def test_sources(paths):
    """{path: text} of the test files: the test paths of squad_settings.py and the files the plan names."""
    pathspecs = getattr(settings, "COVERAGE_TEST_PATHSPECS", getattr(settings, "COVERAGE_EXCLUDES", []))
    listed = git_output("ls-files", "--cached", "--others", "--exclude-standard", "--", *pathspecs).splitlines() \
        if pathspecs else []
    sources = {}
    for path in sorted({p for p in listed if p.strip()} | set(paths)):
        text = read(path, []) if os.path.isfile(path) else None
        if text:
            sources[path] = text
    return sources


def changed_files():
    """The files the branch changes against its merge base with BASE_REF, uncommitted and untracked ones included,
    and whether that merge base was found (without it: the uncommitted and untracked files only)."""
    base = git_output("merge-base", BASE_REF, "HEAD").strip()
    changed = git_output("-c", QUOTE_PATHS, "diff", "--name-only", "--no-renames", base or "HEAD", "--").splitlines()
    untracked = git_output("-c", QUOTE_PATHS, "ls-files", "--others", "--exclude-standard").splitlines()
    return {path for path in changed + untracked if path.strip()}, bool(base)


def carriers(criterion, verification, sources, changed):
    """The test files that carry the criterion: the ones that name one of its tests; when none does, the files its row
    names that carry its id (not as part of a longer word or number); when none of those does either, the files the
    branch changes that carry it. The id alone is the last resort: an earlier feature's tests in a file the branch
    touches carry the same `AC4` (Censio#137, Censio#140), and the id in a file the branch leaves alone belongs to the
    test of an earlier change."""
    _, _, names, files = verification
    named = [path for path, text in sources.items() if any(name in text for name in names)]
    if named:
        return named
    pattern = re.compile(rf"(?<![A-Za-z0-9]){criterion}(?![0-9])")
    with_id = [path for path, text in sources.items() if pattern.search(text)]
    return [path for path in with_id if path in files] or [path for path in with_id if path in changed]


def matrix_row(criterion, planned, settled, sources, head, changed=frozenset()):
    """(the row's cells, whether it is MISSING)."""
    if criterion in settled:
        return [criterion, settled[criterion], "\u2014", RESULTS[settled[criterion]]], False
    if criterion not in planned:
        return [criterion, "\u2014", "\u2014", "MISSING: no test or verification in plan.md"], True
    kind, check, names, _ = planned[criterion]
    if kind != "unit":
        return [criterion, kind, check, RESULTS[kind]], False
    found = carriers(criterion, planned[criterion], sources, changed)
    result = f"pass on `{head}`: {', '.join(found)}" if found else \
        f"MISSING: no test file names {', '.join(names)}, and none its row names or the branch changes carries {criterion}"
    return [criterion, kind, check, result], not found


def conflict_row(criterion, row, line):
    """The matrix row of a *Verification without tests* line of another kind than the criterion's test row."""
    return [criterion, line[0], line[1], f"CONFLICT: *Tests per criterion* verifies it as {row[0]}; the Lead keeps one"]


def matrix_table(folder):
    """The verification matrix of the work folder as Markdown lines, the number of MISSING rows and the number of
    CONFLICT rows."""
    errors = []
    plan = read(os.path.join(folder, PLAN), errors)
    if plan is None:
        return ["plan.md is missing (tiers docs and patch have no matrix: the first log.md row holds their criteria)"], \
            1, 0
    spec = read(os.path.join(folder, SPEC), errors)
    parts = sections(plan)
    if spec is None:
        criteria = set(CRITERION.findall(parts.get(CRITERIA_SECTION, "")))
        settled = check_amendments(PLAN, parts, criteria, errors)
    else:
        criteria, settled = check_spec(spec, errors)
    planned = planned_verifications(parts)
    conflicts = verification_conflicts(parts)
    sources = test_sources({path for _, _, _, paths in planned.values() for path in paths})
    changed, based = changed_files()
    head = git_output("log", "-1", "--format=%h", "--", ".", ":(exclude)specs").strip() or "the working tree"
    lines = [f"Verification matrix of {os.path.basename(folder)} on `{head}`", "",
             "| Criterion | Verification | Tests or check | Result |", "| --- | --- | --- | --- |"]
    missing = 0
    for criterion in sorted(criteria | set(settled), key=criterion_number):
        cells, absent = matrix_row(criterion, planned, settled, sources, head, changed)
        missing += absent
        rows = [cells] + [conflict_row(*conflict) for conflict in conflicts if conflict[0] == criterion]
        lines += ["| " + " | ".join(value.replace("|", "\\|") for value in row) + " |" for row in rows]
    if not based:
        lines += ["", f"Note: no merge base with {BASE_REF} (fetch it): a criterion id counted only in the test files "
                  "its row names and the uncommitted or untracked ones"]
    return lines, missing, len(conflicts)


def matrix(folder):
    """The verification matrix of the work folder as Markdown lines, and the number of MISSING rows."""
    lines, missing, _ = matrix_table(folder)
    return lines, missing


def record_on_base(path):
    """Whether BASE_REF has the decision record: an extended record, not a new one."""
    return bool(git_output("cat-file", "-t", f"{BASE_REF}:{path}").strip())


def challenge_verdict(plan, on_base=record_on_base):
    """(due, reason): the trigger of *Plan challenge* in `.squad/routing.md`, read from the plan."""
    tier = plan_tier(plan)
    if tier not in CHALLENGED_TIERS:
        return False, f"tier {tier} is never challenged"
    if tier == "security":
        return True, "tier security"
    parts = sections(plan)
    if parts.get(WITHOUT_TESTS):
        return False, "no production code (*Verification without tests*)"
    options = parts.get("Options considered", "")
    if options and not ONE_OPTION.match(options):
        return True, "*Options considered* names an alternative"
    new = [path for path in dict.fromkeys(RECORD.findall(parts.get("Decision records", ""))) if not on_base(path)]
    if new:
        return True, f"new decision record {', '.join(new)}"
    return False, "one option and no new decision record"


def print_matrix(folder):
    lines, missing, conflicts = matrix_table(folder)
    print("\n".join(lines))
    failures = ([f"{missing} criterion(s) MISSING — the Tester writes their tests (mode `coverage`)"] if missing else []) \
        + ([f"{conflicts} CONFLICT row(s) — the Lead keeps one verification per criterion (mode `revise`)"]
           if conflicts else [])
    print("FAIL: " + "; ".join(failures) if failures else "PASS")
    return 1 if failures else 0


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    spec_only, matrix_only = "--spec" in argv, "--matrix" in argv
    names = [arg for arg in argv if arg not in ("--spec", "--matrix")]
    if len(names) != 1 or any(arg.startswith("-") for arg in names) or (spec_only and matrix_only):
        print("Usage: python3 .squad/tools/plan-check.py <work folder> [--spec | --matrix]")
        return 1
    folder = resolve_folder(os.getcwd(), names[0])
    if folder is None:
        print(f"No work folder {names[0]!r} under specs/")
        return 1
    if matrix_only:
        return print_matrix(folder)
    errors, warnings = check_spec_only(folder) if spec_only else check(folder)
    for line in warnings:
        print(f"WARNING: {line}")
    for line in errors:
        print(line)
    plan = None if spec_only else read(os.path.join(folder, PLAN), [])
    if plan:
        due, reason = challenge_verdict(plan)
        print(f"Plan challenge: {'due' if due else 'not due'} — {reason}")
    print("PASS" if not errors else f"FAIL: {len(errors)} finding(s) — back to the Lead (mode `revise`)")
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main())
