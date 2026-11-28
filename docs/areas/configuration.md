# Configuration

## Scope

Where PiMonitor's runtime settings come from — built-in defaults, the YAML config file, the `PIMONITOR_API_KEY` and
`PIMONITOR_OPENHAB_TOKEN` environment variables and command-line flags — how they combine, every option with its type, default and valid
range, and how the process fails at startup when the configuration is invalid. Operators editing
`/etc/pimonitor/config.yaml` rely on it, and every other area takes the defaults and valid ranges of its options from
here instead of restating them.

## Behavior

### Sources and precedence

- The configuration is resolved once at startup from four sources, each overriding the previous one: built-in
  defaults, then the YAML file named by `-config`, then the environment variables (`PIMONITOR_API_KEY`,
  `PIMONITOR_OPENHAB_TOKEN`), then the command-line flags.
- Without `-config` no file is read at all: there is no implicit default path. The shipped systemd unit passes
  `-config /etc/pimonitor/config.yaml` ([Deployment](deployment.md)).
- The configuration is read only at startup. A change takes effect after a restart of the process; there is no reload.
- `packaging/pimonitor.example.yaml` sets every option to its built-in default and documents each one; the installer
  copies it to `/etc/pimonitor/config.yaml` when no config file exists yet.

### Config file

- The file is YAML: one mapping whose keys are the option keys below; `thresholds`, `alerts` and `openhab` are nested mappings,
  `alerts.webhooks` is a list of mappings. Implemented in `internal/config/config.go`.
- An option omitted from the file, or given the YAML null value (`~`), keeps its default. An empty or comment-only
  file is valid and yields all defaults.
- A list given in the file (`alerts.webhooks`) replaces the default list as a whole.
- Every key must be known: an unknown key at any nesting level (for example a typo such as `api_kay`) is an error, so a
  misspelled option never silently falls back to its default.
- A key given twice in the same mapping is an error.
- A value of the wrong YAML type is an error, for example a quoted string (`"5"`) for a number option or a list for a
  string option. An unquoted scalar is accepted for a string option and taken as its text.
- A fractional or non-finite value for an integer option (`processes_top_n`, `alerts.notify_max_retries`, `openhab.poll_interval_seconds`, `openhab.history_window_hours`, `thresholds.openhab_things_offline_warn`, `thresholds.openhab_things_offline_crit`) is an error
  while the file is read, also when it arrives through an alias, a merge key or an explicit `!!float` tag; the
  decoder would otherwise truncate it.
- Only the first YAML document of the file is read; further documents after a `---` separator are ignored.
- A file that cannot be read (missing, no permission) is an error.

### Command-line flags

| Flag | Effect |
| ---- | ------ |
| `-config <path>` | Path of the YAML config file. |
| `-listen <addr>` | Overrides `listen_addr`. |
| `-log-level <level>` | Overrides `log_level`. |
| `-api-key <key>` | Overrides `api_key` and `PIMONITOR_API_KEY`. For local development only: a process's command line is readable by every local user (`/proc/<pid>/cmdline`). |
| `-version` | Prints `pimonitor <version>` to standard output and exits with status 0. |
| `-h`, `-help` | Prints the flag usage to standard error and exits with status 1. |

- A flag given with an empty value (`-listen ""`) overrides nothing.
- A flag value is validated like a file value; `-log-level verbose` fails startup.
- An undefined flag prints the usage to standard error and exits with status 1.
- Flag parsing stops at the first argument that is not a flag; that argument and everything after it, flags
  included, are ignored without an error.
- `-version` skips value validation, so it succeeds with a config whose values are out of range. It does not skip
  reading the file: with `-config`, a missing, unreadable or unparsable file still fails as below, and so does a
  fractional or non-finite value for an integer option, which is checked while the file is read.

### Environment

- `PIMONITOR_API_KEY` and `PIMONITOR_OPENHAB_TOKEN` are the only environment variables PiMonitor reads. When
  `PIMONITOR_API_KEY` is set and non-empty it replaces `api_key` from the file; set to the empty string, or unset, it
  changes nothing, so an empty variable never disables a key configured in the file. `-api-key` overrides it.
  `PIMONITOR_OPENHAB_TOKEN` works the same way for `openhab.api_token`; it has no flag.
