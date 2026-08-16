# How to write tests for PiMonitor

## Overview

This document describes how tests are written in this repository. It is binding for
both human contributors and AI agents: **tests are mandatory for newly written or
changed code**, not optional, and new tests must follow the conventions below. Existing
tests are the reference implementation — when in doubt, look at a neighboring `_test.go`
file before inventing a new pattern.

A pull request that adds or changes behavior without corresponding test coverage should
not be merged; see the checklist in [`.github/pull_request_template.md`](../.github/pull_request_template.md).

## Test types in this repository

1. **Unit tests** (the vast majority of the suite)

   A unit test exercises a single parser, collector, handler, or state machine in
   isolation — a `/proc`/`/sys` string in, a typed struct or error out, with no real
   filesystem or network access beyond a `t.TempDir()` fixture. Nearly all of
   `internal/collector`, `internal/config`, and `internal/alert` falls into this
   category.

2. **HTTP-layer tests**

   `internal/httpapi` tests exercise the full middleware chain and routing via
   `httptest.NewRecorder()` / `s.Handler().ServeHTTP(...)` against a fake
   `MetricsProvider` (`fakeMetrics` in `handlers_test.go`), rather than mocking at the
   handler-function level. This verifies routing, middleware (API-key gating, security
   headers), and JSON encoding together, the same way a real request would exercise them.

