#!/usr/bin/env python3
"""Next step: what the orchestrator does after the row it just logged, read from the work folder instead of carried
in memory.

The orchestrator runs it after every row of `log.md`. It reads the work folder's `log.md`, `plan.md`, `spec.md` and
`tasks.md`, runs `plan-check.py` (and its verification matrix before step 7 and in step 10) where the pipeline calls
for it, and prints the next step of `.squad/routing.md` (*Pipeline*) for the run's kind and tier:

- **a launch**: the agent and its mode; the model and effort — the agent file's, or the default override of
  `.squad/orchestration.md`, *Launch model* (every `squad-tester` launch of a tier-`security` run gets `opus`, passed
  explicitly); fresh or continued (*Continuing a member*): a fix, delta or revise round continues the member's last
  agent, the Lead's mode `plan` after mode `spec` continues its spec agent, the next task group continues the member's
  last agent of the same mode, named by the step cell of its fresh launch — and the first launch of another mode, an
  agent continued five times or from 250,000 tokens on, or one that ran on another model than *Launch model* names is a
  fresh launch (the cap and the model: with a short hand-over); what to hand over, with the exact verdict line of the
  Reviewer, the Devil's Advocate, Security and the Lead in mode `decide` to quote in the prompt, so the member does
  not depend on its agent file being current; and the `squad-log.py` command that logs it;
- **the orchestrator's own step**: the row to log (a skipped challenge with the reason `plan-check.py` prints, the
  steps a plan without production or test code skips, the approval by checklist), notes to append verbatim, the
  question for the Product Manager;
- **the gates** due at step 7 for the tier (`gate-stamp.py`, which runs them and logs the stamp), and the review set
  of step 8 for the tier: `squad-reviewer`; with `squad-security` in mode `diff` on tier `security`; `squad-security`
  alone on `patch/security`.

Gates count only as the stamp rows of `gate-stamp.py`, never as a member's report: without a passing stamp on the code
head the next step is step 7 (`plan-check.py --matrix` first, a `MISSING` criterion to the Tester), a review round
belongs to the stamp it follows, and step 9 waits for the stamp to pass `gate-stamp.py --verify`. A review, a
Security verdict and the Devil's Advocate's objections are read from the member's `VERDICT:` line, which the
orchestrator logs as the start of the row's result (a Reviewer's `VERDICT: NON-BLOCKING <m>` counts as `BLOCKING 0`);
a row without one is named, and both ways are printed. A Reviewer's verdict line names the code SHA it reviewed
(`VERDICT: APPROVE on <sha>`); one that names none, or a commit whose code differs from the stamp of its round (a
later commit that touched only `specs/` holds the same code), ties the round to no stamp, and the Reviewer is
continued on the stamp's SHA. The Orchestrator's verbatim rows after a round subtract the findings they name by tag
(`[BLOCKING, verbatim]`, `[NON-BLOCKING, verbatim]`; `squad-log.py` refuses a row without one), so a
non-blocking finding applied verbatim needs no Lead launch, and a word "blocking" in its prose counts nothing; a
Lead `decide` row that orders a change (`FIX:` in capitals, with its colon) sends its owners, the gates and a delta
round before step 9. A new stamp whose code differs from the reviewed one only in the `Status` lines of decision
records and their index rows (step 9's record status) needs no new round, and the code check suggests `--verbatim`
for it. Three blocking rounds in a row (review pass 1
and two delta rounds) send the findings to the Lead (*Loop limits*), and so does a second `CHANGES_REQUIRED` of the Security plan
review; two plan-check revisions of one document without a clean run hand the findings to the Lead in mode `decide`.
Once the log has a row of step 4 or later the plan is approved: `plan-check.py` still runs before the PR, its size
limit only warns, and a finding — one a merge of `main` brought with new checks, say — goes to the Lead in mode
`revise` with the approved scope unchanged (`.squad/orchestration.md`, *Merge conflicts*).

The script recognises a step by the number at the start of its step cell (`2a` and `2b` in `squad-spec`), the member
and, in a feature, the task group in the cell (`6 Implement (G2)`); the step cells it prints are the ones it reads.
Steps 4–6 of a feature run per task group of `tasks.md`, in table order; group `PM` (the Product Manager's and the
Lead's tasks) runs none of them. An orchestrator row of step 4 that says it skipped the step (`4 Skeleton skipped`: the
plan adds no API after all) ends step 4 for its group, a row without a group for every group; the same holds for step
5 (`5 Tests first skipped (G3)`), and an orchestrator coverage row of step 6 without a group (`6 Coverage gate (all
groups)`) ends the coverage pass of every group implemented before it. `6 Implement skipped (G1)` ends step 6's
implementation of the group it names (steps 4–5 left no production code to write); it needs the group in a feature.
A group whose every task row names only documentation files (Markdown, READMEs, in parentheses too) has no skeleton,
no tests first and no coverage pass: the Dev writes its rows, the Tester its test documentation in mode coverage.

`--launch squad-<role> <mode>` prints that one launch — model, fresh or continued, the log command — for a launch the
log cannot foresee (a CI failure or a review comment in step 11). `--stdin` reads the log from standard input after
step 10 removed the work folder; `plan.md`, `spec.md` and `tasks.md` then come from the commit before that removal, and
so do the rows of `log.md` when the piped table starts at step 10 (a comment that links `log.md` holds only those).
The work folder is matched against the folders under `specs/`; the script writes nothing.

Usage, from anywhere inside the repository:
    python3 .squad/tools/squad-next.py <work folder> [--stdin]
    python3 .squad/tools/squad-next.py <work folder> --launch squad-dev fix [--stdin]

Exit code 0 when it printed the next step, 1 when the work folder has no log, 2 on a usage error.
"""
import argparse
import collections
import fnmatch
import importlib.util
import os
import re
import subprocess
import sys
import types

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
try:
    import squad_settings as settings  # noqa: E402  (per-repository settings next to this script)
except ImportError:  # the template's own tests run this file without a target's settings
    settings = types.SimpleNamespace()

LEAD = "squad-lead"
ADVOCATE = "squad-devils-advocate"
SECURITY = "squad-security"
TESTER = "squad-tester"
DEV = "squad-dev"
OFFICER = "squad-code-officer"
REVIEWER = "squad-reviewer"
ORCHESTRATOR = "orchestrator"
MANAGER = "product manager"
USER = "user"
CELLS = {LEAD: "Lead", ADVOCATE: "Devil's Advocate", SECURITY: "Security", TESTER: "Tester", DEV: "Dev",
         OFFICER: "Code Officer", REVIEWER: "Reviewer", ORCHESTRATOR: "Orchestrator", MANAGER: "Product Manager"}
MODES = {LEAD: ("spec", "plan", "revise", "decide", "approve-pr"), ADVOCATE: ("spec", "plan"),
         SECURITY: ("plan", "diff"), TESTER: ("tests-first", "coverage"), DEV: ("skeleton", "implement", "fix"),
         OFFICER: ("code-check",), REVIEWER: ("review",)}
# Continuing a member (.squad/orchestration.md): always fresh / the member's last agent / its last agent of the mode.
FRESH = "fresh"
LAST = "last"
SAME = "same"
FRESH_MODES = {(LEAD, "spec"), (LEAD, "plan"), (LEAD, "decide"), (LEAD, "approve-pr"), (ADVOCATE, "spec"),
               (ADVOCATE, "plan")}
LAST_MODES = {(LEAD, "revise"), (DEV, "fix")}
# The mode of a fresh launch: a word of its step cell, else its step number, else the member's default.
MODE_WORDS = ((LEAD, "decide", "decide"), (LEAD, "approv", "approve-pr"), (DEV, "skeleton", "skeleton"),
              (DEV, "implement", "implement"), (DEV, "document", "implement"), (DEV, "fix", "fix"),
              (TESTER, "tests first", "tests-first"), (TESTER, "tests-first", "tests-first"), (TESTER, "coverage", "coverage"))
STEP_MODES = {(LEAD, "2a"): "spec", (LEAD, "9"): "approve-pr", (ADVOCATE, "2a"): "spec", (SECURITY, "3"): "plan",
              (DEV, "4"): "skeleton", (DEV, "6"): "implement", (TESTER, "5"): "tests-first"}
DEFAULT_MODES = {LEAD: "plan", ADVOCATE: "plan", SECURITY: "diff", TESTER: "coverage", DEV: "fix",
                 OFFICER: "code-check", REVIEWER: "review"}
# The step cell --launch logs a mode under (after step 10: 11).
MODE_STEPS = {"spec": "2a Spec", "plan": "2 Plan", "revise": "2 Plan revise", "decide": "Decide",
              "approve-pr": "9 PR approval", "skeleton": "4 Skeleton", "tests-first": "5 Tests first",
              "implement": "6 Implement", "coverage": "6 Coverage", "fix": "7 Fix", "review": "8 Review delta",
              "diff": "8 Security diff", "code-check": "7 Code check"}
STEP = re.compile(r"^\s*(\d+)([ab])?\b")
GROUP = re.compile(r"\b(G\d+)\b")
RESULT = re.compile(r"RESULT:\s*(DONE|NO CHANGE|ESCALATE|DECIDED|NOT APPROVED|APPROVED)", re.I)
# A note in parentheses after a count (`1 major (N3 unsatisfiable), 5 minor (…)`), one level of nesting deep.
NOTE = r"(?:\s*\((?:[^()]|\([^()]*\))*\))?"
REVIEW = re.compile(r"VERDICT:\s*(?:(?P<approve>APPROVE)\b(?!D)" + NOTE + r"|BLOCKING\s+(?P<blocking>\d+)" + NOTE +
                    r"(?:\s*\\?\|\s*NON-BLOCKING\s+(?P<nonblocking>\d+)" + NOTE + r")?|NON-BLOCKING\s+(?P<only>\d+)"
                    + NOTE + r")(?:\s+on\s+`?(?P<sha>[0-9a-f]{7,40})\b)?", re.I)
# A Lead result that keeps a spec over its size warning and says why (`kept the length: …`, `size: …`).
SIZE_REASON = re.compile(r"\b(?:size|length|word count|words)\b", re.I)
# The ` on <sha>` that ends a Reviewer's verdict clause.
VERDICT_SHA = re.compile(r"\bon\s+`?[0-9a-f]{7,40}\b")
SECURITY_VERDICT = re.compile(r"VERDICT:\s*(APPROVED|CHANGES_REQUIRED)", re.I)
NONBLOCKING_FINDING = re.compile(r"\[NON-BLOCKING\b", re.I)
OBJECTIONS = re.compile(r"VERDICT:\s*(?:(?P<none>NO OBJECTIONS)|OBJECTIONS\s+(?P<major>\d+)\s+major" + NOTE +
                        r",\s*(?P<minor>\d+)\s+minor)", re.I)
WAITS = re.compile(r"\bwaits? for\b|\bposted for agreement\b", re.I)
# The orchestrator's approval row of step 9 starts with these words; a mention of the Lead's NOT APPROVED later in
# its prose decides nothing (Censio#140).
CHECKLIST = re.compile(r"^\s*(?:re-)?approved by checklist\b", re.I)
# A step-5 Tester row names its deferred tests after `Deferred:` (`Deferred: none` without one).
DEFERRED = re.compile(r"\bDeferred:\s*(?P<names>[^\n|]*)", re.I)
# A Dev row that removed the code a deferred test was deferred for (`removed the fallback branch of test_a`).
REMOVED = re.compile(r"\b(?:remov|delet|drop)", re.I)
# A step-11 Orchestrator row that the PR was merged or closed: the watching ends, step 12 runs at once.
PR_ENDED = re.compile(r"\b(?:merged|closed)\b", re.I)
# A continuation that a fix, delta or revise round makes in another mode is no change of mode.
ROUND_OF_SAME_MODE = re.compile(r"\b(?:fix|delta|revis)", re.I)
READY = re.compile(r"\bready for review\b", re.I)
# A plan line that asks for an early draft PR: `Early draft PR: yes` (`routing.md`, *Early draft PR*); anything else, `no`
# included, does not. `plan-check.py` warns about a plan that names one in prose without the line.
EARLY_PR_TAG = re.compile(r"^\s*(?:[-*]\s*)?Early draft PR:\s*yes\b", re.I)
SPEC_SETTLED = re.compile(r"\bspec (?:approved|corrected)\b", re.I)
CORRECTED = re.compile(r"\bspec corrected\b", re.I)
NOTES = re.compile(r"^#{2,6}[ \t]+Challenge(?: notes)?\b", re.I | re.M)
NOTES_ROW = re.compile(r"\bnotes\b", re.I)
# A `7 Manual test` row that reports a failure (`FAIL` in capitals), and the task groups a failed manual test may
# add before the Lead escalates (*Loop limits*, *Manual criterion fails*).
MANUAL_FAILED = re.compile(r"\bFAIL(?:ED|S)?\b")
# The Tester's `6 Private check` row says the same in any case ("AC1 failed: ...").
# The Tester's `6 Private check` row states `PASS` or `FAIL` in capitals per criterion; one `FAIL` fails the row.
VERDICT_PASS = re.compile(r"\bPASS(?:ED|ES)?\b")
MANUAL_GROUPS = 2
CONTINUATION_CAP = 5
# A continuation adds tens of thousands of tokens: from here on the next launch is fresh, so the round never ends past
# the 300,000 where `squad-log.py` warns (Censio#38).
FRESH_TOKENS = 250_000
SPEC_APPROVAL = getattr(settings, "SPEC_APPROVAL", "lead")
LOG_TOOL = "python3 .squad/tools/squad-log.py"
NOTES_TOOL = ".squad/tools/squad-notes.py"
STDIN = " --stdin < log.md > log-updated.md"
ISSUE_HANDOVER = "the issue text and the work folder"
STAMP_TOOL = "python3 .squad/tools/gate-stamp.py"
SUMMARY = "squad-log.py --summary"
# What the result cell starts with: the member's verdict line, which this script reads back.
RESULT_HINTS = {LEAD: "<its RESULT line>, then the result", ADVOCATE: "<its VERDICT line>, then the objections",
                SECURITY: "<its VERDICT line>, then the findings", REVIEWER: "<its VERDICT line>, then the findings"}
