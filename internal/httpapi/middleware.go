package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"
)

// withLogging logs every request's method, path, status code, and
// duration at debug level.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.log.Debug("http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// withAPIKey requires a matching bearer token or X-Api-Key header when
// cfg.APIKey is set. When cfg.APIKey is empty (the default), all requests
// are allowed, keeping the common case (dashboard on a trusted LAN)
// unauthenticated and simple.
func (s *Server) withAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.cfg.APIKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		// Hash both keys so the comparison runs in constant time over a
		// fixed length, leaking neither key bytes nor the configured
		// key's length through response timing.
		provided := sha256.Sum256([]byte(providedAPIKey(r)))
		expected := sha256.Sum256([]byte(s.cfg.APIKey))
		if subtle.ConstantTimeCompare(provided[:], expected[:]) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func providedAPIKey(r *http.Request) string {
	if key := r.Header.Get("X-Api-Key"); key != "" {
		return key
	}
	if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	return ""
}

// statusRecorder captures the status code written by a downstream
// handler, since http.ResponseWriter doesn't expose it directly.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
