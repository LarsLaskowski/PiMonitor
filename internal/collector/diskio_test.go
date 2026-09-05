package collector

import (
	"testing"
	"time"
)

const diskStatsFixture1 = `   8       0 sda 1000 50 20000 500 2000 100 40000 1000 0 800 1500
   8       1 sda1 900 40 18000 450 1900 90 36000 900 0 700 1350
 179       0 mmcblk0 5000 100 100000 2000 3000 200 60000 3000 0 2500 5000
   7       0 loop0 10 0 200 5 0 0 0 0 0 5 5
`

const diskStatsFixture2 = `   8       0 sda 1500 60 40000 700 2500 120 60000 1300 0 1000 2000
   8       1 sda1 1350 50 35000 620 2350 105 54000 1150 0 900 1750
 179       0 mmcblk0 6000 120 140000 2400 3400 230 68000 3400 0 2900 5800
   7       0 loop0 10 0 200 5 0 0 0 0 0 5 5
`

func TestParseDiskStats(t *testing.T) {
	counters, err := parseDiskStats(diskStatsFixture1)
	if err != nil {
		t.Fatalf("parseDiskStats: %v", err)
	}
	if len(counters) != 4 {
		t.Fatalf("expected 4 devices, got %d", len(counters))
	}
	if counters["sda"].sectorsRead != 20000 || counters["sda"].sectorsWritten != 40000 {
		t.Fatalf("unexpected sda counters: %+v", counters["sda"])
	}
	if counters["mmcblk0"].sectorsRead != 100000 || counters["mmcblk0"].sectorsWritten != 60000 {
		t.Fatalf("unexpected mmcblk0 counters: %+v", counters["mmcblk0"])
	}
}

func TestParseDiskStats_IgnoresShortLines(t *testing.T) {
	counters, err := parseDiskStats("   8       0 sda 1 2 3\n")
	if err != nil {
		t.Fatalf("parseDiskStats: %v", err)
	}
	if len(counters) != 0 {
		t.Fatalf("expected short lines to be skipped, got %+v", counters)
	}
}

// TestDiskIOCollector_Collect covers the issue #14 acceptance criterion:
// two /proc/diskstats fixtures a known interval apart yield the expected
// read/write bytes-per-second (sectors × 512 bytes / elapsed seconds).
func TestDiskIOCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "diskstats", diskStatsFixture1)

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &DiskIOCollector{path: path, now: func() time.Time { return fakeNow }}

	first, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if len(first) != 0 {
		t.Fatalf("first Collect should return no devices (no prior sample), got %+v", first)
	}

	overwriteTempFile(t, path, diskStatsFixture2)
	fakeNow = fakeNow.Add(10 * time.Second)

	second, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	// loop0 is excluded; sda, sda1 (its partition) and mmcblk0 remain,
	// sorted by name.
	if len(second) != 3 {
		t.Fatalf("expected 3 devices (loop0 excluded), got %d: %+v", len(second), second)
	}

	mmcblk0 := second[0]
	if mmcblk0.Device != "mmcblk0" {
		t.Fatalf("second[0].Device = %q, want %q", mmcblk0.Device, "mmcblk0")
	}
	wantMMCRead := float64(140000-100000) * diskStatsSectorBytes / 10
	if diffFloat(mmcblk0.ReadBytesPerSec, wantMMCRead) > 0.01 {
		t.Fatalf("mmcblk0 ReadBytesPerSec = %v, want %v", mmcblk0.ReadBytesPerSec, wantMMCRead)
	}
	wantMMCWrite := float64(68000-60000) * diskStatsSectorBytes / 10
	if diffFloat(mmcblk0.WriteBytesPerSec, wantMMCWrite) > 0.01 {
		t.Fatalf("mmcblk0 WriteBytesPerSec = %v, want %v", mmcblk0.WriteBytesPerSec, wantMMCWrite)
	}

	sda := second[1]
	if sda.Device != "sda" {
		t.Fatalf("second[1].Device = %q, want %q", sda.Device, "sda")
	}
	wantSdaRead := float64(40000-20000) * diskStatsSectorBytes / 10
	if diffFloat(sda.ReadBytesPerSec, wantSdaRead) > 0.01 {
		t.Fatalf("sda ReadBytesPerSec = %v, want %v", sda.ReadBytesPerSec, wantSdaRead)
	}
	wantSdaWrite := float64(60000-40000) * diskStatsSectorBytes / 10
	if diffFloat(sda.WriteBytesPerSec, wantSdaWrite) > 0.01 {
		t.Fatalf("sda WriteBytesPerSec = %v, want %v", sda.WriteBytesPerSec, wantSdaWrite)
	}
}

func TestDiskIOCollector_Collect_ExcludesLoopAndRamDevices(t *testing.T) {
	dir := t.TempDir()
	fixture1 := `   7       0 loop0 10 0 200 5 0 0 0 0 0 5 5
   1       0 ram0 5 0 100 2 0 0 0 0 0 2 2
 179       0 mmcblk0 5000 100 100000 2000 3000 200 60000 3000 0 2500 5000
`
	fixture2 := `   7       0 loop0 20 0 400 10 0 0 0 0 0 10 10
   1       0 ram0 10 0 200 4 0 0 0 0 0 4 4
 179       0 mmcblk0 6000 120 140000 2400 3400 230 68000 3400 0 2900 5800
`
	path := writeTempFile(t, dir, "diskstats", fixture1)

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &DiskIOCollector{path: path, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, path, fixture2)
	fakeNow = fakeNow.Add(time.Second)

	devices, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(devices) != 1 || devices[0].Device != "mmcblk0" {
		t.Fatalf("expected only mmcblk0 (loop0/ram0 excluded), got %+v", devices)
	}
}

func TestDiskIOCollector_Collect_SkipsCounterResets(t *testing.T) {
	dir := t.TempDir()
	fixture1 := ` 179       0 mmcblk0 5000 100 100000 2000 3000 200 60000 3000 0 2500 5000
`
	// A device whose counters go backwards (e.g. a device replaced between
	// ticks and re-enumerated from zero) must be skipped rather than
	// reported as a bogus negative-turned-huge rate.
	fixture2 := ` 179       0 mmcblk0 10 0 200 5 0 0 0 0 0 5 5
`
	path := writeTempFile(t, dir, "diskstats", fixture1)

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &DiskIOCollector{path: path, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, path, fixture2)
	fakeNow = fakeNow.Add(time.Second)

	devices, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("expected a counter reset to be skipped, got %+v", devices)
	}
}
