package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/collector"
	"github.com/larslaskowski/pimonitor/internal/config"
)

type fakeMetrics struct {
	snapshot collector.Snapshot
	history  collector.History
	// historyGen is returned verbatim by HistoryGeneration, so tests can
	// simulate a fastTick by incrementing it.
	historyGen uint64
	// historyCalls counts calls to History(), so tests can assert the
	// generation-based cache in handleHistory actually skips re-encoding
	// when the generation hasn't moved.
	historyCalls int
	alerts       alert.Report
	processes    collector.Processes
	openhab      collector.OpenHABStatus
}

func (f *fakeMetrics) Snapshot() collector.Snapshot { return f.snapshot }
func (f *fakeMetrics) History() collector.History {
	f.historyCalls++
	return f.history
}
func (f *fakeMetrics) HistoryGeneration() uint64        { return f.historyGen }
func (f *fakeMetrics) Alerts() alert.Report             { return f.alerts }
func (f *fakeMetrics) Processes() collector.Processes   { return f.processes }
func (f *fakeMetrics) OpenHAB() collector.OpenHABStatus { return f.openhab }

func newTestServer(cfg Config) (*Server, *fakeMetrics) {
	fm := &fakeMetrics{
		snapshot: collector.Snapshot{
			Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			CPU:       collector.CPUUsage{OverallPercent: 12.5},
			CPUValid:  true,
			System:    collector.SystemInfo{KernelVersion: "6.1.0", Distribution: "Raspberry Pi OS", PiModel: "Raspberry Pi 4 Model B"},
		},
		history: collector.History{
			CPUPercent: []collector.HistoryPoint{{Timestamp: time.Now(), Value: 12.5}},
		},
	}
	return New(fm, cfg, nil, nil), fm
}

func TestHandleHealthz(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Body.String() != "ok" {
		t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
	}
}

func TestHandleHealthz_SnapshotStaleness(t *testing.T) {
	const maxStaleness = 30 * time.Second
	// Exact-equality boundary ("age == maxStaleness") can't be pinned
	// deterministically here: the check compares against real wall-clock
	// time.Since, and the microseconds/milliseconds that elapse between
	// setting fm.snapshot.Timestamp and the handler evaluating it would
	// nondeterministically flip an exactly-at-bound age to just over it.
	// A small, fixed margin on either side of the bound pins the handler's
	// strict "age > max" (not ">=") semantics without that flakiness.
	const margin = 100 * time.Millisecond

	tests := []struct {
		name     string
		age      time.Duration
		wantCode int
	}{
		{"snapshot far older than the bound", time.Hour, http.StatusServiceUnavailable},
		{"fresh snapshot well within the bound", 0, http.StatusOK},
		{"snapshot just within the bound", maxStaleness - margin, http.StatusOK},
		{"snapshot just over the bound", maxStaleness + margin, http.StatusServiceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, fm := newTestServer(Config{HealthzMaxStaleness: maxStaleness})
			fm.snapshot.Timestamp = time.Now().Add(-tt.age)

			req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			if rec.Code != tt.wantCode {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantCode)
			}
			if tt.wantCode == http.StatusOK && rec.Body.String() != "ok" {
				t.Fatalf("body = %q, want %q", rec.Body.String(), "ok")
			}
		})
	}
}

func TestHandleMetrics(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got collector.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.CPU.OverallPercent != 12.5 {
		t.Fatalf("CPU.OverallPercent = %v, want 12.5", got.CPU.OverallPercent)
	}
	if got.System.PiModel != "Raspberry Pi 4 Model B" {
		t.Fatalf("System.PiModel = %q", got.System.PiModel)
	}
}

// TestHandleMetrics_CPUValidNotInJSON_AC4 covers issue #243: CPUValid never
// reaches the v1 JSON, so the bodies of both routes are byte-identical for a
// snapshot with and without a CPU reading.
func TestHandleMetrics_CPUValidNotInJSON_AC4(t *testing.T) {
	for _, path := range []string{"/api/v1/metrics", "/api/v1/metrics/cpu"} {
		t.Run("AC4 "+path, func(t *testing.T) {
			s, fm := newTestServer(Config{})
			fm.snapshot.CPU.PerCorePercent = []float64{10, 15}

			fm.snapshot.CPUValid = true
			valid := getBody(t, s, path)
			fm.snapshot.CPUValid = false
			invalid := getBody(t, s, path)

			if valid != invalid {
				t.Errorf("AC4: body differs with CPUValid\ntrue:  %s\nfalse: %s", valid, invalid)
			}
		})
	}
}

// TestHandleMetrics_MemoryValidNotInJSON_AC4 covers issue #246: MemoryValid
// never reaches the v1 JSON, so the bodies of both routes are byte-identical
// for a snapshot with and without a memory reading.
func TestHandleMetrics_MemoryValidNotInJSON_AC4(t *testing.T) {
	for _, path := range []string{"/api/v1/metrics", "/api/v1/metrics/memory"} {
		t.Run("AC4 "+path, func(t *testing.T) {
			s, fm := newTestServer(Config{})
			fm.snapshot.Memory = collector.Memory{TotalBytes: 4137000000, AvailableBytes: 2900000000, UsedPercent: 29.9}
			fm.snapshot.Swap = collector.Swap{TotalBytes: 104857600, UsedBytes: 1024, UsedPercent: 0.1}

			fm.snapshot.MemoryValid = true
			valid := getBody(t, s, path)
			fm.snapshot.MemoryValid = false
			invalid := getBody(t, s, path)

			if valid != invalid {
				t.Errorf("AC4: body differs with MemoryValid\ntrue:  %s\nfalse: %s", valid, invalid)
			}
		})
	}
}

// getBody serves a GET for path and returns the 200 response body.
func getBody(t *testing.T, s *Server, path string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s status = %d, want 200", path, rec.Code)
	}
	return rec.Body.String()
}

