package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestVcgencmdRunner_Run_Unavailable(t *testing.T) {
	// detected: true with no path means a lookup already ran and found
	// nothing; run must report unavailable without attempting an exec.
	r := &vcgencmdRunner{detected: true}

	_, err := r.run(context.Background(), "measure_temp")

	if !errors.Is(err, errVcgencmdUnavailable) {
		t.Fatalf("run() error = %v, want errVcgencmdUnavailable", err)
	}
}

func TestVcgencmdRunner_Run_ThrottlesRedetection(t *testing.T) {
	// A lookup was already attempted (and failed) just now; a second run()
	// within detectRetryInterval must not attempt another lookup.
	now := time.Unix(1_700_000_000, 0)
	r := &vcgencmdRunner{
		now:        func() time.Time { return now },
		lastDetect: now,
	}

	_, err := r.run(context.Background(), "measure_temp")

	if !errors.Is(err, errVcgencmdUnavailable) {
		t.Fatalf("run() error = %v, want errVcgencmdUnavailable", err)
	}
	if r.lastDetect != now {
		t.Fatalf("lastDetect = %v, want unchanged %v (re-detection should be throttled)", r.lastDetect, now)
	}
}

func TestVcgencmdRunner_Run_RetriesAfterThrottleWindow(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	r := &vcgencmdRunner{
		now:        func() time.Time { return now },
		lastDetect: now,
	}

	// Advance past the throttle window: a fresh lookup attempt is made
	// (and updates lastDetect), even though vcgencmd still isn't found in
	// this test environment.
	now = now.Add(detectRetryInterval + time.Second)
	if _, err := r.run(context.Background(), "measure_temp"); !errors.Is(err, errVcgencmdUnavailable) {
		t.Fatalf("run() error = %v, want errVcgencmdUnavailable", err)
	}
	if r.lastDetect != now {
		t.Fatalf("lastDetect = %v, want updated to %v after the throttle window elapsed", r.lastDetect, now)
	}
}

func TestVcgencmdRunner_Run_ExecutesSubcommand(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "temp=42.8'C"`)
	r := &vcgencmdRunner{detected: true, path: path}

	out, err := r.run(context.Background(), "measure_temp")

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "temp=42.8'C" {
		t.Fatalf("run() output = %q, want %q", out, "temp=42.8'C")
	}
}

// TestVcgencmdRunner_Run_PassesSubcommandArguments pins that a
// subcommand's own arguments reach the binary as separate argv entries:
// `measure_temp pmic` only reads the Power-Management IC's sensor (issue
// #56) if the "pmic" argument is actually passed through.
func TestVcgencmdRunner_Run_PassesSubcommandArguments(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "argv: $1|$2"`)
	r := &vcgencmdRunner{detected: true, path: path}

	out, err := r.run(context.Background(), "measure_temp", "pmic")

	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if out != "argv: measure_temp|pmic" {
		t.Fatalf("run() output = %q, want %q", out, "argv: measure_temp|pmic")
	}
}

func TestVcgencmdRunner_Run_CommandFails(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", "exit 1")
	r := &vcgencmdRunner{detected: true, path: path}

	_, err := r.run(context.Background(), "measure_temp")

	if err == nil {
		t.Fatal("expected error when vcgencmd exits non-zero")
	}
	if errors.Is(err, errVcgencmdUnavailable) {
		t.Fatalf("run() error = %v, want a real execution error, not errVcgencmdUnavailable", err)
	}
}

func TestVcgencmdRunner_Run_DoesNotLeakParentEnvironment(t *testing.T) {
	t.Setenv("PIMONITOR_API_KEY", "test-secret-should-not-leak")

	dir := t.TempDir()
	envFile := filepath.Join(dir, "env.out")
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", fmt.Sprintf("env > %q", envFile))
	r := &vcgencmdRunner{detected: true, path: path}

	if _, err := r.run(context.Background(), "measure_temp"); err != nil {
		t.Fatalf("run: %v", err)
	}

	out, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read captured child environment: %v", err)
	}
	if strings.Contains(string(out), "PIMONITOR_API_KEY") {
		t.Fatalf("child environment leaked PIMONITOR_API_KEY: %q", out)
	}
	// PATH is not required: path is already the absolute location resolved
	// by exec.LookPath at detection time, so its absence here confirms the
	// environment was built explicitly rather than inherited (PWD is set by
	// the shell itself, not inherited, so its presence is expected).
	if strings.Contains(string(out), "PATH=") {
		t.Fatalf("child environment unexpectedly contains PATH: %q", out)
	}
}

