package httpapi

import "testing"

func TestRouteBucket_KnownRoutes(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/healthz", "/healthz"},
		{"/metrics", "/metrics"},
		{"/api/v1/metrics", "/api/v1/metrics"},
		{"/api/v1/metrics/history", "/api/v1/metrics/history"},
		{"/api/v1/alerts", "/api/v1/alerts"},
		{"/api/v1/config", "/api/v1/config"},
		{"/api/v1/serverstats", "/api/v1/serverstats"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := routeBucket(tt.path); got != tt.want {
				t.Fatalf("routeBucket(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// TestRouteBucket_UnknownPathsAreBounded guards against an unbounded
// counter map: any path under /api/v1/... that isn't a registered route
// must fall into a single shared bucket, and everything else (static
// assets, or an attacker probing arbitrary paths) into another single
// shared bucket — never a bucket keyed on the raw path.
func TestRouteBucket_UnknownPathsAreBounded(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/api/v1/does-not-exist", "other-api"},
		{"/api/v2/metrics", "static"},
		{"/", "static"},
		{"/app.js", "static"},
		{"/../../etc/passwd", "static"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := routeBucket(tt.path); got != tt.want {
				t.Fatalf("routeBucket(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestServerStats_RecordTracksTotalsAndStatusClasses(t *testing.T) {
	st := newServerStats()

	st.record("/healthz", 200)
	st.record("/api/v1/metrics", 200)
	st.record("/api/v1/metrics", 404)
	st.record("/api/v1/metrics", 503)

	got := st.snapshot()
	if got.Total != 4 {
		t.Fatalf("Total = %d, want 4", got.Total)
	}
	if got.ByStatusClass["2xx"] != 2 {
		t.Fatalf("ByStatusClass[2xx] = %d, want 2", got.ByStatusClass["2xx"])
	}
	if got.ByStatusClass["4xx"] != 1 {
		t.Fatalf("ByStatusClass[4xx] = %d, want 1", got.ByStatusClass["4xx"])
	}
	if got.ByStatusClass["5xx"] != 1 {
		t.Fatalf("ByStatusClass[5xx] = %d, want 1", got.ByStatusClass["5xx"])
	}
	if got.ByRoute["/healthz"] != 1 {
		t.Fatalf("ByRoute[/healthz] = %d, want 1", got.ByRoute["/healthz"])
	}
	if got.ByRoute["/api/v1/metrics"] != 3 {
		t.Fatalf("ByRoute[/api/v1/metrics] = %d, want 3", got.ByRoute["/api/v1/metrics"])
	}
}

// TestServerStats_UnknownBucketIsIgnored guards record against a caller
// passing a bucket name that isn't one of the pre-populated routes (e.g. a
// future bug bypassing routeBucket): the total and status-class counters
// must still increment, but no new map entry should appear.
func TestServerStats_UnknownBucketIsIgnored(t *testing.T) {
	st := newServerStats()

	st.record("not-a-real-bucket", 200)

	got := st.snapshot()
	if got.Total != 1 {
		t.Fatalf("Total = %d, want 1", got.Total)
	}
	if _, ok := got.ByRoute["not-a-real-bucket"]; ok {
		t.Fatal("expected no ByRoute entry for an unknown bucket")
	}
}
