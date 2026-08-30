package collector

import (
	"context"
	"encoding/json"
	"path/filepath"
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

// TestCollector_FastTick_TemperatureValid guards Snapshot.TemperatureValid
// (issue #6 review: the Prometheus renderer must be able to tell a
// genuine 0°C reading apart from no reading at all, which a bare zero
// Temperature can't express). It must be true only when the tick's
// temperature collection actually succeeded, mirroring the disk case above
// rather than just reflecting whether Temperature ended up non-zero.
func TestCollector_FastTick_TemperatureValid(t *testing.T) {
	t.Run("no thermal zone", func(t *testing.T) {
		c := newTestCollector()
		c.temp = &TemperatureCollector{zoneGlob: filepath.Join(t.TempDir(), "thermal_zone*")}

		c.fastTick(context.Background())

		snap := c.Snapshot()
		if snap.TemperatureValid {
			t.Fatalf("expected TemperatureValid = false with no thermal zone, got Temperature = %+v", snap.Temperature)
		}
	})

	t.Run("thermal zone present", func(t *testing.T) {
		root := t.TempDir()
		writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "40000")

		c := newTestCollector()
		c.temp = &TemperatureCollector{zoneGlob: filepath.Join(root, "thermal_zone*")}

		c.fastTick(context.Background())

		snap := c.Snapshot()
		if !snap.TemperatureValid {
			t.Fatal("expected TemperatureValid = true after a successful collection")
		}
		if diffFloat(snap.Temperature.Celsius, 40.0) > 0.001 {
			t.Fatalf("Temperature.Celsius = %v, want 40.0", snap.Temperature.Celsius)
		}
	})
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

func TestCollector_HistoryGeneration_IncrementsOnFastTick(t *testing.T) {
	c := newTestCollector()
	ctx := context.Background()

	if got := c.HistoryGeneration(); got != 0 {
		t.Fatalf("HistoryGeneration before any tick = %d, want 0", got)
	}

	c.fastTick(ctx)
	if got := c.HistoryGeneration(); got != 1 {
		t.Fatalf("HistoryGeneration after 1 tick = %d, want 1", got)
	}

	c.fastTick(ctx)
	if got := c.HistoryGeneration(); got != 2 {
		t.Fatalf("HistoryGeneration after 2 ticks = %d, want 2", got)
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

// historySinceFixture is a History whose series all sample at the same
// three timestamps (base, +5s, +10s), the way a fastTick records them,
// plus a device that only appears at the last of those.
func historySinceFixture(base time.Time) History {
	pts := func(offsets ...time.Duration) []HistoryPoint {
		out := make([]HistoryPoint, len(offsets))
		for i, off := range offsets {
			out[i] = HistoryPoint{Timestamp: base.Add(off), Value: float64(i) + 0.5}
		}
		return out
	}
	return History{
		CPUPercent:        pts(0, 5*time.Second, 10*time.Second),
		Load1:             pts(0, 5*time.Second, 10*time.Second),
		Load5:             pts(0, 5*time.Second, 10*time.Second),
		Load15:            pts(0, 5*time.Second, 10*time.Second),
		Temperature:       pts(0, 5*time.Second, 10*time.Second),
		MemoryUsedPercent: pts(0, 5*time.Second, 10*time.Second),
		SwapUsedPercent:   pts(0, 5*time.Second, 10*time.Second),
		DiskUsedPercent: map[string][]HistoryPoint{
			"/":     pts(0, 5*time.Second, 10*time.Second),
			"/boot": pts(0),
		},
		NetworkRxBytesPerSec: map[string][]HistoryPoint{
			"eth0": pts(0, 5*time.Second, 10*time.Second),
		},
		NetworkTxBytesPerSec: map[string][]HistoryPoint{
			"eth0": pts(0, 5*time.Second, 10*time.Second),
		},
	}
}

func TestHistorySince_ReturnsOnlyNewerPoints(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	got := historySinceFixture(base).Since(base.Add(2 * time.Second))

	if len(got.CPUPercent) != 2 {
		t.Fatalf("CPUPercent = %d points, want 2", len(got.CPUPercent))
	}
	if !got.CPUPercent[0].Timestamp.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("oldest returned point = %v, want %v", got.CPUPercent[0].Timestamp, base.Add(5*time.Second))
	}
	// Every scalar series is filtered, not just the first one.
	for name, points := range map[string][]HistoryPoint{
		"load1":               got.Load1,
		"load5":               got.Load5,
		"load15":              got.Load15,
		"temperature":         got.Temperature,
		"memory_used_percent": got.MemoryUsedPercent,
		"swap_used_percent":   got.SwapUsedPercent,
	} {
		if len(points) != 2 {
			t.Errorf("%s = %d points, want 2", name, len(points))
		}
	}
}

// TestHistorySince_ExcludesExactBoundary pins the strictly-newer rule: a
// client that passes back the timestamp of the newest point it holds must
// not be handed that same point again.
func TestHistorySince_ExcludesExactBoundary(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	got := historySinceFixture(base).Since(base.Add(5 * time.Second))

	if len(got.CPUPercent) != 1 {
		t.Fatalf("CPUPercent = %d points, want 1 (the boundary point must be excluded)", len(got.CPUPercent))
	}
	if !got.CPUPercent[0].Timestamp.Equal(base.Add(10 * time.Second)) {
		t.Fatalf("returned point = %v, want %v", got.CPUPercent[0].Timestamp, base.Add(10*time.Second))
	}
}

// TestHistorySince_FiltersDevicesIndependently covers the per-device rule:
// each key is filtered on its own, and a device left with no points is
// dropped rather than serialised as an empty array.
func TestHistorySince_FiltersDevicesIndependently(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	got := historySinceFixture(base).Since(base.Add(2 * time.Second))

	if len(got.DiskUsedPercent) != 1 {
		t.Fatalf("DiskUsedPercent has %d devices, want 1: %+v", len(got.DiskUsedPercent), got.DiskUsedPercent)
	}
	if _, ok := got.DiskUsedPercent["/boot"]; ok {
		t.Fatal("expected /boot (no points newer than since) to be omitted entirely")
	}
	if len(got.DiskUsedPercent["/"]) != 2 {
		t.Fatalf(`DiskUsedPercent["/"] = %d points, want 2`, len(got.DiskUsedPercent["/"]))
	}
	if len(got.NetworkRxBytesPerSec["eth0"]) != 2 || len(got.NetworkTxBytesPerSec["eth0"]) != 2 {
		t.Fatalf("network series not filtered: rx=%+v tx=%+v", got.NetworkRxBytesPerSec, got.NetworkTxBytesPerSec)
	}
}

func TestHistorySince_NewerThanEverySampleIsEmpty(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	got := historySinceFixture(base).Since(base.Add(time.Hour))

	if len(got.CPUPercent) != 0 {
		t.Fatalf("CPUPercent = %d points, want 0", len(got.CPUPercent))
	}
	if got.CPUPercent == nil {
		t.Fatal("expected an empty (but non-nil) series so it encodes as [] rather than null")
	}
	if got.DiskUsedPercent != nil || got.NetworkRxBytesPerSec != nil || got.NetworkTxBytesPerSec != nil {
		t.Fatalf("expected all device maps to be omitted: %+v", got)
	}
}

func TestHistorySince_OlderThanWholeWindowReturnsEverything(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	full := historySinceFixture(base)
	got := full.Since(base.Add(-time.Hour))

	if len(got.CPUPercent) != len(full.CPUPercent) {
		t.Fatalf("CPUPercent = %d points, want the full %d", len(got.CPUPercent), len(full.CPUPercent))
	}
	if len(got.DiskUsedPercent) != len(full.DiskUsedPercent) {
		t.Fatalf("DiskUsedPercent = %d devices, want the full %d", len(got.DiskUsedPercent), len(full.DiskUsedPercent))
	}
}

// TestHistorySince_ZeroTimeReturnsUnchanged covers the handler's
// no-?since= path, which relies on a zero time meaning "everything".
func TestHistorySince_ZeroTimeReturnsUnchanged(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	full := historySinceFixture(base)
	got := full.Since(time.Time{})

	if len(got.CPUPercent) != len(full.CPUPercent) || len(got.DiskUsedPercent) != len(full.DiskUsedPercent) {
		t.Fatalf("zero since changed the history: %+v", got)
	}
}

// TestHistorySince_DoesNotMutateSource guards the aliasing Since documents:
// the returned series share the source's backing arrays, so filtering must
// stay read-only for the cached copy the HTTP layer keeps.
func TestHistorySince_DoesNotMutateSource(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	full := historySinceFixture(base)

	_ = full.Since(base.Add(5 * time.Second))

	if len(full.CPUPercent) != 3 || len(full.DiskUsedPercent) != 2 {
		t.Fatalf("source history changed: cpu=%d disks=%d", len(full.CPUPercent), len(full.DiskUsedPercent))
	}
	if !full.CPUPercent[0].Timestamp.Equal(base) {
		t.Fatalf("source series start = %v, want %v", full.CPUPercent[0].Timestamp, base)
	}
}
