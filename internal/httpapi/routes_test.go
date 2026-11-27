package httpapi

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/collector"
	"github.com/larslaskowski/pimonitor/internal/web"
)

// TestRouteTable_PathsAreUnique guards the assumption both routeBucket and
// newServerStats rest on: one entry per path. A duplicate would collapse two
// routes onto a single counter and make http.ServeMux panic on the second
// registration, so failing here is friendlier than failing in New.
func TestRouteTable_PathsAreUnique(t *testing.T) {
	seen := make(map[string]bool, len(routeTable))
	for _, rt := range routeTable {
		if seen[rt.path] {
			t.Fatalf("routeTable has duplicate entries for %q", rt.path)
		}
		seen[rt.path] = true
	}
}

// TestRouteTable_EveryRouteHasItsOwnStatsBucket pins the invariant the table
// exists for: a route registered by New is classified into its own
// serverStats bucket, never into one of the shared fallbacks meant for
// unknown paths. Before routeTable, the route set lived in three separate
// lists, and a route added to only one of them was silently counted in
// whichever fallback matched its path.
func TestRouteTable_EveryRouteHasItsOwnStatsBucket(t *testing.T) {
	stats := newServerStats().snapshot()
	for _, rt := range routeTable {
		t.Run(rt.path, func(t *testing.T) {
			if got := routeBucket(rt.path); got != rt.path {
				t.Fatalf("routeBucket(%q) = %q, want %q", rt.path, got, rt.path)
			}
			if _, ok := stats.ByRoute[rt.path]; !ok {
				t.Fatalf("newServerStats has no counter for %q; ByRoute = %v", rt.path, stats.ByRoute)
			}
		})
	}
}

// TestRouteTable_EveryRouteIsRegistered asserts New actually serves every
// table entry, so the table can't drift into listing a route that no handler
// backs. Any status but 404 means the pattern is registered; the handlers'
// own behavior is covered by their tests.
func TestRouteTable_EveryRouteIsRegistered(t *testing.T) {
	s, _ := newTestServer(Config{PrometheusEnabled: true, ProcessesEnabled: true, OpenHABEnabled: true})
	for _, rt := range routeTable {
		t.Run(rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code == http.StatusNotFound {
				t.Fatalf("%s %s = 404, want the route to be registered", rt.method, rt.path)
			}
		})
	}
}

// TestRouteTable_DisabledRoutesAreNotRegistered covers the other half of the
// enabled predicate: a route its configuration switches off must 404 rather
// than be served. It still keeps its counter bucket — see
// TestHandlePrometheusMetrics_DisabledRequestsStillCountedUnderOwnBucket.
func TestRouteTable_DisabledRoutesAreNotRegistered(t *testing.T) {
	cfg := Config{}
	s, _ := newTestServer(cfg)
	for _, rt := range routeTable {
		if rt.enabled == nil || rt.enabled(cfg) {
			continue
		}
		t.Run(rt.path, func(t *testing.T) {
			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("%s %s = %d, want 404 while disabled", rt.method, rt.path, rec.Code)
			}
		})
	}
}

// TestRouteTable_VersionedRoutesGoThroughAPIRoute pins the one table field
// with a security consequence. api is a bool, so its zero value silently
// opts a new entry out of the apiRoute chain: no api_key gate, no in-flight
// limit, no no-store. The per-path middleware tests wouldn't notice, because
// a newly added route isn't in their hardcoded lists.
func TestRouteTable_VersionedRoutesGoThroughAPIRoute(t *testing.T) {
	const key = "s3cret"
	s, _ := newTestServer(Config{APIKey: key, PrometheusEnabled: true, ProcessesEnabled: true, OpenHABEnabled: true})
	for _, rt := range routeTable {
		if !strings.HasPrefix(rt.path, "/api/v1/") {
			continue
		}
		t.Run(rt.path, func(t *testing.T) {
			if !rt.api {
				t.Fatalf("routeTable entry %q has api = false; every /api/v1/... route must go through apiRoute", rt.path)
			}
			req := httptest.NewRequest(rt.method, rt.path, nil)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s %s without credentials = %d, want 401", rt.method, rt.path, rec.Code)
			}
		})
	}
}

