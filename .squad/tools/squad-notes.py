#!/usr/bin/env python3
"""Append a member's report verbatim to a work folder's `plan.md` (or `spec.md`) as its *Challenge notes* or *Security
notes* (`.squad/routing.md`, *Plan challenge*), so the orchestrator never retypes the notes and cannot change them on
the way (PlexToJellyfinSync#298: a retyped replacement string carried the new text on both sides).

The report comes from the member's transcript in Claude Code: `--agent-id <id>` (the agent id the Agent tool returned)
reads the report of the agent's latest round — the message of its `SubagentHandback` call, else its last message —
from `<session>/subagents/agent-<id>.jsonl` of the session `CLAUDE_CODE_SESSION_ID` (or `--session`), as `squad-log.py
--agent-id` reads its usage. Outside Claude Code the report is piped in with `--stdin`. A report without a `VERDICT:`
line is refused: it is narration, not the report. Its closing `VERDICT:` line stays out (the log row carries it);
everything else is written as the member wrote it, under `## Challenge notes` or `## Security notes` at the end of the
document, or below that section's last line when the document has it already. The line ending follows the file.

Usage, from the repository root:
    python3 .squad/tools/squad-notes.py <folder> challenge|security --agent-id <id> [--spec]
    python3 .squad/tools/squad-notes.py <folder> challenge|security --stdin [--spec] < report.md

`<folder>` is the work folder under `specs/` (`issue-12`, `feature-7-export`); `--spec` writes to `spec.md` (a spec
challenge's minor objections) instead of `plan.md`. Exit code 0 when the notes were written, 1 otherwise.
"""
import argparse
import importlib.util
import os
import re
import subprocess
import sys

HEADINGS = {"challenge": "## Challenge notes", "security": "## Security notes"}
VERDICT_LINE = re.compile(r"^\s*\**VERDICT:.*$")
HANDBACK = "SubagentHandback"
SPECS_PREFIX = "specs/"
_TRANSCRIPTS = []


def transcripts():
    """`session-usage.py` next to this script, which reads Claude Code's transcripts, as a module (loaded once)."""
    if not _TRANSCRIPTS:
        here = os.path.dirname(os.path.abspath(__file__))
        spec = importlib.util.spec_from_file_location("session_usage", os.path.join(here, "session-usage.py"))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        _TRANSCRIPTS.append(module)
    return _TRANSCRIPTS[0]


def document(root, folder, name):
    """`specs/<folder>/<name>` when the folder exists; the folder is matched against the directory listing, so the
    argument never becomes a path."""
    wanted = folder.strip().replace("\\", "/").rstrip("/").removeprefix(SPECS_PREFIX)
    specs = os.path.join(root, "specs")
    names = sorted(os.listdir(specs)) if os.path.isdir(specs) else []
    for entry in names:
        if entry == wanted and os.path.isdir(os.path.join(specs, entry)):
            path = os.path.join(specs, entry, name)
            return path if os.path.isfile(path) else None
    return None


def text_blocks(message):
    """The text blocks of an assistant message."""
    content = message.get("content")
    if isinstance(content, str):
        return [content]
    return [block.get("text") or "" for block in content or [] if isinstance(block, dict) and block.get("type") == "text"]


def handback(message):
    """The report a `SubagentHandback` tool call of the message carries (how a subagent of a cloud session hands its
    report back; its plain text is narration then), or None."""
    content = message.get("content")
    for block in content if isinstance(content, list) else []:
        if isinstance(block, dict) and block.get("type") == "tool_use" and block.get("name") == HANDBACK:
            report = (block.get("input") or {}).get("message")
            return report if isinstance(report, str) else None
    return None


def latest_round(path):
    """The assistant messages of the agent's latest round, oldest first (one record per content block)."""
    reader = transcripts()
    messages = []
    for record in reader.records(path):
        if reader.is_message_to_agent(record):
            messages = []
        elif record.get("type") == "assistant" and isinstance(record.get("message"), dict):
            messages.append(record["message"])
    return messages


