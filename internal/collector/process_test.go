package collector

import (
	"encoding/json"
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
	if stat.startTimeTicks != 12345 {
		t.Fatalf("startTimeTicks = %d, want 12345", stat.startTimeTicks)
	}
}

// TestParseProcPidStat_NameWithSpacesAndParens covers a process name that
// itself contains spaces and parentheses (e.g. a script renamed via
// prctl/argv[0] to something like "(sd-pam)" or "my (weird) app"), which a
// naive whitespace split on the whole line would misparse. The name must
// be extracted between the first "(" and the last ")".
func TestParseProcPidStat_NameWithSpacesAndParens(t *testing.T) {
	stat, err := parseProcPidStat("42 (my (weird) app.sh) S 1 42 42 0 -1 0 0 0 0 0 20 8 0 0 20 0 1 0 999 0 0\n")
	if err != nil {
		t.Fatalf("parseProcPidStat: %v", err)
	}
	if stat.comm != "my (weird) app.sh" {
		t.Fatalf("comm = %q, want %q", stat.comm, "my (weird) app.sh")
	}
	if stat.utime != 20 || stat.stime != 8 {
		t.Fatalf("utime/stime = %d/%d, want 20/8", stat.utime, stat.stime)
	}
	if stat.startTimeTicks != 999 {
		t.Fatalf("startTimeTicks = %d, want 999", stat.startTimeTicks)
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
		{"enough fields for utime/stime but not starttime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0\n"},
		{"non-numeric utime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 abc 5 0 0 20 0 1 0 999 0 0\n"},
		{"non-numeric stime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 10 abc 0 0 20 0 1 0 999 0 0\n"},
		{"non-numeric starttime", "1234 (bash) S 1 1234 1234 0 -1 0 0 0 0 0 10 5 0 0 20 0 1 0 abc 0 0\n"},
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
	writeFakeProcess(t, root, 100, fakeProcess{comm: "big", utime: 1000, stime: 200, rssKB: 51200, startTime: 1000})
	writeFakeProcess(t, root, 200, fakeProcess{comm: "small", utime: 500, stime: 100, rssKB: 10240, startTime: 1000})

	c := &ProcessCollector{root: root, now: time.Now}

	procs, err := c.Collect(10, 1)
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
// known interval apart. coreCount 1 keeps CPUPercent equal to raw (non
// core-normalized) usage, so the expected percentages are simple.
func TestProcessCollector_Collect_TopNOrdering(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, fakeProcess{comm: "cpu-hog", utime: 1000, stime: 200, rssKB: 51200, startTime: 1000}) // 60% CPU, 50MB
	writeFakeProcess(t, root, 200, fakeProcess{comm: "mem-hog", utime: 500, stime: 100, rssKB: 102400, startTime: 1000}) // 35% CPU, 100MB
	writeFakeProcess(t, root, 300, fakeProcess{comm: "idle", utime: 100, stime: 50, rssKB: 10240, startTime: 1000})      // 7% CPU, 10MB

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &ProcessCollector{root: root, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(10, 1); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, filepath.Join(root, "100", "stat"), fakeProcPidStatLine(100, "cpu-hog", 1500, 300, 1000))
	overwriteTempFile(t, filepath.Join(root, "200", "stat"), fakeProcPidStatLine(200, "mem-hog", 800, 150, 1000))
	overwriteTempFile(t, filepath.Join(root, "300", "stat"), fakeProcPidStatLine(300, "idle", 150, 70, 1000))
	fakeNow = fakeNow.Add(10 * time.Second)

	procs, err := c.Collect(2, 1)
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

// TestProcessCollector_Collect_NormalizesByCoreCount covers the fix for
// cpu_percent being reported `top`-style per-core (able to exceed 100% on
// multi-core hardware) rather than normalized against total CPU capacity
// like Snapshot.CPU.OverallPercent: the same fixture that yields 60% at
// coreCount 1 must yield 15% at coreCount 4.
func TestProcessCollector_Collect_NormalizesByCoreCount(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, fakeProcess{comm: "cpu-hog", utime: 1000, stime: 200, rssKB: 51200, startTime: 1000})

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &ProcessCollector{root: root, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(10, 4); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, filepath.Join(root, "100", "stat"), fakeProcPidStatLine(100, "cpu-hog", 1500, 300, 1000))
	fakeNow = fakeNow.Add(10 * time.Second)

	procs, err := c.Collect(10, 4)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || diffFloat(procs.ByCPU[0].CPUPercent, 15) > 0.01 {
		t.Fatalf("ByCPU = %+v, want pid 100 at 15%% (60%% / 4 cores)", procs.ByCPU)
	}
}

// TestProcessCollector_Collect_NormalizesByCoreCount_TreatsBelowOneAsOne
// covers the guard against a bogus coreCount (0, or negative from a caller
// bug): it must not divide by zero or invert the sign, but behave like
// coreCount 1.
func TestProcessCollector_Collect_NormalizesByCoreCount_TreatsBelowOneAsOne(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, fakeProcess{comm: "cpu-hog", utime: 1000, stime: 200, rssKB: 51200, startTime: 1000})

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &ProcessCollector{root: root, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(10, 0); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	overwriteTempFile(t, filepath.Join(root, "100", "stat"), fakeProcPidStatLine(100, "cpu-hog", 1500, 300, 1000))
	fakeNow = fakeNow.Add(10 * time.Second)

	procs, err := c.Collect(10, 0)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || diffFloat(procs.ByCPU[0].CPUPercent, 60) > 0.01 {
		t.Fatalf("ByCPU = %+v, want pid 100 at 60%% (coreCount 0 treated as 1)", procs.ByCPU)
	}
}

// TestProcessCollector_Collect_DetectsPIDReuse covers the fix for a pid
// recycled between two Collect calls: without a process-identity check, a
// new occupant of pid 100 whose utime/stime happen to be numerically
// greater than the previous occupant's last sample would pass the
// monotonic-increase check and be attributed a bogus CPU delta belonging to
// a different process entirely. /proc/<pid>/stat's starttime is fixed for
// the life of a process, so a changed starttime must reset the pid to "no
// prior sample" (0% CPU), the same as a genuinely new process.
func TestProcessCollector_Collect_DetectsPIDReuse(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, fakeProcess{comm: "original", utime: 100, stime: 50, rssKB: 1024, startTime: 1000}) // total 150 ticks, starttime 1000

	fakeNow := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := &ProcessCollector{root: root, now: func() time.Time { return fakeNow }}
	if _, err := c.Collect(10, 1); err != nil {
		t.Fatalf("first Collect: %v", err)
	}

	// pid 100 exits and a different process is started, reusing the pid.
	// Its utime/stime (600 ticks total) are numerically greater than the
	// previous occupant's, and its starttime (2000) differs.
	overwriteTempFile(t, filepath.Join(root, "100", "stat"), fakeProcPidStatLine(100, "reused", 500, 100, 2000))
	fakeNow = fakeNow.Add(10 * time.Second)

	procs, err := c.Collect(10, 1)
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || procs.ByCPU[0].CPUPercent != 0 {
		t.Fatalf("ByCPU = %+v, want pid 100 (reused, different starttime) at 0%% CPU, not a bogus delta", procs.ByCPU)
	}
}

