package httpapi

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/larslaskowski/pimonitor/internal/collector"
)

const contentTypeHeader = "Content-Type"

func writeJSON(w http.ResponseWriter, log func(msg string, args ...any), v any) {
	w.Header().Set(contentTypeHeader, "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log("failed to encode JSON response", "error", err)
	}
}

// handleHealthz serves GET /healthz. It reports unhealthy (503) when
// s.cfg.HealthzMaxStaleness is set and the latest snapshot is older than
// that bound — e.g. the collector goroutine has stalled — so systemd/uptime
// monitors can detect that failure mode instead of seeing a static 200 that
// never reflects collection health. s.cfg.HealthzMaxStaleness left at its
// zero value keeps this a pure liveness probe, as in tests that construct
// Config{} directly; production always sets it from
// config.Config.HealthzMaxStaleness(...), which never returns zero.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	if max := s.cfg.HealthzMaxStaleness; max > 0 && time.Since(s.metrics.Snapshot().Timestamp) > max {
		http.Error(w, "stale: latest snapshot is older than the configured healthz_max_staleness_seconds", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set(contentTypeHeader, "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

// handleMetrics serves GET /api/v1/metrics: the current snapshot of every
// metric. This is the main endpoint for third-party integrations (e.g. an
// openHAB HTTP binding polling this URL and extracting fields via
// JSONPath). See docs/API.md for the full response schema.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.log.Error, s.metrics.Snapshot())
}

// handleHistory serves GET /api/v1/metrics/history: the retained
// in-memory history for every time-series metric.
//
// The optional ?since=<RFC 3339 timestamp> parameter reduces the response
// to the points strictly newer than that timestamp, so a client that
// already holds the window (the bundled dashboard, or a third-party
// poller) fetches a handful of new points per poll instead of the whole
// window every time. Omitting it returns the full window, unchanged — the
// parameter is purely additive, so /api/v1 stays compatible.
func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	var since time.Time
	if raw := r.URL.Query().Get("since"); raw != "" {
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			// Deliberately an error rather than silently serving the full
			// window: a client whose timestamp format is wrong would
			// otherwise never notice it is re-fetching everything.
			http.Error(w, "invalid 'since' parameter: expected an RFC 3339 timestamp, e.g. 2026-07-12T18:31:00Z", http.StatusBadRequest)
			return
		}
		since = t
	}

	data, err := s.historyPayload(since)
	if err != nil {
		s.log.Error("failed to encode JSON response", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set(contentTypeHeader, "application/json; charset=utf-8")
	_, _ = w.Write(data)
}

// historyPayload encodes the response for a history request: the cached
// full-window encoding when since is zero, otherwise a delta re-sliced out
// of the cached copy and encoded on its own. Encoding here rather than
// streaming straight to the ResponseWriter keeps an encode failure a clean
// 500 instead of a 200 with a truncated body.
func (s *Server) historyPayload(since time.Time) ([]byte, error) {
	hist, full, err := s.cachedHistory(since.IsZero())
	if err != nil || since.IsZero() {
		return full, err
	}
	return json.Marshal(hist.Since(since))
}

// cachedHistory returns the retained history for the collector's current
// generation and, when full is true, its serialised full-window encoding.
//
// Both are cached per generation, since the history only changes once per
// fastTick: the deep copy MetricsProvider.History() makes is reused by
// ?since= requests, which re-slice it and encode only the tail, and the
// full encoding is reused by requests that want the whole window. The full
// encoding is built lazily so ?since= clients never pay for serialising a
// window nobody asked for.
func (s *Server) cachedHistory(full bool) (collector.History, []byte, error) {
	gen := s.metrics.HistoryGeneration()

	s.historyCacheMu.Lock()
	defer s.historyCacheMu.Unlock()

	if !s.historyCacheValid || s.historyCacheGen != gen {
		s.historyCacheData = s.metrics.History()
		s.historyCacheJSON = nil
		s.historyCacheGen = gen
		s.historyCacheValid = true
	}
	if full && s.historyCacheJSON == nil {
		data, err := json.Marshal(s.historyCacheData)
		if err != nil {
			// Leave historyCacheJSON nil rather than caching a partial or
			// garbage encoding; the next request retries the encode.
			return collector.History{}, nil, err
		}
		s.historyCacheJSON = data
	}
	return s.historyCacheData, s.historyCacheJSON, nil
}

// handleAlerts serves GET /api/v1/alerts: the current per-metric alert
// states (ok/warn/crit) plus the recent list of fired/cleared transition
// events. When alerting is disabled the response reports enabled=false.
func (s *Server) handleAlerts(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.log.Error, s.metrics.Alerts())
}

// handleConfig serves GET /api/v1/config: non-sensitive runtime
// configuration the frontend needs (poll interval, thresholds, feature
// toggles), so these values aren't duplicated/hardcoded client-side.
func (s *Server) handleConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.log.Error, s.cfg.Client)
}

// handlePrometheusMetrics serves GET /metrics: the current snapshot
// rendered in the Prometheus text exposition format, for a Prometheus
// server to scrape directly instead of polling the JSON
// GET /api/v1/metrics endpoint. Only registered when
// s.cfg.PrometheusEnabled is true — see docs/API.md.
func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set(contentTypeHeader, prometheusContentType)
	_, _ = w.Write(renderPrometheusMetrics(s.metrics.Snapshot()))
}

// handleServerStats serves GET /api/v1/serverstats: in-memory counters of
// PiMonitor's own HTTP traffic (total requests, broken down by response
// status class and by route), recorded by withLogging on every request.
// These stay available even when access_log_enabled is false, since they
// are the intended replacement for eyeballing request volume from the
// per-request debug log lines.
func (s *Server) handleServerStats(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.log.Error, s.stats.snapshot())
}
