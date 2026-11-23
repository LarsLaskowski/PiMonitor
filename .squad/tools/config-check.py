#!/usr/bin/env python3
"""Config gate for the squad: agent and skill definitions must load, and the per-repository squad files
must exist and be filled in.

Claude Code silently drops an agent or skill whose YAML front matter does not parse (for example an
unquoted description containing ": "), so a broken file only shows up when a squad run tries to launch
it. This script checks, without arguments:

- every `.claude/agents/*.md` and every `SKILL.md` under `.claude/skills/` has front matter that parses as
  YAML, with a non-empty `name` and `description`;
- an agent's `name` equals its file name, a skill's `name` equals its folder name;
- `.claude/settings.json` declares the `PreToolUse` hooks `.claude/hooks/git-guard.py` for `Bash` and
  `.claude/hooks/file-guard.py` for `Write`, `Edit`, `MultiEdit` and `NotebookEdit`, and both hooks exist: they keep
  Git and GitHub write operations and the files a member does not own out of the squad members' hands (declared in
  the project settings, because Claude Code skips a project agent's front-matter hooks without workspace trust, as in
  a cloud session); every squad agent (`squad-*.md`) names its `model` as an alias (`haiku`, `sonnet`, `opus`) and an
  explicit `effort`;
- `AGENTS.md` (the rules every agent reads) and its GitHub Copilot mirror `.github/copilot-instructions.md` exist
  and are identical from their first `## ` heading on (only the title and introduction differ; Claude Code `@path`
  import lines on a line of their own are left out of the comparison, Copilot reads
  `.github/instructions/*.instructions.md` itself), and `CLAUDE.md` imports `AGENTS.md` (`@AGENTS.md` on a line of
  its own), so Claude Code loads the same rules;
- `.squad/template.json` names the template repository (where lessons about template-managed files are
  filed) and, for a repository with several stack profiles, lists them in `profiles` (the first one is
  `profile`); then every profile has its `analyzer-check-<profile>.py` and `session-start-<profile>.sh`
  next to the dispatchers, and `squad_settings.py` lists at least one `COVERAGE_REPORTS` entry per profile;
- `.squad/tools/decision-check.py` passes (decision records consistent, released records frozen);
- `.squad/stack.md`, `.squad/project.md` and `.squad/tools/squad_settings.py` exist, the settings file loads, and no
  file the template seeded or rebuilt still contains a template placeholder (`{{TODO: …}}` — a marker that
  ordinary Go templates, `docker --format` strings or GitHub Actions expressions never contain);
- no bullet of a seeded file is still a seed's placeholder text with the braces removed (`- its interface` in
  `.squad/project.md`, from the seed's `- {{TODO: its interface}}`; `SEEDED_BULLETS`).

Warnings, which do not fail the check, name an entry of `.squad/project.md` (a bullet, a numbered item or a table
row) with more than `PROJECT_ENTRY_MAX_PROSE` prose words (`squad_settings.py`, default 50; code spans and Markdown
links do not count), and `.squad/project.md` when it gained more than `PROJECT_MAX_GROWTH` words (default 200) against
the merge base with BASE_REF (`squad_settings.py`, default origin/main; skipped without one, or when the file is new
there): five members read it on every launch, so its entries point to the area documents and decision records instead
of restating them, and a change adds pointers, not copies. The same entry check and `TEST_DOUBLES_MAX_GROWTH` (default
100) apply to the *Test doubles* section of `docs/UNIT_TESTS.md`, and a *Test doubles* section still in
`.squad/project.md` is named: it belongs in the test document. The warnings of `decision-check.py` (sizes of records and
area documents, release tags it cannot see) are shown as well.

The front matter is parsed with PyYAML where it is installed. Without it, a built-in parser reads the YAML that
agents and skills use (`key: value` lines, quoted values, block scalars, lists) and still rejects what makes
Claude Code drop a file: a `: ` or a final `:` in an unquoted value, an unterminated quote, text after a closing
quote, a value starting with a reserved character or `- `, a line that is no `key: value`.

Usage, from anywhere inside the repository:
    python3 .squad/tools/config-check.py
    python3 .squad/tools/config-check.py --project   # only the warnings about .squad/project.md and Test doubles

With `--project` only the warnings about `.squad/project.md` and the *Test doubles* of `docs/UNIT_TESTS.md` are
printed, and the exit code is 0: `gate-stamp.py` runs it in step 7 when the branch changes one of them.

Exit code 0 when everything is valid, 1 otherwise.
"""
import glob
import importlib.util
import json
import os
import re
import subprocess
import sys

