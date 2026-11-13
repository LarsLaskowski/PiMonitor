# 0008: Reject non-finite numbers, and fractional values of integer options while reading the file

- **Status:** Accepted
- **Date:** 2026-11-13
- **Area:** Configuration
- **Source:** Issue #200
- **Supersedes:** —

## Context

`gopkg.in/yaml.v3` resolves `.nan` and `.inf` (any case, with an optional sign) to IEEE NaN and infinity. Every
range check in `Config.Validate()` is a comparison, which is false for NaN, so `poll_interval_seconds: .nan` passed
and became a non-positive tick interval; a NaN threshold passed too and makes `GET /api/v1/config` fail to encode.
For an `int` field the decoder truncates a float (`processes_top_n: 2.5` becomes `2`), so by the time `Validate()`
runs the fraction is gone. The Product Manager decided in the issue that both fail startup like any other invalid
value. The decoder must keep rejecting unknown keys (`KnownFields(true)`), which guards against a misspelled
`api_key` silently disabling authentication.

## Options considered

1. **Finiteness in `Validate()` for every float option; the two integer options checked by a second decode of the
   same file into float64 fields, before the real decode** — chosen. The second decode is the same yaml.v3 decoder,
   so aliases, merge keys and explicit tags resolve exactly as in the real decode.
2. **Walk the `yaml.Node` tree for the integer keys** — has to re-implement alias and merge-key resolution, and a
   form it misses is a bypass.
3. **A named integer type with `UnmarshalYAML` for the two fields** — changes exported field types that
   `cmd/pimonitor`, `internal/collector` and `internal/alert` read.
4. **`UnmarshalYAML` on `Config` or `Alerts`** — `yaml.Node.Decode` takes no `KnownFields`, so unknown keys in that
   mapping would be accepted silently.
5. **Check the integer options after the `-version` short-circuit** — `.nan` and `+.inf` for an `int` field already
   fail inside the real decode with a yaml type error, so the "must be a finite number" message could not hold for
   them.

## Decision

Every float option is checked for NaN and ±Inf first in `Config.Validate()`; the two integer options are checked for
non-finite and fractional values while the file is read, before it is decoded into `Config`. The rules and messages
are in [Configuration](../areas/configuration.md#validation-and-startup-failure).

## Consequences

- An integer option's fractional or non-finite value fails even with `-version`, like any other value the file
  cannot be decoded into; a non-finite float option does not, since `-version` skips `Validate()`.
- The file is decoded twice at startup; it is read once.
- A new integer option needs its field in the second decode, or a fraction is truncated again unnoticed.
