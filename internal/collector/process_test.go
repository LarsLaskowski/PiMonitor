package collector

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseProcPidStat(t *testing.T) {
	stat, err := parseProcPidStat("1234 (bash) S 1 1234 1234 0 -1 4194304 100 0 0 0 10 5 0 0 20 0 1 0 12345 123456 100 0 0\n")
	if err != nil {
		t.Fatalf("parseProcPidStat: %v", err)
	}
	if stat.comm != "bash" {
		t.Fatalf("comm = %q, want %q", stat.comm, "bash")
	}
	if stat.utime != 10 || stat.stime != 5 {
		t.Fatalf("utime/stime = %d/%d, want 10/5", stat.utime, stat.stime)
	}
}

// TestParseProcPidStat_NameWithSpacesAndParens covers a process name that
// itself contains spaces and parentheses (e.g. a script renamed via
// prctl/argv[0] to something like "(sd-pam)" or "my (weird) app"), which a
// naive whitespace split on the whole line would misparse. The name must
// be extracted between the first "(" and the last ")".
func TestParseProcPidStat_NameWithSpacesAndParens(t *testing.T) {
	stat, err := parseProcPidStat("42 (my (weird) app.sh) S 1 42 42 0 -1 0 0 0 0 0 20 8 0 0 20 0 1 0 0 0 0\n")
	if err != nil {
		t.Fatalf("parseProcPidStat: %v", err)
	}
	if stat.comm != "my (weird) app.sh" {
		t.Fatalf("comm = %q, want %q", stat.comm, "my (weird) app.sh")
	}
	if stat.utime != 20 || stat.stime != 8 {
		t.Fatalf("utime/stime = %d/%d, want 20/8", stat.utime, stat.stime)
	}
}

func TestParseProcPidStat_MalformedContent(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{"no parens", "1234 bash S 1 1234 1234\n"},
		{"unbalanced parens", "1234 (bash S 1 1234 1234\n"},
		{"too few fields after comm", "1234 (bash) S 1 2\n"},
		{"non-numeric utime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 abc 5 0 0 20 0 1 0 0 0 0\n"},
		{"non-numeric stime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 10 abc 0 0 20 0 1 0 0 0 0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseProcPidStat(tt.data); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestParseProcPidStatusRSS(t *testing.T) {
	rss, err := parseProcPidStatusRSS("Name:\tbash\nVmRSS:\t   1234 kB\nVmSize:\t5000 kB\n")
	if err != nil {
		t.Fatalf("parseProcPidStatusRSS: %v", err)
	}
	if rss != 1234*1024 {
		t.Fatalf("rss = %d, want %d", rss, 1234*1024)
	}
}

// TestParseProcPidStatusRSS_NoVmRSSLine covers a kernel thread, which has
// no resident memory of its own and therefore no VmRSS line in its status
// file: this must report 0 rather than an error.
func TestParseProcPidStatusRSS_NoVmRSSLine(t *testing.T) {
	rss, err := parseProcPidStatusRSS("Name:\tkworker/0:1\nState:\tS\n")
	if err != nil {
		t.Fatalf("parseProcPidStatusRSS: %v", err)
	}
	if rss != 0 {
		t.Fatalf("rss = %d, want 0", rss)
	}
}

func TestParseProcPidStatusRSS_MalformedLine(t *testing.T) {
	if _, err := parseProcPidStatusRSS("VmRSS:\tnot-a-number kB\n"); err == nil {
		t.Fatal("expected error for non-numeric VmRSS value")
	}
}

// TestProcessCollector_Collect_FirstCallHasNoCPUButHasMemory covers the
// documented first-Collect behavior: there is no prior sample to diff CPU
// usage against, so every process reports 0% CPU, but RSS-based ranking is
// meaningful immediately.
func TestProcessCollector_Collect_FirstCallHasNoCPUButHasMemory(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, "big", 1000, 200, 51200)
	writeFakeProcess(t, root, 200, "small", 500, 100, 10240)

	c := &ProcessCollector{root: root, now: time.Now}

	procs, err := c.Collect(10)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, p := range procs.ByCPU {
		if p.CPUPercent != 0 {
			t.Fatalf("first Collect should report 0%% CPU for pid %d, got %v", p.PID, p.CPUPercent)
		}
	}
	if len(procs.ByMemory) != 2 || procs.ByMemory[0].PID != 100 || procs.ByMemory[1].PID != 200 {
		t.Fatalf("ByMemory = %+v, want pid 100 (50MB) before pid 200 (10MB)", procs.ByMemory)
	}
}