// fullSubResourceSnapshot is a snapshot with every field a metrics
// sub-resource serves populated with a distinct, non-zero value, so a
// handler wired to the wrong field can't pass by coincidence.
func fullSubResourceSnapshot() collector.Snapshot {
	return collector.Snapshot{
		Timestamp:     time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC),
		UptimeSeconds: 372014.5,
		CPU:           collector.CPUUsage{OverallPercent: 12.4, PerCorePercent: []float64{10.1, 14.8, 11.2, 13.5}},
		Load:          collector.LoadAverage{Load1: 0.4, Load5: 0.38, Load15: 0.31},
		CPUCount:      4,
		Temperature:   collector.Temperature{Zone: "cpu-thermal", Celsius: 48.1},
		Memory:        collector.Memory{TotalBytes: 4127195136, AvailableBytes: 2893406208, UsedPercent: 29.9},
		Swap:          collector.Swap{TotalBytes: 104853504, UsedBytes: 0, UsedPercent: 0},
		Disks: []collector.Disk{{
			Mountpoint: "/", Device: "/dev/mmcblk0p2", FSType: "ext4",
			TotalBytes: 31036735488, UsedBytes: 8007122944, UsedPercent: 25.8,
		}},
		Network: []collector.NetworkInterface{{Name: "eth0", RxBytesPerSec: 1240.5, TxBytesPerSec: 302.1}},
		System:  collector.SystemInfo{KernelVersion: "6.1.0", Distribution: "Raspberry Pi OS", PiModel: "Raspberry Pi 4 Model B"},
		Updates: collector.Updates{
			Count:           3,
			Packages:        []collector.PackageUpdate{{Name: "openssl", NewVersion: "3.0.11-1", OldVersion: "3.0.9-1", Arch: "arm64"}},
			CacheAgeSeconds: 7200,
			CheckedAt:       time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		},
		Pihole: collector.Pihole{
			Installed:       true,
			UpdateAvailable: true,
			Components: []collector.PiholeComponent{
				{Name: "core", InstalledVersion: "v6.5", LatestVersion: "v6.6", Branch: "master", UpdateAvailable: true},
			},
			CacheAgeSeconds: 3600,
			CheckedAt:       time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC),
		},
	}
}

// TestMetricsSubResources_MatchFullSnapshot is the acceptance test for the
// per-metric endpoints (issue #8): each must return exactly the
// correspondingly named sub-object of GET /api/v1/metrics, so integrators
// polling a single metric parse the same JSON they already do — not a
// second, drift-prone representation of it. Comparing raw bytes against the
// full snapshot's own field, rather than against a hand-written expectation,
// is what makes that hold automatically as the snapshot's structs evolve.
func TestMetricsSubResources_MatchFullSnapshot(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.snapshot = fullSubResourceSnapshot()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/metrics = %d, want 200", rec.Code)
	}
	var full map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &full); err != nil {
		t.Fatalf("unmarshal /api/v1/metrics: %v", err)
	}

	for _, sub := range metricsSubResources {
		t.Run(sub.name, func(t *testing.T) {
			want, ok := full[sub.name]
			if !ok {
				t.Fatalf("GET /api/v1/metrics has no %q field; a sub-resource must be named after the snapshot field it serves", sub.name)
			}

			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sub.path(), nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", sub.path(), rec.Code)
			}
			const wantType = "application/json; charset=utf-8"
			if got := rec.Header().Get(contentTypeHeader); got != wantType {
				t.Fatalf("GET %s Content-Type = %q, want %q", sub.path(), got, wantType)
			}
			// Both bodies are produced by the same encoder over the same
			// struct, so field order matches and an exact byte comparison is
			// the strongest available statement of "the same JSON".
			if got := strings.TrimSpace(rec.Body.String()); got != string(want) {
				t.Fatalf("GET %s body = %s, want %s (the %q field of /api/v1/metrics)", sub.path(), got, want, sub.name)
			}
		})
	}
}

