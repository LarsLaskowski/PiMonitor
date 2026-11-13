# 0006: Publish the host name by default, read once and omitted when unknown

- **Status:** Accepted
- **Date:** 2026-11-13
- **Area:** Metrics
- **Source:** Issue #179
- **Supersedes:** —

## Context

Operators with several Pis cannot tell their dashboards or API responses apart (issue #179). The host name is the
identifier they already use (SSH prompt, DNS, router). `GET /api/v1/metrics` is reachable without authentication
unless `api_key` is set, so publishing the name is a disclosure on the LAN, the same kind the distribution and the
Pi model already make behind `distro_info_enabled` and `pi_model_enabled`. The service runs with
`ProtectHostname=true` (`packaging/pimonitor.service`), so it sees the host name the host had when the service
started. The existing `system` keys are always present and empty when unknown.

## Options considered

1. **Published by default, opt-out `hostname_enabled`** — the feature works without configuration, like the
   distribution and the model; operators who do not want the name on the LAN turn it off.
2. **Off by default, opt-in** — no new disclosure on upgrade, but the dashboard keeps looking the same for nearly
   everyone and the feature is invisible.
3. **Read through the standard library's host name call instead of `/proc/sys/kernel/hostname`** — same value on
   Linux, but no fixture path for tests.
4. **Read on every tick** — would notice a rename, but `ProtectHostname=true` freezes the name the service sees at
   start, so it would never change anyway.
5. **Key present and empty when unknown, like its siblings** — consistent inside `system`, but an empty host name
   is a value no host has; the earlier additive keys (`wireless`, `pihole`) are omitted when they carry nothing.

## Decision

The host name is read once at startup from `/proc/sys/kernel/hostname`, published as `system.hostname` by default
with `hostname_enabled` as the opt-out (option 1), and the key is omitted whenever there is no name (option 5
rejected). The rules are in [Metrics](../areas/metrics.md#system-identity-and-uptime) and
[Configuration](../areas/configuration.md).

## Consequences

- An upgrade starts publishing the host name on hosts without `api_key`; the pull request description and the
  example configuration name the opt-out.
- `system.hostname` is the one `system` key a client must treat as optional.
- A renamed host shows its new name only after a service restart.
