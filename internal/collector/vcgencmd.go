package collector

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// vcgencmdTimeout bounds a single vcgencmd invocation so a hung firmware
// call cannot stall a fast tick indefinitely.
const vcgencmdTimeout = 5 * time.Second

// errVcgencmdUnavailable is returned by run when vcgencmd has not (yet) been
// found on the system. It is not a failure: TemperatureCollector and
// ThrottledCollector both treat it as "no reading available this tick"
// rather than a collection error.
var errVcgencmdUnavailable = errors.New("vcgencmd not available")

// vcgencmdRunner resolves the vcgencmd binary once and runs subcommands
// against it. TemperatureCollector and ThrottledCollector share a single
// instance so the lazy detection/re-detection logic - previously
// duplicated verbatim in both collectors - lives in one place, and the
// exec.LookPath("vcgencmd") call itself only ever runs once instead of
// once per collector.
type vcgencmdRunner struct {
	mu   sync.Mutex
	now  func() time.Time
	path string // empty if vcgencmd is not available
	// detected reports whether a lookup has ever succeeded. Re-detection is
	// throttled (detectRetryInterval) while it has not, so a missing
	// vcgencmd doesn't cost an exec.LookPath call on every tick.
	detected   bool
	lastDetect time.Time
}

// newVcgencmdRunner resolves the vcgencmd binary path, if available. A
// missing vcgencmd is not fatal: run re-attempts detection at most once per
// detectRetryInterval, mirroring TemperatureCollector's thermal-zone
// re-detection, so vcgencmd becoming available after startup is picked up
// without restarting the collectors.
func newVcgencmdRunner(now func() time.Time) *vcgencmdRunner {
	if now == nil {
		now = time.Now
	}
	r := &vcgencmdRunner{now: now}
	r.mu.Lock()
	r.redetectLocked()
	r.mu.Unlock()
	return r
}

// redetectLocked (re)resolves the vcgencmd path if it is currently unknown,
// throttled to at most once per detectRetryInterval. Caller must hold r.mu.
func (r *vcgencmdRunner) redetectLocked() {
	if r.detected {
		return
	}
	now := r.now()
	if !r.lastDetect.IsZero() && now.Sub(r.lastDetect) < detectRetryInterval {
		return
	}
	r.lastDetect = now
	if path, err := exec.LookPath("vcgencmd"); err == nil {
		r.path = path
		r.detected = true
	}
}

// run executes `vcgencmd <subcommand>` with a bounded timeout and returns
// its trimmed stdout. It returns errVcgencmdUnavailable, without attempting
// an exec, if vcgencmd has not been detected.
func (r *vcgencmdRunner) run(ctx context.Context, subcommand string) (string, error) {
	r.mu.Lock()
	r.redetectLocked()
	path := r.path
	r.mu.Unlock()

	if path == "" {
		return "", errVcgencmdUnavailable
	}

	ctx, cancel := context.WithTimeout(ctx, vcgencmdTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, subcommand).Output()
	if err != nil {
		return "", fmt.Errorf("run vcgencmd %s: %w", subcommand, err)
	}
	return strings.TrimSpace(string(out)), nil
}
