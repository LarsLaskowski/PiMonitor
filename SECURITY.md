# Security Policy

## Supported Versions

Security fixes are provided for the latest release only; there is no
long-term support branch.

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
  `/proc`, `/sys/class/thermal`, `/sys/devices/system/cpu/*/cpufreq`,
  `/sys/class/hwmon`, `/etc/os-release`, and the existing apt
  cache under `/var/lib/apt/lists/` (via the read-only `apt list --upgradable`
  command) and, when Pi-hole is installed, the world-readable
  `/etc/pihole/versions`, plus the optional, fixed-argument `vcgencmd measure_temp` /
  `measure_temp pmic` / `get_throttled` calls. It never invokes anything that
  requires root. Its only extra privilege is membership in the `video` group
  (`SupplementaryGroups=video` in `pimonitor.service`), which Raspberry Pi OS
  requires for opening the firmware device `vcgencmd` uses
  (`/dev/vcio_gencmd` on current releases, `/dev/vchiq` on older ones). The group grants access to GPU,
  framebuffer, camera (V4L2) and firmware-mailbox devices (the mailbox also
  accepts write-type property calls), but not to root.
- **openHAB status makes the service an HTTP client of its own machine, and only of that.** When `openhab.enabled` is
  not `false`, the service reads the existence and owner of `/usr/share/openhab`, `/etc/openhab` and
  `/var/lib/openhab`, the `cmdline`, `stat` and `status` files of other users' processes under `/proc` (with
  `hidepid=` mounted they are not visible, and the openHAB values then degrade to absent) and `/proc/net/tcp` and
  `/proc/net/tcp6`, and sends `GET` requests to `openhab.base_url`. That URL must be `http` with a loopback host; the
  connection is checked against a loopback address again when it is made, no proxy is used, redirects are not
  followed, each request has a 5 s timeout and at most 8 MiB of a response is read. See
  [decision 0013](docs/decisions/0013-openhab-requests-only-to-loopback-http.md).
- **The openHAB API token** (`openhab.api_token` in the config file, mode `640 root:pimonitor`, or
  `PIMONITOR_OPENHAB_TOKEN`, loadable from the same restricted `EnvironmentFile=`) is sent only as an
  `Authorization: Bearer` header to `base_url`, only when set, and only over a connection whose server side runs as
  the owner of `/var/lib/openhab`: the connection's own entry in `/proc/net/tcp` or `/proc/net/tcp6` is looked up
  before any byte is sent, so a local user who binds openHAB's port while openHAB is down does not receive it. When
  the directory is missing, the lookup fails or the platform is not Linux, the token is not sent. It never appears in
  a log line, in `GET /api/v1/openhab`, in `GET /api/v1/config` or in any other response. With an
  `openhab.api_token` set, `GET /api/v1/openhab` serves Thing UIDs, labels and status descriptions that openHAB itself
  serves only to authenticated users (`GET /metrics` serves their counts per status); set `api_key` if the LAN should not read them.
- **Webhook URLs are often the webhook's credential** (Slack, Discord, ntfy tokens in the path or query). The full
  URLs live only in the configuration file (mode `640 root:pimonitor`). The service log names a webhook by its index
  in `alerts.webhooks` and the URL's scheme and host; the notifier never logs userinfo, path, query or fragment
  itself, error texts included. Not covered: a service that puts its secret in the host name, and an endpoint that
  echoes the request line back in a malformed response, which `net/http` quotes in the error text.
- **Only apt cache refresh runs as root**, via a separate, narrowly-scoped
  systemd timer/service (`pimonitor-apt-update.timer`) that runs exactly one
  command (`apt-get update`) on a schedule. This unit performs no other
  action and is not reachable from the web-facing service.
- **The HTTP dashboard and REST API are intended for trusted local networks**
  by default (no authentication, plain HTTP). If you expose the API to other
  systems (e.g. for home automation integrations), set the optional `api_key`
  configuration value to require a bearer token on `/api/v1/...` requests
  (and on `GET /metrics`, if `prometheus_enabled` is set),
  and do not expose the service directly to the public internet without
  TLS and additional access control, either via a reverse proxy or by
  setting the optional `tls_cert` / `tls_key` configuration values to have
  the service terminate TLS itself. The bundled
  dashboard keeps working with `api_key` set: it prompts for the key once
  per browser and persists it in `localStorage` (an accepted trade-off —
  anyone with access to the browser profile can read it, and without TLS
  the key is visible on the wire either way).
- **Supply the API key through the config file or the environment, not the
  command line.** `/etc/pimonitor/config.yaml` is kept at mode `640
  root:pimonitor` by `install.sh`; alternatively set `PIMONITOR_API_KEY`,
  which systemd can load from an `EnvironmentFile=` with equally restricted
  permissions. The `-api-key` flag is a development convenience only: a
  process's command line is world-readable via `/proc/<pid>/cmdline`, so a
  key passed that way is exposed to every local user on the machine.
- Shell-outs (`apt list --upgradable`, optional `vcgencmd measure_temp`) are
  invoked with fixed argument lists (no user input is interpolated into
  shell commands), to avoid command injection.
- Shell-outs are also invoked with an explicit, minimal environment rather
  than inheriting the service's own — neither `apt` nor `vcgencmd` needs
  anything beyond `PATH` (and, for `apt`, a couple of apt-specific
  variables), so `PIMONITOR_API_KEY` and `PIMONITOR_OPENHAB_TOKEN` are never copied into a child process's
  `/proc/<pid>/environ`.
- `config.Load` calls `os.Unsetenv("PIMONITOR_API_KEY")` and `os.Unsetenv("PIMONITOR_OPENHAB_TOKEN")` immediately
  after reading each variable, so nothing later in the `pimonitor` process itself
  (a future code path, a library, `os.Environ()`) can read the key or token back out
  of the environment. **This does not scrub the key or token from
  `/proc/<pid>/environ`.** We verified this rather than assuming it: on
  Linux, that file is generated by the kernel once, from the raw
  `argv`/`envp` block captured at the process's `execve()`, and is never
  regenerated from later `setenv`/`unsetenv` calls — `os.Unsetenv` only
  updates the Go runtime's in-process copy of the environment. A local user
  who can already read `/proc/<pid>/environ` for the `pimonitor` process
  (in practice, root or the `pimonitor` service user) can recover the key or token
  from it for the life of the process regardless of this call. The
  meaningful protections remain the ones above: keep `api_key`/
  `PIMONITOR_API_KEY` (and `openhab.api_token`/`PIMONITOR_OPENHAB_TOKEN`) out of the command line and in files with restricted
  permissions (mode `640 root:pimonitor`, as `install.sh` sets up), and rely
  on the explicit subprocess environment (not this unset) to keep the key and token
  out of `apt`'s and `vcgencmd`'s own environments.
- **Requests to any endpoint behind `apiRoute`** (every `/api/v1/...` route,
  plus `GET /metrics` when `prometheus_enabled` is set) **share a
  concurrency limit** (`withMaxInFlight`,
  `internal/httpapi/middleware.go`), so a request flood — from another
  device on the LAN, a misconfigured integration, or a buggy retry loop —
  is bounded rather than able to multiply CPU/memory work without limit on
  constrained hardware such as a Pi Zero. This does not require
  authentication and is not a substitute for `api_key`; it is a resource-
  exhaustion mitigation, not an access control. See
  [`docs/API.md`](docs/API.md#rate-limiting).

If you believe any of these assumptions are violated by the current
implementation, please report it as described above.
