package httpapi

import (
	"encoding/json"
	"net/http"
)

func writeJSON(w http.ResponseWriter, log func(msg string, args ...any), v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log("failed to encode JSON response", "error", err)
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
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
// in-memory history for every time-series metric. The retained history only
// changes once per fastTick, so the encoded response is cached and reused
// across requests until MetricsProvider.HistoryGeneration() moves on,
// rather than deep-copying every ring buffer and re-encoding on every
// request from every dashboard/integration poll.
func (s *Server) handleHistory(w http.ResponseWriter, _ *http.Request) {
	gen := s.metrics.HistoryGeneration()

	s.historyCacheMu.Lock()
	if s.historyCacheJSON == nil || s.historyCacheGen != gen {
		data, err := json.Marshal(s.metrics.History())
		if err != nil {
			s.historyCacheMu.Unlock()
			s.log.Error("failed to encode JSON response", "error", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		s.historyCacheJSON = data
		s.historyCacheGen = gen
	}
	data := s.historyCacheJSON
	s.historyCacheMu.Unlock()

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(data)
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
