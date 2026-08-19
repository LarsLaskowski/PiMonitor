package httpapi

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

func TestSecurityHeaders_SetOnAllResponses(t *testing.T) {
	s, _ := newTestServer(Config{})

	want := map[string]string{
		"Content-Security-Policy": "default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; frame-ancestors 'none'",
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "no-referrer",
	}

	// Dashboard route (404 without a static handler in tests, but the
	// middleware must still apply), health check, and API endpoints.
	for _, path := range []string{"/", "/healthz", "/api/v1/metrics", "/api/v1/metrics/history", "/api/v1/config"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		for header, value := range want {
			if got := rec.Header().Get(header); got != value {
				t.Errorf("GET %s: header %s = %q, want %q", path, header, got, value)
			}
		}
	}
}

func TestSecurityHeaders_SetOnUnauthorizedResponses(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want %q on 401 response", got, "nosniff")
	}
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("Content-Security-Policy missing on 401 response")
	}
}

func TestHandleHistory_GzipWhenAccepted(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.history = collector.History{}
	for i := 0; i < 500; i++ {
		fm.history.CPUPercent = append(fm.history.CPUPercent, collector.HistoryPoint{Value: 12.5})
	}

	plainReq := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/history", nil)
	plainRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(plainRec, plainReq)
	if plainRec.Header().Get("Content-Encoding") != "" {
		t.Fatalf("Content-Encoding = %q without Accept-Encoding header, want empty", plainRec.Header().Get("Content-Encoding"))
	}

	gzipReq := httptest.NewRequest(http.MethodGet, "/api/v1/metrics/history", nil)
	gzipReq.Header.Set("Accept-Encoding", "gzip")
	gzipRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(gzipRec, gzipReq)

	if got := gzipRec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want %q", got, "gzip")
	}
	if got := gzipRec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want %q", got, "Accept-Encoding")
	}

	zr, err := gzip.NewReader(bytes.NewReader(gzipRec.Body.Bytes()))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	decompressed, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("read decompressed body: %v", err)
	}

	var want, got collector.History
	if err := json.Unmarshal(plainRec.Body.Bytes(), &want); err != nil {
		t.Fatalf("unmarshal identity response: %v", err)
	}
	if err := json.Unmarshal(decompressed, &got); err != nil {
		t.Fatalf("unmarshal decompressed response: %v", err)
	}
	if len(got.CPUPercent) != len(want.CPUPercent) {
		t.Fatalf("decompressed CPUPercent length = %d, want %d", len(got.CPUPercent), len(want.CPUPercent))
	}

	if len(gzipRec.Body.Bytes()) >= len(plainRec.Body.Bytes()) {
		t.Fatalf("gzip body (%d bytes) not smaller than identity body (%d bytes)", len(gzipRec.Body.Bytes()), len(plainRec.Body.Bytes()))
	}
}
