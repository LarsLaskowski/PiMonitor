package collector

import (
	"context"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// These tests exercise the real Linux metric sources (/proc, /sys) since
// the orchestrator's job is wiring, not reimplementing parser logic
// (already covered by each metric's own unit tests). They are expected to
// run in a Linux CI environment; a missing thermal zone (common in
// containers) is tolerated gracefully rather than failing the test.

func newTestCollector() *Collector {
	return New(Config{
		FastInterval:          time.Second,
		SlowInterval:          time.Minute,
		HistoryCapacity:       10,
		NetworkEnabled:        true,
		UpdatesStaleThreshold: time.Hour,
		DistroInfoEnabled:     true,
		PiModelEnabled:        true,
	}, nil)
}

func TestCollector_FastTick_PopulatesSnapshot(t *testing.T) {
	c := newTestCollector()
	ctx := context.Background()

	c.collectSysInfo()
	c.fastTick(ctx)

	snap := c.Snapshot()
	if snap.Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp after fastTick")
	}
	if snap.Memory.TotalBytes == 0 {
		t.Fatal("expected non-zero MemTotal after fastTick")
	}
	if snap.System.KernelVersion == "" {
		t.Fatal("expected KernelVersion to be populated by collectSysInfo")
	}
}

func TestCollector_FastTick_BuildsHistory(t *testing.T) {
	c := newTestCollector()
	ctx := context.Background()

	c.collectSysInfo()
	c.fastTick(ctx)
	c.fastTick(ctx)

	hist := c.History()
	if len(hist.MemoryUsedPercent) != 2 {
		t.Fatalf("expected 2 memory history points after 2 ticks, got %d", len(hist.MemoryUsedPercent))
	}
	if len(hist.Load1) != 2 {
		t.Fatalf("expected 2 load1 history points after 2 ticks, got %d", len(hist.Load1))
	}
}

func TestCollector_CollectSysInfo_TogglesDistroAndPiModel(t *testing.T) {
	c := New(Config{
		FastInterval:      time.Second,
		SlowInterval:      time.Minute,
		HistoryCapacity:   10,
		DistroInfoEnabled: false,
		PiModelEnabled:    false,
	}, nil)

	c.collectSysInfo()

	snap := c.Snapshot()
	if snap.System.Distribution != "" {
		t.Fatalf("expected Distribution to be cleared when disabled, got %q", snap.System.Distribution)
	}
	if snap.System.PiModel != "" {
		t.Fatalf("expected PiModel to be cleared when disabled, got %q", snap.System.PiModel)
	}
	if snap.System.KernelVersion == "" {
		t.Fatal("expected KernelVersion to remain populated regardless of toggles")
	}
}

func TestCollector_FastTick_NetworkDisabled(t *testing.T) {
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		NetworkEnabled:  false,
	}, nil)

	c.fastTick(context.Background())

	snap := c.Snapshot()
	if snap.Network != nil {
		t.Fatalf("expected no network data when disabled, got %+v", snap.Network)
	}
	hist := c.History()
	if hist.NetworkRxBytesPerSec != nil {
		t.Fatalf("expected no network history when disabled, got %+v", hist.NetworkRxBytesPerSec)
	}
}

func TestCollector_HistoryCapacityBounded(t *testing.T) {
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 3,
		NetworkEnabled:  false,
	}, nil)

	ctx := context.Background()
	for i := 0; i < 10; i++ {
		c.fastTick(ctx)
	}

	hist := c.History()
	if len(hist.MemoryUsedPercent) != 3 {
		t.Fatalf("expected history bounded to capacity 3, got %d", len(hist.MemoryUsedPercent))
	}
}

func TestCollector_Alerts_DisabledByDefault(t *testing.T) {
	c := newTestCollector()
	c.fastTick(context.Background())

	report := c.Alerts()
	if report.Enabled {
		t.Fatal("expected alerts to be disabled when AlertsEnabled is false")
	}
	if len(report.States) != 0 || len(report.Events) != 0 {
		t.Fatalf("expected empty report when disabled, got %+v", report)
	}
}

func TestCollector_Alerts_EvaluatedOnFastTick(t *testing.T) {
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		AlertsEnabled:   true,
		AlertFor:        0,
		// Zero thresholds mean every real reading classifies as crit, so the
		// wiring is observable regardless of the host's actual metrics.
		Thresholds: config.Thresholds{},
	}, nil)

	c.fastTick(context.Background())

	report := c.Alerts()
	if !report.Enabled {
		t.Fatal("expected alerts to be enabled")
	}
	// CPU, memory, and swap collection succeed on any Linux CI host, so all
	// three states must be present. (Temperature may be skipped when no
	// thermal zone is available, e.g. in a container, so it is not asserted
	// here.)
	var haveCPU, haveMemory, haveSwap bool
	for _, st := range report.States {
		switch st.Metric {
		case "cpu":
			haveCPU = true
		case "memory":
			haveMemory = true
		case "swap":
			haveSwap = true
		}
	}
	if !haveCPU || !haveMemory || !haveSwap {
		t.Fatalf("expected cpu, memory, and swap alert states, got %+v", report.States)
	}
}

func TestCollector_Run_StopsOnContextCancel(t *testing.T) {
	c := New(Config{
		FastInterval:    10 * time.Millisecond,
		SlowInterval:    time.Hour,
		HistoryCapacity: 10,
		NetworkEnabled:  false,
	}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not stop after context cancellation")
	}

	if c.Snapshot().Timestamp.IsZero() {
		t.Fatal("expected at least one tick to have run before cancellation")
	}
}
