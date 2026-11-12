#!/usr/bin/env python3
"""Print the tokens this Claude Code session used, read from Claude Code's own session transcript, for the "Squad run
metrics" of the wrap-up (`.squad/routing.md`, step 12), and read one subagent's launch for `squad-log.py --agent-id`.

`/usage` shows the same numbers on screen only; this script reads them where Claude Code writes them:

- the session's transcript `<projects>/<folder>/<session id>.jsonl`, where `<projects>` is `projects/` under
  `CLAUDE_CONFIG_DIR` (default `~/.claude`) and the session id is `CLAUDE_CODE_SESSION_ID` (or `--session`);
- every subagent's transcript `<session id>/subagents/agent-<id>.jsonl` next to it, with its agent type from
  `agent-<id>.meta.json` — a subagent's tokens are in its own transcript only, never in the session's;
- per model, the input, output, cache-read and cache-write tokens of every API response (a response written
  once per content block is counted once, by its message id).

Next to the raw tokens it prints a **weighted** figure: every token at its list price (`PRICES`, the current model of
the model's family, cache writes by their lifetime), expressed in tokens of Sonnet input. It is proportional to what
the tokens cost, so roles and models can be compared by it, while no cost is printed: Claude Code's own cost snapshot
is written only when the session pauses and mostly predates the run. A model of no listed family gets no weighted
figure (`–`) and is left out of the weighted total.

Calls Claude Code makes on its own (permission checks of auto mode, titles, compaction) are in no transcript, so
the tables are a lower bound: against `/usage`, output and cache-write tokens matched within a few percent, input
and cache-read tokens were 15-20 % lower.

A subagent's transcript holds one **round** per launch of its agent: the first launch and each continuation
(`SendMessage`), a round starting with a message to the agent once the agent has answered. `squad-log.py` reads the
latest round: its tokens per kind, its tool uses, its duration from the first to the last record, the model and
effort its responses ran on, and the agent's context at the end of the round (the four kinds of its last response),
which is the figure a task notification reports as the agent's tokens.

Only these numbers, model ids and agent types are printed; no text of the transcripts, no path, account or other
personal data. The format of these files is Claude Code's internal one and may change with an update: a record
this script cannot read is skipped, never guessed.

Usage, from anywhere:
    python3 .squad/tools/session-usage.py [--session <session id>]

Prints Markdown tables (per model, per subagent type), then the orchestrating session's own share of the weighted
figure, with a note naming its model when that share is above `ORCHESTRATOR_NOTE` percent. Exit code 0 when a transcript was read,
2 when none was found (not a Claude Code session, or the id is unknown): it then prints "not provided", which the
wrap-up records as it stands.
"""
import argparse
import collections
import datetime
import glob
import json
import os
import re
import sys

TOKEN_FIELDS = (
    ("input_tokens", "Input"),
    ("output_tokens", "Output"),
    ("cache_read_input_tokens", "Cache read"),
    ("cache_creation_input_tokens", "Cache write"),
)
# The part of the cache-write tokens written with a one-hour lifetime, which costs more than a five-minute one.
WRITE_1H = "cache_write_1h"
# List prices in USD per million tokens of each family's current model (2026-10): input, output, cache read, cache
# write with a five-minute lifetime, cache write with a one-hour lifetime. A model is priced by the family its id
# names (`claude-opus-5-5`: opus).
PRICES = {
    "fable": (10.0, 50.0, 0.25, 12.5, 20.0),
    "mythos": (10.0, 50.0, 0.25, 12.5, 20.0),
    "opus": (4.0, 20.0, 0.2, 5.0, 8.0),
    "sonnet": (2.0, 10.0, 0.2, 2.5, 4.0),
    "haiku": (0.1, 0.5, 0.01, 0.125, 0.2),
}
# The weighted figure is expressed in tokens of Sonnet input.
REFERENCE_PRICE = PRICES["sonnet"][0]
FAMILY = re.compile(r"(?<![a-z])(" + "|".join(PRICES) + r")(?![a-z])")
NOT_PROVIDED = "not provided (no Claude Code session transcript found)"
# From this share of the weighted figure on, the summary names the orchestrating session's model (in percent).
ORCHESTRATOR_NOTE = 50
NAME = re.compile(r"[\w.-]{1,64}")
TOTAL = "**Total**"
JSONL = ".jsonl"
NONE = "–"
WEIGHTED_NOTE = ("Weighted: every token at its list price, in tokens of Sonnet input (`PRICES` in "
                 "`.squad/tools/session-usage.py`); a model of no listed family has none (–).")


