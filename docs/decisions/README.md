# Decision records

Why the code is the way it is. Each file records one decision — its context, the options considered, what
was chosen and the consequences — so that months later the reasoning is still available without the
pull request, the issue thread or the session that produced it.

A record explains the *why*. What holds today — formats, limits, error behavior, guarantees — is written down
once in the [area document](../areas/README.md) of the record's area, and the record links to it.
`docs/ARCHITECTURE.md` describes how the parts fit together. When a decision changes the architecture,
`ARCHITECTURE.md` is updated as well and links the record.

## Rules

- One decision per file: `NNNN-short-title.md` (four digits, next free number), created from
  [`_template.md`](_template.md). In the index row and in a `Superseded by NNNN` status the number may be
  written as a link, `[NNNN](NNNN-short-title.md)`.
- **The record states the decision in one to three sentences and does not restate behavior.** Context,
  *Options considered* and *Consequences* carry the reasoning; the rules of the resulting behavior (values,
  formats, limits, error texts) belong in the area document and are linked from the record.
- The `Area:` field names the record's area as listed in [`docs/areas/README.md`](../areas/README.md), or `—`
  for a record about the process or about no area. Once areas are defined, every record needs the field. For a
  released record it is part of the content: it changes only through a new record. A released record without
  the field gets its area from an `## Area assignment` table (`| Record | Area |`) in a new record; the
  released record itself stays unedited.
- Written by the squad Lead (see [`.claude/agents/squad-lead.md`](../../.claude/agents/squad-lead.md));
  anyone may add one for a change made outside the squad.
- The squad's own process decisions are no records: they stand in [`process.md`](process.md), whose introduction
  says how they change.
- Committed together with the change it explains.
- **Released or not decides how a record changes.** A record is *released* once the commit that added it is
  contained in a release tag (`v*`; check with `git tag --contains <commit>`, the commit is
  `git log --diff-filter=A --format=%H -- <file>`). Until the first release nothing is released.
- A record that is **not yet released** is edited in place, in the pull request that changes the decision
  (status stays `Proposed` or `Accepted`). Its history is Git and the pull request; there is no
  `Superseded by` chain for it. A record that no longer applies is deleted (numbers are never reused, gaps are
  fine) together with its index row and every link to it.
- A **released** record is **append-only**: it is never rewritten. A changed decision gets a new record that
  names the old one under *Supersedes*, and the old record's status becomes `Superseded by NNNN` (the only
  edit allowed). `Superseded by` is therefore only ever set on a released record.
- A new record that **amends** a released record without superseding it names the older one under
  *Supersedes* and says there, in one sentence, what it amends. The older record is **not edited** — not even
  with a pointer; a reader of the older record finds the amendment through the index and the new record.
- **One record per decision that has lasting weight — not one per pull request, and not one per topic.** Not for
  routine changes: a record is needed when a choice between real alternatives was made that shapes the
  architecture or behavior, a trade-off or limitation was accepted, or a documented guarantee was touched.
  Before adding a record, look for an unreleased record of the same decision and extend it instead (a changed
  option, a new consequence); another decision on a topic that already has a record gets a record of its own,
  which links the other. A review finding that was deliberately not fixed, a follow-up split, or a dependency
  change gets its own record only if it touches a guarantee or the architecture; otherwise it belongs in the pull
  request description or the follow-up issue, or as a sentence in the record of the decision it concerns.
- **Size.** A record that an extension would take past `RECORD_MAX_WORDS` words or `RECORD_MAX_OPTIONS` options
  (`.squad/tools/squad_settings.py`) holds more than one decision and is split instead: one record per decision,
  each with its own context, options and consequences. An unreleased record keeps its number for one of them, the
  others take the next free numbers with their index rows, and every link to it points at the record of its
  decision; a released record is never split.
- `.squad/tools/decision-check.py` (also run by `config-check.py`) checks the index, the links between records
  and that no released record was changed, and warns about an unreleased record past the size limits and an area
  document past `AREA_MAX_WORDS`, naming the split.

## Index

<!-- project:begin index -->
| #    | Title | Status | Date |
| ---- | ----- | ------ | ---- |
| [0001](0001-optional-sources-log-failures-once.md) | Optional metric sources log their failures once | Accepted | 2026-11-12 |
| [0002](0002-bound-vcgencmd-output-in-errors.md) | Bound vcgencmd output in error texts, not the read | Accepted | 2026-11-12 |
| [0003](0003-failed-reading-leaves-history-gap.md) | A failed reading leaves a gap in the history | Accepted | 2026-11-12 |
| [0004](0004-final-history-write-waits.md) | The final history write waits for the running one | Accepted | 2026-11-12 |
| [0005](0005-cleared-event-ends-rate-limit-window.md) | A cleared event ends the webhook rate-limit window | Proposed | 2026-11-12 |
<!-- project:end index -->
