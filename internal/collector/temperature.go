package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

const thermalZoneGlob = "/sys/class/thermal/thermal_zone*"

// detectRetryInterval throttles re-detection of the thermal zone and
// vcgencmd so the glob / PATH lookup is not executed on every collection
// tick on systems that genuinely have no sensor (e.g. development machines).
const detectRetryInterval = 30 * time.Second

// preferredThermalZoneTypes lists thermal-zone "type" values that identify
// the actual CPU/SoC sensor, in priority order. thermal_zone0 is not
// guaranteed to be the CPU sensor on every board, so zones are matched by
// type rather than assuming a fixed index.
var preferredThermalZoneTypes = []string{"cpu-thermal", "soc_thermal", "x86_pkg_temp"}

// findCPUThermalZone picks the sysfs thermal zone directory that reports
// the CPU/SoC temperature. If no zone matches a known type, it falls back
// to the first zone found (typically thermal_zone0), and returns an error
// only if no thermal zone exists at all.
func findCPUThermalZone(glob string) (zonePath string, zoneType string, err error) {
	matches, err := filepath.Glob(glob)
	if err != nil {
		return "", "", fmt.Errorf("glob %s: %w", glob, err)
	}
	if len(matches) == 0 {
		return "", "", fmt.Errorf("no thermal zones found matching %s", glob)
	}

	types := make(map[string]string, len(matches))
	for _, m := range matches {
		t, err := os.ReadFile(filepath.Join(m, "type"))
		if err != nil {
			continue
		}
		types[m] = strings.TrimSpace(string(t))
	}

	for _, preferred := range preferredThermalZoneTypes {
		for path, t := range types {
			if t == preferred {
				return path, t, nil
			}
		}
	}

	// Fall back to the first zone in glob order (usually thermal_zone0).
	return matches[0], types[matches[0]], nil
}

// readThermalZoneMilliC reads a thermal zone's "temp" file, which reports
// millidegrees Celsius as an integer.
func readThermalZoneMilliC(zonePath string) (float64, error) {
	data, err := os.ReadFile(filepath.Join(zonePath, "temp"))
	if err != nil {
		return 0, fmt.Errorf("read %s/temp: %w", zonePath, err)
	}
	milliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse thermal zone temp %q: %w", string(data), err)
	}
	return float64(milliC) / 1000, nil
}

// TemperatureCollector reads CPU temperature from sysfs, with optional
// vcgencmd-sourced GPU/SoC and PMIC readings on Raspberry Pi OS.
//
// The thermal zone is resolved lazily and re-resolved (throttled) when it
// is still missing, so a sensor or driver that appears after the process
// started — e.g. a thermal module loaded late in boot, or a zone path that
// changes across a kernel/driver update — is picked up without restarting
// the collector. vcgencmd detection is handled by the shared vcg runner,
// which TemperatureCollector and ThrottledCollector both use.
type TemperatureCollector struct {
	zoneGlob string // sysfs glob for thermal zones (overridable in tests)

	mu             sync.Mutex
	now            func() time.Time
	zonePath       string
	zoneType       string
	lastZoneDetect time.Time
	vcg            *vcgencmdRunner // nil disables the GPU/SoC and PMIC readings

	// pmicUnsupported latches true once vcgencmd has run successfully but
	// answered `measure_temp pmic` with something other than a temperature
	// reading (see errVcgencmdUnsupportedOutput) — in practice, the board
	// has no PMIC sensor, a Pi 3 and earlier. Unlike a failed exec or a
	// timeout, which are transient and worth retrying, that outcome can
	// never become false at runtime, so latching it avoids paying a
	// pointless vcgencmd invocation on every fast tick for the rest of the
	// process's life. A real exec/timeout failure does not set this flag
	// and keeps retrying, same as the GPU/SoC reading.
	pmicUnsupported bool
}

// NewTemperatureCollector auto-detects the CPU thermal zone. Detection
// failure is not fatal: the collector still works, it just reports errors
// from Collect() until a thermal zone appears (e.g. useful for local
// development off-Pi). If the zone is missing at construction, Collect
// re-attempts detection at most once every detectRetryInterval, so a sensor
// that shows up later is used automatically. vcg is the vcgencmd runner
// shared with ThrottledCollector; pass nil to disable the GPU/SoC and PMIC
// readings.
func NewTemperatureCollector(vcg *vcgencmdRunner) *TemperatureCollector {
	c := &TemperatureCollector{zoneGlob: thermalZoneGlob, now: time.Now, vcg: vcg}
	c.redetectZoneLocked()
	return c
}

