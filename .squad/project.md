# Project

What the squad needs to know about this project that is not stack-specific. Read by the Lead, the Devil's
Advocate, Security, the Tester and the Reviewer. Not template-managed: `adopt-template` creates it once
and never overwrites it. A product PR updates it when the change makes an entry untrue
(`.squad/routing.md`, *Scope of a product PR*).

Every entry points: it names files, types or endpoints and links the document that says more. Behavior, values,
limits and sequences are written once, in the area documents (`docs/areas/README.md`), and the reasons in the
decision records; a copy here goes stale with the next change. `config-check.py` warns when this file grows past
`PROJECT_MAX_WORDS` words (`.squad/tools/squad_settings.py`).

## Security areas

A change that touches one of these lists it under *Security areas touched* in its plan, so Security reviews the
plan (`.squad/routing.md`, *Tiers*, which also says when a change is tier `security`). One line per area: what is
protected, the concrete types, files or endpoints, and a link to the area document that states its rules
(`- Session tokens: issued in src/Auth/, checked by every /api route — [Sessions](../docs/areas/sessions.md)`); no
limits, patterns or sequences.

- {{TODO: Secrets and tokens: where they are read, stored, compared, logged}}
- {{TODO: Authentication and sessions}}
- {{TODO: File writes and paths derived from external input}}
- {{TODO: Parsing of external input (XML/JSON/YAML, uploads, CLI arguments)}}
- {{TODO: Outbound calls (HTTP clients, TLS, timeouts)}}
- {{TODO: Logging of external data}}

## Guarantees

Deliberate behavior that must not change without the Product Manager. One line each: the guarantee in a few words,
a link to its decision record where it was a real choice, and a link to the area document that states it.

- {{TODO: guarantee}} — [decision NNNN](../docs/decisions/NNNN-title.md), [area](../docs/areas/{{TODO: area}}.md)

## Integration surface

What the Reviewer checks when the diff introduces or changes a thing of this kind: every place that must
change with it.

**A new or changed configuration option** touches:
- {{TODO: the options type / config schema}}
- {{TODO: registration / binding}}
- {{TODO: default config file}}
- {{TODO: the configuration table in `README.md` (key, environment variable, default)}}
- {{TODO: the test that pins the binding}}

**A new or changed service / module** touches:
- {{TODO: its interface}}
- {{TODO: registration and lifetime}}
- {{TODO: the test double in the test project}}
- {{TODO: the component list in `docs/ARCHITECTURE.md`}}

**A new external API call or DTO** touches:
- {{TODO: the client and its DTOs — the external shape must not leak past it}}
- {{TODO: the fake/stub in the tests}}

## Test doubles

The hand-written fakes and stubs the tests reuse (no mocking library unless `docs/UNIT_TESTS.md` says
otherwise), one line each: the double, where it lives and what it stands in for; how it behaves is read in its code.

- `{{TODO: FakeX}}` — {{TODO: what it stands in for}}
