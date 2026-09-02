package httpapi

import (
	"compress/gzip"
	"crypto/sha256"
	"crypto/subtle"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// withLogging records self-metrics for every request (see serverStats) and,
// when cfg.AccessLogEnabled is true, also logs the request's method, path,
// status code, and duration at debug level. The counters are recorded
// unconditionally so GET /api/v1/serverstats keeps reporting request volume
// even when per-request debug logging has been turned off.
func (s *Server) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		s.stats.record(routeBucket(r.URL.Path), rec.status)
		if s.cfg.AccessLogEnabled {
			s.log.Debug("http request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration", time.Since(start),
			)
		}
	})
}

// withSecurityHeaders sets standard security response headers on every
// response. The CSP is strict because the dashboard is fully self-contained:
// all scripts and styles are same-origin files and the favicon is a data:
// URI (hence the data: allowance for img-src). frame-ancestors 'none' plus
// X-Frame-Options DENY prevent the dashboard from being framed
// (clickjacking); the latter covers older browsers without CSP2 support.
func (s *Server) withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data:; script-src 'self'; style-src 'self'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
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
		// key's length through response timing. This is a live compare, not
		// storage-at-rest, so SHA-256 is the right tool here: a slow,
		// storage-oriented hash (bcrypt/argon2) would add ~100ms of CPU per
		// request on the Pi for no security benefit against this threat
		// model, and would itself be a DoS vector.
		// codeql[go/weak-sensitive-data-hashing]
		provided := sha256.Sum256([]byte(providedAPIKey(r)))
		// codeql[go/weak-sensitive-data-hashing]
		expected := sha256.Sum256([]byte(s.cfg.APIKey))
		if subtle.ConstantTimeCompare(provided[:], expected[:]) == 1 {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

// withMaxInFlight bounds concurrent request processing across every
// endpoint sharing s.inFlight (see defaultMaxInFlight). The history
// endpoint deep-copies and serialises the whole retained window on a cache
// miss, so an unbounded number of concurrent callers can starve the
// collector's own tick on a single-core Pi. Excess requests get 503 with
// Retry-After rather than being queued indefinitely, so a client backs off
// instead of piling up.
func (s *Server) withMaxInFlight(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.inFlight <- struct{}{}:
			defer func() { <-s.inFlight }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "server busy", http.StatusServiceUnavailable)
		}
	})
}

// withNoStore marks API responses as non-cacheable. Metric snapshots are
// point-in-time data with no reuse value, and when cfg.APIKey is set they are
// credential-protected — a shared cache (e.g. a reverse proxy fronting the Pi)
// must not retain them or hand an authorised response to an unauthorised
// client. Vary names the credential headers as defence in depth for any cache
// that ignores no-store.
func (s *Server) withNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Cache-Control", "no-store")
		h.Add("Vary", "Authorization")
		h.Add("Vary", "X-Api-Key")
		next.ServeHTTP(w, r)
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

// gzipWriterPool amortizes gzip.Writer allocation across requests: writers
// are relatively expensive to construct, and the API can be polled every
// few seconds by the dashboard and third-party integrations alike.
var gzipWriterPool = sync.Pool{
	New: func() any {
		gw, _ := gzip.NewWriterLevel(io.Discard, gzip.BestSpeed)
		return gw
	},
}

// gzipResponseWriter wraps an http.ResponseWriter so that Write goes
// through a pooled *gzip.Writer instead of straight to the connection.
// WriteHeader is forwarded unchanged via the embedded ResponseWriter.
type gzipResponseWriter struct {
	http.ResponseWriter
	gw *gzip.Writer
}

func (w *gzipResponseWriter) Write(b []byte) (int, error) {
	return w.gw.Write(b)
}

// acceptsGzip reports whether the client accepts a gzip-encoded response.
// Accept-Encoding is a q-value list (RFC 9110 §12.5.3), not an opaque string:
// "gzip;q=0" is an explicit refusal and "x-gzip" is a distinct (deprecated)
// token, so substring-matching "gzip" would compress for clients that asked
// us not to. An explicit gzip entry outranks a wildcard, per the same
// section, so "gzip;q=0, *" is still a refusal.
func acceptsGzip(header string) bool {
	wildcard := false
	for _, part := range strings.Split(header, ",") {
		coding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		accepted := !qualityIsZero(params)
		switch strings.ToLower(strings.TrimSpace(coding)) {
		case "gzip":
			return accepted
		case "*":
			wildcard = accepted
		}
	}
	return wildcard
}

// qualityIsZero reports whether an Accept-Encoding parameter list carries a
// qvalue of zero, in any of the forms RFC 9110 permits ("0", "0.", "0.0",
// "0.00", "0.000"). A qvalue that doesn't parse is ignored rather than read
// as a refusal, so a malformed header keeps the pre-existing behaviour of
// compressing instead of silently losing compression.
func qualityIsZero(params string) bool {
	for _, p := range strings.Split(params, ";") {
		k, v, ok := strings.Cut(p, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "q") {
			continue
		}
		q, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return err == nil && q == 0
	}
	return false
}

// withGzip transparently gzip-compresses responses for clients that
// advertise support via Accept-Encoding. Responses to history/metrics
// payloads are highly repetitive JSON and compress roughly 10x, which
// matters on a Pi Zero polled over Wi-Fi. Clients that don't send
// Accept-Encoding: gzip (naive/older /api/v1 consumers) receive identity
// responses unchanged, so this is backward compatible.
func (s *Server) withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r.Header.Get("Accept-Encoding")) {
			next.ServeHTTP(w, r)
			return
		}

		h := w.Header()
		h.Set("Content-Encoding", "gzip")
		h.Add("Vary", "Accept-Encoding")
		h.Del("Content-Length")

		gw := gzipWriterPool.Get().(*gzip.Writer)
		gw.Reset(w)
		defer func() {
			_ = gw.Close()
			gzipWriterPool.Put(gw)
		}()

		next.ServeHTTP(&gzipResponseWriter{ResponseWriter: w, gw: gw}, r)
	})
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
