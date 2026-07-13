package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandler_ServesIndex(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PiMonitor") {
		t.Fatalf("expected index.html to contain %q", "PiMonitor")
	}
}

func TestHandler_ServesStaticAssets(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	for _, path := range []string{"/style.css", "/app.js", "/chart.js", "/gauge.js", "/theme-init.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}
}

func TestHandler_ServesThemeToggle(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `id="theme-toggle"`) {
		t.Errorf("expected index.html to contain the theme toggle button")
	}
	// The pre-paint theme script is an external file (inline scripts are
	// blocked by the Content-Security-Policy) and must load in <head>.
	if !strings.Contains(body, `<script src="theme-init.js"></script>`) {
		t.Errorf("expected index.html to load theme-init.js")
	}

	// The pre-paint script must key persistence off the same localStorage key
	// app.js uses, so a stored choice survives a reload without a flash.
	req = httptest.NewRequest(http.MethodGet, "/theme-init.js", nil)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !strings.Contains(rec.Body.String(), "pimonitor-theme") {
		t.Errorf("expected theme-init.js to reference the pimonitor-theme storage key")
	}
}

func TestHandler_UnknownPath404s(t *testing.T) {
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/does-not-exist.js", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