func TestHandleHistory(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/history", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got collector.History
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.CPUPercent) != 1 {
		t.Fatalf("expected 1 CPUPercent history point, got %d", len(got.CPUPercent))
	}
}

// TestHandleHistory_CachesUntilGenerationChanges is the regression test for
// the generation-based response cache: repeated requests while
// HistoryGeneration() is unchanged must reuse the cached encoding rather
// than calling History() (and therefore deep-copying every ring buffer)
// again, but a request after the generation advances must see fresh data.
func TestHandleHistory_CachesUntilGenerationChanges(t *testing.T) {
	s, fm := newTestServer(Config{})

	req := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/history", nil))
		return rec
	}

	first := req()
	if first.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", first.Code)
	}
	if fm.historyCalls != 1 {
		t.Fatalf("historyCalls after first request = %d, want 1", fm.historyCalls)
	}

	second := req()
	if second.Code != http.StatusOK {
		t.Fatalf("second request status = %d, want 200", second.Code)
	}
	if fm.historyCalls != 1 {
		t.Fatalf("historyCalls after second request (same generation) = %d, want still 1 (cache should have been reused)", fm.historyCalls)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("cached response body changed between requests with the same generation:\nfirst:  %s\nsecond: %s", first.Body.String(), second.Body.String())
	}

	fm.history.CPUPercent = append(fm.history.CPUPercent, collector.HistoryPoint{Value: 99})
	fm.historyGen++

	third := req()
	if third.Code != http.StatusOK {
		t.Fatalf("third request status = %d, want 200", third.Code)
	}
	if fm.historyCalls != 2 {
		t.Fatalf("historyCalls after generation changed = %d, want 2 (cache should have been invalidated)", fm.historyCalls)
	}
	var got collector.History
	if err := json.Unmarshal(third.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal third response: %v", err)
	}
	if len(got.CPUPercent) != 2 {
		t.Fatalf("expected 2 CPUPercent history points after generation change, got %d", len(got.CPUPercent))
	}
}

// TestHandleHistory_MarshalFailureReturns500 exercises the error branch
// that skips the cache: json.Marshal fails on a NaN float (not
// representable in JSON), which handleHistory must turn into a 500 rather
// than caching a partial/garbage encoding or writing a broken body. The
// ?since= path encodes separately and must behave the same way.
func TestHandleHistory_MarshalFailureReturns500(t *testing.T) {
	for _, query := range []string{"", "since=2026-07-12T18:00:00Z"} {
		name := "full window"
		if query != "" {
			name = query
		}
		t.Run(name, func(t *testing.T) {
			s, fm := newTestServer(Config{})
			fm.history = collector.History{
				CPUPercent: []collector.HistoryPoint{
					{Timestamp: time.Date(2026, 7, 12, 18, 0, 30, 0, time.UTC), Value: math.NaN()},
				},
			}

			rec := getHistory(s, query)
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
			s.historyCacheMu.Lock()
			cached := s.historyCacheJSON
			s.historyCacheMu.Unlock()
			if cached != nil {
				t.Fatal("expected no cached response after a marshal failure")
			}
		})
	}
}

// historyFixture is a history whose scalar series and per-device series
// all sample at the same three timestamps (base, +5s, +10s), the way a
// fastTick records them, plus a device seen only at the oldest of them.
func historyFixture(base time.Time) collector.History {
	pts := func(offsets ...time.Duration) []collector.HistoryPoint {
		out := make([]collector.HistoryPoint, len(offsets))
		for i, off := range offsets {
			out[i] = collector.HistoryPoint{Timestamp: base.Add(off), Value: float64(i) + 0.5}
		}
		return out
	}
	return collector.History{
		CPUPercent:        pts(0, 5*time.Second, 10*time.Second),
		Load1:             pts(0, 5*time.Second, 10*time.Second),
		Temperature:       pts(0, 5*time.Second, 10*time.Second),
		MemoryUsedPercent: pts(0, 5*time.Second, 10*time.Second),
		DiskUsedPercent: map[string][]collector.HistoryPoint{
			"/":     pts(0, 5*time.Second, 10*time.Second),
			"/boot": pts(0),
		},
		NetworkRxBytesPerSec: map[string][]collector.HistoryPoint{
			"eth0": pts(0, 5*time.Second, 10*time.Second),
		},
	}
}

// getHistory issues a GET against the history endpoint with the given raw
// query string (empty for none) and returns the recorder.
func getHistory(s *Server, query string) *httptest.ResponseRecorder {
	url := "/api/v1/metrics/history"
	if query != "" {
		url += "?" + query
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func decodeHistory(t *testing.T, rec *httptest.ResponseRecorder) collector.History {
	t.Helper()
	var got collector.History
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v (body %q)", err, rec.Body.String())
	}
	return got
}

// TestHandleHistory_WithoutSinceReturnsFullWindow is the backwards
// compatibility regression test for the ?since= parameter: a request that
// doesn't use it must still get the complete retained window, exactly as
// before the parameter existed.
func TestHandleHistory_WithoutSinceReturnsFullWindow(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)

	rec := getHistory(s, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeHistory(t, rec)
	if len(got.CPUPercent) != 3 || len(got.Load1) != 3 || len(got.Temperature) != 3 || len(got.MemoryUsedPercent) != 3 {
		t.Fatalf("expected every scalar series in full, got %+v", got)
	}
	if len(got.DiskUsedPercent) != 2 || len(got.DiskUsedPercent["/boot"]) != 1 {
		t.Fatalf("expected both disks in full, got %+v", got.DiskUsedPercent)
	}
	if len(got.NetworkRxBytesPerSec["eth0"]) != 3 {
		t.Fatalf("expected the full network series, got %+v", got.NetworkRxBytesPerSec)
	}
}

func TestHandleHistory_SinceReturnsOnlyNewerPoints(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)

	rec := getHistory(s, "since="+base.Add(2*time.Second).Format(time.RFC3339))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeHistory(t, rec)
	if len(got.CPUPercent) != 2 {
		t.Fatalf("CPUPercent = %d points, want 2", len(got.CPUPercent))
	}
	if !got.CPUPercent[0].Timestamp.Equal(base.Add(5 * time.Second)) {
		t.Fatalf("oldest returned point = %v, want %v", got.CPUPercent[0].Timestamp, base.Add(5*time.Second))
	}
	// Per-device series are filtered per key, and a device with nothing new
	// is omitted rather than sent as an empty array.
	if len(got.DiskUsedPercent) != 1 {
		t.Fatalf("DiskUsedPercent = %+v, want only the device with newer points", got.DiskUsedPercent)
	}
	if _, ok := got.DiskUsedPercent["/boot"]; ok {
		t.Fatal("expected /boot to be omitted: it has no points newer than since")
	}
	if len(got.NetworkRxBytesPerSec["eth0"]) != 2 {
		t.Fatalf("NetworkRxBytesPerSec[eth0] = %+v, want 2 points", got.NetworkRxBytesPerSec["eth0"])
	}
}

