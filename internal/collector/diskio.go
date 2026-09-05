package collector

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const procDiskStatsPath = "/proc/diskstats"

// diskStatsSectorBytes is the fixed sector size /proc/diskstats reports
// counters in, regardless of a device's actual logical block size (see
// Documentation/admin-guide/iostats.rst in the kernel source).
const diskStatsSectorBytes = 512

// diskStatCounters holds the raw cumulative sector counters for one block
// device.
type diskStatCounters struct {
	sectorsRead    uint64
	sectorsWritten uint64
}

// parseDiskStats parses /proc/diskstats content into per-device counters.
// Each line has (at least) the form "major minor name reads_completed
// reads_merged sectors_read time_reading writes_completed writes_merged
// sectors_written ...", whitespace-separated with no header, per
// Documentation/admin-guide/iostats.rst. Only the sector counters (fields 6
// and 10, 1-indexed) are needed here.
func parseDiskStats(data string) (map[string]diskStatCounters, error) {
	result := make(map[string]diskStatCounters)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		name := fields[2]
		sectorsRead, err := strconv.ParseUint(fields[5], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse sectors read for %q: %w", name, err)
		}
		sectorsWritten, err := strconv.ParseUint(fields[9], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse sectors written for %q: %w", name, err)
		}
		result[name] = diskStatCounters{sectorsRead: sectorsRead, sectorsWritten: sectorsWritten}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/diskstats: %w", err)
	}
	return result, nil
}

// DiskIOCollector tracks previous /proc/diskstats samples to compute
// per-device read/write throughput. Loop/ram devices and partitions are
// always excluded — loop/ram mirror NetworkCollector excluding the
// loopback interface (neither represents real storage activity worth
// reporting), and partitions are excluded because the kernel's
// part_stat_add already folds every partition's sectors into its parent
// device's own counters (see block/genhd.c), so reporting both would
// double-count the same I/O under two names.
type DiskIOCollector struct {
	path string
	now  func() time.Time

	mu       sync.Mutex
	prev     map[string]diskStatCounters
	prevTime time.Time
}

// NewDiskIOCollector creates a DiskIOCollector reading from
// /proc/diskstats.
func NewDiskIOCollector() *DiskIOCollector {
	return &DiskIOCollector{path: procDiskStatsPath, now: time.Now}
}

// diskIOExcludedPrefixes marks virtual block devices that never represent
// real storage activity, so they don't clutter the reported device list.
var diskIOExcludedPrefixes = []string{"loop", "ram"}

// diskIOPartitionRE matches partition device names for the naming schemes
// /proc/diskstats commonly reports on Linux: SCSI/(P)ATA/virtio-style
// letter-suffixed disks (sda1, hda2, vda1, xvda1) and the numbered-parent
// schemes that separate partition numbers with a "p" (mmcblk0p1, the SD
// card's own partitions on a Pi; nvme0n1p1). There is no sysfs lookup here
// (e.g. a "partition" attribute file) on purpose — collectors in this
// package parse only their fixed /proc or /sys source, so this stays
// unit-testable against a plain fixture string rather than requiring a
// real filesystem layout — see docs/TESTS.md.
var diskIOPartitionRE = regexp.MustCompile(`^(?:(?:sd|hd|vd|xvd)[a-z]+[0-9]+|(?:mmcblk[0-9]+|nvme[0-9]+n[0-9]+)p[0-9]+)$`)

func isExcludedDiskIODevice(name string) bool {
	for _, prefix := range diskIOExcludedPrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return diskIOPartitionRE.MatchString(name)
}

// Collect returns current per-device read/write throughput. The first call
// after process start has no prior sample to diff against, so it returns an
// empty slice; subsequent calls return meaningful rates.
func (c *DiskIOCollector) Collect() ([]DiskIO, error) {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.path, err)
	}
	cur, err := parseDiskStats(string(data))
	if err != nil {
		return nil, err
	}
	now := c.now()

	c.mu.Lock()
	defer c.mu.Unlock()

	var devices []DiskIO
	if c.prev != nil {
		elapsed := now.Sub(c.prevTime).Seconds()
		if elapsed > 0 {
			for name, curCounters := range cur {
				if isExcludedDiskIODevice(name) {
					continue
				}
				prevCounters, ok := c.prev[name]
				if !ok || curCounters.sectorsRead < prevCounters.sectorsRead || curCounters.sectorsWritten < prevCounters.sectorsWritten {
					continue
				}
				devices = append(devices, DiskIO{
					Device:           name,
					ReadBytesPerSec:  float64(curCounters.sectorsRead-prevCounters.sectorsRead) * diskStatsSectorBytes / elapsed,
					WriteBytesPerSec: float64(curCounters.sectorsWritten-prevCounters.sectorsWritten) * diskStatsSectorBytes / elapsed,
				})
			}
		}
	}

	c.prev = cur
	c.prevTime = now

	// Sort by device name so the API response has a stable order rather
	// than reflecting random map iteration.
	sort.Slice(devices, func(i, j int) bool {
		return devices[i].Device < devices[j].Device
	})

	return devices, nil
}
