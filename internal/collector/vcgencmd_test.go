package collector

import (
	"context"
	"errors"
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