// openHABHistoryPoints builds points at base plus each offset.
func openHABHistoryPoints(base time.Time, offsets ...time.Duration) []collector.HistoryPoint {
	out := make([]collector.HistoryPoint, len(offsets))
	for i, off := range offsets {
		out[i] = collector.HistoryPoint{Timestamp: base.Add(off), Value: float64(100 + i)}
	}
	return out
}

var openHABHistoryBase = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

func TestHandleHistory_OpenHABKeys(t *testing.T) {
	t.Run("AC4 keys absent while series empty", checkOpenHABKeysAbsent)
	t.Run("AC4 keys present as point arrays", checkOpenHABKeysPresent)
	t.Run("AC4 other keys unchanged", checkOpenHABKeysLeaveOthers)
	t.Run("AC4 401 without key when api_key is set", checkOpenHABKeysNeedAPIKey)
	t.Run("AC4 since keeps the newer points of each key and omits a key without any", checkOpenHABKeysSince)
}

func checkOpenHABKeysAbsent(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(openHABHistoryBase)

	rec := getHistory(s, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "openhab") {
		t.Fatalf("body = %s, want no openhab key while the series are empty", rec.Body.String())
	}
}

func checkOpenHABKeysPresent(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.history = collector.History{
		OpenHABRSSBytes:      openHABHistoryPoints(openHABHistoryBase, 0, time.Minute),
		OpenHABHeapUsedBytes: openHABHistoryPoints(openHABHistoryBase, 0),
		OpenHABRestarts:      openHABHistoryPoints(openHABHistoryBase, time.Minute),
	}

	rec := getHistory(s, "")

	var raw map[string][]map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	for key, wantLen := range map[string]int{"openhab_rss_bytes": 2, "openhab_heap_used_bytes": 1, "openhab_restarts": 1} {
		points := raw[key]
		if len(points) != wantLen {
			t.Fatalf("%s = %v, want an array of %d points in %s", key, points, wantLen, rec.Body.String())
		}
		if _, hasT := points[0]["t"]; !hasT {
			t.Fatalf("%s[0] = %v, want the point shape of the other series (t, v)", key, points[0])
		}
	}
}

// checkOpenHABKeysLeaveOthers serves the same history twice, without and
// with openHAB series (on two servers: the handler caches per generation),
// and compares every key but the openHAB ones.
func checkOpenHABKeysLeaveOthers(t *testing.T) {
	plain, fmPlain := newTestServer(Config{})
	fmPlain.history = historyFixture(openHABHistoryBase)
	with, fmWith := newTestServer(Config{})
	fmWith.history = historyFixture(openHABHistoryBase)
	fmWith.history.OpenHABRSSBytes = openHABHistoryPoints(openHABHistoryBase, 0, time.Minute)
	fmWith.history.OpenHABRestarts = openHABHistoryPoints(openHABHistoryBase, 0)

	want := getHistoryBody(t, plain)
	got := getHistoryBody(t, with)

	for _, key := range []string{"openhab_rss_bytes", "openhab_restarts"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("body lacks %s", key)
		}
		delete(got, key)
	}
	if gotJSON, wantJSON := mustMarshal(t, got), mustMarshal(t, want); gotJSON != wantJSON {
		t.Fatalf("other keys with openHAB series = %s, want unchanged %s", gotJSON, wantJSON)
	}
}

func checkOpenHABKeysNeedAPIKey(t *testing.T) {
	s, fm := newTestServer(Config{APIKey: "secret123"})
	fm.history = collector.History{OpenHABRSSBytes: openHABHistoryPoints(openHABHistoryBase, 0)}

	rec := getHistory(s, "")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "openhab") {
		t.Fatalf("body = %s, want no openHAB data without the key", rec.Body.String())
	}
}

func checkOpenHABKeysSince(t *testing.T) {
	base := openHABHistoryBase
	s, fm := newTestServer(Config{})
	fm.history = collector.History{
		OpenHABRSSBytes:      openHABHistoryPoints(base, 0, time.Minute, 2*time.Minute),
		OpenHABHeapUsedBytes: openHABHistoryPoints(base, 0, time.Minute, 2*time.Minute),
		OpenHABRestarts:      openHABHistoryPoints(base, 0),
	}

	rec := getHistory(s, "since="+base.Add(30*time.Second).Format(time.RFC3339))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got := decodeHistory(t, rec)
	if len(got.OpenHABRSSBytes) != 2 || len(got.OpenHABHeapUsedBytes) != 2 {
		t.Fatalf("rss, heap points = %d, %d, want 2 each", len(got.OpenHABRSSBytes), len(got.OpenHABHeapUsedBytes))
	}
	if !got.OpenHABRSSBytes[0].Timestamp.Equal(base.Add(time.Minute)) {
		t.Fatalf("oldest returned RSS point = %v, want %v", got.OpenHABRSSBytes[0].Timestamp, base.Add(time.Minute))
	}
	if strings.Contains(rec.Body.String(), "openhab_restarts") {
		t.Fatalf("body = %s, want no openhab_restarts without a newer point", rec.Body.String())
	}
}

