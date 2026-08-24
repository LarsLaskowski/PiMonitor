# Contributing

## Getting started

### Machine setup

To begin you'll need Git and a Go toolchain. Development and testing do not require a
Raspberry Pi: hardware-specific metrics (CPU temperature, Pi model, `vcgencmd`-sourced
readings) simply report as empty/zero on other platforms instead of failing.

The `PiMonitor` repository uses Git as its source control system. If you haven't already
installed it, you can download it [here](https://git-scm.com/downloads) or, if you prefer
a GUI-based approach, try [GitHub Desktop](https://desktop.github.com/).

Once Git is installed, you'll also need the Go version this project targets (currently
**Go 1.26+**, see [`go.mod`](../go.mod)). Instructions and downloads for your preferred OS
can be found [here](https://go.dev/dl/).

> [!NOTE]
> The `go` directive in `go.mod` tracks a currently-supported Go release (Go supports the
> two most recent major releases). Since PiMonitor ships as a single statically-linked
> binary, the toolchain version is a dependency-security property, not just a build detail
> — it's raised whenever the declared version falls out of that support window, independent
> of any new language features being adopted.

For linting, install `golangci-lint` matching the version CI uses (currently `v2.13.1`,
see [`.github/workflows/ci.yml`](../.github/workflows/ci.yml)):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.1
```

> [!IMPORTANT]
> The above steps are a one-time setup for your machine and do not need to be repeated
> after the initial configuration.

### Cloning the repository

Now that your machine is set up, you can clone the `PiMonitor` repository. Open a
terminal and run this command:

```sh
git clone https://github.com/larslaskowski/pimonitor.git
```

Cloning via SSH:

```sh
git clone git@github.com:larslaskowski/pimonitor.git
```

### Building

```sh
make build          # native build, for local development -> bin/pimonitor
make build-arm64     # cross-compile for 64-bit Raspberry Pi OS (Pi 3/4/5)
make build-arm       # cross-compile for 32-bit / Pi Zero/1 (GOARM=6)
```

`make run` starts the server locally against `packaging/pimonitor.example.yaml` — useful
for frontend/API development on a non-Pi machine, since hardware-specific metrics degrade
gracefully instead of failing.

### Running tests

```sh
make test    # go test ./...
make lint    # golangci-lint run
```

For detailed rules on how tests should be structured, named, and what is required for new
code, see [`TESTS.md`](TESTS.md) — tests are **mandatory** for new or changed behavior,
not optional.

### Submitting a pull request

If you'd like to contribute by fixing a bug, implementing a feature, or even correcting
typos in the documentation, you'll need to submit a pull request.

Before submitting a pull request, be sure to [rebase](https://www.atlassian.com/git/tutorials/merging-vs-rebasing)
your branch onto the current `main`. Do not use `git merge` or the *merge* button
provided by GitHub.

Commit messages: a short imperative summary line, with the *why* explained in the body
when it isn't obvious from the diff (for example, a design trade-off or a bug this avoids)
— see the existing history (`git log`) for the established style. Keep PR titles in the
same short, imperative style; GitHub appends the PR number automatically on merge, so
don't include one yourself.

When a PR is related to an issue, use the `Closes #issuenumber` syntax so the issue links
to the PR automatically and closes when the PR is merged.

Follow the PR template in [`.github/pull_request_template.md`](../.github/pull_request_template.md).
Run `make build`, `go vet ./...`, `make test`, `make lint`, and `govulncheck ./...` (install
via `go install golang.org/x/vuln/cmd/govulncheck@latest`) locally before opening the PR —
CI runs the same checks (plus a cross-compile check for `arm`/`arm64`) and will not merge on
a red build.

## Code style

- All source code, comments, commit messages, and documentation are written in
  **English**.
- Prefer the Go standard library over third-party dependencies. The only accepted runtime
  dependency is `gopkg.in/yaml.v3` for config parsing; `/proc`- and `/sys`-parsing is
  hand-rolled rather than pulled in via a library such as `gopsutil`, to keep the binary
  size and dependency surface small on constrained hardware. Adding a new dependency
  needs a clear justification, not just convenience.
- Every metric parser under `internal/collector` must be unit-testable against fixture
  strings, independent of real `/proc`/`/sys` access — see [`TESTS.md`](TESTS.md).
- `internal/web/assets/app.js` must never assign non-empty content to `.innerHTML`;
  build DOM nodes with `document.createElement` + `textContent` instead. This is enforced
  by `internal/web/xss_test.go`, not just a style preference — see
  [`ARCHITECTURE.md`](ARCHITECTURE.md#web-dashboard-internalweb) for why.
- Run `golangci-lint run` (`make lint`) before opening a pull request; CI enforces the
  same configuration.

## REST API stability

Changes that alter the JSON shape of an existing `/api/v1/...` endpoint are breaking
changes: bump to `/api/v2/...` instead of changing `v1` in place, and update
[`API.md`](API.md). See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the reasoning
and [`API.md`](API.md) for the full current contract.

## Stability policy

An essential consideration in every pull request is its impact on the system. Avoid
introducing unnecessary breaking changes, performance or functional regressions, or
negative impacts on usability:

- Preserve the unprivileged/privileged split described in
  [`ARCHITECTURE.md`](ARCHITECTURE.md#privilege-separation-and-deployment) and
  [`SECURITY.md`](../SECURITY.md) — `pimonitor.service` must never need elevated privileges.
- Preserve graceful degradation on non-Pi/non-Linux platforms: a missing hardware source
  (e.g. no thermal zone, no `vcgencmd`) must produce an empty/zero value and a logged
  warning, never a crash.
- Keep new shell-outs (in the style of the existing `apt list --upgradable` /
  `vcgencmd measure_temp` calls) to fixed argument lists — never interpolate external
  input into a shell command.

## Reporting security issues

Do not report security vulnerabilities through public GitHub issues. See
[`SECURITY.md`](../SECURITY.md) for the private reporting process.

## License

By contributing to this project, you agree that your contributions will be licensed
under the same [MIT License](../LICENSE.md) that covers the project.