# The exact verdict lines this script reads, quoted into every launch's hand-over so the member does not depend on
# its agent file being current, and named by `squad-log.py` when a logged row carries none.
VERDICT_FORMATS = {
    REVIEWER: "`VERDICT: APPROVE on <sha>` or `VERDICT: BLOCKING <n> | NON-BLOCKING <m> on <sha>` (<sha>: all 40 "
              "characters of `git rev-parse` of the head it reviewed; `NON-BLOCKING <m> (<k> plan)` when <k> of them "
              "only ask for what the plan already orders)",
    ADVOCATE: "`VERDICT: NO OBJECTIONS` or `VERDICT: OBJECTIONS <n> major, <m> minor`",
    SECURITY: "`VERDICT: APPROVED` or `VERDICT: CHANGES_REQUIRED` (mode diff, approved with non-blocking findings: "
              "`VERDICT: APPROVED | NON-BLOCKING <n>`)",
}
DECIDE_FORMAT = ("`RESULT: DECIDED — <option>`; on review findings `FIX: <Dev|Tester|orchestrator|Code Officer> — "
                 "<change>` per `;`-separated item and `ISSUE: <each linked issue>` after the dash, `FIX:` in capitals "
                 "whenever it orders a change")
FIX_OWNER = re.compile(r"^\s*(?:FIX:\s*)?(dev|tester|orchestrator|code officer)\b", re.I)
# A Lead decision that orders a change: `FIX:` in capitals, with its colon ("no FIX needed" orders none).
FIX_ORDERED = re.compile(r"\bFIX:")
# An orchestrator row of steps 4–6 that says it skipped the step (`4 Skeleton skipped`, `6 Implement skipped (G1)`).
SKIPPED = re.compile(r"\bskip", re.I)
# The non-blocking findings of a Reviewer's verdict line that only ask for what the plan already orders
# (`NON-BLOCKING 2 (1 plan)`): their owner fixes them without a Lead decision (PlexToJellyfinSync#292).
PLAN_FINDINGS = re.compile(r"NON-BLOCKING\s+\d+\s*\((\d+)\s+plan\b", re.I)
# The tags a verbatim row names its findings by, as the Reviewer wrote them (`[NON-BLOCKING, verbatim]`).
NONBLOCKING_TAG = re.compile(r"\[NON-BLOCKING, (?:plan, )?verbatim\]", re.I)
PLAN_VERBATIM_TAG = re.compile(r"\[NON-BLOCKING, plan, verbatim\]", re.I)
BLOCKING_TAG = re.compile(r"\[BLOCKING, verbatim\]", re.I)
STATUS_FIELD = re.compile(r"^[+-]- \*\*Status:\*\*")
DECISIONS = "docs/decisions/"
DO = "  Do: "
FIX_TESTS = "Fix tests"
THEN = "  Then: "
BASE_HANDOVER = "the base ref, the head SHA"
REVIEW_HANDOVER = ("the base ref, the exact code SHA of the stamp, {record} and the stamp's gate output (the file "
                   "its `Full gate output:` line names); later commits during the round may only touch specs/")

Row = collections.namedtuple("Row", "index step number who result")

_MODULES = {}


def module(name):
    """A tool next to this script as a module, loaded once: `squad-log`, `gate-stamp` or `plan-check`."""
    if name not in _MODULES:
        spec = importlib.util.spec_from_file_location(name.replace("-", "_"), os.path.join(HERE, f"{name}.py"))
        loaded = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(loaded)
        _MODULES[name] = loaded
    return _MODULES[name]


def squad_log():
    return module("squad-log")


def stamp_tool():
    return module("gate-stamp")


def plan_check():
    return module("plan-check")


def parse_rows(raw):
    """The log rows with their step number (`2a`), the member as its agent (`squad-dev`) or `orchestrator`, `product
    manager` and `user`."""
    rows = []
    for index, (_, step, member, result) in enumerate(raw):
        match = STEP.match(step)
        number = match.group(1) + (match.group(2) or "") if match else ""
        who = member.strip().lower()
        if who not in (ORCHESTRATOR, MANAGER, USER):
            who = squad_log().member_agent(member)
        rows.append(Row(index, step, number, who, result))
    return rows


class Run:
    """The state of one squad run: its rows, its documents and what they decide."""

    def __init__(self, root, name, raw_rows, after_pr):
        self.root, self.name, self.raw, self.after_pr = root, name, raw_rows, after_pr
        self.rows = parse_rows(raw_rows)
        read = stamp_tool().work_file
        self.plan, self.spec, self.tasks = (read(root, name, f) for f in ("plan.md", "spec.md", "tasks.md"))
        self.folder = os.path.join(root, "specs", name)
        self.feature = name.startswith("feature-") or self.spec is not None
        self.tier = stamp_tool().run_tier(self.plan, raw_rows)
        self.base_tier = (self.tier or "").split("/")[0]
        self.plan_step = "2b" if self.feature else "2"

    def of(self, who, numbers=None, after=-1, word=None):
        """The rows of `who` (an agent, `orchestrator`, …) after the row index `after`, at one of the step numbers,
        whose step cell holds `word` (a regular expression)."""
        return [row for row in self.rows if row.who == who and row.index > after
                and (numbers is None or row.number in numbers)
                and (word is None or re.search(word, row.step, re.I))]

    def last(self, who, numbers=None):
        found = self.of(who, numbers)
        return found[-1] if found else None

    def step_cell(self, number, text):
        return f"{'11' if self.after_pr else number} {text}"

    def code_free(self):
        return stamp_tool().code_free(self.plan)

    def section(self, title):
        return plan_check().sections(self.plan or "").get(title, "")


def none_text(body):
    """True when a section body is empty or says "none"."""
    text = body.strip()
    return not text or bool(re.match(r"^(?:[-*]\s+)?none\b", text, re.I))


def trailer_of(row):
    """(model/effort or None, tokens, continued, the step its continuation names or None) of a launch row."""
    log = squad_log()
    trailer = log.TRAILER.search(row.result) or log.PENDING_TRAILER.search(row.result)
    if trailer is None:
        return None, 0, False, None
    tokens = int((trailer.groupdict().get("tokens") or "0").replace(",", ""))
    return trailer.group("launch"), tokens, bool(trailer.group("continued")), trailer.group("origin")


def agent_chains(run, agent):
    """The agent's launches as chains {step, mode, launch, tokens, continuations, mixed}, the index of its last fresh
    launch and the index of the chain of its last row. "mixed" is the step cell of a continuation logged in another
    mode than the chain's own (not a fix, delta or revise round, and not the Lead's plan after its spec), or None."""
    chains, by_step, fresh, last = [], {}, None, None
    for row in run.of(agent):
        model, tokens, continued, origin = trailer_of(row)
        if not continued:
            fresh = last = by_step[row.step] = len(chains)
            chains.append({"step": row.step, "mode": mode_of(agent, row.number, row.step), "tokens": tokens,
                           "launch": model, "continuations": 0, "mixed": None})
            continue
        index = by_step.get(origin) if origin else fresh
        if index is not None:
            chain = chains[index]
            chain["tokens"] += tokens
            chain["continuations"] += 1
            if chain["mixed"] is None and other_mode(agent, chain["mode"], row):
                chain["mixed"] = row.step
            by_step.setdefault(row.step, index)
            last = index
    return chains, fresh, last


def other_mode(agent, mode, row):
    """True when a continuation row of `agent` works in another mode than its chain's `mode` (Censio#140: tests-first
    continued into coverage, skeleton into implement)."""
    if ROUND_OF_SAME_MODE.search(row.step) or (agent, mode) == (LEAD, "spec"):
        return False
    return mode_of(agent, row.number, row.step) != mode


def mode_of(agent, number, step):
    """The mode of a fresh launch, from its step cell."""
    low = step.lower()
    for owner, word, mode in MODE_WORDS:
        if owner == agent and word in low:
            return mode
    if (agent, "spec") in FRESH_MODES and "spec" in low:
        return "spec"
    return STEP_MODES.get((agent, number), DEFAULT_MODES.get(agent, ""))


def expected_model(run, agent):
    """(model/effort the launch runs on, where that comes from)."""
    log = squad_log()
    base = log.agent_launch(run.root, agent)
    override = log.LAUNCH_OVERRIDES.get(agent)
    if base and override and run.tier == override[0]:
        return f"{override[1]}/{base.split('/', 1)[1]}", ("*Launch model* (.squad/orchestration.md): pass it "
                                                          "explicitly on this launch")
    return base or "unknown", f".claude/agents/{agent}.md"


def default_rule(agent, mode):
    if (agent, mode) in FRESH_MODES:
        return FRESH
    return LAST if (agent, mode) in LAST_MODES else SAME


def pick_chain(run, agent, mode, rule):
    """(the chain to continue or None, why a fresh launch, whether it is the member's last fresh launch)."""
    if rule == FRESH:
        return None, f"the first launch of mode {mode}", False
    chains, fresh, last = agent_chains(run, agent)
    if rule == LAST:
        index = last
    else:
        same = [i for i, chain in enumerate(chains) if chain["mode"] == mode]
        index = same[-1] if same else None
    if index is None:
        return None, (f"no earlier {agent} launch" if rule == LAST else f"the first launch of mode {mode}"), False
    chain = chains[index]
    why = spent(run, agent, chain)
    return (None, why, False) if why else (chain, None, index == fresh)


def spent(run, agent, chain):
    """Why the agent of `chain` is not continued again (*Continuing a member*), or None: the cap, a continuation in
    another mode, or another model than *Launch model* names."""
    if chain["continuations"] >= CONTINUATION_CAP or chain["tokens"] >= FRESH_TOKENS:
        return (f"the agent of \"{chain['step']}\" was continued {chain['continuations']} time(s) at "
                f"{chain['tokens']:,} tokens: from {CONTINUATION_CAP} continuations or {FRESH_TOKENS:,} "
                "tokens on, a fresh launch with a short hand-over")
    if chain["mixed"]:
        return (f"the agent of \"{chain['step']}\" was continued in another mode at \"{chain['mixed']}\": it holds two "
                "modes' work, so a fresh launch with a short hand-over")
    expected, _ = expected_model(run, agent)
    if chain["launch"] and "/" in expected and squad_log().model_warning(agent, chain["launch"], expected, False):
        return (f"the agent of \"{chain['step']}\" ran on {chain['launch']}, not {expected}: never continued, a "
                "fresh launch with the right model and a short hand-over")
    return None


def log_command(run, step, who, continuation=""):
    stdin = STDIN if run.after_pr else ""
    hint = RESULT_HINTS.get(who, "<the result>")
    return f"  Log: {LOG_TOOL} {run.name} \"{step}\" \"{CELLS[who]}\" \"{hint}\" --agent-id <id>{continuation}{stdin}"


def launch(run, agent, mode, step, handover, rule=None, purpose=""):
    """The lines of one launch: agent and mode, model, fresh or continued, hand-over and log command."""
    chain, why, is_last = pick_chain(run, agent, mode, rule or default_rule(agent, mode))
    model, source = expected_model(run, agent)
    lines = [f"  Launch: {agent}, mode {mode}" + (f" — {purpose}" if purpose else ""), f"  Model: {model} ({source})"]
    if chain is None:
        lines.append(f"  Agent: fresh launch, a new agent (no SendMessage to an earlier one) — {why}")
        continuation = ""
    else:
        lines.append(f"  Agent: continue the agent of \"{chain['step']}\" ({chain['continuations']} continuation(s), "
                     f"{chain['tokens']:,} tokens) — the agent id that row was logged with, not simply the member's "
                     "last one; fresh with the same hand-over instead after a session restart (the agent id is gone) "
                     "or when that launch ended on an API error")
        continuation = " --continued" if is_last else f" --continued-from \"{chain['step']}\""
    short = " — short: the plan or task group, what earlier rounds settled, the open findings, the head SHA" \
        if why and "short hand-over" in why else ""
    lines.append(f"  Hand over: {handover}{short}")
    verdict = verdict_format(agent, mode)
    if verdict:
        place = "it starts the report" if agent == REVIEWER else "it ends the report"
        lines.append(f"  Verdict line: quote it in the prompt, exactly as here; {place}: {verdict}")
    lines.append(log_command(run, step, agent, continuation))
    return lines


def verdict_format(agent, mode):
    """The exact verdict or result line a launch of `agent` in `mode` reports, or None."""
    if agent == LEAD:
        return DECIDE_FORMAT if mode == "decide" else None
    return VERDICT_FORMATS.get(agent)


def fix_ordered(result):
    """True when a Lead decision orders a change: `FIX:` in capitals, with its colon."""
    return bool(FIX_ORDERED.search(result))


def row_format_problem(agent, result, step):
    """The fixed format beyond the verdict line that a row lacks, or None: the Tester's `6 Private check` row states
    `PASS` or `FAIL`, and every `FIX:` item of a Lead decision names its owner (`DECIDE_FORMAT`)."""
    if agent == TESTER and re.search(r"private check", step or "", re.I) and private_verdict(result) is None:
        return "a capital `PASS` or `FAIL` per criterion"
    if agent == LEAD and fix_ordered(result) and not all(FIX_OWNER.match(item) for item in fix_items(result)
                                                         if item.strip()):
        return "`FIX: <Dev|Tester|orchestrator|Code Officer> — <change>` per `;`-separated item"
    return None


def verdict_problem(agent, result, step=None):
    """For `squad-log.py`: the exact verdict line (or fixed format, `row_format_problem`) the row of `agent` lacks, or
    None when it carries one (or the member reports none)."""
    problem = row_format_problem(agent, result or "", step)
    if problem:
        return problem
    checks = {REVIEWER: REVIEW, ADVOCATE: OBJECTIONS, SECURITY: SECURITY_VERDICT, LEAD: RESULT}
    found = checks[agent].search(result or "") if agent in checks else None
    if agent not in checks or (found and not (agent == REVIEWER and partial_review(found, result))):
        return None
    return "a `RESULT:` line (`.claude/agents/squad-lead.md`, *Output format*)" if agent == LEAD else \
        VERDICT_FORMATS[agent]


def partial_review(found, result):
    """True when a Reviewer's verdict line holds a non-blocking count or a code SHA the match left out: a note the
    pattern cannot read cut it short. A non-blocking count is looked for in the verdict clause only (up to ` on
    <sha>`): "no blocking or non-blocking findings" after it is prose (PiMonitor#215, Censio#185)."""
    line = result[found.start():].split("\n", 1)[0]
    clause = VERDICT_SHA.split(line, 1)[0].upper()
    lost_count = "NON-BLOCKING" in clause and found.group("nonblocking") is None and found.group("only") is None
    lost_sha = VERDICT_SHA.search(line) is not None and found.group("sha") is None
    return lost_count or lost_sha


def orchestrator_log(run, step, result):
    stdin = STDIN if run.after_pr else ""
    return f"  Log: {LOG_TOOL} {run.name} \"{step}\" Orchestrator \"{result}\"{stdin}"


def heading(text):
    return [f"Next: {text}"]


# Verdicts --------------------------------------------------------------------------------------------------------


