#!/usr/bin/env python3
"""Append one row to a squad working record's `log.md`, the way the pipeline wants it, and summarize what
each role cost over the run.

The orchestrator is the only writer of `log.md`; it records what the members report. This script keeps the
mechanics out of the orchestrator's hands:

- the row is `| <date> <time> | <step> | <member> | <result> |` (`2026-10-11 13:05`, UTC), appended after the
  last line, which gets its line ending first if it lacks one; rows of older records that carry only the date
  are still read;
- the line ending follows the file (CRLF when the file uses it), so rows never end up with mixed endings;
- a `|` in a cell is written as `\\|`, a line break in the result as `<br>` (a Markdown table cell has one line);
- a control character (Unicode categories Cc, Cf, Zl, Zp other than tab and line break) is written as its
  escape (`U+202E`), never as the character itself; a tab becomes a space;
- for a subagent launch in Claude Code, `--agent-id <id>` (the agent id the Agent tool returned) reads the
  launch from the agent's transcript (`session-usage.py`: the latest round of
  `<session>/subagents/agent-<id>.jsonl` of the session `CLAUDE_CODE_SESSION_ID`, or `--session`): the model and
  effort its responses ran on, the agent's tokens (its context at the end of the round, the figure a task
  notification reports), its tool uses and seconds, and its tokens per kind with their price-weighted figure. It
  appends a trailer `(opus/high · 37,445 tokens · 6 tool uses · 57 s · 31 in · 9,870 out · 612,400 cache read ·
  48,210 cache write · 280,527 weighted)`. The agent type comes from the transcript (`--agent`, if given, must
  match it); when the model or effort differs from the agent file's, or from the default override of
  `.squad/orchestration.md`, *Launch model* (read against the *Tier* line of the folder's `plan.md`), the row is
  written with what the launch ran on and a warning names the difference. An agent whose transcript holds more
  than one round was continued: its latest round is logged with `--continued` or `--continued-from`, a single
  round without them. A round logged late, after the agent was already continued (a Lead's `ESCALATE` answered by
  a continuation before its row was written), is logged with `--round N`, its N-th round: round 1 as the fresh
  launch, a later one with `--continued`;
- outside Claude Code (no transcript), the notification path: `--agent squad-<role>` (model and effort read from
  `.claude/agents/squad-<role>.md`, so nothing is typed from memory) or `--launch MODEL/EFFORT` (a launch that
  overrode the agent file; a default override of *Launch model* is refused with `--agent`), with
  `--tokens N --tool-uses N --seconds N` from the usage block of the launch's task notification, appends a trailer
  `(opus/high · 37,445 tokens · 6 tool uses · 57 s)` without tokens per kind. Every number is copied from the usage
  block of a task notification at hand, never estimated, remembered or taken from the member's hand-back; a
  notification whose summary says the agent waits on its own background work is interim (the final notification of
  the same task id counts, a repeated hand-back is ignored). The options below about pending, amended, replaced and
  unreported launches belong to this path;
- the first row replaces the template's heading `# Log: <issue #number | feature name>` with the work folder's
  (`# Log: issue #3`, `# Log: feature incidents-view`), so the posted record carries no placeholder;
- one row names one member: a member cell that lists two (`squad-reviewer / squad-security`, a comma, `and`)
  is refused, because the trailer of a parallel launch belongs to one row each, or `--amend-last` cannot tell
  them apart;
- a continued launch (`SendMessage` to the agent of an earlier round; `.squad/orchestration.md`, *Continuing a member*)
  reports the agent's tokens since its first launch, while its tool uses and its duration cover the round alone
  (all three are passed exactly as the task notification reports them): with `--continued` the script subtracts
  the tokens of that member's last fresh launch and of the continuations logged after it, keeps tool uses and
  seconds as reported and marks the trailer `· continued`; a reported token figure below the earlier total can
  only be the round alone and is kept as it is. When the continued agent is not the member's last fresh launch
  (the Dev's `skeleton` and `implement` agents alternate across task groups, or a fresh launch followed the cap of
  continuations), `--continued-from "<step>"` names the step cell
  of the fresh launch whose agent was continued: only that launch and its own continuations are subtracted, and
  the trailer records the step (`· continued from <step>`), so a later continuation of either agent is computed
  against the right rows. `--continued` needs an earlier row of that member with usage, `--continued-from` a
  fresh launch of that step. A continuation logged at another step number than the fresh launch it continues
  prints a warning, unless its step cell names a fix, delta or revise round: the first launch of another mode
  (`skeleton` to `implement`, `tests-first` to `coverage`, `approve-pr`, `decide`) is a fresh launch, not a
  continuation. `--continued` and `--continued-from` are only for continuing the same agent: a fresh launch of
  the same role at another step is logged without them. A continuation of an agent that was already continued
  five times or reported 300,000 tokens is logged with a warning, and the summary names it under *Past the
  continuation cap* (`.squad/orchestration.md`, *Continuing a member*: a fresh launch with a hand-over was due);
- the usage block often arrives only after the member's hand-back: `--amend-last MEMBER` with the same
  options adds the trailer to the last row of that member afterwards, so the row can be written as soon as
  the report is in. A row gets its trailer once (a pending one is completed); a second amend is refused. When a
  later row of that member exists already (two members that ran in parallel, and the first one's notification
  came last), `--step "<step>"` selects the last row of the member whose step cell is that text, so an earlier launch row
  is still amended with the tool, never by hand;
- a row whose trailer is wrong (a launch logged with made-up numbers before its notification arrived) is
  corrected with `--replace-last MEMBER` and the same options (`--step` selects an earlier row the same way,
  also when other rows of the member follow): the trailer of that row, a pending one too, is replaced, so the
  launch is still counted once. A notification repeated for one task id is no new launch: only the latest
  numbers of the id are logged, on the same row;
- a row whose result text is wrong — a member's verdict line logged in another form than `squad-next.py` reads — is
  corrected with `--replace-last MEMBER --new-result "TEXT"` (with `--step` for an earlier row, also when other rows
  of the member follow): the result text is replaced and the trailer kept, so the launch is not logged twice. A new
  row of the Reviewer, the Devil's Advocate, Security or the Lead whose result carries no verdict line `squad-next.py`
  can read prints a warning that names the exact line;
- a launch whose notification never arrived is marked with `--amend-last MEMBER --unreported --agent
  squad-<role>` (no numbers): the trailer `(opus/medium · usage not reported)` lets the summary count the
  launch, name it as unreported and exit 0, so the table is posted as the tool prints it instead of being
  summed by hand or left out. `--replace-last` with the real numbers turns it into a full trailer later;
- rows of the Orchestrator, the Product Manager and the User (an answer, a measurement the user ran) are
  not launches: write them with exactly the member cell `Orchestrator`, `Product Manager` or `User` (the
  spellings `squad-orchestrator` and `squad-user` are written as those cells) and no metric option, the
  summary does not ask for usage; a metric option, `--amend-last`, `--replace-last` or `--unreported` for such
  a member is refused, since no task notification exists for it;
- the metrics are what a launch really cost: fewer than 1,000 tokens (every launch loads its agent file and
  instructions first, so `--tokens 0` or `--tokens 1` can only be a placeholder) and negative numbers are
  refused, so a placeholder cannot be logged by accident. A row for a launch whose notification has not
  arrived yet is written with `--agent` (or `--launch`) alone: its trailer `(opus/medium · usage pending)` keeps
  model and effort, the summary lists it as incomplete, and `--amend-last` with the numbers completes it — the
  only way to add the numbers later. A continued launch is written the same way as soon as its report is in:
  `--agent` with `--continued` or `--continued-from "<step>"` and no numbers gives `(opus/medium · usage pending ·
  continued from <step>)`, and the amend with the numbers (with or without the continuation options) completes it
  as that continuation.

- the summary refuses a code head without a passing gate stamp: when the branch changes anything outside `specs/`,
  the log's last stamp row of `gate-stamp.py` (step 7, after the PR step 11) must have passed on a commit that holds the
  same code as HEAD (`gate-stamp.py`, `--verify` without the merge check), so a gate result from a member's report
  never stands in for the orchestrator's own run;
- the summary starts with the template version the run worked under, read from `.squad/template.json`:
  `Template: <repository> <release tag> (commit <sha>)` (`untagged` when the template was applied from a commit
  without a release tag), so `squad-report.py` can group the runs by it;
- the wrap-up's lessons row is an Orchestrator row at step 12 whose result names the URL of every lesson issue,
  or is exactly `no lessons` (`.squad/orchestration.md`, *Squad lessons*); the summary fails without one and, while
  there is none or it says `no lessons`, lists on standard error the lesson candidates the log shows: Lead
  `decide` launches, loop limits, launches whose usage was never reported, continuations past the cap or into
  another mode, and launches on another model or effort than their agent file or *Launch model* names;

- `--stdin` reads the log from standard input instead of `specs/<folder>/log.md`, for the rows after step 10
  of `squad-issue` removed the folder: pipe the log table of the "Squad working record" comment in, the result
  of the command goes to standard error and the whole updated log to standard output (paste it back into the
  comment); `--summary --stdin` prints the summary of that log, nothing is written to the repository. A piped
  table whose first row is of step 10 or later gets the rows of the removed `log.md` (in the commit before its
  removal) in front of it, so a comment too long for the whole log links `log.md` and holds only the rows from
  step 10 on; the output is still the piped table with its change, never the earlier rows;

Usage, from anywhere inside the repository:
    python3 .squad/tools/squad-log.py <work folder> <step> <member> <result> [--agent-id a1b2c3d4e5f6a7b8c]
    python3 .squad/tools/squad-log.py <work folder> "8 Review delta" Reviewer "VERDICT: APPROVE on <sha>"
        --agent-id a1b2c3d4e5f6a7b8c --continued
    python3 .squad/tools/squad-log.py <work folder> <step> <member> <result> [--agent squad-lead
        --tokens 37445 --tool-uses 6 --seconds 57]
    python3 .squad/tools/squad-log.py <work folder> --amend-last Lead --agent squad-lead --tokens 37445
        --tool-uses 6 --seconds 57
    python3 .squad/tools/squad-log.py <work folder> --replace-last Lead --agent squad-lead --tokens 37445
        --tool-uses 6 --seconds 57
    python3 .squad/tools/squad-log.py <work folder> --replace-last Reviewer --step "8 Review" --new-result
        "VERDICT: BLOCKING 0 | NON-BLOCKING 1 on <sha> — one naming finding"
    python3 .squad/tools/squad-log.py <work folder> --amend-last Security --step "3 Plan security" --agent
        squad-security --tokens 45751 --tool-uses 7 --seconds 108
    python3 .squad/tools/squad-log.py <work folder> --amend-last "Devil's Advocate" --unreported --agent
        squad-devils-advocate
    python3 .squad/tools/squad-log.py <work folder> "8 Review delta" Reviewer "VERDICT: APPROVE on <sha>"
        --agent squad-reviewer --tokens 139230 --tool-uses 4 --seconds 61 --continued
    python3 .squad/tools/squad-log.py <work folder> "7 Fix" Dev "fixed" --agent squad-dev --tokens 412000
        --tool-uses 9 --seconds 317 --continued-from "6 Implement (rows 9-14)"
    python3 .squad/tools/squad-log.py <work folder> "7 Fix" Dev "fixed" --agent squad-dev
        --continued-from "6 Implement (rows 9-14)"
    python3 .squad/tools/squad-log.py <work folder> --summary
    python3 .squad/tools/squad-log.py <work folder> "11 CI" Dev "fixed" --agent squad-dev --tokens 41200
        --tool-uses 9 --seconds 80 --stdin < log-from-comment.md > log-updated.md

`<work folder>` is the folder's name under `specs/` (`issue-12`, or `specs/issue-12`); it is matched against the
folders that exist there, never used as a path, so no argument reaches the file system; `--agent` is matched
against the files under `.claude/agents/` the same way. `<result>` may contain line breaks.
`--summary` prints a Markdown table per member (launches, with the continuations among them, tokens, tool
uses, seconds) with a total row, for the "Squad working record" comment and the wrap-up report, followed by an
**Orchestrator** table once two rows carry a time of day: the wall time from the first to the last such row,
the part of it no launch covers (an upper bound for the orchestrator's own work and the waiting between
launches; launches that ran in parallel count once, each placed by its row's time and its seconds) and the
number of gate runs (Orchestrator rows of step 7). Then, once a launch carries tokens per kind, a table of the
tokens per kind and the **weighted** figure per member (every token at its list price, `session-usage.py`), with
an Orchestrator row read from this session's own transcript from the first timed row on (the orchestrator's
share; "not provided" outside Claude Code) and each row's share of the weighted total; launches logged from task
notifications carry no tokens per kind and are named below it. Every row of a member other than the Orchestrator, the
Product Manager and the User is a launch: the summary ends with the launch rows that carry no usage yet, so the
orchestrator amends them before posting and an incomplete table is never posted as complete; launches marked
`--unreported` are counted and named below the table instead. Once step 10 has removed the work folder, the summary
also reads the commits after that removal (the fixes of step 11, merges of the base branch left out): the last of
them must be followed by a step-11 row of the Reviewer (the delta review of steps 7–8) or, for a verbatim
documentation fix, of the Orchestrator naming it verbatim, or the summary names the unreviewed commits on standard
error. Exit code 0 when the row was written, amended or
the summary is complete, 1 when the folder has no `log.md`, the agent file is unknown, `--agent-id` finds no
transcript of that agent in the session, no row can be amended or replaced, or `--continued` finds no earlier usage
of the member (`--continued-from`: no fresh launch of that step), 3 when the summary lists launch rows without
usage, 4 when commits after the pull request have no review row after them, 5 when no step-12 lessons row exists, 6
when the code head has no passing gate stamp.
"""
import argparse
import collections
import contextlib
import datetime
import importlib.util
import json
import os
import re
import subprocess
import sys
import tempfile
import unicodedata

