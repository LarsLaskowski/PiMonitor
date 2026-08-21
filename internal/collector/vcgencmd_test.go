package collector

import (
	"context"
	"errors"
	"runtime"
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
