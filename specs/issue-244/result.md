RESULT: DONE

**Tier: `docs`.** The only edit is one wording fix in the *Guarantees* section of `.squad/project.md`, which the *Tiers* rules treat as documentation. No security area, command or `docs/areas/` file changes, so no `plan.md` is written and no decision record is needed.

**Issue claims checked**
- **Confirmed:** `/home/user/PiMonitor/.squad/project.md`, lines 62–64, says shortfalls are "listed under *Open questions*" in `docs/areas/metrics.md`.
- **Confirmed:** `/home/user/PiMonitor/docs/areas/metrics.md` has no such section. Its headings are Scope, Behavior, Limits and errors, Related decisions and Not here.
- **Root cause:** commit 769bf9c (#234) deleted the section from metrics.md when it fixed the section's only entry (the crash on a 7-counter `/proc/stat` cpu line, #195). The pointer in project.md was left in place.
- Six of the eight area documents have an *Open questions* section, but `docs/areas/_template.md` does not define one, and `history.md` has none either.
- No other file points to metrics.md's *Open questions*. I grepped `docs/`, `.squad/*.md`, `README.md` and `SECURITY.md`.

**Options considered**
1. **Reword the pointer in project.md (chosen).** The section's header says each guarantee is "one line: the guarantee, a decision-record link, an area-document link". This was the only line with an extra clause. Linking to `#limits-and-errors` points at the paragraphs that actually state the guarantee: a failing source never stops the service, optional hardware sources are logged once, and no reading is invented.
2. **Add `## Open questions` with "None." back to metrics.md (rejected).** Editing `docs/areas/` makes the change `trivial` or higher (the `docs` row of *Tiers*). It would also add a section the area template does not define, only to say "None.", and the next shortfall would be tracked as an issue anyway, as #195 was.

**Exact edit (orchestrator applies it; `.squad/project.md` is written by the orchestrator)**

File `/home/user/PiMonitor/.squad/project.md`, replace:
```
- A missing hardware source yields an empty/zero value and a logged warning, never a crash or a fabricated
  reading — [Metrics](../docs/areas/metrics.md); where the code falls short of this today is listed under *Open
  questions* there
```
with:
```
- A missing hardware source yields an empty/zero value and a logged warning, never a crash or a fabricated
  reading — [Metrics](../docs/areas/metrics.md#limits-and-errors)
```
No other file changes. There is no changelog, README or ARCHITECTURE.md update: the PR checklist asks for none on a change that leaves behavior as it is.

**Acceptance criteria and how each is checked (no unit test applies)**
- **AC1:** The metrics guarantee in `.squad/project.md` (*Guarantees*) no longer mentions an *Open questions* section, and links to `../docs/areas/metrics.md#limits-and-errors`.
  - Check (read-only, reviewer): `grep -n "Open questions" .squad/project.md` prints nothing.
  - The line contains the link `metrics.md#limits-and-errors`.
- **AC2:** The link target exists.
  - Check (read-only, reviewer): `docs/areas/metrics.md` has the heading `## Limits and errors`, whose anchor is `limits-and-errors`.
  - That section states the no-crash, logged-warning and no-invented-reading rules.
- **AC3:** The diff stays within the `docs` tier.
  - Check (orchestrator): `scope-check.py --tier docs` passes.
  - Check (orchestrator): *Format check* passes.

**Related observation (no change here):** open issue #246 says a failed `/proc/meminfo` read is exported as 0 in Prometheus. That issue concerns how the HTTP API renders a non-hardware source, not a missing hardware source, and it is already tracked, so this change leaves it alone.

**Files**
- `/home/user/PiMonitor/.squad/project.md` (edited)
- `/home/user/PiMonitor/docs/areas/metrics.md` (read only, link target)
