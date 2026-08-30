package httpapi

import (
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
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
}

func (f *fakeMetrics) Snapshot() collector.Snapshot { return f.snapshot }
func (f *fakeMetrics) History() collector.History {
	f.historyCalls++
	return f.history
}
func (f *fakeMetrics) HistoryGeneration() uint64 { return f.historyGen }
func (f *fakeMetrics) Alerts() alert.Report      { return f.alerts }

func newTestServer(cfg Config) (*Server, *fakeMetrics) {
	fm := &fakeMetrics{
		snapshot: collector.Snapshot{
			Timestamp: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC),
			CPU:       collector.CPUUsage{OverallPercent: 12.5},
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
// 4xx count) rather than staying invisible at 0: withLogging buckets by
// request path before the mux decides 404, so counting happens regardless
// of whether the route is registered.
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