// TestHandleHistory_SinceExcludesExactBoundary pins the strictly-newer
// rule at the HTTP layer: a client passing back the timestamp of the
// newest point it holds must not receive that point a second time.
func TestHandleHistory_SinceExcludesExactBoundary(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)

	got := decodeHistory(t, getHistory(s, "since="+base.Add(10*time.Second).Format(time.RFC3339)))
	if len(got.CPUPercent) != 0 {
		t.Fatalf("CPUPercent = %+v, want no points (the boundary point is excluded)", got.CPUPercent)
	}
}

func TestHandleHistory_SinceInFutureReturnsEmptySeries(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)

	rec := getHistory(s, "since="+base.Add(time.Hour).Format(time.RFC3339))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (a since in the future is empty, not an error)", rec.Code)
	}
	if body := rec.Body.String(); strings.Contains(body, "disk_used_percent") || strings.Contains(body, "network_rx_bytes_per_sec") {
		t.Fatalf("expected device maps to be omitted entirely, got %s", body)
	}
	got := decodeHistory(t, rec)
	if len(got.CPUPercent) != 0 || len(got.Load1) != 0 {
		t.Fatalf("expected empty series, got %+v", got)
	}
}

func TestHandleHistory_SinceOlderThanWindowReturnsEverything(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)

	got := decodeHistory(t, getHistory(s, "since="+base.Add(-time.Hour).Format(time.RFC3339)))
	if len(got.CPUPercent) != 3 || len(got.DiskUsedPercent) != 2 {
		t.Fatalf("expected the full window, got %+v", got)
	}
}

// TestHandleHistory_InvalidSinceReturns400 checks that a timestamp the
// server can't parse is rejected loudly rather than silently falling back
// to the full window, which would leave a broken client re-fetching
// everything without ever noticing.
func TestHandleHistory_InvalidSinceReturns400(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	for _, raw := range []string{
		"not-a-timestamp",
		"1752345600",           // Unix seconds, not RFC 3339
		"2026-07-12 18:31:00",  // space instead of T, no offset
		"2026-07-12T18:31:00",  // no offset
		"2026-13-12T18:31:00Z", // month out of range
	} {
		t.Run(raw, func(t *testing.T) {
			s, fm := newTestServer(Config{})
			fm.history = historyFixture(base)

			rec := getHistory(s, "since="+url.QueryEscape(raw))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400", rec.Code)
			}
			if strings.Contains(rec.Body.String(), "cpu_percent") {
				t.Fatalf("expected an error, not a history payload: %s", rec.Body.String())
			}
		})
	}
}

// TestHandleHistory_SinceRequestsReuseCachedCopy is the performance
// regression test for the ?since= path: the deep copy History() makes is
// cached per generation like the full-window encoding is, and a request
// that only wants the delta must not trigger a full-window serialisation.
func TestHandleHistory_SinceRequestsReuseCachedCopy(t *testing.T) {
	base := time.Date(2026, 7, 12, 18, 0, 0, 0, time.UTC)
	s, fm := newTestServer(Config{})
	fm.history = historyFixture(base)
	since := "since=" + base.Format(time.RFC3339)

	if rec := getHistory(s, since); rec.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec.Code)
	}
	if fm.historyCalls != 1 {
		t.Fatalf("historyCalls after first request = %d, want 1", fm.historyCalls)
	}
	if rec := getHistory(s, since); rec.Code != http.StatusOK {
		t.Fatalf("second request status = %d, want 200", rec.Code)
	}
	if fm.historyCalls != 1 {
		t.Fatalf("historyCalls after second request (same generation) = %d, want still 1", fm.historyCalls)
	}

	s.historyCacheMu.Lock()
	cachedJSON := s.historyCacheJSON
	s.historyCacheMu.Unlock()
	if cachedJSON != nil {
		t.Fatal("?since= requests should not have serialised the full window")
	}

	// A full-window request at the same generation reuses the cached copy
	// and only then pays for the full encoding.
	if rec := getHistory(s, ""); rec.Code != http.StatusOK {
		t.Fatalf("full-window request status = %d, want 200", rec.Code)
	}
	if fm.historyCalls != 1 {
		t.Fatalf("historyCalls after full-window request = %d, want still 1", fm.historyCalls)
	}

	fm.historyGen++
	if rec := getHistory(s, since); rec.Code != http.StatusOK {
		t.Fatalf("request after generation change status = %d, want 200", rec.Code)
	}
	if fm.historyCalls != 2 {
		t.Fatalf("historyCalls after generation change = %d, want 2 (cache should have been invalidated)", fm.historyCalls)
	}
}

func TestHandleAlerts(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.alerts = alert.Report{
		Enabled: true,
		States: []alert.State{
			{Metric: "cpu", Level: alert.LevelCrit, Value: 99},
		},
		Events: []alert.Event{
			{Metric: "cpu", Kind: alert.KindFired, From: alert.LevelOK, To: alert.LevelCrit, Value: 99},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got alert.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if !got.Enabled {
		t.Fatal("expected enabled=true")
	}
	if len(got.States) != 1 || got.States[0].Level != alert.LevelCrit {
		t.Fatalf("unexpected states: %+v", got.States)
	}
	if len(got.Events) != 1 || got.Events[0].Kind != alert.KindFired {
		t.Fatalf("unexpected events: %+v", got.Events)
	}
}

func TestHandleAlerts_GatedByAPIKey(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/alerts", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", rec.Code)
	}
}

