#!/usr/bin/env python3
"""PreToolUse hook for the squad subagents: no Git write operation and no GitHub write from a member.

`.claude/settings.json` declares it for `Bash`, so it runs before every Bash call of the session and of its
subagents; it decides only for a squad member (the hook input's `agent_type` is `squad-…`), so the orchestrator
(the session that runs the skill) and other agents commit, push and post as before. It is not declared in the
members' front matter: Claude Code skips a project agent's front-matter hooks in a folder without workspace
trust, which a cloud or `-p` session never gives. It reads the hook input JSON from stdin and, for a member's
`Bash` call, checks every `git` and `gh` invocation in the command (also behind `&&`, `;`, `|`, `$(…)`,
`git -C <dir>`, and inside `bash -c` / `sh -c` / `eval`):

- allowed: read-only Git (`status`, `diff`, `log`, `show`, `ls-files`, `grep`, `rev-parse`, `merge-base`,
  `fetch`, `tag --list` / `--contains`, `branch --list` / `--show-current`, `ls-remote`, `clone`, …),
  `git add` (the coverage gate only counts tracked files), `git worktree add` / `remove` / `prune` /
  `list` (scratch worktrees, `.squad/orchestration.md`, *Concurrency*), `git apply --check` / `--stat` / `--numstat` /
  `--summary` with only options that write no file (`APPLY_HARMLESS`; a dry-run patch checked, never applied:
  `.squad/routing.md`, *Dry-run artifacts*),
  and read-only `gh` (`gh api` without a
  method or field, `gh pr view`, `gh run list`, `gh ruleset list`, …);
- denied: `commit`, `push`, `stash`, `reset`, `checkout`, `switch`, `restore`, `rebase`, `merge`,
  `cherry-pick`, `revert`, `rm`, `mv`, `clean`, `pull`, `am`, `apply` (but its checks above), `tag <name>`, `branch -d/-D/-m/…`,
  `notes`, `update-ref`, `reflog`, `gc`, `filter-branch`, `config` (except `--get*`, `--list`);
- denied: an alias or an included configuration file set for the call (`git -c alias.ci=commit ci`,
  `--config-env=alias.…`, `-c include.path=…`, `GIT_CONFIG_KEY_<n>` / `GIT_CONFIG_PARAMETERS` naming one), whose
  command the guard would never see;
- denied: the `gh` subcommands that write to GitHub, the account or the local Git and `gh` configuration
  (`GH_WRITE_SUBCOMMANDS`, also behind `-R <repo>` and under their aliases such as `gh pr new`): pull requests,
  issues, releases, repositories, labels, discussions, projects, `gh workflow run|enable|disable`,
  `gh run rerun|cancel|delete`, `gh cache delete`, secrets, variables, gists, `gh pr checkout`, `gh alias`, …;
  and `gh api` with a method other than GET (`-X POST`, `-XPOST`, `-X=POST`, `--method POST`, `--method=POST`)
  or a field or input (`-f`, `-F`, `--field`, `--raw-field`, `--input`, also attached as in `-fbody=x`).

A denied call answers `{"hookSpecificOutput": {"permissionDecision": "deny", …}}` with the reason, so the
member reports the need to the orchestrator instead; a denied `git rm` or `git mv` also names the plain `rm` or `mv`
and the `git add` that do the same (`.squad/team.md`, *Git and GitHub*). Anything else, including input the hook cannot
parse, is left to the normal permission flow (exit 0, no decision).

The guard prevents accidents; it is not a security boundary. A command that runs Git or `gh` without naming it
on the command line gets past it: an interpreter (`python3 -c "import subprocess; …"`), a script, an alias from
the Git or `gh` configuration, a shell redirect into `.git/` (`echo x > .git/HEAD`). The rules in
`.squad/team.md` bind there all the same.
"""
import json
import re
import shlex
import sys