CONTROL_CATEGORIES = {"Cc", "Cf", "Zl", "Zp"}
KEEP = {"\t", "\n", "\r"}
# The tokens per kind and the weighted figure a launch read from its transcript carries (`--agent-id`).
KINDS = (r"(?: · (?P<input>[\d,]+) in · (?P<output>[\d,]+) out · (?P<read>[\d,]+) cache read · "
         r"(?P<write>[\d,]+) cache write(?: · (?P<weighted>[\d,]+) weighted)?)?")
TRAILER = re.compile(r"\((?P<launch>[\w.-]+/[\w-]+) · (?P<tokens>[\d,]+) tokens · (?P<tools>\d+) tool uses · "
                     rf"(?P<seconds>\d+) s{KINDS}(?P<continued> · continued(?: from (?P<origin>.+))?)?\)\s*$")
UNREPORTED = "usage not reported"
UNREPORTED_TRAILER = re.compile(rf"\((?P<launch>[\w.-]+/[\w-]+) · {UNREPORTED}\)\s*$")
PENDING = "usage pending"
PENDING_TRAILER = re.compile(rf"\((?P<launch>[\w.-]+/[\w-]+) · {PENDING}"
                             r"(?P<continued> · continued(?: from (?P<origin>.+))?)?\)\s*$")
ANY_TRAILER = re.compile(rf"\((?P<launch>[\w.-]+/[\w-]+) · (?:{UNREPORTED}|{PENDING}(?: · continued(?: from .+)?)?|"
                         r"[\d,]+ tokens · \d+ tool uses · \d+ s(?: · [\d,]+ in · [\d,]+ out · [\d,]+ cache read · "
                         r"[\d,]+ cache write(?: · [\d,]+ weighted)?)?(?: · continued(?: from .+)?)?)\)\s*$")
TWO_MEMBERS = re.compile(r"[,/+&]|\band\b")
ORCHESTRATOR = "orchestrator"
NOT_LAUNCHES = {ORCHESTRATOR, "product manager", "user"}
NOT_LAUNCH_CELLS = {"orchestrator": "Orchestrator", "product manager": "Product Manager", "user": "User"}
STAMP = r"\d{4}-\d{2}-\d{2}(?: \d{2}:\d{2})?"
ROW = re.compile(rf"^\| (?P<date>{STAMP}) \| (?P<step>(?:[^|\\]|\\\|)*) \| (?P<member>(?:[^|\\]|\\\|)*) \| "
                 r"(?P<result>.*) \|\s*$")
MIN_TOKENS = 1000
VERDICT_QUOTE = 80  # characters of a result a verdict warning quotes
STEP_NUMBER = re.compile(r"^\s*(\d+)")
ROUND_OF_SAME_MODE = re.compile(r"\b(?:fix|delta|revis)", re.I)
SPECS_PREFIX = "specs/"
FOLDER_NAME = re.compile(r"\w[\w.-]*")
FIX_STEP = re.compile(r"^\s*11\b")
TEMPLATE_VALUE = re.compile(r"[\w./-]{1,100}")
# The step-12 lessons row (.squad/orchestration.md, *Squad lessons*): a lesson issue's URL, or exactly "no lessons".
LESSONS_STEP = re.compile(r"^\s*12\b")
ISSUE_URL = re.compile(r"https://github\.com/[\w.-]+/[\w.-]+/issues/\d+")
NO_LESSONS = "no lessons"
DECIDE = re.compile(r"\bdecide\b", re.I)
LOOP_LIMIT = re.compile(r"\bloop limit", re.I)
# Continuing a member (.squad/orchestration.md): after this many continuations, or once the agent's reported
# tokens reach this figure, the next round is a fresh launch with a hand-over.
CONTINUATION_CAP = 5
CONTINUATION_TOKENS = 300_000
FRONT_MATTER_KEY = re.compile(r"^(model|effort):\s*(\S+)\s*$", re.M)
PLAN_TIER = re.compile(r"^Tier:\s*`?(\w+)", re.M)
# The launches the orchestrator overrides by default (.squad/orchestration.md, *Launch model*): agent -> (tier, model).
LAUNCH_OVERRIDES = {"squad-tester": ("security", "opus")}


_TRANSCRIPTS = []
_GATE_STAMP = []
_SQUAD_NEXT = []