// TestHandleProcesses is the acceptance test for issue #16's HTTP surface:
// GET /api/v1/processes, once enabled, serves exactly the top-N rankings
// the collector computed.
func TestHandleProcesses(t *testing.T) {
	s, fm := newTestServer(Config{ProcessesEnabled: true})
	fm.processes = collector.Processes{
		ByCPU:    []collector.Process{{PID: 100, Name: "hog", CPUPercent: 60, RSSBytes: 51200}},
		ByMemory: []collector.Process{{PID: 200, Name: "leaky", CPUPercent: 5, RSSBytes: 1048576}},
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/processes", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got collector.Processes
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got.ByCPU) != 1 || got.ByCPU[0].PID != 100 {
		t.Fatalf("ByCPU = %+v, want the fake's single pid-100 entry", got.ByCPU)
	}
	if len(got.ByMemory) != 1 || got.ByMemory[0].PID != 200 {
		t.Fatalf("ByMemory = %+v, want the fake's single pid-200 entry", got.ByMemory)
	}
}

// TestHandleProcesses_DisabledByDefault pins that GET /api/v1/processes
// 404s when processes_enabled is left at its default (false), rather than
// existing but always serving an empty ranking — the same
// register-only-when-enabled pattern as GET /metrics.
func TestHandleProcesses_DisabledByDefault(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/processes", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 while processes_enabled is false", rec.Code)
	}
}

func TestHandleProcesses_GatedByAPIKey(t *testing.T) {
	s, _ := newTestServer(Config{ProcessesEnabled: true, APIKey: "secret123"})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/processes", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", rec.Code)
	}
}

func TestHandleConfig(t *testing.T) {
	s, _ := newTestServer(Config{
		Client: ClientConfig{
			Version:              "v1.2.3",
			PollIntervalSeconds:  5,
			HistoryWindowMinutes: 60,
			NetworkEnabled:       true,
			Thresholds:           config.Thresholds{TemperatureWarnC: 60, TemperatureCritC: 75},
		},
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/config", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var got ClientConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.PollIntervalSeconds != 5 || !got.NetworkEnabled || got.Thresholds.TemperatureCritC != 75 {
		t.Fatalf("unexpected config: %+v", got)
	}
	if got.Version != "v1.2.3" {
		t.Fatalf("Version = %q, want %q", got.Version, "v1.2.3")
	}
	// The dashboard needs the retention window to bound the history it
	// accumulates locally from ?since= deltas.
	if got.HistoryWindowMinutes != 60 {
		t.Fatalf("HistoryWindowMinutes = %v, want 60", got.HistoryWindowMinutes)
	}
}

// TestHandleServerStats_CountsAcrossRoutesAndStatusClasses is the
// acceptance test for issue #43: counters must increment as expected under
// httptest traffic, broken down both by route and by response status class.
func TestHandleServerStats_CountsAcrossRoutesAndStatusClasses(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})

	get := func(path, apiKey string) int {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		if apiKey != "" {
			req.Header.Set("X-Api-Key", apiKey)
		}
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec.Code
	}

	if code := get("/healthz", ""); code != http.StatusOK {
		t.Fatalf("GET /healthz status = %d, want 200", code)
	}
	if code := get("/api/v1/metrics", "secret123"); code != http.StatusOK {
		t.Fatalf("GET /api/v1/metrics status = %d, want 200", code)
	}
	if code := get("/api/v1/metrics", "secret123"); code != http.StatusOK {
		t.Fatalf("GET /api/v1/metrics status = %d, want 200", code)
	}
	// An unauthorized request still counts, at its actual 401 status.
	if code := get("/api/v1/alerts", ""); code != http.StatusUnauthorized {
		t.Fatalf("GET /api/v1/alerts without key status = %d, want 401", code)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/serverstats", nil)
	req.Header.Set("X-Api-Key", "secret123")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/serverstats status = %d, want 200", rec.Code)
	}

	var got serverStatsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	// The 4 requests made above; a request's own counters are only recorded
	// once its response has already been written (withLogging records after
	// the handler returns), so this /api/v1/serverstats call can never see
	// itself in the snapshot it returns.
	if got.Total != 4 {
		t.Fatalf("Total = %d, want 4", got.Total)
	}
	if got.ByRoute["/healthz"] != 1 {
		t.Fatalf("ByRoute[/healthz] = %d, want 1", got.ByRoute["/healthz"])
	}
	if got.ByRoute["/api/v1/metrics"] != 2 {
		t.Fatalf("ByRoute[/api/v1/metrics] = %d, want 2", got.ByRoute["/api/v1/metrics"])
	}
	if got.ByRoute["/api/v1/alerts"] != 1 {
		t.Fatalf("ByRoute[/api/v1/alerts] = %d, want 1", got.ByRoute["/api/v1/alerts"])
	}
	if got.ByRoute["/api/v1/serverstats"] != 0 {
		t.Fatalf("ByRoute[/api/v1/serverstats] = %d, want 0 (not yet recorded when this response was built)", got.ByRoute["/api/v1/serverstats"])
	}
	if got.ByStatusClass["2xx"] != 3 {
		t.Fatalf("ByStatusClass[2xx] = %d, want 3", got.ByStatusClass["2xx"])
	}
	if got.ByStatusClass["4xx"] != 1 {
		t.Fatalf("ByStatusClass[4xx] = %d, want 1", got.ByStatusClass["4xx"])
	}
}

// TestHandleServerStats_GatedByAPIKey guards against the new endpoint
// bypassing the same auth gating every other /api/v1/... route applies.
func TestHandleServerStats_GatedByAPIKey(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/serverstats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", rec.Code)
	}
}

// TestHandlePrometheusMetrics_NotRegisteredByDefault guards the "opt-in,
// off by default" acceptance criterion from issue #6: without
// prometheus_enabled, GET /metrics must not exist at all (404), rather than
// existing but empty.
func TestHandlePrometheusMetrics_NotRegisteredByDefault(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (route should not be registered when PrometheusEnabled is false)", rec.Code)
	}
}