// TestMetricsSubResources_NoData pins what a sub-resource serves when its
// field holds nothing — before the first collection tick, or for network
// with network monitoring switched off. Never a 404: the endpoint exists and
// is answering, and a 404 would be indistinguishable from a misspelled path.
// What it does serve follows the field's type, since the body is the field's
// own encoding: a nil slice is null (the full snapshot either omits such a
// key via omitempty or reports null for it, but a sub-resource has no key to
// omit), while a struct degrades to its zero value, exactly as it does
// inside the full snapshot. docs/API.md documents both halves, so both are
// pinned here.
func TestMetricsSubResources_NoData(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{path: "/api/v1/metrics/network", want: "null"},
		{path: "/api/v1/metrics/disks", want: "null"},
		{path: "/api/v1/metrics/temperature", want: `{"zone":"","celsius":0}`},
		{path: "/api/v1/metrics/memory", want: `{"total_bytes":0,"available_bytes":0,"used_percent":0}`},
		{path: "/api/v1/metrics/pihole", want: `{"is_installed":false,"is_update_available":false,"checked_at":"0001-01-01T00:00:00Z"}`},
	}

	s, fm := newTestServer(Config{})
	fm.snapshot = collector.Snapshot{}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", tt.path, rec.Code)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.want {
				t.Fatalf("GET %s body = %s, want %s", tt.path, got, tt.want)
			}
		})
	}
}

// TestMetricsSubResources_PiholeShape pins the field names of the pihole
// object documented in docs/API.md, which are part of the v1 contract.
func TestMetricsSubResources_PiholeShape(t *testing.T) {
	s, fm := newTestServer(Config{})
	fm.snapshot = fullSubResourceSnapshot()

	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metrics/pihole", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/v1/metrics/pihole = %d, want 200", rec.Code)
	}
	const want = `{"is_installed":true,"is_update_available":true,` +
		`"components":[{"name":"core","installed_version":"v6.5","latest_version":"v6.6","branch":"master","is_update_available":true}],` +
		`"cache_age_seconds":3600,"checked_at":"2026-07-13T10:00:00Z"}`
	if got := strings.TrimSpace(rec.Body.String()); got != want {
		t.Fatalf("GET /api/v1/metrics/pihole body = %s, want %s", got, want)
	}
}

// TestMetricsSubResources_CountedUnderOwnStatsBucket checks the practical
// consequence of the sub-resources being routeTable entries: their traffic
// is attributable per endpoint in GET /api/v1/serverstats instead of
// collapsing into the shared other-api bucket. The generic table invariant
// is covered by TestRouteTable_EveryRouteHasItsOwnStatsBucket; this exercises
// it through a real request.
func TestMetricsSubResources_CountedUnderOwnStatsBucket(t *testing.T) {
	s, _ := newTestServer(Config{})
	for _, sub := range metricsSubResources {
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, sub.path(), nil))
	}

	got := s.stats.snapshot()
	for _, sub := range metricsSubResources {
		if got.ByRoute[sub.path()] != 1 {
			t.Errorf("ByRoute[%s] = %d, want 1", sub.path(), got.ByRoute[sub.path()])
		}
	}
	if got.ByRoute[bucketOtherAPI] != 0 {
		t.Errorf("ByRoute[%s] = %d, want 0 (sub-resource requests must not fall into the shared bucket)", bucketOtherAPI, got.ByRoute[bucketOtherAPI])
	}
}

