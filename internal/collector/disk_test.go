package collector

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

const mountsFixture = `/dev/root / ext4 rw,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev,size=100000k 0 0
/dev/mmcblk0p1 /boot/firmware vfat rw,relatime 0 0
/dev/sda1 /mnt/usb ext4 rw,relatime 0 0
overlay / overlay rw 0 0
`

// writeMountsFixture writes a /proc/mounts fixture into a temp dir and
// returns its path.
func writeMountsFixture(t *testing.T, content string) string {
	t.Helper()
	mountsPath := filepath.Join(t.TempDir(), "mounts")
	if err := os.WriteFile(mountsPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return mountsPath
}

func TestParseMounts(t *testing.T) {
	entries, err := parseMounts(mountsFixture)
	if err != nil {
		t.Fatalf("parseMounts: %v", err)
	}
	if len(entries) != 7 {
		t.Fatalf("expected 7 entries, got %d", len(entries))
	}
	if entries[0].mountpoint != "/" || entries[0].fstype != "ext4" {
		t.Fatalf("unexpected first entry: %+v", entries[0])
	}
}

// /proc/mounts encodes space, tab, newline, and backslash as octal escapes
// since those characters would otherwise be ambiguous in the
// whitespace-separated format (see proc(5)), for all three of the device,
// mountpoint, and fstype fields (e.g. a USB drive labeled "My Drive", or a
// fuse subtype derived from user input). parseMounts must decode them back
// to the real value in each field.
func TestParseMounts_DecodesOctalEscapes(t *testing.T) {
	tests := []struct {
		name           string
		line           string
		wantDevice     string
		wantMountpoint string
		wantFSType     string
	}{
		{
			name:           "escapes in mountpoint",
			line:           `/dev/sda1 /media/pi/My\040Drive\011\012\134end vfat rw,relatime 0 0`,
			wantDevice:     "/dev/sda1",
			wantMountpoint: "/media/pi/My Drive\t\n\\end",
			wantFSType:     "vfat",
		},
		{
			name:           "escapes in device",
			line:           `/dev/disk/by-label/My\040Label /mnt/usb ext4 rw,relatime 0 0`,
			wantDevice:     "/dev/disk/by-label/My Label",
			wantMountpoint: "/mnt/usb",
			wantFSType:     "ext4",
		},
		{
			name:           "escapes in fstype subtype",
			line:           `fusefs /mnt/x fuse.My\040FS rw 0 0`,
			wantDevice:     "fusefs",
			wantMountpoint: "/mnt/x",
			wantFSType:     "fuse.My FS",
		},
		{
			name: "unknown backslash sequences left untouched",
			// Neither "\x41" nor a lone trailing "\" is one of the four
			// escapes /proc/mounts actually emits, so both must pass
			// through unchanged rather than being misinterpreted or
			// panicking on a short slice.
			line:           `/dev/sda1 /mnt/weird\x41\ vfat rw,relatime 0 0`,
			wantDevice:     "/dev/sda1",
			wantMountpoint: `/mnt/weird\x41\`,
			wantFSType:     "vfat",
		},
		{
			name: "escaped backslash is not re-scanned with what follows it",
			// The kernel emits a literal backslash in a path as "\134".
			// A single left-to-right pass that only consumes the three
			// digits of a matched escape must decode "\134040" (an
			// escaped backslash followed by literal "040") to "\040", not
			// to a space — i.e. it must not re-scan the "040" that
			// follows the emitted backslash as a fresh escape.
			line:           `/dev/sda1 /mnt/a\134040b vfat rw,relatime 0 0`,
			wantDevice:     "/dev/sda1",
			wantMountpoint: `/mnt/a\040b`,
			wantFSType:     "vfat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries, err := parseMounts(tt.line + "\n")
			if err != nil {
				t.Fatalf("parseMounts: %v", err)
			}
			if len(entries) != 1 {
				t.Fatalf("expected 1 entry, got %d", len(entries))
			}
			got := entries[0]
			if got.device != tt.wantDevice {
				t.Errorf("device = %q, want %q", got.device, tt.wantDevice)
			}
			if got.mountpoint != tt.wantMountpoint {
				t.Errorf("mountpoint = %q, want %q", got.mountpoint, tt.wantMountpoint)
			}
			if got.fstype != tt.wantFSType {
				t.Errorf("fstype = %q, want %q", got.fstype, tt.wantFSType)
			}
		})
	}
}

func TestDefaultExcludedFSTypes_FiltersPseudoFS(t *testing.T) {
	for _, fstype := range []string{"proc", "sysfs", "tmpfs", "overlay"} {
		if !defaultExcludedFSTypes[fstype] {
			t.Errorf("expected %q to be excluded by default", fstype)
		}
	}
	for _, fstype := range []string{"ext4", "vfat", "btrfs", "exfat"} {
		if defaultExcludedFSTypes[fstype] {
			t.Errorf("expected %q to NOT be excluded by default", fstype)
		}
	}
}

func TestDefaultExcludedFSTypes_FiltersNetworkFS(t *testing.T) {
	for _, fstype := range []string{"nfs", "nfs4", "cifs", "smb3", "9p", "fuse.sshfs"} {
		if !defaultExcludedFSTypes[fstype] {
			t.Errorf("expected network fstype %q to be excluded by default", fstype)
		}
	}
}

func TestDiskCollector_Collect(t *testing.T) {
	mountsPath := writeMountsFixture(t, mountsFixture)

	fakeStatfs := func(path string) (fsStats, error) {
		return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 250, Bavail: 200}, nil
	}

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs:         fakeStatfs,
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}

	// Real filesystems in the fixture: /boot/firmware and /mnt/usb (2).
	// Pseudo ones (proc, sysfs, tmpfs, overlay) must be excluded, and "/"
	// is overmounted by the trailing overlay entry — the overlay is what
	// statfs("/") actually sees, so "/" must not be reported as ext4.
	if len(disks) != 2 {
		t.Fatalf("expected 2 real disks, got %d: %+v", len(disks), disks)
	}
	for _, d := range disks {
		if d.Mountpoint == "/" {
			t.Errorf("overmounted / (overlay) should not be reported")
		}
		if d.TotalBytes != 4096*1000 {
			t.Errorf("disk %s: TotalBytes = %d, want %d", d.Mountpoint, d.TotalBytes, 4096*1000)
		}
		// df semantics: used / (used + avail), not used / total.
		wantUsedPct := (1000.0 - 250.0) / (1000.0 - 250.0 + 200.0) * 100
		if diffFloat(d.UsedPercent, wantUsedPct) > 0.01 {
			t.Errorf("disk %s: UsedPercent = %v, want %v", d.Mountpoint, d.UsedPercent, wantUsedPct)
		}
	}
}

func TestDiskCollector_Collect_ExcludesNetworkFS(t *testing.T) {
	fixture := `/dev/root / ext4 rw,relatime 0 0
nas:/export /mnt/nas nfs4 rw,relatime 0 0
nas:/export2 /mnt/nas2 nfs rw,relatime 0 0
//nas/share /mnt/smb cifs rw,relatime 0 0
user@host:/ /mnt/ssh fuse.sshfs rw,relatime 0 0
`
	mountsPath := writeMountsFixture(t, fixture)

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 250, Bavail: 200}, nil
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(disks) != 1 || disks[0].Mountpoint != "/" {
		t.Fatalf("expected only / to be reported, got %+v", disks)
	}
}

func TestDiskCollector_Collect_UsedPercentMatchesDf(t *testing.T) {
	fixture := "/dev/root / ext4 rw,relatime 0 0\n"
	mountsPath := writeMountsFixture(t, fixture)

	// Bfree > Bavail models ext4's root-reserved blocks: from an
	// unprivileged user's point of view (and df's Use%), the filesystem
	// is 100% full even though Bfree is not zero.
	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 50, Bavail: 0}, nil
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(disks))
	}
	d := disks[0]
	if diffFloat(d.UsedPercent, 100.0) > 0.01 {
		t.Errorf("UsedPercent = %v, want 100 (df semantics: used/(used+avail))", d.UsedPercent)
	}
	if d.TotalBytes != 4096*1000 {
		t.Errorf("TotalBytes = %d, want %d", d.TotalBytes, 4096*1000)
	}
	if d.UsedBytes != 4096*950 {
		t.Errorf("UsedBytes = %d, want %d (total - Bfree, unchanged semantics)", d.UsedBytes, 4096*950)
	}
}

func TestDiskCollector_Collect_ZeroDenominator(t *testing.T) {
	fixture := "/dev/root / ext4 rw,relatime 0 0\n"
	mountsPath := writeMountsFixture(t, fixture)

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			// nothing used, nothing available: denom == 0
			return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 1000, Bavail: 0}, nil
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(disks) != 1 {
		t.Fatalf("expected 1 disk, got %d", len(disks))
	}
	if disks[0].UsedPercent != 0 {
		t.Errorf("UsedPercent = %v, want 0 when used+avail is 0", disks[0].UsedPercent)
	}
}

func TestDiskCollector_Collect_DeduplicatesMountpoints(t *testing.T) {
	// /proc/mounts can list the same mountpoint several times (overmounts,
	// some bind-mount setups). Only the last mount is visible at the path.
	fixture := `/dev/mmcblk0p2 /data ext4 rw,relatime 0 0
/dev/root / ext4 rw,relatime 0 0
/dev/sda1 /data vfat rw,relatime 0 0
`
	mountsPath := writeMountsFixture(t, fixture)

	var statCalls atomic.Int32
	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			statCalls.Add(1)
			return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 250, Bavail: 200}, nil
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(disks) != 2 {
		t.Fatalf("expected 2 disks (deduplicated), got %d: %+v", len(disks), disks)
	}
	// First-seen order is preserved, but the last entry per mountpoint wins.
	if disks[0].Mountpoint != "/data" || disks[0].Device != "/dev/sda1" || disks[0].FSType != "vfat" {
		t.Errorf("expected /data from the later mount (/dev/sda1, vfat), got %+v", disks[0])
	}
	if disks[1].Mountpoint != "/" {
		t.Errorf("expected / as second disk, got %+v", disks[1])
	}
	if got := statCalls.Load(); got != 2 {
		t.Errorf("expected each unique mountpoint to be statted once (2 calls), got %d", got)
	}
}

func TestDiskCollector_Collect_SkipsFailingStatfs(t *testing.T) {
	mountsPath := writeMountsFixture(t, mountsFixture)

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			return fsStats{}, os.ErrNotExist
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect should not fail even if all statfs calls fail: %v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("expected 0 disks when all statfs calls fail, got %d", len(disks))
	}
	if disks == nil {
		t.Fatal("expected non-nil empty slice (marshals as [], not null), got nil")
	}
}

func TestDiskCollector_Collect_NoMountsReturnsEmptyNotNilSlice(t *testing.T) {
	// Only pseudo filesystems, all excluded by default: order/byMountpoint
	// end up empty, but Collect must still return a non-nil slice so
	// Snapshot.Disks marshals as [] rather than null (issue #70).
	fixture := "proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0\n"
	mountsPath := writeMountsFixture(t, fixture)

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string) (fsStats, error) {
			return fsStats{}, os.ErrNotExist
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if disks == nil {
		t.Fatal("expected non-nil empty slice when no mountpoints qualify, got nil")
	}
	if len(disks) != 0 {
		t.Fatalf("expected 0 disks, got %d", len(disks))
	}
}

func TestDiskCollector_Collect_HungStatfsTimesOutAndCoolsDown(t *testing.T) {
	fixture := `/dev/root / ext4 rw,relatime 0 0
nas-fallback /mnt/hung ext4 rw,relatime 0 0
/dev/sda1 /mnt/usb ext4 rw,relatime 0 0
`
	mountsPath := writeMountsFixture(t, fixture)

	release := make(chan struct{})
	defer close(release) // let abandoned goroutines exit at test end
	var hungCalls atomic.Int32
	fakeStatfs := func(path string) (fsStats, error) {
		if path == "/mnt/hung" {
			hungCalls.Add(1)
			<-release // simulate statfs blocking on a dead NFS server
			return fsStats{}, os.ErrDeadlineExceeded
		}
		return fsStats{Bsize: 4096, Blocks: 1000, Bfree: 250, Bavail: 200}, nil
	}

	current := time.Now()
	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs:         fakeStatfs,
		statfsTimeout:  20 * time.Millisecond,
		statfsCooldown: time.Minute,
		now:            func() time.Time { return current },
	}

	start := time.Now()
	disks, err := c.Collect()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	// Generous bound: the hung mount must cost at most one timeout, not
	// block the collection loop indefinitely.
	if elapsed > 2*time.Second {
		t.Fatalf("Collect took %v despite hung statfs, want well under 2s", elapsed)
	}
	if len(disks) != 2 {
		t.Fatalf("expected the 2 healthy mounts despite one hung, got %d: %+v", len(disks), disks)
	}
	for _, d := range disks {
		if d.Mountpoint == "/mnt/hung" {
			t.Errorf("hung mountpoint must not be reported: %+v", d)
		}
	}
	if got := hungCalls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 statfs attempt on the hung mount, got %d", got)
	}

	// Within the cooldown window the bad mountpoint is skipped entirely,
	// so no additional goroutine is parked on it.
	if _, err := c.Collect(); err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if got := hungCalls.Load(); got != 1 {
		t.Fatalf("expected no retry within cooldown, got %d statfs attempts", got)
	}

	// After the cooldown expires the mountpoint is retried.
	current = current.Add(2 * time.Minute)
	if _, err := c.Collect(); err != nil {
		t.Fatalf("third Collect: %v", err)
	}
	if got := hungCalls.Load(); got != 2 {
		t.Fatalf("expected retry after cooldown, got %d statfs attempts", got)
	}
}
