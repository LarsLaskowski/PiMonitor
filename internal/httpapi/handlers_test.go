package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

type fakeMetrics struct {
	snapshot collector.Snapshot
	history  collector.History
}

func (f *fakeMetrics) Snapshot() collector.Snapshot { return f.snapshot }
func (f *fakeMetrics) History() collector.History   { return f.history }

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
