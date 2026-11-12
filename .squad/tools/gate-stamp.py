#!/usr/bin/env python3
"""Gate stamp: the gates of step 7 run by the orchestrator itself and recorded with the commit they ran on.

A gate result is the orchestrator's own run, never a member's report. This script runs the gates the run's tier calls
for, by their names in `.squad/stack.md`, and appends the result to the work folder's `log.md` as one Orchestrator row
with the HEAD SHA (written by `squad-log.py`):

    | 2026-11-06 14:05 | 7 Gate stamp | Orchestrator | PASS on <sha> (tier standard): Format check pass; Analyzer gate
    pass; Test with coverage pass; Coverage gate pass; scope-check.py pass · figure-check.py 2 hit(s) |

The gates (`.squad/routing.md`, *Pipeline* step 7, *Tiers*, *Gates of a patch*, *Changes without production or test
code*):

- tiers `trivial`, `standard` and `security`: *Format check*, *Analyzer gate*, *Test with coverage*, *Coverage gate*
  and `scope-check.py`; a plan with *Verification without tests* leaves out the *Coverage gate*, and a branch that
  changes only Markdown files outside `specs/` runs *Format check* and `scope-check.py` alone, with `decision-check.py`
  when it changes a record or an area document (nothing it changes is built, analysed or tested). When the branch
  changes a file that is neither documentation (Markdown) nor a test, `figure-check.py` runs too: no gate, its hits are
  counted in the row and printed for routing;
- tier `tests`: the *test code only* row, *Format check*, *Analyzer gate* and *Test (project)*, and
  `scope-check.py --tier tests`;
- tier `patch` (also `patch/security`): the rows of *Gates of a patch* that match the files the branch changes outside
  `specs/` — Markdown is documentation; `*.sh`, `.github/workflows/*.yml` and Dockerfiles take the *Analyzer gate* with
  `--lint-only`; the test paths (`COVERAGE_TEST_PATHSPECS`, by default `COVERAGE_EXCLUDES`) are test code;
  `COVERAGE_PATHSPECS` is production code; any other file is a configuration or data file — every row that matches
  (both when the diff spans two kinds), then `scope-check.py --tier patch`. A row that asks for the check the Lead's
  result names (scripts, workflows, Dockerfiles, configuration and data files) needs it as `--check "<command>"`, or
  as `--project` when that check is a test; without one the stamp refuses to run. *Test (project)* is left out where
  *Test with coverage* runs, and the plain *Analyzer gate* covers its `--lint-only` pass;
- tier `docs`: *Format check* and `scope-check.py --tier docs`;
- once step 10 removed the work folder, `scope-check.py` runs with `--no-specs`;
- every tier: when the branch changes a file under `docs/decisions/` or `docs/areas/`, `decision-check.py` runs before
  `scope-check.py`, with `--require-history`: a released record it cannot check (a shallow clone, release tags not
  fetched, a remote that cannot be asked) fails it, with the fix `decision-check.py` names. A verbatim stamp runs it
  the same way. In a shallow clone that check cannot pass, so the stamp refuses before any gate runs and names the
  command that fetches the history (`git fetch --unshallow --tags origin`).

The tier is the *Tier* line of `plan.md`, or for `docs` and `patch` (no `plan.md`) what the Lead's first step-2 row
names (`Tier: patch`, `tier docs`). Each gate's command is the first code span of its row in the *Commands* tables of
`.squad/stack.md` — every profile's row in a repository with several profiles, identical commands once; a command
with a `{{TODO: …}}` placeholder is refused. *Test (project)* fills its `<NAME>` placeholder from `--project NAME=VALUE`
(the test project of the changed code, once per value); without a value for it, or where `stack.md` lists no *Test
(project)*, *Test* runs instead, the next broader gate. The *Coverage gate* is not run after a failed *Test with
coverage*, whose report it reads.

The stamp names a commit: it refuses to run while a file outside `specs/` is changed or untracked (commit first; a
scratch file belongs outside the repository), and it fails when HEAD moves while the gates run.

`--verify` exits 0 only when the log's last stamp passed, HEAD holds the same code (everything outside `specs/`) as
the commit it names, the tier it names is still the run's tier, nothing outside `specs/` is uncommitted, and BASE_REF
(`squad_settings.py`, default origin/main; fetch it first) is contained in HEAD: the first item of the step-9
checklist (`.squad/routing.md`, *PR approval*). `squad-next.py` uses the same check, `squad-log.py --summary` the
same without the last two parts.

`--verbatim` stamps a verbatim documentation fix (`.squad/orchestration.md`, *Verbatim documentation fixes*) and the
record status of step 9 (`.squad/routing.md`, *PR approval*): *Format
check*, `decision-check.py` and `scope-check.py` only, marked `(tier <tier>, verbatim)`; it is refused unless the last
stamp of the full gates passed and HEAD differs from it only in Markdown files and `docs/` (any other file only in
whitespace at the end of its lines or blank lines at its end: a `git diff --check` fix; a changed line ending or final
newline, mode or file type is none), and `--verify` accepts it on
the same terms. A review round belongs to the full stamp before it, so a verbatim fix needs no new round.

Each gate's whole output goes to a file outside the repository (`<temp>/squad-gates/<folder>-<sha>.log`, named at the
end); the session sees each command with `pass`, or with `FAIL` and the last `GATE_TAIL_LINES` lines of its output,
so a long test log is not carried in the orchestrator's context for the rest of the run.

`--dry-run` prints the gates and their commands without running them. `--stdin` reads the log from standard input
(after step 10 removed the work folder; `plan.md` is then read from the commit before that removal), appends the row
and prints the updated log to standard output and everything else to standard error, as `squad-log.py --stdin` does.

The work folder is matched against the folders under `specs/`, the `--project` values are names and paths only, and
the commands come from `stack.md` and the orchestrator's own `--check`; they run with `bash -c` from the repository
root.

Usage, from anywhere inside the repository:
    python3 .squad/tools/gate-stamp.py <work folder> [--project NAME=VALUE]... [--check "COMMAND"]... [--verbatim]
        [--dry-run] [--stdin]
    python3 .squad/tools/gate-stamp.py <work folder> --verify [--stdin]

Exit code 0 when every gate passed (`--verify`: the head has a passing stamp), 1 when a gate failed or the stamp
cannot be made or verified, 2 on a usage error.
"""
import argparse
import contextlib
import importlib.util
import os
import re
import subprocess
import sys
import tempfile
import types

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
try:
    import squad_settings as settings  # noqa: E402  (per-repository settings next to this script)