def transcripts():
    """`session-usage.py` next to this script, which reads Claude Code's transcripts, as a module (loaded once)."""
    if not _TRANSCRIPTS:
        here = os.path.dirname(os.path.abspath(__file__))
        spec = importlib.util.spec_from_file_location("session_usage", os.path.join(here, "session-usage.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        _TRANSCRIPTS.append(module)
    return _TRANSCRIPTS[0]


def gate_stamp():
    """`gate-stamp.py` next to this script, which reads the log's gate stamps, as a module (loaded once)."""
    if not _GATE_STAMP:
        here = os.path.dirname(os.path.abspath(__file__))
        spec = importlib.util.spec_from_file_location("gate_stamp", os.path.join(here, "gate-stamp.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        _GATE_STAMP.append(module)
    return _GATE_STAMP[0]


def squad_next():
    """`squad-next.py` next to this script, which reads the members' verdict lines back, as a module (loaded once)."""
    if not _SQUAD_NEXT:
        here = os.path.dirname(os.path.abspath(__file__))
        spec = importlib.util.spec_from_file_location("squad_next", os.path.join(here, "squad-next.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        _SQUAD_NEXT.append(module)
    return _SQUAD_NEXT[0]


def verdict_warning(member, step, row):
    """A warning when the written row of a member whose verdict `squad-next.py` reads carries no verdict line it can
    read, naming the exact line, the start of the result it read and how to correct the row; None otherwise. The row
    as written is checked, the cell `squad-next.py` reads back, not the argument (PlexToJellyfinSync#302)."""
    if is_not_launch(member):
        return None
    found = ROW.match(row)
    result = found.group("result") if found else row
    missing = squad_next().verdict_problem(member_agent(member), result)
    if missing is None:
        return None
    start = result if len(result) <= VERDICT_QUOTE else result[:VERDICT_QUOTE] + "…"
    return (f"WARNING: the row of {member} carries no verdict line squad-next.py can read; it needs {missing}. The "
            f"result as written starts \"{start}\" (a backtick or `$` inside double quotes is run by the shell). When "
            f"the report states the line, correct the row without a new launch: --replace-last \"{member}\" --step "
            f"\"{step}\" --new-result \"<the verdict line>, then the result\"")


def escape_controls(text):
    """Every control character other than tab and line break as `U+XXXX`."""
    return "".join(f"U+{ord(ch):04X}" if unicodedata.category(ch) in CONTROL_CATEGORIES and ch not in KEEP else ch
                   for ch in text)


def canonical_member(member):
    """`Orchestrator`, `Product Manager` and `User` for their `squad-` prefixed or differently cased spellings
    (`squad-orchestrator`, `squad-user`); any other member is returned as given."""
    if member is None:
        return None
    name = re.sub(r"^squad[- ]", "", member.strip().lower()).replace("-", " ")
    return NOT_LAUNCH_CELLS.get(name, member)


def cell(text):
    text = escape_controls(text).replace("\t", " ")
    text = re.sub(r"\r\n|\r|\n", "<br>", text.strip())
    return text.replace("|", "\\|")


def trailer(launch, tokens, tool_uses, seconds, continued=False, origin=None, kinds=None):
    """The launch trailer, already escaped as part of a cell; with `tokens` None, the mark of a launch whose usage
    was never reported. A continuation of a fresh launch other than the member's last names that launch's step.
    `kinds` (input, output, cache read, cache write, weighted or None) comes from the launch's transcript."""
    launch = cell(launch)
    if tokens is None:
        return f"({launch} · {UNREPORTED})"
    mark = ""
    if continued:
        mark = f" · continued from {cell(origin)}" if origin else " · continued"
    if tokens == PENDING:
        return f"({launch} · {PENDING}{mark})"
    return f"({launch} · {tokens:,} tokens · {tool_uses} tool uses · {seconds} s{kind_text(kinds)}{mark})"


def kind_text(kinds):
    """The trailer's part with the tokens per kind and the weighted figure, or "" without them."""
    if not kinds:
        return ""
    text = f" · {kinds[0]:,} in · {kinds[1]:,} out · {kinds[2]:,} cache read · {kinds[3]:,} cache write"
    return text if kinds[4] is None else f"{text} · {kinds[4]:,} weighted"


def metrics(match):
    """(tokens, tool uses, seconds) of a trailer match."""
    return (int(match.group("tokens").replace(",", "")), int(match.group("tools")), int(match.group("seconds")))


def kind_metrics(match):
    """(input, output, cache read, cache write, weighted or None) of a trailer match, or None for a launch logged
    without its transcript."""
    if match.group("input") is None:
        return None
    values = [match.group(name) for name in ("input", "output", "read", "write", "weighted")]
    return tuple(None if value is None else int(value.replace(",", "")) for value in values)


def agent_chains(rows, member):
    """The agents of `member` (an escaped cell) in log order, as {"step", "tokens", "continuations", "over"}: a fresh
    launch starts one, a continuation row joins the fresh launch its trailer names (`· continued from <step>`), or
    else the member's last fresh launch before it. "tokens" is the agent's reported total, "over" the steps of its
    continuations launched after the cap (`CONTINUATION_CAP`, `CONTINUATION_TOKENS`) was already reached; a
    continuation without any fresh launch before it starts a chain with no step. Also the index of the member's
    last fresh launch and {fresh step: index} as the third and second item."""
    chains, fresh, current = [], {}, None
    for _, row_step, row_member, result in rows:
        match = TRAILER.search(result) if row_member == member else None
        if not match:
            continue
        tokens = metrics(match)[0]
        if not match.group("continued"):
            current = fresh[row_step] = len(chains)
            chains.append({"step": row_step, "tokens": tokens, "continuations": 0, "over": []})
            continue
        own = match.group("origin")
        index = fresh.get(own) if own else current
        if index is None:
            index = current = len(chains)
            chains.append({"step": None, "tokens": 0, "continuations": 0, "over": []})
        chain = chains[index]
        if at_cap(chain):
            chain["over"].append(row_step)
        chain["tokens"] += tokens
        chain["continuations"] += 1
    return chains, fresh, current


def at_cap(chain):
    """True when the agent may not be continued again (`.squad/orchestration.md`, *Continuing a member*)."""
    return chain["continuations"] >= CONTINUATION_CAP or chain["tokens"] >= CONTINUATION_TOKENS


def continued_chain(path, member, skip_last=False, step=None, origin=None):
    """The agent a continuation of `member` continues, as `agent_chains` gives it: with `origin` the member's last
    fresh launch whose step cell is that text, else its last fresh launch. None when the member has no row with
    usage (with `origin`: no fresh launch of that step). With `skip_last` the member's last trailer is left out
    (it is the one being replaced); with `step`, only the rows before the member's last row of that step count
    (that row is the one amended or replaced)."""
    with open(path, encoding="utf-8-sig") as handle:
        rows = parse_rows(handle.read())
    wanted = cell(member)
    if step is not None:
        rows = rows_before(rows, wanted, cell(step))
    elif skip_last:
        last = [index for index, row in enumerate(rows) if row[2] == wanted and TRAILER.search(row[3])]
        rows = rows[:last[-1]] + rows[last[-1] + 1:] if last else rows
    chains, fresh, current = agent_chains(rows, wanted)
    index = fresh.get(cell(origin)) if origin is not None else current
    return None if index is None else chains[index]


def previous_tokens(path, member, skip_last=False, step=None, origin=None):
    """The tokens the continued agent had used before the round being logged: its fresh launch plus the
    continuations logged for it (`continued_chain`, which takes the same arguments); None when there is none."""
    chain = continued_chain(path, member, skip_last, step, origin)
    return None if chain is None else chain["tokens"]


def cap_warning(path, member, skip_last=False, step=None, origin=None):
    """A warning, or None, for a continuation of an agent that had already been continued `CONTINUATION_CAP` times
    or reported `CONTINUATION_TOKENS` tokens (the arguments as for `continued_chain`)."""
    chain = continued_chain(path, member, skip_last, step, origin)
    if chain is None or not at_cap(chain):
        return None
    return (f"WARNING: {member}'s agent was already continued {chain['continuations']} time(s) at "
            f"{chain['tokens']:,} tokens: from {CONTINUATION_CAP} continuations or {CONTINUATION_TOKENS:,} tokens "
            "on, the next round is a fresh launch with a short hand-over (.squad/orchestration.md, *Continuing a "
            "member*)")


def rows_before(rows, member, step):
    """The rows before the last row of `member` whose step cell is `step` (both already escaped as cells; with
    `step` None, the member's last row); all rows when there is no such row."""
    for index in range(len(rows) - 1, -1, -1):
        if rows[index][2] == member and (step is None or rows[index][1] == step):
            return rows[:index]
    return rows


def continued_launch(path, member, launch, skip_last=False, step=None, origin=None):
    """The round's own share of a continued launch: the reported tokens minus the agent's earlier tokens, tool
    uses and seconds as reported (they cover the round alone). A reported token figure below the earlier total
    was already the round alone and is kept. None when there is nothing to continue from (`previous_tokens`). A
    pending launch keeps its pending mark and records the continuation, which its amend then completes."""
    earlier = previous_tokens(path, member, skip_last, step, origin)
    if earlier is None:
        return None
    model, tokens, tool_uses, seconds = launch[:4]
    if tokens == PENDING:
        return (model, PENDING, None, None, True, origin)
    # The tokens per kind of a transcript (the seventh item) are the round's own already.
    return (model, tokens - earlier if tokens >= earlier else tokens, tool_uses, seconds, True, origin, *launch[6:])


def mode_change_warning(path, member, own_step, origin=None, amending=False, step=None):
    """A warning, or None, for a continuation logged at another step number than the fresh launch it continues
    (`origin`, or else the member's last fresh launch) whose step cell names no fix, delta or revise round: the
    first launch of another mode is a fresh launch (`.squad/orchestration.md`, *Continuing a member*). With `amending`,
    the row being amended (the member's last, or its last of `step`) is left out."""
    with open(path, encoding="utf-8-sig") as handle:
        rows = parse_rows(handle.read())
    wanted = cell(member)
    if amending:
        rows = rows_before(rows, wanted, None if step is None else cell(step))
    fresh = [row_step for _, row_step, row_member, result in rows if row_member == wanted
             and TRAILER.search(result) and not TRAILER.search(result).group("continued")]
    if origin is not None:
        continued = cell(origin)
    elif fresh:
        continued = fresh[-1]
    else:
        continued = None
    numbers = [STEP_NUMBER.match(text or "") for text in (continued, own_step)]
    if None in numbers or numbers[0].group(1) == numbers[1].group(1) or ROUND_OF_SAME_MODE.search(own_step):
        return None
    return mode_change_text(member, own_step, continued, fresh if origin is None else [])


def mode_change_text(member, own_step, continued, fresh):
    """The warning for a continuation into another mode; a note naming `--continued-from` when one of the member's
    fresh launches has the step number of `own_step` (another task group's agent of the same mode)."""
    number = STEP_NUMBER.match(own_step).group(1)
    same = [text for text in fresh if STEP_NUMBER.match(text) and STEP_NUMBER.match(text).group(1) == number]
    if same:
        return (f"NOTE: '{own_step}' is logged as continuing {member}'s last fresh launch, '{continued}'. If the "
                f"round continued the agent of '{same[-1]}' (the same mode, another task group), log it with "
                f"--continued-from \"{same[-1]}\" so its tokens are counted against that agent; nothing else is "
                "wrong")
    return (f"WARNING: '{own_step}' continues {member}'s launch of step '{continued}': a fix, delta or revise "
            "round continues, but the first launch of another mode is a fresh launch (.squad/orchestration.md, "
            "*Continuing a member*)")


def resolve_log(root, folder):
    """The `log.md` of the named work folder under `specs/`, or None. The name is compared with the folders
    that exist there and the path is built from the directory listing, so the argument never becomes a path."""
    wanted = folder.strip().replace("\\", "/").rstrip("/")
    wanted = wanted.removeprefix(SPECS_PREFIX)
    specs = os.path.join(root, "specs")
    if not os.path.isdir(specs):
        return None
    for name in sorted(os.listdir(specs)):
        if name == wanted and name not in (".", "..") and os.path.isdir(os.path.join(specs, name)):
            path = os.path.join(specs, name, "log.md")
            return path if os.path.isfile(path) else None
    return None


def agent_file(root, agent):
    """`.claude/agents/<agent>.md` when it exists; found through the directory listing, so the argument never
    becomes a path."""
    wanted = agent.strip().removesuffix(".md") + ".md"
    agents = os.path.join(root, ".claude", "agents")
    if not os.path.isdir(agents):
        return None
    names = [n for n in os.listdir(agents) if n == wanted and os.path.isfile(os.path.join(agents, n))]
    return os.path.join(agents, names[0]) if names else None


def agent_launch(root, agent):
    """`model/effort` from the front matter of `.claude/agents/<agent>.md`, or None when there is no such file or
    it names no model or effort."""
    path = agent_file(root, agent)
    if path is None:
        return None
    with open(path, encoding="utf-8-sig") as handle:
        text = handle.read().replace("\r\n", "\n")
    end = text.find("\n---", 4) if text.startswith("---\n") else -1
    found = dict(FRONT_MATTER_KEY.findall(text[4:end])) if end > 0 else {}
    return f"{found['model']}/{found['effort']}" if "model" in found and "effort" in found else None


def amendable(result, replace):
    """(True, the trailer match or None) when the row may take a new trailer: one without a trailer or with a pending
    one is amended; one with any trailer, a pending one included, is replaced."""
    old = ANY_TRAILER.search(result)
    pending = bool(PENDING_TRAILER.search(result))
    if replace:
        return bool(old), old
    return (not old or pending), old


def target_row(path, member, step=None):
    """(step cell, result cell) of the row `--amend-last`/`--replace-last` would change, or (None, None)."""
    with open(path, encoding="utf-8-sig") as handle:
        rows = parse_rows(handle.read())
    wanted, wanted_step = cell(member), None if step is None else cell(step)
    for _, row_step, row_member, result in reversed(rows):
        if row_member == wanted and (wanted_step is None or row_step == wanted_step):
            return row_step, result
    return None, None


def amend_last(path, member, launch, replace=False, step=None):
    """Add the launch trailer to the last row of `member` (with `step`, to its last row of that step); the amended
    row, or None when no such row exists or it already carries a trailer (a pending one is completed). With
    `replace`, that trailer is replaced instead; a row without one is left alone (None); a pending one is replaced too."""
    with open(path, "rb") as handle:
        data = handle.read()
    eol = b"\r\n" if b"\r\n" in data else b"\n"
    lines = data.decode("utf-8-sig").split(eol.decode())
    wanted, wanted_step = cell(member), None if step is None else cell(step)
    for index in range(len(lines) - 1, -1, -1):
        match = ROW.match(lines[index])
        if not match or match.group("member") != wanted or (wanted_step is not None and match.group("step") != wanted_step):
            continue
        allowed, old = amendable(match.group("result"), replace)
        if not allowed:
            return None
        result = match.group("result")[:old.start()] if old else match.group("result")
        lines[index] = f"| {match.group('date')} | {match.group('step')} | {match.group('member')} | " \
                       f"{result.rstrip()} {trailer(*launch)} |"
        with open(path, "wb") as handle:
            handle.write(eol.decode().join(lines).encode())
        return lines[index]
    return None


def replace_result(path, member, text, step=None):
    """Replace the result text of the last row of `member` (with `step`, of its last row of that step), keeping its
    launch trailer, so a corrected verdict line is no second launch; the changed row, or None without such a row."""
    with open(path, "rb") as handle:
        data = handle.read()
    eol = b"\r\n" if b"\r\n" in data else b"\n"
    lines = data.decode("utf-8-sig").split(eol.decode())
    wanted, wanted_step = cell(member), None if step is None else cell(step)
    for index in range(len(lines) - 1, -1, -1):
        match = ROW.match(lines[index])
        if not match or match.group("member") != wanted or (wanted_step is not None and match.group("step") != wanted_step):
            continue
        old = ANY_TRAILER.search(match.group("result"))
        result = cell(text) + (f" {old.group(0)}" if old else "")
        lines[index] = f"| {match.group('date')} | {match.group('step')} | {match.group('member')} | {result} |"
        with open(path, "wb") as handle:
            handle.write(eol.decode().join(lines).encode())
        return lines[index]
    return None


PLACEHOLDER_HEADING = b"# Log: <issue #number | feature name>"


def log_heading(path):
    """`issue #3` for `specs/issue-3/log.md`, `feature x` for `specs/feature-x/log.md`, else the folder name."""
    name = os.path.basename(os.path.dirname(os.path.abspath(path)))
    kind, _, rest = name.partition("-")
    if kind == "issue" and rest.isdigit():
        return f"issue #{rest}"
    return f"{kind} {rest}" if rest else name


def append_row(path, step, member, result, today=None, launch=None):
    with open(path, "rb") as handle:
        data = handle.read()
    if data.startswith(PLACEHOLDER_HEADING):
        data = f"# Log: {log_heading(path)}".encode() + data[len(PLACEHOLDER_HEADING):]
    eol = b"\r\n" if b"\r\n" in data else b"\n"
    date = today or datetime.datetime.now(datetime.UTC).strftime("%Y-%m-%d %H:%M")
    text = cell(result) if launch is None else f"{cell(result)} {trailer(*launch)}"
    row = f"| {date} | {cell(step)} | {cell(member)} | {text} |".encode()
    if data and not data.endswith((b"\n", b"\r\n")):
        data += eol
    with open(path, "wb") as handle:
        handle.write(data + row + eol)
    return row.decode()


def parse_rows(text):
    """(stamp, step, member, result) of every `| date [time] | step | member | result |` row; the title may contain
    escaped pipes."""
    rows = []
    for line in text.splitlines():
        cells = [c.strip() for c in re.split(r"(?<!\\)\|", line.strip())]
        if len(cells) >= 6 and not cells[0] and not cells[-1] and re.fullmatch(STAMP, cells[1]):
            rows.append((cells[1], cells[2], cells[3], "|".join(cells[4:-1])))
    return rows


def orchestrator(rows):
    """Markdown table: wall time from the first to the last row with a time of day, the part of it the launch
    trailers do not cover, and the gate runs (Orchestrator rows of step 7); None with fewer than two timed rows."""
    times = [datetime.datetime.strptime(stamp, "%Y-%m-%d %H:%M") for stamp, _, _, _ in rows if " " in stamp]
    if len(times) < 2:
        return None
    first, last = min(times), max(times)
    wall = int((last - first).total_seconds())
    covered = covered_seconds(rows, first, last)
    gates = sum(1 for _, step, member, _ in rows if member == "Orchestrator" and re.match(r"7\b", step))
    return "\n".join(["| Member | Wall time (s) | Not covered by launches (s) | Gate runs |",
                      "| ------ | ------------- | --------------------------- | --------- |",
                      f"| Orchestrator | {wall:,} | {max(wall - covered, 0):,} | {gates} |"])


def covered_seconds(rows, first, last):
    """Seconds between `first` and `last` during which at least one launch ran: each timed row with a trailer
    ends at its time of day and started its seconds earlier; the intervals are merged, so parallel launches
    count once."""
    intervals = []
    for stamp, _, _, result in rows:
        match = TRAILER.search(result)
        if " " not in stamp or not match:
            continue
        end = datetime.datetime.strptime(stamp, "%Y-%m-%d %H:%M")
        start = end - datetime.timedelta(seconds=metrics(match)[2])
        intervals.append((max(start, first), min(end, last)))
    covered, reach = 0, first
    for start, end in sorted(intervals):
        start = max(start, reach)
        if end > start:
            covered += int((end - start).total_seconds())
            reach = end
    return covered


def is_not_launch(member):
    """True for the Orchestrator, the Product Manager and the User, whose rows are never launches."""
    return member.strip().lower() in NOT_LAUNCHES


def missing_usage(rows):
    """(stamp, step, member) of every launch row — a member other than the Orchestrator, the Product Manager and
    the User — without usage: no trailer, or a pending one; a launch marked as unreported counts as complete."""
    return [(stamp, step, member) for stamp, step, member, result in rows
            if not is_not_launch(member) and (not ANY_TRAILER.search(result) or PENDING_TRAILER.search(result))]


def unreported_usage(rows):
    """(step, member) of every launch row marked `usage not reported`."""
    return [(step, member) for _, step, member, result in rows if UNREPORTED_TRAILER.search(result)]


def member_totals(rows):
    """{(member, model/effort): [launches, tokens, tool uses, seconds, continued launches]} from the trailers."""
    per_member = {}
    for _, _, member, result in rows:
        match = TRAILER.search(result) or UNREPORTED_TRAILER.search(result) or PENDING_TRAILER.search(result)
        if not match:
            continue
        entry = per_member.setdefault((member, match.group("launch")), [0, 0, 0, 0, 0])
        entry[0] += 1
        if "tokens" not in match.groupdict():
            continue
        entry[4] += 1 if match.group("continued") else 0
        for index, value in enumerate(metrics(match), start=1):
            entry[index] += value
    return per_member


def over_cap(rows):
    """(member, steps) of every member with continuations launched past the cap (`agent_chains`)."""
    found = []
    for member in dict.fromkeys(row[2] for row in rows):
        steps = [step for chain in agent_chains(rows, member)[0] for step in chain["over"]]
        if steps:
            found.append((member, steps))
    return found


def usage_notes(rows):
    """The notes under the table: launches whose usage never arrived, and launch rows still without usage."""
    text = ""
    unreported = unreported_usage(rows)
    if unreported:
        names = ", ".join(f"`{step} · {member}`" for step, member in unreported)
        text += (f"\n\n**Without reported usage:** {len(unreported)} launch(es) whose task notification never "
                 f"arrived — counted in the launches, not in the totals: {names}")
    over = over_cap(rows)
    if over:
        names = ", ".join(f"`{member} · {', '.join(steps)}`" for member, steps in over)
        text += (f"\n\n**Past the continuation cap:** continuations launched after the agent had been continued "
                 f"{CONTINUATION_CAP} times or had reported {CONTINUATION_TOKENS:,} tokens, where a fresh launch with "
                 f"a hand-over was due (`.squad/orchestration.md`, *Continuing a member*): {names}")
    missing = missing_usage(rows)
    if missing:
        names = ", ".join(f"`{step} · {member}`" for _, step, member in missing)
        text += (f"\n\n**Incomplete:** {len(missing)} launch row(s) without usage — add it with `--amend-last` "
                 f"from the launch's task notification before posting this table: {names}")
    return text


def kind_totals(rows):
    """({(member, model/effort): [input, output, cache read, cache write, weighted]} of the launches whose trailer
    carries tokens per kind, the number of launches with usage but without them)."""
    per_member, without = {}, 0
    for _, _, member, result in rows:
        match = TRAILER.search(result)
        kinds = kind_metrics(match) if match else None
        if match and kinds is None:
            without += 1
        if kinds is None:
            continue
        entry = per_member.setdefault((member, match.group("launch")), [0, 0, 0, 0, 0])
        for index, value in enumerate(kinds):
            entry[index] += value or 0
    return per_member, without


def orchestrator_kinds(usage):
    """("Orchestrator", model/effort, [input, output, cache read, cache write, weighted]) of the orchestrating
    session's own responses (`session-usage.py`, `usage_since`), or None when there are none."""
    per_model, efforts = usage
    if not per_model:
        return None
    reader = transcripts()
    counter = collections.Counter()
    for model_counter in per_model.values():
        counter.update(model_counter)
    models = "+".join(sorted({reader.family(model) or model for model in per_model}))
    effort = efforts.most_common(1)[0][0] if efforts else "default"
    values = [counter[field] for field, _ in reader.TOKEN_FIELDS]
    return "Orchestrator", f"{models}/{effort}", values + [reader.weighted_sum(per_model)[0]]


def kind_table(rows, usage):
    """The table of tokens per kind and the weighted figure per member, with the orchestrator's own row (`usage`
    as `orchestrator_kinds` takes it, or None outside Claude Code) and each row's share of the weighted total; ""
    when no launch carries tokens per kind."""
    per_member, without = kind_totals(rows)
    if not per_member:
        return ""
    entries = [(member, launch, values) for (member, launch), values in sorted(per_member.items())]
    own = orchestrator_kinds(usage) if usage else None
    entries += [own] if own else []
    total = [sum(values[index] for _, _, values in entries) for index in range(5)]
    lines = ["Tokens per kind and their weighted figure (every token at its list price, in tokens of Sonnet input: "
             "`.squad/tools/session-usage.py`):", "",
             "| Member | Model/effort | Input | Output | Cache read | Cache write | Weighted | Share |",
             "| ------ | ------------ | ----- | ------ | ---------- | ----------- | -------- | ----- |"]
    for member, launch, values in entries:
        share = f"{values[4] / total[4]:.0%}" if total[4] else "–"
        lines.append(f"| {member} | {launch} | " + " | ".join(f"{value:,}" for value in values) + f" | {share} |")
    lines.append("| **Total** | | " + " | ".join(f"{value:,}" for value in total) + " | 100% |")
    if not own:
        lines.append("\nThe orchestrator's own share: not provided (no Claude Code session transcript found).")
    if without:
        lines.append(f"\n{without} launch(es) logged from task notifications carry no tokens per kind and are not in "
                     "this table.")
    return "\n\n" + "\n".join(lines)


def summary(path, usage=None):
    """Markdown table: launches, tokens, tool uses and seconds per member (and model/effort), with a total; then
    the orchestrator's table when the rows carry a time of day, and the table of tokens per kind with the
    orchestrator's share from `usage` (`kind_table`)."""
    with open(path, encoding="utf-8-sig") as handle:
        rows = parse_rows(handle.read())
    lines = ["| Member | Model/effort | Launches | Tokens | Tool uses | Seconds |",
             "| ------ | ------------ | -------- | ------ | --------- | ------- |"]
    total = [0, 0, 0, 0, 0]
    for (member, launch), entry in sorted(member_totals(rows).items()):
        lines.append(f"| {member} | {launch} | {launches(entry[0], entry[4])} | {entry[1]:,} | {entry[2]} | "
                     f"{entry[3]} |")
        total = [a + b for a, b in zip(total, entry, strict=True)]
    lines.append(f"| **Total** | | {launches(total[0], total[4])} | {total[1]:,} | {total[2]} | {total[3]} |")
    own = orchestrator(rows)
    return "\n".join(lines) + (f"\n\n{own}" if own else "") + kind_table(rows, usage) + usage_notes(rows)


def first_time(rows):
    """The time of day of the log's first timed row (naive UTC), or None."""
    times = [datetime.datetime.strptime(stamp, "%Y-%m-%d %H:%M") for stamp, _, _, _ in rows if " " in stamp]
    return min(times) if times else None


def session_transcript(session):
    """The orchestrating session's transcript (`session` or `CLAUDE_CODE_SESSION_ID`), or None."""
    reader = transcripts()
    return reader.find_transcript(reader.projects_dir(), session or os.environ.get("CLAUDE_CODE_SESSION_ID"))


def orchestrator_usage(rows, session):
    """The orchestrating session's own usage from the log's first timed row on, as `usage_since` returns it; None
    without a session transcript."""
    path = session_transcript(session)
    return None if path is None else transcripts().usage_since(path, first_time(rows))


def fixes_after_pr(root, folder):
    """(count, time of the last one, UTC) of the branch's own commits after the commit that removed the work folder
    in step 10 (the first removal; a merge of the base branch and the commits it brings in are left out); None
    before that removal (or outside a Git repository)."""
    name = folder.strip().replace("\\", "/").strip("/").removeprefix(SPECS_PREFIX)
    if not FOLDER_NAME.fullmatch(name):
        return None

    def git(*args):
        return subprocess.run(["git", "-C", root, *args], capture_output=True, text=True, check=False).stdout

    removals = git("log", "--diff-filter=D", "--format=%H", "--", f"specs/{name}/log.md",
                   f":(glob)specs/{name}-*/log.md").split()
    if not removals:
        return None
    stamps = [int(value) for value in
              git("log", "--first-parent", "--no-merges", "--format=%ct", f"{removals[-1]}..HEAD").split()]
    last = datetime.datetime.fromtimestamp(max(stamps), datetime.UTC).replace(tzinfo=None) if stamps else None
    return len(stamps), last


def reviewed_after(rows, when):
    """True when a step-11 row of the Reviewer, or of the Orchestrator for a verbatim fix, has a time of day at or
    after `when` (rows carry minutes, so `when` is cut to the minute)."""
    when = when.replace(second=0, microsecond=0)
    for stamp, step, member, result in rows:
        if " " not in stamp or not FIX_STEP.match(step):
            continue
        reviewer = "review" in member.lower()
        verbatim = member.strip().lower() == ORCHESTRATOR and "verbatim" in result.lower()
        if (reviewer or verbatim) and datetime.datetime.strptime(stamp, "%Y-%m-%d %H:%M") >= when:
            return True
    return False


def unreviewed_fixes(root, folder, rows):
    """The note on the commits after the pull request that no review row follows, or None."""
    fixes = fixes_after_pr(root, folder)
    if not fixes or not fixes[0] or reviewed_after(rows, fixes[1]):
        return None
    return (f"Unreviewed after the PR: {fixes[0]} commit(s) after the work folder was removed, the last at "
            f"{fixes[1]:%Y-%m-%d %H:%M} UTC, and no step-11 row of the Reviewer (or of the Orchestrator for a "
            "verbatim fix) after it. Run the delta review of steps 7-8 on the head, log it at step 11 and run the "
            "summary again.")


def launches(count, continued):
    return f"{count} ({continued} continued)" if continued else str(count)


def whole_seconds(text):
    """argparse type for --seconds: a number, rounded to whole seconds (duration_ms / 1000 is usually a decimal)."""
    try:
        return round(float(text))
    except (ValueError, OverflowError):
        raise argparse.ArgumentTypeError(f"invalid number of seconds: '{text}'") from None


def build_parser():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("folder")
    parser.add_argument("step", nargs="?")
    parser.add_argument("member", nargs="?")
    parser.add_argument("result", nargs="?")
    parser.add_argument("--summary", action="store_true", help="print the per-member launch table instead of a row")
    parser.add_argument("--amend-last", metavar="MEMBER", help="add the launch trailer to the last row of this member")
    parser.add_argument("--replace-last", metavar="MEMBER", help="replace the launch trailer of the last row of "
                                                                 "this member (to correct wrong metrics)")
    parser.add_argument("--new-result", metavar="TEXT", help="with --replace-last and no metric option: replace the "
                        "row's result text and keep its trailer (a verdict line logged wrong, corrected from the report)")
    parser.add_argument("--step", dest="at_step", metavar="STEP", help="with --amend-last or --replace-last: the "
                        "member's last row of this step instead of its last row (for a parallel launch whose "
                        "notification arrived after the next row was written)")
    parser.add_argument("--stdin", action="store_true", help="read the log from standard input instead of "
                        "specs/<folder>/log.md (step 10 removed the folder): the updated log goes to standard output")
    parser.add_argument("--unreported", action="store_true",
                        help="with --amend-last: the launch's task notification never arrived; the trailer marks "
                             "it as unreported (with --agent or --launch, without numbers)")
    parser.add_argument("--agent-id", metavar="ID", help="the launched agent's id (the Agent tool's result): model, "
                        "effort, tokens per kind, tool uses and seconds of its latest round come from its transcript")
    parser.add_argument("--round", type=int, metavar="N", help="with --agent-id: log the agent's N-th round instead "
                        "of its latest (a round logged late, after the agent was continued; round 1 is the fresh "
                        "launch, a later one is logged with --continued)")
    parser.add_argument("--session", metavar="SESSION", help="the Claude Code session whose transcripts --agent-id "
                        "and --summary read (default: CLAUDE_CODE_SESSION_ID)")
    parser.add_argument("--agent", metavar="squad-ROLE", help="the launched agent; model and effort come from its file")
    parser.add_argument("--launch", metavar="MODEL/EFFORT", help="the launched subagent's model and effort (when "
                                                               "the launch overrode the agent file)")
    parser.add_argument("--tokens", type=int, help="subagent_tokens from the usage block of the task notification")
    parser.add_argument("--tool-uses", type=int, help="tool_uses from the usage block of the task notification")
    parser.add_argument("--seconds", type=whole_seconds,
                        help="duration of the launch in seconds (duration_ms / 1000, rounded to whole seconds)")
    parser.add_argument("--continued", action="store_true",
                        help="the launch continued the member's last fresh agent: the usage block reports the "
                             "agent's tokens since its first launch, the trailer gets this round's share; its tool uses "
                             "and seconds cover the round alone and are passed as reported, too")
    parser.add_argument("--continued-from", metavar="STEP",
                        help="like --continued, for the agent of the member's fresh launch whose step cell is STEP "
                             "(when a later fresh launch of the member exists)")
    return parser


def is_pending_row(args):
    """A new launch row with the agent alone: no numbers yet, not an amend (a continuation may be pending too)."""
    return not has_numbers(args) and not (args.amend_last or args.replace_last)


def has_numbers(args):
    """Whether any of --tokens, --tool-uses and --seconds is given."""
    return any(value is not None for value in (args.tokens, args.tool_uses, args.seconds))


def unreported_launch(parser, model, args):
    """The launch of an --unreported row, which takes the agent alone."""
    if not model or has_numbers(args) or args.continued or args.continued_from is not None:
        parser.error("--unreported goes with --agent (or --launch) alone: no --tokens, --tool-uses, --seconds "
                     "or --continued")
    return (model, None, None, None)


def plan_tier(log_path):
    """The tier the *Tier* line of the work folder's `plan.md` names, or None (no plan: tiers `docs` and `patch`,
    or a log read from standard input after step 10)."""
    path = os.path.join(os.path.dirname(log_path), "plan.md")
    if not os.path.isfile(path):
        return None
    with open(path, encoding="utf-8-sig") as handle:
        found = PLAN_TIER.search(handle.read())
    return found.group(1) if found else None


def default_override(log_path, agent, launch):
    """The `model/effort` the orchestrator launches `agent` with by default in this run instead of `launch` from
    its agent file, or None when no override applies."""
    override = LAUNCH_OVERRIDES.get(os.path.basename(agent or "").removesuffix(".md"))
    if override is None or launch is None or plan_tier(log_path) != override[0]:
        return None
    return f"{override[1]}/{launch.split('/', 1)[1]}"


def launch_metrics(parser, root, args, path):
    """(model/effort, tokens, tool uses, seconds) from the options, or None when no metric option is given; an
    incomplete set is a usage error, an unknown agent file ends the run with exit code 1. A launch the orchestrator
    overrides by default is refused with `--agent` alone, so the override is logged as launched. With `--agent-id`,
    the launch comes from the agent's transcript (`transcript_launch`)."""
    if args.agent_id:
        return transcript_launch(parser, root, args, path)
    if not (args.agent or args.launch or has_numbers(args)):
        return None
    model = args.launch or (agent_launch(root, args.agent) if args.agent else None)
    if args.agent and not args.launch and model is None:
        sys.exit(f"No .claude/agents/{args.agent}.md with a model and an effort")
    expected = None if args.launch else default_override(path, args.agent, model)
    if expected:
        parser.error(f"{args.agent} is launched with model {expected.split('/')[0]} in a tier-"
                     f"{plan_tier(path)} run (.squad/orchestration.md, *Launch model*): log the launch with "
                     f"--launch {expected} instead of --agent, or with the model it really ran on")
    if args.unreported:
        return unreported_launch(parser, model, args)
    if model and is_pending_row(args):
        return (model, PENDING, None, None)  # a new launch row whose notification has not arrived yet
    return reported_launch(parser, model, args)


def reported_launch(parser, model, args):
    """The launch of a row with its usage from the task notification: --tokens, --tool-uses and --seconds all given
    and in range."""
    if not model or None in (args.tokens, args.tool_uses, args.seconds):
        parser.error("--agent (or --launch), --tokens, --tool-uses and --seconds go together; a launch whose "
                     "notification has not arrived yet is logged with --agent alone (usage pending) and completed "
                     "with --amend-last")
    if args.tokens < MIN_TOKENS or args.tool_uses < 0 or args.seconds < 0:
        parser.error(f"--tokens must be at least {MIN_TOKENS:,} and --tool-uses and --seconds not negative; a launch "
                     "whose notification has not arrived yet is logged with --agent alone and completed with "
                     "--amend-last")
    return (model, args.tokens, args.tool_uses, args.seconds)


def transcript_launch(parser, root, args, path):
    """(model/effort, tokens, tool uses, seconds, False, None, tokens per kind) of the latest round of the agent
    `--agent-id` names, read from its transcript; a warning about the model goes to `args.warnings`. Exit code 1
    when this session has no transcript of that agent."""
    if args.launch or has_numbers(args) or args.unreported:
        parser.error("--agent-id reads model, effort and usage from the agent's transcript: no --launch, --tokens, "
                     "--tool-uses, --seconds or --unreported with it")
    reader = transcripts()
    session = session_transcript(args.session)
    found = reader.subagent_transcript(session, args.agent_id) if session else None
    rounds = reader.agent_rounds(found) if found else []
    if not rounds:
        sys.exit(f"No transcript of agent '{args.agent_id}' with a response in this session (CLAUDE_CODE_SESSION_ID "
                 "or --session): outside Claude Code, log the launch from its task notification (--agent with "
                 "--tokens, --tool-uses and --seconds)")
    agent = transcript_agent(parser, args, reader.agent_type(found))
    if args.round is not None and not 1 <= args.round <= len(rounds):
        parser.error(f"agent '{args.agent_id}' ran {len(rounds)} round(s): --round {args.round} names none of them")
    check_rounds(parser, args, args.round or len(rounds))
    last = rounds[(args.round or len(rounds)) - 1]
    expected, overridden = expected_launch(root, path, agent)
    model = max(last["models"], key=last["models"].get)
    effort = last["effort"] or (expected.split("/", 1)[1] if expected else "default")
    launched = f"{reader.family(model) or model}/{effort}"
    args.warnings.append(model_warning(agent, launched, expected, overridden))
    usage = last["usage"]
    kinds = (*(usage[field] for field, _ in reader.TOKEN_FIELDS), reader.weighted(usage, model))
    seconds = round((last["end"] - last["start"]).total_seconds()) if last["start"] else 0
    return (launched, last["context"], last["tool_uses"], seconds, False, None, kinds)


def transcript_agent(parser, args, recorded):
    """The agent the transcript records (`--agent`, if given, must name the same one), or `--agent` when the
    transcript records none."""
    given = args.agent.strip().removesuffix(".md") if args.agent else None
    if recorded == "unknown":
        return given
    if given and given != recorded:
        parser.error(f"agent '{args.agent_id}' is {recorded}, not {given}: check the agent id")
    return recorded


def check_rounds(parser, args, count):
    """A new row of an agent with several rounds is a continuation, one of an agent with one round a fresh launch;
    with `--round N`, the round logged is the N-th (`count`), so round 1 is the fresh launch."""
    if args.amend_last or args.replace_last:
        return
    continued = args.continued or args.continued_from is not None
    if count > 1 and not continued:
        parser.error(f"agent '{args.agent_id}' ran {count} rounds: its latest continues it — log it with --continued, "
                     "or --continued-from \"<step>\" when it is not the member's last fresh launch; when no round "
                     "of it is logged yet (a round logged late), log round 1 first with --round 1, then this one with "
                     "--continued")
    if count == 1 and continued:
        parser.error(f"agent '{args.agent_id}' ran one round: a fresh launch, logged without --continued or "
                     "--continued-from")


def expected_launch(root, path, agent):
    """(model/effort the agent should run on, whether *Launch model* overrides the agent file for it in this run);
    (None, False) without an agent file."""
    base = agent_launch(root, agent) if agent else None
    override = default_override(path, agent, base)
    return (override or base), bool(override)


def model_warning(agent, launched, expected, overridden):
    """A warning, or None, when the launch ran on another model family or effort than expected."""
    if expected is None:
        return None
    family = transcripts().family
    want_model, want_effort = expected.split("/", 1)
    got_model, got_effort = launched.split("/", 1)
    same_model = family(want_model) is None or family(want_model) == family(got_model)
    if same_model and want_effort == got_effort:
        return None
    source = ".squad/orchestration.md, *Launch model*," if overridden else f".claude/agents/{agent}.md"
    return (f"WARNING: {agent} ran on {launched}, but {source} names {expected}: the row records what it ran on; "
            "its next round is a fresh launch with the right model, never a continuation (.squad/orchestration.md, "
            "*Continuing a member*)")


def share_of_round(path, member, launch, continued, replace=False, step=None, origin=None):
    """The launch as logged: with `continued`, this round's share of the reported totals (with `origin`, of the
    agent of the member's fresh launch of that step); None when there is no earlier usage to subtract (reported
    to the user, exit code 1). With `replace`, the trailer being replaced is not part of the earlier usage; with
    `step`, only the rows before the amended one are."""
    if launch is None or not continued:
        return launch
    share = continued_launch(path, member, launch, skip_last=replace, step=step, origin=origin)
    if share is None and origin is not None:
        print(f"--continued-from: no fresh launch of {member} with usage at step '{origin}'")
    elif share is None:
        print(f"--continued: no earlier row of {member} with usage to continue from; an agent whose first round "
              "is not logged yet: log that round first with --agent-id <id> --round 1, then this one")
    return share


def amend_command(parser, path, folder, member, launch, replace, continued, step=None, origin=None):
    option = "--replace-last" if replace else "--amend-last"
    if launch is None:
        parser.error(f"{option} needs --agent (or --launch), --tokens, --tool-uses and --seconds (or --unreported)")
    if replace and launch[1] is None:
        parser.error("--unreported marks a row without a trailer: use it with --amend-last")
    own_step, current = target_row(path, member, step)
    current = current or ""
    continued, origin = pending_continuation(current, launch, continued, origin)
    # Only a replaced trailer with real numbers is part of the earlier usage a continuation subtracts.
    replaces_usage = replace and bool(TRAILER.search(current))
    launch = share_of_round(path, member, launch, continued, replaces_usage, step, origin)
    if launch is None:
        return 1
    warnings = []
    if continued:
        warnings = [cap_warning(path, member, replaces_usage, step, origin)]
        if own_step:
            warnings.insert(0, mode_change_warning(path, member, own_step, origin, True, step))
    row = amend_last(path, member, launch, replace=replace, step=step)
    if row is None:
        print(no_row_message(folder, member, replace, step))
        return 1
    for warning in filter(None, warnings):
        print(warning)
    print(row)
    return 0


def pending_continuation(result, launch, continued, origin):
    """(continued, origin) for an amend or replace with numbers: as given, or else as the pending trailer of the row
    being changed records them (`· usage pending · continued from <step>`), so the pending row's continuation is
    not lost when the amend leaves out --continued."""
    match = PENDING_TRAILER.search(result)
    if continued or launch[1] in (None, PENDING) or not match or not match.group("continued"):
        return continued, origin
    found = match.group("origin")
    return True, None if found is None else found.replace("\\|", "|")


def no_row_message(folder, member, replace, step):
    state = "with" if replace else "without"
    where = f"of step '{step}' " if step is not None else ""
    hint = "" if replace else " (a row that already carries one, also the unreported mark, is corrected with " \
                              "--replace-last)"
    return f"No row of {member} {where}{state} a launch trailer in {folder}{hint}"


def row_command(parser, path, args, launch):
    if args.step is None or args.member is None or args.result is None:
        parser.error("a row needs <step> <member> <result> (or --summary, --amend-last or --replace-last)")
    if TWO_MEMBERS.search(args.member):
        parser.error(f"one row per launch: '{args.member}' names more than one member; write a row for each, "
                     "also when they ran in parallel")
    if args.continued and launch is None:
        parser.error("--continued goes with the launch (--agent or --launch, with --tokens, --tool-uses and "
                     "--seconds or alone for a pending row), on the row or with --amend-last")
    if args.at_step is not None or args.unreported:
        parser.error("--step and --unreported go with --amend-last or --replace-last")
    launch = share_of_round(path, args.member, launch, args.continued, origin=args.continued_from)
    if args.continued and launch is None:
        return 1
    warnings = [mode_change_warning(path, args.member, cell(args.step), args.continued_from),
                cap_warning(path, args.member, origin=args.continued_from)] if args.continued else []
    for warning in filter(None, warnings):
        print(warning)
    row = append_row(path, args.step, args.member, args.result, launch=launch)
    print(row)
    warning = verdict_warning(args.member, args.step, row)
    if warning:
        print(warning)
    return 0


def new_result_command(parser, path, args):
    """`--replace-last MEMBER --new-result TEXT`: the result text of that row replaced, its trailer kept."""
    if not args.replace_last or args.amend_last or args.unreported or args.agent or args.launch or args.agent_id \
            or has_numbers(args) or args.continued or args.continued_from is not None or args.step is not None:
        parser.error("--new-result goes with --replace-last MEMBER (and --step) alone: it changes the result text and "
                     "keeps the trailer; a wrong trailer is replaced in a call of its own")
    row = replace_result(path, args.replace_last, args.new_result, args.at_step)
    if row is None:
        where = f" of step '{args.at_step}'" if args.at_step is not None else ""
        print(f"No row of {args.replace_last}{where} in {args.folder}")
        return 1
    print(row)
    warning = verdict_warning(args.replace_last, ROW.match(row).group("step"), row)
    if warning:
        print(warning)
    return 0


def summary_command(root, folder, path, session=None):
    """Print the summary; the exit code: 3 with launch rows still without usage, 4 with unreviewed fixes after
    the pull request, 5 without a step-12 lessons row (`lessons_check`), 6 without a passing gate stamp on the code
    head (`stamp_check`)."""
    with open(path, encoding="utf-8-sig") as handle:
        rows = parse_rows(handle.read())
    version = template_line(root)
    print((f"{version}\n\n" if version else "") + summary(path, orchestrator_usage(rows, session)))
    if missing_usage(rows):
        return 3
    note = unreviewed_fixes(root, folder, rows)
    if note:
        print(note, file=sys.stderr)
        return 4
    return lessons_check(root, path, rows) or stamp_check(root, rows)


def stamp_check(root, rows):
    """0 when the branch changes no code or the code head carries a passing gate stamp (`gate-stamp.py`), else 6
    with a note."""
    problem = gate_stamp().summary_problem(root, rows)
    if problem is None:
        return 0
    print(f"No passing gate stamp on the code head: {problem}; then run the summary again.", file=sys.stderr)
    return 6


def template_line(root):
    """`Template: <repository> <tag> (commit <sha>)` from `.squad/template.json`, the template version the run worked
    under (`untagged` for a template applied from a commit without a release tag), or None without the file."""
    try:
        with open(os.path.join(root, ".squad", "template.json"), encoding="utf-8-sig") as handle:
            record = json.load(handle)
    except (OSError, ValueError):
        return None
    if not isinstance(record, dict):
        return None

    def value(key, fallback):
        found = record.get(key)
        return found if isinstance(found, str) and TEMPLATE_VALUE.fullmatch(found) else fallback
    return f"Template: {value('repository', 'unknown')} {value('tag', 'untagged')} (commit {value('commit', 'unknown')[:12]})"


def lessons_rows(rows):
    """The results of the Orchestrator's step-12 rows that name a lesson issue's URL or say exactly "no lessons"."""
    return [result for _, step, member, result in rows if LESSONS_STEP.match(step) and member == "Orchestrator"
            and (ISSUE_URL.search(result) or result.strip().lower() == NO_LESSONS)]


def lessons_check(root, path, rows):
    """0 once a step-12 lessons row exists, else 5 with a note; while there is none, or the last one says "no
    lessons", the log's lesson candidates go to standard error."""
    found = lessons_rows(rows)
    candidates = lesson_candidates(root, path, rows) if not found or found[-1].strip().lower() == NO_LESSONS else []
    if candidates:
        print("Lesson candidates in this log; compare them with \"no lessons\" (.squad/orchestration.md, *Squad "
              "lessons*): " + "; ".join(candidates), file=sys.stderr)
    if found:
        return 0
    print("No lessons row: log one Orchestrator row at step 12 whose result names the URL of every lesson issue, or "
          "is exactly \"no lessons\" (.squad/orchestration.md, *Squad lessons*), and run the summary again.",
          file=sys.stderr)
    return 5


def lesson_candidates(root, path, rows):
    """What the log shows of the lesson candidates of `.squad/orchestration.md`, *Squad lessons*: Lead `decide`
    launches, loop limits, launches whose usage was never reported, continuations past the cap or into another
    mode, and launches on another model or effort than expected."""
    found = []
    for _, step, member, result in rows:
        if member == "Lead" and DECIDE.search(f"{step} {result}"):
            found.append(f"`{step} · {member}`: a Lead decide launch")
        elif LOOP_LIMIT.search(f"{step} {result}"):
            found.append(f"`{step} · {member}`: a loop limit")
    found += [f"`{step} · {member}`: usage never reported" for step, member in unreported_usage(rows)]
    found += [f"`{member} · {', '.join(steps)}`: continued past the cap" for member, steps in over_cap(rows)]
    found += [f"`{step} · {member}`: continued into another mode" for step, member in mode_changes(rows)]
    return found + model_mismatches(root, path, rows)


def mode_changes(rows):
    """(step, member) of every continuation logged at another step number than the fresh launch it continues, whose
    step cell names no fix, delta or revise round (what `mode_change_warning` warns about when it is logged)."""
    found, fresh = [], {}
    for _, step, member, result in rows:
        match = TRAILER.search(result)
        if match and not match.group("continued"):
            fresh[member] = step
        if not match or not match.group("continued"):
            continue
        numbers = [STEP_NUMBER.match(text or "") for text in (match.group("origin") or fresh.get(member), step)]
        if None not in numbers and numbers[0].group(1) != numbers[1].group(1) and not ROUND_OF_SAME_MODE.search(step):
            found.append((step, member))
    return found


def member_agent(member):
    """The agent of a member cell: `squad-devils-advocate` for `Devil's Advocate` (and for `squad-devils-advocate`)."""
    name = re.sub(r"^squad[- ]", "", member.strip().lower().replace("'", ""))
    return "squad-" + re.sub(r"[^a-z]+", "-", name).strip("-")


def allowed_launches(root, path, agent):
    """The model/effort values a launch of `agent` may have run on in this run: its agent file's, or the default
    override of *Launch model* in a run of its tier; both when the plan is gone (after step 10) and the tier unknown."""
    base = agent_launch(root, agent)
    override = LAUNCH_OVERRIDES.get(agent)
    if base is None or override is None:
        return [base] if base else []
    replaced = f"{override[1]}/{base.split('/', 1)[1]}"
    tier = plan_tier(path)
    if tier is None:
        return [base, replaced]
    return [replaced] if tier == override[0] else [base]


def model_mismatches(root, path, rows):
    """A candidate for every launch whose trailer names another model family or effort than its agent file or the
    default override of *Launch model* allows."""
    found = []
    for _, step, member, result in rows:
        match = TRAILER.search(result)
        agent = member_agent(member) if match else None
        allowed = allowed_launches(root, path, agent) if agent else []
        if allowed and all(model_warning(agent, match.group("launch"), launch, False) for launch in allowed):
            found.append(f"`{step} · {member}`: ran on {match.group('launch')}, not {' or '.join(allowed)}")
    return found


def run(parser, args, root, path):
    """The command the arguments ask for, against the log at `path`; the exit code."""
    if args.summary:
        return summary_command(root, args.folder, path, args.session)
    if args.new_result is not None:
        return new_result_command(parser, path, args)
    target = args.amend_last or args.replace_last or args.member
    uses_metrics = args.amend_last or args.replace_last or args.unreported or args.agent or args.launch \
        or args.agent_id or has_numbers(args) or args.continued or args.continued_from is not None
    if target is not None and is_not_launch(target) and uses_metrics:
        parser.error(f"'{target}' is no launch: its rows are written without any metric option and never "
                     "amended, and the summary asks no usage for them")
    args.warnings = []
    launch = launch_metrics(parser, root, args, path)
    for warning in filter(None, args.warnings):
        print(warning)
    args.continued = args.continued or args.continued_from is not None
    if args.amend_last and args.replace_last:
        parser.error("--amend-last and --replace-last exclude each other")
    if args.amend_last or args.replace_last:
        return amend_command(parser, path, args.folder, args.amend_last or args.replace_last, launch,
                             replace=bool(args.replace_last), continued=args.continued, step=args.at_step,
                             origin=args.continued_from)
    return row_command(parser, path, args, launch)


def removed_log(root, folder):
    """The text of `specs/<folder>/log.md` in the commit before step 10 removed it, or None (no such folder name, no
    removal, no Git)."""
    name = folder.strip().replace("\\", "/").rstrip("/").removeprefix(SPECS_PREFIX)
    if not re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9._-]*", name):
        return None
    rel = f"{SPECS_PREFIX}{name}/log.md"
    found = subprocess.run(["git", "-C", root, "log", "--diff-filter=D", "--format=%H", "-1", "--", rel],
                           capture_output=True, text=True, check=False)
    if found.returncode != 0 or not found.stdout.strip():
        return None
    shown = subprocess.run(["git", "-C", root, "show", f"{found.stdout.strip()}^:{rel}"], capture_output=True,
                           check=False)
    return shown.stdout.decode("utf-8-sig", "replace") if shown.returncode == 0 else None


def earlier_rows(root, folder, piped):
    """The row lines of the removed `log.md`, joined with newlines, when the piped log table starts at step 10 or
    later (a working-record comment that links `log.md` holds only the rows from step 10 on); empty when the table
    holds earlier rows (the whole log) or there is no removed `log.md`."""
    rows = parse_rows(piped)
    first = STEP_NUMBER.match(rows[0][1]) if rows else None
    if rows and (first is None or int(first.group(1)) < 10):
        return ""
    removed = removed_log(root, folder)
    if removed is None:
        print(f"WARNING: the piped log table starts at step 10 or later, but no removed specs/{folder}/log.md was found "
              "to read the earlier rows from (a shallow clone: git fetch --unshallow; or another folder name): the "
              "steps before it are missing", file=sys.stderr)
        return ""
    return "".join(f"{line.strip()}\n" for line in removed.splitlines() if parse_rows(line))


def with_earlier_rows(root, folder, piped):
    """The piped log with the removed `log.md`'s rows in front when it starts at step 10 (`earlier_rows`): what
    `squad-next.py --stdin` reads."""
    return earlier_rows(root, folder, piped) + piped


def run_on_stdin(parser, args, root):
    """`--stdin`: the log comes from standard input and lives in a temporary directory only, with the rows of the
    removed `log.md` it lacks in front of it; a command that changes it prints the result to standard error and the
    piped log with its change to standard output."""
    piped = sys.stdin.buffer.read()
    if piped.strip() and not piped.endswith(b"\n"):
        # A table copied from a comment ends without a line break: the new row would join its last line (Vandox#440).
        piped += b"\r\n" if b"\r\n" in piped else b"\n"
    prefix = earlier_rows(root, args.folder, piped.decode("utf-8-sig", "replace")).encode()
    with tempfile.TemporaryDirectory() as directory:
        path = os.path.join(directory, "log.md")
        with open(path, "wb") as handle:
            handle.write(prefix + piped)
        if args.summary:
            return run(parser, args, root, path)
        with contextlib.redirect_stdout(sys.stderr):
            code = run(parser, args, root, path)
        if code != 0:
            return code
        with open(path, "rb") as handle:
            data = handle.read()
        if not data.startswith(prefix):
            print("A row before the comment's log table cannot change: it is in the linked log.md", file=sys.stderr)
            return 1
        sys.stdout.buffer.write(data[len(prefix):])
        return 0


def main():
    parser = build_parser()
    args = parser.parse_args()
    args.member = canonical_member(args.member)
    args.amend_last = canonical_member(args.amend_last)
    args.replace_last = canonical_member(args.replace_last)
    if args.round is not None and not args.agent_id:
        parser.error("--round names a round of the transcript --agent-id reads")
    toplevel = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=False)
    root = toplevel.stdout.strip() if toplevel.returncode == 0 else os.getcwd()
    if args.stdin:
        return run_on_stdin(parser, args, root)
    path = resolve_log(root, args.folder)
    if path is None:
        print(f"No log.md in {args.folder} (create it from specs/_template/log.md first; after step 10 of the "
              "squad skills removed the work folder, pipe the working-record comment's log table in with --stdin)")
        return 1
    return run(parser, args, root, path)


if __name__ == "__main__":
    sys.exit(main())
