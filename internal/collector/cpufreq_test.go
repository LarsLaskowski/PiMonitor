package collector

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// writeCPUFreqCore creates a fake /sys/devices/system/cpu/cpuN/cpufreq
// directory under root with the given scaling_cur_freq (kHz) and
// scaling_governor content.
func writeCPUFreqCore(t *testing.T, root string, core int, curFreqKHz, governor string) {
	t.Helper()
	dir := filepath.Join(root, "cpu"+strconv.Itoa(core), "cpufreq")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scaling_cur_freq"), []byte(curFreqKHz+"\n"), 0o644); err != nil {
		t.Fatalf("write scaling_cur_freq: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scaling_governor"), []byte(governor+"\n"), 0o644); err != nil {
		t.Fatalf("write scaling_governor: %v", err)
	}
}

func TestCPUFreqCollector_Collect(t *testing.T) {
	root := t.TempDir()
	writeCPUFreqCore(t, root, 0, "600000", "ondemand")
	writeCPUFreqCore(t, root, 1, "1500000", "performance")

	c := &CPUFreqCollector{glob: filepath.Join(root, "cpu[0-9]*", "cpufreq")}
	freqs, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(freqs) != 2 {
		t.Fatalf("expected 2 core readings, got %d: %+v", len(freqs), freqs)
	}

	want := []CPUCoreFrequency{
		{Core: 0, MHz: 600, Governor: "ondemand"},
		{Core: 1, MHz: 1500, Governor: "performance"},
	}
	for i, w := range want {
		if freqs[i] != w {
			t.Fatalf("freqs[%d] = %+v, want %+v", i, freqs[i], w)
		}
	}
}

func TestCPUFreqCollector_Collect_SortedByCoreIndex(t *testing.T) {
	root := t.TempDir()
	// Written out of order to exercise the sort.
	writeCPUFreqCore(t, root, 3, "800000", "schedutil")
	writeCPUFreqCore(t, root, 1, "1200000", "schedutil")
	writeCPUFreqCore(t, root, 2, "900000", "schedutil")
	writeCPUFreqCore(t, root, 0, "700000", "schedutil")

	c := &CPUFreqCollector{glob: filepath.Join(root, "cpu[0-9]*", "cpufreq")}
	freqs, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(freqs) != 4 {
		t.Fatalf("expected 4 core readings, got %d", len(freqs))
	}
	for i, f := range freqs {
		if f.Core != i {
			t.Fatalf("freqs[%d].Core = %d, want %d (expected sorted order)", i, f.Core, i)
		}
	}
}

func TestCPUFreqCollector_Collect_NoDriver(t *testing.T) {
	root := t.TempDir()
	c := &CPUFreqCollector{glob: filepath.Join(root, "cpu[0-9]*", "cpufreq")}
	freqs, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(freqs) != 0 {
		t.Fatalf("expected no readings without a cpufreq driver, got %+v", freqs)
	}
}

func TestCPUFreqCollector_Collect_SkipsIncompleteCore(t *testing.T) {
	root := t.TempDir()
	writeCPUFreqCore(t, root, 0, "600000", "ondemand")

	// A core directory missing scaling_governor (e.g. a driver that only
	// exposes frequency) must be skipped rather than failing the call.
	incomplete := filepath.Join(root, "cpu1", "cpufreq")
	if err := os.MkdirAll(incomplete, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(incomplete, "scaling_cur_freq"), []byte("1000000\n"), 0o644); err != nil {
		t.Fatalf("write scaling_cur_freq: %v", err)
	}

	c := &CPUFreqCollector{glob: filepath.Join(root, "cpu[0-9]*", "cpufreq")}
	freqs, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(freqs) != 1 || freqs[0].Core != 0 {
		t.Fatalf("expected only core 0, got %+v", freqs)
	}
}

func TestReadCPUCoreFrequency_MalformedFreq(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "cpu0", "cpufreq")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scaling_cur_freq"), []byte("notanumber\n"), 0o644); err != nil {
		t.Fatalf("write scaling_cur_freq: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "scaling_governor"), []byte("ondemand\n"), 0o644); err != nil {
		t.Fatalf("write scaling_governor: %v", err)
	}

	if _, err := readCPUCoreFrequency(dir); err == nil {
		t.Fatal("expected error for malformed scaling_cur_freq")
	}
}

func TestCoreIndexFromCPUFreqDir(t *testing.T) {
	got, err := coreIndexFromCPUFreqDir("/sys/devices/system/cpu/cpu7/cpufreq")
	if err != nil {
		t.Fatalf("coreIndexFromCPUFreqDir: %v", err)
	}
	if got != 7 {
		t.Fatalf("core index = %d, want 7", got)
	}
}

func TestCoreIndexFromCPUFreqDir_Malformed(t *testing.T) {
	if _, err := coreIndexFromCPUFreqDir("/sys/devices/system/cpu/cpufreq"); err == nil {
		t.Fatal("expected error for a directory without a cpuN parent")
	}
}