CLAUDE_DIR = ".claude"
GITHUB_DIR = ".github"
SQUAD_DIR = ".squad"
AGENTS_DIR = os.path.join(CLAUDE_DIR, "agents")
GIT_GUARD = os.path.join(CLAUDE_DIR, "hooks", "git-guard.py")
FILE_GUARD = os.path.join(CLAUDE_DIR, "hooks", "file-guard.py")
# The members' guards and the tools the project settings must run each one for.
SQUAD_GUARDS = [(GIT_GUARD, ("Bash",)), (FILE_GUARD, ("Write", "Edit", "MultiEdit", "NotebookEdit"))]
CLAUDE_SETTINGS = os.path.join(CLAUDE_DIR, "settings.json")
MODELS = ("haiku", "sonnet", "opus")
EFFORTS = ("low", "medium", "high", "xhigh", "max")
SKILLS_DIR = os.path.join(CLAUDE_DIR, "skills")
INSTRUCTION_FILES = ["AGENTS.md", os.path.join(GITHUB_DIR, "copilot-instructions.md")]
CLAUDE_FILE = "CLAUDE.md"
RULES_IMPORT = re.compile(r"^@AGENTS\.md[ \t]*$", re.M)
PROJECT_FILE = os.path.join(SQUAD_DIR, "project.md")
SETTINGS_FILE = os.path.join(SQUAD_DIR, "tools", "squad_settings.py")
REQUIRED_FILES = [os.path.join(SQUAD_DIR, "stack.md"), PROJECT_FILE, SETTINGS_FILE]
PROJECT_MAX_GROWTH = 200
PROJECT_ENTRY_MAX_PROSE = 50
# The test doubles' section of the test document, which the Tester keeps (`.squad/team.md`, *Files*).
UNIT_TESTS_FILE = os.path.join("docs", "UNIT_TESTS.md")
TEST_DOUBLES = "Test doubles"
TEST_DOUBLES_MAX_GROWTH = 100
# No quantifier overlaps another one, so matching stays linear (no backtracking).
PLACEHOLDER = re.compile(r"\{\{TODO:([^{}]*)\}\}")
IMPORT_LINE = re.compile(r"^@[^\s@]\S*[ \t]*\n", re.M)
PLACEHOLDER_GLOBS = INSTRUCTION_FILES + REQUIRED_FILES + [
    "SECURITY.md", "sonar-project.properties",
    os.path.join(SQUAD_DIR, "**", "*.md"), os.path.join("docs", "**", "*.md"),
    os.path.join(GITHUB_DIR, "**", "*.md"), os.path.join(GITHUB_DIR, "**", "*.yml"),
]
# The text of every seed bullet that starts with a placeholder, braces and `TODO:` removed, by the file it is seeded
# as: a bullet that still reads like this was never filled in (tools/test_config_check.py keeps it equal to the seeds).
SEEDED_BULLETS = {
    PROJECT_FILE: (
        "Secrets and tokens: where they are read, stored, compared, logged",
        "Authentication and sessions",
        "File writes and paths derived from external input",
        "Parsing of external input (XML/JSON/YAML, uploads, CLI arguments)",
        "Outbound calls (HTTP clients, TLS, timeouts)",
        "Logging of external data",
        "guarantee — [decision NNNN](../docs/decisions/NNNN-title.md), [area](../docs/areas/area.md)",
        "the options type / config schema",
        "registration / binding",
        "default config file",
        "the configuration table in `README.md` (key, environment variable, default)",
        "the test that pins the binding",
        "its interface",
        "registration and lifetime",
        "the test double in the test project",
        "the component list in `docs/ARCHITECTURE.md`",
        "the client and its DTOs — the external shape must not leak past it",
        "the fake/stub in the tests",
    ),
    UNIT_TESTS_FILE: (
        "`FakeX` — what it stands in for",
    ),
    "SECURITY.md": (
        "The project's attack surface — the same areas as *Security areas* in `.squad/project.md`.",
    ),
    os.path.join(SQUAD_DIR, "stack.md"): (
        "project-specific conventions",
        "project-specific conventions: naming, module layout, error handling",
    ),
}
# Seed bullets of earlier template versions, still recognized in a repository adopted then (the seed is never refreshed).
EARLIER_SEED_BULLETS = {
    PROJECT_FILE: ("guarantee — [decision NNNN](../docs/decisions/NNNN-title.md)", "`FakeX` — what it stands in for"),
}
BULLET = re.compile(r"^[ \t]*+[-*+][ \t]++(\S.*)$", re.M)
# The entries of the size checks: a list item (bullet or numbered), a table row and the rule line under a table's
# header, a heading; a code span and a Markdown link, which the prose words leave out.
LIST_ITEM = re.compile(r"^[ \t]*+(?:[-*+]|\d{1,9}[.)])[ \t]++(\S.*+)")
TABLE_ROW = re.compile(r"^[ \t]*+\|")
TABLE_RULE = re.compile(r"[ \t|:-]++")
HEADING = re.compile(r"(#{1,6})[ \t]++(.*+)")
CODE_SPAN = re.compile(r"`[^`\n]*+`")
LINK = re.compile(r"!?\[[^\]\n]*+\]\([^)\n]*+\)")
PROSE_WORD = re.compile(r"\w")

