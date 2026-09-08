package collector

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const procRootPath = "/proc"

// processClockTicksPerSecond is USER_HZ, the fixed scale /proc/<pid>/stat's
// utime/stime jiffie counters are reported in, regardless of the kernel's
// own timer frequency (CONFIG_HZ) — see `man 5 proc`. It has been 100 on
// every architecture Linux supports for decades, so it is hardcoded here
// rather than queried via sysconf(_SC_CLK_TCK), which would require cgo.
const processClockTicksPerSecond = 100

// processStat holds one process's name and cumulative CPU-time counters,
// read from /proc/<pid>/stat.
type processStat struct {
	comm         string
	utime, stime uint64
}

// parseProcPidStat parses /proc/<pid>/stat content into a processStat. The
// process name is enclosed in parentheses and may itself contain spaces or
// even parentheses (e.g. a process renamed to "(sshd)"), so it is extracted
// between the first "(" and the last ")" rather than by naive field
// splitting. Every field after the closing paren is then whitespace
// delimited with state as its first entry, so utime/stime — fields 14 and
// 15 of the whole line — sit at offsets 11 and 12 of that remainder.
func parseProcPidStat(data string) (processStat, error) {
	open := strings.IndexByte(data, '(')
	closeIdx := strings.LastIndexByte(data, ')')
	if open < 0 || closeIdx < open {
		return processStat{}, fmt.Errorf("unexpected /proc/<pid>/stat content: %q", data)
	}
	fields := strings.Fields(data[closeIdx+1:])
	if len(fields) < 13 {
		return processStat{}, fmt.Errorf("too few fields after comm in /proc/<pid>/stat content: %q", data)
	}
	utime, err := strconv.ParseUint(fields[11], 10, 64)
	if err != nil {
		return processStat{}, fmt.Errorf("parse utime: %w", err)
	}
	stime, err := strconv.ParseUint(fields[12], 10, 64)
	if err != nil {
		return processStat{}, fmt.Errorf("parse stime: %w", err)
	}
	return processStat{comm: data[open+1 : closeIdx], utime: utime, stime: stime}, nil
}

// parseProcPidStatusRSS parses /proc/<pid>/status content for its VmRSS
// line (e.g. "VmRSS:\t    1234 kB") and returns the resident set size in
// bytes. A process with no VmRSS line (e.g. a kernel thread, which has no
// resident memory of its own) reports 0 rather than an error.
func parseProcPidStatusRSS(data string) (uint64, error) {
	for _, line := range strings.Split(data, "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("unexpected VmRSS line in /proc/<pid>/status content: %q", line)
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmRSS on line %q: %w", line, err)
		}
		return kb * 1024, nil
	}
	return 0, nil
}

// ProcessCollector tracks previous per-process CPU-time samples to compute
// each process's CPU% between two Collect calls, and reports the current
// top-N processes by CPU usage and by resident memory (RSS). It is
// intended to be driven from the slow tick rather than the fast one:
// walking every /proc/<pid> entry is far more expensive than reading a
// single /proc file, so doing it at the same cadence as CPU/memory/disk
// sampling would cost too much on constrained hardware such as a Pi Zero.
type ProcessCollector struct {
	root string
	now  func() time.Time

	mu       sync.Mutex
	prev     map[int]processStat
	prevTime time.Time
}

// NewProcessCollector creates a ProcessCollector reading from /proc.
func NewProcessCollector() *ProcessCollector {
	return &ProcessCollector{root: procRootPath, now: time.Now}
}

// Collect walks every /proc/<pid> entry and returns the topN processes by
// CPU usage and by RSS. The first call after process start has no prior
// sample to diff CPU usage against, so every process reports 0% CPU on
// that call; RSS is meaningful immediately. Subsequent calls report real
// CPU deltas over the time elapsed since the previous call.
//
// A process that exits between the directory listing and reading its own
// stat/status files, or whose files aren't readable (e.g. a zombie, or a
// kernel thread with a restricted /proc/<pid>/status), is skipped rather
// than treated as a collection error — with potentially hundreds of
// processes churning, that is the expected steady state, not a failure.
func (c *ProcessCollector) Collect(topN int) (Processes, error) {
	entries, err := os.ReadDir(c.root)
	if err != nil {
		return Processes{}, fmt.Errorf("read %s: %w", c.root, err)
	}

	cur := make(map[int]processStat, len(entries))
	rss := make(map[int]uint64, len(entries))
	for _, e := range entries {
		pid, convErr := strconv.Atoi(e.Name())
		if convErr != nil {
			continue // not a pid directory (self, net, sys, ...)
		}
		statData, readErr := os.ReadFile(c.root + "/" + e.Name() + "/stat")
		if readErr != nil {
			continue
		}
		stat, parseErr := parseProcPidStat(string(statData))
		if parseErr != nil {
			continue
		}
		statusData, readErr := os.ReadFile(c.root + "/" + e.Name() + "/status")
		if readErr != nil {
			continue
		}
		rssBytes, parseErr := parseProcPidStatusRSS(string(statusData))
		if parseErr != nil {
			continue
		}
		cur[pid] = stat
		rss[pid] = rssBytes
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	all := make([]Process, 0, len(cur))
	elapsed := now.Sub(c.prevTime).Seconds()
	for pid, stat := range cur {
		var cpuPercent float64
		if c.prev != nil && elapsed > 0 {
			if p, ok := c.prev[pid]; ok && stat.utime >= p.utime && stat.stime >= p.stime {
				ticks := float64((stat.utime - p.utime) + (stat.stime - p.stime))
				cpuPercent = ticks / processClockTicksPerSecond / elapsed * 100
			}
		}
		all = append(all, Process{PID: pid, Name: stat.comm, CPUPercent: cpuPercent, RSSBytes: rss[pid]})
	}

	c.prev = cur
	c.prevTime = now

	byCPU := append([]Process(nil), all...)
	sort.Slice(byCPU, func(i, j int) bool {
		if byCPU[i].CPUPercent != byCPU[j].CPUPercent {
			return byCPU[i].CPUPercent > byCPU[j].CPUPercent
		}
		return byCPU[i].PID < byCPU[j].PID
	})
	byMemory := append([]Process(nil), all...)
	sort.Slice(byMemory, func(i, j int) bool {
		if byMemory[i].RSSBytes != byMemory[j].RSSBytes {
			return byMemory[i].RSSBytes > byMemory[j].RSSBytes
		}
		return byMemory[i].PID < byMemory[j].PID
	})

	return Processes{ByCPU: topProcesses(byCPU, topN), ByMemory: topProcesses(byMemory, topN)}, nil
}

// topProcesses returns the first n entries of sorted (which must already be
// sorted by the caller's desired ranking), clamped to [0, len(sorted)], as
// a freshly allocated non-nil slice so an empty result marshals as [] and
// never null.
func topProcesses(sorted []Process, n int) []Process {
	if n < 0 {
		n = 0
	}
	if n > len(sorted) {
		n = len(sorted)
	}
	out := make([]Process, n)
	copy(out, sorted[:n])
	return out
}
