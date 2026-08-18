package alert

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// receivedRequest captures one delivered webhook POST.
type receivedRequest struct {
	body        []byte
	contentType string
}

// newCapturingServer returns an httptest server that pushes each received
// request body onto the returned channel and replies with the given status.
func newCapturingServer(t *testing.T, status int) (*httptest.Server, <-chan receivedRequest) {
	t.Helper()
	ch := make(chan receivedRequest, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ch <- receivedRequest{body: body, contentType: r.Header.Get("Content-Type")}
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, ch
}

// firedEvent is a convenient crit "fired" transition at a fixed time.
func firedEvent(at time.Time) Event {
	return Event{Metric: "cpu", Kind: KindFired, From: LevelOK, To: LevelCrit, Value: 98, At: at}
}

func waitForRequest(t *testing.T, ch <-chan receivedRequest) receivedRequest {
	t.Helper()
	select {
	case req := <-ch:
		return req
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for webhook delivery")
		return receivedRequest{}
	}
}

// Pointed at a local httptest server, firing an alert must deliver the
// expected default JSON payload (acceptance criterion).
func TestNotifier_DeliversDefaultPayload(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: srv.URL}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	at := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	n.Notify([]Event{firedEvent(at)})

	req := waitForRequest(t, ch)
	if req.contentType != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", req.contentType)
	}

	var got eventView
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("payload is not valid JSON: %v (body=%s)", err, req.body)
	}
	if got.Metric != "cpu" || got.Kind != KindFired || got.To != LevelCrit {
		t.Errorf("payload = %+v, want cpu fired->crit", got)
	}
	if got.Value != 98 {
		t.Errorf("payload value = %v, want 98", got.Value)
	}
	if !got.At.Equal(at) {
		t.Errorf("payload at = %v, want %v", got.At, at)
	}
	if got.Message == "" {
		t.Error("payload message should be non-empty")
	}
}

// A custom template controls the request body.
func TestNotifier_RendersTemplate(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{
			URL:      srv.URL,
			Template: `{"text":"{{.Metric}} is {{.To}}"}`,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	req := waitForRequest(t, ch)
	if string(req.body) != `{"text":"cpu is crit"}` {
		t.Errorf("templated body = %s, want {\"text\":\"cpu is crit\"}", req.body)
	}
}

// A custom content_type overrides the default, so a plain-text template body
// isn't mislabeled as JSON.
func TestNotifier_CustomContentType(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{
			URL:         srv.URL,
			Template:    `{{.Message}}`,
			ContentType: "text/plain",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	req := waitForRequest(t, ch)
	if req.contentType != "text/plain" {
		t.Errorf("Content-Type = %q, want text/plain", req.contentType)
	}
}

// A malformed template is rejected at construction so it fails fast at
// startup rather than silently at delivery time.
func TestNewNotifier_BadTemplate(t *testing.T) {
	_, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: "http://example", Template: "{{.Metric"}},
	}, nil)
	if err == nil {
		t.Fatal("expected an error for a malformed template")
	}
}

// No webhooks means no notifier (notifications disabled), reported as a nil
// return without error.
func TestNewNotifier_NoWebhooks(t *testing.T) {
	n, err := NewNotifier(config.Alerts{}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	if n != nil {
		t.Fatalf("expected nil notifier when no webhooks configured, got %+v", n)
	}
}

// A failing delivery retries with backoff and then gives up, without crashing
// or blocking (acceptance criterion). The worker must survive and the total
// number of attempts must equal 1 + maxRetries.
func TestNotifier_RetriesThenGivesUp(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	n, err := NewNotifier(config.Alerts{
		Webhooks:                  []config.Webhook{{URL: srv.URL}},
		NotifyMaxRetries:          2,
		NotifyRetryBackoffSeconds: 0.001, // 1ms so the test is fast
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	// Expect exactly 1 initial attempt + 2 retries = 3.
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&attempts) < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
		case <-time.After(time.Millisecond):
		}
	}
	// Give it a moment to ensure it does not keep retrying past the limit.
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&attempts); got != 3 {
		t.Fatalf("expected exactly 3 attempts (1 + 2 retries), got %d", got)
	}
}

// The per-webhook severity filter drops events that don't reach min_level.
// The worker processes the queue in order, so sending a warn event followed by
// a crit event to a crit-only webhook must deliver only the crit one.
func TestNotifier_MinLevelFiltersLowSeverity(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: srv.URL, MinLevel: "crit"}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	warn := Event{Metric: "cpu", Kind: KindFired, From: LevelOK, To: LevelWarn, Value: 85, At: time.Now()}
	crit := Event{Metric: "temperature", Kind: KindFired, From: LevelOK, To: LevelCrit, Value: 90, At: time.Now()}
	n.Notify([]Event{warn, crit})

	req := waitForRequest(t, ch)
	var got eventView
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if got.Metric != "temperature" || got.To != LevelCrit {
		t.Fatalf("expected only the crit event delivered, got %+v", got)
	}
	// No further delivery should arrive (the warn event was filtered out).
	select {
	case extra := <-ch:
		t.Fatalf("unexpected extra delivery: %s", extra.body)
	case <-time.After(100 * time.Millisecond):
	}
}