def review_verdict(row):
    """(blocking, non-blocking) of a Reviewer or Security diff row, or None without a VERDICT line."""
    if row.who == SECURITY:
        found = SECURITY_VERDICT.search(row.result)
        return None if not found else (int(found.group(1).upper() != "APPROVED"), security_nonblocking(row, found))
    found = REVIEW.search(row.result)
    if not found:
        return None
    if found.group("approve"):
        return 0, 0
    if found.group("only"):
        return 0, int(found.group("only"))
    return int(found.group("blocking")), int(found.group("nonblocking") or 0)


def security_nonblocking(row, found):
    """The non-blocking findings of a Security diff row: the count its verdict names, else its `[NON-BLOCKING …]` tags
    (PiMonitor#235: on `patch/security` they went to step 10 undecided)."""
    if row.number not in ("8", "11"):
        return 0
    count = re.match(r"[ \t]*(?:\\?\|[ \t]*)?NON-BLOCKING[ \t]+(\d+)", row.result[found.end():], re.I)
    return int(count.group(1)) if count else len(NONBLOCKING_FINDING.findall(row.result))


def objections(row):
    """(major, minor) of a Devil's Advocate row, or None without a VERDICT line."""
    found = OBJECTIONS.search(row.result)
    if not found:
        return None
    return (0, 0) if found.group("none") else (int(found.group("major")), int(found.group("minor")))


def lead_result(row):
    found = RESULT.search(row.result) if row else None
    return found.group(1).upper() if found else None


def no_verdict(row):
    lines = [f"  Note: the row \"{row.step} · {CELLS.get(row.who, row.who)}\" carries no VERDICT line; the next step "
             "follows its report:"]
    if row.who in VERDICT_FORMATS:
        lines.append(f"  When the report states its counts, correct the row's result instead of a new launch: "
                     f"{LOG_TOOL} <folder> --replace-last \"{CELLS[row.who]}\" --step \"{row.step}\" --new-result "
                     f"\"<the verdict line>, then the result\" — the line: {VERDICT_FORMATS[row.who]}")
    return lines


# Stages ----------------------------------------------------------------------------------------------------------


def intake(run):
    if run.of(ORCHESTRATOR, {"1"}) or run.rows:
        return None
    return heading("step 1 — Intake (*Bug or feature* first)") + [
        DO + "decide bug or feature, branch off the latest main, create the work folder from specs/_template/, commit "
        "and push",
        orchestrator_log(run, "1 Intake", "<feature|bug>: <what decided it>; branch <name>")]


def escalation(run):
    lead = run.last(LEAD)
    if lead is None or lead_result(lead) != "ESCALATE":
        return None
    answers = [row for row in run.rows if row.index > lead.index and row.who in (MANAGER, USER)]
    if answers:
        chains, _, last = agent_chains(run, LEAD)
        mode = chains[last]["mode"] if last is not None else "plan"
        return heading("the Product Manager answered the Lead's escalation") + launch(
            run, LEAD, mode, f"{lead.number} {mode.capitalize()} (answer)", "the Product Manager's answer, verbatim",
            rule=LAST)
    return heading("the Lead escalates") + [
        DO + "relay the Lead's question verbatim, with its options and recommendation, to the Product Manager and wait",
        f"  Log: {LOG_TOOL} {run.name} \"{lead.step}\" \"Product Manager\" \"<the answer>\"",
        THEN + "continue the Lead with the answer (run this script again)"]


def waiting(run):
    last = run.rows[-1] if run.rows else None
    if last is None or last.who != ORCHESTRATOR or not WAITS.search(last.result):
        return None
    return heading("the run waits (*Outside authors*, *Spec approval* in .squad/orchestration.md)") + [
        DO + "push the work branch and stop; the user resumes the run with squad-spec on the same issue or branch, and "
        "the answer is logged as a Product Manager row (spec approved, or spec corrected with the corrections)"]


def no_change(run):
    lead = run.last(LEAD)
    if lead is None or lead_result(lead) != "NO CHANGE":
        return None
    if run.of(ORCHESTRATOR, after=lead.index):
        return wrap_up(run) or finished(run)
    return heading("Outcome \"no change\" (.squad/routing.md)") + [
        DO + "show the Lead's proposed issue comment to the Product Manager, post it after confirmation with the log as "
        "a collapsed \"Squad working record\" block (with an issue), remove the work folder with a commit, push; no PR",
        orchestrator_log(run, run.step_cell("2", "No change"), "comment posted: <url>"),
        THEN + "step 12"]


def spec_stage(run):
    """Step 2a of a feature: spec, plan-check --spec, spec challenge, spec approval."""
    if not run.feature or run.base_tier == "docs":
        return None
    if run.spec is None:
        return heading("step 2a — Spec") + launch(run, LEAD, "spec", "2a Spec",
                                                   "the issue text (or the user's description) and the work folder")
    challenge = run.of(ADVOCATE, {"2a"})
    return spec_check(run, challenge) or spec_challenge(run, challenge) or spec_approval(run)


def spec_check(run, challenge):
    """plan-check.py --spec before the spec challenge; after it, plan-check.py of the whole folder runs in 2b."""
    if (challenge and run.of(LEAD, {"2b"})) or not os.path.isdir(run.folder):
        return None
    errors, warnings = plan_check().check_spec_only(run.folder)
    # Vandox#440: a reason for the length in the Lead's last result settles the warning without another launch.
    # The first spec row was written before the warning was shown: only a later row's reason counts.
    sized = run.of(LEAD, {"2a"}, word="size") or [row for row in run.of(LEAD, {"2a"})[1:][-1:]
                                                  if SIZE_REASON.search(row.result)]
    if not errors and (not warnings or sized):
        return None
    since = max([row.index for row in challenge + run.of(MANAGER, {"2a"})] + [-1])
    rounds = run.of(LEAD, {"2a"}, after=since, word="plan-check")
    findings = errors or warnings
    lines = heading("step 2a — plan-check.py --spec findings") + [f"  Finding: {line}" for line in findings]
    if errors and len(rounds) >= 2:
        return lines + launch(run, LEAD, "decide", "2a Decide (plan-check)",
                              "the findings left after two revisions: it fixes them or escalates")
    cell = "2a Spec revise (plan-check)" if errors else "2a Spec revise (size)"
    return lines + launch(run, LEAD, "revise", cell, "the findings only (no challenge round)" if errors else
                          "the size warning: shorten the spec, or keep it and give the reason in the result")


def spec_challenge(run, challenge):
    if not challenge:
        return heading("step 2a — Spec challenge") + launch(run, ADVOCATE, "spec", "2a Spec challenge",
                                                             ISSUE_HANDOVER)
    verdict = objections(challenge[-1])
    if verdict is None:
        return heading("step 2a — route the spec challenge") + no_verdict(challenge[-1]) + [
            "  major objection: continue the Lead (mode revise) with all objections; minor only: append them verbatim "
            "to spec.md as *Challenge notes*"]
    major, minor = verdict
    if major and not run.of(LEAD, {"2a"}, after=challenge[-1].index):
        return heading("step 2a — a major spec objection") + launch(
            run, LEAD, "revise", "2a Spec revise (challenge)",
            "all objections, minor ones included: it answers each in the spec's *Challenge* section")
    if minor and not major and not notes_settled(run, run.spec, {"2a"}, challenge[-1]):
        return heading("step 2a — minor spec objections") + [
            DO + "append the minor objections verbatim to spec.md as a *Challenge notes* section (the Lead answers them "
            f"while it plans) with `python3 {NOTES_TOOL} {run.name} challenge --spec --agent-id <the Devil's Advocate's "
            "id>`, and apply each exact edit of spec.md a note states (*Plan challenge*); commit",
            orchestrator_log(run, "2a Challenge notes", "minor objections appended verbatim to spec.md")]
    return None


def notes_written(document):
    """True when a section of NOTES holds a line that is not the plan template's own (its *Challenge* section says
    "Left out when the plan is written", which settles nothing)."""
    template = plan_check().template_lines()
    level = None
    for line in document.splitlines():
        heading = re.match(r"^(#{1,6})[ \t]", line)
        if heading and level is not None and len(heading.group(1)) <= level:
            level = None
        if NOTES.match(line):
            level = len(heading.group(1))
        elif level is not None and line.strip() and line.strip() not in template:
            return True
    return False


def notes_settled(run, document, numbers, challenge):
    """True when the minor objections of `challenge` are in `document`: its *Challenge notes* section is there, the
    orchestrator logged the append, or a later Lead launch folded or answered them (Censio#140: a Lead `decide` that
    folds the notes must not send the orchestrator back to append them again). The Lead's *Challenge* section that
    answers them counts as the notes, and an orchestrator's notes row counts at any step number (PiMonitor#228)."""
    if notes_written(document or ""):
        return True
    later = [row for row in run.rows if row.index > challenge.index]
    return any(row.who == ORCHESTRATOR and NOTES_ROW.search(f"{row.step} {row.result}") for row in later) or \
        any(row.who == LEAD and row.number in numbers for row in later)


def spec_approval(run):
    settled = run.of(MANAGER, {"2a"}, word="approv")
    if SPEC_APPROVAL == "pm" and not [row for row in settled if SPEC_SETTLED.search(row.result)]:
        return heading("step 2a — Spec approval by the Product Manager (SPEC_APPROVAL = \"pm\")") + [
            DO + "show the Product Manager the summary *Spec approval* (.squad/orchestration.md) names, with a link to "
            "spec.md on the pushed work branch, and wait",
            f"  Log: {LOG_TOOL} {run.name} \"2a Spec approval\" \"Product Manager\" \"spec approved | spec corrected: …\""]
    corrected = [row for row in settled if CORRECTED.search(row.result)]
    if corrected and not run.of(LEAD, {"2a", "2b"}, after=corrected[-1].index):
        return heading("step 2a — the Product Manager corrected the spec") + launch(
            run, LEAD, "revise", "2a Spec revise (approval)", "the Product Manager's corrections")
    return None


def plan_written(run):
    """Step 2 (2b): the Lead's plan, and plan-check.py after every Lead write."""
    if run.base_tier in ("docs", "patch"):
        return None
    if not run.of(LEAD, {run.plan_step}):
        if run.feature:
            note = [] if SPEC_APPROVAL == "pm" else ["  Note: an installer or deployment feature has the Product Manager "
                                                     "approve the spec first, whatever SPEC_APPROVAL says"]
            return heading("step 2b — Plan") + note + launch(
                run, LEAD, "plan", "2b Plan", "the work folder (and the Product Manager's answer, if any)", rule=LAST)
        return heading("step 2 — Plan") + launch(run, LEAD, "plan", "2 Plan", ISSUE_HANDOVER)
    if run.plan is None:
        return heading(f"step {run.plan_step} — plan.md is missing") + launch(
            run, LEAD, "revise", f"{run.plan_step} Plan revise", "plan.md is missing: write it (or return the docs/patch "
            "result with its tier)")
    return plan_findings(run)


def approved_at(run):
    """The index of the run's first row of step 4 or later, from which on the plan counts as approved, or None."""
    return next((row.index for row in run.rows if row.number and int(STEP.match(row.number).group(1)) >= 4), None)


def plan_findings(run):
    if run.after_pr or not os.path.isdir(run.folder):
        return None
    approved = approved_at(run)
    errors, warnings = plan_check().check(run.folder, approved=approved is not None)
    if not errors:
        return None
    since = approved if approved is not None else max(
        [row.index for row in run.rows if row.who in (ADVOCATE, SECURITY) and row.number in ("2", "2b", "3")] + [-1])
    rounds = run.of(LEAD, {run.plan_step}, after=since, word="plan-check")
    title = "plan-check.py findings" + (" on the approved plan" if approved is not None else "")
    lines = heading(f"step {run.plan_step} — {title}") + [f"  Finding: {line}" for line in errors] + [
        f"  Warning: {line}" for line in warnings]
    if len(rounds) >= 2:
        return lines + launch(run, LEAD, "decide", f"{run.plan_step} Decide (plan-check)",
                              "the findings left after two revisions: it fixes them or escalates")
    scope = "the findings only, the approved scope unchanged" if approved is not None else "the findings only"
    if approved is None and open_plan_verdicts(run):
        # DockerUpdateGuard#439: plan check and challenge came back together; one revision answers both, and
        # `revision` counts its row as the answer to the challenge and to Security.
        scope = ("the findings, all objections (minor ones included) and Security's CHANGES_REQUIRED findings logged "
                 "before it: one revision answers them together")
    return lines + launch(run, LEAD, "revise", f"{run.plan_step} Plan revise (plan-check)", f"{scope} (no challenge round)")


def plan_revisions(run):
    """The Lead's revise and decide rows of the plan; a plan-check revise only when it answered the verdicts too."""
    return [row for row in run.of(LEAD, {run.plan_step, "2", "3"}) if re.search(r"revise|decide", row.step, re.I)
            and (not re.search("plan-check", row.step, re.I) or answers_verdicts(run, row))]


def answers_verdicts(run, revise):
    """True when a plan-check revise came after a major objection or a CHANGES_REQUIRED: its hand-over named them, so
    it answers them too (DockerUpdateGuard#439); otherwise it answers the findings alone."""
    return any(row.index < revise.index for row in open_plan_verdicts(run, before=revise.index))


def open_plan_verdicts(run, before=None):
    """The plan challenge's major objections and Security's CHANGES_REQUIRED that no Lead revision (before the row
    index `before`, when given) answered yet."""
    revised = max([row.index for row in run.of(LEAD, {run.plan_step, "2", "3"})
                   if re.search(r"revise|decide", row.step, re.I) and (before is None or row.index < before)] + [-1])
    challenge = [row for row in run.of(ADVOCATE, {run.plan_step, "2"}) if row.index > revised
                 and (objections(row) or (0, 0))[0]]
    rejected = [row for row in run.of(SECURITY, {"3"}) if row.index > revised
                and security_verdict(row) == "CHANGES_REQUIRED"]
    return challenge + rejected


