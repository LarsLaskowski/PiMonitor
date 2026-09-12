package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const hwmonGlob = "/sys/class/hwmon/hwmon*"

// tempInputPattern matches a hwmon temperature channel's input file, e.g.
// "temp1_input"; the capture group is the channel index used to find the
// sibling "temp<N>_label" file.
var tempInputPattern = regexp.MustCompile(`^temp(\d+)_input$`)

// HwmonCollector enumerates every temperature sensor exposed by the
// kernel's hwmon subsystem: the SoC sensor itself, plus, depending on the
// board and attached hardware, a PoE-HAT fan controller, NVMe/SSD drives,
// or user-attached I2C/1-Wire sensors. It is additive breadth alongside
// TemperatureCollector's dedicated CPU/SoC thermal-zone reading, not a
// replacement for it.
type HwmonCollector struct {
	glob string // sysfs glob for hwmon chip directories (overridable in tests)
}

// NewHwmonCollector creates an HwmonCollector reading from
// /sys/class/hwmon.
func NewHwmonCollector() *HwmonCollector {
	return &HwmonCollector{glob: hwmonGlob}
}

// Collect returns every hwmon temperature channel found, sorted by chip
// directory then channel index. A host with no hwmon directory at all
// (off-Pi, or no hwmon driver loaded) is not an error - it degrades to an
// empty list, like WirelessCollector does for hardware it can't find.
func (c *HwmonCollector) Collect() ([]TemperatureSensor, error) {
	return readHwmonSensors(c.glob)
}

func readHwmonSensors(glob string) ([]TemperatureSensor, error) {
	chipDirs, err := filepath.Glob(glob)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", glob, err)
	}
	sort.Strings(chipDirs)

	var sensors []TemperatureSensor
	for _, chipDir := range chipDirs {
		chipName := readHwmonChipName(chipDir)
		for _, idx := range hwmonTempChannels(chipDir) {
			milliC, err := readHwmonMilliC(filepath.Join(chipDir, fmt.Sprintf("temp%d_input", idx)))
			if err != nil {
				// A channel that vanished or failed to read between the
				// directory listing and this read is skipped rather than
				// failing the whole chip.
				continue
			}
			sensors = append(sensors, TemperatureSensor{
				Chip:    chipName,
				Label:   hwmonLabel(chipDir, chipName, idx),
				Hwmon:   filepath.Base(chipDir),
				Celsius: float64(milliC) / 1000,
			})
		}
	}
	return sensors, nil
}

// hwmonTempChannels lists the temperature channel indices (the <N> in
// "temp<N>_input") present in a hwmon chip directory, sorted ascending. An
// unreadable directory yields no channels rather than an error, consistent
// with readHwmonSensors treating each chip independently.
func hwmonTempChannels(chipDir string) []int {
	entries, err := os.ReadDir(chipDir)
	if err != nil {
		return nil
	}
	var channels []int
	for _, e := range entries {
		m := tempInputPattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		idx, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		channels = append(channels, idx)
	}
	sort.Ints(channels)
	return channels
}

// readHwmonChipName reads a hwmon chip's "name" file (e.g. "cpu_thermal",
// "nvme"). A missing or unreadable name file leaves it empty rather than
// failing collection - the channel is still reported, just without a chip
// name in its fallback label (see hwmonLabel).
func readHwmonChipName(chipDir string) string {
	data, err := os.ReadFile(filepath.Join(chipDir, "name"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// hwmonLabel returns a channel's human-readable label: the sibling
// "temp<N>_label" file's content when present, otherwise the chip name plus
// channel index (or just "temp<N>" if even the chip name is unavailable).
func hwmonLabel(chipDir, chipName string, idx int) string {
	data, err := os.ReadFile(filepath.Join(chipDir, fmt.Sprintf("temp%d_label", idx)))
	if err == nil {
		if label := strings.TrimSpace(string(data)); label != "" {
			return label
		}
	}
	if chipName == "" {
		return fmt.Sprintf("temp%d", idx)
	}
	return fmt.Sprintf("%s temp%d", chipName, idx)
}

// readHwmonMilliC reads a hwmon "temp<N>_input" file, which reports
// millidegrees Celsius as an integer (the same convention as a thermal
// zone's "temp" file, see readThermalZoneMilliC in temperature.go).
func readHwmonMilliC(path string) (int64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", path, err)
	}
	milliC, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse hwmon temp %q: %w", string(data), err)
	}
	return milliC, nil
}