// newTestServerWithDashboard is newTestServer with production's dashboard
// handler as the static handler. With a nil handler the mux answers 405 for a
// wrong method itself, so the wrong-method tests would pass on code that never
// registers the bare path; the real handler answers every method on "/" the way
// production does.
func newTestServerWithDashboard(t *testing.T, cfg Config) *Server {
	t.Helper()
	dash, err := web.Handler()
	if err != nil {
		t.Fatalf("web.Handler() error = %v", err)
	}
	_, fm := newTestServer(cfg)
	return New(fm, cfg, dash, nil)
}

// allRoutesConfig switches on every optional route of routeTable.
func allRoutesConfig() Config {
	return Config{PrometheusEnabled: true, ProcessesEnabled: true, OpenHABEnabled: true}
}

func serveMethod(s *Server, method, target string, hdr map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	return rec
}

func assertMethodNotAllowed(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("%s status = %d, want 405", what, rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != "GET, HEAD" {
		t.Errorf("%s Allow = %q, want %q", what, got, "GET, HEAD")
	}
	if got := rec.Body.String(); got != "method not allowed\n" {
		t.Errorf("%s body = %q, want %q", what, got, "method not allowed\n")
	}
}

func assertSecurityHeaders(t *testing.T, rec *httptest.ResponseRecorder, what string) {
	t.Helper()
	for _, h := range []string{"Content-Security-Policy", "X-Content-Type-Options", "X-Frame-Options", "Referrer-Policy"} {
		if rec.Header().Get(h) == "" {
			t.Errorf("%s: header %s is missing", what, h)
		}
	}
}

// TestRouteTable_WrongMethodAnswers405 pins AC2: every registered route
// answers any method but GET/HEAD with 405, Allow and the fixed body.
func TestRouteTable_WrongMethodAnswers405(t *testing.T) {
	s := newTestServerWithDashboard(t, allRoutesConfig())
	methods := []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions, "get"}
	for _, rt := range routeTable {
		for _, m := range methods {
			t.Run("AC2 "+m+" "+rt.path, func(t *testing.T) {
				rec := serveMethod(s, m, rt.path, nil)
				assertMethodNotAllowed(t, rec, m+" "+rt.path)
			})
		}
	}
	t.Run("AC2 E3 POST with a bad query", func(t *testing.T) {
		rec := serveMethod(s, http.MethodPost, "/api/v1/metrics/history?since=bad", nil)
		assertMethodNotAllowed(t, rec, "POST history?since=bad")
	})
}

// countingBody records every Read call.
type countingBody struct{ reads int }

func (b *countingBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

// TestRouteTable_WrongMethodReadsNoBody pins N1: no handler or middleware
// reads the request body to answer a 405.
func TestRouteTable_WrongMethodReadsNoBody(t *testing.T) {
	s := newTestServerWithDashboard(t, allRoutesConfig())
	for _, rt := range routeTable {
		t.Run("AC2 N1 "+rt.path, func(t *testing.T) {
			body := &countingBody{}
			req := httptest.NewRequest(http.MethodPost, rt.path, body)
			rec := httptest.NewRecorder()
			s.Handler().ServeHTTP(rec, req)
			assertMethodNotAllowed(t, rec, "POST "+rt.path)
			if body.reads != 0 {
				t.Errorf("POST %s read the body %d times, want 0", rt.path, body.reads)
			}
		})
	}
}

const wrongMethodKey = "s3cret"

func keyedConfig() Config {
	cfg := allRoutesConfig()
	cfg.APIKey = wrongMethodKey
	return cfg
}

// TestWrongMethod_APIChain_Key pins AC4: the key check comes before the 405.
func TestWrongMethod_APIChain_Key(t *testing.T) {
	s := newTestServerWithDashboard(t, keyedConfig())
	rec := serveMethod(s, http.MethodPost, "/api/v1/metrics", nil)
	if rec.Code != http.StatusUnauthorized || rec.Body.String() != "unauthorized\n" {
		t.Fatalf("AC4 POST without key = %d %q, want 401 %q", rec.Code, rec.Body.String(), "unauthorized\n")
	}
	if rec := serveMethod(s, http.MethodPost, "/metrics", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("AC4 POST /metrics without key = %d, want 401", rec.Code)
	}
}

// TestWrongMethod_APIChain_Headers pins AC4: no-store, Vary and security headers.
func TestWrongMethod_APIChain_Headers(t *testing.T) {
	s := newTestServerWithDashboard(t, keyedConfig())
	for _, path := range []string{"/api/v1/metrics", "/metrics"} {
		rec := serveMethod(s, http.MethodPost, path, map[string]string{"X-Api-Key": wrongMethodKey})
		assertMethodNotAllowed(t, rec, "AC4 POST "+path+" with key")
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("AC4 %s Cache-Control = %q, want no-store", path, got)
		}
		vary := strings.Join(rec.Header().Values("Vary"), ",")
		for _, want := range []string{"Authorization", "X-Api-Key"} {
			if !strings.Contains(vary, want) {
				t.Errorf("AC4 %s Vary = %q, want it to name %s", path, vary, want)
			}
		}
		assertSecurityHeaders(t, rec, "AC4 POST "+path)
	}
}

