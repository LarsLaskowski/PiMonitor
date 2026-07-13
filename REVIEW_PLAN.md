# Project Review Plan — Performance, Security, Robustness

- **Date:** 2026-07-13
- **Scope:** full repository review of PiMonitor covering the three focus
  areas **performance**, **security**, and **robustness**.
- **Status:** ✅ completed — all files reviewed, all findings filed as issues.
- **Issue tracking:** every finding below has a GitHub issue labeled
  `review-report` plus one category label (`security`, `performance`, or
  `robustness`).

## Review criteria

| Area | What was checked |
| --- | --- |
| Performance | Allocation patterns in hot paths (5 s collection tick, per-request JSON encoding), payload sizes on slow links (Pi Zero / Wi-Fi), unbounded growth of in-memory state, redundant work per tick/request. |
| Security | AuthN/AuthZ of the REST API, injection surfaces (XSS, command injection), secret handling (API key at rest and in transit), HTTP response hardening, systemd sandboxing, file permissions set by the installer, CI/release pipeline permissions. |
| Robustness | Behavior on invalid configuration, hardware/files missing at startup or disappearing at runtime (thermal zones, mounts, interfaces), blocking syscalls/shell-outs inside the collection loop, parser tolerance for unexpected `/proc`/`/sys`/apt output, graceful shutdown. |

## File checklist

Checked off (`[x]`) once reviewed against all three criteria.

### Application entry & configuration

- [x] `cmd/pimonitor/main.go`
- [x] `internal/config/config.go`
- [x] `internal/config/config_test.go`

### HTTP layer

- [x] `internal/httpapi/server.go`
- [x] `internal/httpapi/handlers.go`
- [x] `internal/httpapi/middleware.go`
- [x] `internal/httpapi/handlers_test.go`

### Collectors

- [x] `internal/collector/collector.go`
- [x] `internal/collector/types.go`
- [x] `internal/collector/cpu.go`
- [x] `internal/collector/loadavg.go`
- [x] `internal/collector/memory.go`
- [x] `internal/collector/disk.go`
- [x] `internal/collector/network.go`
- [x] `internal/collector/temperature.go`
- [x] `internal/collector/sysinfo.go`
- [x] `internal/collector/updates.go`
- [x] `internal/collector/uptime.go`
- [x] `internal/collector/ringbuffer.go`
- [x] `internal/collector/collector_test.go`
- [x] `internal/collector/cpu_test.go`
- [x] `internal/collector/loadavg_test.go`
- [x] `internal/collector/memory_test.go`
- [x] `internal/collector/disk_test.go`
- [x] `internal/collector/network_test.go`
- [x] `internal/collector/temperature_test.go`
- [x] `internal/collector/sysinfo_test.go`
- [x] `internal/collector/updates_test.go`
- [x] `internal/collector/uptime_test.go`
- [x] `internal/collector/ringbuffer_test.go`
- [x] `internal/collector/testhelpers_test.go`

### Web frontend

- [x] `internal/web/embed.go`
- [x] `internal/web/embed_test.go`
- [x] `internal/web/assets/index.html`
- [x] `internal/web/assets/app.js`
- [x] `internal/web/assets/chart.js`
- [x] `internal/web/assets/gauge.js`
- [x] `internal/web/assets/style.css`

### Packaging & deployment

- [x] `packaging/install.sh`
- [x] `packaging/pimonitor.service`
- [x] `packaging/pimonitor-apt-update.service`
- [x] `packaging/pimonitor-apt-update.timer`
- [x] `packaging/pimonitor.example.yaml`

### Build, CI & project meta

- [x] `Makefile`
- [x] `go.mod` / `go.sum`
- [x] `.github/workflows/ci.yml`
- [x] `.github/workflows/release.yml`
- [x] `.goreleaser.yaml`
- [x] `.gitignore`
- [x] `README.md`
- [x] `SECURITY.md`
- [x] `docs/API.md`
- [x] `CLAUDE.md`
- [x] `.github/pull_request_template.md`, `.github/ISSUE_TEMPLATE/*`
- [x] `.claude/skills/*` (create-pr, review-pr, fix-issue)

## Findings

Summary — full, implementation-ready write-ups (problem, fix steps,
acceptance criteria) live in the linked issues.