// TestProcessCollector_Collect_TopNClampedToAvailableProcesses covers
// requesting more processes than exist: the result must not be padded with
// zero-value entries.
func TestProcessCollector_Collect_TopNClampedToAvailableProcesses(t *testing.T) {
	root := t.TempDir()
	writeFakeProcess(t, root, 100, fakeProcess{comm: "only", utime: 100, stime: 50, rssKB: 1024, startTime: 1000})

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(50, 1)
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
	writeFakeProcess(t, root, 100, fakeProcess{comm: "only", utime: 100, stime: 50, rssKB: 1024, startTime: 1000})

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(0, 1)
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
	writeFakeProcess(t, root, 100, fakeProcess{comm: "healthy", utime: 100, stime: 50, rssKB: 1024, startTime: 1000})
	// pid 200 has a stat file but no status file, simulating a process that
	// exited between the two reads.
	if err := os.MkdirAll(filepath.Join(root, "200"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeTempFile(t, filepath.Join(root, "200"), "stat", fakeProcPidStatLine(200, "vanished", 100, 50, 1000))
	// A non-pid entry (e.g. "self") must be ignored, not treated as an error.
	writeTempFile(t, root, "self", "not a pid")

	c := &ProcessCollector{root: root, now: time.Now}
	procs, err := c.Collect(10, 1)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(procs.ByCPU) != 1 || procs.ByCPU[0].PID != 100 {
		t.Fatalf("ByCPU = %+v, want only pid 100", procs.ByCPU)
	}
}