def challenge_and_security(run):
    """The plan challenge and Security's plan review (steps 2–3), in parallel on one plan commit."""
    if run.base_tier in ("docs", "patch") or run.plan is None:
        return None
    due, reason = plan_check().challenge_verdict(run.plan)
    areas = plan_check().areas_value(run.plan) or ""
    security_due = not plan_check().NONE.match(areas)
    challenge = run.of(ADVOCATE, {run.plan_step, "2"})
    skipped = [row for row in run.of(ORCHESTRATOR, {run.plan_step, "2"})
               if re.search(r"challenge skipped", f"{row.step} {row.result}", re.I)]
    reviews = run.of(SECURITY, {"3"})
    lines = []
    if not due and not skipped:
        lines += heading(f"step {run.plan_step} — log the skipped challenge") + [
            orchestrator_log(run, f"{run.plan_step} Challenge skipped", f"challenge skipped — {reason}")]
    if due and not challenge:
        lines += heading(f"step {run.plan_step} — Plan challenge ({reason})") + launch(
            run, ADVOCATE, "plan", f"{run.plan_step} Plan challenge", ISSUE_HANDOVER)
    if security_due and not reviews:
        lines += heading(f"step 3 — Plan security review (Security areas touched: {areas})") + launch(
            run, SECURITY, "plan", "3 Plan security", "the work folder (a feature: spec.md and plan.md together)")
    if lines:
        return lines + ([THEN + "the two run in parallel on the same plan commit"] if due and security_due else [])
    return revision(run, challenge, reviews, security_due)


def revision(run, challenge, reviews, security_due):
    """One Lead revision answers the major objections and a CHANGES_REQUIRED together; Security confirms it once."""
    verdict = objections(challenge[-1]) if challenge else (0, 0)
    security = [security_verdict(row) for row in reviews]
    if verdict is None or None in security:
        missing = challenge[-1] if verdict is None else reviews[security.index(None)]
        return heading("steps 2–3 — route the verdicts") + no_verdict(missing) + [
            "  a major objection or CHANGES_REQUIRED: the Lead (mode revise) with all of them; minor objections only: "
            "append them verbatim as *Challenge notes*; APPROVED with notes: append them as *Security notes* (both with "
            f"{NOTES_TOOL})"]
    revisions = plan_revisions(run)
    answered = [row for row in revisions if reviews and row.index > reviews[-1].index]
    rejected = bool(security) and security[-1] == "CHANGES_REQUIRED" and not answered
    major_open = bool(verdict[0]) and not [row for row in revisions if row.index > challenge[-1].index]
    return lead_revision(run, rejected, security.count("CHANGES_REQUIRED"), major_open) or \
        fix_now_revision(run, reviews, revisions) or confirmation(run, security_due and bool(answered), verdict, challenge)


# A Security note that recommends the fix in this change ends `Recommendation: fix now`; `Recommendation: issue` files it.
FIX_NOW_TAG = re.compile(r"\bRecommendation:\s*fix now\b", re.I)
# Markdown emphasis around the tag (`**Recommendation:** fix now`) is no part of it.
EMPHASIS = re.compile(r"[*_`]")
LOOSE_FIX_NOW = re.compile(r"\bfix now\b", re.I)
RECOMMENDATION = re.compile(r"\bRecommendation:", re.I)


def fix_now_asked(notes):
    """True when a line of the *Security notes* carries the tag `Recommendation: fix now` (`squad-security.md`), or says
    `fix now` and carries no `Recommendation:` tag at all: a note written before the tag existed recommends it
    loosely, and `squad-notes.py` warns about such a line when it writes one. `Recommendation: issue` files it."""
    lines = [EMPHASIS.sub("", line) for line in notes.splitlines()]
    return any(FIX_NOW_TAG.search(line) or (LOOSE_FIX_NOW.search(line) and not RECOMMENDATION.search(line))
               for line in lines)


def fix_now_revision(run, reviews, revisions):
    """The Lead's revision for the *Security notes* of an approved plan review that recommend `fix now`, before the
    skeleton: the notes are appended to `plan.md` and the Lead would otherwise never see them (Censio#38)."""
    answers = [row.index for row in revisions if re.search("security notes", row.step, re.I)]
    approved = [row for row in reviews if security_verdict(row) == "APPROVED"
                and not any(index < row.index for index in answers)]
    if not approved or not fix_now_asked(run.section("Security notes")) or any(
            row.index > approved[-1].index for row in revisions):
        return None
    return heading(f"step {run.plan_step} — Lead revision for Security's `fix now` notes") + launch(
        run, LEAD, "revise", f"{run.plan_step} Plan revise (security notes)", "the *Security notes* of plan.md that "
        "recommend `fix now`: it writes each into the plan, or decides to file it as an issue and says why")


def lead_revision(run, rejected, rejections, major_open):
    if rejected and rejections >= 3:
        return heading("step 3 — Security rejected the fix the Lead accepted (*Loop limits*, Plan ↔ Security)") + [
            DO + "escalate to the Product Manager: one question with the options and the Lead's recommendation",
            f"  Log: {LOG_TOOL} {run.name} \"3 Answer\" \"Product Manager\" \"<the answer>\""]
    if rejected and rejections >= 2:
        return heading("step 3 — the 2nd CHANGES_REQUIRED (*Loop limits*, Plan ↔ Security)") + launch(
            run, LEAD, "decide", "3 Decide", "Security's findings of both rounds and the revision: narrow the scope, "
            "split, escalate, or accept and fix a defect the loop limit allows (then one Security delta confirmation)")
    if major_open or rejected:
        return heading(f"step {run.plan_step} — Lead revision") + launch(
            run, LEAD, "revise", f"{run.plan_step} Plan revise", "all objections (minor ones included) and Security's "
            "CHANGES_REQUIRED findings; a Security note that leaves the plan a choice, and every *Security notes* entry "
            "that recommends `fix now`, too")
    return None


def confirmation(run, unconfirmed, verdict, challenge):
    if unconfirmed:
        return heading("step 3 — Security delta confirmation of the revision") + launch(
            run, SECURITY, "plan", "3 Plan security delta", "the revised plan's commit and the findings it answers")
    later = {run.plan_step, "2", "3", "4", "5", "6", "7", "8", "9", "10", "11", "12"}
    if verdict[1] and not verdict[0] and challenge and not notes_settled(run, run.plan, later, challenge[-1]):
        return heading(f"step {run.plan_step} — minor objections only") + [
            DO + "append the minor objections verbatim to plan.md as *Challenge notes* (and Security's non-blocking "
            f"notes as *Security notes*) with `python3 {NOTES_TOOL} {run.name} challenge|security --agent-id <the "
            "member's id>`, and apply each exact edit of plan.md a note states (*Plan challenge*); commit — no Lead "
            "launch",
            orchestrator_log(run, f"{run.plan_step} Challenge notes", "minor objections appended verbatim to plan.md")]
    return None


def security_verdict(row):
    found = SECURITY_VERDICT.search(row.result)
    return found.group(1).upper() if found else None


def groups(run):
    """[(group, has a skeleton row, documentation rows alone)] of tasks.md in table order, group `PM` left out (steps
    4–6 launch no member for it); one unnamed group for a bug fix."""
    check = plan_check()
    if not run.feature:
        api = check.signature_names(check.signatures_section(run.plan or ""))
        return [("", bool(api), False)]
    found, no_code = {}, False
    for name, task, files in task_rows(run.tasks):
        if name == check.NO_CODE_GROUP:
            no_code = True
            continue
        had_skeleton, all_docs = found.get(name, (False, True))
        found[name] = (had_skeleton or bool(re.match(r"skeleton\b", task, re.I)), all_docs and documentation_cell(files))
    if not found and no_code:
        return []
    return [(name, *state) for name, state in found.items()] or [("", False, False)]


def cell_paths(text):
    """The paths a *Files* cell names, in parentheses too."""
    paths = [word.strip(".,;:()`'\"") for word in re.split(r"[\s,]+", (text or "").replace("`", " "))]
    return [path for path in paths if "/" in path or "." in path.strip(".")]


def skeleton_gaps(run, group):
    """Once the Dev logged the group's skeleton: the files its skeleton task rows name that the branch has not changed,
    committed or not (Censio#193: a report named a function its file did not hold)."""
    if not group or not group_rows(run, DEV, "4", group):
        return []
    planned = {path for name, task, files in task_rows(run.tasks)
               if name == group and re.match(r"skeleton\b", task, re.I) for path in cell_paths(files)}
    tool = stamp_tool()
    try:
        touched = tool.changed(run.root, tool.merge_base(run.root)) | set(tool.uncommitted(run.root))
    except tool.Refusal:
        return []
    return sorted(path for path in planned if not planned_path_changed(path, touched))


def planned_path_changed(path, touched):
    """True when a path a *Files* cell names matches a changed file: a glob by `fnmatch`, a directory (`src/A.B` too) by
    prefix, a bare file name by the file names of the changed paths (DockerUpdateGuard#465, #477, Censio#202)."""
    path = path[2:] if path.startswith("./") else path
    if any(char in path for char in "*?["):
        prefix = path.split("**", 1)[0] if "**" in path else None
        return any(fnmatch.fnmatchcase(changed, path) or (prefix is not None and changed.startswith(prefix))
                   for changed in touched)
    if path in touched or any(changed.startswith(path.rstrip("/") + "/") for changed in touched):
        return True
    return "/" not in path and any(changed.rsplit("/", 1)[-1] == path for changed in touched)


def skeleton_gap_lines(run, group):
    gaps = skeleton_gaps(run, group)
    return [f"  First: the skeleton changed none of {', '.join(gaps)}, which its task rows name: back to the Dev (mode "
            "skeleton) before the Tester starts"] if gaps else []


def documentation_cell(text):
    """True when every path a *Files* cell names, in parentheses too, is a Markdown file or a README: a group of such
    rows has no code to skeleton or test first."""
    paths = cell_paths(text)
    return bool(paths) and all(path.lower().endswith(".md") or path.rsplit("/", 1)[-1].upper().startswith("README")
                               for path in paths)


def group_owners(run, group):
    """The owners the task rows of `group` name, lower-case (`dev`, `tester`, `orchestrator`, …)."""
    check = plan_check()
    found = set()
    for header, rows in check.tables(run.tasks or ""):
        index, owner = check.column(header, "group"), check.column(header, "owner")
        for row in rows:
            if check.cell_at(row, index) == group:
                found |= check.owners(check.cell_at(row, owner))
    return found


def trivial_tests_first(run, group, tag, rows):
    """Tier trivial: tests first when tasks.md puts the group's Tester rows before the Dev's (Vandox#440)."""
    if run.base_tier != "trivial" or not tester_first(run, group) or tests_first_ended(run, group):
        return None
    return heading(f"step 5 — Tests first{tag} (tier trivial: tasks.md puts the Tester first)") + launch(
        run, TESTER, "tests-first", f"5 Tests first{tag}", f"{rows}, the tests of *Tests per criterion*, "
        f"{BASE_HANDOVER}")


def tester_first(run, group):
    """True when the first task row of `group` that names the Dev or the Tester names the Tester (and not the Dev)."""
    check = plan_check()
    for header, rows in check.tables(run.tasks or ""):
        index, owner = check.column(header, "group"), check.column(header, "owner")
        for row in rows:
            named = check.owners(check.cell_at(row, owner)) & {"dev", "tester"} if check.cell_at(row, index) == group \
                else set()
            if named:
                return named == {"tester"}
    return False


def task_rows(tasks):
    """(group, task, files or None without a *Files* column) of every task row of tasks.md that names its group."""
    check = plan_check()
    for header, rows in check.tables(tasks or ""):
        group, task, files = (check.column(header, name) for name in ("group", "task", "files"))
        for row in rows:
            if check.cell_at(row, group):
                yield (check.cell_at(row, group), check.cell_at(row, task),
                       None if files is None else check.cell_at(row, files))


def in_group(row, group):
    tag = GROUP.search(row.step)
    return not group or (tag is not None and tag.group(1) == group)


def group_rows(run, who, number, group, word=None):
    return [row for row in run.of(who, {number}, word=word) if in_group(row, group)]


def skeleton_skipped(run, group):
    """True when the orchestrator logged step 4 as skipped for the group, or without a group for every group."""
    return any(SKIPPED.search(f"{row.step} {row.result}") and (in_group(row, group) or not GROUP.search(row.step))
               for row in run.of(ORCHESTRATOR, {"4"}))


def tests_first_ended(run, group):
    """True when step 5 is over for the group: a Tester row of it, or an orchestrator row of step 5 that says it skipped
    and names the group (`5 Tests first skipped (G3)`) or no group."""
    if group_rows(run, TESTER, "5", group):
        return True
    return any(SKIPPED.search(f"{row.step} {row.result}") and (in_group(row, group) or not GROUP.search(row.step))
               for row in run.of(ORCHESTRATOR, {"5"}))


def deferred_tests(run, group):
    """The deferred tests the group's last step-5 Tester row names after `Deferred:` (Censio#137), or ''."""
    row = deferred_row(run, group)
    found = DEFERRED.search(squad_log().ANY_TRAILER.sub("", row.result)) if row else None
    names = found.group("names").strip().rstrip(";,.") if found else ""
    return "" if not names or re.match(r"none\b", names, re.I) else names


def deferred_row(run, group):
    """The group's last step-5 Tester row that carries a `Deferred:` line (a later fix round may carry none), or None."""
    rows = [row for row in group_rows(run, TESTER, "5", group) if DEFERRED.search(row.result)]
    return rows[-1] if rows else None


def deferral_settled(run, group):
    """True when a later Dev row that says it removed something names every deferred test of the group (the branch
    the test was deferred for is gone, Vandox#470), so no coverage pass waits for it."""
    row = deferred_row(run, group)
    names = [name.strip(" `'\"") for name in re.split(r"[,;]\s*", deferred_tests(run, group)) if name.strip()]
    later = " ".join(squad_log().ANY_TRAILER.sub("", other.result) for other in run.of(DEV, after=row.index)
                     if REMOVED.search(other.result))
    return bool(names) and all(name.split(" (")[0] in later for name in names)


def implement_ended(run, group):
    """True when the group's implementation is over: a Dev implement row of step 6, or an orchestrator row of step 6
    that says it skipped the implementation and names the group (`6 Implement skipped (G1)`: steps 4–5 left nothing to
    write, PlexToJellyfinSync#292); unlike steps 4 and 5, a row without a group ends only a run without groups."""
    if group_rows(run, DEV, "6", group, word="implement"):
        return True
    return any(SKIPPED.search(f"{row.step} {row.result}") and (not group or GROUP.search(row.step))
               for row in group_rows(run, ORCHESTRATOR, "6", group, word="implement"))


def coverage_ended(run, group):
    """True when the group's coverage pass is over: a Tester row of step 6, an orchestrator coverage row of step 6 that
    names the group, or one without a group after the group's implementation (`6 Coverage gate (all groups)`). With
    deferred tests open, only a Tester row of step 6 ends it."""
    if deferred_tests(run, group) and not deferral_settled(run, group):
        tests_first = deferred_row(run, group).index
        return any(row.index > tests_first for row in group_rows(run, TESTER, "6", group))
    if group_rows(run, TESTER, "6", group) or [row for row in run.of(ORCHESTRATOR, {"6"}, word="coverage")
                                               if GROUP.search(row.step) and in_group(row, group)]:
        return True
    implemented = group_rows(run, DEV, "6", group, word="implement")
    after = implemented[-1].index if implemented else len(run.rows)
    # Vandox#470: a passing full stamp on a head that holds the group's implementation passed its Coverage gate.
    if implemented and last_full_stamp_row(run) > after:
        return True
    return any(not GROUP.search(row.step) for row in run.of(ORCHESTRATOR, {"6"}, after=after, word="coverage"))


