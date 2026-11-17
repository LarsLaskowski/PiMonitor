package collector

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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
		DiskIOReadBytesPerSec: map[string][]HistoryPoint{
			"mmcblk0": pts(0, 5*time.Second),
		},
		DiskIOWriteBytesPerSec: map[string][]HistoryPoint{
			"mmcblk0": pts(0),
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
	mapsEqual(t, "DiskIOReadBytesPerSec", got.DiskIOReadBytesPerSec, want.DiskIOReadBytesPerSec)
	mapsEqual(t, "DiskIOWriteBytesPerSec", got.DiskIOWriteBytesPerSec, want.DiskIOWriteBytesPerSec)
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
	c1.persistWG.Wait()

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
	c.persistWG.Wait()
	c.importHistory(fixtureHistory(now.Add(-time.Minute)), now)
	c.persistHistory() // second write must atomically replace the first
	c.persistWG.Wait()

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

// blockingWriteFile returns a writeFile replacement that signals started the
// first time it is invoked and then blocks until proceed is closed, before
// delegating to writeFileAtomic. calls counts every invocation.
func blockingWriteFile(started chan<- struct{}, proceed <-chan struct{}, calls *int32) func(string, []byte) error {
	var signaled int32
	return func(path string, data []byte) error {
		atomic.AddInt32(calls, 1)
		if atomic.CompareAndSwapInt32(&signaled, 0, 1) {
			close(started)
		}
		<-proceed
		return writeFileAtomic(path, data)
	}
}

func TestCollector_PersistHistory_DoesNotBlockOnSlowWrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)

	c := newPersistTestCollector(path, time.Hour)
	c.importHistory(fixtureHistory(now.Add(-time.Minute)), now)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var calls int32
	c.writeFile = blockingWriteFile(started, proceed, &calls)

	returned := make(chan struct{})
	go func() {
		c.persistHistory()
		close(returned)
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("persistHistory did not return while the write was blocked")
	}

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("write function was never invoked")
	}

	// The data must still land once the slow write is allowed to finish.
	close(proceed)
	c.persistWG.Wait()

	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()
	historiesEqual(t, c2.History(), c.History())
}

func TestCollector_PersistHistory_SkipsOverlappingWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	c := newPersistTestCollector(path, time.Hour)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var calls int32
	c.writeFile = blockingWriteFile(started, proceed, &calls)

	c.persistHistory() // starts the write, which blocks
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("write function was never invoked")
	}

	// Both of these must be skipped: a write is already in flight.
	c.persistHistory()
	c.persistHistory()

	close(proceed)
	c.persistWG.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("write function invoked %d times while overlapping, want 1", got)
	}
}

func TestCollector_Run_ShutdownWaitsForFinalFlush(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	c := New(Config{
		FastInterval:    10 * time.Millisecond,
		SlowInterval:    time.Hour,
		HistoryCapacity: 100,
		PersistPath:     path,
		HistoryWindow:   time.Hour,
	}, nil)

	started := make(chan struct{})
	proceed := make(chan struct{})
	var calls int32
	c.writeFile = blockingWriteFile(started, proceed, &calls)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		c.Run(ctx)
		close(done)
	}()

	select {
	case <-started:
		t.Fatal("final flush must not start before shutdown")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("final flush was never started")
	}

	// Run must block on the in-flight flush rather than returning early.
	select {
	case <-done:
		t.Fatal("Run returned before the blocked final flush completed")
	case <-time.After(200 * time.Millisecond):
	}

	close(proceed)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after the final flush completed")
	}

	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()
	if len(c2.History().MemoryUsedPercent) == 0 {
		t.Fatal("expected history persisted on shutdown to contain points")
	}
}

