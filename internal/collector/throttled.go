package collector

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Bit positions in the bitmask reported by `vcgencmd get_throttled`. The
// low bits describe the current state; the high bits (16+) latch whether the
// condition has occurred at any point since boot.
//
// See https://www.raspberrypi.com/documentation/computers/os.html#get_throttled
const (
	throttledBitUnderVoltageNow      = 0
	throttledBitFrequencyCappedNow   = 1
	throttledBitThrottledNow         = 2
	throttledBitSoftTempLimitNow     = 3
	throttledBitUnderVoltageSince    = 16
	throttledBitFrequencyCappedSince = 17
	throttledBitThrottledSince       = 18
	throttledBitSoftTempLimitSince   = 19
)

// ThrottledCollector reports the Raspberry Pi under-voltage / throttling
// state decoded from `vcgencmd get_throttled`. It is a Pi-only signal: on
// systems without vcgencmd (e.g. development machines) it degrades to no
// reading rather than failing.
//
// The vcgencmd path is resolved lazily and re-resolved (throttled to at most
// once per detectRetryInterval) while it is still missing, mirroring
// TemperatureCollector, so a firmware tool that appears after startup is
// picked up without restarting the collector.
type ThrottledCollector struct {
	mu                 sync.Mutex
	now                func() time.Time
	vcgencmdPath       string // empty if vcgencmd is not available
	vcgencmdDetected   bool   // whether a vcgencmd lookup has ever succeeded
	lastVcgencmdDetect time.Time
}

// NewThrottledCollector checks whether vcgencmd is available. A missing
// vcgencmd is not fatal: Collect simply returns no reading until vcgencmd
// appears (re-detection is retried at most once per detectRetryInterval).
func NewThrottledCollector() *ThrottledCollector {
	c := &ThrottledCollector{now: time.Now}
	c.redetectVcgencmdLocked()
	return c
}

// redetectVcgencmdLocked retries exec.LookPath("vcgencmd") if it has never
// been found, throttled to at most once per detectRetryInterval. Caller must
// hold c.mu (the constructor is single-threaded, so it also qualifies).
func (c *ThrottledCollector) redetectVcgencmdLocked() {
	if c.vcgencmdDetected {
		return
	}
	now := c.now()
	if !c.lastVcgencmdDetect.IsZero() && now.Sub(c.lastVcgencmdDetect) < detectRetryInterval {
		return
	}
	c.lastVcgencmdDetect = now
	if path, err := exec.LookPath("vcgencmd"); err == nil {
		c.vcgencmdPath = path
		c.vcgencmdDetected = true
	}
}

// Collect runs `vcgencmd get_throttled` and decodes the bitmask. It returns
// (nil, nil) when vcgencmd is not available, so the throttled object is
// simply omitted from the snapshot off-Pi.
func (c *ThrottledCollector) Collect(ctx context.Context) (*Throttled, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collectors built as struct literals in tests may not set the clock.
	if c.now == nil {
		c.now = time.Now
	}

	c.redetectVcgencmdLocked()
	if c.vcgencmdPath == "" {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, c.vcgencmdPath, "get_throttled").Output()
	if err != nil {
		return nil, fmt.Errorf("run vcgencmd get_throttled: %w", err)
	}
	t, err := parseThrottled(string(out))
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// parseThrottled decodes output of the form "throttled=0x50005" into the
// individual flags.
func parseThrottled(output string) (Throttled, error) {
	output = strings.TrimSpace(output)
	const prefix = "throttled="
	if !strings.HasPrefix(output, prefix) {
		return Throttled{}, fmt.Errorf("unexpected vcgencmd get_throttled output: %q", output)
	}
	raw := strings.TrimPrefix(output, prefix)

	// Base 0 lets strconv infer the base from the "0x" prefix vcgencmd emits.
	bits, err := strconv.ParseUint(raw, 0, 64)
	if err != nil {
		return Throttled{}, fmt.Errorf("parse throttled bitmask %q: %w", raw, err)
	}

	isSet := func(bit uint) bool { return bits&(1<<bit) != 0 }
	return Throttled{
		UnderVoltageNow:          isSet(throttledBitUnderVoltageNow),
		FrequencyCappedNow:       isSet(throttledBitFrequencyCappedNow),
		ThrottledNow:             isSet(throttledBitThrottledNow),
		SoftTempLimitNow:         isSet(throttledBitSoftTempLimitNow),
		UnderVoltageSinceBoot:    isSet(throttledBitUnderVoltageSince),
		FrequencyCappedSinceBoot: isSet(throttledBitFrequencyCappedSince),
		ThrottledSinceBoot:       isSet(throttledBitThrottledSince),
		SoftTempLimitSinceBoot:   isSet(throttledBitSoftTempLimitSince),
		Raw:                      raw,
	}, nil
}
