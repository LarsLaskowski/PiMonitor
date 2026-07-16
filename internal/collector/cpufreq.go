package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// cpuFreqGlob matches the per-core cpufreq sysfs directory of every CPU,
// e.g. /sys/devices/system/cpu/cpu0/cpufreq. The [0-9]* restricts matches to
// numbered cpuN directories, excluding sibling non-core entries such as the
// top-level /sys/devices/system/cpu/cpufreq policy directory.
const cpuFreqGlob = "/sys/devices/system/cpu/cpu[0-9]*/cpufreq"

// CPUFreqCollector reads per-core CPU clock speed and scaling governor from
// sysfs (/sys/devices/system/cpu/cpuN/cpufreq/{scaling_cur_freq,scaling_governor}).
// It is a cpufreq-driver signal: on systems without one (e.g. many
// development machines, or a kernel built without CONFIG_CPU_FREQ) it
// degrades to an empty reading rather than failing.
type CPUFreqCollector struct {
	glob string // sysfs glob for per-core cpufreq dirs (overridable in tests)
}

// NewCPUFreqCollector creates a CPUFreqCollector reading from the standard
// sysfs cpufreq location.
func NewCPUFreqCollector() *CPUFreqCollector {
	return &CPUFreqCollector{glob: cpuFreqGlob}
}

// Collect returns one reading per core with a readable cpufreq directory,
// sorted by core index. A core that is offline, or whose driver does not
// expose scaling_cur_freq/scaling_governor, is silently skipped rather than
// failing the whole call - cpufreq availability varies across Pi models and
// kernels.
func (c *CPUFreqCollector) Collect() ([]CPUCoreFrequency, error) {
	dirs, err := filepath.Glob(c.glob)
	if err != nil {
		return nil, fmt.Errorf("glob %s: %w", c.glob, err)
	}

	var freqs []CPUCoreFrequency
	for _, dir := range dirs {
		reading, err := readCPUCoreFrequency(dir)
		if err != nil {
			continue
		}
		freqs = append(freqs, reading)
	}

	sort.Slice(freqs, func(i, j int) bool { return freqs[i].Core < freqs[j].Core })
	return freqs, nil
}

// coreIndexFromCPUFreqDir extracts the core number from a cpufreq sysfs
// directory path, e.g. ".../cpu3/cpufreq" -> 3.
func coreIndexFromCPUFreqDir(dir string) (int, error) {
	coreDir := filepath.Base(filepath.Dir(dir))
	numPart := strings.TrimPrefix(coreDir, "cpu")
	if numPart == coreDir {
		return 0, fmt.Errorf("unexpected cpufreq directory %q", dir)
	}
	n, err := strconv.Atoi(numPart)
	if err != nil {
		return 0, fmt.Errorf("parse core index from %q: %w", dir, err)
	}
	return n, nil
}

// readCPUCoreFrequency reads a single core's scaling_cur_freq (reported in
// kHz, converted to MHz) and scaling_governor from its cpufreq sysfs
// directory.
func readCPUCoreFrequency(dir string) (CPUCoreFrequency, error) {
	core, err := coreIndexFromCPUFreqDir(dir)
	if err != nil {
		return CPUCoreFrequency{}, err
	}

	freqData, err := os.ReadFile(filepath.Join(dir, "scaling_cur_freq"))
	if err != nil {
		return CPUCoreFrequency{}, fmt.Errorf("read %s/scaling_cur_freq: %w", dir, err)
	}
	khz, err := strconv.ParseUint(strings.TrimSpace(string(freqData)), 10, 64)
	if err != nil {
		return CPUCoreFrequency{}, fmt.Errorf("parse scaling_cur_freq %q: %w", string(freqData), err)
	}

	govData, err := os.ReadFile(filepath.Join(dir, "scaling_governor"))
	if err != nil {
		return CPUCoreFrequency{}, fmt.Errorf("read %s/scaling_governor: %w", dir, err)
	}

	return CPUCoreFrequency{
		Core:     core,
		MHz:      float64(khz) / 1000,
		Governor: strings.TrimSpace(string(govData)),
	}, nil
}
