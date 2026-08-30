package httpapi

import (
	"net/http"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

// Bucket names for requests that don't match a registered route. They are
// the only two counter keys not derived from routeTable, and are shared by
// routeBucket and newServerStats so the classifier can never produce a
// bucket the counter map doesn't hold.
const (
	bucketOtherAPI = "other-api"
	bucketStatic   = "static"
)

// route describes one route PiMonitor serves on its own mux.
type route struct {
	method string
	// path doubles as the route's serverStats counter key, so it must be
	// the exact request path withLogging sees.
	path string
	// handler is a method expression, e.g. (*Server).handleMetrics, so the
	// table can be a package-level value built before any Server exists.
	handler func(*Server, http.ResponseWriter, *http.Request)
	// api reports whether the handler is wrapped in the apiRoute middleware
	// chain (api_key, gzip, no-store, and the shared in-flight limit).
	api bool
	// enabled reports whether cfg registers this route at all; nil means
	// unconditionally. A route that is not registered still gets a counter
	// bucket — see routeTable's comment.
	enabled func(Config) bool
}

// routeTable is the single source of truth for the set of routes PiMonitor
// serves: New registers from it, routeBucket classifies against it, and
// newServerStats pre-populates its counters from it. Adding a route here is
// therefore enough to keep all three in step — before this table they were
// three separate lists, and a route added to only the first got no bucket of
// its own: its traffic fell into whichever shared fallback matched its path
// (bucketOtherAPI under /api/v1/, bucketStatic otherwise, which is how the
// Prometheus endpoint's scrapes ended up counted as dashboard assets).
//
// Bucketing deliberately ignores the enabled predicate: routeBucket
// classifies r.URL.Path, not whichever handler — or the mux's 404 fallback —
// ended up serving the request, so a request to a disabled route is still
// counted under its own key (with a matching 4xx), and the shape of the
// GET /api/v1/serverstats response stays independent of configuration.
//
// The static dashboard handler is not in the table: it is registered on the
// catch-all "/" pattern rather than an exact path, and its traffic is what
// the bucketStatic fallback counts.
var routeTable = append([]route{
	// /healthz is intentionally not an apiRoute: it must stay answerable
	// while the API is shedding load, so a monitoring system can still tell
	// the process is alive. The static dashboard shell is left unwrapped
	// for the same reason — it has to load to render its "server busy"
	// state.
	{method: http.MethodGet, path: "/healthz", handler: (*Server).handleHealthz},
	{method: http.MethodGet, path: "/api/v1/metrics", handler: (*Server).handleMetrics, api: true},
	{method: http.MethodGet, path: "/api/v1/metrics/history", handler: (*Server).handleHistory, api: true},
	{method: http.MethodGet, path: "/api/v1/alerts", handler: (*Server).handleAlerts, api: true},
	{method: http.MethodGet, path: "/api/v1/config", handler: (*Server).handleConfig, api: true},
	{method: http.MethodGet, path: "/api/v1/serverstats", handler: (*Server).handleServerStats, api: true},
	{
		method:  http.MethodGet,
		path:    "/metrics",
		handler: (*Server).handlePrometheusMetrics,
		api:     true,
		enabled: func(cfg Config) bool { return cfg.PrometheusEnabled },
	},
}, metricsSubResourceRoutes()...)

// metricsSubResource is one narrow, read-only view of the current snapshot,
// served at /api/v1/metrics/<name>. These exist so an integrator that polls
// a single metric — an openHAB item bound to the CPU temperature, say — can
// fetch just that field instead of the whole snapshot, which cuts both
// bandwidth and the coupling to fields it never reads.
//
// A sub-resource serves exactly the value of the identically named field of
// GET /api/v1/metrics: the same JSON, sliced out, never a shape of its own.
// That is what keeps these additive to /api/v1 — there is no second
// representation of a metric that could drift from the full snapshot's.
type metricsSubResource struct {
	// name is the endpoint's last path segment and, deliberately, also the
	// field's key in the GET /api/v1/metrics object, so the response body is
	// byte for byte what a caller would have picked out of the full snapshot
	// under that key. TestMetricsSubResources_MatchFullSnapshot pins that.
	name string
	// value selects the snapshot field the endpoint serves. It returns any
	// rather than being generic because the only thing done with the result
	// is JSON-encoding it, and route.handler is not parameterised by type.
	value func(collector.Snapshot) any
}

// metricsSubResources is the set of fields GET /api/v1/metrics is sliced
// into. Adding an entry here is enough: it registers the route, gives it its
// own serverStats bucket, and puts it under the routeTable invariant tests —
// all of which derive from routeTable, which these become part of. Fields
// with no integration value of their own (timestamp, uptime) are deliberately
// left out; a caller that wants those wants the full snapshot anyway.
var metricsSubResources = []metricsSubResource{
	{name: "cpu", value: func(s collector.Snapshot) any { return s.CPU }},
	{name: "temperature", value: func(s collector.Snapshot) any { return s.Temperature }},
	{name: "memory", value: func(s collector.Snapshot) any { return s.Memory }},
	{name: "disks", value: func(s collector.Snapshot) any { return s.Disks }},
	{name: "network", value: func(s collector.Snapshot) any { return s.Network }},
	{name: "updates", value: func(s collector.Snapshot) any { return s.Updates }},
}

// path is the endpoint the sub-resource is served at.
func (m metricsSubResource) path() string { return "/api/v1/metrics/" + m.name }

// route renders the sub-resource as a routeTable entry. Like every other
// /api/v1/... entry it goes through apiRoute, so the api_key gate, the
// in-flight limit, gzip and no-store apply to it unchanged.
func (m metricsSubResource) route() route {
	return route{
		method: http.MethodGet,
		path:   m.path(),
		handler: func(s *Server, w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, s.log.Error, m.value(s.metrics.Snapshot()))
		},
		api: true,
	}
}

// metricsSubResourceRoutes renders every sub-resource as a routeTable entry.
func metricsSubResourceRoutes() []route {
	routes := make([]route, 0, len(metricsSubResources))
	for _, sub := range metricsSubResources {
		routes = append(routes, sub.route())
	}
	return routes
}

// routePaths is the set of paths in routeTable, built once at package
// initialization so routeBucket can classify a request with a single map
// lookup instead of scanning the table on every request.
var routePaths = func() map[string]struct{} {
	paths := make(map[string]struct{}, len(routeTable))
	for _, rt := range routeTable {
		paths[rt.path] = struct{}{}
	}
	return paths
}()

// pattern renders the route as an http.ServeMux pattern.
func (r route) pattern() string { return r.method + " " + r.path }

// register installs the route on mux, wrapping it in the apiRoute chain when
// the route asks for it. Routes whose enabled predicate rejects cfg are
// skipped, so an unconfigured endpoint 404s rather than being served.
func (r route) register(mux *http.ServeMux, s *Server) {
	if r.enabled != nil && !r.enabled(s.cfg) {
		return
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.handler(s, w, req)
	})
	if r.api {
		mux.Handle(r.pattern(), s.apiRoute(h))
		return
	}
	mux.Handle(r.pattern(), h)
}
