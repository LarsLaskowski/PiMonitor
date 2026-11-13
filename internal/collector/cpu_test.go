package collector

import (
	"os"
	"path/filepath"
	"strings"
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

// procStatNoAggregate is procStatFixture1 without its aggregate cpu line.
// No kernel prints such content, so the fixture is hand-typed.
const procStatNoAggregate = `cpu0 50 0 50 350 0 0 0 0 0 0
cpu1 50 0 50 350 0 0 0 0 0 0
intr 12345
ctxt 6789
`

// procStatShortAggregate has an aggregate line with seven counters, which
// parseProcStat skips, and valid cpuN lines.
const procStatShortAggregate = `cpu  100 0 100 700 0 0 0
cpu0 50 0 50 350 0 0 0 0 0 0
cpu1 50 0 50 350 0 0 0 0 0 0
intr 12345
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

func TestParseProcStat_SevenCounters(t *testing.T) {
	t.Run("AC1 only cpu line has seven counters: error, no panic", func(t *testing.T) {
		if _, err := parseProcStat("cpu  100 0 100 700 0 0 0\n"); err == nil {
			t.Fatal("got nil error, want error for a cpu line with seven counters")
		}
	})
	t.Run("AC2 cpuN line with seven counters is skipped, eight counters parse", func(t *testing.T) {
		lines, err := parseProcStat("cpu  100 0 100 700 0 0 0 0\ncpu0 50 0 50 350 0 0 0\ncpu1 50 0 50 350 0 0 0 0\n")
		if err != nil {
			t.Fatalf("parseProcStat: got error %v, want nil", err)
		}
		if len(lines) != 2 || lines[0].name != "cpu" || lines[1].name != "cpu1" {
			t.Fatalf("got %d lines %+v, want cpu and cpu1", len(lines), lines)
		}
	})
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

	t.Run("AC5 no aggregate line", func(t *testing.T) {
		c := &CPUCollector{path: writeStatFile(t, procStatNoAggregate)}
		count, err := c.CoreCount()
		if err != nil {
			t.Fatalf("CoreCount: got error %v, want nil", err)
		}
		if count != 2 {
			t.Fatalf("CoreCount = %d, want 2", count)
		}
	})
}

// writeStatFile writes content to a new file in a temporary directory and
// returns its path.
func writeStatFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stat")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write stat file: %v", err)
	}
	return path
}

// rewriteStatFile replaces the content of the file at path.
func rewriteStatFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("rewrite stat file: %v", err)
	}
}

const noAggregateErrText = "no aggregate cpu line in /proc/stat content"

// checkCollectFailed fails the test unless got is a zero CPUUsage and err
// is the missing-aggregate-line error.
func checkCollectFailed(t *testing.T, got CPUUsage, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("Collect error = nil, want one containing %q", noAggregateErrText)
	}
	if !strings.Contains(err.Error(), noAggregateErrText) {
		t.Errorf("Collect error = %q, want it to contain %q", err.Error(), noAggregateErrText)
	}
	if got.OverallPercent != 0 {
		t.Errorf("OverallPercent = %v, want 0", got.OverallPercent)
	}
	if got.PerCorePercent != nil {
		t.Errorf("PerCorePercent = %v, want nil", got.PerCorePercent)
	}
}

func TestCPUCollector_Collect_MissingAggregateLine(t *testing.T) {
	t.Run("AC1 first call", func(t *testing.T) {
		c := &CPUCollector{path: writeStatFile(t, procStatNoAggregate)}
		got, err := c.Collect()
		checkCollectFailed(t, got, err)
	})
	t.Run("AC1 after a valid sample", func(t *testing.T) {
		path := writeStatFile(t, procStatFixture1)
		c := &CPUCollector{path: path}
		if _, err := c.Collect(); err != nil {
			t.Fatalf("first Collect: %v", err)
		}
		rewriteStatFile(t, path, procStatNoAggregate)
		got, err := c.Collect()
		checkCollectFailed(t, got, err)
	})
	t.Run("AC2 aggregate line with seven counters", func(t *testing.T) {
		path := writeStatFile(t, procStatFixture1)
		c := &CPUCollector{path: path}
		if _, err := c.Collect(); err != nil {
			t.Fatalf("first Collect: %v", err)
		}
		rewriteStatFile(t, path, procStatShortAggregate)
		got, err := c.Collect()
		checkCollectFailed(t, got, err)

		fresh := &CPUCollector{path: path}
		got, err = fresh.Collect()
		checkCollectFailed(t, got, err)
	})
}

func TestCPUCollector_Collect_FailedReadKeepsPreviousSample_AC3(t *testing.T) {
	path := writeStatFile(t, procStatFixture1)
	c := &CPUCollector{path: path}
	if _, err := c.Collect(); err != nil {
		t.Fatalf("collect sample A: %v", err)
	}
	rewriteStatFile(t, path, procStatNoAggregate)
	if _, err := c.Collect(); err == nil {
		t.Fatal("collect without aggregate line: got nil error, want an error")
	}
	rewriteStatFile(t, path, procStatFixture2)
	got, err := c.Collect()
	if err != nil {
		t.Fatalf("collect sample B: %v", err)
	}
	wantOverall := (1 - float64(100)/float64(300)) * 100
	if diffFloat(got.OverallPercent, wantOverall) > 0.01 {
		t.Errorf("OverallPercent = %v, want %v (A to B delta)", got.OverallPercent, wantOverall)
	}
	if len(got.PerCorePercent) != 2 {
		t.Fatalf("PerCorePercent has %d values, want 2", len(got.PerCorePercent))
	}
	wantCore := (1 - float64(50)/float64(150)) * 100
	for i, v := range got.PerCorePercent {
		if diffFloat(v, wantCore) > 0.01 {
			t.Errorf("PerCorePercent[%d] = %v, want %v", i, v, wantCore)
		}
	}
}

func diffFloat(a, b float64) float64 {
	if a > b {
		return a - b
	}
	return b - a
}