def code_steps(run):
    """Steps 4–6, per task group of a feature."""
    if run.base_tier not in ("trivial", "standard", "security", "tests") or run.plan is None:
        return None
    if run.code_free():
        return code_free_steps(run)
    for group, skeleton, docs_only in groups(run):
        tag = f" ({group})" if group else ""
        found = decided_fix(run, group, tag) or group_step(run, group, skeleton, docs_only)
        if found:
            return found
    return documentation(run)


def code_free_steps(run):
    if not run.of(ORCHESTRATOR, {"4", "5"}, word="skip"):
        return heading("steps 4–5 — not applicable (*Changes without production or test code*)") + [
            orchestrator_log(run, "4-5 Skipped", "no production or test code: see *Verification without tests*")]
    tester = tester_named(run)
    if not run.of(DEV, {"6"}):
        extra = "; then the Tester's share the plan names" if tester else " (no Tester's share in the plan)"
        return heading(f"step 6 — the Dev's edits{extra}; no *Test with coverage*, no *Coverage gate*") + \
            launch(run, DEV, "implement", "6 Implement", "the plan (a feature: the task rows and spec.md), the base "
                   "ref and the head SHA") + [THEN + "run the plan's *Verification without tests* where it names you"]
    if tester and not run.of(TESTER, {"6"}):
        return heading("step 6 — the Tester's share of a plan without production or test code (test documentation, "
                       "a dry-run artifact: *Dry-run artifacts* in .squad/routing.md)") + launch(
            run, TESTER, "coverage", "6 Tester share", "the plan's rows that name the Tester (*Verification without "
            "tests*, *Documentation updates*); a dry-run artifact as a patch file outside the working tree, checked with "
            f"git apply --check, its path in the report; {BASE_HANDOVER}")
    return None


def tester_named(run):
    """True when the plan's *Verification without tests* or *Documentation updates* names the Tester (Vandox#410)."""
    text = run.section("Verification without tests") + "\n" + run.section("Documentation updates")
    return bool(re.search(r"\btester\b", plan_check().strip_comments(text), re.I))


def group_step(run, group, skeleton, docs_only=False):
    tag = f" ({group})" if group else ""
    rows = "the group's task rows and spec.md" if group else "the plan"
    if docs_only:
        return documentation_group(run, group, tag)
    if run.base_tier in ("standard", "security"):
        if skeleton and not group_rows(run, DEV, "4", group) and not skeleton_skipped(run, group):
            return heading(f"step 4 — Skeleton{tag}") + launch(
                run, DEV, "skeleton", f"4 Skeleton{tag}", f"the body the *Skeleton* section of .squad/stack.md names, "
                f"quoted in the prompt; the planned signatures and the test call sites the plan assigns to the Dev "
                f"({rows})") + [
                "  Not due when the plan states it adds no API (its *Signatures* names only private members): " +
                orchestrator_log(run, f"4 Skeleton skipped{tag}", "skipped — <where the plan says it adds no API>").strip()]
        if not tests_first_ended(run, group):
            return heading(f"step 5 — Tests first{tag}") + skeleton_gap_lines(run, group) + launch(
                run, TESTER, "tests-first", f"5 Tests first{tag}", f"{rows}, the tests of *Tests per criterion*, "
                f"{BASE_HANDOVER}") + [THEN + "confirm yourself that the new tests compile and fail on the current "
                                       "code; log its `Deferred: <test names>` line (`Deferred: none`) in the row",
                                       "  Not due when the group adds no production behaviour (a limit change that "
                                       "was not due, say): " + orchestrator_log(
                                           run, f"5 Tests first skipped{tag}", "skipped — <why>").strip()]
    trivial = trivial_tests_first(run, group, tag, rows)
    if trivial:
        return trivial
    if run.base_tier == "tests":
        if not group_rows(run, TESTER, "6", group):
            return heading(f"step 6 — the Tester writes the plan's tests{tag} (tier tests: no Dev)") + launch(
                run, TESTER, "coverage", f"6 Coverage{tag}", f"{rows}, the tests of *Tests per criterion*, "
                f"{BASE_HANDOVER}")
        return None
    if not implement_ended(run, group):
        return heading(f"step 6 — Implement{tag}") + launch(
            run, DEV, "implement", f"6 Implement{tag}", f"{rows}, the test names (the deferred ones marked), "
            f"{BASE_HANDOVER}") + [
            "  Not due when steps 4 and 5 left the group no production code to write (the skeleton was the whole "
            "change): " + orchestrator_log(run, f"6 Implement skipped{tag}", "skipped — <what steps 4–5 did>").strip(),
            decide_pointer(run, tag)] + deferred_note(run, group)
    if not coverage_ended(run, group):
        return coverage_pass(run, tag, rows, deferred_tests(run, group)) + [decide_pointer(run, tag)]
    return None


def decide_rows(run, group):
    """The step-6 Lead `decide` rows of the group, a private check's excepted: a row that names it, or in a feature a row
    that names none (the launch of `--launch squad-lead decide` prints no group) after the latest row of steps 4–6
    that does."""
    rows = [row for row in run.of(LEAD, {"6"}, word="decide") if "private check" not in row.step.lower()]
    found = []
    for row in rows:
        before = [other for other in run.rows if other.number in ("4", "5", "6") and other.index < row.index
                  and GROUP.search(other.step)]
        tag = GROUP.search(row.step) or (GROUP.search(before[-1].step) if before else None)
        if not group or (tag is not None and tag.group(1) == group):
            found.append(row)
    return found


def fix_items(result):
    """The `;`-separated items of the `FIX:` part of a Lead decision (up to its `ISSUE:`)."""
    return result[result.index("FIX:"):].split("ISSUE:")[0].split(";")


def fix_owners(fix):
    """(Dev, Tester) that the items of a `FIX:` line name as owners: the first words of an item, `FIX: Dev — x; Tester —
    y` (`DECIDE_FORMAT`). A line that names no owner is the Dev's; one that names only the orchestrator or the Code
    Officer is neither's."""
    names = {match.group(1).lower() for item in fix.split(";") if (match := FIX_OWNER.match(item))}
    return "dev" in names or not names, "tester" in names


def decided_fix(run, group, tag):
    """The launch of the work a step-6 Lead `decide` of the group orders with `FIX:`, before step 7 and also when the
    Coverage gate passed (DockerUpdateGuard#520): the Dev, then the Tester when `FIX:` names both, each until a row of
    its own follows the decision. A `FIX:` that names only the Tester goes to the Tester, one that names another owner
    (the orchestrator, the Code Officer) and no Dev gets no launch; a decision with an item that names no owner
    (`DECIDE_FORMAT`) is corrected while no Dev or Tester row follows it; a decision before the group's
    implementation waits for it; tier `tests` has no Dev; the decision of a private check has its own route."""
    decisions = decide_rows(run, group)
    if not decisions or not fix_ordered(decisions[-1].result):
        return None
    decision = decisions[-1]
    if run.base_tier != "tests" and not implement_ended(run, group):
        return None
    done = {who: any(row.index > decision.index for row in group_rows(run, who, "6", group)) for who in (DEV, TESTER)}
    items = [item for item in fix_items(decision.result) if item.strip()]
    if not all(FIX_OWNER.match(item) for item in items):
        return None if any(done.values()) else fix_owner_missing(run, decision, tag)
    dev, tester = fix_owners(";".join(items))
    dev = dev and run.base_tier != "tests"
    if dev and not done[DEV]:
        who, mode, label = DEV, "implement", "Implement"
    elif tester and not done[TESTER]:
        who, mode, label = TESTER, "coverage", "Coverage"
    else:
        return None
    return heading(f"step 6 — the work the Lead's decision orders{tag}") + launch(
        run, who, mode, f"6 {label} (decide){tag}", f"the Lead's decision (row \"{decision.step}\") and its "
        f"`FIX:` line, {BASE_HANDOVER}")


def fix_owner_missing(run, decision, tag):
    """The correction of a Lead decision whose `FIX:` has an item without its owner, from the Lead's report."""
    return heading(f"step 6 — the Lead's decision orders a change without naming who makes it{tag}") + [
        DO + "correct the row without a new launch, from the Lead's report: "
        f"{LOG_TOOL} {run.name} --replace-last \"Lead\" --step \"{decision.step}\" --new-result \"RESULT: DECIDED — "
        "FIX: <Dev|Tester|orchestrator|Code Officer> — <change>[; <owner> — <change>]\""]


def decide_pointer(run, tag):
    """Where a step-6 question for the Lead goes: a disputed test or lines reported as not unit-testable are a Lead
    `decide`, a fresh launch the orchestrator would otherwise make without this tool (Censio#180)."""
    return (f"  A disputed test or lines reported as not unit-testable: the Lead in mode decide, step \"6 Decide{tag}\" — "
            f"`python3 .squad/tools/squad-next.py {run.name} --launch squad-lead decide` prints the launch (a fresh "
            "agent, never a continuation of the spec or plan agent)")


def documentation_group(run, group, tag):
    """A group of documentation rows alone: no skeleton, no tests first, no coverage pass. The Dev writes its rows, the
    Tester its test documentation (mode coverage), and the orchestrator an edit under `.squad/` from the text the
    member reports (`.squad/team.md`, *Files*)."""
    owners = group_owners(run, group)
    title = f"step 6 — Documentation{tag} (the group names documentation files alone: no skeleton, no tests first)"
    done = group_rows(run, ORCHESTRATOR, "6", group, word="document")
    if "dev" in owners and not group_rows(run, DEV, "6", group) and not done:
        return heading(title) + launch(
            run, DEV, "implement", f"6 Documentation{tag}", "the group's task rows of the Dev (squad-dev.md, "
            f"*Documentation*), {BASE_HANDOVER}", rule=SAME)
    if "tester" in owners and not group_rows(run, TESTER, "6", group) and not done:
        return heading(title) + launch(
            run, TESTER, "coverage", f"6 Test documentation{tag}", "the group's task rows of the Tester (test "
            f"documentation; rows under .squad/ as exact text in the report), {BASE_HANDOVER}")
    return None


def deferred_note(run, group):
    """The line that tells, before the Dev's row, that the deferred tests make the Tester's coverage pass due
    (Censio#244: a coverage skip logged right after the Dev); squad-log.py refuses that skip."""
    deferred = deferred_tests(run, group)
    if not deferred or deferral_settled(run, group):
        return []
    return [f"  After it: the Tester's coverage pass for the deferred tests ({deferred}), also when the gate passes; "
            "no coverage skip row"]


def coverage_pass(run, tag, rows, deferred=""):
    if deferred:
        return heading(f"step 6 — Coverage{tag}: the Tester's pass is due for the deferred tests, also when the gate "
                       "passes") + launch(run, TESTER, "coverage", f"6 Coverage{tag}",
                                          f"the deferred tests ({deferred}), {rows}, {BASE_HANDOVER}")
    return heading(f"step 6 — Coverage{tag}") + [
        DO + "run *Test with coverage* and the *Coverage gate* from .squad/stack.md yourself",
        "  The Tester's pass runs only when the gate fails, the Dev reports uncovered new lines, a resource bound of the "
        "plan has no test, a deferred test or test documentation is open, or a reviewer asks for a test:"] + launch(
        run, TESTER, "coverage", f"6 Coverage{tag}", f"the reason, {rows}, {BASE_HANDOVER}") + [
        "  Otherwise: " + orchestrator_log(run, f"6 Coverage skipped{tag}", "<why>").strip()]


def documentation(run):
    body = run.section("Documentation updates")
    if none_text(body) or run.of(DEV, {"6"}, word="document") or run.of(ORCHESTRATOR, {"6"}, word="document"):
        return None
    return heading("step 6 — Documentation updates the plan lists, once the code is green") + launch(
        run, DEV, "implement", "6 Documentation", "the plan's *Documentation updates* (squad-dev.md, *Documentation*); "
        "test documentation goes to the Tester in mode coverage", rule=SAME) + [
        "  Already written in the implementation: " + orchestrator_log(run, "6 Documentation", "written in <step>").strip(),
        THEN + "run *Test* again"]


def matrix_check(run):
    """plan-check.py --matrix before step 7: a MISSING criterion goes to the Tester."""
    if run.plan is None or run.after_pr or not os.path.isdir(run.folder) or run.base_tier in ("docs", "patch"):
        return None
    if stamp_ok(run):
        return None
    lines, missing = plan_check().matrix(run.folder)
    if not missing:
        return None
    rows = [line for line in lines if "MISSING" in line]
    return heading("before step 7 — plan-check.py --matrix reports MISSING criteria") + [f"  {row}" for row in rows] + \
        launch(run, TESTER, "coverage", "6 Coverage (matrix)", f"the MISSING criteria and their tests, {BASE_HANDOVER}")


def stamp_ok(run):
    return stamp_tool().stamp_problem(run.root, run.raw, run.tier, base_check=False, tree_check=True) is None


def quick_edits(run):
    """Tiers docs and patch: the orchestrator applies the Lead's exact edits first."""
    if run.base_tier not in ("docs", "patch") or stamp_tool().stamps(run.raw) or stamp_tool().uncommitted(run.root):
        return None
    tested = run.of(TESTER, {"2"})
    if stamp_tool().branch_changes_code(run.root) and not (tested and tests_only(run)):
        return None
    result = [] if tested else [DO + f"write the Lead's whole result to result.md with `python3 {NOTES_TOOL} "
                                f"{run.name} result --agent-id <the Lead's id>` (never retyped)"]
    if run.base_tier == "patch" and not tested:
        # PiMonitor#235: the test reproduces the defect only on the unfixed code, so it comes before the edits.
        result += ["  For patch: when the Lead names a test to add, launch squad-tester once (the only extra launch), "
                   "before the edits, on the unfixed head:"] + launch(
            run, TESTER, "coverage", "2 Patch test", f"result.md and the test it names, {BASE_HANDOVER}") + [
            "  then run the test yourself and confirm it fails as the result expects (step 5), commit it, and apply "
            "the edits; without such a test, apply them now"]
    return heading(f"tier {run.tier} — apply the Lead's exact edits (*Tiers*)") + result + [
        DO + "apply its edits yourself, run *Format* on them, commit", THEN + "the gate stamp (step 7)"]


