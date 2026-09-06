package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

const procNetWirelessPath = "/proc/net/wireless"

// parseWireless parses /proc/net/wireless content into per-interface signal
// readings. The file has two header lines followed by one line per wireless
// interface in the form
// "iface: status link level noise nwid crypt frag retry misc beacon", where
// link/level/noise are often written with a trailing "." (e.g. "70.", "-40.")
// by the kernel's wireless extensions code; strconv.ParseFloat accepts that
// form.
func parseWireless(data string) ([]Wireless, error) {
	var result []Wireless
	scanner := bufio.NewScanner(strings.NewReader(data))
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum <= 2 {
			continue // skip the two header lines
		}
		line := scanner.Text()
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		name := strings.TrimSpace(line[:colonIdx])
		fields := strings.Fields(line[colonIdx+1:])
		if len(fields) < 3 {
			continue
		}
		link, err := strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return nil, fmt.Errorf("parse link quality for %q: %w", name, err)
		}
		level, err := strconv.ParseFloat(fields[2], 64)
		if err != nil {
			return nil, fmt.Errorf("parse signal level for %q: %w", name, err)
		}
		result = append(result, Wireless{
			Interface:   name,
			LinkQuality: link,
			SignalDBm:   level,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/net/wireless: %w", err)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Interface < result[j].Interface })
	return result, nil
}

// WirelessCollector reports link quality and signal level for wireless
// network interfaces from /proc/net/wireless.
type WirelessCollector struct {
	path string
}

// NewWirelessCollector creates a WirelessCollector reading from
// /proc/net/wireless.
func NewWirelessCollector() *WirelessCollector {
	return &WirelessCollector{path: procNetWirelessPath}
}

// Collect returns the current signal reading for every wireless interface.
// A host with no wireless hardware typically has no /proc/net/wireless at
// all, so a missing file is not treated as a collection error - it degrades
// to an empty slice like any other optional, hardware-dependent metric.
func (c *WirelessCollector) Collect() ([]Wireless, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", c.path, err)
	}
	return parseWireless(string(data))
}