def projects_dir():
    base = os.environ.get("CLAUDE_CONFIG_DIR") or os.path.join(os.path.expanduser("~"), ".claude")
    return os.path.join(base, "projects")


def find_transcript(root, session):
    """The session's transcript under any project folder, or None; the id is matched, never used as a path."""
    if not session or not NAME.fullmatch(session) or not os.path.isdir(root):
        return None
    for folder in sorted(os.listdir(root)):
        path = os.path.join(root, folder, session + JSONL)
        if os.path.isfile(path):
            return path
    return None


def subagent_transcript(session_path, agent_id):
    """`<session>/subagents/agent-<id>.jsonl` of the session's transcript, or None; the id is matched against the
    directory listing, never used as a path."""
    wanted = "agent-" + (agent_id or "").strip().removeprefix("agent-")
    folder = os.path.join(session_path[: -len(JSONL)], "subagents")
    if not NAME.fullmatch(wanted) or not os.path.isdir(folder):
        return None
    names = [name for name in os.listdir(folder) if name == wanted + JSONL]
    return os.path.join(folder, names[0]) if names else None


def records(path):
    """The JSON objects of a JSONL file; unreadable lines are skipped."""
    try:
        with open(path, encoding="utf-8", errors="replace") as handle:
            lines = handle.readlines()
    except OSError:
        return
    for line in lines:
        try:
            record = json.loads(line)
        except ValueError:
            continue
        if isinstance(record, dict):
            yield record


def safe_name(value, fallback):
    return value if isinstance(value, str) and NAME.fullmatch(value) else fallback


def response_usage(record):
    """(message id, model, usage) of an API response record, or None."""
    message = record.get("message")
    if record.get("type") != "assistant" or not isinstance(message, dict):
        return None
    usage = message.get("usage")
    if not isinstance(usage, dict):
        return None
    return message.get("id"), safe_name(message.get("model"), "unknown"), usage


def add_usage(counter, usage):
    for field, _ in TOKEN_FIELDS:
        value = usage.get(field)
        if isinstance(value, int) and value > 0:
            counter[field] += value
    lifetimes = usage.get("cache_creation")
    value = lifetimes.get("ephemeral_1h_input_tokens") if isinstance(lifetimes, dict) else None
    if isinstance(value, int) and value > 0:
        counter[WRITE_1H] += value


def family(model):
    """The model family a model id or alias names (`opus` for `claude-opus-5-5`), or None."""
    found = FAMILY.search((model or "").lower())
    return found.group(1) if found else None


def weighted(counter, model):
    """The tokens of `counter` at the list prices of `model`'s family, in tokens of Sonnet input; None when the
    model names no listed family."""
    prices = PRICES.get(family(model))
    if prices is None:
        return None
    one_hour = min(counter[WRITE_1H], counter["cache_creation_input_tokens"])
    amounts = (counter["input_tokens"], counter["output_tokens"], counter["cache_read_input_tokens"],
               counter["cache_creation_input_tokens"] - one_hour, one_hour)
    return round(sum(amount * price for amount, price in zip(amounts, prices, strict=True)) / REFERENCE_PRICE)


def weighted_sum(per_model):
    """(weighted total of the priced models, whether a model had no price) of {model: counter}."""
    values = [weighted(counter, model) for model, counter in per_model.items()]
    return sum(value for value in values if value is not None), None in values


def read_transcript(path, per_model, seen):
    """Add the transcript's responses to `per_model`; return its own per-model usage."""
    own = collections.defaultdict(collections.Counter)
    for record in records(path):
        found = response_usage(record)
        if not found:
            continue
        message_id, model, usage = found
        if message_id and message_id in seen:
            continue
        seen.add(message_id)
        add_usage(per_model[model], usage)
        add_usage(own[model], usage)
    return own