except ImportError:  # the template's own tests run this file without a target's settings
    settings = types.SimpleNamespace()

BASE_REF = getattr(settings, "BASE_REF", "origin/main")
TEST_PATHSPECS = getattr(settings, "COVERAGE_TEST_PATHSPECS", getattr(settings, "COVERAGE_EXCLUDES", []))
PRODUCTION_PATHSPECS = getattr(settings, "COVERAGE_PATHSPECS", [])
SPECS = "specs"
OUTSIDE_SPECS = ["--", ".", ":(exclude)specs"]
QUOTE_PATHS = "core.quotePath=false"
STACK = os.path.join(".squad", "stack.md")
ORCHESTRATOR = "Orchestrator"
STAMP_STEP = "7 Gate stamp"
AFTER_PR_STEP = "11 Gate stamp"
STAMP_CELL = re.compile(r"^\s*(?:7|11)\b.*\bgate stamp\b", re.I)
STAMP = re.compile(r"^(?P<verdict>PASS|FAIL) on (?P<sha>[0-9a-f]{40}) \(tier (?P<tier>[\w/]+)(?P<verbatim>, verbatim)?\)"
                   r"(?:: (?P<gates>.*))?$")
FOLDER_NAME = re.compile(r"\w[\w.-]*")
PLAN_TIER = re.compile(r"^Tier:\s*`?(\w+)", re.M)
LEAD_TIER = re.compile(r"\btier\b\W{0,3}(docs|patch(?:/security)?)\b", re.I)
STEP_TWO = re.compile(r"^\s*2[ab]?\b")
WITHOUT_TESTS = re.compile(r"^## Verification without tests[ \t]*$", re.M)
NEXT_SECTION = re.compile(r"^## ", re.M)
COMMENT = re.compile(r"<!--.*?-->", re.S)
COMMAND_ROW = re.compile(r"^\|\s*\*(?P<name>[^*|\n]+)\*[^|\n]*\|\s*`(?P<command>[^`\n]+)`")
PLACEHOLDER = re.compile(r"<(\w+)>")
PROJECT_VALUE = re.compile(r"(\w+)=([\w./-]+)")
TODO = "{{TODO"
WARNING_LINE = re.compile(r"^WARNING: ", re.M)