// redetectZoneLocked (re)resolves the CPU thermal zone if we currently have
// none, throttled to at most once per detectRetryInterval. Caller must hold
// c.mu (the constructor is single-threaded, so it also qualifies).
func (c *TemperatureCollector) redetectZoneLocked() {
	if c.zonePath != "" {
		return
	}
	now := c.now()
	if !c.lastZoneDetect.IsZero() && now.Sub(c.lastZoneDetect) < detectRetryInterval {
		return
	}
	c.lastZoneDetect = now
	if zonePath, zoneType, err := findCPUThermalZone(c.zoneGlob); err == nil {
		c.zonePath = zonePath
		c.zoneType = zoneType
	}
}

// Collect returns the current CPU temperature and, if vcgencmd is
// available, the GPU/SoC and PMIC temperatures as secondary readings.
func (c *TemperatureCollector) Collect(ctx context.Context) (Temperature, *GPUTemperature, *PMICTemperature, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collectors built as struct literals in tests may not set the clock.
	if c.now == nil {
		c.now = time.Now
	}

	c.redetectZoneLocked()
	if c.zonePath == "" {
		return Temperature{}, nil, nil, fmt.Errorf("no CPU thermal zone detected")
	}
	celsius, err := readThermalZoneMilliC(c.zonePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// The cached zone path vanished (driver/kernel change). Drop it
			// and try to re-detect, subject to the same throttle.
			c.zonePath = ""
			c.redetectZoneLocked()
			if c.zonePath != "" {
				celsius, err = readThermalZoneMilliC(c.zonePath)
			}
		}
		if err != nil {
			return Temperature{}, nil, nil, err
		}
	}
	temp := Temperature{Zone: c.zoneType, Celsius: celsius}

	// The vcgencmd readings are optional extra data points: unavailability
	// or failure must fail neither the whole collection nor each other. The
	// PMIC sensor in particular exists only on the Pi 4/5, so on older
	// boards `measure_temp` succeeds while `measure_temp pmic` does not.
	var gpuTemp *GPUTemperature
	if gpuC, err := c.readVcgencmdTemp(ctx); err == nil {
		gpuTemp = &GPUTemperature{Celsius: gpuC}
	}
	var pmicTemp *PMICTemperature
	if !c.pmicUnsupported {
		pmicC, err := c.readVcgencmdTemp(ctx, "pmic")
		switch {
		case err == nil:
			pmicTemp = &PMICTemperature{Celsius: pmicC}
		case errors.Is(err, errVcgencmdUnsupportedOutput):
			// vcgencmd ran and answered, just not with a PMIC reading:
			// this board has no PMIC sensor, which cannot change at
			// runtime. Stop asking.
			c.pmicUnsupported = true
		}
	}
	return temp, gpuTemp, pmicTemp, nil
}

// readVcgencmdTemp runs `vcgencmd measure_temp [args...]` (via the shared
// vcg runner) and parses output of the form "temp=42.8'C". args carries the
// subcommand's own arguments: none for the GPU/SoC die reading, "pmic" for
// the Power-Management IC's own sensor.
func (c *TemperatureCollector) readVcgencmdTemp(ctx context.Context, args ...string) (float64, error) {
	if c.vcg == nil {
		return 0, errVcgencmdUnavailable
	}
	out, err := c.vcg.run(ctx, "measure_temp", args...)
	if err != nil {
		return 0, err
	}
	return parseVcgencmdTemp(out)
}

// errVcgencmdUnsupportedOutput indicates vcgencmd executed successfully but
// answered with something other than a "temp=NN.N'C" line — its
// "error=1 error_msg=..." response, or any other output parseVcgencmdTemp
// doesn't recognize. For a sensor-specific subcommand (measure_temp pmic)
// this means the requested sensor does not exist on this board: a
// permanent condition for the life of the process, unlike a failed exec or
// a timeout, which are transient and worth retrying. Callers that want to
// tell the two apart (see TemperatureCollector.pmicUnsupported) check for
// this with errors.Is.
var errVcgencmdUnsupportedOutput = errors.New("vcgencmd: output is not a temperature reading")

// parseVcgencmdTemp decodes vcgencmd's "temp=NN.N'C" output into degrees
// Celsius. The form is identical for every measure_temp variant, so the
// GPU/SoC and PMIC readings share this parser.
func parseVcgencmdTemp(output string) (float64, error) {
	output = strings.TrimSpace(output)
	const prefix = "temp="
	if !strings.HasPrefix(output, prefix) {
		return 0, fmt.Errorf("%w: unexpected vcgencmd output: %q", errVcgencmdUnsupportedOutput, output)
	}
	rest := strings.TrimPrefix(output, prefix)
	rest = strings.TrimSuffix(rest, "'C")
	celsius, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return 0, fmt.Errorf("%w: parse vcgencmd temp %q: %w", errVcgencmdUnsupportedOutput, output, err)
	}
	return celsius, nil
}
