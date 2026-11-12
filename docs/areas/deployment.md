# Deployment

## Scope

How PiMonitor gets onto a Raspberry Pi and runs there: the release archive and `download-latest.sh`, what
`install.sh` does on a first install and on an upgrade, the three systemd units with their users, privileges and
sandboxing, the root-owned apt refresh timer, the data directory, and how the process starts, logs and stops. Operators
installing, upgrading or hardening PiMonitor rely on it.

## Behavior

### Release archive

- A release archive is named `pimonitor_<version>_linux_<arch>.tar.gz`, `<version>` without the leading `v`, `<arch>`
  `arm64` (64-bit Raspberry Pi OS) or `armv6` (32-bit Raspberry Pi OS, any Pi). It extracts to one directory of the
  same name without `.tar.gz`.
- That directory holds the binary `pimonitor`, `install.sh`, `download-latest.sh`, `pimonitor.service`,
  `pimonitor-apt-update.service`, `pimonitor-apt-update.timer`, `pimonitor.example.yaml`, `README.md` and `LICENSE.md`
  side by side, so installing is `cd <dir> && sudo ./install.sh`.

### `download-latest.sh`

- Asks the GitHub API for the latest release of `larslaskowski/pimonitor`, downloads the archive for the Pi's
  architecture into the current directory, extracts it there, lists the extracted files and prints the
  `sudo ./install.sh` command to run next. It does not install anything and does not need root.
- The architecture comes from `uname -m`: `aarch64`/`arm64` selects `arm64`; `armv6l`, `armv7l` and `armv8l` select
  `armv6`. The environment variable `ARCH` (`arm64` or `armv6`) overrides the detection.
- Every request is HTTPS only, redirects included, and is retried up to 3 times, 2 s apart, on any transfer error.
- The downloaded archive is left in the current directory next to the extracted directory.
- Requires `curl` and `tar`.

### `install.sh`

The installer must run as root. It is idempotent: a first install and an upgrade are the same run. Implemented in
`packaging/install.sh`; in order, it:

1. Picks the binary: the path given as its only argument (which must be a file), else `pimonitor` next to the
   script, else one it builds from the source checkout around the script with a local Go toolchain (static,
   `CGO_ENABLED=0`, version from `git describe --tags --always --dirty`, or `dev` outside a Git checkout).
2. Installs the binary to `/usr/local/bin/pimonitor`, mode `755`, replacing an existing one.
3. Creates the system user `pimonitor` (no home directory, shell `/usr/sbin/nologin`, with its own `pimonitor`
   group) if it does not exist; an existing user is left as it is.
4. Creates `/etc/pimonitor`, owner `root:pimonitor`, mode `750`.
5. Writes `/etc/pimonitor/config.yaml` from `pimonitor.example.yaml`, owner `root:pimonitor`, mode `640`, only when
   no such file exists. An existing config file keeps its content; only its owner and mode are reset to
   `root:pimonitor` and `640`.
6. Installs `pimonitor.service`, `pimonitor-apt-update.service` and `pimonitor-apt-update.timer` to
   `/etc/systemd/system/`, mode `644`, replacing existing ones. Drop-ins under `/etc/systemd/system/<unit>.d/` are
   not touched, so local unit changes belong there.
7. Reloads systemd, then enables and starts `pimonitor.service` and `pimonitor-apt-update.timer`. Either unit that was
   already active before the run is restarted, so a new binary, unit or schedule takes effect at once.
8. Runs `pimonitor-apt-update.service` once and waits for it, so the update count is available on first view. Its
   failure prints a warning; the installer still succeeds.

Any other failing step ends the installer with a non-zero status. The installer's success does not mean the service
is running: it does not wait to see whether `pimonitor.service` stays up (check with
`systemctl status pimonitor.service`).

### `pimonitor.service`

- Runs `/usr/local/bin/pimonitor -config /etc/pimonitor/config.yaml` as user and group `pimonitor`, with `video` as
  its only supplementary group (needed by `vcgencmd` to open `/dev/vcio_gencmd` or `/dev/vchiq`). It never runs as
  root and holds no capabilities.
- Starts after `network-online.target`, is wanted by `multi-user.target`, and is restarted 5 s after it exits with
  a failure (`Restart=on-failure`, `RestartSec=5`).
