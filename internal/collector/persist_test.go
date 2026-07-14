package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fixtureHistory builds a History with every series kind populated,
// oldest point first, using millisecond-precision timestamps (the
// resolution the binary format preserves).
func fixtureHistory(base time.Time) History {
	base = base.Truncate(time.Millisecond)
	pts := func(offsets ...time.Duration) []HistoryPoint {
		out := make([]HistoryPoint, len(offsets))
		for i, off := range offsets {
			out[i] = HistoryPoint{Timestamp: base.Add(off), Value: float64(i) + 0.5}
		}
		return out
	}
	return History{
		CPUPercent:        pts(0, 5*time.Second, 10*time.Second),
		Load1:             pts(0, 5*time.Second),
		Load5:             pts(0),
		Load15:            pts(0),
		Temperature:       pts(0, 5*time.Second),
		MemoryUsedPercent: pts(0),
		SwapUsedPercent:   []HistoryPoint{},
		DiskUsedPercent: map[string][]HistoryPoint{
			"/":     pts(0, 5*time.Second),
			"/boot": pts(0),
		},
		NetworkRxBytesPerSec: map[string][]HistoryPoint{
			"eth0": pts(0, 5*time.Second, 10*time.Second),
		},
		NetworkTxBytesPerSec: map[string][]HistoryPoint{
			"eth0": pts(0),
		},
	}
}

func pointsEqual(t *testing.T, name string, got, want []HistoryPoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d points, want %d", name, len(got), len(want))
	}
	for i := range got {
		if !got[i].Timestamp.Equal(want[i].Timestamp) || got[i].Value != want[i].Value {
			t.Fatalf("%s[%d] = (%v, %v), want (%v, %v)",
				name, i, got[i].Timestamp, got[i].Value, want[i].Timestamp, want[i].Value)
		}
	}
}

func mapsEqual(t *testing.T, name string, got, want map[string][]HistoryPoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got keys %v, want keys %v", name, keysOf(got), keysOf(want))
	}
	for k, wantPts := range want {
		gotPts, ok := got[k]
		if !ok {
			t.Fatalf("%s: missing key %q", name, k)
		}
		pointsEqual(t, name+"["+k+"]", gotPts, wantPts)
	}
}

func keysOf(m map[string][]HistoryPoint) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

func historiesEqual(t *testing.T, got, want History) {
	t.Helper()
	pointsEqual(t, "CPUPercent", got.CPUPercent, want.CPUPercent)
	pointsEqual(t, "Load1", got.Load1, want.Load1)
	pointsEqual(t, "Load5", got.Load5, want.Load5)
	pointsEqual(t, "Load15", got.Load15, want.Load15)
	pointsEqual(t, "Temperature", got.Temperature, want.Temperature)
	pointsEqual(t, "MemoryUsedPercent", got.MemoryUsedPercent, want.MemoryUsedPercent)
	pointsEqual(t, "SwapUsedPercent", got.SwapUsedPercent, want.SwapUsedPercent)
	mapsEqual(t, "DiskUsedPercent", got.DiskUsedPercent, want.DiskUsedPercent)
	mapsEqual(t, "NetworkRxBytesPerSec", got.NetworkRxBytesPerSec, want.NetworkRxBytesPerSec)
	mapsEqual(t, "NetworkTxBytesPerSec", got.NetworkTxBytesPerSec, want.NetworkTxBytesPerSec)
}

func TestHistory_EncodeDecodeRoundTrip(t *testing.T) {
	want := fixtureHistory(time.Now().UTC())

	got, err := decodeHistory(encodeHistory(want))
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	historiesEqual(t, got, want)
}

func TestDecodeHistory_RejectsBadMagic(t *testing.T) {
	if _, err := decodeHistory([]byte("NOPE-not-a-history-file")); err == nil {
		t.Fatal("expected error for bad magic")
	}
}

func TestDecodeHistory_RejectsUnsupportedVersion(t *testing.T) {
	enc := encodeHistory(fixtureHistory(time.Now()))
	enc[4] = 99 // version field follows the 4-byte magic
	if _, err := decodeHistory(enc); err == nil {
		t.Fatal("expected error for unsupported version")
	}
}

func TestDecodeHistory_RejectsTruncatedFile(t *testing.T) {
	enc := encodeHistory(fixtureHistory(time.Now()))
	if _, err := decodeHistory(enc[:len(enc)-5]); err == nil {
		t.Fatal("expected error for truncated file")
	}
}

