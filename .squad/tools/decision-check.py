#!/usr/bin/env python3
"""Decision-record gate: the records in `docs/decisions/` must be consistent and released records frozen.

Checks, run from anywhere inside the repository (it takes no arguments):

- every `docs/decisions/NNNN-title.md` has a valid `Status` (`Proposed`, `Accepted` or `Superseded by NNNN`)
  and a `Supersedes` field naming existing records;
- the index in `docs/decisions/README.md` has exactly one row per record, with the same status (the number
  in a row, and the record in a `Superseded by` status, may be written as a link, `[NNNN](NNNN-title.md)`);
- `Superseded by NNNN` names an existing record that lists this one under `Supersedes`;
- no two records share a number, and every relative link from a record or the index to a record file
  (`[0003](0003-title.md)`, `../decisions/0003-title.md#why`) names a file that exists, so a renumbered or
  renamed record leaves no stale link behind;
- once `docs/areas/README.md` lists areas, every area links an existing document in `docs/areas/` and every
  record has an `Area:` field naming a listed area, or `—` for a record about no area (a repository without
  areas is not checked for this). A released record without the field, which can no longer be edited, takes
  its area from a `## Area assignment` table (`| Record | Area |`) in a later record instead; it fails when no
  table names it, two tables disagree or the area is not listed. A table naming a record that has its own
  `Area:` field, or no record at all, fails as well;
- a record is *released* once the commit that added it is contained in a release tag (`v*`). A released
  record is never deleted and never changed except for its status becoming `Superseded by NNNN`; an
  unreleased record is never `Superseded` (it is edited in place or deleted instead).

`docs/decisions/process.md` holds the squad's process decisions; the template overwrites it on every refresh, so it
is no record and is not checked: it needs no number, status or index row, and a link to it is no link to a record.

Size warnings, which do not fail the check and name the split each file needs: an unreleased record past
`RECORD_MAX_WORDS` words or `RECORD_MAX_OPTIONS` options under *Options considered* holds more than one decision; an
area document in `docs/areas/` past `AREA_MAX_WORDS` words covers more than one area. When the release of the records
cannot be told, the records past the limits are one warning that names their numbers and says they may all be
released, which exempts them. The limits come from `.squad/tools/squad_settings.py` (defaults 1500, 8 and 5000).

Without release tags nothing is released and the freeze checks pass. Without local ones but with a remote
configured, the remotes are asked (`git ls-remote --tags <remote> 'v*'`): when none has a release tag either,
nothing is released; when one has, or cannot be asked, the freeze checks are skipped with a warning, because the
tags may simply not be fetched. Without Git history (not a Git repository, shallow clone) the freeze checks are
skipped with a warning; a shallow clone's names its fix, `git fetch --unshallow --tags`. With `--require-history`
(`gate-stamp.py` passes it) every such skip is an error instead: a gate must not pass a released record it could not
check, which a shallow clone of a session otherwise lets through.

Usage, from anywhere inside the repository:
    python3 .squad/tools/decision-check.py [--require-history]

Exit code 0 when everything is valid, 1 otherwise.
"""
import argparse
import glob
import os
import re
import subprocess
import sys

sys.dont_write_bytecode = True  # importing the settings must leave no __pycache__ in the repository
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
try:
    import squad_settings as settings  # per-repository settings next to this script
# Missing outside a target, or broken: the defaults apply, config-check.py reports it.
except Exception:  # noqa: BLE001
    settings = None

