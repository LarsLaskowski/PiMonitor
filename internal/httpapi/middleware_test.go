package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
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
	if got := rec.Header().Get("Content-Security-Policy"); got == "" {
		t.Error("Content-Security-Policy missing on 401 response")
	}
}