// TestProcessCollector_Collect_TopNOrdering is the acceptance test for
// issue #16: a fixture pid set produces the correct top-N ordering by CPU
// and by memory, computed independently of each other from two samples a
// known interval apart.
func TestProcessCollector_Collect_TopNOrdering(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, "cpu-hog", 1000, 200, 51200) // 60% CPU, 50MB
	writeFakeProcess(t, root, 200, "mem-hog", 500, 100, 102400) // 35% CPU, 100MB
	writeFakeProcess(t, root, 300, "idle", 100, 50, 10240)      // 7% CPU, 10MB

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &ProcessCollector{root: root, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(10); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, filepath.Join(root, "100", "stat"), fakeStatLine(100, "cpu-hog", 1500, 300))
	overwriteTempFile(t, filepath.Join(root, "200", "stat"), fakeStatLine(200, "mem-hog", 800, 150))
	overwriteTempFile(t, filepath.Join(root, "300", "stat"), fakeStatLine(300, "idle", 150, 70))
	fakeNow = fakeNow.Add(10 * time.Second)

	procs, err := c.Collect(2)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}

	if len(procs.ByCPU) != 2 {
		t.Fatalf("ByCPU has %d entries, want 2 (topN)", len(procs.ByCPU))
	}
	if procs.ByCPU[0].PID != 100 || diffFloat(procs.ByCPU[0].CPUPercent, 60) > 0.01 {
		t.Fatalf("ByCPU[0] = %+v, want pid 100 at 60%%", procs.ByCPU[0])
	}
	if procs.ByCPU[1].PID != 200 || diffFloat(procs.ByCPU[1].CPUPercent, 35) > 0.01 {
		t.Fatalf("ByCPU[1] = %+v, want pid 200 at 35%%", procs.ByCPU[1])
	}

	if len(procs.ByMemory) != 2 {
		t.Fatalf("ByMemory has %d entries, want 2 (topN)", len(procs.ByMemory))
	}
	if procs.ByMemory[0].PID != 200 || procs.ByMemory[0].RSSBytes != 102400*1024 {
		t.Fatalf("ByMemory[0] = %+v, want pid 200 at 100MB", procs.ByMemory[0])
	}
	if procs.ByMemory[1].PID != 100 || procs.ByMemory[1].RSSBytes != 51200*1024 {
		t.Fatalf("ByMemory[1] = %+v, want pid 100 at 50MB", procs.ByMemory[1])
	}
}

// TestProcessCollector_Collect_TopNClampedToAvailableProcesses covers
// requesting more processes than exist: the result must not be padded with
// zero-value entries.
func TestProcessCollector_Collect_TopNClampedToAvailableProcesses(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, "only", 100, 50, 1024)

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(50)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || len(procs.ByMemory) != 1 {
		t.Fatalf("expected exactly 1 entry per ranking, got ByCPU=%d ByMemory=%d", len(procs.ByCPU), len(procs.ByMemory))
	}
}

// TestProcessCollector_Collect_TopZeroReturnsEmptyNotNil pins the JSON
// shape GET /api/v1/processes must serve when topN is 0: [] in both
// rankings, never null.
func TestProcessCollector_Collect_TopZeroReturnsEmptyNotNil(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, "only", 100, 50, 1024)

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(0)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if procs.ByCPU == nil || procs.ByMemory == nil {
		t.Fatalf("expected non-nil empty slices, got %+v", procs)
	}
	b, err := json.Marshal(procs)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	want := `{"by_cpu":[],"by_memory":[]}`
	if string(b) != want {
		t.Fatalf("json = %s, want %s", b, want)
	}
}

// TestProcessCollector_Collect_SkipsUnreadableProcesses covers a process
// that exits (or was never fully readable, e.g. a zombie) between the
// directory listing and reading its files: it must be skipped rather than
// failing the whole collection.
func TestProcessCollector_Collect_SkipsUnreadableProcesses(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, "healthy", 100, 50, 1024)
	// pid 200 has a stat file but no status file, simulating a process that
	// exited between the two reads.
	if err := os.MkdirAll(filepath.Join(root, "200"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTempFile(t, filepath.Join(root, "200"), "stat", fakeStatLine(200, "vanished", 100, 50))
	// A non-pid entry (e.g. "self") must be ignored, not treated as an error.
	writeTempFile(t, root, "self", "not a pid")

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(10)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || procs.ByCPU[0].PID != 100 {
		t.Fatalf("ByCPU = %+v, want only pid 100", procs.ByCPU)
	}
}

// fakeStatLine renders a synthetic /proc/<pid>/stat line with the given
// utime/stime, matching the layout writeFakeProcess writes.
func fakeStatLine(pid int, comm string, utime, stime uint64) string {
	return fmt.Sprintf("%d (%s) S 1 1 1 0 -1 0 0 0 0 0 %d %d 0 0 20 0 1 0 0 0 0\n", pid, comm, utime, stime)
}
