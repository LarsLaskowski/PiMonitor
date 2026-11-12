# 0002: Bound vcgencmd output in error texts, not the read

- **Status:** Proposed
- **Date:** 2026-11-12
- **Area:** Metrics
- **Source:** Issue #205
- **Supersedes:** —

## Context

`vcgencmd` is a root-owned binary that `vcgencmdRunner.run` (`internal/collector/vcgencmd.go`) executes on every
fast tick. Its exec-failure detail was already cut to one line of at most 200 bytes, but the parsers of its
successful output (`parseVcgencmdTemp`, `parseThrottled`) quoted the whole output into their errors, and the
`strconv.NumError` they wrapped quoted the number text a second time. Since
[0001](0001-optional-sources-log-failures-once.md) these errors are logged through the log-once rule, and the PMIC
reading keeps its unsupported-output error for the life of the process, so an unexpected output of any length would
reach the journal and stay in memory.

## Options considered

1. **Bound the output where it enters an error** — every quote of `vcgencmd` output in an error goes through the
   same bound as the exec-failure detail, and the wrapped `strconv` error is replaced by its cause (`ErrSyntax`,
   `ErrRange`) so the text is not quoted twice. Covers everything that is logged or kept; a few lines.
2. **Also cap the bytes `run` reads from stdout** (the issue's optional part) — replace `cmd.Output` with capped
   writers for stdout and stderr and fail on overflow. Bounds the transient buffer of one call as well, but rewrites
   the exec path and its stderr fallback for a buffer that is freed after the tick, whose growth is already limited
   in time by `vcgencmdTimeout`, and that only a replaced root-owned binary could fill.
3. **Keep the `strconv.NumError` and bound its `Num` field** — keeps today's short-error text, but embeds the
   output twice, so the message would carry up to twice the bound.

## Decision

Option 1: every `vcgencmd` output quoted in an error is bounded by `sanitizeVcgencmdDetail`, and a number-parse
failure wraps the `strconv` cause instead of the `NumError`. The read itself stays uncapped. The resulting rule is
in [Metrics](../areas/metrics.md#limits-and-errors).

## Consequences

- No log line and no latched error carries more than the bound of `vcgencmd` output; parse errors show the output
  on one line (whitespace runs collapsed), as exec failures already did.
- A short number-parse error loses the redundant `strconv.ParseFloat: parsing "…"` part; `errors.Is` with
  `strconv.ErrSyntax`/`strconv.ErrRange` still works.
- One call can still buffer whatever `vcgencmd` prints within `vcgencmdTimeout`. Revisit (option 2) if `vcgencmd`
  is ever resolved from a path the service user can write.
