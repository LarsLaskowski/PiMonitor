package collector

import (
	"bufio"
	"errors"
	"fmt"
	"math"
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
	if err := validateMeminfo(result); err != nil {
		return nil, err
	}
	return result, nil
}

// validateMeminfo rejects parsed content that is no valid reading: a missing
// required field, a value beyond the byte range, a zero MemTotal or a free
// value above its total.
func validateMeminfo(result map[string]uint64) error {
	for _, key := range requiredMeminfoKeys {
		if _, ok := result[key]; !ok {
			return fmt.Errorf("no %s field in /proc/meminfo content", key)
		}
	}
	for _, key := range meminfoByteRangeKeys {
		if result[key] > maxMeminfoKB {
			return fmt.Errorf("%s exceeds the byte range in /proc/meminfo content", key)
		}
	}
	if result["MemTotal"] == 0 {
		return errors.New("MemTotal is 0 in /proc/meminfo content")
	}
	for _, pair := range meminfoFreeTotalPairs {
		if result[pair.free] > result[pair.total] {
			return fmt.Errorf("%s exceeds %s in /proc/meminfo content", pair.free, pair.total)
		}
	}
	return nil
}

// requiredMeminfoKeys lists the fields every kernel writes unconditionally;
// content lacking one of them is not a valid reading. MemAvailable is
// optional (missing before Linux 3.14).
var requiredMeminfoKeys = []string{"MemTotal", "MemFree", "SwapTotal", "SwapFree"}

// maxMeminfoKB is the largest kB value that still fits in bytes as a uint64
// ((2^64-1)/1024); a larger one would wrap when multiplied by 1024.
const maxMeminfoKB = math.MaxUint64 / 1024

// meminfoByteRangeKeys lists the fields PiMonitor reads, in the order the
// byte-range check names them. A missing MemAvailable reads 0.
var meminfoByteRangeKeys = []string{"MemTotal", "MemFree", "MemAvailable", "SwapTotal", "SwapFree"}

// meminfoFreeTotalPairs lists the free values the kernel never reports above
// their total; content that does so is a failed reading, not a negative used
// percent or an invented 0 % swap.
var meminfoFreeTotalPairs = []struct{ free, total string }{
	{"MemFree", "MemTotal"},
	{"MemAvailable", "MemTotal"},
	{"SwapFree", "SwapTotal"},
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