try:
    import yaml
except ImportError:  # the front matter is then read by parse_front_matter
    yaml = None
PARSE_ERRORS = (ValueError, yaml.YAMLError) if yaml else (ValueError,)


def read_text(path):
    with open(path, encoding="utf-8-sig") as handle:
        return handle.read().replace("\r\n", "\n")


def front_matter(path):
    text = read_text(path)
    if not text.startswith("---\n"):
        raise ValueError("no front matter")
    end = text.find("\n---", 4)
    if end < 0:
        raise ValueError("unterminated front matter")
    data = yaml.safe_load(text[4:end]) if yaml else parse_front_matter(text[4:end])
    if not isinstance(data, dict):
        raise ValueError("front matter is not a mapping")
    return data


FRONT_MATTER_KEY = re.compile(r"(\w[\w .-]*?):(?: ++(.*+))?$")
DOUBLE_QUOTED = re.compile(r"\"((?:[^\"\\]|\\.)*+)\"(?:[ \t]++#.*+)?")
SINGLE_QUOTED = re.compile(r"'((?:[^']|'')*+)'(?:[ \t]++#.*+)?")
BLOCK_HEADER = re.compile(r"[|>][1-9+-]{0,2}(?:[ \t]++#.*+)?")
COMMENT = re.compile(r"(?:^|[ \t])#.*$")
# Characters a plain value cannot start with, and `- ` (a list where a value belongs).
RESERVED_STARTS = ("@", "`", "%", ",", "]", "}", "- ")


def front_matter_entries(text):
    """(line number, key, first line of the value, the lines continuing it) of every top-level key."""
    entries = []
    for number, line in enumerate(text.split("\n"), start=1):
        if line[:1] in (" ", "\t") or line.startswith("- ") or not line.strip():
            if entries:
                entries[-1][3].append(line)
            elif line.strip():
                raise ValueError(f"line {number}: indented line before the first key")
        elif not line.startswith("#"):
            key = FRONT_MATTER_KEY.fullmatch(line)
            if not key:
                raise ValueError(f"line {number}: expected 'key: value'")
            entries.append((number, key.group(1), (key.group(2) or "").strip(), []))
    return entries


def parse_front_matter(text):
    """The front matter's top-level mapping without PyYAML: {key: value} for its `key: value` lines, a value continued
    on indented lines; a ValueError, with its line, for what PyYAML (and Claude Code) would not parse."""
    data = {}
    for number, name, first, rest in front_matter_entries(text):
        try:
            data[name] = scalar_value(first, rest)
        except ValueError as error:
            raise ValueError(f"line {number}: {error} ('{name}')") from None
    return data or None