// TestHandlePrometheusMetrics_DisabledRequestsStillCountedUnderOwnBucket
// pins the docs/API.md claim that a scrape against a disabled endpoint
// still shows up under the "/metrics" serverstats bucket (with a matching
// 4xx count) rather than staying invisible at 0: routeBucket classifies
// r.URL.Path itself, so the bucket doesn't depend on which handler — or
// the mux's 404 fallback — ended up serving the request.
func TestHandlePrometheusMetrics_DisabledRequestsStillCountedUnderOwnBucket(t *testing.T) {
	s, _ := newTestServer(Config{})

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("GET /metrics status = %d, want 404", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/serverstats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var got serverStatsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ByRoute["/metrics"] != 2 {
		t.Fatalf("ByRoute[/metrics] = %d, want 2 (counted even though the route 404s)", got.ByRoute["/metrics"])
	}
	if got.ByStatusClass["4xx"] != 2 {
		t.Fatalf("ByStatusClass[4xx] = %d, want 2", got.ByStatusClass["4xx"])
	}
}

func TestHandlePrometheusMetrics(t *testing.T) {
	s, _ := newTestServer(Config{PrometheusEnabled: true})
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != prometheusContentType {
		t.Fatalf("Content-Type = %q, want %q", ct, prometheusContentType)
	}
	if !strings.Contains(rec.Body.String(), "pimonitor_cpu_usage_percent 12.5") {
		t.Fatalf("body missing expected CPU gauge line, got:\n%s", rec.Body.String())
	}
}

// TestHandlePrometheusMetrics_GatedByAPIKey ensures enabling the endpoint
// does not create a way to bypass an already-configured API key.
func TestHandlePrometheusMetrics_GatedByAPIKey(t *testing.T) {
	s, _ := newTestServer(Config{PrometheusEnabled: true, APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.Header.Set("X-Api-Key", "secret123")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with correct key = %d, want 200", rec.Code)
	}
}

// TestHandlePrometheusMetrics_CountedInItsOwnServerStatsBucket is the
// regression test for the PR #146 review: /metrics has no /api/v1/ prefix
// and, before routeBucket gained a case for it, fell through to the
// "static" bucket shared with dashboard asset requests — silently mixing
// Prometheus scrape volume into a counter meant for something else. A
// scrape must land in its own "/metrics" key instead.
func TestHandlePrometheusMetrics_CountedInItsOwnServerStatsBucket(t *testing.T) {
	s, _ := newTestServer(Config{PrometheusEnabled: true})

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /metrics status = %d, want 200", rec.Code)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/serverstats", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	var got serverStatsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if got.ByRoute["/metrics"] != 3 {
		t.Fatalf("ByRoute[/metrics] = %d, want 3", got.ByRoute["/metrics"])
	}
	if got.ByRoute["static"] != 0 {
		t.Fatalf("ByRoute[static] = %d, want 0 (the 3 /metrics requests must not have landed here)", got.ByRoute["static"])
	}
}

func TestAPIKey_RequiredWhenConfigured(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})

	// No key: rejected.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status without key = %d, want 401", rec.Code)
	}

	// Wrong key: rejected.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("X-Api-Key", "wrong")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status with wrong key = %d, want 401", rec.Code)
	}

	// Correct key via X-Api-Key: allowed.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("X-Api-Key", "secret123")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with correct X-Api-Key = %d, want 200", rec.Code)
	}

	// Correct key via Authorization: Bearer: allowed.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Authorization", "Bearer secret123")
	rec = httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status with correct Bearer token = %d, want 200", rec.Code)
	}
}

func TestAPIKey_NotRequiredByDefault(t *testing.T) {
	s, _ := newTestServer(Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (no API key configured)", rec.Code)
	}
}

func TestHealthz_NotGatedByAPIKey(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (healthz should not require API key)", rec.Code)
	}
}

// metricsSystemObject requests GET /api/v1/metrics with system as the
// snapshot's SystemInfo and returns the decoded "system" object.
func metricsSystemObject(t *testing.T, system collector.SystemInfo) map[string]any {
	t.Helper()
	s, fm := newTestServer(Config{})
	fm.snapshot.System = system
	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	sys, ok := body["system"].(map[string]any)
	if !ok {
		t.Fatalf("system = %#v, want a JSON object", body["system"])
	}
	return sys
}

func TestHandleMetrics_Hostname(t *testing.T) {
	base := collector.SystemInfo{
		KernelVersion: "6.1.0",
		Distribution:  "Raspberry Pi OS",
		PiModel:       "Raspberry Pi 4 Model B",
		CPUModel:      "Cortex-A72",
	}
	existing := map[string]any{
		"kernel_version": "6.1.0",
		"distribution":   "Raspberry Pi OS",
		"pi_model":       "Raspberry Pi 4 Model B",
		"cpu_model":      "Cortex-A72",
	}

	t.Run("AC1 system.hostname present and the four existing system keys unchanged", func(t *testing.T) {
		withName := base
		withName.Hostname = "raspberrypi"
		want := map[string]any{"hostname": "raspberrypi"}
		for k, v := range existing {
			want[k] = v
		}

		got := metricsSystemObject(t, withName)

		if !reflect.DeepEqual(got, want) {
			t.Fatalf("system = %v, want %v", got, want)
		}
	})

	t.Run("AC2 key omitted when the host name is empty", func(t *testing.T) {
		got := metricsSystemObject(t, base)

		if _, present := got["hostname"]; present {
			t.Fatalf("system.hostname present = %v, want the key omitted", got["hostname"])
		}
		if !reflect.DeepEqual(got, existing) {
			t.Fatalf("system = %v, want %v", got, existing)
		}
	})
}

