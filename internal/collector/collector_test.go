package collector

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
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

// TestCollector_Disks_NeverMarshalsAsNull covers issue #70: docs/API.md
// documents disks as reading as an empty array before the first fast tick,
// but a nil []Disk marshals to JSON null, not []. Snapshot.Disks must stay
// a non-nil (possibly empty) slice both before the first tick and after.
func TestCollector_Disks_NeverMarshalsAsNull(t *testing.T) {
	c := newTestCollector()

	assertDisksMarshalsAsEmptyArray := func(t *testing.T, snap Snapshot) {
		t.Helper()
		if snap.Disks == nil {
			t.Fatal("expected Snapshot.Disks to be non-nil")
		}
		b, err := json.Marshal(snap)
		if err != nil {
			t.Fatalf("json.Marshal: %v", err)
		}
		var raw map[string]json.RawMessage
		if err := json.Unmarshal(b, &raw); err != nil {
			t.Fatalf("json.Unmarshal: %v", err)
		}
		if got := string(raw["disks"]); got == "null" {
			t.Fatalf(`expected "disks" to marshal as [], got %s`, got)
		}
	}

	// Before the first fast tick completes.
	assertDisksMarshalsAsEmptyArray(t, c.Snapshot())

	// After a tick.
	c.fastTick(context.Background())
	assertDisksMarshalsAsEmptyArray(t, c.Snapshot())
}

// TestCollector_FastTick_DiskCollectionErrorYieldsEmptyNotNilDisks covers
// the failed-collection path directly: DiskCollector.Collect returns
// (nil, err) when /proc/mounts can't be read, and fastTick must still
// normalize that nil into a non-nil empty slice rather than storing it
// verbatim.
func TestCollector_FastTick_DiskCollectionErrorYieldsEmptyNotNilDisks(t *testing.T) {
	c := newTestCollector()
	c.disk = &DiskCollector{
		mountsPath:     "/nonexistent/proc/mounts",
		excludedFSType: defaultExcludedFSTypes,
	}

	c.fastTick(context.Background())

	snap := c.Snapshot()
	if snap.Disks == nil {
		t.Fatal("expected Snapshot.Disks to be non-nil even when disk collection fails")
	}
	if len(snap.Disks) != 0 {
		t.Fatalf("expected 0 disks after a failed collection, got %d", len(snap.Disks))
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

// TestCollector_Notifier_NotWiredWhenAlertsDisabled ensures a notifier built
// from configured webhooks is never wired in while the alert engine is
// disabled, since a disabled engine never produces events to deliver.
func TestCollector_Notifier_NotWiredWhenAlertsDisabled(t *testing.T) {
	notifier, err := alert.NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: "http://example.invalid/webhook"}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	if notifier == nil {
		t.Fatal("expected a non-nil notifier for a configured webhook")
	}

	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		AlertsEnabled:   false,
		Notifier:        notifier,
	}, nil)

	if c.notifier != nil {
		t.Fatal("expected notifier to stay unwired when AlertsEnabled is false")
	}
}

func TestEvictStaleSeries(t *testing.T) {
	const window = 3 * time.Second
	now := time.Unix(1_700_000_000, 0)

	tests := []struct {
		name        string
		present     bool
		age         time.Duration
		wantEvicted bool
	}{
		{"present device is kept regardless of sample age", true, 10 * window, false},
		{"absent device within the window is kept", false, window - time.Second, false},
		{"absent device exactly at the window boundary is kept", false, window, false},
		{"absent device past the window boundary is evicted", false, window + time.Nanosecond, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rb := NewRingBuffer[HistoryPoint](3)
			rb.Add(HistoryPoint{Timestamp: now.Add(-tt.age), Value: 1})
			hist := map[string]*RingBuffer[HistoryPoint]{"dev": rb}
			current := map[string]struct{}{}
			if tt.present {
				current["dev"] = struct{}{}
			}

			evictStaleSeries(hist, current, now, window)

			_, ok := hist["dev"]
			if ok == tt.wantEvicted {
				t.Fatalf("evictStaleSeries: entry present = %v, want evicted = %v", ok, tt.wantEvicted)
			}
		})
	}
}