GIT_DENIED = {
    "commit", "push", "stash", "reset", "checkout", "switch", "restore", "rebase", "merge", "cherry-pick",
    "revert", "rm", "mv", "clean", "pull", "am", "notes", "update-ref", "reflog", "gc",
    "filter-branch", "replace", "submodule", "bisect",
}
GIT_OPTIONS_WITH_VALUE = {"-C", "-c", "--config-env", "--git-dir", "--work-tree", "--namespace", "--exec-path"}
GIT_CONFIG_OPTIONS = ("-c", "--config-env")
# Configuration keys that make Git run a command the guard never sees: an alias, or a file that may define one.
GIT_ALIAS_KEYS = ("alias.", "include.", "includeif.")
GIT_CONFIG_ENV = re.compile(r"GIT_CONFIG_(?:KEY_\d+|PARAMETERS)=.*(?:alias|include)", re.I)
WORKTREE_ALLOWED = {"add", "remove", "prune", "list", "lock", "unlock"}
# Every gh subcommand that writes to GitHub, the account or the local Git and gh configuration (gh 2.102,
# `gh help reference`); a two-word entry is a subcommand of a subcommand.
GH_WRITE_SUBCOMMANDS = {
    "pr": {"create", "new", "comment", "review", "merge", "close", "edit", "ready", "reopen", "lock", "unlock",
           "revert", "update-branch", "checkout", "co"},
    "issue": {"create", "new", "comment", "close", "edit", "reopen", "delete", "transfer", "pin", "unpin", "lock",
              "unlock", "develop"},
    "release": {"create", "new", "delete", "delete-asset", "edit", "upload"},
    "repo": {"create", "new", "delete", "edit", "fork", "rename", "archive", "unarchive", "sync", "set-default",
             "autolink create", "autolink new", "autolink delete", "deploy-key add", "deploy-key delete"},
    "label": {"create", "delete", "edit", "clone"},
    "discussion": {"create", "comment", "edit"},
    "project": {"close", "copy", "create", "delete", "edit", "field-create", "field-delete", "item-add",
                "item-archive", "item-create", "item-delete", "item-edit", "link", "mark-template", "unlink"},
    "workflow": {"run", "enable", "disable"},
    "run": {"rerun", "cancel", "delete"},
    "cache": {"delete"},
    "secret": {"set", "delete", "remove"},
    "variable": {"set", "delete", "remove"},
    "gist": {"create", "new", "edit", "delete", "rename"},
    "agent-task": {"create"},
    "codespace": {"create", "delete", "edit", "stop", "rebuild"},
    "alias": {"set", "delete", "import"},
    "config": {"set"},
    "auth": {"login", "logout", "refresh", "setup-git", "switch"},
    "extension": {"install", "remove", "uninstall", "upgrade", "create"},
    "skill": {"install", "add", "update", "publish"},
    "ssh-key": {"add", "delete"},
    "gpg-key": {"add", "delete"},
}
GH_GROUP_ALIASES = {"agent": "agent-task", "agents": "agent-task", "agent-tasks": "agent-task", "cs": "codespace",
                    "ext": "extension", "extensions": "extension", "skills": "skill"}
GH_OPTIONS_WITH_VALUE = {"-R", "--repo"}
GH_API_WRITE_OPTIONS = {"--field", "--raw-field", "--input"}
# `gh api` shorthands by their long name; `-i` (--include) is the one that takes no value.
GH_API_SHORTHANDS = {"F": "--field", "f": "--raw-field", "H": "--header", "X": "--method", "p": "--preview",
                     "q": "--jq", "t": "--template"}
GH_API_OPTIONS_WITH_VALUE = {"--field", "--raw-field", "--header", "--method", "--preview", "--jq", "--template",
                             "--input", "--hostname", "--cache"}
SEGMENT_SPLIT = re.compile(r"&&|\|\||[;|()`\n]|\$\(")
TAG_READ = {"-l", "--list", "--contains", "--points-at", "--merged", "--no-merged"}
BRANCH_WRITE = {"-d", "-D", "-m", "-M", "-c", "-C", "-f", "--delete", "--move", "--copy", "--force",
                "--set-upstream-to", "-u", "--unset-upstream", "--edit-description"}
BRANCH_READ = {"-l", "--list", "-a", "-r", "--all", "--remotes", "--show-current", "--contains", "--merged",
               "--no-merged", "-v", "-vv", "--verbose"}
CONFIG_READ = {"-l", "--list", "--show-origin", "--show-scope"}
# `git apply` options that only read the patch: with one of them and without `--apply`, nothing is applied.
APPLY_READ = {"--check", "--stat", "--numstat", "--summary"}
# The other `git apply` options that write no file beside a read-only one (Squad-Spec-Repository-Template#328: an
# allowlist, so `--build-fake-ancestor=<file>`, which writes an index file, and any option Git adds later stay denied).
# `-3`/`--3way` is left out: beside `--check` it still stores the merged blob in `.git/objects` (#328's review).
APPLY_HARMLESS = {"-R", "--reverse", "-v", "--verbose", "-q", "--quiet", "-z", "--cached", "--index", "--reject",
                  "--unidiff-zero", "--recount", "--allow-empty",
                  "--allow-binary-replacement", "--binary", "--ignore-space-change", "--ignore-whitespace",
                  "--inaccurate-eof", "--no-add"}