// getOpenHAB serves one GET /api/v1/openhab, with apiKey as X-Api-Key when
// non-empty, against a server whose fake returns status.
func getOpenHAB(cfg Config, status collector.OpenHABStatus, apiKey string) *httptest.ResponseRecorder {
	s, fm := newTestServer(cfg)
	fm.openhab = status
	req := httptest.NewRequest(http.MethodGet, "/api/v1/openhab", nil)
	if apiKey != "" {
		req.Header.Set("X-Api-Key", apiKey)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func openHABOKStatus() collector.OpenHABStatus {
	at := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	level, items, addons := 100, 440, 29
	uptime := uint64(362335)
	percent := 33.5
	return collector.OpenHABStatus{
		State:               collector.OpenHABStateOK,
		PollIntervalSeconds: 60,
		HistoryWindowHours:  24,
		APITokenConfigured:  true,
		CollectedAt:         &at,
		Version:             "5.2.4",
		StartLevel:          &level,
		UptimeSeconds:       &uptime,
		Process:             &collector.OpenHABProcess{PID: 938, RSSBytes: 1417166848, RSSPercentOfRAM: &percent},
		JVM:                 &collector.OpenHABJVM{HeapUsedBytes: 664407376, HeapTotalBytes: 729808896},
		Things: &collector.OpenHABThings{Total: 2, ByStatus: map[string]int{"ONLINE": 1, "OFFLINE": 1},
			NotOnline: []collector.OpenHABThing{{UID: "hue:device:b1:l1", Label: "<b>Lamp</b>", Status: "OFFLINE", StatusDetail: "NONE"}}},
		ItemsCount:      &items,
		AddonsInstalled: &addons,
		Refused:         []string{"addons"},
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func TestHandleOpenHAB_AC9(t *testing.T) {
	const key = "secret123"
	tests := []struct {
		name     string
		cfg      Config
		status   collector.OpenHABStatus
		apiKey   string
		wantCode int
		wantBody string
	}{
		{"AC9 S7 serves the collector status", Config{OpenHABEnabled: true}, openHABOKStatus(), "",
			http.StatusOK, mustMarshal(t, openHABOKStatus())},
		{"AC9 S5 not_installed", Config{OpenHABEnabled: true},
			collector.OpenHABStatus{State: collector.OpenHABStateNotInstalled, PollIntervalSeconds: 60, HistoryWindowHours: 24}, "",
			http.StatusOK, `{"state":"not_installed","poll_interval_seconds":60,"history_window_hours":24,"api_token_configured":false}`},
		{"AC9 S9 pending", Config{OpenHABEnabled: true},
			collector.OpenHABStatus{State: collector.OpenHABStatePending, PollIntervalSeconds: 120, HistoryWindowHours: 24, APITokenConfigured: true}, "",
			http.StatusOK, `{"state":"pending","poll_interval_seconds":120,"history_window_hours":24,"api_token_configured":true}`},
		{"AC9 API key required", Config{OpenHABEnabled: true, APIKey: key}, openHABOKStatus(), "",
			http.StatusUnauthorized, ""},
		{"AC9 API key accepted", Config{OpenHABEnabled: true, APIKey: key}, openHABOKStatus(), key,
			http.StatusOK, mustMarshal(t, openHABOKStatus())},
		{"AC9 S6 404 when disabled", Config{}, openHABOKStatus(), "", http.StatusNotFound, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec := getOpenHAB(tc.cfg, tc.status, tc.apiKey)

			checkOpenHABResponse(t, rec, tc.wantCode, tc.wantBody)
		})
	}
}

// checkOpenHABResponse asserts the status code and, for a 200, the exact
// JSON body and the API chain's headers.
func checkOpenHABResponse(t *testing.T, rec *httptest.ResponseRecorder, wantCode int, wantBody string) {
	t.Helper()
	if rec.Code != wantCode {
		t.Fatalf("status = %d, want %d", rec.Code, wantCode)
	}
	if wantCode != http.StatusOK {
		return
	}
	if got := strings.TrimSuffix(rec.Body.String(), "\n"); got != wantBody {
		t.Fatalf("body = %s, want %s", got, wantBody)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", got)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", got)
	}
}

// getHistoryBody returns the history response as a generic JSON object.
func getHistoryBody(t *testing.T, s *Server) map[string]json.RawMessage {
	t.Helper()
	var out map[string]json.RawMessage
	rec := getHistory(s, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
	}
	return out
}

func TestHandleOpenHAB_HistoryWindowHours_AC9(t *testing.T) {
	tests := []struct {
		name  string
		state string
		hours int
	}{
		{"AC9 ok carries the configured window", collector.OpenHABStateOK, 168},
		{"AC9 pending carries the configured window", collector.OpenHABStatePending, 1},
		{"AC9 not_installed carries the configured window", collector.OpenHABStateNotInstalled, 72},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			status := collector.OpenHABStatus{State: tc.state, PollIntervalSeconds: 60, HistoryWindowHours: tc.hours}

			rec := getOpenHAB(Config{OpenHABEnabled: true}, status, "")

			var got map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatalf("unmarshal %q: %v", rec.Body.String(), err)
			}
			if v, ok := got["history_window_hours"].(float64); !ok || int(v) != tc.hours {
				t.Fatalf("history_window_hours = %v, want %d in %s", got["history_window_hours"], tc.hours, rec.Body.String())
			}
		})
	}
}

func TestHandleOpenHAB_ServerStatsBucket_AC9(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		wantCode int
	}{
		{"AC9 enabled", Config{OpenHABEnabled: true}, http.StatusOK},
		{"AC9 disabled", Config{}, http.StatusNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newTestServer(tc.cfg)

			codes := []int{serveGet(s, "/api/v1/openhab"), serveGet(s, "/api/v1/openhab")}
			stats := serveGet(s, "/api/v1/serverstats")

			if codes[0] != tc.wantCode || codes[1] != tc.wantCode || stats != http.StatusOK {
				t.Fatalf("status codes = %v, serverstats %d, want %d twice, 200", codes, stats, tc.wantCode)
			}
			got := s.stats.snapshot().ByRoute
			if n, ok := got["/api/v1/openhab"]; !ok || n != 2 {
				t.Fatalf("by_route[/api/v1/openhab] = %d (present %v), want 2; by_route = %v", n, ok, got)
			}
			if got[bucketOtherAPI] != 0 {
				t.Fatalf("by_route[%s] = %d, want 0", bucketOtherAPI, got[bucketOtherAPI])
			}
		})
	}
}

