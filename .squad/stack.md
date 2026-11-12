# Stack: Go

The toolchain and the exact commands of this repository. Every squad member, skill and instruction file
refers to the entries below by their *italic name*. Seeded from the Squad-Spec-Repository-Template `go`
profile and owned by this repository: keep it true when the build changes.

## Toolchain

- Go `1.27.4` (module `github.com/larslaskowski/pimonitor`); the `go` directive tracks a supported Go release
  (`docs/CONTRIBUTING.md`, *Machine setup*).
- `gofmt` as the formatter, `go vet` and **golangci-lint** `v2.15.0` (configured in `.golangci.yml`) as the
  analyzers, `govulncheck` (a `tool` dependency in `go.mod`) for known vulnerabilities.
- The SessionStart hook `.claude/hooks/session-start.sh` runs `go mod download` in remote sessions.

## Layout

- Production code: `cmd/pimonitor` (the binary), `internal/collector`, `internal/alert`, `internal/httpapi`,
  `internal/web` (with the embedded dashboard assets under `internal/web/assets/`: vanilla HTML/CSS/JS, no build
  step and no JS test framework) and `internal/config`.
- Packaging: `packaging/` (`install.sh`, `download-latest.sh`, the systemd units, `pimonitor.example.yaml`); releases
  are built by GoReleaser (`.goreleaser.yaml`, `.github/workflows/release.yml`).
- `Makefile`: `make build` (native, `bin/pimonitor`), `make build-arm64`, `make build-arm` (`GOARM=6`), `make run`
  (against `packaging/pimonitor.example.yaml`), `make test`, `make lint`.
- Tests: colocated `_test.go` files next to the code they cover, one per source file under test
  (`foo.go` → `foo_test.go`).

## Commands

| Name | Command |
| ---- | ------- |
| *Restore* | `go mod download` |
| *Format* (in the squad: by the member that changed the files, `.squad/team.md`) | `gofmt -w .` |
| *Format check* | `test -z "$(gofmt -l .)"` |
| *Build* | `go build ./...` |
| *Test* | `go test ./... -race` |
| *Single test* | `go test ./<package> -run '^TestName$'` |
| *Test (project)* | `go test ./<package>/... -race` (the package of the changed code; a `patch` that changes only test code, `.squad/routing.md` *Gates of a patch* — it writes no coverage report, so a `patch` that changes production code runs *Test with coverage* instead) |
| *Test with coverage* | `go test ./... -race -coverprofile=coverage.out` (overwrites `coverage.out`) |
| *Coverage gate* | `python3 .squad/tools/coverage-check.py` |
| *Analyzer gate* | `python3 .squad/tools/analyzer-check.py` |
| *Sonar gate* | `python3 .squad/tools/sonar-check.py pr <number>` (step 11, on the draft pull request once its CI has finished; see *Sonar gate* below) |
| *Image build check* | none (the repository builds no image); a change to `go.mod`, `go.sum` or an embedded file still runs the reproduction under *Image build check* below, which checks the same module pinning for the GoReleaser build |

## Analyzer gate

`analyzer-check.py` runs `go vet ./...` (must pass for the whole module) and
`golangci-lint run --new-from-merge-base=origin/main --whole-files ./...`, which reports every issue
anywhere in a file changed since the merge base with `origin/main`. Fixable style findings are the Code Officer's; findings that need a
code change go to the Dev or Tester. `.golangci.yml` seeds `gocognit` with `min-complexity: 15` as a stand-in
for SonarQube `go:S3776` (cognitive complexity above 15), so those findings surface here instead of after the PR
is open. `gocognit` also counts the bare `if err != nil` check, which SonarQube ignores: where that alone puts an
existing function over the limit, add a named exclusion in `.golangci.yml` and note it in `.squad/project.md`.
Other SonarQube Cloud findings (and hotspots, duplication) have no local Go equivalent: the *Sonar gate* below reports their issues and hotspots and the quality gate, duplication
included, on the draft pull request.

Changed shell scripts (`*.sh`), GitHub workflows (`.github/workflows/*.yml`) and Dockerfiles are checked with
`shellcheck`, `actionlint` and `hadolint`; the SessionStart hook installs the three as pinned, checksum-verified
binaries (`.claude/hooks/install-linters.sh`), and the gate reports NOT RUN for one that is missing (a refused
download, for example). The gate itself checks two SonarQube Cloud shell rules shellcheck does not report:
`shelldre:S7679` (a positional parameter `$1`…`$9` used as a word of its own inside a function — assign it to a
local variable first) and `shelldre:S7688` (`[ … ]` instead of `[[ … ]]`). Other `shelldre:*` findings are first
reported by the *Sonar gate* below.

*Sonar gate* reads the CI code analysis of the draft pull request (squad step 11, *Draft until green* in
`.squad/orchestration.md`): `sonar-check.py pr <number>` checks that the analysis belongs to the pushed head and
fails on a failed quality gate, on every open issue and on every security hotspot to review. The work branch is
not scanned before the pull request: SonarQube Cloud does not let every organization read the data of non-main
branches through its API. A private project needs a SonarQube credential: a network secret of the cloud
environment for `sonarcloud.io` and `*.sonarcloud.io` (the agent proxy attaches the token to those requests, so no
session or member sees it), or else `SONAR_TOKEN` in the environment, which every process of the session can
read. A finding of a rule no local analyzer reports is noted in this section, in the same change, instead of a
squad lesson.

## Image build check

PiMonitor builds no container image (no Dockerfile); the release binaries are built by GoReleaser from the
module as committed, so the reproduction below still applies to a change of `go.mod`, `go.sum` or an embedded file
(`internal/web/assets/`, `//go:embed`).

