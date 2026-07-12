package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const procUptimePath = "/proc/uptime"

// parseUptime parses /proc/uptime content, e.g. "12345.67 89012.34", and
// returns the first field: seconds since boot.
func parseUptime(data string) (float64, error) {
	fields := strings.Fields(data)
	if len(fields) < 1 {
		return 0, fmt.Errorf("unexpected /proc/uptime content: %q", data)
	}
	secs, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, fmt.Errorf("parse uptime: %w", err)
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
