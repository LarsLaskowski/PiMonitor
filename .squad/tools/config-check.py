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

A warning, which does not fail the check, names `.squad/project.md` when it is longer than `PROJECT_MAX_WORDS` words
(`squad_settings.py`, default 1500): five members read it on every launch, so its entries point to the area
documents and decision records instead of restating them. The warnings of `decision-check.py` (sizes of records and
area documents, release tags it cannot see) are shown as well.

The front matter is parsed with PyYAML where it is installed. Without it, a built-in parser reads the YAML that
agents and skills use (`key: value` lines, quoted values, block scalars, lists) and still rejects what makes
Claude Code drop a file: a `: ` or a final `:` in an unquoted value, an unterminated quote, text after a closing
quote, a value starting with a reserved character or `- `, a line that is no `key: value`.

Usage, from anywhere inside the repository:
    python3 .squad/tools/config-check.py

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
PROJECT_MAX_WORDS = 1500
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
    PROJECT_FILE: ("guarantee — [decision NNNN](../docs/decisions/NNNN-title.md)",),
}
BULLET = re.compile(r"^[ \t]*+[-*+][ \t]++(\S.*)$", re.M)

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


def check_project_size(warnings):
    """`.squad/project.md` past PROJECT_MAX_WORDS: a warning that says how to shorten it."""
    if not os.path.isfile(PROJECT_FILE):
        return
    try:
        limit = getattr(load_settings(), "PROJECT_MAX_WORDS", PROJECT_MAX_WORDS)
    except Exception:  # noqa: BLE001  (a missing or broken settings file leaves the default; check_settings_file reports it)
        limit = PROJECT_MAX_WORDS
    words = len(read_text(PROJECT_FILE).split())
    if words > limit:
        warnings.append(f"{PROJECT_FILE}: {words} words (PROJECT_MAX_WORDS {limit}), and five members read it on every "
                        "launch - make every entry a pointer: one line per security area, guarantee and test double "
                        "that names the files and links its area document or decision record; move behavior, values "
                        "and limits into the area document and the reasons into the record (its introduction)")


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


def main():
    # Resolve paths from the repository root, whatever the current directory is.
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", ".."))
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