// writeFileAtomic must fsync the parent directory after the rename, so the
// new directory entry is durable and not only the file contents. At the time
// of the sync the renamed file must already be in place and no temp file may
// be left behind.
func TestWriteFileAtomic_SyncsParentDirAfterRename(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	path := filepath.Join(dir, "history.bin")

	var synced []string
	orig := syncDir
	t.Cleanup(func() { syncDir = orig })
	syncDir = func(d string) error {
		synced = append(synced, d)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("history file not in place when directory is synced: %v", err)
		} else if string(got) != "payload" {
			t.Errorf("history file content at sync time = %q, want %q", got, "payload")
		}
		entries, err := os.ReadDir(d)
		if err != nil {
			t.Fatalf("ReadDir: %v", err)
		}
		if len(entries) != 1 {
			t.Errorf("directory holds %d entries at sync time, want only the renamed file", len(entries))
		}
		return orig(d)
	}

	if err := writeFileAtomic(path, []byte("payload")); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
	if len(synced) != 1 || synced[0] != dir {
		t.Fatalf("syncDir calls = %q, want exactly [%q]", synced, dir)
	}
}

// A directory fsync that fails (unsupported on the platform, or an I/O
// error after the rename already succeeded) must not fail the write: the
// file is in place, only its crash durability is weaker.
func TestWriteFileAtomic_IgnoresDirSyncError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")

	orig := syncDir
	t.Cleanup(func() { syncDir = orig })
	syncDir = func(string) error { return errors.New("fsync not supported") }

	if err := writeFileAtomic(path, []byte("payload")); err != nil {
		t.Fatalf("writeFileAtomic = %v, want nil despite a failed directory sync", err)
	}
	if got, err := os.ReadFile(path); err != nil || string(got) != "payload" {
		t.Fatalf("ReadFile = %q, %v; want %q", got, err, "payload")
	}
}

// A failed write never reaches the directory sync: there is no rename to
// make durable.
func TestWriteFileAtomic_NoDirSyncOnFailedWrite(t *testing.T) {
	// A regular file as the parent makes MkdirAll fail.
	parent := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(parent, nil, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	orig := syncDir
	t.Cleanup(func() { syncDir = orig })
	called := false
	syncDir = func(string) error { called = true; return nil }

	if err := writeFileAtomic(filepath.Join(parent, "history.bin"), []byte("payload")); err == nil {
		t.Fatal("writeFileAtomic = nil, want an error when the parent is not a directory")
	}
	if called {
		t.Fatal("syncDir called after a failed write")
	}
}

func TestSyncDirectory_ExistingDir(t *testing.T) {
	if err := syncDirectory(t.TempDir()); err != nil {
		t.Fatalf("syncDirectory = %v, want nil", err)
	}
}

func TestSyncDirectory_MissingDir(t *testing.T) {
	if err := syncDirectory(filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("syncDirectory = nil, want an error for a missing directory")
	}
}

// waitForHistoryPoint fails the test unless c records a memory point within timeout.
func waitForHistoryPoint(t *testing.T, c *Collector, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for len(c.History().MemoryUsedPercent) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Run recorded no history point")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// assertHistoryFileMatches fails the test unless the history file at path holds
// the point count of every scalar series of c, and holds points in the memory
// and load1 series.
func assertHistoryFileMatches(t *testing.T, c *Collector, path string) {
	t.Helper()
	c2 := newPersistTestCollector(path, time.Hour)
	c2.loadHistory()
	want := scalarSeriesLens(c.History())
	got := scalarSeriesLens(c2.History())
	for key, w := range want {
		if got[key] != w {
			t.Errorf("series %s: file holds %d points, collector has %d", key, got[key], w)
		}
	}
	for _, key := range []string{"memory_used_percent", "load1"} {
		if got[key] == 0 {
			t.Errorf("series %s of the file has no point, want at least one", key)
		}
	}
}

// TestCollector_Run_FinalWriteWaitsForRunningWrite covers issue #198: a write
// still running when shutdown starts must not make the final write get
// skipped; the final write runs after it and the file holds the latest state.
func TestCollector_Run_FinalWriteWaitsForRunningWrite(t *testing.T) {
	t.Run("AC4 final write runs after the running write", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		c := New(Config{
			FastInterval:    10 * time.Millisecond,
			SlowInterval:    time.Hour,
			HistoryCapacity: 100,
			PersistPath:     path,
			HistoryWindow:   time.Hour,
		}, nil)

		started := make(chan struct{})
		proceed := make(chan struct{})
		var calls int32
		c.writeFile = blockingWriteFile(started, proceed, &calls)

		// A write of the still-empty history is running and blocked.
		c.persistHistory()
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Fatal("write function was never invoked")
		}

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			c.Run(ctx)
			close(done)
		}()

		// Let Run collect a few points while the write blocks, then shut down.
		waitForHistoryPoint(t, c, 2*time.Second)
		cancel()

		select {
		case <-done:
			t.Fatal("Run returned while a write was still running")
		case <-time.After(200 * time.Millisecond):
		}

		close(proceed)

		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("Run did not return after the running write completed")
		}

		if got := atomic.LoadInt32(&calls); got != 2 {
			t.Errorf("write function invoked %d times, want 2 (running write and final write)", got)
		}

		assertHistoryFileMatches(t, c, path)
	})
}