| ID | Category | Severity | Finding | Issue |
| --- | --- | --- | --- | --- |
| S1 | security | medium | API key compared with `==` instead of a constant-time comparison (timing side channel) — `internal/httpapi/middleware.go` | [#18](https://github.com/LarsLaskowski/PiMonitor/issues/18) |
| S2 | security | medium | Dashboard interpolates mountpoint/interface names into `innerHTML` unescaped (XSS) — `internal/web/assets/app.js` | [#19](https://github.com/LarsLaskowski/PiMonitor/issues/19) |
| S3 | security | medium | Installer writes `/etc/pimonitor/config.yaml` world-readable (mode 644) although it may contain `api_key` — `packaging/install.sh` | [#20](https://github.com/LarsLaskowski/PiMonitor/issues/20) |
| S4 | security | low–med | No security response headers (CSP, `X-Content-Type-Options`, frame protection, referrer policy) — `internal/httpapi` | [#21](https://github.com/LarsLaskowski/PiMonitor/issues/21) |
| S5 | security | low | systemd sandboxing can be tightened (namespaces, syscall filter, W^X, …) — `packaging/pimonitor.service` | [#22](https://github.com/LarsLaskowski/PiMonitor/issues/22) |
| P1 | performance | medium | JSON API responses served uncompressed; history payload is ~hundreds of KB per poll on default config — `internal/httpapi` | [#23](https://github.com/LarsLaskowski/PiMonitor/issues/23) |
| P2 | performance | medium | Per-device history maps (`diskHist`/`rxHist`/`txHist`) never evict vanished devices → unbounded growth under veth/mount churn — `internal/collector/collector.go` | [#24](https://github.com/LarsLaskowski/PiMonitor/issues/24) |
| R1 | robustness | high | No config validation; `poll_interval_seconds: 0` panics `time.NewTicker` and crash-loops the service — `internal/config`, `internal/collector` | [#25](https://github.com/LarsLaskowski/PiMonitor/issues/25) |
| R2 | robustness | high* | `statfs` on hung network filesystems (NFS/CIFS not excluded, no timeout) freezes the entire collection loop — `internal/collector/disk.go` | [#26](https://github.com/LarsLaskowski/PiMonitor/issues/26) |
| R3 | robustness | medium | Disk usage percent based on `Bfree` instead of `Bavail` (under-reports fullness vs. `df`); duplicate mountpoints not deduplicated — `internal/collector/disk.go` | [#27](https://github.com/LarsLaskowski/PiMonitor/issues/27) |
| R4 | robustness | low | Thermal zone / `vcgencmd` detected only at startup, never re-detected; constructor comment promises recovery that doesn't exist — `internal/collector/temperature.go` | [#28](https://github.com/LarsLaskowski/PiMonitor/issues/28) |

\* high in deployments with network mounts (a Pi mounting a NAS share is common).

### Suggested implementation order

1. **R1** (#25) — crash bug, smallest fix, unlocks safe config handling.
2. **S1** (#18), **S3** (#20) — small, self-contained security fixes.
3. **S2** (#19), **S4** (#21) — frontend XSS fix plus CSP; S4's CSP also mitigates S2, do S2 first so the CSP never masks it.
4. **R2** (#26), **R3** (#27) — both touch `internal/collector/disk.go`; implement together to avoid merge conflicts.
5. **P2** (#24) — collector map eviction.
6. **P1** (#23) — gzip middleware.
7. **S5** (#22), **R4** (#28) — hardening and low-severity cleanup; S5 needs on-device verification.

## Minor observations (no issue filed)

Noted for completeness; each is cosmetic or debatable and not worth a
tracked finding:

- `go.mod` marks `gopkg.in/yaml.v3` as `// indirect` although it is a
  direct dependency (`go mod tidy` fixes the comment).
- `app.js` treats a temperature of exactly `0 °C` as "n/a" (`if
  (snap.temperature.celsius)` — falsy-zero check).
- After a failed `slowTick`, the previous updates result (including its
  old `checked_at`) is served indefinitely; acceptable since `checked_at`
  makes the staleness visible to clients.
- `GET /healthz` reports `ok` before the first collection tick has
  completed; fine for a liveness probe, but a readiness semantic would
  need a separate check.

## What was explicitly reviewed and found sound

- No command injection: shell-outs (`apt list --upgradable`, `vcgencmd
  measure_temp`) use fixed argument vectors with timeouts; no user input
  reaches them.
- Path traversal on the static file server is handled by
  `http.FileServerFS` over an embedded FS.
- Ring buffer (`ringbuffer.go`) is correct and bounded; snapshot copies
  under lock, no data races found (`-race` used in CI).
- CPU/network delta computations guard against counter resets and
  division by zero; parsers tolerate malformed `/proc` lines.
- Privilege separation is well designed: the web-facing service is
  unprivileged; only the isolated apt-refresh timer runs as root.
- CI/release workflows use least-privilege `permissions:` blocks and
  pinned major action versions.