- The API key may be kept out of the config file in an environment file holding `PIMONITOR_API_KEY=<key>`: the unit
  carries a commented-out `EnvironmentFile=/etc/pimonitor/pimonitor.env`, to be enabled (preferably in a drop-in)
  with that file owned `root:root`, mode `600`. The key is never put on the `ExecStart=` command line
  ([Configuration](configuration.md#environment)).
- Sandboxing:

  | Directives | Effect |
  | ---------- | ------ |
  | `NoNewPrivileges=true`, `CapabilityBoundingSet=` (empty), `RestrictSUIDSGID=true` | No privilege can be gained; ports below 1024 cannot be bound. |
  | `ProtectSystem=strict`, `StateDirectory=pimonitor`, `ReadWritePaths=/var/lib/pimonitor` | The whole file system is read-only except `/var/lib/pimonitor`. |
  | `ProtectHome=true`, `PrivateTmp=true` | Home directories are inaccessible; `/tmp` is private to the service. |
  | `ProtectKernelTunables`, `ProtectKernelModules`, `ProtectKernelLogs`, `ProtectControlGroups`, `ProtectHostname` (all `true`) | Kernel tunables and cgroups are read-only, kernel modules cannot be loaded, the kernel log cannot be read, the hostname cannot be changed. |
  | `RestrictAddressFamilies=AF_INET AF_INET6 AF_UNIX` | Only IPv4, IPv6 and Unix sockets. |
  | `RestrictNamespaces`, `LockPersonality`, `MemoryDenyWriteExecute`, `RestrictRealtime`, `RemoveIPC` (all `true`) | No namespaces, personality changes, writable-executable memory, realtime scheduling or leftover IPC objects. |
  | `SystemCallArchitectures=native`, `SystemCallFilter=@system-service`, `SystemCallFilter=~@privileged @resources`, `SystemCallErrorNumber=EPERM` | Only the service system calls; a filtered call fails with `EPERM` instead of killing the process. |
  | `UMask=0077` | Files the service creates are readable only by the `pimonitor` user. |

- `PrivateDevices=` and `ProtectClock=` are deliberately not set: either would block the device nodes `vcgencmd`
  needs.
- The same restrictions apply to the child processes the service starts (`apt list --upgradable`, `vcgencmd`).

### `pimonitor-apt-update.service` and `.timer`

- The service runs `/usr/bin/apt-get update` once (`Type=oneshot`) as root. It is the only part of PiMonitor with
  write access to the apt cache and the only one running as root; `pimonitor.service` never refreshes the cache
  itself, it only reads it ([Metrics](metrics.md)).
- Its sandbox: `NoNewPrivileges=true`, `ProtectSystem=strict` with write access only to `/var/lib/apt` and
  `/var/cache/apt`, `ProtectHome=true`, `PrivateTmp=true`, `ProtectKernelModules=true`, `ProtectKernelLogs=true`,
  `ProtectControlGroups=true`, and the capabilities limited to `CAP_CHOWN CAP_DAC_OVERRIDE CAP_FOWNER CAP_SETGID
  CAP_SETUID`. It runs at `Nice=19` with idle I/O priority.
- The service has no `[Install]` section; the timer starts it 10 minutes after boot and then 6 hours after each
  activation. The timer is wanted by `timers.target`.

### Data directory

- `/var/lib/pimonitor` is created by systemd when the service starts (`StateDirectory=`), owned by the `pimonitor`
  user, and is the only path the service can write. It holds the history file `history.bin`
  ([History](history.md)); neither the installer nor an upgrade removes it.
- `data_dir` ([Configuration](configuration.md)) must point at a directory the service can write. Under the shipped
  unit that is `/var/lib/pimonitor` only; another directory needs a drop-in that grants write access to it.

### Process lifecycle

- At startup the process resolves and validates its configuration
  ([Configuration](configuration.md#validation-and-startup-failure)), starts collecting and starts listening on
  `listen_addr`.
- Log lines go to standard error as `key=value` text (`time`, `level`, `msg`, then fields), filtered by `log_level`;
  under systemd they end up in the journal (`journalctl -u pimonitor`).
- `SIGTERM` or `SIGINT` (`systemctl stop`, `systemctl restart`) shuts down gracefully: the server stops accepting
  connections and in-flight requests may finish, then the process waits for the collector's last history snapshot
  ([History](history.md)); both together are bounded by 10 s. The process then exits with status 0, or with status 1
  if the HTTP server did not finish within the 10 s.
- `pimonitor -version` prints the installed version without starting the service.

### Upgrading

- An upgrade is a run of the new release's `install.sh` (*`install.sh`* above): binary and unit files are replaced,
  running units are restarted, the config file's content, unit drop-ins and the data directory are kept.
- Replacing only the binary does not apply changes a release makes to the unit files.

## Limits and errors

- `install.sh` run without root exits with status 1. Without a binary argument, without `pimonitor` next to the
  script and without `go` on the `PATH`, it exits with status 1; so does a binary path that is not a file.
- When `pimonitor.service` cannot start (invalid configuration, port in use, port below 1024, unreadable TLS files) the
  process exits with status 1 and systemd restarts it every 5 s; the reason is in the journal.
- A `SIGHUP` ends the process; it does not reload the configuration, and the unit defines no reload.
- `download-latest.sh` exits with status 2 for an architecture it cannot map or an unsupported `ARCH`, and with
  status 1 when `curl` or `tar` is missing, the latest version cannot be determined, the target directory already
  exists, or the archive does not extract to the expected directory. It does not verify the release checksum.
- There is no uninstall script.

## Related decisions

None yet.

## Not here

- Every configuration option, its default and valid range, and the precedence of file, environment and flags:
  [Configuration](configuration.md).
- What the service reads from `/proc`, `/sys`, `apt` and `vcgencmd`, and when: [Metrics](metrics.md).
- The `history.bin` format and when it is written or restored: [History](history.md).
- TLS, the API key and the `/healthz` check: [HTTP API](http-api.md).
- The threat model and the reasoning behind the privilege split: [`SECURITY.md`](../../SECURITY.md).
- Building, tagging and publishing releases: [`CONTRIBUTING.md`](../CONTRIBUTING.md).

## Open questions

- `pimonitor-apt-update.timer` sets `Persistent=true`, which systemd applies only to `OnCalendar=` timers; with the
  timer's `OnBootSec=`/`OnUnitActiveSec=` it has no effect. Should it be removed or the schedule changed to a
  calendar one?
- `download-latest.sh` does not check the archive against the release's `checksums.txt`. Should it?