func TestCollector_FlushHistory_WriteFailureIsLoggedAndLockReleased(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	log, buf := newCapturingLogger()
	c := New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		PersistPath:     path,
		HistoryWindow:   time.Hour,
	}, log)

	wantErr := errors.New("disk full")
	var calls int32
	c.writeFile = func(string, []byte) error {
		atomic.AddInt32(&calls, 1)
		return wantErr
	}

	done := make(chan struct{})
	go func() {
		c.flushHistory()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("flushHistory did not return after a failed write")
	}

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("write function invoked %d times, want 1", got)
	}
	out := buf.String()
	if !strings.Contains(out, "could not persist metric history") || !strings.Contains(out, "disk full") {
		t.Errorf("log = %q, want the persist warning carrying the write error", out)
	}
	select {
	case c.persisting <- struct{}{}:
		<-c.persisting
	default:
		t.Error("persisting slot still held after a failed flush, want it released")
	}
}

// On-disk kinds of the openHAB series (docs/areas/history.md, File format);
// the values are part of the format and never change.
const (
	kindOpenHABRSS      uint8 = 12
	kindOpenHABHeapUsed uint8 = 13
	kindOpenHABRestarts uint8 = 14
)

// recordKinds returns the kind byte of each series record of an encoded
// history file, in file order.
func recordKinds(t *testing.T, data []byte) []uint8 {
	t.Helper()
	pos := len(historyMagic) + 2
	count := int(binary.LittleEndian.Uint32(data[pos:]))
	pos += 4
	kinds := make([]uint8, 0, count)
	for range count {
		kinds = append(kinds, data[pos])
		keyLen := int(binary.LittleEndian.Uint16(data[pos+1:]))
		points := int(binary.LittleEndian.Uint32(data[pos+3+keyLen:]))
		pos += 1 + 2 + keyLen + 4 + points*pointSize
	}
	if pos != len(data) {
		t.Fatalf("records end at byte %d, file has %d bytes", pos, len(data))
	}
	return kinds
}

// openHABHistoryFixture holds the three openHAB series, oldest point first.
func openHABHistoryFixture(base time.Time) History {
	base = base.Truncate(time.Millisecond)
	return History{
		OpenHABRSSBytes: []HistoryPoint{
			{Timestamp: base, Value: 812e6}, {Timestamp: base.Add(time.Minute), Value: 815e6}, {Timestamp: base.Add(2 * time.Minute), Value: 820e6}},
		OpenHABHeapUsedBytes: []HistoryPoint{
			{Timestamp: base, Value: 400e6}, {Timestamp: base.Add(time.Minute), Value: 410e6}},
		OpenHABRestarts: []HistoryPoint{{Timestamp: base.Add(time.Minute), Value: 2000}},
	}
}