AREA_MAX_WORDS = getattr(settings, "AREA_MAX_WORDS", 5000)
RECORD_MAX_WORDS = getattr(settings, "RECORD_MAX_WORDS", 1500)
RECORD_MAX_OPTIONS = getattr(settings, "RECORD_MAX_OPTIONS", 8)
DECISIONS_DIR = os.path.join("docs", "decisions")
AREAS_DIR = os.path.join("docs", "areas")
NO_AREA = "\u2014"
AREA_FIELD = "'- **Area:**' line"
RECORD_NAME = re.compile(r"^(\d{4})-.+\.md$")
STATUS_LINE = re.compile(r"^- \*\*Status:\*\*(.*)$", re.MULTILINE)
SUPERSEDES_LINE = re.compile(r"^- \*\*Supersedes:\*\*(.*)$", re.MULTILINE)
AREA_LINE = re.compile(r"^- \*\*Area:\*\*(.*)$", re.MULTILINE)
AREA_INDEX_BLOCK = re.compile(r"<!-- project:begin area-index -->(.*?)<!-- project:end area-index -->", re.DOTALL)
ASSIGNMENT_SECTION = re.compile(r"^## Area assignment[ \t]*\n((?:(?!^## ).)*)", re.MULTILINE | re.DOTALL)
AREA_LINK = re.compile(r"^\[([^\]]+)\]\(([^)]+\.md)\)$")
SUPERSEDED_BY = re.compile(r"^Superseded by (\d{4})$")
NUMBER = re.compile(r"\b(\d{4})\b")
INDEX_BLOCK = re.compile(r"<!-- project:begin index -->(.*?)<!-- project:end index -->", re.DOTALL)
INDEX_NUMBER = re.compile(r"^\d{4}$")
RECORD_LINK = re.compile(r"\[(\d{4})\]\([^)]*\)")
LINK_TARGET = re.compile(r"\]\(<?([^)\s>]+)>?(?:\s+\"[^\"]*\")?\)")
RELEASE_TAGS = "v*"
FETCH_TAGS = "run 'git fetch origin main --tags'"
UNSHALLOW = "run 'git fetch --unshallow --tags' and check again"
REMOTE_TIMEOUT = 30
INDEX_FILE = "README.md"
TEMPLATE_FILE = "_template.md"
OPTIONS_SECTION = re.compile(r"^## Options considered[ \t]*\n((?:(?!^## ).)*)", re.MULTILINE | re.DOTALL)
OPTION_ITEM = re.compile(r"^(?:\d+[.)]|[-*+])[ \t]", re.MULTILINE)


def plain(text):
    """`[NNNN](NNNN-title.md)` written as `NNNN`, so a linked number or status compares like a bare one."""
    return RECORD_LINK.sub(r"\1", text).strip()


def read_text(path):
    with open(path, encoding="utf-8-sig") as handle:
        return handle.read().replace("\r\n", "\n")


def git(root, *args):
    result = subprocess.run(["git", "-C", root, *args], capture_output=True, text=True, encoding="utf-8")
    return result.stdout if result.returncode == 0 else None


def load_records(root, errors):
    records = {}
    for path in sorted(glob.glob(os.path.join(root, DECISIONS_DIR, "*.md"))):
        name = os.path.basename(path)
        match = RECORD_NAME.match(name)
        if not match:
            continue  # README.md, _template.md and process.md (the squad's process decisions) are no records
        number, text = match.group(1), read_text(path)
        rel = f"{DECISIONS_DIR}/{name}".replace(os.sep, "/")
        status = STATUS_LINE.search(text)
        supersedes = SUPERSEDES_LINE.search(text)
        area = AREA_LINE.search(text)
        if not status:
            errors.append(f"{rel}: no '- **Status:**' line")
            continue
        status = plain(status.group(1))
        if status not in ("Proposed", "Accepted") and not SUPERSEDED_BY.match(status):
            errors.append(f"{rel}: status '{status}' must be Proposed, Accepted or Superseded by NNNN")
        if number in records:
            errors.append(f"{rel}: number {number} is used twice")
        records[number] = {"rel": rel, "status": status, "text": text,
                           "area": area.group(1).strip() if area else None,
                           "assigns": parse_assignments(text),
                           "supersedes": list(dict.fromkeys(NUMBER.findall(plain(supersedes.group(1)))))
                           if supersedes else []}
    return records


def parse_assignments(text):
    """(number, area) for every `| NNNN | Area |` row of the record's `## Area assignment` section."""
    section = ASSIGNMENT_SECTION.search(text)
    rows = []
    for line in section.group(1).splitlines() if section else []:
        cells = [plain(cell) for cell in line.strip().split("|")]
        if len(cells) == 4 and not cells[0] and not cells[-1] and INDEX_NUMBER.match(cells[1]):
            rows.append((cells[1], cells[2]))
    return rows


def check_links(records, errors):
    for number, record in records.items():
        for older in record["supersedes"]:
            if older not in records:
                errors.append(f"{record['rel']}: Supersedes {older}, which does not exist")
        match = SUPERSEDED_BY.match(record["status"])
        if not match:
            continue
        newer = records.get(match.group(1))
        if newer is None:
            errors.append(f"{record['rel']}: Superseded by {match.group(1)}, which does not exist")
        elif number not in newer["supersedes"]:
            errors.append(f"{record['rel']}: Superseded by {match.group(1)}, but {newer['rel']} does not list "
                          f"{number} under Supersedes")