- Once read, each variable is removed from the process's own environment, whether it was empty or not. This
  does not remove it from `/proc/<pid>/environ`; see [`SECURITY.md`](../../SECURITY.md).
- The intended way to supply them is a root-only `EnvironmentFile=` for the systemd unit ([Deployment](deployment.md)).

### Options

Types: *string*, *bool* (YAML `true`/`false`), *number* (integer or decimal, and finite: `.nan` and `.inf` in any case or
sign are rejected), *integer* (may be written with a zero fraction such as `5.0` or `1e1`; a fractional value is
rejected). Every range below is
checked at startup; a value outside it fails startup as described under *Validation and startup failure*.

#### Top level

| Key | Type | Default | Valid values | Meaning |
| --- | ---- | ------- | ------------ | ------- |
| `listen_addr` | string | `":8080"` | non-empty `host:port`; the format is checked only when the server starts listening | Address of the dashboard and API. See [HTTP API](http-api.md). |
| `log_level` | string | `"info"` | `debug`, `info`, `warn`, `error` (lower case, exact) | Minimum level of log lines written. |
| `access_log_enabled` | bool | `true` | — | Whether each HTTP request is logged as a debug line ([HTTP API](http-api.md)). |
| `poll_interval_seconds` | number | `5` | 1 to 604800 | Fast collection interval ([Metrics](metrics.md)). |
| `updates_check_minutes` | number | `15` | 1 to 10080 | Slow collection interval ([Metrics](metrics.md)). |
| `updates_stale_threshold_minutes` | number | `720` | 0 to 525600; `0` turns the stale flag off | Age of the apt cache above which the update count is flagged stale ([Metrics](metrics.md)). |
| `history_window_minutes` | number | `60` | > 0 and ≤ 525600, and `history_window_minutes × 60 / poll_interval_seconds` ≤ 1,000,000 | History retention window ([History](history.md)). |
| `history_persist_enabled` | bool | `true` | — | Whether history is saved to and restored from disk ([History](history.md)). |
| `data_dir` | string | `"/var/lib/pimonitor"` | non-empty while `history_persist_enabled` is `true` | Directory of the history file `history.bin` ([History](history.md)). |
| `network_enabled` | bool | `true` | — | Whether network throughput is collected ([Metrics](metrics.md)). |
| `hwmon_enabled` | bool | `true` | — | Whether hwmon sensors are enumerated ([Metrics](metrics.md)). |
| `distro_info_enabled` | bool | `true` | — | Whether the OS distribution is collected ([Metrics](metrics.md)). |
| `pi_model_enabled` | bool | `true` | — | Whether the Raspberry Pi model is collected ([Metrics](metrics.md)). |
| `hostname_enabled` | bool | `true` | — | Whether the host name is read and published ([Metrics](metrics.md)). |
| `processes_enabled` | bool | `false` | — | Whether `GET /api/v1/processes` exists ([Metrics](metrics.md)). |
| `processes_top_n` | integer | `5` | 1 to 100; checked even while `processes_enabled` is `false` | Entries per process ranking ([Metrics](metrics.md)). |
| `prometheus_enabled` | bool | `false` | — | Whether `GET /metrics` exists ([HTTP API](http-api.md)). |
| `api_key` | string | `""` | any; empty means no authentication | Key required by the API ([HTTP API](http-api.md)). |
| `tls_cert` | string | `""` | set together with `tls_key`, or both empty | Certificate file path; both set means HTTPS ([HTTP API](http-api.md)). |
| `tls_key` | string | `""` | set together with `tls_cert`, or both empty | Private key file path. |
| `healthz_max_staleness_seconds` | number | `0` | `0`, or `poll_interval_seconds` to 31536000 | Maximum snapshot age before `/healthz` reports unhealthy ([HTTP API](http-api.md)). `0` derives the bound: 3 × `poll_interval_seconds` + 34 s. |