def tests_only(run):
    """True when the branch changes test code only outside `specs/`: the patch test, before the Lead's edits."""
    tool = stamp_tool()
    code, base = tool.git(run.root, "merge-base", tool.BASE_REF, "HEAD")
    return code == 0 and tool.changed_kinds(run.root, base.strip()) == {"test"}


def gates_due(run):
    """The gates gate-stamp.py would run, as lines; its refusal when it cannot plan them."""
    tool = stamp_tool()
    try:
        context = tool.run_context(run.root, run.name, run.raw, run.after_pr)
        gates, scope, needs_check, figure = tool.due_gates(context["tier"], context["plan"], context["kinds"])
    except tool.Refusal as refusal:
        return [f"  Gates due: not known yet — {refusal}"]
    names = gates + [" ".join([tool.SCOPE, *scope, *(["--no-specs"] if run.after_pr else [])])]
    lines = [f"  Gates due (tier {context['tier']}): " + ", ".join(names) + (" + figure-check.py (no gate)" if figure
                                                                             else "")]
    if needs_check:
        lines.append("  The patch touches a script, workflow, Dockerfile or data file: pass the check the Lead's result "
                     "names with --check \"<command>\"")
    if context.get("records") and tool.shallow(run.root):
        lines.append(f"  The clone is shallow: run '{tool.UNSHALLOW}' before the stamp, which runs "
                     f"{tool.DECISION} --require-history")
    if tool.TEST_COVERAGE in gates:
        lines.append("  Start the stamp in the background (Bash run_in_background): the full suite can outlast a "
                     "foreground command's time limit")
    if tool.TEST_PROJECT in gates:
        lines.append("  *Test (project)*: pass the test project of the changed code as --project NAME=VALUE (the "
                     "placeholder in .squad/stack.md), or *Test* runs instead")
    return lines


def code_check(run):
    """Step 7: a passing gate stamp on the code head."""
    tool = stamp_tool()
    problem = tool.stamp_problem(run.root, run.raw, run.tier, base_check=False, tree_check=True)
    if problem is None:
        return None
    found = tool.stamps(run.raw)
    last = found[-1] if found else None
    step = run.step_cell("7", "Code check")
    if last is not None and last.group("verdict") == "FAIL" and tool.same_code(run.root, last.group("sha")):
        return failed_gates(run, last)
    dirty = tool.uncommitted(run.root)
    lines = heading(f"step {step.split()[0]} — Code check: {problem}") + (
        [f"  Commit first, these files outside specs/ are uncommitted: {', '.join(dirty[:10])}"] if dirty else []) + [
        DO + "git fetch origin main and merge main when it moved (*Merge conflicts*); commit (a finished step's "
        "commit without `[skip ci]`, *Commits*); then run",
        f"    {STAMP_TOOL} {run.name}" + (STDIN if run.after_pr else "")]
    if last is not None and last.group("verdict") == "PASS" and verbatim_fix(run, last):
        lines.append("  A verbatim documentation fix since the stamp: add --verbatim (Format check, decision-check.py "
                     "and scope-check.py only; no delta round)")
    elif last is not None and last.group("verdict") == "PASS" and record_status_only(run, last.group("sha"), "HEAD"):
        lines.append("  Only decision-record status lines and index rows changed since the stamp (step 9, *PR "
                     "approval*): add --verbatim (Format check, decision-check.py and scope-check.py only; no delta "
                     "round)")
    elif last is not None and last.group("verdict") == "PASS" and (tag := unfixed_review_tag(run)) and \
            tool.documentation_only(run.root, last.group("sha"), "HEAD"):
        # Censio#194: a verbatim fix stamped in full before its row: the round's counts were never subtracted.
        cell = run.step_cell("8", "Verbatim")
        lines.append("  Only documentation changed since the stamp while the review round has findings: a verbatim "
                     f"fix (*Verbatim documentation fixes*) is logged first ({LOG_TOOL} {run.name} \"{cell}\" "
                     f"Orchestrator \"verbatim: [{tag}, verbatim] <file:line>\"), then stamped with --verbatim")
    return lines + gates_due(run)


def unfixed_review_tag(run):
    """`BLOCKING` or `NON-BLOCKING` when a review row of the set after the last full stamp reports findings and no Dev
    or Tester row followed it (an ordinary fix, which takes its delta round), else None."""
    rows = [row for agent, _ in review_set(run) for row in review_rows(run, agent, last_full_stamp_row(run))
            if (verdict := review_verdict(row)) and any(verdict)]
    if not rows:
        return None
    last = max(row.index for row in rows)
    if any(row.who in (DEV, TESTER) and row.index > last for row in run.rows):
        return None
    return "BLOCKING" if any(review_verdict(row)[0] for row in rows) else "NON-BLOCKING"


def verbatim_rows(run, after):
    """The Orchestrator's rows of a verbatim documentation fix after the row index `after` (stamps left out)."""
    return [row for row in run.of(ORCHESTRATOR, after=after) if "verbatim" in f"{row.step} {row.result}".lower()
            and not stamp_tool().STAMP_CELL.match(row.step)]


def verbatim_findings(run, after):
    """(blocking, non-blocking, plan-ordered) findings the Orchestrator's verbatim rows after the row index `after`
    applied: each `BLOCKING` and `NON-BLOCKING` tag a row names counts once, `[NON-BLOCKING, plan, verbatim]` as a
    plan-ordered one too; a row that names no tag counts as one blocking finding."""
    blocking = nonblocking = planned = 0
    for row in verbatim_rows(run, after):
        tagged_nonblocking = len(NONBLOCKING_TAG.findall(row.result))
        tagged_blocking = len(BLOCKING_TAG.findall(row.result))
        nonblocking += tagged_nonblocking
        planned += len(PLAN_VERBATIM_TAG.findall(row.result))
        blocking += tagged_blocking if tagged_blocking or tagged_nonblocking else 1
    return blocking, nonblocking, planned


def verbatim_fix(run, last):
    return stamp_tool().documentation_only(run.root, last.group("sha"), "HEAD") and bool(
        verbatim_rows(run, last_full_stamp_row(run)))


def record_status_only(run, old, new):
    """True when everything that differs between `old` and `new` outside `specs/` is the `Status` line of decision
    records and rows of their index: the record status step 9 sets, bookkeeping that needs no delta round."""
    code, out = stamp_tool().git(run.root, "-c", stamp_tool().QUOTE_PATHS, "diff", "-U0", "--no-renames", old, new,
                                 *stamp_tool().OUTSIDE_SPECS)
    if code != 0 or not out.strip():
        return False
    path, in_header = None, False
    for line in out.splitlines():
        if line.startswith("diff --git "):
            path, in_header = None, True
        elif line.startswith("@@"):
            in_header = False
        elif in_header:
            path = header_path(path, line)
            if path is False:
                return False
        elif line.startswith(("+", "-")) and not status_change(path, line):
            return False
    return True


def header_path(path, line):
    """The path a file header line of a diff names (`+++ b/<path>`), the path so far for another header line, or
    False for a new, deleted or binary file, which no record status changes. Only lines before a file's first `@@`
    are headers: with `-U0` a removed `-- comment` shows as `--- comment` in the hunk."""
    if line.startswith(("new file", "deleted file", "Binary")):
        return False
    return line[6:] if line.startswith("+++ b/") else path


def status_change(path, line):
    """True when a changed line of a diff is a decision record's `Status` line or a row of the records' index."""
    if path is None or not path.startswith(DECISIONS):
        return False
    return bool(STATUS_FIELD.match(line)) or (path == f"{DECISIONS}README.md" and line[1:].startswith("|"))


def failed_gates(run, last):
    entries = [entry for entry in (last.group("gates") or "").split(" · ")[0].split("; ") if not entry.endswith(" pass")]
    lines = heading(f"step 7 — the gate stamp on {last.group('sha')[:12]} failed: " + "; ".join(entries))
    names = " ".join(entries)
    step = run.step_cell("7", "Fix")
    tests = [entry for entry in entries if entry.startswith(("Test ", "Test (project)", "check ")) and "FAIL" in entry]
    if "Format check" in names:
        lines.append(DO + "run *Format*, git restore -- every file it changed outside the change, commit, stamp again")
    if "Analyzer gate" in names:
        lines += launch(run, OFFICER, "code-check", run.step_cell("7", "Code check"), "the base ref and the stamp's "
                        "analyzer output; structural items it hands back go to the Dev (tests: the Tester)",
                        purpose="analyzer diagnostics and style findings that need reading")
    if tests:
        lines += launch(run, DEV, "fix", step, "the failing output and the head SHA (a defect in a test: squad-tester "
                        "in mode coverage instead)")
    if "Coverage gate FAIL" in entries:
        lines += launch(run, TESTER, "coverage", run.step_cell("7", "Coverage"), "the Coverage gate's uncovered lines "
                        "(Dev and Tester iterate; lines no unit test can cover go to the Lead)")
    if "scope-check" in names:
        lines.append(DO + "a scope finding goes to the owner of the file (a squad file is restored)")
    return lines + [THEN + f"commit and run {STAMP_TOOL} {run.name} again"]


def early_pr_asked(line):
    """True for a plan line `Early draft PR: yes`."""
    return bool(EARLY_PR_TAG.search(line))


def early_pr(run):
    if run.after_pr or run.plan is None or run.of(ORCHESTRATOR, {"7"}, word="draft"):
        return None
    check = plan_check()
    template = check.template_lines()
    own = [line for line in (run.plan or "").splitlines() if line.strip() and line.strip() not in template]
    if not any(early_pr_asked(line) for line in own):
        return early_pr_line_missing(run, own)
    return heading("step 7 — Early draft PR (the plan asks for one)") + [
        DO + "open the PR as a draft as create-pr step 8 says, without its review loop (specs/ stays, the working "
        "record waits for step 10), subscribe to its activity, wait for CI and run sonar-check.py pr <number> before "
        "step 8; findings are worked as in step 11",
        orchestrator_log(run, "7 Early draft PR", "<PR url>")]


def early_pr_line_missing(run, own):
    """The Lead's revision for a plan that names an early draft PR in prose without a line of its own `Early draft PR:
    yes` or `: no` (`plan-check.py`'s `early_pr_unstated`: a plan written before the line existed, or one that kept the
    template's `: no`, #464). A revision that still leaves no line gets one more, the Lead continued; after that the run
    goes on without the draft PR."""
    if not plan_check().early_pr_unstated(run.plan or ""):
        return None
    revisions = run.of(LEAD, {"7"}, word="early draft")
    if len(revisions) >= 2:
        return None
    task = ("the plan: add the line `Early draft PR: yes` where the prose asks for one, `Early draft PR: no — <why>` "
            "where it declines (the template's bare `: no` line counts as unset next to such prose); the scope stays, "
            "and the answer is `RESULT: DONE`")
    if revisions:
        return heading("step 7 — the Lead's revision left the plan without its own line `Early draft PR: yes|no`") + \
            launch(run, LEAD, "revise", "7 Plan revise (early draft PR line)", task, rule=LAST)
    return heading("step 7 — the plan names an early draft PR in prose, without the line `Early draft PR: yes|no`") + \
        launch(run, LEAD, "revise", "7 Plan revise (early draft PR)", task, rule=LAST)


def manual_check(run):
    """Before the review set: a criterion the plan verifies by the Product Manager's manual test (`manual:`, a dry run,
    or a manual check its prose names) runs on the stamped head first, so a document the run corrects is reviewed once
    (Censio#176, Vandox#440, Censio#191); one whose check names a reviewer of the set is part of its step-8 round
    (Vandox#451, Censio#188)."""
    if run.after_pr or run.plan is None or run.base_tier in ("docs", "patch") or not stamp_ok(run):
        return None
    failed = manual_failure(run)
    if failed is not None:
        return failed or None
    logged = [row for who in (MANAGER, ORCHESTRATOR) for row in run.of(who, {"7", "8", "9"}, word="manual")]
    if logged or any(review_rows(run, agent, -1) for agent, _ in review_set(run)):
        return None
    planned = manual_criteria(run)
    manual = [criterion for criterion, owner in planned.items() if owner is None]
    prose = [] if manual else plan_check().prose_manual_checks(run.plan)
    if not manual and not prose:
        return None
    what = ", ".join(manual) if manual else "the plan's manual check"
    return heading(f"step 7 — the manual test of {what} before the review") + [f"  Plan: {line}" for line in prose[:3]] + [
        DO + "ask the Product Manager (or the stand-in the plan names) to run it on the stamped head, as the plan "
        "says, and wait; a document the run corrects is committed and stamped again before step 8",
        f"  Log: {LOG_TOOL} {run.name} \"7 Manual test\" \"Product Manager\" \"<PASS or FAIL per criterion, then the result>\""]


def manual_failure(run):
    """None when the last `7 Manual test` row did not fail; else the lines of what follows a failed manual test
    (*Loop limits*, *Manual criterion fails*, Vandox#469): the Lead's decision, the work it added until a new stamp,
    then the manual test again."""
    rows = sorted((row for who in (MANAGER, ORCHESTRATOR) for row in run.of(who, {"7"}, word="manual")),
                  key=lambda row: row.index)
    if not rows or not MANUAL_FAILED.search(rows[-1].result):
        return None
    runs = sum(1 for row in rows if MANUAL_FAILED.search(row.result))
    decided = run.of(LEAD, after=rows[-1].index)
    if not decided:
        if runs > MANUAL_GROUPS:
            return heading(f"step 7 — the manual test failed after {MANUAL_GROUPS} added task groups") + launch(
                run, LEAD, "decide", "7 Decide (manual test)", "the failed manual test's rows and the groups added "
                "for it: it escalates relaxing the criterion to the Product Manager (*Loop limits*)")
        return heading(f"step 7 — the manual test failed (task group {runs} of {MANUAL_GROUPS} for it)") + launch(
            run, LEAD, "decide", "7 Decide (manual test)", "the failed manual test's row and the plan: it adds a "
            "task group for the options it takes, which runs steps 4–7 and the manual test again (*Loop limits*)")
    if last_full_stamp_row(run) < decided[-1].index:
        return manual_rework(run, decided[0])
    return heading(f"step 7 — the manual test again, on the stamp after the added task group (run {runs + 1})") + [
        DO + "ask the Product Manager (or the stand-in the plan names) to run it on the stamped head, as the plan "
        "says, and wait",
        f"  Log: {LOG_TOOL} {run.name} \"7 Manual test\" \"Product Manager\" \"<PASS or FAIL per criterion, then the result>\""]