def check_record_links(root, records, errors):
    """Every relative Markdown link from a record or the index to a record file must name an existing file."""
    decisions = os.path.join(root, DECISIONS_DIR)
    sources = [(record["rel"], record["text"]) for record in records.values()]
    index = os.path.join(decisions, INDEX_FILE)
    if os.path.isfile(index):
        sources.append((f"{DECISIONS_DIR}/{INDEX_FILE}".replace(os.sep, "/"), read_text(index)))
    for rel, text in sources:
        for target in LINK_TARGET.findall(text):
            path = target.split("#", 1)[0]
            if not path or "://" in path or path.startswith("mailto:") or not RECORD_NAME.match(os.path.basename(path)):
                continue
            base = root if path.startswith("/") else decisions
            if not os.path.isfile(os.path.normpath(os.path.join(base, path.lstrip("/")))):
                errors.append(f"{rel}: links {target}, which does not exist")


def parse_index_row(line):
    """(number, status) of a `| NNNN | Title | Status | Date |` row, or None for any other line. The title may
    contain pipes, so the status and the date are taken from the end of the row."""
    cells = [plain(cell) for cell in line.strip().split("|")]
    if len(cells) < 6 or cells[0] or cells[-1] or not INDEX_NUMBER.match(cells[1]):
        return None
    return cells[1], cells[-3]


def check_index(root, records, errors):
    path = os.path.join(root, DECISIONS_DIR, INDEX_FILE)
    if not os.path.isfile(path):
        if records:
            errors.append(f"{DECISIONS_DIR}/README.md is missing")
        return
    block = INDEX_BLOCK.search(read_text(path))
    if not block:
        errors.append(f"{DECISIONS_DIR}/README.md: no 'project:begin index' block")
        return
    rows = {}
    for line in block.group(1).splitlines():
        row = parse_index_row(line)
        if row:
            if row[0] in rows:
                errors.append(f"{DECISIONS_DIR}/README.md: index lists {row[0]} twice")
            rows[row[0]] = row[1]
    compare_index(rows, records, errors)


def compare_index(rows, records, errors):
    for number, record in records.items():
        if number not in rows:
            errors.append(f"{record['rel']}: missing in the index of {DECISIONS_DIR}/README.md")
        elif rows[number] != record["status"]:
            errors.append(f"{record['rel']}: index says '{rows[number]}', the record says '{record['status']}'")
    for number in rows:
        if number not in records:
            errors.append(f"{DECISIONS_DIR}/README.md: index row {number} has no record file")


def parse_area_row(line):
    """(name, document) of a `| [Name](slug.md) | Scope | Not here |` row, (name, None) when the first cell is
    no link to a Markdown file, or None for the header, the separator and any other line."""
    cells = [cell.strip() for cell in line.strip().split("|")]
    if len(cells) < 5 or cells[0] or cells[-1] or not cells[1] or set(cells[1]) <= set("-: ") \
            or cells[1] == "Area":
        return None
    link = AREA_LINK.match(cells[1])
    return (link.group(1), link.group(2)) if link else (cells[1], None)


def load_areas(root, errors):
    """Area name -> document for the areas `docs/areas/README.md` lists; empty without a list."""
    path = os.path.join(root, AREAS_DIR, INDEX_FILE)
    block = AREA_INDEX_BLOCK.search(read_text(path)) if os.path.isfile(path) else None
    areas = {}
    for line in block.group(1).splitlines() if block else []:
        row = parse_area_row(line)
        if row is None:
            continue
        name, document = row
        if document is None:
            errors.append(f"{AREAS_DIR}/README.md: area '{name}' must link its document, [name](slug.md)")
        elif not os.path.isfile(os.path.join(root, AREAS_DIR, document)):
            errors.append(f"{AREAS_DIR}/README.md: area '{name}' links {document}, which does not exist")
        areas[name] = document
    return areas


def collect_assignments(records, errors):
    """Number -> {area: rel of each record whose `## Area assignment` table gives it that area}."""
    assigned = {}
    for record in records.values():
        for number, area in record["assigns"]:
            target = records.get(number)
            if target is None:
                errors.append(f"{record['rel']}: Area assignment names {number}, which does not exist")
            elif target["area"] is not None:
                errors.append(f"{record['rel']}: Area assignment names {number}, which has its own "
                              f"{AREA_FIELD}")
            else:
                assigned.setdefault(number, {}).setdefault(area, []).append(record["rel"])
    return assigned