// Rate limiting drops events that arrive within min_interval of the previous
// delivery to the same URL. Timestamps are event-driven, so this is
// deterministic.
func TestNotifier_RateLimitsPerWebhook(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks:                 []config.Webhook{{URL: srv.URL}},
		NotifyMinIntervalSeconds: 60,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := firedEvent(base)
	// A second firing of the same metric only 1s later — well within the 60s
	// rate-limit window — must be coalesced away.
	second := firedEvent(base.Add(time.Second))
	n.Notify([]Event{first, second})

	req := waitForRequest(t, ch)
	var got eventView
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if got.Kind != KindFired || !got.At.Equal(base) {
		t.Fatalf("expected the first firing delivered, got %+v", got)
	}
	select {
	case extra := <-ch:
		t.Fatalf("rate-limited second firing should not have been delivered: %s", extra.body)
	case <-time.After(100 * time.Millisecond):
	}
}

// A cleared event is never rate-limited: even when it arrives within the
// rate-limit window of the preceding fired delivery, the recovery signal must
// still be delivered so a state-based downstream consumer can't get stuck
// reporting an alert that has actually cleared.
func TestNotifier_ClearedBypassesRateLimit(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks:                 []config.Webhook{{URL: srv.URL}},
		NotifyMinIntervalSeconds: 60,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fired := firedEvent(base)
	// Cleared only 1s later, deep inside the 60s window.
	cleared := Event{Metric: "cpu", Kind: KindCleared, From: LevelCrit, To: LevelOK, Value: 10, At: base.Add(time.Second)}
	n.Notify([]Event{fired, cleared})

	kinds := map[string]bool{}
	for i := 0; i < 2; i++ {
		req := waitForRequest(t, ch)
		var v eventView
		if err := json.Unmarshal(req.body, &v); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		kinds[v.Kind] = true
	}
	if !kinds[KindFired] || !kinds[KindCleared] {
		t.Fatalf("expected both fired and cleared delivered, got %v", kinds)
	}
}

// Rate limiting is keyed per metric, so two distinct metrics firing in the
// same tick are both delivered even within the rate-limit window.
func TestNotifier_RateLimitIsPerMetric(t *testing.T) {
	srv, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks:                 []config.Webhook{{URL: srv.URL}},
		NotifyMinIntervalSeconds: 60,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cpu := Event{Metric: "cpu", Kind: KindFired, From: LevelOK, To: LevelCrit, Value: 98, At: at}
	disk := Event{Metric: "disk", Resource: "/", Kind: KindFired, From: LevelOK, To: LevelCrit, Value: 99, At: at}
	n.Notify([]Event{cpu, disk})

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		req := waitForRequest(t, ch)
		var v eventView
		if err := json.Unmarshal(req.body, &v); err != nil {
			t.Fatalf("bad payload: %v", err)
		}
		got[v.Metric] = true
	}
	if !got["cpu"] || !got["disk"] {
		t.Fatalf("expected both cpu and disk delivered, got %v", got)
	}
}