// serveGet serves one GET of path and returns the status code.
func serveGet(s *Server, path string) int {
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec.Code
}

func TestHandleOpenHAB_TokenNotServed_AC8(t *testing.T) {
	const sentinel = "oh.sentinelTOKENvalue.4711"
	c := collector.NewOpenHABCollector(collector.OpenHABConfig{
		Enabled:  true,
		BaseURL:  "http://127.0.0.1:1",
		Token:    sentinel,
		Interval: time.Minute,
		Root:     t.TempDir(),
	}, func() uint64 { return 0 }, nil)

	rec := getOpenHAB(Config{OpenHABEnabled: true}, c.Status(), "")

	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, `"api_token_configured":true`) {
		t.Fatalf("status, body = %d, %s, want 200 with api_token_configured true", rec.Code, body)
	}
	if strings.Contains(body, sentinel) {
		t.Fatalf("body = %s, want no token", body)
	}
}

// promGet serves one GET /metrics against a server whose snapshot is fixed and
// whose openHAB status is status.
func promGet(t *testing.T, status collector.OpenHABStatus) (string, collector.Snapshot) {
	t.Helper()
	s, fm := newTestServer(Config{PrometheusEnabled: true})
	fm.openhab = status
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	return rec.Body.String(), fm.snapshot
}

func TestHandlePrometheusMetrics_OpenHABNotInstalledKeepsBody_AC1(t *testing.T) {
	for _, state := range []string{collector.OpenHABStateNotInstalled, collector.OpenHABStatePending, ""} {
		t.Run("AC1 state "+state, func(t *testing.T) {
			status := collector.OpenHABStatus{State: state, Version: "5.2.4", ItemsCount: new(int)}
			body, snap := promGet(t, status)
			if want := string(renderPrometheusMetrics(snap)); body != want {
				t.Fatalf("body differs from renderPrometheusMetrics:\ngot:\n%s\nwant:\n%s", body, want)
			}
			if strings.Contains(body, "pimonitor_openhab_") {
				t.Fatalf("body names an openHAB metric:\n%s", body)
			}
		})
	}
}

func TestHandlePrometheusMetrics_OpenHABAfterExistingFamilies_AC6(t *testing.T) {
	status := collector.OpenHABStatus{State: collector.OpenHABStateOK, Version: "5.2.4"}
	body, snap := promGet(t, status)
	existing := string(renderPrometheusMetrics(snap))
	if !strings.HasPrefix(body, existing) {
		t.Fatalf("AC6 body does not start with the existing families:\n%s", body)
	}
	rest := strings.TrimPrefix(body, existing)
	if !strings.HasPrefix(rest, "# HELP pimonitor_openhab_up ") {
		t.Fatalf("AC6 openHAB part = %q, want it to start with the up family", rest)
	}
	if !strings.Contains(rest, "pimonitor_openhab_up 1\n") {
		t.Fatalf("AC6 openHAB part lacks up 1:\n%s", rest)
	}
	if strings.Contains(existing, "pimonitor_openhab_") {
		t.Fatalf("AC6 existing families name an openHAB metric:\n%s", existing)
	}
}

// nanMetricsServer returns a server whose snapshot cannot be encoded as JSON:
// json.Marshal rejects the NaN.
func nanMetricsServer(log *slog.Logger) *Server {
	fm := &fakeMetrics{}
	fm.snapshot.CPU.OverallPercent = math.NaN()
	return New(fm, Config{}, nil, log)
}

func serveGetEncoded(s *Server, path, acceptEncoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func decodedBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	if rec.Header().Get("Content-Encoding") != "gzip" {
		return rec.Body.String()
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	out, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("gunzip body: %v", err)
	}
	return string(out)
}

func TestWriteJSON_EncodeFailureReturns500_AC1(t *testing.T) {
	tests := []struct {
		name, path, acceptEncoding string
	}{
		{"AC1 GET /api/v1/metrics", "/api/v1/metrics", ""},
		{"AC1 GET /api/v1/metrics/cpu", "/api/v1/metrics/cpu", ""},
		{"AC1 GET /api/v1/metrics gzip", "/api/v1/metrics", "gzip"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := nanMetricsServer(nil)

			rec := serveGetEncoded(s, tt.path, tt.acceptEncoding)

			if rec.Code != http.StatusInternalServerError {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
			}
			if got, want := rec.Header().Get("Content-Type"), "text/plain; charset=utf-8"; got != want {
				t.Errorf("Content-Type = %q, want %q", got, want)
			}
			if got, want := rec.Header().Get("Content-Encoding"), tt.acceptEncoding; got != want {
				t.Errorf("Content-Encoding = %q, want %q", got, want)
			}
			if got, want := decodedBody(t, rec), "internal error\n"; got != want {
				t.Errorf("body = %q, want %q", got, want)
			}
		})
	}
}

func TestWriteJSON_EncodeFailureCountedAs5xx_AC3(t *testing.T) {
	s := nanMetricsServer(nil)
	serveGetEncoded(s, "/api/v1/metrics", "")
	s.metrics.(*fakeMetrics).snapshot.CPU.OverallPercent = 1

	rec := serveGetEncoded(s, "/api/v1/serverstats", "")

	var got serverStatsSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal serverstats %q: %v", rec.Body.String(), err)
	}
	if got.ByStatusClass["5xx"] != 1 {
		t.Errorf("5xx = %d, want 1", got.ByStatusClass["5xx"])
	}
	if got.ByStatusClass["2xx"] != 0 {
		t.Errorf("2xx = %d, want 0", got.ByStatusClass["2xx"])
	}
}