def scalar_value(first, rest):
    """The value of a key from its first line and the lines continuing it."""
    if not first or first.startswith("#"):
        return "\n".join(line.strip() for line in rest if line.strip()) or None
    if first[0] in "|>":
        return block_scalar(first, rest)
    if first[0] in "\"'":
        return quoted_scalar(" ".join([first, *(line.strip() for line in rest if line.strip())]))
    if first[0] in "[{":
        return " ".join([first, *(line.strip() for line in rest)]).strip()
    if first.startswith(RESERVED_STARTS) or first == "-":
        raise ValueError(f"a value cannot start with '{first[0]}' unless it is quoted")
    return plain_scalar(first, rest)


def block_scalar(first, rest):
    """A `|` (literal) or `>` (folded) value from its indented lines."""
    if not BLOCK_HEADER.fullmatch(first):
        raise ValueError("a block scalar header ('|' or '>') takes no text on its line")
    lines = [line.strip() for line in rest]
    return ("\n" if first[0] == "|" else " ").join(lines).strip()


def quoted_scalar(text):
    """A double- or single-quoted value; text after its closing quote, other than a comment, is an error."""
    double = DOUBLE_QUOTED.fullmatch(text)
    if double:
        return json.loads(f'"{double.group(1)}"', strict=False)
    single = SINGLE_QUOTED.fullmatch(text)
    if single:
        return single.group(1).replace("''", "'")
    raise ValueError("unterminated quoted value, or text after its closing quote")


def plain_scalar(first, rest):
    """An unquoted value, continued on its indented lines; a `: ` or a final `:` in it is an error (YAML would read a
    mapping there: quote the value)."""
    lines = [COMMENT.sub("", line).strip() for line in [first, *rest]]
    text = " ".join(line for line in lines if line)
    if ": " in text or text.endswith(":"):
        raise ValueError("mapping values are not allowed here: quote a value that contains ': '")
    return text


def check(path, expected_name, errors, agent=False):
    try:
        data = front_matter(path)
    except PARSE_ERRORS as error:
        errors.append(f"{path}: {str(error).splitlines()[0]}")
        return
    for key in ("name", "description"):
        if not str(data.get(key) or "").strip():
            errors.append(f"{path}: missing '{key}'")
    if data.get("name") and data["name"] != expected_name:
        errors.append(f"{path}: name '{data['name']}' does not match '{expected_name}'")
    if agent and expected_name.startswith("squad-"):
        check_squad_agent(path, data, errors)


def check_squad_agent(path, data, errors):
    """A squad member names an alias as model and an explicit effort."""
    if str(data.get("model") or "") not in MODELS:
        errors.append(f"{path}: 'model' must be one of {', '.join(MODELS)} (an alias, so a model change rolls out "
                      "with the template)")
    if str(data.get("effort") or "") not in EFFORTS:
        errors.append(f"{path}: 'effort' must be set to one of {', '.join(EFFORTS)}")


def runs_script(group, script, tools):
    """Whether a hooks.PreToolUse group's matcher names every one of the tools and one of its hooks runs the script."""
    if not isinstance(group, dict) or not isinstance(group.get("hooks"), list):
        return False
    matcher = str(group.get("matcher", ""))
    return all(re.search(rf"\b{tool}\b", matcher) for tool in tools) and any(
        isinstance(hook, dict) and os.path.basename(script) in str(hook.get("command", "")) for hook in group["hooks"])


def declares_hook(data, script, tools):
    """True when the settings' hooks.PreToolUse has a group whose matcher names every one of the tools and whose
    command runs the script."""
    hooks = data.get("hooks")
    groups = hooks.get("PreToolUse") if isinstance(hooks, dict) else None
    return isinstance(groups, list) and any(runs_script(group, script, tools) for group in groups)


def check_guards(errors):
    """The members' guards exist and the project settings run each one before the tool calls it checks."""
    for script, _ in SQUAD_GUARDS:
        if not os.path.isfile(script):
            errors.append(f"{script} is missing (written by adopt-template)")
    try:
        with open(CLAUDE_SETTINGS, encoding="utf-8-sig") as handle:
            settings = json.load(handle)
    except (OSError, ValueError) as error:
        errors.append(f"{CLAUDE_SETTINGS} cannot be read ({error}); a refresh with adopt-template adds the squad's hooks")
        return
    for script, tools in SQUAD_GUARDS:
        if not isinstance(settings, dict) or not declares_hook(settings, script, tools):
            errors.append(f"{CLAUDE_SETTINGS}: no PreToolUse hook for {'|'.join(tools)} running {script} (it keeps the "
                          "squad members to the Git and file rules of .squad/team.md; a refresh with adopt-template "
                          "adds it)")


