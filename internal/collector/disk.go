package collector

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

const procMountsPath = "/proc/mounts"

const (
	// defaultStatfsTimeout bounds how long a single statfs call may block
	// before its mountpoint is skipped for this tick. statfs on a dying
	// device (or an unexpectedly present network mount) can hang, and the
	// collection loop runs all collectors sequentially — one stuck call
	// must not freeze every metric.
	defaultStatfsTimeout = 2 * time.Second
	// defaultStatfsCooldown is how long a mountpoint whose statfs timed
	// out is skipped before being retried, so a persistently hung mount
	// parks at most one goroutine per cooldown period instead of one per
	// tick.
	defaultStatfsCooldown = time.Minute
)

// errStatfsTimeout marks a statfs call abandoned after defaultStatfsTimeout.
var errStatfsTimeout = errors.New("statfs timed out")

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

	// Network filesystems are excluded as well: statfs(2) on an
	// unreachable server can block indefinitely (e.g. a hard-mounted NFS
	// share whose NAS is down), which would stall the entire collection
	// loop — and remote capacity belongs to the remote system's own
	// monitoring anyway.
	"nfs":        true,
	"nfs4":       true,
	"cifs":       true,
	"smbfs":      true,
	"smb3":       true,
	"9p":         true,
	"fuse.sshfs": true,
	"davfs":      true,
	"afs":        true,
	"glusterfs":  true,
	"ceph":       true,
	"curlftpfs":  true,
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
	// statfsTimeout bounds each statfs call; zero disables the timeout
	// and calls statfs inline.
	statfsTimeout time.Duration
	// statfsCooldown is how long a timed-out mountpoint stays in badUntil.
	statfsCooldown time.Duration
	// now allows tests to control the cooldown clock; nil means time.Now.
	now func() time.Time
	// badUntil maps mountpoints whose statfs timed out to when they may
	// be retried. Only accessed from Collect, which the collection loop
	// calls from a single goroutine.
	badUntil map[string]time.Time
}

// NewDiskCollector creates a DiskCollector reading from /proc/mounts,
// filtering the default set of pseudo filesystem types.
func NewDiskCollector() *DiskCollector {
	return &DiskCollector{
		mountsPath:     procMountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs:         syscall.Statfs,
		statfsTimeout:  defaultStatfsTimeout,
		statfsCooldown: defaultStatfsCooldown,
	}
}

func (c *DiskCollector) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

// statfsWithTimeout runs c.statfs in its own goroutine and gives up after
// c.statfsTimeout. statfs(2) cannot be canceled: on timeout the goroutine
// is abandoned and lingers until the syscall returns (for a hard-mounted,
// unreachable network share that can be forever). Collect bounds the
// pile-up via the badUntil cooldown so a stuck mountpoint is not retried
// on every tick. A zero timeout calls statfs inline without a goroutine.
func (c *DiskCollector) statfsWithTimeout(path string) (syscall.Statfs_t, error) {
	if c.statfsTimeout <= 0 {
		var buf syscall.Statfs_t
		err := c.statfs(path, &buf)
		return buf, err
	}

	type result struct {
		buf syscall.Statfs_t
		err error
	}
	// Buffered so an abandoned goroutine can send its late result and
	// exit instead of leaking forever on the channel.
	ch := make(chan result, 1)
	go func() {
		var buf syscall.Statfs_t
		err := c.statfs(path, &buf)
		ch <- result{buf: buf, err: err}
	}()

	timer := time.NewTimer(c.statfsTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.buf, r.err
	case <-timer.C:
		return syscall.Statfs_t{}, errStatfsTimeout
	}
}

// Collect returns usage for every mounted filesystem not in the excluded
// fstype set. Mountpoints that fail to stat (e.g. removed between reading
// /proc/mounts and statfs) or whose statfs hangs beyond the timeout are
// skipped rather than failing the whole call.
func (c *DiskCollector) Collect() ([]Disk, error) {
	data, err := os.ReadFile(c.mountsPath)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", c.mountsPath, err)
	}
	entries, err := parseMounts(string(data))
	if err != nil {
		return nil, err
	}

	order, byMountpoint := dedupeMounts(entries)

	now := c.clock()
	// Always non-nil (even with zero entries) so Snapshot.Disks marshals as
	// [] rather than null, matching docs/API.md.
	disks := make([]Disk, 0, len(order))
	for _, mp := range order {
		e := byMountpoint[mp]
		if c.excludedFSType[e.fstype] {
			continue
		}
		if disk, ok := c.collectDisk(e, now); ok {
			disks = append(disks, disk)
		}
	}
	return disks, nil
}

// dedupeMounts collapses /proc/mounts entries to the last entry per
// mountpoint. /proc/mounts can list the same mountpoint several times
// (overmounts, some bind-mount setups); only the last mount is visible at
// the path — and the one statfs reports on. First-seen order is preserved
// for stable output.
func dedupeMounts(entries []mountEntry) (order []string, byMountpoint map[string]mountEntry) {
	order = make([]string, 0, len(entries))
	byMountpoint = make(map[string]mountEntry, len(entries))
	for _, e := range entries {
		if _, seen := byMountpoint[e.mountpoint]; !seen {
			order = append(order, e.mountpoint)
		}
		byMountpoint[e.mountpoint] = e
	}
	return order, byMountpoint
}

// collectDisk statfs's a single mountpoint and reports its usage. ok is
// false when the mountpoint should be skipped: it's on the bad-mount
// cooldown, statfs failed, or it reports zero size.
func (c *DiskCollector) collectDisk(e mountEntry, now time.Time) (disk Disk, ok bool) {
	if until, bad := c.badUntil[e.mountpoint]; bad {
		if now.Before(until) {
			return Disk{}, false
		}
		delete(c.badUntil, e.mountpoint)
	}
	buf, err := c.statfsWithTimeout(e.mountpoint)
	if err != nil {
		if errors.Is(err, errStatfsTimeout) {
			c.markBad(e.mountpoint, now)
		}
		return Disk{}, false
	}
	total := uint64(buf.Blocks) * uint64(buf.Bsize)
	if total == 0 {
		return Disk{}, false
	}
	free := uint64(buf.Bfree) * uint64(buf.Bsize)
	avail := uint64(buf.Bavail) * uint64(buf.Bsize)
	used := total - free
	// df-compatible percentage: used / (used + avail). Bfree includes
	// blocks reserved for root (typically 5% on ext4) that services
	// cannot write to, so a Blocks-based percentage under-reports
	// fullness — showing ~95% while df (and failing writes) already
	// say 100%.
	var usedPercent float64
	if denom := used + avail; denom > 0 {
		usedPercent = float64(used) / float64(denom) * 100
	}
	return Disk{
		Mountpoint:  e.mountpoint,
		Device:      e.device,
		FSType:      e.fstype,
		TotalBytes:  total,
		UsedBytes:   used,
		UsedPercent: usedPercent,
	}, true
}

// markBad puts mountpoint on cooldown after a statfs timeout so subsequent
// Collect calls skip it instead of blocking again for statfsTimeout.
func (c *DiskCollector) markBad(mountpoint string, now time.Time) {
	if c.badUntil == nil {
		c.badUntil = make(map[string]time.Time)
	}
	c.badUntil[mountpoint] = now.Add(c.statfsCooldown)
}