def manual_rework(run, decision):
    """The work a Lead decision on a failed manual test added, before its stamp: a feature's group runs from tasks.md
    (`code_steps` comes first), so only a bug fix, or a feature whose group is not in tasks.md yet, gets here."""
    handover = f"the Lead's decision (row \"{decision.step}\") on the failed manual test, {BASE_HANDOVER}"
    if run.feature:
        return heading("step 7 — the Lead's decision on the failed manual test adds a task group that tasks.md does "
                       "not hold yet") + launch(run, LEAD, "revise", "7 Plan revise (manual test)", "its decision: "
                                            "it writes the group's rows into tasks.md", rule=LAST)
    if not run.of(DEV, after=decision.index):
        return heading("step 7 — the Dev's work the Lead's decision on the failed manual test orders") + launch(
            run, DEV, "implement", "6 Implement (manual test)", handover)
    if not run.of(TESTER, after=decision.index):
        return heading("step 7 — the Tester's coverage pass on that work") + launch(
            run, TESTER, "coverage", "6 Coverage (manual test)", handover)
    return heading("step 7 — the work the Lead's decision ordered changed no code since the stamp") + [
        DO + "when the decision orders no code (a rerun, a corrected test setup), stamp this head again; the manual "
        "test follows. Otherwise the decision is not carried out: back to the Dev with it",
        f"    {STAMP_TOOL} {run.name}"]


def private_criteria(run):
    """The criteria whose `manual:` check the Tester runs over a private input (`plan-check.py`, `tester_private_check`)."""
    plan = plan_check()
    planned = plan.planned_verifications(plan.sections(run.plan or ""))
    return sorted((criterion for criterion, row in planned.items() if row[0] == "manual"
                   and plan.tester_private_check(row[1])), key=plan.criterion_number)


def private_verdict(result):
    """`FAIL` when a `6 Private check` row holds a capital `FAIL` (`FAILED`, `FAILS`), else `PASS` when it holds a capital
    `PASS`, else None: the row states no verdict and is corrected (`squad-log.py --replace-last`)."""
    if MANUAL_FAILED.search(result):
        return "FAIL"
    return "PASS" if VERDICT_PASS.search(result) else None


def failed_private_check(run, rows):
    """The lines that follow a failed `6 Private check` row: the Lead's decision, then the Dev's work when the decision
    orders a change; None once the check runs again."""
    decided = run.of(LEAD, after=rows[-1].index)
    if not decided:
        failures = len(rows)
        where = (f"task group {failures} of {MANUAL_GROUPS} for it" if failures <= MANUAL_GROUPS else
                 f"after {MANUAL_GROUPS} added task groups")
        return heading(f"step 6 — the Tester's private check failed ({where})") + launch(
            run, LEAD, "decide", "6 Decide (private check)", "the failed check's rows and the plan: it adds a task "
            "group for the options it takes" + (", or escalates relaxing the criterion to the Product Manager "
            "(*Loop limits*)" if failures > MANUAL_GROUPS else ""))
    if fix_ordered(decided[-1].result) and not run.of(DEV, after=decided[-1].index):
        return heading("step 6 — the Dev's work the Lead's decision on the failed private check orders") + launch(
            run, DEV, "implement", "6 Implement (private check)",
            f"the Lead's decision (row \"{decided[0].step}\") on the failed private check, {BASE_HANDOVER}")
    if not fix_ordered(decided[-1].result) and lead_result(decided[-1]) != "ESCALATE" \
            and not [row for row in run.rows if row.index > decided[-1].index]:
        # #464: a decision that orders code without `FIX:` would rerun the check on unchanged code.
        return heading("step 6 — the Lead's decision on the failed private check names no `FIX:`") + [
            DO + "when the Lead's report orders a change, correct the row without a new launch: "
            f"{LOG_TOOL} {run.name} --replace-last \"Lead\" --step \"{decided[-1].step}\" --new-result \"RESULT: "
            "DECIDED — FIX: <Dev|Tester|orchestrator|Code Officer> — <change>\"",
            "  Only when it orders no change (another input, the same code) the check runs again:"] + \
            private_launch(run, private_criteria(run))
    return None


def private_check(run):
    """Step 6, after the coverage pass and before the gate stamp: the Tester runs a manual criterion over an input that
    must stay out of the repository, handed over by its location (Censio#125). A failed run goes to the Lead like a
    failed manual test (*Loop limits*, *Manual criterion fails*) and, once the Dev has worked on its decision, runs
    again."""
    if run.after_pr or run.plan is None or not (criteria := private_criteria(run)):
        return None
    rows = run.of(TESTER, {"6"}, word="private")
    verdict = private_verdict(rows[-1].result) if rows else None
    if rows and verdict is None:
        return heading("step 6 — the Tester's private-check row states no PASS or FAIL") + [
            DO + "correct the row without a new launch, from the Tester's report: "
            f"{LOG_TOOL} {run.name} --replace-last \"Tester\" --step \"{rows[-1].step}\" --new-result "
            "\"<PASS or FAIL per criterion in capitals, then the result>\""]
    if rows and verdict == "PASS":
        return None
    if rows and (failed := failed_private_check(run, rows)):
        return failed
    return heading(f"step 6 — the Tester's check of {', '.join(criteria)} over a private input") + \
        private_launch(run, criteria) + [THEN + "the stamp follows once the row says PASS or FAIL"]


def private_launch(run, criteria):
    """The Tester's launch of the private check of `criteria`."""
    lines = launch(
        run, TESTER, "coverage", run.step_cell("6", "Private check"),
        f"the plan's manual check of {', '.join(criteria)} and where the private input is (ask the Product Manager if "
        "the plan does not say); the input is never committed, copied or quoted in the report, only its result")
    lines[-1] = lines[-1].replace("\"<the result>\"", "\"<PASS or FAIL per criterion, then the result>\"")
    return lines


def manual_criteria(run):
    """{criterion: the reviewer of the review set that runs its `manual:` check in step 8, or None (the Product
    Manager's test of step 7)}; a check that names a member outside the set stays the Product Manager's."""
    plan = plan_check()
    planned = plan.planned_verifications(plan.sections(run.plan or ""))
    members = {CELLS.get(agent, agent) for agent, _ in review_set(run)}
    owners = {criterion: plan.manual_owner(row[1]) for criterion, row in planned.items()
              if row[0] == "manual" and not plan.tester_private_check(row[1])}
    return {criterion: owners[criterion] if owners[criterion] in members else None
            for criterion in sorted(owners, key=plan.criterion_number)}


def review_set(run):
    """The launches of a review round by tier (*Tiers*): (agent, mode)."""
    if run.tier == "patch/security":
        return [(SECURITY, "diff")]
    if run.tier == "security":
        return [(REVIEWER, "review"), (SECURITY, "diff")]
    return [(REVIEWER, "review")]


def full_stamp_rows(run):
    """The row indexes of the passing stamps of the full gates (not a verbatim fix's)."""
    tool = stamp_tool()
    found = []
    for row in run.of(ORCHESTRATOR):
        match = tool.STAMP.match(row.result.strip()) if tool.STAMP_CELL.match(row.step) else None
        if match and match.group("verdict") == "PASS" and not match.group("verbatim"):
            found.append(row.index)
    return found


def last_full_stamp_row(run):
    found = full_stamp_rows(run)
    return found[-1] if found else -1


def review_rows(run, agent, after):
    numbers = {"8", "11"}
    return [row for row in run.of(agent, numbers, after=after)
            if agent != SECURITY or mode_of(SECURITY, row.number, row.step) == "diff"]


def merged_base_only(run, old, new):
    """True when every commit from `old` to `new` that changes code is a merge that took both sides unchanged (no
    combined diff): the merge of the base branch that needs the gates again but no new review round (*Merge
    conflicts*)."""
    git = stamp_tool().git
    code, own = git(run.root, "rev-list", "--first-parent", "--no-merges", f"{old}..{new}", *stamp_tool().OUTSIDE_SPECS)
    _, merges = git(run.root, "rev-list", "--first-parent", "--merges", f"{old}..{new}")
    if code != 0 or own.strip() or not merges.split():
        return False
    return all(not git(run.root, "diff-tree", "--cc", "--no-commit-id", "-p", merge)[1].strip()
               for merge in merges.split())


def reviewed_stamp_row(run):
    """The row of the full stamp the current review round belongs to: the last one, or the reviewed one before it
    when only a merge of the base branch came between them."""
    rows = full_stamp_rows(run)
    if not rows:
        return -1
    reviewed = [index for index in rows if any(review_rows(run, agent, index) for agent, _ in review_set(run))]
    if not reviewed or reviewed[-1] == rows[-1]:
        return rows[-1]
    sha = {row.index: stamp_tool().STAMP.match(row.result.strip()).group("sha") for row in run.rows
           if row.index in (reviewed[-1], rows[-1])}
    old, new = sha[reviewed[-1]], sha[rows[-1]]
    return reviewed[-1] if merged_base_only(run, old, new) or record_status_only(run, old, new) else rows[-1]


def review(run):
    """Step 8 (after the PR: the delta rounds of step 11): the review set on the stamped head."""
    stamp_index = reviewed_stamp_row(run)
    reviewers = review_set(run)
    missing = [(agent, mode) for agent, mode in reviewers if not review_rows(run, agent, stamp_index)]
    if missing:
        return review_launch(run, missing)
    rows = [review_rows(run, agent, stamp_index)[-1] for agent, _ in reviewers]
    verdicts = [review_verdict(row) for row in rows]
    if None in verdicts:
        return heading("step 8 — route the review round") + no_verdict(rows[verdicts.index(None)]) + [
            "  blocking findings: their owner, then steps 6–7 and a delta round; non-blocking: the Lead (mode decide) "
            "first; clean: step 9"]
    stale = unmatched_review(run, stamp_index, rows)
    if stale:
        return stale
    fixed_blocking, fixed_nonblocking, fixed_planned = verbatim_findings(run, max(r.index for r in rows))
    planned = sum(plan_ordered(row) for row in rows)
    # A verbatim fix of a plan-ordered finding needs no owner (DockerUpdateGuard#479): a tag naming `plan` takes it
    # off the planned ones, and untagged verbatim fixes beyond the other non-blocking findings do too.
    others = max(sum(v[1] for v in verdicts) - planned, 0)
    plain = fixed_nonblocking - fixed_planned
    # A plan tag on a verdict line without a `(n plan)` note still names a non-blocking finding it fixed.
    surplus = max(fixed_planned - planned, 0)
    planned = max(planned - fixed_planned - max(plain - others, 0), 0)
    blocking = sum(v[0] for v in verdicts) - fixed_blocking + planned
    nonblocking = others - plain - surplus
    return review_outcome(run, rows, max(blocking, 0), max(nonblocking, 0))


def plan_ordered(row):
    """The non-blocking findings a Reviewer's verdict line marks as asked for by the plan (`NON-BLOCKING 2 (1 plan)`):
    they go to their owner with the round's fixes, as blocking ones do, without a Lead decision."""
    found = REVIEW.search(row.result) if row.who == REVIEWER else None
    plan = PLAN_FINDINGS.search(row.result[found.start():].split("\n", 1)[0]) if found else None
    return int(plan.group(1)) if plan else 0


def unmatched_review(run, stamp_index, rows):
    """The Reviewer's row whose verdict line names no code SHA, or another code than the stamp of its round: the
    round is not tied to the stamp, so the Reviewer is continued on the stamp's SHA."""
    stamp = next((stamp_tool().STAMP.match(row.result.strip()) for row in run.rows if row.index == stamp_index), None)
    for row in rows:
        found = REVIEW.search(row.result) if row.who == REVIEWER and stamp else None
        sha = found.group("sha") if found else None
        if found is None or (sha and names_stamped_code(run, sha, stamp.group("sha"))):
            continue
        what = f"names {sha[:12]}, not the stamped code {stamp.group('sha')[:12]}: a stale round" if sha else \
            f"names no code SHA: the round cannot be matched to the stamp on {stamp.group('sha')[:12]}"
        return heading(f"step {row.number} — the Reviewer's verdict line {what}") + launch(
            run, REVIEWER, "review", f"{row.number} Review (stamp SHA)", f"the stamp's code SHA {stamp.group('sha')}: "
            "it reviews that SHA, copies it from `git rev-parse` into its verdict line, in the `on <sha>` form below, "
            "and reports again")
    return None


def names_stamped_code(run, sha, stamp):
    """True when the SHA a Reviewer names is the stamp's, or a commit with the same code (one that touched only
    `specs/` after it), or a later commit that adds only documentation to the stamped code: the Reviewer read the
    stamped code and the documentation edit with it (Censio#176)."""
    if stamp.startswith(sha.lower()):
        return True
    tool = stamp_tool()
    if tool.git(run.root, "diff", "--quiet", stamp, sha, *tool.OUTSIDE_SPECS)[0] == 0:
        return True
    later = tool.git(run.root, "merge-base", "--is-ancestor", stamp, sha)[0] == 0
    return later and tool.documentation_only(run.root, stamp, sha)


def work_record(run):
    """What the reviewers read of the work folder."""
    if run.base_tier in ("docs", "patch"):
        record = "result.md (the Lead's whole result; the first log.md row carries its RESULT line)"
    else:
        record = "spec.md, plan.md and tasks.md" if run.feature else "plan.md"
    return record + (" from the working-record comment" if run.after_pr else "")


def review_launch(run, missing):
    earlier = any(review_rows(run, agent, -1) for agent, _ in missing)
    word = "delta round" if earlier else "round 1 (full)"
    number = "11" if run.after_pr else "8"
    record = work_record(run)
    lines = heading(f"step {number} — Review, {word}: " + ", ".join(f"{a} (mode {m})" for a, m in missing))
    for agent, mode in missing:
        suffix = " delta" if review_rows(run, agent, -1) else ""
        cell = f"{number} Review{suffix}" if agent == REVIEWER else f"{number} Security diff{suffix}"
        handover = REVIEW_HANDOVER.format(record=record) + ("; for a delta round the last round's findings and the "
                                                            "new code SHA" if suffix else "")
        checks = [criterion for criterion, owner in manual_criteria(run).items() if owner == CELLS.get(agent, agent)]
        if checks and not suffix and not run.after_pr:
            handover += f"; and the manual check of {', '.join(checks)} plan.md's matrix gives it, with its result per " \
                        "criterion in the report"
        lines += launch(run, agent, mode, cell, handover)
    if run.tier not in ("security", "patch/security"):
        lines.append(f"  No Security diff review on tier {run.tier} (an unlisted security area the Reviewer finds: "
                     "*Unlisted security areas*)")
    return lines + ([THEN + "the two run in parallel; avoid commits while the round runs"] if len(missing) > 1 else [])


