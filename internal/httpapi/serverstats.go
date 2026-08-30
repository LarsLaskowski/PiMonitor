package httpapi

import (
	"strings"
	"sync/atomic"
)

// routeBucket maps a request path to one of a small, fixed set of counter
// buckets for serverStats, rather than using the raw path as a map key.
// Without this, a client (or an attacker) probing arbitrary paths could grow
// an unbounded number of distinct map entries; bucketing by known route
// (falling back to bucketOtherAPI/bucketStatic for anything else) keeps the
// counter set bounded regardless of what a request asks for.
//
// The known routes are exactly routeTable's paths, so a route registered by
// New always has its own bucket instead of falling through to bucketStatic.
func routeBucket(path string) string {
	if _, ok := routePaths[path]; ok {
		return path
	}
	if strings.HasPrefix(path, "/api/v1/") {
		return bucketOtherAPI
	}
	return bucketStatic
}

// serverStats holds in-memory counters describing PiMonitor's own HTTP
// traffic: total requests served, broken down by response status class and
// by route. withLogging records one call per request, regardless of
// cfg.AccessLogEnabled, so an operator retains visibility into request
// volume even with per-request debug logging turned off; handleServerStats
// serves a snapshot via GET /api/v1/serverstats.
//
// Every field is an atomic.Uint64 rather than being guarded by a mutex: the
// route map is populated once at construction and never mutated afterwards,
// so concurrent map reads paired with atomic increments on each entry's
// counter are safe without further locking, even on the request hot path.
type serverStats struct {
	total     atomic.Uint64
	status1xx atomic.Uint64
	status2xx atomic.Uint64
	status3xx atomic.Uint64
	status4xx atomic.Uint64
	status5xx atomic.Uint64
	routes    map[string]*atomic.Uint64
}

// newServerStats builds a serverStats with every known route bucket
// pre-populated at zero, so a GET /api/v1/serverstats response always lists
// the full set of routes rather than only the ones that happened to be hit.
// The buckets are routeTable's paths plus the two fallbacks, which is the
// same set routeBucket can return — including routes the running
// configuration does not register, so the response shape does not vary with
// configuration.
func newServerStats() *serverStats {
	routes := make(map[string]*atomic.Uint64, len(routeTable)+2)
	for _, rt := range routeTable {
		routes[rt.path] = &atomic.Uint64{}
	}
	for _, fallback := range []string{bucketOtherAPI, bucketStatic} {
		routes[fallback] = &atomic.Uint64{}
	}
	return &serverStats{routes: routes}
}

// record increments the total, the counter for status's response class, and
// the counter for bucket's route, in that order. bucket should be the
// result of routeBucket so it always matches an entry pre-populated by
// newServerStats.
func (st *serverStats) record(bucket string, status int) {
	st.total.Add(1)
	switch {
	case status < 200:
		st.status1xx.Add(1)
	case status < 300:
		st.status2xx.Add(1)
	case status < 400:
		st.status3xx.Add(1)
	case status < 500:
		st.status4xx.Add(1)
	default:
		st.status5xx.Add(1)
	}
	if counter, ok := st.routes[bucket]; ok {
		counter.Add(1)
	}
}

// serverStatsSnapshot is the JSON shape served by GET /api/v1/serverstats.
type serverStatsSnapshot struct {
	Total         uint64            `json:"total"`
	ByStatusClass map[string]uint64 `json:"by_status_class"`
	ByRoute       map[string]uint64 `json:"by_route"`
}

// snapshot returns a point-in-time copy of the counters for JSON encoding.
// Reading the fields isn't a single atomic operation across all of them, so
// under concurrent traffic the returned totals may not sum up perfectly
// consistently with each other — acceptable for an operational counter
// endpoint, not a source of truth requiring transactional accuracy.
func (st *serverStats) snapshot() serverStatsSnapshot {
	byRoute := make(map[string]uint64, len(st.routes))
	for name, counter := range st.routes {
		byRoute[name] = counter.Load()
	}
	return serverStatsSnapshot{
		Total: st.total.Load(),
		ByStatusClass: map[string]uint64{
			"1xx": st.status1xx.Load(),
			"2xx": st.status2xx.Load(),
			"3xx": st.status3xx.Load(),
			"4xx": st.status4xx.Load(),
			"5xx": st.status5xx.Load(),
		},
		ByRoute: byRoute,
	}
}