def assigned_area(number, record, assignments, released, errors):
    """The area a released record without `Area:` takes from an `## Area assignment` table, or None."""
    rel = record["rel"]
    if released is not None and number not in released:
        errors.append(f"{rel}: no {AREA_FIELD} (an area from {AREAS_DIR}/README.md, or {NO_AREA} for a "
                      f"record about no area)")
        return None
    if released is None and not assignments:
        errors.append(f"{rel}: possibly released (release tags unknown: a remote has tags that are not fetched or "
                      f"cannot be asked, or no history); add an {AREA_FIELD} (an area from {AREAS_DIR}/README.md, or "
                      f"{NO_AREA}) if it is unreleased")
        return None
    if not assignments:
        errors.append(f"{rel}: released without a {AREA_FIELD} and no '## Area assignment' table in a "
                      f"later record names it")
        return None
    if len(assignments) > 1:
        sources = "; ".join(f"'{area}' in {', '.join(rels)}" for area, rels in assignments.items())
        errors.append(f"{rel}: Area assignment tables disagree: {sources}")
        return None
    return next(iter(assignments))


def check_areas(root, records, released, errors):
    """`released` is the set of released record numbers, or None when it is unknown (no Git
    history, or release tags not fetched)."""
    areas = load_areas(root, errors)
    if not areas:
        return
    assigned = collect_assignments(records, errors)
    for number, record in records.items():
        area = record["area"]
        if area is None:
            area = assigned_area(number, record, assigned.get(number, {}), released, errors)
        if area is not None and area != NO_AREA and area not in areas:
            errors.append(f"{record['rel']}: area '{area}' is not listed in {AREAS_DIR}/README.md")


def without_status(text):
    return STATUS_LINE.sub("- **Status:**", text, count=1)


def find_released(root, records, errors):
    """Number -> first release tag containing the commit that added the record. An unreleased record that is
    `Superseded` is reported."""
    released = {}
    for number, record in records.items():
        added = (git(root, "log", "--diff-filter=A", "--format=%H", "--", record["rel"]) or "").split()
        if not added:
            continue  # not committed yet
        containing = (git(root, "tag", "--contains", added[-1], "--list", RELEASE_TAGS) or "").split()
        if containing:
            released[number] = containing[0]
        elif SUPERSEDED_BY.match(record["status"]):
            errors.append(f"{record['rel']}: is not released (no '{RELEASE_TAGS}' tag contains it) and must not "
                          f"be 'Superseded' - edit the record in place or delete it")
    return released


def check_released_record(root, record, tag, errors):
    old = git(root, "show", f"{tag}:{record['rel']}")
    if old is None:
        return
    old = old.replace("\r\n", "\n").lstrip("\ufeff")
    if without_status(old) != without_status(record["text"]):
        errors.append(f"{record['rel']}: released in {tag} and changed since - only its status may change "
                      f"(supersede it with a new record instead)")
    old_status = STATUS_LINE.search(old)
    if old_status and old_status.group(1).strip() != record["status"] \
            and not SUPERSEDED_BY.match(record["status"]):
        errors.append(f"{record['rel']}: released in {tag}; its status may only change to 'Superseded by NNNN'")


def check_deleted(root, tags, records, errors):
    for tag in tags:
        listing = git(root, "ls-tree", "--name-only", f"{tag}:{DECISIONS_DIR.replace(os.sep, '/')}") or ""
        for name in listing.split():
            match = RECORD_NAME.match(name)
            if match and match.group(1) not in records:
                errors.append(f"{DECISIONS_DIR}/{name}: released in {tag} and deleted since - a released record "
                              f"is never deleted")


def remote_release_tags(root, remote):
    """The release tags `remote` has (`git ls-remote`), or None when it cannot be asked (offline, no access, no
    answer within REMOTE_TIMEOUT seconds); it never prompts for credentials."""
    env = dict(os.environ, GIT_TERMINAL_PROMPT="0")
    try:
        result = subprocess.run(["git", "-C", root, "ls-remote", "--tags", remote, RELEASE_TAGS], capture_output=True,
                                text=True, encoding="utf-8", env=env, timeout=REMOTE_TIMEOUT, check=False)
    except (OSError, subprocess.TimeoutExpired):
        return None
    return result.stdout.split() if result.returncode == 0 else None


def without_local_tags(root, warnings):
    """The released records when no local release tag exists: none when no remote has a release tag either (a
    repository before its first release), None with a warning when one has (not fetched) or cannot be asked."""
    for remote in (git(root, "remote") or "").split():
        tags = remote_release_tags(root, remote)
        if tags is None:
            warnings.append(f"no local '{RELEASE_TAGS}' tags and the remote '{remote}' could not be asked for its "
                            f"tags - released records are not checked ({FETCH_TAGS})")
            return None
        if tags:
            warnings.append(f"no local '{RELEASE_TAGS}' tags although the remote '{remote}' has some - released "
                            f"records are not checked ({FETCH_TAGS})")
            return None
    return set()