def check_skills(errors):
    skills = []
    for path in sorted(glob.glob(os.path.join(SKILLS_DIR, "*", "SKILL.md"))):
        name = os.path.basename(os.path.dirname(path))
        check(path, name, errors)
        skills.append(name)
    return skills


def instruction_body(path):
    """An instruction file from its first `## ` heading on, without Claude Code import lines."""
    text = read_text(path)
    start = text.find("\n## ")
    return IMPORT_LINE.sub("", text[start:]) if start >= 0 else ""


def check_instructions(errors):
    missing = [path for path in INSTRUCTION_FILES if not os.path.isfile(path)]
    for path in missing:
        errors.append(f"{path} is missing (written by adopt-template)")
    if missing:
        return
    reference, mirror = INSTRUCTION_FILES
    if instruction_body(mirror) != instruction_body(reference):
        errors.append(f"{mirror} differs from {reference} after the first '## ' heading - make the same change in both")
    if not os.path.isfile(CLAUDE_FILE):
        errors.append(f"{CLAUDE_FILE} is missing (written by adopt-template)")
    elif not RULES_IMPORT.search(read_text(CLAUDE_FILE)):
        errors.append(f"{CLAUDE_FILE} does not import {reference} ('@{reference}' on a line of its own) - "
                      "refresh the squad with adopt-template")


def check_template_record(errors):
    path = os.path.join(SQUAD_DIR, "template.json")
    try:
        with open(path, encoding="utf-8") as handle:
            record = json.load(handle)
    except (OSError, ValueError) as error:
        errors.append(f"{path}: {error} (written by adopt-template)")
        return
    if not isinstance(record, dict) or not str(record.get("repository") or "").strip():
        errors.append(f"{path}: no 'repository' - refresh the squad with adopt-template")
        return
    check_profiles(record, errors)


def check_profiles(record, errors):
    path = os.path.join(SQUAD_DIR, "template.json")
    profiles = record.get("profiles")
    if profiles is None:
        if record.get("additionalProfiles"):
            errors.append(f"{path}: 'additionalProfiles' is not read - refresh the squad with adopt-template, "
                          "which records 'profiles'")
        return
    if (not isinstance(profiles, list) or not profiles or len(set(profiles)) != len(profiles)
            or not all(isinstance(p, str) and re.fullmatch(r"[\w-]+", p) for p in profiles)):
        errors.append(f"{path}: 'profiles' must be a list of distinct profile names")
        return
    if record.get("profile") != profiles[0]:
        errors.append(f"{path}: 'profile' must be the first entry of 'profiles' ({profiles[0]})")
    if len(profiles) == 1:
        return
    for rel in [f for p in profiles for f in (os.path.join(SQUAD_DIR, "tools", f"analyzer-check-{p}.py"),
                                              os.path.join(CLAUDE_DIR, "hooks", f"session-start-{p}.sh"))] + \
            [os.path.join(SQUAD_DIR, "tools", "analyzer-check.py"), os.path.join(CLAUDE_DIR, "hooks", "session-start.sh")]:
        if not os.path.isfile(rel):
            errors.append(f"{rel} is missing (written by adopt-template for the profiles {', '.join(profiles)})")
    try:
        reports = getattr(load_settings(), "COVERAGE_REPORTS", None)
    except Exception:  # noqa: BLE001  (check_settings_file reports a file that does not load)
        return
    if not isinstance(reports, (list, tuple)) or len(reports) < len(profiles):
        errors.append(f"{SQUAD_DIR}/tools/squad_settings.py: COVERAGE_REPORTS needs a (format, glob) entry for "
                      f"each of the {len(profiles)} profiles")