FORMAT_CHECK = "Format check"
ANALYZER = "Analyzer gate"
ANALYZER_LINT = "Analyzer gate --lint-only"
TEST = "Test"
TEST_PROJECT = "Test (project)"
TEST_COVERAGE = "Test with coverage"
COVERAGE_GATE = "Coverage gate"
SCOPE = "scope-check.py"
FIGURE = "figure-check.py"
DECISION = "decision-check.py"
# The files whose change runs decision-check.py in the full gates too: the records and the area documents.
RECORD_PATHSPECS = ["docs/decisions/", "docs/areas/"]
VERBATIM = ", verbatim"
CHECK = "check"
PASSED = "pass"
# The lines of a failed gate's output the session sees; the whole output is in the file `output_path` names.
GATE_TAIL_LINES = 60
FULL = (FORMAT_CHECK, ANALYZER, TEST_COVERAGE, COVERAGE_GATE)
ORDER = (FORMAT_CHECK, ANALYZER_LINT, ANALYZER, TEST_PROJECT, TEST_COVERAGE, COVERAGE_GATE)
# .squad/routing.md, *Gates of a patch*: the kind of a changed file -> the gates of its row.
PATCH_ROWS = {"documentation": (FORMAT_CHECK,), "lint": (ANALYZER_LINT,), "data": (),
              "test": (FORMAT_CHECK, ANALYZER, TEST_PROJECT), "production": FULL}
NEEDS_CHECK = {"lint", "data"}
DOCUMENTATION_ONLY = {"documentation"}
UNSHALLOW = "git fetch --unshallow --tags origin"
FIGURE_TIERS = ("trivial", "standard", "security")
DEPENDS = {COVERAGE_GATE: TEST_COVERAGE}


class Refusal(Exception):
    """The stamp cannot be made: the message says why (exit code 1)."""


_LOG = []


