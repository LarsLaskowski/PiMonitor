# CLAUDE.md

Guidance for Claude Code (and human contributors) working in this repository.

## Project Overview

PiMonitor is a lightweight system-monitoring dashboard for the Raspberry Pi.
A single Go binary collects system metrics (CPU usage & load average, CPU
temperature, RAM/swap, filesystem usage, network throughput, kernel version,
distribution, Pi model, available apt updates), serves a self-contained web
dashboard, and exposes a versioned REST API (`/api/v1/...`) for third-party
integration (e.g. openHAB). It runs as a systemd service on the Pi; a
separate, root-privileged systemd timer refreshes the apt package cache so
the main service itself never needs elevated privileges.

See `README.md` for the full feature list and architecture diagram, and
`docs/API.md` for the REST API contract.

## Language

All source code, comments, commit messages, and documentation in this
repository are written in **English**.

## Build & Test Commands

- `make build` — native build to `bin/pimonitor` (for local development)
- `make build-arm64` / `make build-arm` — cross-compile for Raspberry Pi
  (`GOARM=6` for Pi Zero/1, `GOARM=7` for Pi 2/3, arm64 for Pi 3/4/5 64-bit OS)
- `make run` — run locally against `packaging/pimonitor.example.yaml`
  (metrics that require real Pi/Linux hardware, e.g. CPU temperature, degrade
  gracefully to empty/zero values on other platforms instead of failing)
- `make test` — `go test ./...`
- `make lint` — `golangci-lint run` (also enforced in CI)

## Conventions

- Prefer the Go standard library over third-party dependencies. The only
  accepted runtime dependency is `gopkg.in/yaml.v3` for config parsing.
  `/proc`- and `/sys`-parsing is hand-rolled rather than using `gopsutil` —
  see the plan/PR history for the reasoning (binary size / dependency
  surface on constrained hardware).
- Every metric parser under `internal/collector` should be unit-testable
  against fixture strings, independent of real `/proc`/`/sys` access.
- REST API changes that alter the JSON shape of an existing `/api/v1/...`
  endpoint are breaking changes — bump to `/api/v2/...` instead of changing
  `v1` in place, and update `docs/API.md`.
- Commit messages: short imperative summary line, explain *why* in the body
  when not obvious from the diff.

## Related Skills

Project-specific Claude Code skills live under `.claude/skills/`:

- `create-pr` — run tests/lint locally, then open a PR following
  `.github/pull_request_template.md`.
- `review-pr` — check out a PR, run tests, and review it against this
  project's Go/security conventions.
- `fix-issue` — reproduce a reported issue (ideally as a failing test),
  implement a minimal fix, and open a PR referencing the issue.
