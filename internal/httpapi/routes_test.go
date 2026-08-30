package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	s, _ := newTestServer(Config{PrometheusEnabled: true})
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
	s, _ := newTestServer(Config{APIKey: key, PrometheusEnabled: true})
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
