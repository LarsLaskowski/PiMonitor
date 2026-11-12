# Contributing

<!-- project:begin getting-started -->
## Getting started

### Machine setup

To begin you'll need Git and a Go toolchain. Development and testing do not require a
Raspberry Pi: hardware-specific metrics (CPU temperature, Pi model, `vcgencmd`-sourced
readings) simply report as empty/zero on other platforms instead of failing.

The `PiMonitor` repository uses Git as its source control system. If you haven't already
installed it, you can download it [here](https://git-scm.com/downloads) or, if you prefer
a GUI-based approach, try [GitHub Desktop](https://desktop.github.com/).

Once Git is installed, you'll also need the Go version this project targets (see
[`go.mod`](../go.mod) and [`.squad/stack.md`](../.squad/stack.md), *Toolchain*). Instructions
and downloads for your preferred OS can be found [here](https://go.dev/dl/).

> [!NOTE]
> The `go` directive in `go.mod` tracks a currently-supported Go release (Go supports the
> two most recent major releases). Since PiMonitor ships as a single statically-linked
> binary, the toolchain version is a dependency-security property, not just a build detail
> — it's raised whenever the declared version falls out of that support window, independent
> of any new language features being adopted.

For linting, install `golangci-lint` matching the version CI uses (see
[`.github/workflows/ci.yml`](../.github/workflows/ci.yml); the current version is also named in
`.squad/stack.md`, *Toolchain*):

```sh
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>
```

`golangci-lint` refuses to lint a module whose `go` directive is newer than the Go version
it was built with. `go install …@version` builds with your local default toolchain rather
than the one `go.mod` asks for, so if that default is older than the `go` line in
[`go.mod`](../go.mod), prefix the command with `GOTOOLCHAIN=go<version>` (for example
`GOTOOLCHAIN=go1.27.4 go install …`). `govulncheck` needs no install: it is a `tool`
dependency in `go.mod` and runs as `go tool govulncheck ./...`.

> [!IMPORTANT]
> The above steps are a one-time setup for your machine and do not need to be repeated
> after the initial configuration.

### Cloning the repository

```shell
git clone https://github.com/larslaskowski/pimonitor.git
```

Cloning via SSH:

```shell
git clone git@github.com:larslaskowski/pimonitor.git
```

### Building and running

```sh
go mod download      # Restore
make build           # native build, for local development -> bin/pimonitor
make build-arm64     # cross-compile for 64-bit Raspberry Pi OS (Pi 3/4/5)
make build-arm       # cross-compile for 32-bit / Pi Zero/1 (GOARM=6)
```

`make run` starts the server locally against `packaging/pimonitor.example.yaml` — useful
for frontend/API development on a non-Pi machine, since hardware-specific metrics degrade
gracefully instead of failing.

### Running tests

```sh
go test ./... -race  # Test (make test runs go test ./... without -race)
make lint            # golangci-lint run
```

For detailed rules on how unit tests are structured and named, see [`UNIT_TESTS.md`](UNIT_TESTS.md).
**Unit tests are mandatory for newly written code** — see the checklist there before opening a pull
request.
<!-- project:end getting-started -->

## Submitting a pull request

Nothing is ever committed or pushed directly to `main` — every change goes through a separate branch and
a pull request.

Pull requests are merged with **Squash and merge**: the PR title becomes the single commit subject on
`main` and the description its body, so the commits on the branch are working history and need not be
curated. Keep the branch up to date by merging the current `main` into it (no force-push needed); do not
use the plain *Create a merge commit* or *Rebase and merge* buttons (see the process decisions in
[`decisions/process.md`](decisions/process.md)).

For PR naming use the following convention: `[area] Description` (no period at the end).

- For the area, use one of the areas listed below, capitalized. Issue titles use the same `[area]` prefix.
- For the description, do not reference an issue number in there. A clear, short summary of what
  the change entails is enough; there is room to elaborate in the description.

<!-- project:begin areas -->
Areas: `Collector`, `Alerts`, `API`, `Dashboard`, `Config`, `Packaging`, `Tests`, `CI`, `Docs`.
<!-- project:end areas -->

When a PR is related to an issue, use the `Closes #issuenumber` syntax so the issue links to the
PR automatically and closes when the PR is merged.

The PR's label decides its section in the generated release notes (`.github/release.yml`): `breaking-change`,
`enhancement` or `feature`, `security`, `bug`; any other PR is listed under *Other changes*, and `ignore-for-release` leaves it out.

Follow the PR template in [`.github/pull_request_template.md`](../.github/pull_request_template.md).

## Contributions from outside

Issues and pull requests from forks are welcome. Changes in this repository are spec-driven: what a feature does
is agreed as acceptance criteria before it is built, and the maintainers own those criteria.

- **Bug fixes and documentation** may come as a pull request directly. Describe how to reproduce the bug, or
  link an issue that does.
- **Features and changes of behavior start with an issue** (the feature request template). A maintainer answers
  with the proposed scenarios and acceptance criteria; open a pull request once you agree on them, and link the
  issue with `Closes #…`. A feature pull request without an agreed issue is put on hold until there is one.
- **Out of scope for an outside pull request:** the AI squad and agent instructions (`.squad/` except
  `stack.md` and `project.md`, `.claude/`, `AGENTS.md`, `CLAUDE.md`, `.github/copilot-instructions.md`), the
  text outside the `<!-- project:… -->` blocks of the template's marked files, and the workflows under
  `.github/workflows/`. The first two come from a shared template and are changed there, the workflows are the
  repository's own; the *Fork scope* check fails on the first two. Open an issue instead.
- **Checks on a fork:** pull requests from forks run without repository secrets, so the analysis that needs one
  (e.g. SonarQube Cloud) does not run there; a maintainer runs it before merging. Run the quality gates below
  locally so the pull request arrives clean. The CI run of a first-time contributor's fork waits for a maintainer's
  approval before it starts.
- Maintainers may push to your branch ("Allow edits by maintainers") or carry the change on in a branch of their
  own; you stay credited as co-author of the squashed commit.

## Quality gates

Code-style rules are documented in [`AGENTS.md`](../AGENTS.md) and in
[`.squad/stack.md`](../.squad/stack.md), and are binding for all contributions. Before opening a pull
request, run the commands from `stack.md`: *Format*, *Build*, the *Analyzer gate* (no analyzer diagnostic
of any severity in a changed file) and the *Coverage gate* (line coverage on new or changed production code
and overall, thresholds in [`UNIT_TESTS.md`](UNIT_TESTS.md#code-coverage)). A pull request is
expected to arrive clean (see the process decisions in [`decisions/process.md`](decisions/process.md)).

<!-- project:begin releases -->
## Versioning and releases

PiMonitor is versioned `v<major>.<minor>.<patch>`. A release is cut by pushing a tag of that form on
`main` (it needs the maintainer's approval); `.github/workflows/release.yml` then runs GoReleaser
(`.goreleaser.yaml`), which builds the `linux/arm` (`GOARM=6`) and `linux/arm64` binaries, packs each with
the files under `packaging/`, `README.md` and `LICENSE.md` into a `tar.gz` archive, writes `checksums.txt` and
publishes a GitHub release whose notes GitHub generates from the pull request labels
(`.github/release.yml`). A published release also triggers `.github/workflows/notify-website.yml`. Merging a
PR by itself never publishes a release.
<!-- project:end releases -->

<!-- project:begin stability -->
## Stability policy

An essential consideration in every pull request is its impact on the system. Avoid introducing
unnecessary breaking changes, performance or functional regressions, or negative impacts on usability. In
particular, preserve the guarantees listed in [`.squad/project.md`](../.squad/project.md) (*Guarantees*)
and described in [`ARCHITECTURE.md`](ARCHITECTURE.md) unless a change explicitly intends to alter one:

- Preserve the unprivileged/privileged split described in
  [`ARCHITECTURE.md`](ARCHITECTURE.md#privilege-separation-and-deployment) and
  [`SECURITY.md`](../SECURITY.md) — `pimonitor.service` must never need elevated privileges.
- Preserve graceful degradation on non-Pi/non-Linux platforms: a missing hardware source
  (e.g. no thermal zone, no `vcgencmd`) must produce an empty/zero value and a logged
  warning, never a crash.
- Keep new shell-outs (in the style of the existing `apt list --upgradable` /
  `vcgencmd measure_temp` calls) to fixed argument lists — never interpolate external
  input into a shell command.
- Prefer the Go standard library. The only accepted runtime dependency is `gopkg.in/yaml.v3`
  for config parsing; `/proc`- and `/sys`-parsing is hand-rolled rather than pulled in via a
  library such as `gopsutil`, to keep the binary size and dependency surface small on
  constrained hardware. Adding a new dependency needs a clear justification, not just convenience.
- `internal/web/assets/app.js` must never assign non-empty content to `.innerHTML`; build DOM
  nodes with `document.createElement` + `textContent` instead. This is enforced by
  `internal/web/xss_test.go` — see [`ARCHITECTURE.md`](ARCHITECTURE.md#web-dashboard-internalweb).

### REST API stability

Changes that alter the JSON shape of an existing `/api/v1/...` endpoint are breaking
changes: bump to `/api/v2/...` instead of changing `v1` in place, and update
[`API.md`](API.md). See [`ARCHITECTURE.md`](ARCHITECTURE.md) for the reasoning
and [`API.md`](API.md) for the full current contract.
<!-- project:end stability -->

## Reporting security issues

Do not report security vulnerabilities through public GitHub issues. See
[`SECURITY.md`](../SECURITY.md) for the private reporting process.

## License

<!-- project:begin license -->
By contributing to this project, you agree that your contributions will be licensed
under the same [MIT License](../LICENSE.md) that covers the project.
<!-- project:end license -->