func TestHistory_EncodeDecodeRoundTrip_OpenHAB_AC6(t *testing.T) {
	want := openHABHistoryFixture(time.Now().UTC())

	data := encodeHistory(want)
	got, err := decodeHistory(data)

	if err != nil {
		t.Fatalf("decodeHistory: %v", err)
	}
	pointsEqual(t, "OpenHABRSSBytes", got.OpenHABRSSBytes, want.OpenHABRSSBytes)
	pointsEqual(t, "OpenHABHeapUsedBytes", got.OpenHABHeapUsedBytes, want.OpenHABHeapUsedBytes)
	pointsEqual(t, "OpenHABRestarts", got.OpenHABRestarts, want.OpenHABRestarts)
	kinds := recordKinds(t, data)
	for _, kind := range []uint8{kindOpenHABRSS, kindOpenHABHeapUsed, kindOpenHABRestarts} {
		if n := countKind(kinds, kind); n != 1 {
			t.Fatalf("records of kind %d = %d in %v, want exactly 1 (version stays %d)", kind, n, kinds, historyVersion)
		}
	}
}

func countKind(kinds []uint8, kind uint8) int {
	n := 0
	for _, k := range kinds {
		if k == kind {
			n++
		}
	}
	return n
}

func TestEncodeHistory_NoOpenHABSeriesWritesNoRecord_AC6(t *testing.T) {
	h := fixtureHistory(time.Now().UTC())
	h.OpenHABRSSBytes = []HistoryPoint{}
	h.OpenHABHeapUsedBytes = nil

	kinds := recordKinds(t, encodeHistory(h))

	for _, kind := range []uint8{kindOpenHABRSS, kindOpenHABHeapUsed, kindOpenHABRestarts} {
		if n := countKind(kinds, kind); n != 0 {
			t.Fatalf("records of kind %d = %d in %v, want none while the openHAB series are empty", kind, n, kinds)
		}
	}
	if len(kinds) == 0 {
		t.Fatal("no records written at all, want the other series")
	}
}

func TestEncodeHistory_OnlyNonEmptyOpenHABSeriesWritten_AC6(t *testing.T) {
	h := fixtureHistory(time.Now().UTC())
	h.OpenHABRSSBytes = openHABHistoryFixture(time.Now().UTC()).OpenHABRSSBytes

	kinds := recordKinds(t, encodeHistory(h))

	if countKind(kinds, kindOpenHABRSS) != 1 {
		t.Fatalf("record kinds = %v, want the RSS series (kind %d) written", kinds, kindOpenHABRSS)
	}
	if countKind(kinds, kindOpenHABHeapUsed) != 0 || countKind(kinds, kindOpenHABRestarts) != 0 {
		t.Fatalf("record kinds = %v, want no record for an empty heap or restart series", kinds)
	}
}

// newOHPersistCollector builds a Collector with persistence at path, the
// other series' window, and the openHAB poll enabled with its own window
// and capacity (hours of 0 leaves it disabled).
func newOHPersistCollector(path string, window time.Duration, hours, capacity int) *Collector {
	return New(Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Minute,
		HistoryCapacity: 10,
		PersistPath:     path,
		HistoryWindow:   window,
		OpenHAB: OpenHABConfig{
			Enabled:            hours > 0,
			Interval:           time.Minute,
			HistoryWindowHours: hours,
			HistoryCapacity:    capacity,
		},
	}, nil)
}

// writeHistoryFile writes h as a history file at path.
func writeHistoryFile(t *testing.T, path string, h History) {
	t.Helper()
	if err := writeFileAtomic(path, encodeHistory(h)); err != nil {
		t.Fatalf("writeFileAtomic: %v", err)
	}
}

func TestCollector_PersistAndLoadHistory_OpenHAB_AC6(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	now := time.Now().Truncate(time.Millisecond)
	want := openHABHistoryFixture(now.Add(-10 * time.Minute))

	c1 := newOHPersistCollector(path, time.Hour, 24, 1440)
	c1.importHistory(want, now)
	c1.persistHistory()
	c1.persistWG.Wait()
	c2 := newOHPersistCollector(path, time.Hour, 24, 1440)
	c2.loadHistory()

	got := c2.History()
	pointsEqual(t, "OpenHABRSSBytes", got.OpenHABRSSBytes, want.OpenHABRSSBytes)
	pointsEqual(t, "OpenHABHeapUsedBytes", got.OpenHABHeapUsedBytes, want.OpenHABHeapUsedBytes)
	pointsEqual(t, "OpenHABRestarts", got.OpenHABRestarts, want.OpenHABRestarts)
}

