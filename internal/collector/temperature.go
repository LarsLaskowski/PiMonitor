package collector

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// TemperatureCollector reads CPU temperature from sysfs, with an optional
// vcgencmd-sourced GPU/SoC reading on Raspberry Pi OS.
//
// The thermal zone and vcgencmd path are resolved lazily and re-resolved
// (throttled) when they are still missing, so a sensor or driver that
// appears after the process started — e.g. a thermal module loaded late in
// boot, or a zone path that changes across a kernel/driver update — is
// picked up without restarting the collector.
type TemperatureCollector struct {
	zoneGlob string // sysfs glob for thermal zones (overridable in tests)

	mu                 sync.Mutex
	now                func() time.Time
	zonePath           string
	zoneType           string
	lastZoneDetect     time.Time
	vcgencmdPath       string // empty if vcgencmd is not available
	vcgencmdDetected   bool   // whether a vcgencmd lookup has ever succeeded
	lastVcgencmdDetect time.Time
}

// NewTemperatureCollector auto-detects the CPU thermal zone and checks
// whether vcgencmd is available. Detection failures are not fatal: the
// collector still works, it just reports errors from Collect() until a
// thermal zone appears (e.g. useful for local development off-Pi). If the
// zone (or vcgencmd) is missing at construction, Collect re-attempts
// detection at most once every detectRetryInterval, so a sensor that shows
// up later is used automatically.
func NewTemperatureCollector() *TemperatureCollector {
	c := &TemperatureCollector{zoneGlob: thermalZoneGlob, now: time.Now}
	c.redetectZoneLocked()
	c.redetectVcgencmdLocked()
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

// redetectVcgencmdLocked retries exec.LookPath("vcgencmd") if it has never
// been found, throttled the same way as zone re-detection.
func (c *TemperatureCollector) redetectVcgencmdLocked() {
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

// Collect returns the current CPU temperature and, if vcgencmd is
// available, the GPU/SoC temperature as a secondary reading.
func (c *TemperatureCollector) Collect(ctx context.Context) (Temperature, *GPUTemperature, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Collectors built as struct literals in tests may not set the clock.
	if c.now == nil {
		c.now = time.Now
	}

	c.redetectZoneLocked()
	if c.zonePath == "" {
		return Temperature{}, nil, fmt.Errorf("no CPU thermal zone detected")
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
			return Temperature{}, nil, err
		}
	}
	temp := Temperature{Zone: c.zoneType, Celsius: celsius}

	c.redetectVcgencmdLocked()
	if c.vcgencmdPath == "" {
		return temp, nil, nil
	}

	gpuTemp, err := c.readVcgencmdTemp(ctx)
	if err != nil {
		// vcgencmd is an optional extra data point; its failure should not
		// fail the whole collection.
		return temp, nil, nil
	}
	return temp, &gpuTemp, nil
}

// readVcgencmdTemp runs `vcgencmd measure_temp` and parses output of the
// form "temp=42.8'C".
func (c *TemperatureCollector) readVcgencmdTemp(ctx context.Context) (GPUTemperature, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, c.vcgencmdPath, "measure_temp").Output()
	if err != nil {
		return GPUTemperature{}, fmt.Errorf("run vcgencmd: %w", err)
	}
	return parseVcgencmdTemp(string(out))
}

func parseVcgencmdTemp(output string) (GPUTemperature, error) {
	output = strings.TrimSpace(output)
	const prefix = "temp="
	if !strings.HasPrefix(output, prefix) {
		return GPUTemperature{}, fmt.Errorf("unexpected vcgencmd output: %q", output)
	}
	rest := strings.TrimPrefix(output, prefix)
	rest = strings.TrimSuffix(rest, "'C")
	celsius, err := strconv.ParseFloat(rest, 64)
	if err != nil {
		return GPUTemperature{}, fmt.Errorf("parse vcgencmd temp %q: %w", output, err)
	}
	return GPUTemperature{Celsius: celsius}, nil
}