# Options of `git apply` that take a value, attached (`--directory=src`) or as the next argument.
APPLY_WITH_VALUE = {"--directory", "--exclude", "--include", "--whitespace"}
APPLY_NUMBERED = re.compile(r"^-[pC]\d*$")
SHELLS = {"sh", "bash", "zsh", "dash", "ksh"}
# What a member runs instead of a denied command that has an allowed equivalent (`.squad/team.md`, *Git and GitHub*).
GIT_INSTEAD = {
    "git rm": "To delete a file the change removes, run `rm <path>`, then `git add <path>`, and name it in your report.",
    "git mv": "To move a file, run `mv <old> <new>`, then `git add <old> <new>`, and name both in your report.",
}
MEMBER_PREFIX = "squad-"


def git_subcommand(tokens):
    """(subcommand, arguments, global options) of a git invocation, skipping global options such as `-C <dir>`."""
    i = 0
    while i < len(tokens):
        token = tokens[i]
        if token in GIT_OPTIONS_WITH_VALUE:
            i += 2
        elif token.startswith("-"):
            i += 1
        else:
            return token, tokens[i + 1:], tokens[:i]
    return None, [], tokens


def config_setting(options, i):
    """The `name=value` a global `-c` or `--config-env` option at position i sets, or ''."""
    option = options[i]
    if option in GIT_CONFIG_OPTIONS:
        return options[i + 1] if i + 1 < len(options) else ""
    return option.removeprefix("--config-env=") if option.startswith("--config-env=") else ""


def defines_alias(options):
    """Whether git's global options set an alias or include a configuration file for the call."""
    return any(config_setting(options, i).lower().startswith(GIT_ALIAS_KEYS) for i in range(len(options)))


def tag_verdict(args):
    """`git tag <name>` creates a tag; the listing and query forms are reads."""
    if args and not any(a in TAG_READ or a.startswith("--list=") for a in args):
        return "git tag <name>"
    return None


def branch_verdict(args):
    if any(a in BRANCH_WRITE for a in args):
        return "git branch (delete/move/copy/upstream)"
    if args and not any(a in BRANCH_READ or a.startswith("--") for a in args):
        return "git branch <name>"
    return None


def worktree_verdict(args):
    if args and args[0] in WORKTREE_ALLOWED:
        return None
    return "git worktree " + (args[0] if args else "")


def config_verdict(args):
    if any(a.startswith("--get") or a in CONFIG_READ for a in args):
        return None
    return "git config (write)"


def apply_option_ok(args, i):
    """(whether the option at `args[i]` writes no file, how many arguments it takes up)."""
    arg = args[i]
    name = arg.split("=", 1)[0]
    if name in APPLY_WITH_VALUE:
        return True, 1 if "=" in arg else 2
    return arg in APPLY_READ or arg in APPLY_HARMLESS or bool(APPLY_NUMBERED.match(arg)), 1


def apply_verdict(args):
    """`git apply` with `--check`, `--stat`, `--numstat` or `--summary` and otherwise only options that write no file
    (`APPLY_HARMLESS`) reads a patch; any other option — `--apply`, `--build-fake-ancestor`, an abbreviation, one Git
    adds later — makes the call a write."""
    i, read = 0, False
    while i < len(args):
        arg = args[i]
        if arg == "--":
            break
        if arg.startswith("-") and arg != "-":
            ok, used = apply_option_ok(args, i)
            if not ok:
                return f"git apply {arg} (only --check, --stat, --numstat or --summary with options that write no file " \
                       "reads a patch)"
            read = read or arg in APPLY_READ
            i += used
            continue
        i += 1
    return None if read else "git apply (only --check, --stat, --numstat or --summary reads a patch)"


GIT_CHECKS = {"tag": tag_verdict, "branch": branch_verdict, "worktree": worktree_verdict, "config": config_verdict,
              "apply": apply_verdict}


def git_verdict(tokens):
    sub, args, options = git_subcommand(tokens)
    if defines_alias(options):
        return "git -c alias.…/include.… (an alias or configuration file set for the call)"
    if sub is None:
        return None
    if sub in GIT_DENIED:
        return f"git {sub}"
    check = GIT_CHECKS.get(sub)
    return check(args) if check else None


def api_option(token):
    """(long option name, attached value or None) of one `gh api` argument: `--method=POST`, `-XPOST`, `-X=POST`,
    `-iX` (a shorthand after `-i`); (None, None) for a positional argument."""
    if token.startswith("--"):
        name, separator, value = token.partition("=")
        return name, (value if separator else None)
    cluster = token[1:].lstrip("i") if token.startswith("-") else ""
    if not cluster:
        return None, None
    value = cluster[1:].removeprefix("=")
    return GH_API_SHORTHANDS.get(cluster[0], "-" + cluster[0]), (value or None)