3. **Static-asset guard tests**

   `internal/web/xss_test.go` is a special case: it doesn't test Go logic, it scans the
   embedded `app.js` source at test time to enforce a security invariant (no
   non-empty `.innerHTML` assignment — see [`ARCHITECTURE.md`](ARCHITECTURE.md#web-dashboard-internalweb)).
   Treat this pattern — a test that great-greps a source file for a banned construct — as
   the template for any future "this must never appear in committed code" rule that a
   linter doesn't already cover.

4. **Integration / hardware tests**

   PiMonitor does not have tests that touch real `/proc`/`/sys`/`vcgencmd` on a live
   system, and does not have a build tag or CI job for running on actual Raspberry Pi
   hardware. Every collector is designed specifically so it *doesn't need one* — see
   [Why fixtures, not real `/proc`/`/sys`](#why-fixtures-not-real-procsys) below. When a
   fix is only verifiable on real Pi hardware, say so explicitly in the PR instead of
   claiming coverage that doesn't exist (see the `fix-issue` skill).

5. **Load tests**

   PiMonitor does not currently have load tests.

## Why unit test?

- **Fast feedback.** The full suite runs in seconds via `go test ./...` with no external
  services, database, or hardware required.
- **Protection against regression on hostile/unusual input.** `/proc` and `/sys` content
  varies across kernel versions, distributions, and Pi models; the fixture-based tests
  guard every parser against malformed, truncated, or unexpected-format input, not just
  the happy path.
- **Executable documentation.** A well-named test tells the reader what a parser or
  handler does for a given input without needing to read its implementation first.
- **Enables graceful degradation.** Because every collector must work from an isolated
  fixture, unavailable-on-this-platform behavior (no thermal zone, no `vcgencmd`, no
  cpufreq driver) is a first-class tested path, not an untested edge case — see
  [`ARCHITECTURE.md`](ARCHITECTURE.md) for why this matters for non-Pi/non-Linux
  development.

## Test stack

| Concern | Tooling |
| --- | --- |
| Test framework | Go standard library `testing` only (`go test ./...`) |
| Assertions | Plain `if got != want { t.Fatalf(...) }` / `t.Errorf(...)` — no assertion or matcher library |
| Mocking | No mocking framework. Fixtures (temp files via `t.TempDir()`, in-memory fixture strings) and small hand-written fakes (e.g. `fakeMetrics` in `httpapi`) instead |
| HTTP testing | `net/http/httptest` (`httptest.NewRecorder`, `httptest.NewRequest`) |
| Race/coverage | `go test ./... -race -cover` (CI); `-race` is not run by `make test` locally, run it explicitly when touching concurrent code (`collector`, `alert`) |
| Lint | `golangci-lint run` (`make lint`), same config as CI |

Do not introduce a third-party test framework, assertion library (e.g. `testify`), or
mocking library — the project standardizes on the standard library `testing` package and
hand-written fakes, consistent with its overall "prefer the standard library" dependency
policy (see [`CONTRIBUTING.md`](CONTRIBUTING.md)).

## Where tests live

- Tests are colocated with the code they test, as `<file>_test.go` next to `<file>.go`,
  in the same package (`internal/collector`, `internal/config`, `internal/alert`,
  `internal/httpapi`, `internal/web`) — never a separate `_test` suffixed package or a
  top-level `tests/` directory.
- One test file per source file under test (`cpu.go` → `cpu_test.go`, `disk.go` →
  `disk_test.go`, ...). A package-wide test helper file is named `testhelpers_test.go`
  (see `internal/collector/testhelpers_test.go`) rather than attached to any one metric's
  test file.
- Fixture data is almost always an inline Go string constant near the top of the test
  file (e.g. `procStatFixture1` in `cpu_test.go`, `osReleaseFixture` in
  `sysinfo_test.go`), not a checked-in binary or external file — `/proc`/`/sys` content is
  plain text, so this keeps a fixture and the test that reads it next to each other and
  diff-reviewable.

## Naming your tests

Test function names follow `Test<Subject>_<Scenario>` — the subject (function, type, or
handler under test) in Go-idiomatic `CamelCase`, an underscore, then a short scenario in
free-form words. This differs from strict Go convention (which often omits the
underscore) but is the convention this codebase already uses consistently — follow it
rather than either extreme.

**Examples from this codebase:**

```go
func TestParseProcStat(t *testing.T)
func TestParseProcStat_NoCPULines(t *testing.T)
func TestParseProcStat_MalformedField(t *testing.T)
func TestDistributionName_FallsBackToIDAndVersion(t *testing.T)
func TestHandleAlerts_GatedByAPIKey(t *testing.T)
func TestAPIKey_RequiredWhenConfigured(t *testing.T)
```

When there's exactly one obvious scenario for a subject, the bare `Test<Subject>` form
(no underscore/scenario suffix) is fine — don't force a scenario suffix that adds no
information.

## Arranging your tests

Follow Arrange, Act, Assert without labeling the sections with comments — a blank line
before the act and before the assert block is enough to separate them:

```go
func TestCPUCollector_CoreCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	if err := os.WriteFile(path, []byte(procStatFixture1), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := &CPUCollector{path: path}

	count, err := c.CoreCount()

	if err != nil {
		t.Fatalf("CoreCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("CoreCount = %d, want 2", count)
	}
}
```

## Always explain a failure in the message

Every `t.Fatalf`/`t.Errorf` call should make the failure self-explanatory without
requiring the reader to open the implementation — state what was computed and what was
expected, not just that something was wrong:

```go
if second.OverallPercent <= 0 {
	t.Fatalf("second Collect should report >0%% usage, got %v", second.OverallPercent)
}
```

Prefer the `got`/`want` phrasing (`"X = %v, want %v"`) for value comparisons, and a plain
descriptive sentence for boolean/error-presence checks (`"expected error for input with
no cpu lines"`).

## Table-driven tests for multiple scenarios

When several inputs exercise the same behavior, use a table (a slice of an anonymous or
named struct) with `t.Run` subtests instead of writing the scenario out as repeated
top-level test functions or branching inside one test body:

```go
tests := []struct {
	name  string
	mutate func(*Config)
}{
	{"zero poll interval", func(c *Config) { c.PollIntervalSeconds = 0 }},
	{"negative poll interval", func(c *Config) { c.PollIntervalSeconds = -1 }},
	// ...
}
for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		cfg := Default()
		tt.mutate(&cfg)
		if err := cfg.Validate(); err == nil {
			t.Fatalf("Validate() accepted invalid config (%s)", tt.name)
		}
	})
}
```

Each table entry's `name` should describe the scenario, not just repeat the field being
mutated, and should be specific enough that a failing subtest name alone tells you what
broke (`go test -run TestValidate/negative_poll_interval` should be a meaningful
sub-selector).

## Prefer helper functions over shared fixtures with hidden state

Factor repeated setup into a small helper function, marked `t.Helper()` so failures in
the helper report the caller's line number, rather than duplicating setup inline or
relying on `TestMain`/package-level shared state:

```go
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file %s: %v", path, err)
	}
	return path
}
```

`t.TempDir()` gives every test (and every subtest) an isolated, auto-cleaned directory,
so there is no need for a shared fixture directory or manual cleanup — use it instead of
a package-level `TestMain` for filesystem setup.

## Why fixtures, not real `/proc`/`/sys`

Every collector accepts its source path as a field (e.g. `CPUCollector{path: path}` in
the example above) rather than hard-coding `/proc/stat`, specifically so tests can point
it at a `t.TempDir()` fixture file instead of the real, platform-dependent path. This is
the concrete mechanism behind the [`CONTRIBUTING.md`](CONTRIBUTING.md) rule that "every
metric parser under `internal/collector` should be unit-testable against fixture
strings, independent of real `/proc`/`/sys` access": it's what makes the whole suite
pass identically on Linux, Windows, and macOS dev machines, and what makes
malformed/truncated-input tests possible at all (you cannot easily truncate the real
`/proc/stat` to test error handling, but you can trivially write a truncated fixture
string). When adding a new collector, follow this pattern from the start rather than
retrofitting testability later.

## Testing HTTP handlers

Build a `Server` via `New(...)` against a fake `MetricsProvider` and drive it through
`s.Handler().ServeHTTP(rec, req)` with `httptest`, rather than calling handler methods
directly — this exercises the real routing and middleware chain (API-key gating,
security headers) exactly as a live request would:

```go
func TestHandleMetrics(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got collector.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.CPU.OverallPercent != 12.5 {
		t.Fatalf("CPU.OverallPercent = %v, want 12.5", got.CPU.OverallPercent)
	}
}
```

Add a dedicated test whenever behavior depends on an HTTP concern — status code,
header, or auth gating — not just the JSON body (see `TestHandleAlerts_GatedByAPIKey`,
`TestHealthz_NotGatedByAPIKey`).

## Testing concurrent/stateful code

`internal/collector` (ring buffers, history persistence) and `internal/alert` (the
debounce state machine, the notifier's queue/worker) hold mutable state across calls.
When adding a test for this kind of code:

- Drive time explicitly by passing an increasing `time.Time` into the function under
  test (see `feed` in `alert_test.go`, which advances one second per sample) rather than
  sleeping in the test — this keeps debounce-window tests fast and deterministic.
  `alert.Notifier`'s rate limiter follows the same principle: it keys off the event's own
  timestamp, not wall-clock time, specifically so it can be tested deterministically.
- Run `go test ./... -race` locally when changing anything under `collector` or `alert`
  before opening a PR — CI runs with `-race` but catching a data race locally is faster
  than waiting on CI.

## Code coverage

Coverage is collected in CI via `go test ./... -race -cover`. Run the same command
locally before opening a PR that adds new logic, and make sure the new code path is
actually exercised — a `-cover` run reporting an untouched new function is a signal the
PR is missing a test, not something to ignore.

The separate `sonarqube` CI job (see [`ARCHITECTURE.md`](ARCHITECTURE.md)) re-runs the
suite with `go test ./... -coverprofile=coverage.out` and uploads that profile to
SonarCloud alongside the source analysis, configured by `sonar-project.properties` at the
repo root. There is no additional local tooling for this — it only runs in CI.

## Checklist for new tests

- [ ] Test file colocated with the code under test, `<file>_test.go` next to `<file>.go`,
      same package.
- [ ] Test function named `Test<Subject>_<Scenario>` (or bare `Test<Subject>` when there
      is only one obvious scenario).
- [ ] Arrange / Act / Assert, separated by blank lines, no section comments.
- [ ] Every failure message states what was computed and what was expected
      (`got`/`want` phrasing for values).
- [ ] Multiple related scenarios use a table + `t.Run`, not repeated top-level functions
      or in-test branching.
- [ ] Shared setup factored into a `t.Helper()` function, not duplicated inline.
- [ ] New `internal/collector` parsers accept their source path/input as a field or
      parameter so tests can substitute a `t.TempDir()` fixture instead of a real
      `/proc`/`/sys` path.
- [ ] No third-party test/assertion/mocking library introduced.
- [ ] `go test ./... -race -cover` and `golangci-lint run` pass locally.