#### `thresholds`

Warn and critical cutoffs for the dashboard colors and the alert engine ([Alerts](alerts.md),
[Dashboard](dashboard.md)). Every value must be ≥ 0, and each `_warn_` value must be ≤ its `_crit_` counterpart.
There is no upper bound. The four `openhab_*` keys are the exception to the warn ≤ crit rule and to the meaning of
`0`: a `0` turns that cutoff off, so warn ≤ crit is checked only when both are non-zero, `0` for both turns the metric
off, and they are not used for any dashboard color ([0023](../decisions/0023-openhab-alert-thresholds-zero-is-off.md)).

| Key | Type | Default | Unit |
| --- | ---- | ------- | ---- |
| `thresholds.temperature_warn_c` | number | `60` | °C |
| `thresholds.temperature_crit_c` | number | `75` | °C |
| `thresholds.cpu_warn_percent` | number | `80` | % |
| `thresholds.cpu_crit_percent` | number | `95` | % |
| `thresholds.disk_warn_percent` | number | `80` | % |
| `thresholds.disk_crit_percent` | number | `95` | % |
| `thresholds.swap_warn_percent` | number | `50` | % |
| `thresholds.swap_crit_percent` | number | `90` | % |
| `thresholds.memory_warn_percent` | number | `80` | % |
| `thresholds.memory_crit_percent` | number | `95` | % |
| `thresholds.openhab_rss_warn_percent` | number | `50` | % of the Pi's RAM used by the openHAB process; `0` is off |
| `thresholds.openhab_rss_crit_percent` | number | `75` | % of the Pi's RAM used by the openHAB process; `0` is off |
| `thresholds.openhab_things_offline_warn` | integer | `1` | offline Things; `0` is off |
| `thresholds.openhab_things_offline_crit` | integer | `0` | offline Things; `0` is off (the default) |

#### `openhab`

The values of this section are validated even while `openhab.enabled` is `false`. The poll itself is described in
[openHAB](openhab.md).

| Key | Type | Default | Valid values | Meaning |
| --- | ---- | ------- | ------------ | ------- |
| `openhab.enabled` | string | `"auto"` | `auto`, `true`, `false` in any letter case, quoted or not; `yes`, `no`, `on`, `off`, `1`, `0` and the empty string are rejected | `auto` polls only when openHAB is detected; `true` always polls (with alerts on and no openHAB on the host, `openhab_down` then stays at `crit`); `false` turns the feature off, and `GET /api/v1/openhab` answers `404`. |
| `openhab.base_url` | string | `"http://127.0.0.1:8080"` | an `http` URL (the scheme in any letter case) whose host is an IP literal in `127.0.0.0/8`, `::1` or `::ffff:127.x.y.z`, or `localhost`; a port of 1 to 65535 or none; no user info, no path other than `/`, no query, no fragment; short or numeric address forms (`127.1`, `2130706433`, `0x7f.0.0.1`, `0127.0.0.1`), a zone, a trailing dot and `https` are rejected | Root URL of openHAB's REST API. |
| `openhab.api_token` | string | `""` | no whitespace or control character; empty means no token | openHAB API token, sent as a bearer token; `PIMONITOR_OPENHAB_TOKEN` overrides it. Never part of any response or log line. |
| `openhab.poll_interval_seconds` | integer | `60` | 60 to 86400 | Time between openHAB polls. |
| `openhab.history_window_hours` | integer | `24` | 1 to 168; checked even while `openhab.enabled` is `false`; the error reads `openhab.history_window_hours must be between 1 and 168 (got <value>)` | How long the openHAB memory history is kept, independently of `history_window_minutes`; each of its series holds at most `openhab.history_window_hours` × 3600 / `openhab.poll_interval_seconds` points, at least 1 (1,440 at the defaults, 10,080 at most) ([History](history.md)). Also reported by `GET /api/v1/openhab`. |

#### `alerts`

The values of this section are validated even while `alerts.enabled` is `false`.

