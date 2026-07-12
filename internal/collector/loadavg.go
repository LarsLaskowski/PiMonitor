package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const procLoadAvgPath = "/proc/loadavg"

// parseLoadAvg parses the content of /proc/loadavg, e.g.
// "0.52 0.58 0.59 1/523 12345".
func parseLoadAvg(data string) (LoadAverage, error) {
	fields := strings.Fields(data)
	if len(fields) < 3 {
		return LoadAverage{}, fmt.Errorf("unexpected /proc/loadavg content: %q", data)
	}
	load1, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return LoadAverage{}, fmt.Errorf("parse load1: %w", err)
	}
	load5, err := strconv.ParseFloat(fields[1], 64)
	if err != nil {
		return LoadAverage{}, fmt.Errorf("parse load5: %w", err)
	}
	load15, err := strconv.ParseFloat(fields[2], 64)
	if err != nil {
		return LoadAverage{}, fmt.Errorf("parse load15: %w", err)
	}
	return LoadAverage{Load1: load1, Load5: load5, Load15: load15}, nil
}

// LoadAvgCollector reads the kernel-computed load averages. Unlike CPU
// usage, no delta computation is needed: the kernel already smooths these
// values over 1/5/15 minutes.
type LoadAvgCollector struct {
	path string
}

// NewLoadAvgCollector creates a LoadAvgCollector reading from /proc/loadavg.
func NewLoadAvgCollector() *LoadAvgCollector {
	return &LoadAvgCollector{path: procLoadAvgPath}
}

// Collect returns the current 1/5/15 minute load averages.
func (c *LoadAvgCollector) Collect() (LoadAverage, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return LoadAverage{}, fmt.Errorf("read %s: %w", c.path, err)
	}
	return parseLoadAvg(string(data))
}