def last_report(path):
    """The report of the agent's latest round: the message of its last `SubagentHandback` call, else the text of its
    last message (a message whose text blocks span several records is joined block by block, a blank line between
    two blocks)."""
    messages = latest_round(path)
    handed = [report for report in map(handback, messages) if report]
    if handed:
        return handed[-1].strip()
    texts, last_id = [], None
    for message in messages:
        blocks = [text for text in text_blocks(message) if text.strip()]
        if blocks and message.get("id") != last_id:
            texts, last_id = [], message.get("id")
        texts += blocks
    return "\n\n".join(text.strip() for text in texts)


def transcript_report(agent_id, session):
    """The report of the agent `agent_id` from this session's transcripts; exits when there is none."""
    reader = transcripts()
    found = reader.find_transcript(reader.projects_dir(), session or os.environ.get("CLAUDE_CODE_SESSION_ID"))
    path = reader.subagent_transcript(found, agent_id) if found else None
    report = last_report(path) if path else ""
    if not report:
        sys.exit(f"No report of agent '{agent_id}' in this session's transcripts (CLAUDE_CODE_SESSION_ID or --session): "
                 "outside Claude Code, pipe the report in with --stdin")
    return report


def notes(report):
    """The report without its closing `VERDICT:` line and the blank lines around it."""
    lines = report.strip().splitlines()
    while lines and (not lines[-1].strip() or VERDICT_LINE.match(lines[-1])):
        lines.pop()
    return "\n".join(line.rstrip() for line in lines)


def with_notes(text, heading, body):
    """The document with the notes under `heading`: below the section's last line when it exists, else as a new
    section at the end."""
    lines = text.splitlines()
    start = next((index for index, line in enumerate(lines) if line.strip().lower() == heading.lower()), None)
    if start is None:
        return text.rstrip("\n") + f"\n\n{heading}\n\n{body}\n"
    end = next((index for index in range(start + 1, len(lines)) if re.match(r"^#{1,2}[ \t]", lines[index])),
               len(lines))
    while end > start + 1 and not lines[end - 1].strip():
        end -= 1
    rest = lines[end:]
    merged = lines[:end] + ["", body] + ([""] + rest if rest and rest[0].strip() else rest)
    return "\n".join(merged) + "\n"


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("folder")
    parser.add_argument("kind", choices=sorted(HEADINGS))
    source = parser.add_mutually_exclusive_group(required=True)
    source.add_argument("--agent-id", help="read the report from this agent's transcript")
    source.add_argument("--stdin", action="store_true", help="read the report from standard input")
    parser.add_argument("--spec", action="store_true", help="write to spec.md instead of plan.md")
    parser.add_argument("--session", help="the orchestrating session (default: CLAUDE_CODE_SESSION_ID)")
    args = parser.parse_args(argv)
    toplevel = subprocess.run(["git", "rev-parse", "--show-toplevel"], capture_output=True, text=True, check=False)
    root = toplevel.stdout.strip() if toplevel.returncode == 0 else os.getcwd()
    name = "spec.md" if args.spec else "plan.md"
    path = document(root, args.folder, name)
    if path is None:
        print(f"No {name} in specs/{args.folder}")
        return 1
    report = sys.stdin.read() if args.stdin else transcript_report(args.agent_id, args.session)
    if not any(VERDICT_LINE.match(line) for line in report.splitlines()):
        print("The report carries no VERDICT line: it is not the member's report (the Devil's Advocate and Security "
              "end theirs with one); nothing written")
        return 1
    body = notes(report)
    if not body:
        print("The report holds no notes besides its VERDICT line: nothing written")
        return 1
    with open(path, "rb") as handle:
        raw = handle.read()
    eol = "\r\n" if b"\r\n" in raw else "\n"
    text = with_notes(raw.decode("utf-8-sig").replace("\r\n", "\n"), HEADINGS[args.kind], body)
    with open(path, "wb") as handle:
        handle.write(text.replace("\n", eol).encode("utf-8"))
    print(f"{HEADINGS[args.kind]} of {os.path.relpath(path, root)}: {len(body.splitlines())} line(s) written verbatim")
    return 0


if __name__ == "__main__":
    sys.exit(main())
