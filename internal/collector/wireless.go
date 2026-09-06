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
// link/level/noise carry a trailing "." (e.g. "70.", "-40.") when the kernel
// marks that particular value as currently updated (see
// wireless_seq_printf_stats in the kernel's net/wireless/wext-proc.c);
// strconv.ParseFloat accepts that trailing-dot form the same as a plain
// integer.
//
// The kernel prints a line for every wireless netdev regardless of whether
// it has a current reading — an interface that exists but isn't associated
// to any network (or hasn't received a stats update yet) gets the "null
// stats" line "0000    0     0     0        0 ...", with no trailing "."
// on any field and all-zero values. Without filtering that out, such an
// interface would be reported as a real reading of signal_dbm 0, which is
// actually the strongest possible signal — the opposite of "no signal" this
// represents. That line is skipped rather than parsed.
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
		if isNullWirelessStats(fields[1], fields[2], link, level) {
			continue
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

// isNullWirelessStats reports whether a parsed link/level pair is the
// kernel's "null stats" placeholder for a wireless interface with no
// current reading, rather than a genuine zero reading: neither field
// carries the "updated" marker (a trailing ".") and both parsed to zero.
func isNullWirelessStats(linkField, levelField string, link, level float64) bool {
	return !strings.HasSuffix(linkField, ".") && !strings.HasSuffix(levelField, ".") && link == 0 && level == 0
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
// The file is present on any kernel built with wireless extensions support
// (the common case, including Raspberry Pi OS) regardless of whether
// wireless hardware exists - a host with none just sees an empty file body
// after the two header lines. A minimal kernel without that support has no
// /proc/net/wireless at all, so a missing file is not treated as a
// collection error either - it degrades to an empty slice like any other
// optional, hardware-dependent metric.
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
