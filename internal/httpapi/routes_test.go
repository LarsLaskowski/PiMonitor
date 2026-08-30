package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/collector"
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
