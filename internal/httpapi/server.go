// Package httpapi serves the PiMonitor web dashboard and its versioned
// REST API (/api/v1/...) intended for both the bundled frontend and
// third-party consumers (e.g. home automation systems such as openHAB).
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/collector"
	"github.com/larslaskowski/pimonitor/internal/config"
)

// MetricsProvider is the subset of *collector.Collector the HTTP layer
// depends on. Defined as an interface so handlers can be tested against a
// fake implementation without touching real /proc/sys sources.
type MetricsProvider interface {
	Snapshot() collector.Snapshot
	History() collector.History
	// HistoryGeneration increments whenever History() would return a
	// different result. handleHistory uses it to reuse the cached copy and
	// its encoding instead of redoing the deep copy and JSON encode on
	// every request.
	HistoryGeneration() uint64
	Alerts() alert.Report
}

// defaultMaxInFlight bounds how many requests may be actively processing at
// once across the /api/v1/... endpoints. GET /api/v1/metrics/history is the
// most expensive of them: on a cache miss it deep-copies every retained
// history ring buffer and JSON-encodes the result while holding the
// collector's lock. Left unbounded, enough concurrent callers can starve
// the collector's own tick on a single-core Pi. 16 is generous for the
// intended use (one dashboard plus a handful of integrations) while
// capping worst-case CPU and memory; it is deliberately a constant rather
// than a config option to keep the config surface small.
const defaultMaxInFlight = 16

// ClientConfig is the non-sensitive runtime configuration exposed via
// GET /api/v1/config, so the frontend doesn't have to duplicate values
// (poll interval, thresholds, feature toggles) that are already defined
// server-side. Thresholds reuses config.Thresholds directly rather than a
// second, hand-copied struct, so the yaml-configured and JSON-served
// representations can never drift apart in the field set.
type ClientConfig struct {
	// Version is the build-time version of the running binary (the release
	// tag for release builds, or "dev" for an unversioned local build). It
	// is set from main.version via -ldflags. The frontend renders it in the
	// footer; it may carry a leading "v" depending on the build path.
	Version             string  `json:"version"`
	PollIntervalSeconds float64 `json:"poll_interval_seconds"`
	// HistoryWindowMinutes is how far back GET /api/v1/metrics/history
	// retains points. The dashboard needs it to bound the window it builds
	// up locally from ?since= deltas to the same span the server keeps, so
	// what it charts stays what a full-window fetch would have returned.
	HistoryWindowMinutes float64           `json:"history_window_minutes"`
	NetworkEnabled       bool              `json:"network_enabled"`
	Thresholds           config.Thresholds `json:"thresholds"`
}

// Config configures the HTTP server.
type Config struct {
	// ListenAddr is the address the server listens on, e.g. ":8080".
	ListenAddr string
	// APIKey, if non-empty, is required (as a bearer token or X-Api-Key
	// header) to access /api/v1/... endpoints. Leave empty to keep the
	// dashboard usable without authentication on a trusted LAN.
	APIKey string
	// Client is echoed back verbatim by GET /api/v1/config.
	Client ClientConfig
}

// Server serves the PiMonitor dashboard and REST API.
type Server struct {
	httpServer *http.Server
	metrics    MetricsProvider
	cfg        Config
	log        *slog.Logger

	// inFlight is the semaphore withMaxInFlight acquires from; its capacity
	// is defaultMaxInFlight. Shared across every /api/v1/... endpoint so the
	// limit bounds total concurrent API work, not each endpoint separately.
	inFlight chan struct{}

	// historyCacheMu guards the historyCache* fields below. historyCacheData
	// is the deep copy History() returned for historyCacheGen (served, sliced,
	// to ?since= requests); historyCacheJSON is its full-window encoding, built
	// on demand and nil until a request asks for the whole window.
	historyCacheMu    sync.Mutex
	historyCacheGen   uint64
	historyCacheValid bool
	historyCacheData  collector.History
	historyCacheJSON  []byte
}

// New builds a Server. staticHandler serves the embedded web dashboard
// assets (index.html, CSS, JS) at "/"; pass nil to serve only the API,
// e.g. in tests.
func New(metrics MetricsProvider, cfg Config, staticHandler http.Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{metrics: metrics, cfg: cfg, log: log, inFlight: make(chan struct{}, defaultMaxInFlight)}

	mux := http.NewServeMux()
	// /healthz and the static dashboard assets are intentionally not wrapped
	// by withMaxInFlight: a monitoring system should still be able to tell
	// the process is alive while the API is shedding load, and the shell
	// the dashboard needs to render its "server busy" state must load too.
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /api/v1/metrics", s.apiRoute(s.handleMetrics))
	mux.Handle("GET /api/v1/metrics/history", s.apiRoute(s.handleHistory))
	mux.Handle("GET /api/v1/alerts", s.apiRoute(s.handleAlerts))
	mux.Handle("GET /api/v1/config", s.apiRoute(s.handleConfig))
	if staticHandler != nil {
		mux.Handle("/", staticHandler)
	}

	s.httpServer = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      s.withLogging(s.withSecurityHeaders(mux)),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return s
}

// apiRoute wraps a /api/v1/... handler in the common middleware chain shared
// by all four routes, outermost first: withNoStore must run before withGzip
// so its Cache-Control/Vary headers are set before withGzip mutates Vary
// itself (Add, not Set, so neither clobbers the other).
func (s *Server) apiRoute(h http.HandlerFunc) http.Handler {
	return s.withNoStore(s.withMaxInFlight(s.withGzip(s.withAPIKey(h))))
}

// Handler returns the server's http.Handler, for use in tests via
// httptest without binding a real port.
func (s *Server) Handler() http.Handler {
	return s.httpServer.Handler
}

// ListenAndServe starts serving. It blocks until the server stops.
func (s *Server) ListenAndServe() error {
	s.log.Info("http server listening", "addr", s.cfg.ListenAddr)
	return s.httpServer.ListenAndServe()
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	return s.httpServer.Shutdown(ctx)
}