func TestEvictStaleSeries_ZeroWindowDisablesEviction(t *testing.T) {
	rb := NewRingBuffer[HistoryPoint](3)
	rb.Add(HistoryPoint{Timestamp: time.Unix(0, 0), Value: 1})
	hist := map[string]*RingBuffer[HistoryPoint]{"dev": rb}

	evictStaleSeries(hist, map[string]struct{}{}, time.Now(), 0)

	if _, ok := hist["dev"]; !ok {
		t.Fatal("expected a non-positive window to disable eviction")
	}
}

// TestCollector_FastTick_EvictsStaleDeviceSeries drives eviction through the
// production code path (fastTick), rather than calling evictStaleSeries
// directly, so the wiring in fastTick itself — not just the helper — is
// under test.
func TestCollector_FastTick_EvictsStaleDeviceSeries(t *testing.T) {
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 3, // history window = 3s
		NetworkEnabled:  true,
	}, nil)

	// A mountpoint/interface name the host running this test can never
	// actually report, seeded with a sample far outside the history window.
	const goneDevice = "pimonitor-test-gone"
	stale := HistoryPoint{Timestamp: time.Now().Add(-time.Hour), Value: 1}
	for _, m := range []map[string]*RingBuffer[HistoryPoint]{c.diskHist, c.rxHist, c.txHist} {
		rb := NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
		rb.Add(stale)
		m[goneDevice] = rb
	}

	c.fastTick(context.Background())

	hist := c.History()
	if _, ok := hist.DiskUsedPercent[goneDevice]; ok {
		t.Fatal("expected fastTick to evict the stale disk series")
	}
	if _, ok := hist.NetworkRxBytesPerSec[goneDevice]; ok {
		t.Fatal("expected fastTick to evict the stale rx series")
	}
	if _, ok := hist.NetworkTxBytesPerSec[goneDevice]; ok {
		t.Fatal("expected fastTick to evict the stale tx series")
	}
}

// TestCollector_FastTick_KeepsRecentlyMissingDeviceSeries checks the
// counterpart of the eviction rule through the same fastTick path: a device
// missing for a single tick must not lose its history, matching
// DiskCollector.Collect skipping mountpoints that transiently fail to stat.
func TestCollector_FastTick_KeepsRecentlyMissingDeviceSeries(t *testing.T) {
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 3, // history window = 3s
		NetworkEnabled:  true,
	}, nil)

	const flakyDevice = "pimonitor-test-flaky"
	recent := HistoryPoint{Timestamp: time.Now(), Value: 1}
	for _, m := range []map[string]*RingBuffer[HistoryPoint]{c.diskHist, c.rxHist, c.txHist} {
		rb := NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
		rb.Add(recent)
		m[flakyDevice] = rb
	}

	c.fastTick(context.Background())

	hist := c.History()
	if _, ok := hist.DiskUsedPercent[flakyDevice]; !ok {
		t.Fatal("expected fastTick to keep a disk series missing for only one tick")
	}
	if _, ok := hist.NetworkRxBytesPerSec[flakyDevice]; !ok {
		t.Fatal("expected fastTick to keep an rx series missing for only one tick")
	}
	if _, ok := hist.NetworkTxBytesPerSec[flakyDevice]; !ok {
		t.Fatal("expected fastTick to keep a tx series missing for only one tick")
	}
}

// TestCollector_FastTick_EvictsWithNonPositiveFastInterval guards against
// the eviction window silently collapsing to zero (which disables eviction,
// see evictStaleSeries) for a Collector constructed with a non-positive
// FastInterval. Run's ticker already defends against this case via
// clampInterval; fastTick's history window must derive from the same
// clamped value rather than the raw, unclamped config field.
func TestCollector_FastTick_EvictsWithNonPositiveFastInterval(t *testing.T) {
	c := New(Config{
		FastInterval:    0, // invalid; clamped to 1s
		SlowInterval:    time.Minute,
		HistoryCapacity: 3, // clamped history window = 3s
	}, nil)

	const goneDevice = "pimonitor-test-gone"
	stale := HistoryPoint{Timestamp: time.Now().Add(-time.Hour), Value: 1}
	rb := NewRingBuffer[HistoryPoint](c.cfg.HistoryCapacity)
	rb.Add(stale)
	c.diskHist[goneDevice] = rb

	c.fastTick(context.Background())

	if _, ok := c.History().DiskUsedPercent[goneDevice]; ok {
		t.Fatal("expected eviction to still apply when FastInterval is non-positive")
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