def check_freeze(root, records, errors, warnings):
    """Checks the released records and returns their numbers; None when that cannot be told."""
    if git(root, "rev-parse", "--git-dir") is None:
        warnings.append("not a Git repository - released records are not checked")
        return None
    tags = (git(root, "tag", "--list", RELEASE_TAGS) or "").split()
    if not tags:
        return without_local_tags(root, warnings)
    if (git(root, "rev-parse", "--is-shallow-repository") or "").strip() == "true":
        warnings.append(f"shallow clone - released records are not checked ({UNSHALLOW})")
        return None
    released = find_released(root, records, errors)
    for number, tag in released.items():
        check_released_record(root, records[number], tag, errors)
    check_deleted(root, tags, records, errors)
    return set(released)


def option_count(text):
    """The options a record weighs: the top-level list items of its `## Options considered` section."""
    section = OPTIONS_SECTION.search(text)
    return len(OPTION_ITEM.findall(section.group(1))) if section else 0


def check_record_sizes(records, released, warnings):
    """An unreleased record past RECORD_MAX_WORDS or RECORD_MAX_OPTIONS holds several decisions: a warning that
    names the split. `released` is the set of released numbers, or None when it cannot be told: then one summary line,
    since every record it names may be released and exempt."""
    oversized = []
    for number, record in records.items():
        if released is not None and number in released:
            continue  # append-only: a released record is never split
        words, options = len(record["text"].split()), option_count(record["text"])
        if words > RECORD_MAX_WORDS or options > RECORD_MAX_OPTIONS:
            oversized.append((number, record, words, options))
    limits = f"RECORD_MAX_WORDS {RECORD_MAX_WORDS}, RECORD_MAX_OPTIONS {RECORD_MAX_OPTIONS}"
    if released is None and oversized:
        warnings.append(f"{len(oversized)} record(s) past {limits} ({', '.join(item[0] for item in oversized)}), but "
                        "which records are released is not known (see the warning above): they may all be released, "
                        "and a released record is never split - do not split one or file an issue for them before "
                        "the release state is known; fetch the full history and tags and check again for the "
                        "unreleased ones to split")
        return
    for _, record, words, options in oversized:
        warnings.append(f"{record['rel']}: {words} words and {options} options ({limits}) - split it into one record "
                        "per decision: each keeps its own context, options and consequences, the new ones take the "
                        "next free numbers with their index rows, links to it point at the record of their decision, "
                        "and the behavior it states moves to its area document (docs/decisions/README.md, Rules)")


def check_area_sizes(root, warnings):
    """An area document past AREA_MAX_WORDS covers more than one area: a warning that names the split."""
    for path in sorted(glob.glob(os.path.join(root, AREAS_DIR, "*.md"))):
        name = os.path.basename(path)
        words = len(read_text(path).split())
        if name in (INDEX_FILE, TEMPLATE_FILE) or words <= AREA_MAX_WORDS:
            continue
        warnings.append(f"{AREAS_DIR}/{name}".replace(os.sep, "/") + f": {words} words (AREA_MAX_WORDS "
                        f"{AREA_MAX_WORDS}) - split it: keep this area for its core behavior, move each other part "
                        "to a narrower area with its own document and index row, link the new documents from its "
                        "*Not here* section, and state each behavior once (docs/areas/README.md, Size)")


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--require-history", action="store_true",
                        help="fail instead of warning when the released records cannot be checked")
    options = parser.parse_args(argv)
    root = (git(".", "rev-parse", "--show-toplevel") or ".").strip()
    errors, warnings = [], []
    records = load_records(root, errors)
    check_links(records, errors)
    check_record_links(root, records, errors)
    check_index(root, records, errors)
    freeze_warnings = []
    released = check_freeze(root, records, errors, freeze_warnings)
    (errors if options.require_history else warnings).extend(freeze_warnings)
    check_areas(root, records, released, errors)
    check_record_sizes(records, released, warnings)
    check_area_sizes(root, warnings)
    for warning in warnings:
        print(f"WARNING: {warning}")
    for error in errors:
        print(f"ERROR: {error}")
    print(f"\nChecked {len(records)} decision records: {'PASS' if not errors else 'FAIL'}")
    return 0 if not errors else 1


if __name__ == "__main__":
    sys.exit(main())
