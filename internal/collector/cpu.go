package collector

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
)

const procStatPath = "/proc/stat"

// cpuStatLine holds the raw jiffie counters for one "cpu"/"cpuN" line of
// /proc/stat. Only the fields needed for a busy/idle usage calculation are
// kept.
type cpuStatLine struct {
	name                                                  string
	user, nice, system, idle, iowait, irq, softirq, steal uint64
}

func (l cpuStatLine) total() uint64 {
	return l.user + l.nice + l.system + l.idle + l.iowait + l.irq + l.softirq + l.steal
}

func (l cpuStatLine) idleTotal() uint64 {
	return l.idle + l.iowait
}

// parseProcStat parses the "cpu" and "cpuN" lines of /proc/stat content.
// The aggregate "cpu" line, if present, is returned first.
func parseProcStat(data string) ([]cpuStatLine, error) {
	var lines []cpuStatLine
	for _, raw := range strings.Split(data, "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 8 || !strings.HasPrefix(fields[0], "cpu") {
			continue
		}
		vals := make([]uint64, 8)
		for i := 0; i < 8; i++ {
			v, err := strconv.ParseUint(fields[i+1], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("parse /proc/stat field %d on line %q: %w", i+1, raw, err)
			}
			vals[i] = v
		}
		lines = append(lines, cpuStatLine{
			name:    fields[0],
			user:    vals[0],
			nice:    vals[1],
			system:  vals[2],
			idle:    vals[3],
			iowait:  vals[4],
			irq:     vals[5],
			softirq: vals[6],
			steal:   vals[7],
		})
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("no cpu lines found in /proc/stat content")
	}
	return lines, nil
}

// usagePercent computes the busy percentage between two samples of the
// same CPU line. Returns 0 if the samples are equal or out of order.
func usagePercent(prev, cur cpuStatLine) float64 {
	totalDelta := float64(cur.total()) - float64(prev.total())
	idleDelta := float64(cur.idleTotal()) - float64(prev.idleTotal())
	if totalDelta <= 0 {
		return 0
	}
	pct := (1 - idleDelta/totalDelta) * 100
	if pct < 0 {
		return 0
	}
	if pct > 100 {
		return 100
	}
	return pct
}

// CPUCollector tracks previous /proc/stat samples to compute usage deltas.
type CPUCollector struct {
	path string
	mu   sync.Mutex
	prev map[string]cpuStatLine
}

// NewCPUCollector creates a CPUCollector reading from /proc/stat.
func NewCPUCollector() *CPUCollector {
	return &CPUCollector{path: procStatPath}
}

// Collect returns the current CPU usage. The first call after process
// start has no prior sample to diff against, so it returns all-zero
// values; subsequent calls return meaningful deltas.
func (c *CPUCollector) Collect() (CPUUsage, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return CPUUsage{}, fmt.Errorf("read %s: %w", c.path, err)
	}
	lines, err := parseProcStat(string(data))
	if err != nil {
		return CPUUsage{}, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	usage := CPUUsage{}
	if c.prev != nil {
		for _, l := range lines {
			if p, ok := c.prev[l.name]; ok {
				pct := usagePercent(p, l)
				if l.name == "cpu" {
					usage.OverallPercent = pct
				} else {
					usage.PerCorePercent = append(usage.PerCorePercent, pct)
				}
			}
		}
	}

	c.prev = make(map[string]cpuStatLine, len(lines))
	for _, l := range lines {
		c.prev[l.name] = l
	}

	return usage, nil
}

// CoreCount returns the number of per-core lines in /proc/stat (excluding
// the aggregate "cpu" line), used to give the load-average gauge a
// reference scale.
func (c *CPUCollector) CoreCount() (int, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", c.path, err)
	}
	lines, err := parseProcStat(string(data))
	if err != nil {
		return 0, err
	}
	count := 0
	for _, l := range lines {
		if l.name != "cpu" {
			count++
		}
	}
	return count, nil
}
