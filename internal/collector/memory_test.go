package collector

import (
	"os"
	"path/filepath"
	"testing"
)

const meminfoFixture = `MemTotal:        1000000 kB
MemFree:          200000 kB
MemAvailable:     400000 kB
Buffers:           50000 kB
Cached:           150000 kB
SwapTotal:        500000 kB
SwapFree:         100000 kB
`

func TestParseMeminfo(t *testing.T) {
	fields, err := parseMeminfo(meminfoFixture)
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}
	if fields["MemTotal"] != 1000000 {
		t.Fatalf("MemTotal = %d, want 1000000", fields["MemTotal"])
	}
	if fields["SwapFree"] != 100000 {
		t.Fatalf("SwapFree = %d, want 100000", fields["SwapFree"])
	}
}

func TestParseMeminfo_Empty(t *testing.T) {
	if _, err := parseMeminfo(""); err == nil {
		t.Fatal("expected error for empty /proc/meminfo content")
	}
}

func TestMemAndSwapFromFields(t *testing.T) {
	fields, err := parseMeminfo(meminfoFixture)
	if err != nil {
		t.Fatalf("parseMeminfo: %v", err)
	}
	mem, swap := memAndSwapFromFields(fields)

	if mem.TotalBytes != 1000000*1024 {
		t.Fatalf("mem.TotalBytes = %d, want %d", mem.TotalBytes, 1000000*1024)
	}
	if mem.AvailableBytes != 400000*1024 {
		t.Fatalf("mem.AvailableBytes = %d, want %d", mem.AvailableBytes, 400000*1024)
	}
	wantUsedPct := (1 - 400000.0/1000000.0) * 100
	if diffFloat(mem.UsedPercent, wantUsedPct) > 0.01 {
		t.Fatalf("mem.UsedPercent = %v, want %v", mem.UsedPercent, wantUsedPct)
	}

	if swap.TotalBytes != 500000*1024 {
		t.Fatalf("swap.TotalBytes = %d, want %d", swap.TotalBytes, 500000*1024)
	}
	wantSwapUsed := uint64(400000 * 1024)
	if swap.UsedBytes != wantSwapUsed {
		t.Fatalf("swap.UsedBytes = %d, want %d", swap.UsedBytes, wantSwapUsed)
	}
}

func TestMemAndSwapFromFields_NoSwap(t *testing.T) {
	fields := map[string]uint64{"MemTotal": 1000, "MemAvailable": 500}
	_, swap := memAndSwapFromFields(fields)
	if swap.TotalBytes != 0 || swap.UsedPercent != 0 {
		t.Fatalf("expected zero swap when no swap configured, got %+v", swap)
	}
}

func TestMemAndSwapFromFields_OldKernelFallback(t *testing.T) {
	// Kernels before 3.14 have no MemAvailable field.
	fields := map[string]uint64{"MemTotal": 1000, "MemFree": 300}
	mem, _ := memAndSwapFromFields(fields)
	if mem.AvailableBytes != 300*1024 {
		t.Fatalf("expected MemFree fallback, got AvailableBytes=%d", mem.AvailableBytes)
	}
}

func TestMemoryCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte(meminfoFixture), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := &MemoryCollector{path: path}
	mem, swap, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if mem.TotalBytes == 0 || swap.TotalBytes == 0 {
		t.Fatalf("expected non-zero totals, got mem=%+v swap=%+v", mem, swap)
	}
}