| Key | Type | Default | Valid values | Meaning |
| --- | ---- | ------- | ------------ | ------- |
| `alerts.enabled` | bool | `true` | — | Whether the alert engine runs ([Alerts](alerts.md)). |
| `alerts.for_seconds` | number | `30` | 0 to 604800; `0` fires on the first crossing | Debounce window ([Alerts](alerts.md)). |
| `alerts.openhab_for_seconds` | number | `300` | 0 to 604800; `0` fires on the first crossing | Debounce window of the three `openhab_*` alerts, counted per openHAB poll ([Alerts](alerts.md)); with `openhab.enabled: true` on a host without openHAB, `openhab_down` fires once it has held that long. |
| `alerts.webhooks` | list | empty | entries as below | Webhook destinations ([Notifications](notifications.md)). |
| `alerts.notify_max_retries` | integer | `3` | ≥ 0; `0` means one attempt | Retries of a failed delivery ([Notifications](notifications.md)). |
| `alerts.notify_retry_backoff_seconds` | number | `1` | 0 to 604800 | Delay before the first retry ([Notifications](notifications.md)). |
| `alerts.notify_min_interval_seconds` | number | `5` | 0 to 604800; `0` turns rate limiting off | Rate-limit window ([Notifications](notifications.md)). |

#### `alerts.webhooks[]`

| Key | Type | Default | Valid values | Meaning |
| --- | ---- | ------- | ------------ | ------- |
| `url` | string | none (required) | non-empty; its format is not checked | Endpoint the event is POSTed to. |
| `min_level` | string | `""`, which means `warn` | `""`, `warn`, `crit` (lower case, exact) | Lowest severity delivered. |
| `template` | string | `""`, which means the default payload | a template that parses; checked at startup | Request body template ([Notifications](notifications.md)). |
| `content_type` | string | `""`, which means `application/json` | any | `Content-Type` of the request. |
| `timeout_seconds` | number | `0`, which means 10 s | 0 to 604800 | Timeout of one delivery attempt. |

### Visibility to clients

- `GET /api/v1/config` returns `poll_interval_seconds`, `history_window_minutes`, `network_enabled` and the
  `thresholds` section to API clients and the dashboard; the response shape is in [`API.md`](../API.md).
- `api_key` and `openhab.api_token` are never part of any response or log line. Whether a token is set is reported by
  `GET /api/v1/openhab` (`api_token_configured`), never its value.

### Validation and startup failure

- All values are validated once, after all four sources are merged and before anything is started; the first
  violation found ends startup.
- The finiteness check runs first: a NaN or infinite value of any number option fails with
  `<option> must be a finite number`, naming the key as written in the file (`thresholds.cpu_warn_percent`,
  `alerts.webhooks[0].timeout_seconds`), before any range is compared. The integer options are checked while the
  file is read, before it is decoded: a non-finite value fails with the same message, a fractional one with
  `<option> must be a whole number (got <value>)`, for example
  `pimonitor: load config: processes_top_n must be a whole number (got 2.5)`.
- The upper bounds follow the finiteness check and come before every other range check, so a value above its bound
  fails with `<option> must be <= <bound> (got <value>)` even when it also breaks another rule: `poll_interval_seconds`,
  `alerts.notify_retry_backoff_seconds`, `alerts.notify_min_interval_seconds` and
  `alerts.webhooks[<i>].timeout_seconds` (`<i>` the first webhook above it) at most 604800, `updates_check_minutes` at
  most 10080, `updates_stale_threshold_minutes` and `history_window_minutes` at most 525600,
  `healthz_max_staleness_seconds` at most 31536000. The alert and webhook bounds are checked also while
  `alerts.enabled` is `false`; the bounds keep the conversion to a duration from overflowing
  ([0035](../decisions/0035-float-duration-options-capped-at-seven-or-365-days.md)).
- `poll_interval_seconds` and `updates_check_minutes` below 1 fail with `<option> must be >= 1 (got <value>)`, also
  for zero and negative values
  ([0036](../decisions/0036-poll-and-updates-intervals-at-least-one-unit.md)).
