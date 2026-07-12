package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const thermalZoneGlob = "/sys/class/thermal/thermal_zone*"

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
type TemperatureCollector struct {
	zonePath     string
	zoneType     string
	vcgencmdPath string // empty if vcgencmd is not available
}

// NewTemperatureCollector auto-detects the CPU thermal zone and checks
// whether vcgencmd is available. Detection failures are not fatal: the
// collector still works, it just reports errors from Collect() until a
// thermal zone appears (e.g. useful for local development off-Pi).
func NewTemperatureCollector() *TemperatureCollector {
	c := &TemperatureCollector{}
	if zonePath, zoneType, err := findCPUThermalZone(thermalZoneGlob); err == nil {
		c.zonePath = zonePath
		c.zoneType = zoneType
	}
	if path, err := exec.LookPath("vcgencmd"); err == nil {
		c.vcgencmdPath = path
	}
	return c
}

// Collect returns the current CPU temperature and, if vcgencmd is
// available, the GPU/SoC temperature as a secondary reading.
func (c *TemperatureCollector) Collect(ctx context.Context) (Temperature, *GPUTemperature, error) {
	if c.zonePath == "" {
		return Temperature{}, nil, fmt.Errorf("no CPU thermal zone detected")
	}
	celsius, err := readThermalZoneMilliC(c.zonePath)
	if err != nil {
		return Temperature{}, nil, err
	}
	temp := Temperature{Zone: c.zoneType, Celsius: celsius}

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