// TestWrongMethod_APIChain_Gzip pins AC4: the 405 is gzip-compressed on request.
func TestWrongMethod_APIChain_Gzip(t *testing.T) {
	s := newTestServerWithDashboard(t, allRoutesConfig())
	rec := serveMethod(s, http.MethodPost, "/api/v1/metrics", map[string]string{"Accept-Encoding": "gzip"})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("AC4 status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("AC4 Content-Encoding = %q, want gzip", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader error = %v", err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatalf("reading gzip body error = %v", err)
	}
	if string(data) != "method not allowed\n" {
		t.Errorf("AC4 decoded body = %q, want %q", data, "method not allowed\n")
	}
}

// TestWrongMethod_APIChain_InFlightLimit pins AC4/E2: beyond the limit it is 503.
func TestWrongMethod_APIChain_InFlightLimit(t *testing.T) {
	s := newTestServerWithDashboard(t, allRoutesConfig())
	for i := 0; i < cap(s.inFlight); i++ {
		s.inFlight <- struct{}{}
	}
	rec := serveMethod(s, http.MethodPost, "/api/v1/metrics", nil)
	if rec.Code != http.StatusServiceUnavailable || rec.Body.String() != "server busy\n" {
		t.Fatalf("AC4 POST over the limit = %d %q, want 503 %q", rec.Code, rec.Body.String(), "server busy\n")
	}
}

// TestWrongMethod_Healthz pins AC4 for /healthz: its own middleware only.
func TestWrongMethod_Healthz(t *testing.T) {
	cfg := allRoutesConfig()
	cfg.APIKey = "s3cret"
	s := newTestServerWithDashboard(t, cfg)
	rec := serveMethod(s, http.MethodPost, "/healthz", nil)
	assertMethodNotAllowed(t, rec, "POST /healthz")
	if got := rec.Header().Get("Cache-Control"); got != "" {
		t.Errorf("Cache-Control = %q, want none on /healthz", got)
	}
	assertSecurityHeaders(t, rec, "POST /healthz")
}

// TestWrongMethod_CountedInServerStats pins AC4: the 405 is counted under its
// route's bucket and in the global 4xx class (the two are separate counters).
func TestWrongMethod_CountedInServerStats(t *testing.T) {
	s := newTestServerWithDashboard(t, Config{})
	before := s.stats.snapshot()
	rec := serveMethod(s, http.MethodPost, "/api/v1/metrics", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	after := s.stats.snapshot()
	if got, want := after.ByRoute["/api/v1/metrics"], before.ByRoute["/api/v1/metrics"]+1; got != want {
		t.Errorf("AC4 ByRoute[/api/v1/metrics] = %d, want %d", got, want)
	}
	if got, want := after.ByStatusClass["4xx"], before.ByStatusClass["4xx"]+1; got != want {
		t.Errorf("AC4 ByStatusClass[4xx] = %d, want %d", got, want)
	}
}