func TestCollector_LoadHistory_OpenHAB(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	t.Run("AC6 file without openHAB series restores the others", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		recent := HistoryPoint{Timestamp: now.Add(-time.Minute), Value: 2}
		writeHistoryFile(t, path, History{CPUPercent: []HistoryPoint{recent}, Load1: []HistoryPoint{recent}})
		c := newOHPersistCollector(path, time.Hour, 24, 1440)

		c.loadHistory()

		h := c.History()
		pointsEqual(t, "CPUPercent", h.CPUPercent, []HistoryPoint{recent})
		pointsEqual(t, "Load1", h.Load1, []HistoryPoint{recent})
		if len(h.OpenHABRSSBytes)+len(h.OpenHABHeapUsedBytes)+len(h.OpenHABRestarts) != 0 {
			t.Fatalf("openHAB series = %v, %v, %v, want none from a file without them",
				h.OpenHABRSSBytes, h.OpenHABHeapUsedBytes, h.OpenHABRestarts)
		}
	})
	t.Run("AC6 disabled restores none", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		recent := HistoryPoint{Timestamp: now.Add(-time.Minute), Value: 2}
		writeHistoryFile(t, path, History{
			CPUPercent: []HistoryPoint{recent}, OpenHABRSSBytes: []HistoryPoint{recent},
			OpenHABHeapUsedBytes: []HistoryPoint{recent}, OpenHABRestarts: []HistoryPoint{recent}})
		c := newOHPersistCollector(path, time.Hour, 0, 0)

		c.loadHistory()

		h := c.History()
		pointsEqual(t, "CPUPercent", h.CPUPercent, []HistoryPoint{recent})
		if h.OpenHABRSSBytes != nil || h.OpenHABHeapUsedBytes != nil || h.OpenHABRestarts != nil {
			t.Fatalf("openHAB series = %v, %v, %v, want none restored with openHAB disabled",
				h.OpenHABRSSBytes, h.OpenHABHeapUsedBytes, h.OpenHABRestarts)
		}
		c.mu.Lock()
		held := c.ohRSSHist != nil || c.ohHeapHist != nil || c.ohRestartHist != nil
		c.mu.Unlock()
		if held {
			t.Fatal("openHAB buffers exist with openHAB disabled")
		}
	})
	t.Run("AC6 drops points older than the openHAB window", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		old := HistoryPoint{Timestamp: now.Add(-3 * time.Hour), Value: 1}
		recent := HistoryPoint{Timestamp: now.Add(-time.Minute), Value: 2}
		writeHistoryFile(t, path, History{
			OpenHABRSSBytes: []HistoryPoint{old, recent}, OpenHABHeapUsedBytes: []HistoryPoint{old}, OpenHABRestarts: []HistoryPoint{old, recent}})
		c := newOHPersistCollector(path, time.Hour, 2, 1440)

		c.loadHistory()

		h := c.History()
		pointsEqual(t, "OpenHABRSSBytes", h.OpenHABRSSBytes, []HistoryPoint{recent})
		pointsEqual(t, "OpenHABRestarts", h.OpenHABRestarts, []HistoryPoint{recent})
		if len(h.OpenHABHeapUsedBytes) != 0 {
			t.Fatalf("OpenHABHeapUsedBytes = %v, want the fully stale series dropped", h.OpenHABHeapUsedBytes)
		}
	})
	t.Run("AC6 uses the openHAB window, not history_window_minutes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		inOpenHABWindow := HistoryPoint{Timestamp: now.Add(-90 * time.Minute), Value: 5}
		writeHistoryFile(t, path, History{OpenHABRSSBytes: []HistoryPoint{inOpenHABWindow}, CPUPercent: []HistoryPoint{inOpenHABWindow}})
		c := newOHPersistCollector(path, time.Hour, 2, 1440)

		c.loadHistory()

		h := c.History()
		pointsEqual(t, "OpenHABRSSBytes", h.OpenHABRSSBytes, []HistoryPoint{inOpenHABWindow})
		if len(h.CPUPercent) != 0 {
			t.Fatalf("CPUPercent = %v, want the 1 h window to still drop the 90 min old point", h.CPUPercent)
		}
	})
	t.Run("AC6 caps to the openHAB capacity", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "history.bin")
		points := make([]HistoryPoint, 8)
		for i := range points {
			points[i] = HistoryPoint{Timestamp: now.Add(time.Duration(i-10) * time.Minute), Value: float64(i)}
		}
		writeHistoryFile(t, path, History{OpenHABRSSBytes: points})
		c := newOHPersistCollector(path, time.Hour, 24, 5)

		c.loadHistory()

		pointsEqual(t, "OpenHABRSSBytes", c.History().OpenHABRSSBytes, points[3:])
	})
}

