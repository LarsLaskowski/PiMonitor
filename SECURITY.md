# Security Policy

## Supported Versions

PiMonitor is currently pre-1.0. Security fixes are provided for the latest
release only; there is no long-term support branch at this stage.

| Version | Supported |
| ------- | --------- |
| latest  | yes       |
| < latest | no       |

## Reporting a Vulnerability

Please report security vulnerabilities using
[GitHub Security Advisories](https://github.com/larslaskowski/pimonitor/security/advisories/new)
for this repository rather than opening a public issue. This allows a fix to
be prepared and released before the details become public.

If GitHub Security Advisories are not available to you, open an issue with
minimal detail asking for a private contact channel, and a maintainer will
follow up.

Please include:

- A description of the vulnerability and its potential impact
- Steps to reproduce (Pi model, OS/distribution, PiMonitor version)
- Any relevant logs or configuration (with secrets/API keys redacted)

We aim to acknowledge reports within a few days and to release a fix as soon
as reasonably possible depending on severity.

## Threat Model / Design Notes

These notes summarize the security-relevant design decisions in PiMonitor so
reporters and reviewers have context:

- **The main `pimonitor` service runs unprivileged**, as a dedicated system
  user with no special capabilities. It only reads world-readable files under
  `/proc`, `/sys/class/thermal`, `/etc/os-release`, and the existing apt
  cache under `/var/lib/apt/lists/` (via the read-only `apt list --upgradable`
  command). It never invokes anything that requires root.
- **Only apt cache refresh runs as root**, via a separate, narrowly-scoped
  systemd timer/service (`pimonitor-apt-update.timer`) that runs exactly one
  command (`apt-get update`) on a schedule. This unit performs no other
  action and is not reachable from the web-facing service.
- **The HTTP dashboard and REST API are intended for trusted local networks**
  by default (no authentication, plain HTTP). If you expose the API to other
  systems (e.g. for home automation integrations), set the optional `api_key`
  configuration value to require a bearer token on `/api/v1/...` requests,
  and do not expose the service directly to the public internet without a
  reverse proxy providing TLS and additional access control. The bundled
  dashboard keeps working with `api_key` set: it prompts for the key once
  per browser and persists it in `localStorage` (an accepted trade-off —
  anyone with access to the browser profile can read it, and without TLS
  the key is visible on the wire either way).
- Shell-outs (`apt list --upgradable`, optional `vcgencmd measure_temp`) are
  invoked with fixed argument lists (no user input is interpolated into
  shell commands), to avoid command injection.

If you believe any of these assumptions are violated by the current
implementation, please report it as described above.
