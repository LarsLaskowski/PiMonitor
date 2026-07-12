package collector

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"syscall"
)

const procMountsPath = "/proc/mounts"

// defaultExcludedFSTypes mirrors df's default exclusion of virtual/pseudo
// filesystems, so the dashboard shows real storage (root, SD card
// partitions, USB drives) rather than being cluttered with tmpfs/proc/etc.
var defaultExcludedFSTypes = map[string]bool{
	"tmpfs":       true,
	"devtmpfs":    true,
	"proc":        true,
	"sysfs":       true,
	"cgroup":      true,
	"cgroup2":     true,
	"overlay":     true,
	"debugfs":     true,
	"tracefs":     true,
	"devpts":      true,
	"securityfs":  true,
	"pstore":      true,
	"bpf":         true,
	"autofs":      true,
	"mqueue":      true,
	"hugetlbfs":   true,
	"configfs":    true,
	"fusectl":     true,
	"rpc_pipefs":  true,
	"binfmt_misc": true,
}

// mountEntry is one parsed line of /proc/mounts.
type mountEntry struct {
	device     string
	mountpoint string
	fstype     string
}

// parseMounts parses /proc/mounts content into mount entries.
func parseMounts(data string) ([]mountEntry, error) {
	var entries []mountEntry
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		entries = append(entries, mountEntry{
			device:     fields[0],
			mountpoint: fields[1],
			fstype:     fields[2],
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan /proc/mounts: %w", err)
	}
	return entries, nil
}

// statfsFunc matches syscall.Statfs's signature so tests can inject a fake
// implementation instead of touching the real filesystem.
type statfsFunc func(path string, buf *syscall.Statfs_t) error

// DiskCollector reports usage of real (non-pseudo) mounted filesystems.
type DiskCollector struct {
	mountsPath     string
	excludedFSType map[string]bool
	statfs         statfsFunc
}

// NewDiskCollector creates a DiskCollector reading from /proc/mounts,
// filtering the default set of pseudo filesystem types.
func NewDiskCollector() *DiskCollector {
	return &DiskCollector{
		mountsPath:     procMountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs:         syscall.Statfs,
	}
}

// Collect returns usage for every mounted filesystem not in the excluded
// fstype set. Mountpoints that fail to stat (e.g. removed between reading
// /proc/mounts and statfs) are skipped rather than failing the whole call.
func (c *DiskCollector) Collect() ([]Disk, error) {
	data, err := os.ReadFile(c.mountsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.mountsPath, err)
	}
	entries, err := parseMounts(string(data))
	if err != nil {
		return nil, err
	}

	var disks []Disk
	for _, e := range entries {
		if c.excludedFSType[e.fstype] {
			continue
		}
		var buf syscall.Statfs_t
		if err := c.statfs(e.mountpoint, &buf); err != nil {
			continue
		}
		total := uint64(buf.Blocks) * uint64(buf.Bsize)
		free := uint64(buf.Bfree) * uint64(buf.Bsize)
		if total == 0 {
			continue
		}
		used := total - free
		disks = append(disks, Disk{
			Mountpoint:  e.mountpoint,
			Device:      e.device,
			FSType:      e.fstype,
			TotalBytes:  total,
			UsedBytes:   used,
			UsedPercent: float64(used) / float64(total) * 100,
		})
	}
	return disks, nil
}