- The openHAB alert options fail with `thresholds.<key> must be >= 0 (got <value>)`,
  `thresholds.<warn key> (<value>) must be <= thresholds.<crit key> (<value>)` (only when both are non-zero) and
  `alerts.openhab_for_seconds must be >= 0 (got <value>)` or `alerts.openhab_for_seconds must be <= 604800 (got <value>)`,
  also while `alerts.enabled` is `false`.
- `alerts.for_seconds` fails with `alerts.for_seconds must be >= 0 (got <value>)` or
  `alerts.for_seconds must be <= 604800 (got <value>)`, also while `alerts.enabled` is `false`; the bound keeps the
  conversion to a duration from overflowing ([0034](../decisions/0034-alert-debounce-windows-capped-at-seven-days.md)).
- An invalid `openhab` option fails with its own text, which never quotes the URL or the token:
  `openhab.enabled must be auto, true or false (got "<value>")`,
  `openhab.base_url must be an http URL with a loopback host (127.0.0.0/8, ::1 or localhost) and no user, path, query or fragment`,
  `openhab.poll_interval_seconds must be between 60 and 86400 (got <value>)` and
  `openhab.api_token must not contain whitespace or control characters`.
- After validation, each webhook `template` is parsed; a template that does not parse ends startup.
- A startup failure writes one line `pimonitor: <reason>` to standard error and exits with status 1 before any
  port is opened or any metric is collected. The reason names the option, for example
  `pimonitor: load config: poll_interval_seconds must be >= 1 (got 0)`,
  `pimonitor: load config: parse <file>: …` for a file error, and
  `pimonitor: configure alert webhooks: alerts.webhooks[0].template: …` for a template.
- Webhooks configured while `alerts.enabled` is `false` are valid; startup logs a warning that no notifications will
  be sent.

## Limits and errors

- Values that pass validation can still fail when the server starts listening: an unparsable `listen_addr`, a port in
  use or below 1024 under the shipped unit, or `tls_cert`/`tls_key` files that are missing or unreadable. The process
  then exits with status 1 and the error on standard error ([Deployment](deployment.md)).
- Validation never checks or reports the value of `api_key` or quotes `openhab.base_url` or `openhab.api_token`; the messages quote only the offending value of
  non-secret options.
- `history_window_minutes × 60 / poll_interval_seconds` above 1,000,000 points per series is rejected, whatever the
  two values are individually.
- A config file written for a newer release, containing a key this version does not know, fails startup. A file from
  an older release keeps working: options added since then take their defaults.

## Related decisions

- [0013: openHAB requests go only to a loopback address over plain HTTP](../decisions/0013-openhab-requests-only-to-loopback-http.md) — why `openhab.base_url` is restricted.
- [0034: Alert debounce windows are capped at seven days](../decisions/0034-alert-debounce-windows-capped-at-seven-days.md)
- [0035: Float duration options are capped at seven or 365 days](../decisions/0035-float-duration-options-capped-at-seven-or-365-days.md)
- [0036: The fast and slow collection intervals are at least one second and one minute](../decisions/0036-poll-and-updates-intervals-at-least-one-unit.md)
- [0008: Reject non-finite numbers, and fractional values of integer options while reading the file](../decisions/0008-reject-non-finite-and-fractional-config-numbers.md)

## Not here

- What each option does in detail: [Metrics](metrics.md), [History](history.md), [Alerts](alerts.md),
  [Notifications](notifications.md), [HTTP API](http-api.md), [Dashboard](dashboard.md), [openHAB](openhab.md).
- The API key's header format and comparison, TLS serving and the `/healthz` check: [HTTP API](http-api.md).
- The `GET /api/v1/config` response shape: [`API.md`](../API.md).
- Where the config file lives on the Pi, its owner and mode, and the `EnvironmentFile=`: [Deployment](deployment.md).
- Why the API key must not be passed as a flag, and the limits of removing it from the environment:
  [`SECURITY.md`](../../SECURITY.md).

## Open questions

None.