def squad_log():
    """`squad-log.py` next to this script as a module (loaded once): its row writer and log parser."""
    if not _LOG:
        spec = importlib.util.spec_from_file_location("squad_log", os.path.join(HERE, "squad-log.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        _LOG.append(module)
    return _LOG[0]


def git(root, *args):
    """(exit code, standard output) of a git command in `root`."""
    try:
        done = subprocess.run(["git", "-C", root, *args], capture_output=True, text=True, check=False)
    except OSError:
        return 1, ""
    return done.returncode, done.stdout


def folder_name(folder):
    """The work folder's name under `specs/` (`issue-12` for `specs/issue-12/`), or None when it is no plain name."""
    name = folder.strip().replace("\\", "/").strip("/").removeprefix(SPECS + "/")
    return name if FOLDER_NAME.fullmatch(name) else None


def work_file(root, name, filename):
    """The text of `specs/<name>/<filename>`: from the working tree while the folder exists, else, after step 10
    removed the folder, from the commit before that removal; None when there is neither."""
    path = os.path.join(root, SPECS, name, filename)
    if os.path.isdir(os.path.dirname(path)):
        if not os.path.isfile(path):
            return None
        with open(path, encoding="utf-8-sig") as handle:
            return handle.read()
    rel = f"{SPECS}/{name}/{filename}"
    code, removal = git(root, "log", "--diff-filter=D", "--format=%H", "-1", "--", rel)
    if code != 0 or not removal.strip():
        return None
    code, text = git(root, "show", f"{removal.strip()}^:{rel}")
    return text if code == 0 else None


def run_tier(plan, rows):
    """The run's tier: the *Tier* line of `plan.md`, else what the Lead's first step-2 row names (`docs`, `patch` or
    `patch/security`); None when neither says."""
    if plan:
        found = PLAN_TIER.search(plan)
        return found.group(1).lower() if found else None
    for _, step, member, result in rows:
        if squad_log().member_agent(member) == "squad-lead" and STEP_TWO.match(step):
            found = LEAD_TIER.search(result)
            if found:
                return found.group(1).lower()
    return None


def code_free(plan):
    """True when the plan has a filled *Verification without tests* section (no production or test code)."""
    found = WITHOUT_TESTS.search(plan or "")
    if not found:
        return False
    body = plan[found.end():]
    end = NEXT_SECTION.search(body)
    return bool(COMMENT.sub("", body[:end.start()] if end else body).strip())


def merge_base(root):
    code, out = git(root, "merge-base", BASE_REF, "HEAD")
    if code != 0:
        raise Refusal(f"Cannot find the merge base with {BASE_REF}: fetch it (git fetch origin main) or set BASE_REF in "
                      ".squad/tools/squad_settings.py")
    return out.strip()


def changed(root, base, pathspecs=None):
    """The files the branch changes between `base` and HEAD (deleted ones included), outside `specs/`; with
    `pathspecs`, only those they select."""
    if pathspecs is not None and not pathspecs:
        return set()
    selection = OUTSIDE_SPECS if pathspecs is None else ["--", *pathspecs]
    _, out = git(root, "-c", QUOTE_PATHS, "diff", "--name-only", "--no-renames", base, "HEAD", *selection)
    return {line for line in out.splitlines() if line and not line.startswith(SPECS + "/")}


def is_lint_file(path):
    """A shell script, a GitHub workflow or a Dockerfile: the files the *Analyzer gate* `--lint-only` checks."""
    name = os.path.basename(path)
    workflow = path.startswith(".github/workflows/") and path.endswith((".yml", ".yaml"))
    dockerfile = name == "Dockerfile" or name.endswith(".Dockerfile") or (
        name.startswith("Dockerfile.") and not name.endswith((".md", ".txt", ".html")))
    return path.endswith(".sh") or workflow or dockerfile


def file_kind(path, tests, production):
    """The row of *Gates of a patch* a changed file belongs to."""
    if path.endswith(".md"):
        return "documentation"
    if path in tests:
        return "test"
    if is_lint_file(path):
        return "lint"
    return "production" if path in production else "data"


def changed_kinds(root, base):
    """The kinds of the files the branch changes outside `specs/` (`file_kind`)."""
    tests, production = changed(root, base, TEST_PATHSPECS), changed(root, base, PRODUCTION_PATHSPECS)
    return {file_kind(path, tests, production) for path in changed(root, base)}


def patch_gates(kinds):
    """The gates of the rows of *Gates of a patch* that match the kinds, in the table's order, each once."""
    due = {gate for kind in kinds for gate in PATCH_ROWS[kind]}
    if TEST_COVERAGE in due:
        due.discard(TEST_PROJECT)  # the production row runs the full suite with coverage instead
    if ANALYZER in due:
        due.discard(ANALYZER_LINT)  # the full analyzer gate runs the lint pass too
    return [gate for gate in ORDER if gate in due]


def due_gates(tier, plan, kinds):
    """(the stack gates, the options of scope-check.py, whether a check the Lead names is needed, whether
    figure-check.py runs) for the tier and the kinds of changed files."""
    base = tier.split("/")[0]
    if base == "docs":
        return [FORMAT_CHECK], ["--tier", "docs"], False, False
    if base == "patch":
        return patch_gates(kinds), ["--tier", "patch"], bool(kinds & NEEDS_CHECK), False
    if base == "tests":
        return [FORMAT_CHECK, ANALYZER, TEST_PROJECT], ["--tier", "tests"], False, False
    if kinds and kinds <= DOCUMENTATION_ONLY:
        return [FORMAT_CHECK], [], False, False  # nothing the branch changes is built, analysed or tested
    gates = [gate for gate in FULL if not (gate == COVERAGE_GATE and code_free(plan))]
    figure = base in FIGURE_TIERS and bool(kinds - {"documentation", "test"})
    return gates, [], False, figure


def stack_commands(text):
    """{command name: [commands]} from the *Commands* tables of `stack.md`: the first code span of each row, every
    profile's row in a repository with several profiles, identical commands once."""
    commands = {}
    for line in text.splitlines():
        match = COMMAND_ROW.match(line)
        if match:
            found = commands.setdefault(match.group("name").strip(), [])
            if match.group("command") not in found:
                found.append(match.group("command"))
    return commands


def parse_projects(values):
    """{placeholder: [values]} from the `--project NAME=VALUE` options."""
    projects = {}
    for value in values:
        match = PROJECT_VALUE.fullmatch(value)
        if not match:
            raise Refusal(f"--project {value!r}: write NAME=VALUE, NAME the placeholder of *Test (project)* in "
                          ".squad/stack.md and VALUE a name or path (letters, digits, . / _ -)")
        projects.setdefault(match.group(1), []).append(match.group(2))
    return projects


def fill_placeholders(commands, projects):
    """Each command with its one `<NAME>` placeholder filled, once per value given for it; a command whose
    placeholder has no value is left out."""
    filled = []
    for command in commands:
        names = set(PLACEHOLDER.findall(command))
        if len(names) == 1 and names <= set(projects):
            name = names.pop()
            filled += [command.replace(f"<{name}>", value) for value in projects[name]]
        elif not names:
            filled.append(command)
    return filled


def gate_commands(name, commands, projects):
    """(label, commands) of one gate: *Test (project)* falls back to *Test* without a filled command."""
    if name == TEST_PROJECT:
        filled = fill_placeholders(commands.get(TEST_PROJECT, []), projects)
        return (TEST_PROJECT, filled) if filled else (TEST, commands.get(TEST, []))
    if name == ANALYZER_LINT:
        return name, [f"{command} --lint-only" for command in commands.get(ANALYZER, [])]
    return name, commands.get(name, [])


def checked(label, commands):
    """The commands of a gate, refused when `stack.md` has none or one still holds a placeholder."""
    if not commands:
        raise Refusal(f"*{label}* is not in the Commands table of {STACK}")
    for command in commands:
        if TODO in command or PLACEHOLDER.search(command):
            raise Refusal(f"*{label}* still holds a placeholder in {STACK} (`{command}`): fill it in first")
    return commands


def plan_run(root, context, options):
    """[(label, commands)] of the gates to run, in order, and whether figure-check.py runs too."""
    gates, scope, needs_check, figure = due_gates(context["tier"], context["plan"], context["kinds"])
    if options.verbatim:
        (gates, scope), needs_check, figure = verbatim_run(context), False, False
    if needs_check and not (options.check or options.project):
        raise Refusal("the branch changes a script, workflow, Dockerfile, configuration or data file (Gates of a patch): "
                      "pass the check the Lead's result names for it with --check \"<command>\" (or --project when it is "
                      "a test)")
    with open(os.path.join(root, STACK), encoding="utf-8-sig") as handle:
        commands = stack_commands(handle.read())
    projects = parse_projects(options.project)
    run = [(label, checked(label, found)) for label, found in (gate_commands(name, commands, projects)
                                                               for name in gates)]
    run += [(f"{CHECK} {command}", [command]) for command in options.check]
    if options.verbatim or context.get("records"):
        run.append(decision_gate())
    run.append(tool_gate(SCOPE, scope + (["--no-specs"] if context["after_pr"] else [])))
    return run, figure


def verbatim_run(context):
    """The checks of a verbatim documentation fix (`.squad/orchestration.md`, *Verbatim documentation fixes*): *Format
    check*, `decision-check.py` and `scope-check.py` with the tier's options."""
    _, scope, _, _ = due_gates(context["tier"], context["plan"], set())
    return [FORMAT_CHECK], scope


def shallow(root):
    """True when the repository is a shallow clone (`git rev-parse --is-shallow-repository`)."""
    code, out = git(root, "rev-parse", "--is-shallow-repository")
    return code == 0 and out.strip() == "true"


def shallow_refusal(root, run):
    """Refuse a run with `decision-check.py --require-history` in a shallow clone before any gate runs: the check
    cannot pass there, and the gates before it may take minutes."""
    if any(label == DECISION for label, _ in run) and shallow(root):
        raise Refusal(f"the clone is shallow, so {DECISION} --require-history cannot check the released records: run "
                      f"'{UNSHALLOW}' (step 1 of the skill) and stamp again")


def decision_gate():
    """(label, [command]) of `decision-check.py` in a gate: a released record it cannot check (a shallow clone, tags
    not fetched) fails it instead of a warning."""
    return DECISION, [f"python3 .squad/tools/{DECISION} --require-history"]


def tool_gate(tool, options):
    """(label, [command]) of one of the squad's own tools under `.squad/tools/`."""
    return " ".join([tool, *options]), [" ".join(["python3", f".squad/tools/{tool}", *options])]


def output_path(name, sha):
    """The file the full output of a stamp's gates goes to, outside the repository: `<temp>/squad-gates/<folder>-<sha>.log`."""
    directory = os.path.join(tempfile.gettempdir(), "squad-gates")
    os.makedirs(directory, exist_ok=True)
    return os.path.join(directory, f"{name}-{(sha or 'head')[:12]}.log")


def run_command(command, root, stream, full=None):
    """Run one command with `bash -c` from the repository root; True when it exits 0. Its whole output goes to the
    file `full` and `stream` (None: standard output) gets the command and its result, a failure with the last
    `GATE_TAIL_LINES` lines of its output (#313: a long test log is not carried in the orchestrator's context); without
    `full`, `stream` gets the whole output."""
    out = stream or sys.stdout
    print(f"$ {command}", file=out, flush=True)
    done = subprocess.run(["bash", "-c", command], cwd=root, capture_output=True, text=True, check=False)
    text = done.stdout + done.stderr
    if full is None:
        print(text, file=out, end="", flush=True)
        return done.returncode == 0
    full.write(f"$ {command}\n{text}" + ("" if text.endswith("\n") or not text else "\n"))
    full.flush()
    if done.returncode == 0:
        print("  pass", file=out, flush=True)
        return True
    lines = text.splitlines()
    shown = lines[-GATE_TAIL_LINES:]
    head_note = f"  FAIL (exit {done.returncode}); the last {len(shown)} of {len(lines)} lines:" if len(lines) > \
        len(shown) else f"  FAIL (exit {done.returncode}):"
    print("\n".join([head_note, *shown]), file=out, flush=True)
    return False


def run_gates(run, root, stream, full=None):
    """[(label, status)]: `pass`, `FAIL`, or `not run (<gate> failed)` for a gate whose input failed."""
    results, failed = [], set()
    for label, commands in run:
        before = DEPENDS.get(label)
        if before in failed:
            results.append((label, f"not run ({before} failed)"))
            failed.add(label)
            continue
        passed = True
        for command in commands:  # every profile's command runs, also after one failed
            passed = run_command(command, root, stream, full) and passed
        results.append((label, PASSED if passed else "FAIL"))
        if not passed:
            failed.add(label)
    return results


def figure_hits(root, stream):
    """The number of hits of figure-check.py (its warnings, also printed), or None when it did not run."""
    done = subprocess.run(["python3", os.path.join(".squad", "tools", FIGURE)], cwd=root, capture_output=True,
                          text=True, check=False)
    print(done.stdout + done.stderr, file=stream or sys.stdout, end="", flush=True)
    return len(WARNING_LINE.findall(done.stdout)) if done.returncode == 0 else None


def stamp_text(verdict, sha, tier, results, hits, verbatim=False):
    """The result cell of the stamp row."""
    kind = VERBATIM if verbatim else ""
    text = f"{verdict} on {sha} (tier {tier}{kind}): " + "; ".join(f"{label} {status}" for label, status in results)
    if hits is not None:
        text += f" · {FIGURE} {hits} hit(s)"
    return text


def uncommitted(root):
    """The files outside `specs/` that are changed, staged or untracked (not ignored)."""
    _, out = git(root, "-c", QUOTE_PATHS, "status", "--porcelain", "--untracked-files=all", *OUTSIDE_SPECS)
    return [line[3:] for line in out.splitlines() if line.strip()]


def head(root):
    code, out = git(root, "rev-parse", "HEAD")
    return out.strip() if code == 0 else None


def read_rows(log_path):
    with open(log_path, encoding="utf-8-sig") as handle:
        return squad_log().parse_rows(handle.read())


def run_context(root, name, rows, after_pr):
    """What decides the gates: tier, plan text, kinds of changed files and whether step 10 has passed."""
    plan = work_file(root, name, "plan.md")
    tier = run_tier(plan, rows)
    if tier is None:
        raise Refusal("The tier is unknown: plan.md has no Tier line, and no Lead row of step 2 names tier docs or "
                      "patch")
    base = merge_base(root)
    return {"tier": tier, "plan": plan, "kinds": changed_kinds(root, base), "after_pr": after_pr,
            "records": bool(changed(root, base, RECORD_PATHSPECS))}


def run_logged(run, root, name, sha, stream):
    """`run_gates` with the whole output in the file `output_path` names, which it prints at the end."""
    path = output_path(name, sha)
    with open(path, "w", encoding="utf-8") as full:
        results = run_gates(run, root, stream, full)
    print(f"Full gate output: {path} (hand it to the reviewers as the stamp's gate output)", file=stream or sys.stdout)
    return results


def stamp_command(options, root, name, log_path, after_pr, stream=None):
    """Run the gates and append the stamp row; the exit code."""
    dirty = uncommitted(root)
    if dirty and not options.dry_run:
        raise Refusal("Commit first: the stamp names the commit its gates ran on, and these files outside specs/ are "
                      "changed or untracked: " + ", ".join(dirty[:10]))
    rows = read_rows(log_path)
    context = run_context(root, name, rows, after_pr)
    if options.verbatim:
        verbatim_refusal(root, rows)
    run, figure = plan_run(root, context, options)
    if not options.dry_run:
        shallow_refusal(root, run)
    if options.dry_run:
        for label, commands in run:
            print(f"{label}: " + " && ".join(f"`{command}`" for command in commands))
        if figure:
            print(f"{FIGURE} (no gate)")
        return 0
    sha = head(root)
    results = run_logged(run, root, name, sha, stream)
    hits = figure_hits(root, stream) if figure else None
    if head(root) != sha:
        results.append(("HEAD", f"moved to {head(root)} while the gates ran"))
    verdict = "PASS" if all(status == PASSED for _, status in results) else "FAIL"
    row = squad_log().append_row(log_path, AFTER_PR_STEP if after_pr else STAMP_STEP, ORCHESTRATOR,
                                 stamp_text(verdict, sha, context["tier"], results, hits, options.verbatim))
    print(row)
    print(f"Gate stamp: {verdict} on {sha[:12]}" + ("" if verdict == "PASS" else " — fix the failing gates (the "
                                                     "output above), commit and run the stamp again"))
    return 0 if verdict == "PASS" else 1


def stamps(rows):
    """The stamp rows of the log as `STAMP` matches, in log order."""
    found = []
    for _, step, member, result in rows:
        match = STAMP.match(result.strip()) if member == ORCHESTRATOR and STAMP_CELL.match(step) else None
        if match:
            found.append(match)
    return found


def same_code(root, sha):
    """True when HEAD holds the same files outside `specs/` as `sha`."""
    code, _ = git(root, "diff", "--quiet", sha, "HEAD", *OUTSIDE_SPECS)
    return code == 0


def documentation_only(root, old, new):
    """True when every file that differs between `old` and `new` outside `specs/` is Markdown or under `docs/`, or
    differs only in whitespace at the end of its lines (a `git diff --check` fix): what a verbatim fix may touch."""
    code, out = git(root, "-c", QUOTE_PATHS, "diff", "--name-only", "--no-renames", old, new, *OUTSIDE_SPECS)
    if code != 0:
        return False
    others = [path for path in out.splitlines() if path and not (path.endswith(".md") or path.startswith("docs/"))]
    if not others:
        return True
    return all(whitespace_fix(root, old, new, path) for path in others)


def blob(root, sha, path):
    """The bytes of `path` at `sha`, or None when the commit has no such file."""
    try:
        done = subprocess.run(["git", "-C", root, "show", f"{sha}:{path}"], capture_output=True, check=False)
    except OSError:
        return None
    return done.stdout if done.returncode == 0 else None


def tree_entry(root, sha, path):
    """The mode and type of `path` at `sha` (`git ls-tree`), or None when the commit has no such file."""
    code, out = git(root, "ls-tree", sha, "--", path)
    fields = out.split(None, 2)
    return tuple(fields[:2]) if code == 0 and len(fields) == 3 else None


def strip_line_end(line):
    """A line without the spaces and tabs before its end; a carriage return that ends it stays."""
    carriage = line.endswith(b"\r")
    return (line[:-1] if carriage else line).rstrip(b" \t") + (b"\r" if carriage else b"")


def whitespace_normal(data):
    """`data` without spaces and tabs before a line end and without blank lines at its end; line endings (a carriage
    return) and the final newline are kept, so a conversion of either is no whitespace fix."""
    final = data.endswith(b"\n")
    lines = [strip_line_end(line) for line in data.split(b"\n")]
    if final:
        lines.pop()
    while lines and not lines[-1].strip():
        lines.pop()
    return b"\n".join(lines), final


def whitespace_fix(root, old, new, path):
    """True when `path` differs between `old` and `new` only in spaces and tabs at the end of its lines or in blank
    lines at its end (what `git diff --check` reports); a changed line ending, final newline, mode or file type is no
    such fix."""
    if tree_entry(root, old, path) != tree_entry(root, new, path):
        return False  # a changed mode or file type (the exec bit, a symlink) is no whitespace fix
    before, after = blob(root, old, path), blob(root, new, path)
    if before is None or after is None:
        return False
    return whitespace_normal(before) == whitespace_normal(after)


def last_full(found):
    """The last stamp of the full gates (not a verbatim fix's), or None."""
    full = [match for match in found if not match.group("verbatim")]
    return full[-1] if full else None


def verbatim_refusal(root, rows):
    """Refuse a verbatim stamp unless the last full stamp passed and HEAD differs from it in documentation only."""
    full = last_full(stamps(rows))
    if full is None or full.group("verdict") != "PASS":
        raise Refusal("--verbatim needs a passing stamp of the full gates before the fix: run gate-stamp.py without it")
    if not documentation_only(root, full.group("sha"), "HEAD"):
        raise Refusal(f"--verbatim: the head changes more than documentation since the stamp on "
                      f"{full.group('sha')[:12]}: that is an ordinary fix, run gate-stamp.py without --verbatim")


def chain_problem(root, found, last):
    """For a verbatim stamp: why the full stamp before it does not carry it, or None."""
    full = last_full(found)
    if full is None or full.group("verdict") != "PASS":
        return "the stamp of the full gates before the verbatim fix did not pass: run gate-stamp.py"
    if not documentation_only(root, full.group("sha"), last.group("sha")):
        return "more than documentation changed since the full gate stamp: run gate-stamp.py"
    return None


def stamp_problem(root, rows, tier=None, base_check=True, tree_check=True):
    """None when HEAD's code carries a passing stamp, else why not. `tier` (when known) must be the stamp's tier;
    `tree_check` refuses uncommitted files outside `specs/`, `base_check` a head that lacks BASE_REF."""
    found = stamps(rows)
    if not found:
        return "no gate stamp in the log: run gate-stamp.py (step 7)"
    last = found[-1]
    sha = last.group("sha")
    if last.group("verdict") != "PASS":
        return f"the last gate stamp failed on {sha[:12]}: fix the failing gates, commit and run gate-stamp.py again"
    if not same_code(root, sha):
        return f"the code changed since the gate stamp on {sha[:12]}: run gate-stamp.py on the new head (step 7)"
    chain = chain_problem(root, found, last) if last.group("verbatim") else None
    if chain:
        return chain
    if tier and last.group("tier") != tier:
        return f"the gate stamp ran for tier {last.group('tier')}, the run is tier {tier} now: run gate-stamp.py again"
    if tree_check and uncommitted(root):
        return "files outside specs/ are changed or untracked: commit them and run gate-stamp.py again"
    if base_check and git(root, "merge-base", "--is-ancestor", BASE_REF, "HEAD")[0] != 0:
        return (f"{BASE_REF} has commits the head lacks (or is not fetched): git fetch origin main, merge it (*Merge "
                "conflicts*) and run gate-stamp.py again")
    return None


def branch_changes_code(root):
    """True when the branch changes anything outside `specs/` against its merge base with BASE_REF, or when that
    merge base cannot be found; False outside a Git repository."""
    if git(root, "rev-parse", "--git-dir")[0] != 0:
        return False
    code, base = git(root, "merge-base", BASE_REF, "HEAD")
    if code != 0:
        return True
    return git(root, "diff", "--quiet", base.strip(), "HEAD", *OUTSIDE_SPECS)[0] != 0


def summary_problem(root, rows):
    """For `squad-log.py --summary`: None when the branch changes no code or HEAD's code carries a passing stamp,
    else why not."""
    if not branch_changes_code(root):
        return None
    return stamp_problem(root, rows, base_check=False, tree_check=False)


def verify_command(root, name, log_path):
    rows = read_rows(log_path)
    tier = run_tier(work_file(root, name, "plan.md"), rows)
    problem = stamp_problem(root, rows, tier)
    print(f"Gate stamp: FAIL — {problem}" if problem else "Gate stamp: PASS — the head carries a passing stamp")
    return 1 if problem else 0


def build_parser():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("folder", help="the work folder's name under specs/ (issue-12, feature-12-export)")
    parser.add_argument("--project", action="append", default=[], metavar="NAME=VALUE",
                        help="fills the <NAME> placeholder of *Test (project)*: the test project of the changed code")
    parser.add_argument("--check", action="append", default=[], metavar="COMMAND",
                        help="the check the Lead's result names for a patch edit (Gates of a patch)")
    parser.add_argument("--verbatim", action="store_true", help="the checks of a verbatim documentation fix after a "
                        "passing stamp: Format check, decision-check.py and scope-check.py")
    parser.add_argument("--verify", action="store_true", help="exit 0 only when the head carries a passing stamp")
    parser.add_argument("--dry-run", action="store_true", help="print the gates and their commands, run nothing")
    parser.add_argument("--stdin", action="store_true", help="read the log from standard input (after step 10)")
    return parser


def dispatch(options, root, name, log_path, after_pr, stream=None):
    if options.verify:
        return verify_command(root, name, log_path)
    return stamp_command(options, root, name, log_path, after_pr, stream)


def run_on_stdin(options, root, name):
    """`--stdin`: the log lives in a temporary file; a new row prints the whole log to standard output."""
    with tempfile.TemporaryDirectory() as directory:
        path = os.path.join(directory, "log.md")
        with open(path, "wb") as handle:
            handle.write(sys.stdin.buffer.read())
        with contextlib.redirect_stdout(sys.stderr):
            code = dispatch(options, root, name, path, True, sys.stderr)
        if not (options.verify or options.dry_run) and code in (0, 1):
            with open(path, encoding="utf-8", newline="") as handle:
                sys.stdout.write(handle.read())
        return code


def main(argv=None):
    parser = build_parser()
    options = parser.parse_args(argv)
    if options.verify and (options.project or options.check or options.dry_run or options.verbatim):
        parser.error("--verify reads the log only: no --project, --check, --dry-run or --verbatim with it")
    name = folder_name(options.folder)
    if name is None:
        parser.error(f"{options.folder!r} is no work folder name (issue-12, feature-12-export)")
    code, top = git(os.getcwd(), "rev-parse", "--show-toplevel")
    root = top.strip() if code == 0 else os.getcwd()
    try:
        if options.stdin:
            return run_on_stdin(options, root, name)
        log_path = squad_log().resolve_log(root, name)
        if log_path is None:
            print(f"No log.md in specs/{name} (after step 10, pipe the working-record comment's log table in with --stdin)")
            return 1
        return dispatch(options, root, name, log_path, False)
    except Refusal as refusal:
        print(f"Gate stamp refused: {refusal}", file=sys.stderr if options.stdin else sys.stdout)
        return 1


if __name__ == "__main__":
    sys.exit(main())