def api_options(args):
    """The (long option name, value) pairs of a `gh api` call; an option's value is never read as an option."""
    i = 0
    while i < len(args):
        name, value = api_option(args[i])
        i += 1
        if name in GH_API_OPTIONS_WITH_VALUE and value is None:
            value = args[i] if i < len(args) else ""
            i += 1
        if name:
            yield name, value


def gh_api_verdict(args):
    for name, value in api_options(args):
        if name in GH_API_WRITE_OPTIONS:
            return f"gh api {name} (a field or input: a write)"
        if name == "--method" and value and value.upper() != "GET":
            return f"gh api --method {value}"
    return None


def gh_words(args):
    """The first two words after a gh command group, skipping its options such as `-R <repo>`."""
    words, i = [], 0
    while i < len(args) and len(words) < 2:
        token = args[i]
        i += 2 if token in GH_OPTIONS_WITH_VALUE else 1
        if not token.startswith("-"):
            words.append(token)
    return words


def gh_verdict(tokens):
    if len(tokens) < 2:
        return None
    group = GH_GROUP_ALIASES.get(tokens[0], tokens[0])
    if group == "api":
        return gh_api_verdict(tokens[1:])
    writes = GH_WRITE_SUBCOMMANDS.get(group, set())
    words = gh_words(tokens[1:])
    found = next((command for command in (" ".join(words[:1]), " ".join(words)) if command in writes), None)
    return f"gh {group} {found}" if found else None


def basename(token):
    return token.rsplit("/", 1)[-1]


def nested_command(name, args):
    """The command string a shell runs with `-c` (also `-lc`, `-ec`, …) or `eval` runs, or None."""
    if name == "eval":
        return " ".join(args)
    if name not in SHELLS:
        return None
    for i, option in enumerate(args):
        if not option.startswith("-") or option.startswith("--"):
            return None
        if "c" in option[1:]:
            return args[i + 1] if i + 1 < len(args) else None
    return None


VERDICTS = {"git": git_verdict, "gh": gh_verdict}


def word_verdict(tokens, i):
    """(whether the word at position i starts a command the guard checks, the denial or None)."""
    name = basename(tokens[i])
    nested = nested_command(name, tokens[i + 1:])
    if nested is not None:
        return True, check_command(nested)
    if GIT_CONFIG_ENV.match(tokens[i]):
        return True, "GIT_CONFIG_* naming alias.…/include.… (an alias or configuration file set for the call)"
    if name in VERDICTS:
        return True, VERDICTS[name](tokens[i + 1:])
    return False, None


def check_command(command):
    """The first denied invocation in the command as text, or None."""
    for segment in SEGMENT_SPLIT.split(command):
        try:
            tokens = shlex.split(segment)
        except ValueError:
            tokens = segment.split()
        for i in range(len(tokens)):
            checked, found = word_verdict(tokens, i)
            if found:
                return found
            if checked:
                break
    return None


def deny_reason(data):
    """The reason to deny the Bash call described by the hook input, or None when it may run."""
    if not isinstance(data, dict) or data.get("tool_name") != "Bash":
        return None
    agent = data.get("agent_type")
    if not isinstance(agent, str) or not agent.startswith(MEMBER_PREFIX):
        return None  # the orchestrator or another agent: not a squad member
    command = (data.get("tool_input") or {}).get("command")
    found = check_command(command) if isinstance(command, str) else None
    if found is None:
        return None
    reason = (f"{found} is denied to {agent}: members run no Git or GitHub write operation, nor a Git alias set for "
              "the call, whose command the guard cannot see; subagents report, the orchestrator commits, pushes and "
              "posts (.squad/team.md, Shared rules). Scratch worktrees (git worktree add/remove) and git add stay "
              "allowed.")
    return f"{reason} {GIT_INSTEAD[found]}" if found in GIT_INSTEAD else reason


def main():
    """Exit code 0 either way: a denial is the JSON decision on stdout, not an error."""
    try:
        data = json.load(sys.stdin)
    except ValueError:
        return
    reason = deny_reason(data)
    if reason is not None:
        print(json.dumps({"hookSpecificOutput": {"hookEventName": "PreToolUse", "permissionDecision": "deny",
                                                 "permissionDecisionReason": reason}}))


if __name__ == "__main__":
    main()