// rawPoint is one point of a history file as it lies on disk: the
// timestamp in Unix milliseconds and the IEEE 754 bits of the value.
type rawPoint struct {
	ms   int64
	bits uint64
}

func rawBytes(points []rawPoint) []byte {
	raw := make([]byte, 0, len(points)*pointSize)
	for _, p := range points {
		raw = binary.LittleEndian.AppendUint64(raw, uint64(p.ms))
		raw = binary.LittleEndian.AppendUint64(raw, p.bits)
	}
	return raw
}

// validRawPoints are two finite points of a current year, in file order.
var (
	validBefore = rawPoint{ms: 1_700_000_000_000, bits: math.Float64bits(1.5)}
	validAfter  = rawPoint{ms: 1_700_000_001_000, bits: math.Float64bits(2.5)}
)

func assertPointBits(t *testing.T, name string, got []HistoryPoint, want []rawPoint) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: got %d points %v, want %d", name, len(got), got, len(want))
	}
	for i := range got {
		if got[i].Timestamp.UnixMilli() != want[i].ms || math.Float64bits(got[i].Value) != want[i].bits {
			t.Fatalf("%s[%d] = (%d ms, bits %#x), want (%d ms, bits %#x)", name, i,
				got[i].Timestamp.UnixMilli(), math.Float64bits(got[i].Value), want[i].ms, want[i].bits)
		}
	}
}

func localMillis(year int) int64 {
	return time.Date(year, 1, 1, 0, 0, 0, 0, time.Local).UnixMilli()
}

func TestDecodeHistoryPoints_DropsUnencodablePoints_AC1(t *testing.T) {
	finite := math.Float64bits(3.5)
	nowMs := validBefore.ms
	rows := []struct {
		name string
		pt   rawPoint
		keep bool
	}{
		{"AC1 quiet NaN value", rawPoint{nowMs, 0x7FF8000000000001}, false},
		{"AC1 signalling NaN value", rawPoint{nowMs, 0x7FF0000000000001}, false},
		{"AC1 negative NaN value", rawPoint{nowMs, 0xFFF8000000000000}, false},
		{"AC1 NaN with full mantissa", rawPoint{nowMs, 0x7FFFFFFFFFFFFFFF}, false},
		{"AC1 positive infinity", rawPoint{nowMs, 0x7FF0000000000000}, false},
		{"AC1 negative infinity", rawPoint{nowMs, 0xFFF0000000000000}, false},
		{"AC1 timestamp MaxInt64", rawPoint{math.MaxInt64, finite}, false},
		{"AC1 timestamp MinInt64", rawPoint{math.MinInt64, finite}, false},
		{"AC1 first millisecond of year 10000", rawPoint{localMillis(10000), finite}, false},
		{"AC1 millisecond before year 0", rawPoint{localMillis(0) - 1, finite}, false},
		{"AC1 max finite value", rawPoint{nowMs, 0x7FEFFFFFFFFFFFFF}, true},
		{"AC1 min finite value", rawPoint{nowMs, 0xFFEFFFFFFFFFFFFF}, true},
		{"AC1 smallest subnormal", rawPoint{nowMs, 0x0000000000000001}, true},
		{"AC1 negative zero", rawPoint{nowMs, 0x8000000000000000}, true},
		{"AC1 first millisecond of year 0", rawPoint{localMillis(0), finite}, true},
		{"AC1 last millisecond of year 9999", rawPoint{localMillis(10000) - 1, finite}, true},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			raw := rawBytes([]rawPoint{validBefore, row.pt, validAfter})
			want := []rawPoint{validBefore, validAfter}
			if row.keep {
				want = []rawPoint{validBefore, row.pt, validAfter}
			}

			got := decodeHistoryPoints(raw, 3)

			assertPointBits(t, "points", got, want)
		})
	}
}

