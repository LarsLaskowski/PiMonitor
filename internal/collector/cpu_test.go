package collector

import (
	"os"
	"path/filepath"
	"testing"
)

const procStatFixture1 = `cpu  100 0 100 700 0 0 0 0 0 0
cpu0 50 0 50 350 0 0 0 0 0 0
cpu1 50 0 50 350 0 0 0 0 0 0
intr 12345
ctxt 6789
`

const procStatFixture2 = `cpu  200 0 200 800 0 0 0 0 0 0
cpu0 100 0 100 400 0 0 0 0 0 0
cpu1 100 0 100 400 0 0 0 0 0 0
intr 12400
ctxt 6800
`

func TestParseProcStat(t *testing.T) {
	lines, err := parseProcStat(procStatFixture1)
	if err != nil {
		t.Fatalf("parseProcStat: %v", err)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 cpu lines, got %d", len(lines))
	}
	if lines[0].name != "cpu" {
		t.Fatalf("expected first line to be aggregate 'cpu', got %q", lines[0].name)
	}
}

func TestParseProcStat_NoCPULines(t *testing.T) {
	_, err := parseProcStat("intr 123\nctxt 456\n")
	if err == nil {
		t.Fatal("expected error for input with no cpu lines")
	}
}

func TestParseProcStat_MalformedField(t *testing.T) {
	_, err := parseProcStat("cpu  abc 0 100 700 0 0 0 0 0 0\n")
	if err == nil {
		t.Fatal("expected error for non-numeric field")
	}
}

func TestUsagePercent(t *testing.T) {
	prev, err := parseProcStat(procStatFixture1)
	if err != nil {
		t.Fatalf("parseProcStat prev: %v", err)
	}
	cur, err := parseProcStat(procStatFixture2)
	if err != nil {
		t.Fatalf("parseProcStat cur: %v", err)
	}

	// Deltas: total 800->1000 = +200 real delta compare (1000-800=200... let's recompute)
	// prev aggregate: total=100+0+100+700=900, idle=700
	// cur aggregate:  total=200+0+200+800=1200, idle=800
	// totalDelta=300, idleDelta=100 -> busy=200/300=66.67%
	got := usagePercent(prev[0], cur[0])
	want := (1 - float64(100)/float64(300)) * 100
	if diffFloat(got, want) > 0.01 {
		t.Fatalf("usagePercent = %v, want %v", got, want)
	}
}

func TestUsagePercent_NoDelta(t *testing.T) {
	prev, _ := parseProcStat(procStatFixture1)
	got := usagePercent(prev[0], prev[0])
	if got != 0 {
		t.Fatalf("usagePercent with identical samples = %v, want 0", got)
	}
}

func TestCPUCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	if err := os.WriteFile(path, []byte(procStatFixture1), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	c := &CPUCollector{path: path}

	first, err := c.Collect()
	if err != nil {
		t.Fatalf("first Collect: %v", err)
	}
	if first.OverallPercent != 0 {
		t.Fatalf("first Collect should report 0%% (no prior sample), got %v", first.OverallPercent)
	}

	if err := os.WriteFile(path, []byte(procStatFixture2), 0o644); err != nil {
		t.Fatalf("write fixture2: %v", err)
	}

	second, err := c.Collect()
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if second.OverallPercent <= 0 {
		t.Fatalf("second Collect should report >0%% usage, got %v", second.OverallPercent)
	}
	if len(second.PerCorePercent) != 2 {
		t.Fatalf("expected 2 per-core values, got %d", len(second.PerCorePercent))
	}
}

func TestCPUCollector_CoreCount(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stat")
	if err := os.WriteFile(path, []byte(procStatFixture1), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := &CPUCollector{path: path}
	count, err := c.CoreCount()
	if err != nil {
		t.Fatalf("CoreCount: %v", err)
	}
	if count != 2 {
		t.Fatalf("CoreCount = %d, want 2", count)
	}
}

func diffFloat(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
