// Package httpapi serves the PiMonitor web dashboard and its versioned
// REST API (/api/v1/...) intended for both the bundled frontend and
// third-party consumers (e.g. home automation systems such as openHAB).
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/collector"
)

// MetricsProvider is the subset of *collector.Collector the HTTP layer
// depends on. Defined as an interface so handlers can be tested against a
// fake implementation without touching real /proc/sys sources.
type MetricsProvider interface {
	Snapshot() collector.Snapshot
	History() collector.History
	Alerts() alert.Report
}

// Thresholds are the color-coding thresholds the frontend uses to render
// metric cards as ok/warn/critical.
type Thresholds struct {
	TemperatureWarnC  float64 `json:"temperature_warn_c"`
	TemperatureCritC  float64 `json:"temperature_crit_c"`
	CPUWarnPercent    float64 `json:"cpu_warn_percent"`
	CPUCritPercent    float64 `json:"cpu_crit_percent"`
	DiskWarnPercent   float64 `json:"disk_warn_percent"`
	DiskCritPercent   float64 `json:"disk_crit_percent"`
	SwapWarnPercent   float64 `json:"swap_warn_percent"`
	SwapCritPercent   float64 `json:"swap_crit_percent"`
	MemoryWarnPercent float64 `json:"memory_warn_percent"`
	MemoryCritPercent float64 `json:"memory_crit_percent"`
}

// ClientConfig is the non-sensitive runtime configuration exposed via
// GET /api/v1/config, so the frontend doesn't have to duplicate values
// (poll interval, thresholds, feature toggles) that are already defined
// server-side.
type ClientConfig struct {
	// Version is the build-time version of the running binary (the release
	// tag for release builds, or "dev" for an unversioned local build). It
	// is set from main.version via -ldflags. The frontend renders it in the
	// footer; it may carry a leading "v" depending on the build path.
	Version             string     `json:"version"`
	PollIntervalSeconds float64    `json:"poll_interval_seconds"`
	NetworkEnabled      bool       `json:"network_enabled"`
	Thresholds          Thresholds `json:"thresholds"`
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
}

// New builds a Server. staticHandler serves the embedded web dashboard
// assets (index.html, CSS, JS) at "/"; pass nil to serve only the API,
// e.g. in tests.
func New(metrics MetricsProvider, cfg Config, staticHandler http.Handler, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	s := &Server{metrics: metrics, cfg: cfg, log: log}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.Handle("GET /api/v1/metrics", s.withAPIKey(http.HandlerFunc(s.handleMetrics)))
	mux.Handle("GET /api/v1/metrics/history", s.withAPIKey(http.HandlerFunc(s.handleHistory)))
	mux.Handle("GET /api/v1/alerts", s.withAPIKey(http.HandlerFunc(s.handleAlerts)))
	mux.Handle("GET /api/v1/config", s.withAPIKey(http.HandlerFunc(s.handleConfig)))
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
