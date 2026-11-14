package collector

import (
	"os"
	"path/filepath"
	"strings"
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

// meminfoRealSample is /proc/meminfo of a Linux 6.18.44 x86_64 host (issue #249):
// a modern kernel with MemAvailable and no swap.
const meminfoRealSample = `MemTotal:       16473776 kB
MemFree:        15666136 kB
MemAvailable:   15982088 kB
Buffers:           38296 kB
Cached:           480400 kB
SwapCached:            0 kB
Active:           130360 kB
Inactive:         565876 kB
Active(anon):         68 kB
Inactive(anon):   194100 kB
Active(file):     130292 kB
Inactive(file):   371776 kB
Unevictable:       14684 kB
Mlocked:           10708 kB
SwapTotal:             0 kB
SwapFree:              0 kB
Zswap:                 0 kB
Zswapped:              0 kB
Dirty:                48 kB
Writeback:             0 kB
AnonPages:        192572 kB
Mapped:           148360 kB
Shmem:             16632 kB
KReclaimable:      36772 kB
Slab:              59336 kB
SReclaimable:      36772 kB
SUnreclaim:        22564 kB
KernelStack:        1760 kB
PageTables:         2220 kB
SecPageTables:         0 kB
NFS_Unstable:          0 kB
Bounce:                0 kB
WritebackTmp:          0 kB
CommitLimit:     8236888 kB
Committed_AS:     455804 kB
VmallocTotal:   34359738367 kB
VmallocUsed:        6464 kB
VmallocChunk:          0 kB
Percpu:              992 kB
AnonHugePages:         0 kB
ShmemHugePages:        0 kB
ShmemPmdMapped:        0 kB
FileHugePages:         0 kB
FilePmdMapped:         0 kB
Balloon:               0 kB
HugePages_Total:       0
HugePages_Free:        0
HugePages_Rsvd:        0
HugePages_Surp:        0
Hugepagesize:       2048 kB
Hugetlb:               0 kB
DirectMap4k:       20480 kB
DirectMap2M:     2076672 kB
DirectMap1G:    16777216 kB
`

// meminfoWithout returns content without the line of the given key.
func meminfoWithout(t *testing.T, content, key string) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if !strings.HasPrefix(line, key+":") {
			kept = append(kept, line)
		}
	}
	if len(kept) == len(lines) {
		t.Fatalf("test setup: no %s line in the content", key)
	}
	return strings.Join(kept, "\n")
}

// meminfoWith returns content with the line of the given key replaced.
func meminfoWith(t *testing.T, content, key, line string) string {
	t.Helper()
	lines := strings.Split(content, "\n")
	replaced := false
	for i, l := range lines {
		if strings.HasPrefix(l, key+":") {
			lines[i] = line
			replaced = true
		}
	}
	if !replaced {
		t.Fatalf("test setup: no %s line in the content", key)
	}
	return strings.Join(lines, "\n")
}

func TestParseMeminfo_MissingField_AC1(t *testing.T) {
	for _, key := range []string{"MemTotal", "MemFree", "SwapTotal", "SwapFree"} {
		t.Run("AC1 missing "+key, func(t *testing.T) {
			_, err := parseMeminfo(meminfoWithout(t, meminfoRealSample, key))
			want := "no " + key + " field in /proc/meminfo content"
			if err == nil {
				t.Fatalf("AC1: parseMeminfo without %s: no error, want %q", key, want)
			}
			if err.Error() != want {
				t.Errorf("AC1: error = %q, want %q", err.Error(), want)
			}
		})
	}
	t.Run("AC1 empty content", func(t *testing.T) {
		_, err := parseMeminfo("")
		want := "no MemTotal field in /proc/meminfo content"
		if err == nil || err.Error() != want {
			t.Errorf("AC1: error = %v, want %q", err, want)
		}
	})
	t.Run("AC1 first missing in order", func(t *testing.T) {
		content := meminfoWithout(t, meminfoWithout(t, meminfoRealSample, "SwapTotal"), "MemFree")
		_, err := parseMeminfo(content)
		want := "no MemFree field in /proc/meminfo content"
		if err == nil || err.Error() != want {
			t.Errorf("AC1: error = %v, want %q", err, want)
		}
	})
}

func TestParseMeminfo_ZeroMemTotal_AC2(t *testing.T) {
	content := meminfoWith(t, meminfoRealSample, "MemTotal", "MemTotal:              0 kB")
	_, err := parseMeminfo(content)
	want := "MemTotal is 0 in /proc/meminfo content"
	if err == nil {
		t.Fatalf("AC2: parseMeminfo with MemTotal 0: no error, want %q", want)
	}
	if err.Error() != want {
		t.Errorf("AC2: error = %q, want %q", err.Error(), want)
	}
}

