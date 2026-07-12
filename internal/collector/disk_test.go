package collector

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

const mountsFixture = `/dev/root / ext4 rw,relatime 0 0
proc /proc proc rw,nosuid,nodev,noexec,relatime 0 0
sysfs /sys sysfs rw,nosuid,nodev,noexec,relatime 0 0
tmpfs /run tmpfs rw,nosuid,nodev,size=100000k 0 0
/dev/mmcblk0p1 /boot/firmware vfat rw,relatime 0 0
/dev/sda1 /mnt/usb ext4 rw,relatime 0 0
overlay / overlay rw 0 0
`

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

func TestDiskCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	mountsPath := filepath.Join(dir, "mounts")
	if err := os.WriteFile(mountsPath, []byte(mountsFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	fakeStatfs := func(path string, buf *syscall.Statfs_t) error {
		buf.Bsize = 4096
		buf.Blocks = 1000
		buf.Bfree = 250
		return nil
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

	// Real filesystems in the fixture: /, /boot/firmware, /mnt/usb (3),
	// pseudo ones (proc, sysfs, tmpfs, overlay) must be excluded.
	if len(disks) != 3 {
		t.Fatalf("expected 3 real disks, got %d: %+v", len(disks), disks)
	}
	for _, d := range disks {
		if d.TotalBytes != 4096*1000 {
			t.Errorf("disk %s: TotalBytes = %d, want %d", d.Mountpoint, d.TotalBytes, 4096*1000)
		}
		wantUsedPct := (1000.0 - 250.0) / 1000.0 * 100
		if diffFloat(d.UsedPercent, wantUsedPct) > 0.01 {
			t.Errorf("disk %s: UsedPercent = %v, want %v", d.Mountpoint, d.UsedPercent, wantUsedPct)
		}
	}
}

func TestDiskCollector_Collect_SkipsFailingStatfs(t *testing.T) {
	dir := t.TempDir()
	mountsPath := filepath.Join(dir, "mounts")
	if err := os.WriteFile(mountsPath, []byte(mountsFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	c := &DiskCollector{
		mountsPath:     mountsPath,
		excludedFSType: defaultExcludedFSTypes,
		statfs: func(path string, buf *syscall.Statfs_t) error {
			return os.ErrNotExist
		},
	}

	disks, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect should not fail even if all statfs calls fail: %v", err)
	}
	if len(disks) != 0 {
		t.Fatalf("expected 0 disks when all statfs calls fail, got %d", len(disks))
	}
}