The Dockerfile copies `go.mod` and `go.sum` first, runs `go mod download`, then copies the sources and builds
without network. Where Docker runs, build the image. Where it does not (the sandbox), reproduce that order:
`go mod download`, then `GOFLAGS=-mod=readonly GOPROXY=off go build ./...` — a module the build needs that
`go.sum` does not pin, or an embedded file (`//go:embed`) the Dockerfile's `COPY` lines leave out, fails here
as it would in the image. Every change to `go.mod`, `go.sum`, an embedded file or the Dockerfile runs it
before the review; a build from the full working tree with network does not replace it.

## Coverage exclusions

Two places compute coverage and must agree (`.squad/routing.md`, *Code a unit test cannot cover*): the
*Coverage gate* reads `COVERAGE_PATHSPECS` and `COVERAGE_EXCLUDES` in `.squad/tools/squad_settings.py`, and
SonarQube Cloud reads `sonar.coverage.exclusions` in `sonar-project.properties`. A file no unit test can cover
(a generated file, `main.go` startup glue) is added to both with a decision record — never to
`sonar.exclusions`, which would drop it from the analysis too.

## Writing code

- `gofmt` formatting; package names short and lower-case; exported identifiers documented with a comment
  that starts with the identifier's name.
- Errors are returned and wrapped with context (`fmt.Errorf("…: %w", err)`), never ignored; no `panic` in
  library code.
- `context.Context` is the first parameter of anything that does I/O or can block, and is honored.
- Prefer the standard library; `gopkg.in/yaml.v3` is the only accepted runtime dependency (`AGENTS.md`, *Code
  style*).
- A collector takes its source path (`/proc`, `/sys`, a command's output) as a field or parameter so tests can point
  it at a fixture; a missing source yields an empty/zero value and a logged warning, never a crash.
- `exec.Command` with fixed argument lists only.
- Cross-compilation: CI builds `./cmd/pimonitor` for `linux/arm64`, `linux/arm` (`GOARM=6`), `darwin/arm64` and
  `windows/amd64` with `CGO_ENABLED=0`; platform-specific code is split into `_linux.go`/`_other.go` files
  (`kernel_*.go`, `statfs_*.go`) so every target builds.

## Logger arguments

CodeQL's *Clear-text logging of sensitive information* (`go/clear-text-logging`) judges a value by its
**name**: a variable, field or call whose name contains `secret` (not `issecret`), `trusted` (not `untrusted`,
`istrusted`), `puid`, `username`, `userid`, `password`, `passwd`, `passcode`, `passphrase` (unless followed by
`question`) or `authkey`, `authenticationkey`, `authorizationkey`, `apikey`, `secretkey` counts as sensitive,
unless the name also contains `test`, `redact`, `censor`, `obfuscate`, `hash`, `md5`, `mask`, `sha`, `crypt` or
`code` (`go/ql/lib/semmle/go/security/SensitiveActions.qll` in `github/codeql`, `HeuristicNames`). Such a name
passed to a log call fails the PR in CI, also when it holds only a label, and no local gate reports it. The
remedy is a neutral name for the value whose text a test pins, never a suppression.

## Writing tests

**Scripts only CI can run.** A script that needs systemd, root or a service the session lacks (an
end-to-end install check, for example) has a local smoke substitute, which the Tester runs before handing back:
`bash -n` and `shellcheck` on the script, and a mode of the script (or a test) that runs the documented lines
against a scratch directory with stub commands, without systemd. The plan lists the rows that can only run in
CI (`.squad/routing.md`, *Operator environments and documented procedures*).

See `docs/UNIT_TESTS.md`: the standard `testing` package, table-driven tests with `t.Run`, `t.Helper()` in
helpers, `t.TempDir()` for files, failure messages that state got and want.

Pitfalls of leak and error-text tests: never put a leak sentinel into a subtest name or any other name that
becomes a path (a `t.TempDir()` directory) when the error under test prints that path — strip the path from
the error text before the leak check. Where the plan fixes the error format, compare the exact text instead
of forbidding substrings, so a forbidden substring never overlaps required text (`yaml:` against the
required prefix `config: test.yaml:`). A claim of bounded memory gets a heap-bound test (`runtime.MemStats`
or `testing.AllocsPerRun`) with the smallest input that still detects the failure.

## Skeleton

New functions and methods with their full signature and doc comment, bodies
return a zero value or an error (`errors.New("not implemented")`) where the signature allows it, and
`panic("not implemented")` only where it does not (a `panic` aborts the whole test binary), so the module
builds and the tests compile and fail.
A constructor (`New…`) only stores its arguments and returns the value without an error or a panic, so a test
harness that builds the type does not fail before its assertion.
The `Skeleton` body above is the default. A zero-value stub is allowed only where it would break a test that
exists and passes today: the new tests must still fail on the stub, and the Dev reports it as a deviation.

## Dependencies

`go get <module>@<version>`, then `go mod tidy`; commit `go.mod` and `go.sum` together. A new dependency is
a `security`-tier change.

## Concurrency

Builds and test runs share the Go build cache safely, but run one squad member that builds or tests at a
time so results are attributable.

## Known pitfalls

- `make test` runs `go test ./...` without `-race`; the squad's *Test* and *Test with coverage* add it, and CI runs
  with it.
- `golangci-lint` must be on the PATH; the version in CI (`.github/workflows/ci.yml`) is the reference.
  It refuses to run ("the Go language version used to build golangci-lint is lower than the targeted Go
  version") when it was built with an older Go than `go.mod` targets — install a release built with a
  matching Go version (`go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>` with
  `GOTOOLCHAIN` set to the toolchain `go.mod` targets). The session-start hook does this for remote
  sessions, using the version pinned in `.github/workflows/ci.yml`.