def agent_type(transcript):
    meta = transcript[: -len(JSONL)] + ".meta.json"
    try:
        with open(meta, encoding="utf-8") as handle:
            data = json.load(handle)
    except (OSError, ValueError):
        return "unknown"
    return safe_name(data.get("agentType") if isinstance(data, dict) else None, "unknown")


def read_session(path):
    """(per model, per agent type, the session's own per model) of the session and all its subagents."""
    per_model = collections.defaultdict(collections.Counter)
    seen = set()
    own = read_transcript(path, per_model, seen)
    agents = {}
    pattern = os.path.join(path[: -len(JSONL)], "subagents", "agent-*.jsonl")
    for transcript in sorted(glob.glob(pattern)):
        launched = read_transcript(transcript, per_model, seen)
        entry = agents.setdefault(agent_type(transcript), {"launches": 0, "models": collections.defaultdict(
            collections.Counter)})
        entry["launches"] += 1
        for model, counter in launched.items():
            entry["models"][model].update(counter)
    return per_model, agents, own


def timestamp(record):
    """The record's time as a naive UTC datetime, or None."""
    value = record.get("timestamp")
    if not isinstance(value, str):
        return None
    try:
        parsed = datetime.datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None
    return parsed.astimezone(datetime.UTC).replace(tzinfo=None) if parsed.tzinfo else parsed


def is_message_to_agent(record):
    """True for a message to the agent (a prompt or a continuation), not a tool result."""
    message = record.get("message")
    if record.get("type") != "user" or not isinstance(message, dict):
        return False
    content = message.get("content")
    if isinstance(content, str):
        return True
    return isinstance(content, list) and not any(isinstance(block, dict) and block.get("type") == "tool_result"
                                                 for block in content)


def new_round():
    return {"usage": collections.Counter(), "models": collections.Counter(), "effort": None, "tool_uses": 0,
            "start": None, "end": None, "context": 0, "responses": 0}


def add_response(current, record, found, tools):
    """Add an API response record to the round: its usage once per message id (`found` is None for a further content
    block of a counted response), its tool uses once per block id, its model and effort."""
    message = record["message"]
    for block in message.get("content") if isinstance(message.get("content"), list) else []:
        if isinstance(block, dict) and block.get("type") == "tool_use" and block.get("id") not in tools:
            tools.add(block.get("id"))
            current["tool_uses"] += 1
    if isinstance(record.get("effort"), str) and NAME.fullmatch(record["effort"]):
        current["effort"] = record["effort"]
    if found is None:
        return
    _, model, usage = found
    add_usage(current["usage"], usage)
    current["models"][model] += 1
    current["responses"] += 1
    current["context"] = sum(value for field, _ in TOKEN_FIELDS if isinstance(value := usage.get(field), int))


def agent_rounds(path):
    """The rounds of a subagent's transcript, oldest first (see the module description), each a dict with "usage"
    (a counter of the token kinds), "models" (responses per model), "effort", "tool_uses", "start" and "end"
    (datetimes), "context" (the four kinds of its last response) and "responses"; rounds without a response are
    left out."""
    rounds, seen, tools = [], set(), set()
    current = None
    for record in records(path):
        if current is None or (is_message_to_agent(record) and current["responses"]):
            current = new_round()
            rounds.append(current)
        stamp = timestamp(record)
        if stamp is not None:
            current["start"] = current["start"] or stamp
            current["end"] = stamp
        found = response_usage(record)
        if found is None:
            continue
        counted = found if not (found[0] and found[0] in seen) else None
        seen.add(found[0])
        add_response(current, record, counted, tools)
    return [entry for entry in rounds if entry["responses"]]


