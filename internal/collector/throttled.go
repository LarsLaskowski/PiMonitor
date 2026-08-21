package collector

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
// vcgencmd detection/execution is delegated to a vcgencmdRunner shared with
// TemperatureCollector, so the lazy detection/re-detection logic and the
// exec.LookPath("vcgencmd") call both live and run in one place rather than
// being duplicated per collector.
type ThrottledCollector struct {
	vcg *vcgencmdRunner // nil disables collection entirely
}

// NewThrottledCollector wraps vcg, the vcgencmd runner shared with
// TemperatureCollector. A missing vcgencmd is not fatal: Collect simply
// returns no reading until vcgencmd appears (re-detection is retried by vcg
// at most once per detectRetryInterval). Pass nil to disable collection.
func NewThrottledCollector(vcg *vcgencmdRunner) *ThrottledCollector {
	return &ThrottledCollector{vcg: vcg}
}

// Collect runs `vcgencmd get_throttled` (via the shared vcg runner) and
// decodes the bitmask. It returns (nil, nil) when vcgencmd is not
// available, so the throttled object is simply omitted from the snapshot
// off-Pi.
func (c *ThrottledCollector) Collect(ctx context.Context) (*Throttled, error) {
	if c.vcg == nil {
		return nil, nil
	}

	out, err := c.vcg.run(ctx, "get_throttled")
	if err != nil {
		if errors.Is(err, errVcgencmdUnavailable) {
			return nil, nil
		}
		return nil, err
	}
	t, err := parseThrottled(out)
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
