package collector

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const procNetDevPath = "/proc/net/dev"

// netDevCounters holds the raw cumulative byte counters for one interface.
type netDevCounters struct {
	rxBytes uint64
	txBytes uint64
}

// parseNetDev parses /proc/net/dev content into per-interface counters.
// The file has two header lines followed by one line per interface in the
// form "iface: rxBytes rxPackets ... txBytes txPackets ...".
func parseNetDev(data string) (map[string]netDevCounters, error) {
	result := make(map[string]netDevCounters)
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
		if len(fields) < 9 {
			continue
		}
		rx, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse rx bytes for %q: %w", name, err)
		}
		tx, err := strconv.ParseUint(fields[8], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse tx bytes for %q: %w", name, err)
		}
		result[name] = netDevCounters{rxBytes: rx, txBytes: tx}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/net/dev: %w", err)
	}
	return result, nil
}

// NetworkCollector tracks previous /proc/net/dev samples to compute
// per-interface throughput. The loopback interface is always excluded.
type NetworkCollector struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	prev     map[string]netDevCounters
	prevTime time.Time
}

// NewNetworkCollector creates a NetworkCollector reading from
// /proc/net/dev.
func NewNetworkCollector() *NetworkCollector {
	return &NetworkCollector{path: procNetDevPath, now: time.Now}
}

// Collect returns current per-interface throughput. The first call after
// process start has no prior sample to diff against, so it returns an
// empty slice; subsequent calls return meaningful rates.
func (c *NetworkCollector) Collect() ([]NetworkInterface, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.path, err)
	}
	cur, err := parseNetDev(string(data))
	if err != nil {
		return nil, err
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	var ifaces []NetworkInterface
	if c.prev != nil {
		elapsed := now.Sub(c.prevTime).Seconds()
		if elapsed > 0 {
			for name, curCounters := range cur {
				if name == "lo" {
					continue
				}
				prevCounters, ok := c.prev[name]
				if !ok || curCounters.rxBytes < prevCounters.rxBytes || curCounters.txBytes < prevCounters.txBytes {
					continue
				}
				ifaces = append(ifaces, NetworkInterface{
					Name:          name,
					RxBytesPerSec: float64(curCounters.rxBytes-prevCounters.rxBytes) / elapsed,
					TxBytesPerSec: float64(curCounters.txBytes-prevCounters.txBytes) / elapsed,
				})
			}
		}
	}

	c.prev = cur
	c.prevTime = now

	return ifaces, nil
}