// historyFileWithSeries builds a history file of one series of the given
// kind and key holding points.
func historyFileWithSeries(kind uint8, key string, points []rawPoint) []byte {
	buf := append([]byte(historyMagic), 0, 0)
	binary.LittleEndian.PutUint16(buf[len(historyMagic):], historyVersion)
	buf = binary.LittleEndian.AppendUint32(buf, 1)
	buf = append(buf, kind)
	buf = binary.LittleEndian.AppendUint16(buf, uint16(len(key)))
	buf = append(buf, key...)
	buf = binary.LittleEndian.AppendUint32(buf, uint32(len(points)))
	return append(buf, rawBytes(points)...)
}

func TestDecodeHistory_DropsUnencodablePointsPerKind_AC2(t *testing.T) {
	kinds := []struct {
		name string
		kind uint8
		key  string
		get  func(History) []HistoryPoint
	}{
		{"CPUPercent", seriesCPUPercent, "", func(h History) []HistoryPoint { return h.CPUPercent }},
		{"Load1", seriesLoad1, "", func(h History) []HistoryPoint { return h.Load1 }},
		{"Load5", seriesLoad5, "", func(h History) []HistoryPoint { return h.Load5 }},
		{"Load15", seriesLoad15, "", func(h History) []HistoryPoint { return h.Load15 }},
		{"Temperature", seriesTemperature, "", func(h History) []HistoryPoint { return h.Temperature }},
		{"MemoryUsedPercent", seriesMemoryUsedPercent, "", func(h History) []HistoryPoint { return h.MemoryUsedPercent }},
		{"SwapUsedPercent", seriesSwapUsedPercent, "", func(h History) []HistoryPoint { return h.SwapUsedPercent }},
		{"DiskUsedPercent", seriesDiskUsedPercent, "/", func(h History) []HistoryPoint { return h.DiskUsedPercent["/"] }},
		{"NetworkRxBytesPerSec", seriesNetworkRxBytesPerSec, "eth0", func(h History) []HistoryPoint { return h.NetworkRxBytesPerSec["eth0"] }},
		{"NetworkTxBytesPerSec", seriesNetworkTxBytesPerSec, "eth0", func(h History) []HistoryPoint { return h.NetworkTxBytesPerSec["eth0"] }},
		{"DiskIOReadBytesPerSec", seriesDiskIOReadBytesPerSec, "mmcblk0", func(h History) []HistoryPoint { return h.DiskIOReadBytesPerSec["mmcblk0"] }},
		{"DiskIOWriteBytesPerSec", seriesDiskIOWriteBytesPerSec, "mmcblk0", func(h History) []HistoryPoint { return h.DiskIOWriteBytesPerSec["mmcblk0"] }},
		{"OpenHABRSSBytes", seriesOpenHABRSSBytes, "", func(h History) []HistoryPoint { return h.OpenHABRSSBytes }},
		{"OpenHABHeapUsedBytes", seriesOpenHABHeapUsedBytes, "", func(h History) []HistoryPoint { return h.OpenHABHeapUsedBytes }},
		{"OpenHABRestarts", seriesOpenHABRestarts, "", func(h History) []HistoryPoint { return h.OpenHABRestarts }},
	}
	third := rawPoint{ms: 1_700_000_002_000, bits: math.Float64bits(4.5)}
	fourth := rawPoint{ms: 1_700_000_003_000, bits: math.Float64bits(5.5)}
	fifth := rawPoint{ms: 1_700_000_004_000, bits: math.Float64bits(6.5)}
	finite := math.Float64bits(1)
	points := []rawPoint{
		validBefore,
		{validBefore.ms, math.Float64bits(math.NaN())},
		validAfter,
		{validAfter.ms, math.Float64bits(math.Inf(1))},
		third,
		{third.ms, math.Float64bits(math.Inf(-1))},
		fourth,
		{localMillis(10000), finite},
		fifth,
	}
	want := []rawPoint{validBefore, validAfter, third, fourth, fifth}
	for _, k := range kinds {
		t.Run("AC2 "+k.name, func(t *testing.T) {
			data := historyFileWithSeries(k.kind, k.key, points)

			h, err := decodeHistory(data)

			if err != nil {
				t.Fatalf("decodeHistory: %v, want no error", err)
			}
			assertPointBits(t, k.name, k.get(h), want)
		})
	}
}

