package collector

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
)

const procUptimePath = "/proc/uptime"

// parseUptime parses /proc/uptime content, e.g. "12345.67 89012.34", and
// returns the first field: seconds since boot. A value strconv.ParseFloat
// decodes as NaN or ±Inf is rejected, because it would make the JSON encoding
// of the snapshot fail.
func parseUptime(data string) (float64, error) {
	fields := strings.Fields(data)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime content: %q", data)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime: %w", err)
	}
	if math.IsNaN(secs) || math.IsInf(secs, 0) {
		return 0, fmt.Errorf("parse uptime: non-finite value %q", fields[0])
	}
	return secs, nil
}

// UptimeCollector reports how long the system has been running.
type UptimeCollector struct {
	path string
}

// NewUptimeCollector creates an UptimeCollector reading from /proc/uptime.
func NewUptimeCollector() *UptimeCollector {
	return &UptimeCollector{path: procUptimePath}
}

// Collect returns the seconds elapsed since boot.
func (c *UptimeCollector) Collect() (float64, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", c.path, err)
	}
	return parseUptime(string(data))
}