// A delivery that fails all retries must not be treated as "sent" for rate
// limiting purposes: the next firing of the same metric, even inside the
// rate-limit window, must still be delivered once the endpoint recovers
// (regression test for #62).
func TestNotifier_RateLimitDoesNotCountFailedDelivery(t *testing.T) {
	var failing atomic.Bool
	failing.Store(true)
	ch := make(chan receivedRequest, 16)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if failing.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		ch <- receivedRequest{body: body, contentType: r.Header.Get("Content-Type")}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	n, err := NewNotifier(config.Alerts{
		Webhooks:                  []config.Webhook{{URL: srv.URL}},
		NotifyMinIntervalSeconds:  60,
		NotifyMaxRetries:          0,
		NotifyRetryBackoffSeconds: 0.001,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	n.Notify([]Event{firedEvent(base)})

	// The first delivery fails; no request is ever pushed onto ch for it, so
	// give the worker time to exhaust its (zero) retries before proceeding.
	time.Sleep(50 * time.Millisecond)

	// The endpoint recovers, and a second firing of the same metric arrives
	// 1s later — deep inside the 60s rate-limit window. Because the first
	// delivery failed, this one must still go out.
	failing.Store(false)
	n.Notify([]Event{firedEvent(base.Add(time.Second))})

	req := waitForRequest(t, ch)
	var got eventView
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if !got.At.Equal(base.Add(time.Second)) {
		t.Fatalf("expected the second firing delivered after the first failed, got %+v", got)
	}
}

func TestEventReaches(t *testing.T) {
	cases := []struct {
		name string
		ev   Event
		min  Level
		want bool
	}{
		{"warn fired reaches warn", Event{From: LevelOK, To: LevelWarn}, LevelWarn, true},
		{"warn fired below crit", Event{From: LevelOK, To: LevelWarn}, LevelCrit, false},
		{"crit fired reaches crit", Event{From: LevelWarn, To: LevelCrit}, LevelCrit, true},
		{"clear from crit reaches crit", Event{From: LevelCrit, To: LevelWarn}, LevelCrit, true},
		{"clear from warn below crit", Event{From: LevelWarn, To: LevelOK}, LevelCrit, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := eventReaches(c.ev, c.min); got != c.want {
				t.Fatalf("eventReaches(%+v, %s) = %v, want %v", c.ev, c.min, got, c.want)
			}
		})
	}
}

// A permanent client error (e.g. 404 from a revoked webhook URL) must not be
// retried: re-POSTing the identical body cannot change the answer, and the
// retry budget would only delay this webhook's remaining events
// (regression test for #63).
func TestNotifier_DoesNotRetryPermanentClientError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	n, err := NewNotifier(config.Alerts{
		Webhooks:                  []config.Webhook{{URL: srv.URL}},
		NotifyMaxRetries:          3,
		NotifyRetryBackoffSeconds: 0.001,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	// Give the worker ample time to run any retries it might (wrongly) attempt.
	time.Sleep(100 * time.Millisecond)
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected exactly 1 attempt for a 404, got %d", got)
	}
}

// 429 Too Many Requests explicitly invites a retry, so unlike other 4xx it
// must still consume the retry budget.
func TestNotifier_RetriesRetryableClientError(t *testing.T) {
	var attempts int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	n, err := NewNotifier(config.Alerts{
		Webhooks:                  []config.Webhook{{URL: srv.URL}},
		NotifyMaxRetries:          2,
		NotifyRetryBackoffSeconds: 0.001,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&attempts) < 3 {
		select {
		case <-deadline:
			t.Fatalf("expected 3 attempts (1 + 2 retries) for a 429, got %d", atomic.LoadInt32(&attempts))
		case <-time.After(time.Millisecond):
		}
	}
}

func TestRetryable(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"transport error", errors.New("connection refused"), true},
		{"400 bad request", &statusError{code: http.StatusBadRequest}, false},
		{"404 not found", &statusError{code: http.StatusNotFound}, false},
		{"410 gone", &statusError{code: http.StatusGone}, false},
		{"408 request timeout", &statusError{code: http.StatusRequestTimeout}, true},
		{"429 too many requests", &statusError{code: http.StatusTooManyRequests}, true},
		{"500 internal server error", &statusError{code: http.StatusInternalServerError}, true},
		{"503 service unavailable", &statusError{code: http.StatusServiceUnavailable}, true},
		{"304 not modified", &statusError{code: http.StatusNotModified}, true},
		{"wrapped 403", fmt.Errorf("post failed: %w", &statusError{code: http.StatusForbidden}), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := retryable(c.err); got != c.want {
				t.Fatalf("retryable(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

// A hung webhook must not delay deliveries to the other configured webhooks:
// each destination drains its own queue on its own goroutine, so a healthy
// endpoint gets its event while a dead one is still blocked
// (regression test for #63).
func TestNotifier_SlowWebhookDoesNotBlockOthers(t *testing.T) {
	release := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		w.WriteHeader(http.StatusOK)
	}))
	defer slow.Close()
	defer close(release)

	fast, ch := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: slow.URL}, {URL: fast.URL}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Now())})

	// The fast webhook must be delivered while the slow one is still hanging.
	waitForRequest(t, ch)
}

// Rate-limit state is per webhook, so a delivery to one destination must not
// suppress the same event on another.
func TestNotifier_RateLimitStateIsPerWebhook(t *testing.T) {
	first, ch1 := newCapturingServer(t, http.StatusOK)
	second, ch2 := newCapturingServer(t, http.StatusOK)

	n, err := NewNotifier(config.Alerts{
		Webhooks:                 []config.Webhook{{URL: first.URL}, {URL: second.URL}},
		NotifyMinIntervalSeconds: 60,
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	n.Start(ctx)

	n.Notify([]Event{firedEvent(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))})

	waitForRequest(t, ch1)
	waitForRequest(t, ch2)
}