def usage_since(path, since):
    """({model: counter}, {effort: responses}) of the transcript's own responses from `since` (a naive UTC datetime,
    None for all) on: the orchestrating session's share, its subagents being in their own transcripts."""
    per_model, efforts, seen = collections.defaultdict(collections.Counter), collections.Counter(), set()
    for record in records(path):
        found = response_usage(record)
        stamp = timestamp(record)
        if not found or (since is not None and (stamp is None or stamp < since)):
            continue
        if isinstance(record.get("effort"), str) and NAME.fullmatch(record["effort"]):
            efforts[record["effort"]] += 1
        message_id, model, usage = found
        if message_id and message_id in seen:
            continue
        seen.add(message_id)
        add_usage(per_model[model], usage)
    return per_model, efforts


def number(value):
    return f"{value:,}"


def token_cells(counter):
    return " | ".join(number(counter[field]) for field, _ in TOKEN_FIELDS)


def weighted_cell(value):
    return NONE if value is None else number(value)


def model_table(per_model):
    heads = " | ".join(label for _, label in TOKEN_FIELDS)
    lines = [f"| Model | {heads} | Weighted |", "| --- |" + " ---: |" * (len(TOKEN_FIELDS) + 1)]
    total = collections.Counter()
    for model in sorted(per_model):
        lines.append(f"| {model} | {token_cells(per_model[model])} | "
                     f"{weighted_cell(weighted(per_model[model], model))} |")
        total.update(per_model[model])
    lines.append(f"| {TOTAL} | {token_cells(total)} | {number(weighted_sum(per_model)[0])} |")
    return lines


def agent_table(agents):
    heads = " | ".join(label for _, label in TOKEN_FIELDS)
    lines = [f"| Subagent | Launches | Models | {heads} | Weighted |",
             "| --- | ---: | --- |" + " ---: |" * (len(TOKEN_FIELDS) + 1)]
    total = collections.Counter()
    launches = weight = 0
    for name in sorted(agents):
        entry = agents[name]
        models = ", ".join(sorted(entry["models"])) or NONE
        tokens = collections.Counter()
        for counter in entry["models"].values():
            tokens.update(counter)
        value, unpriced = weighted_sum(entry["models"])
        lines.append(f"| {name} | {entry['launches']} | {models} | {token_cells(tokens)} | "
                     f"{NONE if unpriced and not value else number(value)} |")
        total.update(tokens)
        launches += entry["launches"]
        weight += value
    lines.append(f"| {TOTAL} | {launches} | | {token_cells(total)} | {number(weight)} |")
    return lines


def orchestrator_lines(per_model, own):
    """The orchestrating session's own share of the weighted figure, and a note naming its model when the share is
    above `ORCHESTRATOR_NOTE` (Squad-Spec-Repository-Template#313)."""
    total, mine = weighted_sum(per_model)[0], weighted_sum(own)[0]
    if not total or not own:
        return []
    share = round(100 * mine / total)
    models = ", ".join(sorted(own))
    lines = [f"Orchestrator (this session without its subagents): {number(mine)} weighted, {share} % of the total, on "
             f"{models}."]
    if share > ORCHESTRATOR_NOTE:
        lines.append(f"Note: the orchestrating session carries more than {ORCHESTRATOR_NOTE} % of the run's weighted "
                     f"figure on {models}; a squad run's orchestrator on Sonnet with medium effort costs several times "
                     "less, and the members' work does not change with it.")
    return [""] + lines


def report(per_model, agents, own=None):
    lines = ["Tokens per model (the session and all its subagents; Claude Code's own background calls are not in "
             "the transcripts, so this is a lower bound):", ""]
    lines += model_table(per_model)
    lines += ["", "Subagents:", ""]
    lines += agent_table(agents) if agents else ["No subagent was launched in this session."]
    lines += orchestrator_lines(per_model, own or {})
    lines += ["", WEIGHTED_NOTE]
    return "\n".join(lines)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Print this Claude Code session's tokens per model and per subagent type.")
    parser.add_argument("--session", help="session id (default: CLAUDE_CODE_SESSION_ID)")
    args = parser.parse_args(argv)
    path = find_transcript(projects_dir(), args.session or os.environ.get("CLAUDE_CODE_SESSION_ID"))
    if not path:
        print(NOT_PROVIDED)
        return 2
    print(report(*read_session(path)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