func TestCollector_LoadHistory_DropsUnencodablePoints_AC3(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.bin")
	base := time.Now().Truncate(time.Millisecond).Add(-10 * time.Minute)
	valid := func(i int) HistoryPoint {
		return HistoryPoint{Timestamp: base.Add(time.Duration(i) * time.Minute), Value: float64(i) + 0.5}
	}
	bad := func(i int, v float64) HistoryPoint {
		return HistoryPoint{Timestamp: base.Add(time.Duration(i) * time.Minute), Value: v}
	}
	farFuture := HistoryPoint{Timestamp: time.Date(10000, 1, 1, 0, 0, 0, 0, time.Local), Value: 1}
	mixed := []HistoryPoint{valid(0), bad(1, math.NaN()), valid(2), bad(3, math.Inf(1)), valid(4), bad(5, math.Inf(-1)), valid(6), farFuture, valid(7)}
	wantMixed := []HistoryPoint{valid(0), valid(2), valid(4), valid(6), valid(7)}
	onlyBad := []HistoryPoint{bad(1, math.NaN()), bad(2, math.Inf(1)), farFuture}
	writeHistoryFile(t, path, History{
		CPUPercent:             mixed,
		Temperature:            mixed,
		DiskUsedPercent:        map[string][]HistoryPoint{"/": mixed},
		NetworkRxBytesPerSec:   map[string][]HistoryPoint{"eth0": onlyBad},
		DiskIOReadBytesPerSec:  map[string][]HistoryPoint{"mmcblk0": {valid(1), valid(2)}},
		OpenHABRSSBytes:        mixed,
		OpenHABHeapUsedBytes:   mixed,
		OpenHABRestarts:        mixed,
		NetworkTxBytesPerSec:   map[string][]HistoryPoint{"eth0": mixed},
		DiskIOWriteBytesPerSec: map[string][]HistoryPoint{"mmcblk0": mixed},
	})
	c := newOHPersistCollector(path, time.Hour, 2, 1440)

	c.loadHistory()

	h := c.History()
	pointsEqual(t, "CPUPercent", h.CPUPercent, wantMixed)
	pointsEqual(t, "Temperature", h.Temperature, wantMixed)
	pointsEqual(t, "OpenHABRSSBytes", h.OpenHABRSSBytes, wantMixed)
	pointsEqual(t, "OpenHABHeapUsedBytes", h.OpenHABHeapUsedBytes, wantMixed)
	pointsEqual(t, "OpenHABRestarts", h.OpenHABRestarts, wantMixed)
	mapsEqual(t, "DiskUsedPercent", h.DiskUsedPercent, map[string][]HistoryPoint{"/": wantMixed})
	mapsEqual(t, "NetworkTxBytesPerSec", h.NetworkTxBytesPerSec, map[string][]HistoryPoint{"eth0": wantMixed})
	mapsEqual(t, "DiskIOWriteBytesPerSec", h.DiskIOWriteBytesPerSec, map[string][]HistoryPoint{"mmcblk0": wantMixed})
	mapsEqual(t, "DiskIOReadBytesPerSec", h.DiskIOReadBytesPerSec, map[string][]HistoryPoint{"mmcblk0": {valid(1), valid(2)}})
	if len(h.NetworkRxBytesPerSec) != 0 {
		t.Fatalf("NetworkRxBytesPerSec = %v, want the series of only dropped points not restored", h.NetworkRxBytesPerSec)
	}
	if _, err := json.Marshal(h); err != nil {
		t.Fatalf("json.Marshal(History()): %v, want the restored history encodable", err)
	}
}