func TestParseMeminfo_Complete_AC3(t *testing.T) {
	t.Run("AC3 real sample", func(t *testing.T) {
		fields, err := parseMeminfo(meminfoRealSample)
		if err != nil {
			t.Fatalf("AC3: parseMeminfo(real sample): %v, want no error", err)
		}
		mem, swap := memAndSwapFromFields(fields)
		if want := uint64(16473776) * 1024; mem.TotalBytes != want {
			t.Errorf("AC3: MemTotal = %d, want %d", mem.TotalBytes, want)
		}
		if swap.TotalBytes != 0 || swap.UsedBytes != 0 || swap.UsedPercent != 0 {
			t.Errorf("AC3: swap = %+v, want all 0", swap)
		}
	})
	t.Run("AC3 without MemAvailable", func(t *testing.T) {
		fields, err := parseMeminfo(meminfoWithout(t, meminfoRealSample, "MemAvailable"))
		if err != nil {
			t.Fatalf("AC3: parseMeminfo without MemAvailable: %v, want no error", err)
		}
		mem, _ := memAndSwapFromFields(fields)
		if want := uint64(15666136) * 1024; mem.AvailableBytes != want {
			t.Errorf("AC3: AvailableBytes = %d, want %d (MemFree)", mem.AvailableBytes, want)
		}
	})
}

// wantMeminfoError fails the test unless parseMeminfo of content fails with
// exactly want.
func wantMeminfoError(t *testing.T, ac, content, want string) {
	t.Helper()
	_, err := parseMeminfo(content)
	if err == nil {
		t.Fatalf("%s: parseMeminfo: no error, want %q", ac, want)
	}
	if err.Error() != want {
		t.Errorf("%s: error = %q, want %q", ac, err.Error(), want)
	}
}

// meminfoSwap returns the real sample with both swap lines replaced.
func meminfoSwap(t *testing.T, total, free string) string {
	t.Helper()
	content := meminfoWith(t, meminfoRealSample, "SwapTotal", "SwapTotal: "+total+" kB")
	return meminfoWith(t, content, "SwapFree", "SwapFree: "+free+" kB")
}

func TestParseMeminfo_OutOfRange_AC1(t *testing.T) {
	// The issue's example: MemTotal 100, MemFree 50, MemAvailable 300.
	content := meminfoWith(t, meminfoRealSample, "MemTotal", "MemTotal: 100 kB")
	content = meminfoWith(t, content, "MemFree", "MemFree: 50 kB")
	content = meminfoWith(t, content, "MemAvailable", "MemAvailable: 300 kB")
	wantMeminfoError(t, "AC1", content, "MemAvailable exceeds MemTotal in /proc/meminfo content")
}

func TestParseMeminfo_OutOfRange_AC2(t *testing.T) {
	const want = "MemFree exceeds MemTotal in /proc/meminfo content"
	t.Run("AC2 without MemAvailable", func(t *testing.T) {
		content := meminfoWithout(t, meminfoRealSample, "MemAvailable")
		content = meminfoWith(t, content, "MemFree", "MemFree: 16473777 kB")
		wantMeminfoError(t, "AC2", content, want)
	})
	t.Run("AC2 with MemAvailable in range", func(t *testing.T) {
		content := meminfoWith(t, meminfoRealSample, "MemFree", "MemFree: 16473777 kB")
		wantMeminfoError(t, "AC2", content, want)
	})
	t.Run("AC2 both exceed", func(t *testing.T) {
		content := meminfoWith(t, meminfoRealSample, "MemFree", "MemFree: 16473777 kB")
		content = meminfoWith(t, content, "MemAvailable", "MemAvailable: 16473778 kB")
		wantMeminfoError(t, "AC2", content, want)
	})
}

func TestParseMeminfo_OutOfRange_AC3(t *testing.T) {
	content := meminfoSwap(t, "1000", "1001")
	wantMeminfoError(t, "AC3", content, "SwapFree exceeds SwapTotal in /proc/meminfo content")
}

func TestMemoryCollector_Collect_OutOfRange_AC5(t *testing.T) {
	content := meminfoWith(t, meminfoRealSample, "MemTotal", "MemTotal: 100 kB")
	content = meminfoWith(t, content, "MemFree", "MemFree: 50 kB")
	content = meminfoWith(t, content, "MemAvailable", "MemAvailable: 300 kB")
	c := &MemoryCollector{path: writeTempFile(t, t.TempDir(), "meminfo", content)}
	mem, swap, err := c.Collect()
	want := "MemAvailable exceeds MemTotal in /proc/meminfo content"
	if err == nil || err.Error() != want {
		t.Fatalf("AC5: Collect error = %v, want %q", err, want)
	}
	if mem != (Memory{}) {
		t.Errorf("AC5: Memory = %+v, want zero", mem)
	}
	if swap != (Swap{}) {
		t.Errorf("AC5: Swap = %+v, want zero", swap)
	}
}

func TestParseMeminfo_ByteRange_AC6(t *testing.T) {
	const over = "18014398509481984"
	fields := []string{"MemTotal", "MemFree", "MemAvailable", "SwapTotal", "SwapFree"}
	for _, key := range fields {
		t.Run("AC6 "+key, func(t *testing.T) {
			content := meminfoWith(t, meminfoRealSample, key, key+": "+over+" kB")
			wantMeminfoError(t, "AC6", content, key+" exceeds the byte range in /proc/meminfo content")
		})
	}
	t.Run("AC6 first field in order", func(t *testing.T) {
		content := meminfoWith(t, meminfoRealSample, "MemFree", "MemFree: "+over+" kB")
		content = meminfoWith(t, content, "SwapFree", "SwapFree: "+over+" kB")
		wantMeminfoError(t, "AC6", content, "MemFree exceeds the byte range in /proc/meminfo content")
	})
}
