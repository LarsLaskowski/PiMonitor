package alert

import (
	"context"
	"encoding/json"
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
	// Only 1s later — well within the 60s rate-limit window.
	second := Event{Metric: "cpu", Kind: KindCleared, From: LevelCrit, To: LevelOK, Value: 10, At: base.Add(time.Second)}
	n.Notify([]Event{first, second})

	req := waitForRequest(t, ch)
	var got eventView
	if err := json.Unmarshal(req.body, &got); err != nil {
		t.Fatalf("bad payload: %v", err)
	}
	if got.Kind != KindFired {
		t.Fatalf("expected the first (fired) event delivered, got %+v", got)
	}
	select {
	case extra := <-ch:
		t.Fatalf("rate-limited second event should not have been delivered: %s", extra.body)
	case <-time.After(100 * time.Millisecond):
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