func TestDecodeHistory_RejectsTrailingData(t *testing.T) {
	enc := encodeHistory(fixtureHistory(time.Now()))
	if _, err := decodeHistory(append(enc, 0xFF)); err == nil {
		t.Fatal("expected error for trailing data")
	}
}

func TestDecodeHistory_SkipsUnknownSeriesKind(t *testing.T) {
	// A file written by a hypothetical newer version that adds a series
	// kind must still load; the unknown series is ignored.
	unknown := History{CPUPercent: fixtureHistory(time.Now()).CPUPercent}
	enc := encodeHistory(unknown)
	enc[len(historyMagic)+2+4] = 200 // kind byte of the first series

	got, err := decodeHistory(enc)
	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	if len(got.CPUPercent) != 0 {
		t.Fatalf("expected unknown series to be dropped, got %d points", len(got.CPUPercent))
	}
}

func newPersistTestCollector(path string, window time.Duration) *Collector {
	return New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		PersistPath:     path,
		HistoryWindow:   window,
	}, nil)
}

func TestCollector_PersistAndLoadHistory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)

	c1 := newPersistTestCollector(path, time.Hour)
	c1.importHistory(fixtureHistory(now.Add(-time.Minute)), now)
	c1.persistHistory()

	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()

	historiesEqual(t, c2.History(), c1.History())
}

func TestCollector_LoadHistory_TrimsOldPoints(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)
	old := HistoryPoint{Timestamp: now.Add(-2 * time.Hour), Value: 1}
	recent := HistoryPoint{Timestamp: now.Add(-time.Minute), Value: 2}

	if err := writeFileAtomic(path, encodeHistory(History{
		CPUPercent: []HistoryPoint{old, recent},
		DiskUsedPercent: map[string][]HistoryPoint{
			"/":      {old, recent},
			"/stale": {old},
		},
	})); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	c := newPersistTestCollector(path, time.Hour)
	c.loadHistory()

	h := c.History()
	pointsEqual(t, "CPUPercent", h.CPUPercent, []HistoryPoint{recent})
	pointsEqual(t, `DiskUsedPercent["/"]`, h.DiskUsedPercent["/"], []HistoryPoint{recent})
	if _, ok := h.DiskUsedPercent["/stale"]; ok {
		t.Fatal("expected fully-stale disk series to be dropped on load")
	}
}

func TestCollector_LoadHistory_CapsToCapacity(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)

	points := make([]HistoryPoint, 25)
	for i := range points {
		points[i] = HistoryPoint{Timestamp: now.Add(time.Duration(i-25) * time.Second), Value: float64(i)}
	}
	if err := writeFileAtomic(path, encodeHistory(History{CPUPercent: points})); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}

	c := newPersistTestCollector(path, time.Hour) // HistoryCapacity: 10
	c.loadHistory()

	// Only the newest 10 points fit; the oldest are discarded.
	pointsEqual(t, "CPUPercent", c.History().CPUPercent, points[15:])
}

func TestCollector_LoadHistory_MissingFileStartsEmpty(t *testing.T) {
	c := newPersistTestCollector(filepath.Join(t.TempDir(), "history.bin"), time.Hour)
	c.loadHistory()

	if got := c.History().CPUPercent; len(got) != 0 {
		t.Fatalf("expected empty history for missing file, got %v", got)
	}
}

func TestCollector_LoadHistory_CorruptFileStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	c := newPersistTestCollector(path, time.Hour)
	c.loadHistory()

	if got := c.History().CPUPercent; len(got) != 0 {
		t.Fatalf("expected empty history for corrupt file, got %v", got)
	}
}

func TestCollector_PersistHistory_OverwritesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)

	c := newPersistTestCollector(path, time.Hour)
	c.persistHistory() // first write: empty history
	c.importHistory(fixtureHistory(now.Add(-time.Minute)), now)
	c.persistHistory() // second write must atomically replace the first

	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()
	historiesEqual(t, c2.History(), c.History())
}

func TestCollector_Run_PersistsOnShutdown(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	c := New(Config{
		FastInterval:    10 * time.Millisecond,
		SlowInterval:    time.Hour,
		HistoryCapacity: 100,
		PersistPath:     path,
		HistoryWindow:   time.Hour,
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

	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()
	if len(c2.History().MemoryUsedPercent) == 0 {
		t.Fatal("expected history persisted on shutdown to contain points")
	}
}
