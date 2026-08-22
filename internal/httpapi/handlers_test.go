package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/collector"
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
			Version:             "v1.2.3",
			PollIntervalSeconds: 5,
			NetworkEnabled:      true,
			Thresholds:          Thresholds{TemperatureWarnC: 60, TemperatureCritC: 75},
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
