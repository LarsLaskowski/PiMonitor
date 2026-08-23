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
	if vary := gzipRec.Header().Values("Vary"); !containsAll(vary, "Accept-Encoding", "Authorization", "X-Api-Key") {
		t.Fatalf("Vary = %v, want it to include Accept-Encoding, Authorization, and X-Api-Key", vary)
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

// TestWithMaxInFlight_RejectsBeyondLimit fills the semaphore with requests
// that block on a channel the test controls, then asserts that exactly one
// additional request is rejected with 503 and a Retry-After header. Uses
// explicit channel synchronization rather than time.Sleep so the test is
// deterministic (see docs/TESTS.md's guidance on concurrent code).
func TestWithMaxInFlight_RejectsBeyondLimit(t *testing.T) {
	const limit = 3
	s := &Server{inFlight: make(chan struct{}, limit)}

	release := make(chan struct{})
	entered := make(chan struct{}, limit)
	blocking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
		w.WriteHeader(http.StatusOK)
	})
	handler := s.withMaxInFlight(blocking)

	results := make(chan *httptest.ResponseRecorder, limit)
	for i := 0; i < limit; i++ {
		go func() {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
			results <- rec
		}()
	}
	for i := 0; i < limit; i++ {
		<-entered
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status beyond limit = %d, want %d", rec.Code, http.StatusServiceUnavailable)
	}
	if got := rec.Header().Get("Retry-After"); got != "1" {
		t.Fatalf("Retry-After = %q, want %q", got, "1")
	}

	close(release)
	for i := 0; i < limit; i++ {
		blocked := <-results
		if blocked.Code != http.StatusOK {
			t.Fatalf("blocked request status = %d, want 200", blocked.Code)
		}
	}
}

// TestWithMaxInFlight_ReleasesPermit is the regression test for a missing
// `defer func() { <-sem }()`: after every in-flight request has completed,
// a subsequent request beyond the original limit must still succeed rather
// than finding the semaphore permanently full.
func TestWithMaxInFlight_ReleasesPermit(t *testing.T) {
	const limit = 2
	s := &Server{inFlight: make(chan struct{}, limit)}
	handler := s.withMaxInFlight(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < limit; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status after permits should have been released = %d, want 200", rec.Code)
	}
}

// TestWithMaxInFlight_AllowsTrafficBelowLimit guards against an off-by-one
// making the limiter fire too eagerly: sequential requests below the limit
// must all succeed.
func TestWithMaxInFlight_AllowsTrafficBelowLimit(t *testing.T) {
	const limit = 4
	s := &Server{inFlight: make(chan struct{}, limit)}
	handler := s.withMaxInFlight(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < limit-1; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status = %d, want 200", i, rec.Code)
		}
	}
}

