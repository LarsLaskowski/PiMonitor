package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const procMeminfoPath = "/proc/meminfo"

// parseMeminfo parses /proc/meminfo content into a key->kB map. Every
// value in /proc/meminfo is reported in kB regardless of the trailing
// unit label, per the kernel documentation.
func parseMeminfo(data string) (map[string]uint64, error) {
	result := make(map[string]uint64)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		rest := strings.Fields(line[idx+1:])
		if len(rest) == 0 {
			continue
		}
		v, err := strconv.ParseUint(rest[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse /proc/meminfo value for %q: %w", key, err)
		}
		result[key] = v
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/meminfo: %w", err)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no fields found in /proc/meminfo content")
	}
	return result, nil
}

func memAndSwapFromFields(fields map[string]uint64) (Memory, Swap) {
	const kB = 1024

	memTotal := fields["MemTotal"] * kB
	memAvailable := fields["MemAvailable"] * kB
	if memAvailable == 0 && fields["MemFree"] > 0 {
		// MemAvailable was added in Linux 3.14; fall back to MemFree on
		// older kernels rather than reporting a bogus 100% used.
		memAvailable = fields["MemFree"] * kB
	}
	var memUsedPct float64
	if memTotal > 0 {
		memUsedPct = (1 - float64(memAvailable)/float64(memTotal)) * 100
	}

	swapTotal := fields["SwapTotal"] * kB
	swapFree := fields["SwapFree"] * kB
	swapUsed := uint64(0)
	if swapTotal > swapFree {
		swapUsed = swapTotal - swapFree
	}
	var swapUsedPct float64
	if swapTotal > 0 {
		swapUsedPct = float64(swapUsed) / float64(swapTotal) * 100
	}

	return Memory{
			TotalBytes:     memTotal,
			AvailableBytes: memAvailable,
			UsedPercent:    memUsedPct,
		}, Swap{
			TotalBytes:  swapTotal,
			UsedBytes:   swapUsed,
			UsedPercent: swapUsedPct,
		}
}

// MemoryCollector reads RAM and swap usage from /proc/meminfo.
type MemoryCollector struct {
	path string
}

// NewMemoryCollector creates a MemoryCollector reading from /proc/meminfo.
func NewMemoryCollector() *MemoryCollector {
	return &MemoryCollector{path: procMeminfoPath}
}

// Collect returns current RAM and swap usage.
func (c *MemoryCollector) Collect() (Memory, Swap, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return Memory{}, Swap{}, fmt.Errorf("read %s: %w", c.path, err)
	}
	fields, err := parseMeminfo(string(data))
	if err != nil {
		return Memory{}, Swap{}, err
	}
	mem, swap := memAndSwapFromFields(fields)
	return mem, swap, nil
}
