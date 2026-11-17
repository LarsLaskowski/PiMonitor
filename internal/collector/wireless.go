package collector

import (
	"bufio"
	"fmt"
	"math"
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
//
// A link or level value strconv.ParseFloat decodes as NaN or ±Inf is
// rejected, because it would make the JSON encoding of the snapshot fail.
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
		link, err := parseWirelessValue(name, fields[1], "link quality")
		if err != nil {
			return nil, err
		}
		level, err := parseWirelessValue(name, fields[2], "signal level")
		if err != nil {
			return nil, err
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

// parseWirelessValue parses one link or level field of a /proc/net/wireless
// line. what names the value in error messages. A value that ParseFloat
// decodes as NaN or ±Inf is rejected, as documented on parseWireless.
func parseWirelessValue(name, field, what string) (float64, error) {
	v, err := strconv.ParseFloat(field, 64)
	if err != nil {
		return 0, fmt.Errorf("parse %s for %q: %w", what, name, err)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("parse %s for %q: non-finite value %q", what, name, field)
	}
	return v, nil
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
// /proc/net/wireless at all: that is an error (the collector logs it once)
// and the result is nil, so the snapshot omits the field.
func (c *WirelessCollector) Collect() ([]Wireless, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.path, err)
	}
	return parseWireless(string(data))
}