func TestNewVcgencmdRunner_NilClockDefaultsToTimeNow(t *testing.T) {
	r := newVcgencmdRunner(nil)

	if r.now == nil {
		t.Fatal("newVcgencmdRunner(nil) left now nil, want it defaulted to time.Now")
	}
}

func TestNewVcgencmdRunner_DetectsBinaryOnPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake vcgencmd fixture is a Unix shell script")
	}
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "vcgencmd", "true")
	t.Setenv("PATH", dir)

	r := newVcgencmdRunner(time.Now)

	if !r.detected || r.path != path {
		t.Fatalf("newVcgencmdRunner() detected=%v path=%q, want detected=true path=%q", r.detected, r.path, path)
	}
}

// TestVcgencmdRunner_Run_CommandFailsIncludesStderr covers issue #176: a
// vcgencmd that cannot reach the firmware (older releases, /dev/vchiq)
// exits 255 and explains why on stderr. The
// returned error must carry that explanation, not just "exit status 255".
func TestVcgencmdRunner_Run_CommandFailsIncludesStderr(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "VCHI initialization failed" >&2; exit 255`)
	r := &vcgencmdRunner{detected: true, path: path}

	_, err := r.run(context.Background(), "get_throttled")

	if err == nil {
		t.Fatal("expected error when vcgencmd exits 255")
	}
	want := "run vcgencmd get_throttled: exit status 255: VCHI initialization failed"
	if err.Error() != want {
		t.Fatalf("run() error = %q, want %q", err.Error(), want)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("run() error = %v, want it to wrap *exec.ExitError", err)
	}
}

// TestVcgencmdRunner_Run_CommandFailsIncludesStdout covers issue #176 as
// observed on a Pi 5: without the video group, vcgencmd prints its
// device-open failure to stdout, not stderr, and exits 255. The error must
// still carry that message.
func TestVcgencmdRunner_Run_CommandFailsIncludesStdout(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "Can't open device file: /dev/vcio_gencmd"; exit 255`)
	r := &vcgencmdRunner{detected: true, path: path}

	_, err := r.run(context.Background(), "get_throttled")

	want := "run vcgencmd get_throttled: exit status 255: Can't open device file: /dev/vcio_gencmd"
	if err == nil || err.Error() != want {
		t.Fatalf("run() error = %v, want %q", err, want)
	}
}

// TestVcgencmdRunner_Run_CommandFailsPrefersStderr pins that stderr, when
// present, is reported instead of stdout.
func TestVcgencmdRunner_Run_CommandFailsPrefersStderr(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "partial output"; echo "real cause" >&2; exit 1`)
	r := &vcgencmdRunner{detected: true, path: path}

	_, err := r.run(context.Background(), "get_throttled")

	want := "run vcgencmd get_throttled: exit status 1: real cause"
	if err == nil || err.Error() != want {
		t.Fatalf("run() error = %v, want %q", err, want)
	}
}

func TestVcgencmdRunner_Run_CommandFailsWithoutStderr(t *testing.T) {
	dir := t.TempDir()
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", "exit 1")
	r := &vcgencmdRunner{detected: true, path: path}

	_, err := r.run(context.Background(), "get_throttled")

	want := "run vcgencmd get_throttled: exit status 1"
	if err == nil || err.Error() != want {
		t.Fatalf("run() error = %v, want %q", err, want)
	}
}

func TestSanitizeVcgencmdDetail(t *testing.T) {
	long := strings.Repeat("x", vcgencmdDetailMaxLen+50)
	// "é" is two bytes; placing it across the cut point checks that
	// truncation never splits a multi-byte rune.
	straddling := strings.Repeat("a", vcgencmdDetailMaxLen-1) + "é" + "tail"
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty", input: "", want: ""},
		{name: "whitespace only", input: " \n\t", want: ""},
		{name: "trims and joins lines", input: "  VCHI initialization failed\n  second line \n", want: "VCHI initialization failed second line"},
		{name: "truncates long output", input: long, want: strings.Repeat("x", vcgencmdDetailMaxLen) + "..."},
		{name: "truncates at rune boundary", input: straddling, want: strings.Repeat("a", vcgencmdDetailMaxLen-1) + "..."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeVcgencmdDetail([]byte(tt.input))

			if got != tt.want {
				t.Fatalf("sanitizeVcgencmdDetail(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