def blocking_rounds(run):
    """The review rounds in a row, back from the last, that returned blocking findings (one round per stamp)."""
    indexes = full_stamp_rows(run)
    count = 0
    for start, end in reversed(list(zip(indexes, indexes[1:] + [len(run.rows)], strict=True))):
        rows = [row for agent, _ in review_set(run) for row in review_rows(run, agent, start) if row.index < end]
        verdicts = [review_verdict(row) for row in rows]
        if not rows or not any(v and v[0] for v in verdicts):
            break
        count += 1
    return count


def review_outcome(run, rows, blocking, nonblocking):
    last = max(row.index for row in rows)
    decided = run.of(LEAD, after=last, word="decide")
    if blocking and blocking_rounds(run) >= 3 and not decided:
        return heading("step 8 — blocking findings after two delta rounds (*Loop limits*, Review ↔ Dev)") + launch(
            run, LEAD, "decide", run.step_cell("8", "Decide (loop limit)"), "the open blocking findings: accept with "
            "justification, split into a follow-up issue, abort, or escalate")
    if nonblocking and not decided:
        return heading("step 8 — non-blocking findings (*Non-blocking findings*)") + [
            "  Verbatim ones: *Verbatim documentation fixes*, logged as " + verbatim_row(run),
            "  All others in one Lead launch, before the round's fix round:"] + launch(
            run, LEAD, "decide", run.step_cell("8", "Decide"), "the round's non-blocking findings (and, for context, "
            "its blocking ones)")
    ordered = bool(decided) and fix_ordered(decided[-1].result)
    if not blocking and not ordered:
        return None
    fixed = [row for row in run.rows if row.index > last and row.who in (DEV, TESTER)]
    if fixed:
        return None
    what = "blocking findings" if blocking else "the fixes the Lead ordered"
    if run.base_tier in ("docs", "patch"):
        return heading(f"step 8 — {what} (tiers docs and patch)") + [
            DO + "fix them yourself, commit and run the gate stamp again; then a delta round"]
    verbatim = ["  A fix marked verbatim: apply it yourself (*Verbatim documentation fixes*), then " + verbatim_row(run)]
    if not blocking:
        return heading("step 8 — the Lead ordered fixes: their owner, then steps 6–7 and a mandatory delta round") + \
            owner_fixes(run, "8", "the Lead's decide row") + verbatim
    return heading("step 8 — blocking findings to their owner, then steps 6–7 and a mandatory delta round") + launch(
        run, DEV, "fix", run.step_cell("8", "Fix"), "the blocking findings on code and documentation (and the "
        "non-blocking fixes the Lead ordered or the plan already orders), the head SHA",
        purpose="code and documentation") + launch(
        run, TESTER, "coverage", run.step_cell("8", FIX_TESTS), "the blocking findings on tests, the head SHA",
        purpose="tests") + verbatim


def verbatim_row(run):
    """The exact row of a verbatim fix, each finding named by the Reviewer's tag (Censio#244): an untagged row counts
    as one blocking finding (`verbatim_findings`)."""
    return (f"{LOG_TOOL} {run.name} \"{run.step_cell('8', 'Verbatim fix')}\" Orchestrator \"verbatim: [BLOCKING, "
            "verbatim] <file:line>; [NON-BLOCKING, verbatim] <file:line>\" (one tag per finding, as the Reviewer "
            "wrote it; a plan-ordered one `[NON-BLOCKING, plan, verbatim]`)")


def owner_fixes(run, number, source):
    """The Dev's and the Tester's fix launches for the changes a Lead row (`source`) orders, each when it names them."""
    return launch(run, DEV, "fix", run.step_cell(number, "Fix"), f"the fixes {source} orders on code and "
                  "documentation, the head SHA", purpose="when the Lead names the Dev") + launch(
        run, TESTER, "coverage", run.step_cell(number, FIX_TESTS), f"the fixes {source} orders on tests, the head SHA",
        purpose="when the Lead names the Tester")


def approval(run):
    """Step 9 (after a fix of step 11: the re-approval)."""
    if run.base_tier in ("docs", "patch"):
        return None
    if run.after_pr and not [row for row in run.of(ORCHESTRATOR, {"11"}) if stamp_tool().STAMP_CELL.match(row.step)]:
        return None
    last_review = max([row.index for agent, _ in review_set(run) for row in review_rows(run, agent, -1)] + [-1])
    rows = [row for row in run.rows if row.index > last_review and row.number in ("9", "11")
            and approval_verdict(row)]
    if rows and approval_verdict(rows[-1]) == "APPROVED":
        return None
    verdict = rows[-1] if rows else None
    if verdict and not fix_ordered(verdict.result) and not settled_by_decision(run, verdict):
        return heading("step 9 — NOT APPROVED without a fix") + launch(
            run, LEAD, "decide", run.step_cell("9", "Decide"), "the Lead's NOT APPROVED reasons: it decides or escalates")
    if verdict and fix_ordered(verdict.result):
        return heading("step 9 — NOT APPROVED with a fix") + [
            "  The fixes the Lead orders go to their owner, then the stamp (step 7) and a delta round; once that round "
            "is clean the checklist runs again, and the Lead's approve-pr agent is continued only for a point the fix "
            "leaves open"] + owner_fixes(run, "9", "the Lead's approve-pr row")
    problem = stamp_tool().stamp_problem(run.root, run.raw, run.tier)
    if problem:
        return heading("step 9 — gate-stamp.py --verify fails: " + problem) + [
            DO + "git fetch origin main; merge it when it moved (*Merge conflicts*); then the gates again (step 7)"]
    return heading("step 9 — PR approval (*PR approval*: the stamp and the review round are checked)") + [
        DO + "check that the area documents and documentation updates the plan names are in the diff; set each Proposed "
        "decision record of this change and its index row to Accepted, run decision-check.py, commit, and stamp that "
        f"commit with {STAMP_TOOL} {run.name} --verbatim (the record status needs no delta round)",
        "  No decision open: " + orchestrator_log(run, run.step_cell("9", "PR approval"), "approved by checklist").strip(),
        "  A decision open (a plan deviation, a Lead decision of steps 3–8, an unplanned change to docs/ARCHITECTURE.md "
        "or .squad/project.md):"] + launch(run, LEAD, "approve-pr", run.step_cell("9", "PR approval"),
                                           "the base ref, the gate output, the review outcome and the open points one "
                                           "by one (after the PR also the finished check-run list)") + [
        "  The Lead's `RESULT: APPROVED` row ends the step (with the record status stamped): no checklist row after it"]


def settled_by_decision(run, verdict):
    """True when a Lead row of step 9 or 11 after the NOT APPROVED row `verdict` decided it without ordering a fix: the
    checklist runs again (a decision that orders one goes the way of step 8's fixes)."""
    decided = [row for row in run.of(LEAD, {"9", "11"}, after=verdict.index) if lead_result(row) == "DECIDED"]
    return bool(decided) and not fix_ordered(decided[-1].result)


def approval_verdict(row):
    """`APPROVED` or `NOT APPROVED` of a step-9 row: the Lead's `RESULT:` line, or the orchestrator's row that starts
    with "approved by checklist"; None for any other row."""
    if row.who == LEAD:
        found = lead_result(row)
        return found if found in ("APPROVED", "NOT APPROVED") else None
    return "APPROVED" if row.who == ORCHESTRATOR and CHECKLIST.match(row.result) else None


def pull_request(run):
    if run.after_pr or run.of(ORCHESTRATOR, {"10"}):
        return None
    lines = []
    if run.plan is not None and os.path.isdir(run.folder):
        matrix, missing = plan_check().matrix(run.folder)
        if missing:
            return heading("step 10 — plan-check.py --matrix reports MISSING criteria: back to step 6") + [
                f"  {line}" for line in matrix if "MISSING" in line] + launch(
                run, TESTER, "coverage", "6 Coverage (matrix)", f"the MISSING criteria, {BASE_HANDOVER}")
        lines.append("  Matrix: plan-check.py --matrix passes; fill in each result still to record")
    early = run.of(ORCHESTRATOR, {"7"}, word="draft")
    return heading("step 10 — Pull request") + lines + [
        DO + "post the working record (the matrix, plan.md, log.md; a feature also spec.md and tasks.md) as one comment "
        "(a part too long for it as a permalink, the log table then from this row on: squad-issue step 10), "
        f"git rm -r specs/{run.name}/, scope-check.py --no-specs, commit \"Remove squad working record\", push, and "
        + ("update the early draft PR" if early else "open the PR as a draft (create-pr step 8, without its review loop)"),
        f"  Log (into the comment's log table): {LOG_TOOL} {run.name} \"10 Pull request\" Orchestrator \"<PR url>\" "
        "--stdin < log.md > log-updated.md"]


def after_pr(run):
    if not run.after_pr or [row for row in run.of(ORCHESTRATOR, {"11"})
                            if READY.search(f"{row.step} {row.result}") or PR_ENDED.search(row.step)]:
        return None
    due = [] if squad_log().lessons_rows(run.raw) else [
        "  Step 12 is still due: a merge or close notification ends the watching, never the run — log it, then run "
        "step 12 at once: " + orchestrator_log(run, "11 Merged", "<merged|closed> at <time>").strip()]
    return heading("step 11 — After the PR (*Draft until green*)") + [
        DO + "subscribe to the PR's activity; merge conflicts first; a CI failure, a Sonar finding or a review comment "
        "goes to its owner — get the exact launch with --launch squad-<role> <mode> — and then through the stamp and "
        "a delta round again; once *Draft until green* holds, mark the PR ready",
        orchestrator_log(run, "11 Ready for review", "marked ready for review")] + due


def wrap_up(run):
    if squad_log().lessons_rows(run.raw):
        return None
    return heading("step 12 — Wrap-up") + [
        DO + "check the log for the lesson candidates (.squad/orchestration.md, *Squad lessons*), file the lessons as "
        "one issue per label, link them from the working record",
        orchestrator_log(run, "12 Wrap-up", "<URL of every lesson issue> | no lessons")]


def finished(run):
    return heading("the run's last steps") + [
        f"  Run {SUMMARY} " + ("--stdin " if run.after_pr else "") + "until it exits 0, append its tables to the "
        "working record under \"Squad run metrics\", then session-usage.py under \"Session usage\", and report to the "
        "user; the run is then complete"]


STAGES = (intake, escalation, waiting, no_change, spec_stage, plan_written, challenge_and_security, code_steps,
          quick_edits, matrix_check, private_check, code_check, early_pr, manual_check, review, approval,
          pull_request, after_pr, wrap_up, finished)


def run_header(run):
    tier = run.tier or "not decided yet"
    kind = "feature" if run.feature else "bug fix"
    line = f"Run {run.name} ({kind}): tier {tier}"
    if run.tier:
        tester, _ = expected_model(run, TESTER)
        line += " · review set: " + " + ".join(f"{a} ({m})" for a, m in review_set(run)) + f" · Tester model {tester}"
    return line + (" · after step 10 (log from --stdin)" if run.after_pr else "")


def cap_warnings(run):
    """A warning for every member whose last launch row continued an agent already past the continuation cap
    (`squad-log.py`'s *Past the continuation cap*, Censio#146), until that member's next launch is fresh."""
    found = []
    log = squad_log()
    for agent, member in CELLS.items():
        launches = [i for i, row in enumerate(run.raw) if row[2] == member and log.TRAILER.search(row[3])]
        if agent not in MODES or not launches:
            continue
        rows = run.raw[launches[-1]]
        trailer = log.TRAILER.search(rows[3])
        if not trailer.group("continued"):
            continue
        chains, fresh, current = log.agent_chains(run.raw[:launches[-1]], member)
        index = log.chain_of_step(chains, fresh, trailer.group("origin")) if trailer.group("origin") else current
        if index is not None and log.at_cap(chains[index]):
            found.append(f"WARNING: {member}'s last launch, \"{rows[1]}\", continued an agent past the continuation "
                         "cap (.squad/orchestration.md, *Continuing a member*): its next launch is a new agent with a "
                         "short hand-over, as the launch below says")
    return found


def next_step(run):
    lines = [run_header(run)] + cap_warnings(run)
    for stage in STAGES:
        found = stage(run)
        if found:
            return lines + found
    return lines


def launch_only(run, agent, mode):
    if agent not in MODES or mode not in MODES[agent]:
        known = "; ".join(f"{a}: {', '.join(m)}" for a, m in MODES.items())
        raise ValueError(f"unknown agent or mode {agent} {mode} ({known})")
    step = MODE_STEPS[mode]
    if agent == SECURITY and mode == "plan":
        step = "3 Plan security delta"
    if agent == ADVOCATE:
        step = "2a Spec challenge" if mode == "spec" else f"{run.plan_step} Plan challenge"
    if run.after_pr or mode == "decide":
        step = f"{'11' if run.after_pr else '<step>'} {step.split(' ', 1)[-1]}"
    return [run_header(run)] + cap_warnings(run) + [f"Launch {agent} in mode {mode}:"] + launch(
        run, agent, mode, step, "only what it needs (squad-issue: the plan or task rows, the test names, the base ref, "
        "the code SHA, the gate output for the head)")


def read_log(options, root, name):
    """The log's raw rows and whether they come from standard input; None when the folder has no log."""
    if options.stdin:
        return squad_log().parse_rows(squad_log().with_earlier_rows(root, name, sys.stdin.read())), True
    path = squad_log().resolve_log(root, name)
    if path is None:
        return None
    with open(path, encoding="utf-8-sig") as handle:
        return squad_log().parse_rows(handle.read()), False


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("folder", help="the work folder's name under specs/ (issue-12, feature-12-export)")
    parser.add_argument("--launch", nargs=2, metavar=("AGENT", "MODE"), help="print this one launch")
    parser.add_argument("--stdin", action="store_true", help="read the log from standard input (after step 10)")
    options = parser.parse_args(argv)
    name = stamp_tool().folder_name(options.folder)
    if name is None:
        parser.error(f"{options.folder!r} is no work folder name (issue-12, feature-12-export)")
    top = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=False)
    root = top.stdout.strip() if top.returncode == 0 else os.getcwd()
    os.chdir(root)
    found = read_log(options, root, name)
    if found is None:
        print(f"No log.md in specs/{name} (after step 10, pipe the working-record comment's log table in with --stdin)")
        return 1
    run = Run(root, name, found[0], found[1])
    try:
        lines = launch_only(run, *options.launch) if options.launch else next_step(run)
    except ValueError as error:
        parser.error(str(error))
    print("\n".join(lines))
    return 0


if __name__ == "__main__":
    sys.exit(main())