def load_settings():
    """The repository's `.squad/tools/squad_settings.py` as a module, loaded from its path each time; raises what
    loading it raises."""
    sys.dont_write_bytecode = True
    spec = importlib.util.spec_from_file_location("squad_settings", SETTINGS_FILE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


def check_settings_file(errors):
    """`.squad/tools/squad_settings.py` loads; every gate imports it."""
    if not os.path.isfile(SETTINGS_FILE):
        return  # reported as a missing required file
    try:
        load_settings()
    except Exception as error:  # noqa: BLE001  (any failure to load the settings is the finding)
        errors.append(f"{SETTINGS_FILE}: does not load ({type(error).__name__}: {error})")


def setting(name, default):
    """A value of `squad_settings.py`, or the default when the file lacks it, is missing or does not load (that is
    check_settings_file's finding)."""
    try:
        return getattr(load_settings(), name, default)
    except Exception:  # noqa: BLE001  (a missing or broken settings file leaves the default)
        return default


def git_text(*args):
    """The output of a read-only git command, or None when it fails (no repository, no such ref or file)."""
    try:
        done = subprocess.run(["git", *args], capture_output=True, text=True, encoding="utf-8", check=False)
    except OSError:
        return None
    return done.stdout if done.returncode == 0 else None


def base_text(path):
    """The file's text at the merge base of BASE_REF and HEAD; None without a merge base or when the file is new."""
    base = (git_text("merge-base", setting("BASE_REF", "origin/main"), "HEAD") or "").strip()
    if not base:
        return None
    text = git_text("show", f"{base}:{path.replace(os.sep, '/')}")
    return text.replace("\r\n", "\n") if text is not None else None


def word_count(text):
    """Words split at white space, the count `--project` prints (`wc -w` differs: a stand-alone dash, Censio#156)."""
    return len(text.split())


def prose_words(text):
    """The words of an entry outside its code spans and Markdown links (a table row's pipes are no words)."""
    prose = LINK.sub(" ", CODE_SPAN.sub(" ", text))
    return sum(1 for word in prose.split() if PROSE_WORD.search(word))


def entry_start(line):
    """(kind, text) of a line that starts an entry: a bullet, a numbered item or a table row (not a table's rule
    line); None for any other line."""
    item = LIST_ITEM.match(line)
    if item:
        return "item", item.group(1)
    if TABLE_ROW.match(line) and not TABLE_RULE.fullmatch(line):
        return "row", line.strip()
    return None


def entries(text):
    """[section heading, kind, text] of every entry of a Markdown text; a list item's continuation lines belong to it,
    fenced code blocks and HTML comment lines hold none."""
    found, section, current, fenced = [], "", None, False
    for line in text.split("\n"):
        if line.lstrip().startswith(("```", "~~~")):
            fenced, current = not fenced, None
            continue
        heading = None if fenced else HEADING.match(line)
        start = None if fenced or heading else entry_start(line)
        if heading:
            section, current = heading.group(2).strip(), None
        elif start:
            current = [section, *start]
            found.append(current)
        elif current and current[1] == "item" and line.strip() and not line.lstrip().startswith("<!--"):
            current[2] += " " + line.strip()
        else:
            current = None
    return found


def double_row(kind, entry):
    """True for an entry of *Test doubles* that names a double: a table row, or a list item that starts with a code
    span or a bold label (Seluna: `**GitHub API double** — …`); the section's rule bullets, plain text, are no double
    rows (Vandox keeps rules there)."""
    return kind == "row" or entry.lstrip().startswith(("`", "**"))


def check_entries(path, text, warnings, keep=None, before=None):
    """Every entry of `text` (the file at `path`) with more prose words than PROJECT_ENTRY_MAX_PROSE; with `keep`,
    only the entries it accepts (kind, text); with `before`, only the entries not in that text verbatim (the ones the
    branch writes)."""
    limit = setting("PROJECT_ENTRY_MAX_PROSE", PROJECT_ENTRY_MAX_PROSE)
    old = {" ".join(entry.split()) for _, _, entry in entries(before)} if before is not None else set()
    for section, kind, entry in entries(text):
        if (keep and not keep(kind, entry)) or " ".join(entry.split()) in old:
            continue
        words = prose_words(entry)
        if words > limit:
            first = " ".join(entry.strip("|-*+ ").split()[:8])
            warnings.append(f"{path} ({section or 'before the first heading'}, \"{first} …\"): {words} prose words in "
                            f"one entry (PROJECT_ENTRY_MAX_PROSE {limit}) - make it a pointer: name the files, types or "
                            "endpoints and link the area document or decision record, which state the behavior, "
                            "values and limits")


def check_project_size(warnings, new_only=False, counts=None):
    """`.squad/project.md`: every entry past PROJECT_ENTRY_MAX_PROSE prose words (with `new_only`, only those the branch
    writes), and the words the branch adds past PROJECT_MAX_GROWTH; `counts` gets its words and growth."""
    if not os.path.isfile(PROJECT_FILE):
        return
    text = read_text(PROJECT_FILE)
    before = base_text(PROJECT_FILE)
    check_entries(PROJECT_FILE, text, warnings, before=before if new_only else None)
    if counts is not None:
        counts.append(size_line(PROJECT_FILE, word_count(text), None if before is None else word_count(before)))
    limit = setting("PROJECT_MAX_GROWTH", PROJECT_MAX_GROWTH)
    if before is not None and word_count(text) - word_count(before) > limit:
        warnings.append(f"{PROJECT_FILE}: gained {word_count(text) - word_count(before)} words against the merge base "
                        f"(PROJECT_MAX_GROWTH {limit}), and five members read it on every launch - write the new "
                        "entries as pointers: one line per security area, guarantee or coupling point that names the "
                        "files and links its area document or decision record")


def doubles_text(text):
    """The *Test doubles* sections of a Markdown text, each to the next heading of its level or a higher one (a
    repository with several profiles has one per profile), or None without one."""
    lines, level = [], None
    for line in text.split("\n"):
        heading = HEADING.match(line)
        if heading and (level is None or len(heading.group(1)) <= level):
            level = len(heading.group(1)) if heading.group(2).strip().startswith(TEST_DOUBLES) else None
        if level is not None:
            lines.append(line)
    return "\n".join(lines) if lines else None


def doubles_before():
    """(words, text) of the test doubles at the merge base: the sections of the test document and of
    `.squad/project.md` added up, so the move from one to the other is no growth (a test document can have a section
    of its own already, Vandox); None without a merge base or when neither file is there."""
    tests, project = base_text(UNIT_TESTS_FILE), base_text(PROJECT_FILE)
    if tests is None and project is None:
        return None
    sections = "\n".join(filter(None, (doubles_text(tests or ""), doubles_text(project or ""))))
    return word_count(sections), sections


def size_line(name, now, before):
    """The words of a file or section now and its growth against the merge base, as `--project` prints them."""
    growth = "no merge base" if before is None else f"{now - before:+d} against the merge base"
    return f"{name}: {now} words ({growth})"


def check_test_doubles(warnings, new_only=False, counts=None):
    """A *Test doubles* section left in `.squad/project.md`; in the test document's, every double row past
    PROJECT_ENTRY_MAX_PROSE prose words (with `new_only`, only those the branch writes) and the words the branch adds
    past TEST_DOUBLES_MAX_GROWTH; `counts` gets the section's words and growth."""
    if os.path.isfile(PROJECT_FILE) and doubles_text(read_text(PROJECT_FILE)) is not None:
        warnings.append(f"{PROJECT_FILE}: has a *{TEST_DOUBLES}* section - move it verbatim to {UNIT_TESTS_FILE}, where "
                        "the Tester keeps it (a refresh does: adopt-template step 1)")
    section = doubles_text(read_text(UNIT_TESTS_FILE)) if os.path.isfile(UNIT_TESTS_FILE) else None
    if section is None:
        return
    before, old_text = doubles_before() or (None, "")
    check_entries(UNIT_TESTS_FILE, section, warnings, keep=double_row, before=old_text if new_only else None)
    if counts is not None:
        counts.append(size_line(f"{UNIT_TESTS_FILE} (*{TEST_DOUBLES}*)", word_count(section), before))
    limit = setting("TEST_DOUBLES_MAX_GROWTH", TEST_DOUBLES_MAX_GROWTH)
    if before is not None and word_count(section) - before > limit:
        warnings.append(f"{UNIT_TESTS_FILE} (*{TEST_DOUBLES}*): gained {word_count(section) - before} words against the "
                        f"merge base (TEST_DOUBLES_MAX_GROWTH {limit}) - one line per double: where it lives and what it "
                        "stands in for; how it behaves is read in its code")


def check_project_files(errors):
    for path in REQUIRED_FILES:
        if not os.path.isfile(path):
            errors.append(f"{path} is missing (seeded by adopt-template)")
    paths = sorted({p for pattern in PLACEHOLDER_GLOBS for p in glob.glob(pattern, recursive=True)
                    if os.path.isfile(p)})
    for path in paths:
        text = read_text(path)
        for match in PLACEHOLDER.finditer(text):
            line = text.count("\n", 0, match.start()) + 1
            first = " ".join(match.group(1).split())[:60]
            errors.append(f"{path}:{line}: template placeholder '{{{{TODO: {first}}}}}' not filled in")
    for path in SEEDED_BULLETS.keys() | EARLIER_SEED_BULLETS.keys():
        bullets = SEEDED_BULLETS.get(path, ()) + EARLIER_SEED_BULLETS.get(path, ())
        if os.path.isfile(path):
            check_seeded_bullets(path, {bullet_key(bullet) for bullet in bullets}, errors)


def bullet_key(text):
    """A bullet's text as it is compared with the seed's: no `TODO:` prefix, Markdown emphasis, code marks or final
    punctuation, dashes and whitespace unified, case ignored."""
    text = re.sub(r"[`*]", "", text).replace("\u2014", "-").replace("\u2013", "-")
    text = " ".join(text.split()).strip(" .;:,").casefold()
    return text.removeprefix("todo:").strip()


def check_seeded_bullets(path, seeded, errors):
    """A bullet of the seeded file that still reads like the seed's placeholder, its braces removed."""
    text = read_text(path)
    for match in BULLET.finditer(text):
        if bullet_key(match.group(1)) in seeded:
            line = text.count("\n", 0, match.start()) + 1
            shown = " ".join(match.group(1).split())[:60]
            errors.append(f"{path}:{line}: '{shown}' is the seed's placeholder text without its braces - describe "
                          "this project instead")


ERROR_PREFIX = "ERROR: "


WARNING_PREFIX = "WARNING: "


def check_decisions(errors, warnings):
    script = os.path.join(SQUAD_DIR, "tools", "decision-check.py")
    if not os.path.isfile(script):
        errors.append(f"{script} is missing (written by adopt-template)")
        return
    result = subprocess.run([sys.executable, script], capture_output=True, text=True, encoding="utf-8")
    findings = [line[len(ERROR_PREFIX):] for line in result.stdout.splitlines() if line.startswith(ERROR_PREFIX)]
    errors.extend(findings)
    warnings.extend(line[len(WARNING_PREFIX):] for line in result.stdout.splitlines() if line.startswith(WARNING_PREFIX))
    if result.returncode != 0 and not findings:
        errors.append(f"{script} failed: {result.stderr.strip() or 'no output'}")


def project_only():
    """`--project`: print the words of `.squad/project.md` and of the test doubles with their growth against the merge
    base, and the warnings about what the branch writes there (an older entry is left to the full run); always exit
    code 0."""
    warnings, counts = [], []
    check_project_size(warnings, new_only=True, counts=counts)
    check_test_doubles(warnings, new_only=True, counts=counts)
    for line in counts:
        print(line)
    for warning in warnings:
        print(f"{WARNING_PREFIX}{warning}")
    print(f"Checked {PROJECT_FILE} and the {TEST_DOUBLES} of {UNIT_TESTS_FILE}: {len(warnings)} warning(s)")
    return 0


def main(argv=None):
    argv = sys.argv[1:] if argv is None else argv
    # Resolve paths from the repository root, whatever the current directory is.
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
    if "--project" in argv:
        return project_only()
    errors, warnings = [], []
    agents = sorted(glob.glob(os.path.join(AGENTS_DIR, "*.md")))
    for path in agents:
        check(path, os.path.splitext(os.path.basename(path))[0], errors, agent=True)
    if agents:
        check_guards(errors)
    skills = check_skills(errors)
    check_instructions(errors)
    check_template_record(errors)
    check_project_files(errors)
    check_settings_file(errors)
    check_project_size(warnings)
    check_test_doubles(warnings)
    check_decisions(errors, warnings)

    if not agents or not skills:
        errors.append("no agents or skills found - has the squad been adopted in this repository?")
    for warning in warnings:
        print(f"{WARNING_PREFIX}{warning}")
    for error in errors:
        print(error)
    parser = "" if yaml else " (front matter read without PyYAML)"
    print(f"\nChecked {len(agents)} agents and {len(skills)} skills{parser}: {'PASS' if not errors else 'FAIL'}")
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main())