// containsAll reports whether every want value is present somewhere in got.
func containsAll(got []string, want ...string) bool {
	for _, w := range want {
		found := false
		for _, g := range got {
			if g == w {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TestNoStore_SetOnAllAPIRoutes is the regression test for issue #105:
// metric/alert/config responses are point-in-time and, when an API key is
// configured, credential-protected, so a reverse proxy fronting the Pi must
// never cache them.
func TestNoStore_SetOnAllAPIRoutes(t *testing.T) {
	s, _ := newTestServer(Config{})

	for _, path := range []string{"/api/v1/metrics", "/api/v1/metrics/history", "/api/v1/alerts", "/api/v1/config"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)

		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("GET %s: Cache-Control = %q, want %q", path, got, "no-store")
		}
	}
}

// TestNoStore_VaryNamesCredentialHeaders guards against a shared cache
// keyed only on URL (+ Accept-Encoding) serving an authorised response to an
// unauthorised client, or vice versa.
func TestNoStore_VaryNamesCredentialHeaders(t *testing.T) {
	s, _ := newTestServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	if !containsAll(vary, "Authorization", "X-Api-Key") {
		t.Fatalf("Vary = %v, want it to include Authorization and X-Api-Key", vary)
	}
}

// TestNoStore_VarySurvivesGzip is the regression test for withGzip's former
// h.Set("Vary", ...) clobbering any Vary value set upstream: since withGzip
// runs inside withNoStore, a Set there would erase the credential Vary
// values on every gzip-negotiated request.
func TestNoStore_VarySurvivesGzip(t *testing.T) {
	s, _ := newTestServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	vary := rec.Header().Values("Vary")
	if !containsAll(vary, "Accept-Encoding", "Authorization", "X-Api-Key") {
		t.Fatalf("Vary = %v, want it to include Accept-Encoding, Authorization, and X-Api-Key", vary)
	}
}

// TestNoStore_NotSetOnHealthz guards against withNoStore being wired too
// broadly: /healthz is a trivial constant and must stay cacheable/simple.
func TestNoStore_NotSetOnHealthz(t *testing.T) {
	s, _ := newTestServer(Config{})

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want unset on /healthz", got)
	}
}

// TestNoStore_SetOnUnauthorizedResponse ensures a cached 401 can't be served
// to a correctly-authenticated client later: the no-store directive must be
// set before withAPIKey has a chance to reject the request.
func TestNoStore_SetOnUnauthorizedResponse(t *testing.T) {
	s, _ := newTestServer(Config{APIKey: "secret123"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control on 401 response = %q, want %q", got, "no-store")
	}
}

// TestHealthz_BypassesMaxInFlight documents the deliberate choice that
// /healthz is not gated by withMaxInFlight: a monitoring system should
// still be able to tell the process is alive while the API is shedding
// load. Fills s.inFlight directly to the real defaultMaxInFlight capacity
// (rather than via blocking goroutines) so the assertion is deterministic.
func TestHealthz_BypassesMaxInFlight(t *testing.T) {
	s, _ := newTestServer(Config{})
	for i := 0; i < defaultMaxInFlight; i++ {
		s.inFlight <- struct{}{}
	}
	defer func() {
		for i := 0; i < defaultMaxInFlight; i++ {
			<-s.inFlight
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status while API in-flight limit is full = %d, want 200", rec.Code)
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
	apiRec := httptest.NewRecorder()
	s.Handler().ServeHTTP(apiRec, apiReq)
	if apiRec.Code != http.StatusServiceUnavailable {
		t.Fatalf("api status while in-flight limit is full = %d, want %d (sanity check that the fill above actually exercised the limiter)", apiRec.Code, http.StatusServiceUnavailable)
	}
}

// TestWithGzip_AcceptEncodingNegotiation exercises Accept-Encoding parsing
// through the full handler chain. The header is a q-value list per RFC 9110
// §12.5.3, so an explicit refusal ("gzip;q=0") must yield an identity
// response and a lookalike token ("x-gzip") must not be treated as gzip.
func TestWithGzip_AcceptEncodingNegotiation(t *testing.T) {
	tests := []struct {
		name           string
		acceptEncoding string
		wantCompressed bool
	}{
		{"plain gzip", "gzip", true},
		{"gzip among several codings", "gzip, deflate, br", true},
		{"gzip with explicit q=1.0", "deflate, gzip;q=1.0, *;q=0.5", true},
		{"wildcard", "*", true},
		{"uppercase coding", "GZIP", true},
		{"gzip refused", "gzip;q=0", false},
		{"gzip refused alongside identity", "identity, gzip;q=0", false},
		{"gzip refused with trailing zeros", "gzip;q=0.000", false},
		{"wildcard refused", "*;q=0", false},
		{"explicit refusal outranks wildcard", "gzip;q=0, *", false},
		{"explicit acceptance alongside refused wildcard", "gzip, *;q=0", true},
		{"x-gzip lookalike", "x-gzip", false},
		{"other coding only", "deflate", false},
		{"empty header", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newTestServer(Config{})

			req := httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil)
			if tt.acceptEncoding != "" {
				req.Header.Set("Accept-Encoding", tt.acceptEncoding)
			}
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)

			got := rec.Header().Get("Content-Encoding")
			if tt.wantCompressed && got != "gzip" {
				t.Fatalf("Accept-Encoding %q: Content-Encoding = %q, want %q", tt.acceptEncoding, got, "gzip")
			}
			if !tt.wantCompressed && got != "" {
				t.Fatalf("Accept-Encoding %q: Content-Encoding = %q, want identity (empty)", tt.acceptEncoding, got)
			}
		})
	}
}

// TestAcceptsGzip_HeaderForms covers the Accept-Encoding grammar directly,
// including whitespace and parameter forms that are tedious to drive through
// a full request.
func TestAcceptsGzip_HeaderForms(t *testing.T) {
	tests := []struct {
		header string
		want   bool
	}{
		{"gzip", true},
		{"gzip, deflate, br", true},
		{"deflate, gzip;q=1.0, *;q=0.5", true},
		{"*", true},
		{"  gzip  ;  q = 0.5  ", true},
		{"GZip;Q=1", true},
		{"gzip;q=0.001", true},
		{"deflate, gzip", true},
		{"gzip;q=0", false},
		{"identity, gzip;q=0", false},
		{"gzip;q=0.000", false},
		{"gzip;q=0.", false},
		{"*;q=0", false},
		{"x-gzip", false},
		{"gzipped", false},
		{"deflate", false},
		{"identity;q=0", false},
		{"", false},
		{",", false},
	}

	for _, tt := range tests {
		if got := acceptsGzip(tt.header); got != tt.want {
			t.Errorf("acceptsGzip(%q) = %v, want %v", tt.header, got, tt.want)
		}
	}
}
