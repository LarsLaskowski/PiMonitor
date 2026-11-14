# Specs

Working records of the squad (see [`.squad/`](../.squad/team.md) and
[`.squad/routing.md`](../.squad/routing.md)). They live **only on a work branch**:

- `specs/issue-<number>/` — `plan.md`, `log.md` (created by the `squad-issue` skill; tiers `docs` and `patch`:
  `result.md` instead of `plan.md`)
- `specs/feature-<short-slug>/` (from an issue `specs/feature-<number>-<short-slug>/`) — `spec.md` (written
  and challenged first, the only list of acceptance criteria), `plan.md`, `tasks.md`, `log.md` (created by the
  `squad-spec` skill; a feature that changes documentation only: `log.md` and `result.md`)

`.squad/tools/plan-check.py <folder>` checks a work folder against these templates; with `--matrix` it prints the
verification matrix the working record starts with.

Before the pull request is opened, the content is posted as a "Squad working record" comment on the issue
(or the PR) and the folder is removed, so `main` only holds this README and the templates in
[`_template/`](_template). The lasting reasoning behind a change is recorded in
[`docs/decisions/`](../docs/decisions/README.md).