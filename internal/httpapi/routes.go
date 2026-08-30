package httpapi

import "net/http"

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
var routeTable = []route{
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
