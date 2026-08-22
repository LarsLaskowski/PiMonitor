package alert

// notify.go delivers alert transition events to configured HTTP webhooks.
//
// Delivery is fully decoupled from metric collection: Notify only enqueues
// events onto bounded channels and returns immediately, and background workers
// drain those queues and perform the (potentially slow, retrying) HTTP POSTs.
// This guarantees that a hung or failing webhook can never block the
// collector's fast tick.
//
// Each webhook gets its own queue and worker, so a dead or slow endpoint can
// only delay its own deliveries — it can never head-of-line-block the other
// webhooks. Attempts that fail with a permanent client error (most 4xx) are
// not retried at all, since re-POSTing the same body cannot change the answer.
//
// A single generic webhook — a URL plus an optional Go text/template body —
// is enough to target Slack, Discord, Home Assistant, ntfy, and similar
// incoming-webhook endpoints.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"text/template"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// Notifier defaults. These apply when the corresponding config value is zero.
const (
	defaultNotifyTimeout     = 10 * time.Second
	defaultNotifyQueueSize   = 256
	defaultNotifyContentType = "application/json"
)

// webhook is a single resolved delivery destination: the config values with
// the template pre-parsed and defaults applied.
type webhook struct {
	url         string
	minLevel    Level
	tmpl        *template.Template // nil for the default JSON payload
	timeout     time.Duration
	contentType string
}

// Notifier POSTs alert transition events to configured webhooks. Construct it
// with NewNotifier, call Start once to launch its worker, and feed it events
// via Notify. It is safe for concurrent use.
type Notifier struct {
	workers     []*webhookWorker
	client      *http.Client
	maxRetries  int
	backoff     time.Duration
	minInterval time.Duration
	log         *slog.Logger

	wg sync.WaitGroup
}

// webhookWorker owns everything that belongs to exactly one destination: its
// resolved config, its own bounded queue, and its rate-limiting state. Giving
// every webhook a private queue and goroutine is what keeps one unreachable
// endpoint from delaying deliveries to the healthy ones.
type webhookWorker struct {
	wh    webhook
	queue chan Event

	// lastSent tracks the last delivery time per (metric, resource) for rate
	// limiting. Only this webhook's own goroutine touches it, so it needs no
	// lock.
	lastSent map[string]time.Time
}

// NewNotifier builds a Notifier from the alerts configuration. It returns nil
// (and no error) when no webhooks are configured, so callers can treat a nil
// Notifier as "notifications disabled". An error is returned only for a
// malformed webhook template, so a typo fails fast at startup.
func NewNotifier(cfg config.Alerts, log *slog.Logger) (*Notifier, error) {
	if len(cfg.Webhooks) == 0 {
		return nil, nil
	}
	if log == nil {
		log = slog.Default()
	}

	workers := make([]*webhookWorker, 0, len(cfg.Webhooks))
	for i, w := range cfg.Webhooks {
		wh := webhook{
			url:         w.URL,
			minLevel:    parseMinLevel(w.MinLevel),
			timeout:     time.Duration(w.TimeoutSeconds * float64(time.Second)),
			contentType: w.ContentType,
		}
		if wh.timeout <= 0 {
			wh.timeout = defaultNotifyTimeout
		}
		if wh.contentType == "" {
			wh.contentType = defaultNotifyContentType
		}
		if w.Template != "" {
			tmpl, err := template.New(fmt.Sprintf("webhook[%d]", i)).Parse(w.Template)
			if err != nil {
				return nil, fmt.Errorf("alerts.webhooks[%d].template: %w", i, err)
			}
			wh.tmpl = tmpl
		}
		workers = append(workers, &webhookWorker{
			wh:       wh,
			queue:    make(chan Event, defaultNotifyQueueSize),
			lastSent: make(map[string]time.Time),
		})
	}

	return &Notifier{
		workers:     workers,
		client:      &http.Client{},
		maxRetries:  cfg.NotifyMaxRetries,
		backoff:     time.Duration(cfg.NotifyRetryBackoffSeconds * float64(time.Second)),
		minInterval: time.Duration(cfg.NotifyMinIntervalSeconds * float64(time.Second)),
		log:         log,
	}, nil
}

// parseMinLevel maps a config severity string to a Level, defaulting to warn
// (config.Alerts.validate has already rejected anything else).
func parseMinLevel(s string) Level {
	switch s {
	case "crit":
		return LevelCrit
	default:
		return LevelWarn
	}
}

// Start launches one background delivery worker per configured webhook. It
// returns immediately; the workers run until ctx is canceled, at which point
// in-flight retries stop promptly and any queued-but-undelivered events are
// dropped. Call Start at most once.
func (n *Notifier) Start(ctx context.Context) {
	for _, w := range n.workers {
		n.wg.Add(1)
		go func(w *webhookWorker) {
			defer n.wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case ev := <-w.queue:
					n.dispatch(ctx, w, ev)
				}
			}
		}(w)
	}
}

// Stop waits for the delivery workers to exit. It must be called only after
// the context passed to Start has been canceled, otherwise it blocks until
// that happens; any events still queued at cancellation are dropped, since
// delivery is best-effort. It joins the worker goroutines so a caller can be
// sure no delivery is still in flight once Stop returns.
func (n *Notifier) Stop() {
	n.wg.Wait()
}

// Notify fans each event out onto the queue of every webhook it matches,
// applying the (cheap, stateless) per-webhook severity filter inline. It never
// blocks: if a webhook's queue is full (a backlog of slow deliveries to that
// endpoint), the event is dropped for that webhook with a warning rather than
// stalling the collector — and only that webhook is affected.
func (n *Notifier) Notify(events []Event) {
	for _, ev := range events {
		for _, w := range n.workers {
			if !eventReaches(ev, w.wh.minLevel) {
				continue
			}
			select {
			case w.queue <- ev:
			default:
				n.log.Warn("alert notification queue full, dropping event",
					"url", w.wh.url, "metric", ev.Metric, "resource", ev.Resource, "kind", ev.Kind)
			}
		}
	}
}

// dispatch delivers one event to one webhook, applying that webhook's rate
// limit. It runs on the webhook's own worker goroutine.
func (n *Notifier) dispatch(ctx context.Context, w *webhookWorker, ev Event) {
	// cleared events bypass the rate limiter: a recovery signal must always be
	// delivered so a state-based consumer (e.g. a Home Assistant
	// binary_sensor) can never get stuck reporting an alert that has actually
	// cleared. The limiter only coalesces repeated firings.
	if ev.Kind != KindCleared && n.rateLimited(w, ev) {
		n.log.Warn("alert notification rate-limited, dropping event",
			"url", w.wh.url, "metric", ev.Metric, "resource", ev.Resource, "kind", ev.Kind)
		return
	}
	body, err := renderBody(w.wh, ev)
	if err != nil {
		n.log.Error("alert notification render failed", "url", w.wh.url, "error", err)
		return
	}
	if n.deliver(ctx, w.wh, body) {
		n.recordSent(w, ev)
	}
}

// rateLimited reports whether delivering ev to w should be suppressed because
// the previous successful delivery of the same metric to the same webhook was
// too recent. The state lives on the worker and is keyed per
// (metric, resource) so a fast-flapping metric can't flood a webhook, while a
// distinct metric alerting in the same tick is still delivered. Only firing
// events reach here (cleared events bypass the limiter in dispatch), so it
// purely coalesces repeated escalations. The event timestamp (not wall clock)
// drives the decision so it is deterministic and testable.
func (n *Notifier) rateLimited(w *webhookWorker, ev Event) bool {
	if n.minInterval <= 0 {
		return false
	}
	last, ok := w.lastSent[rateLimitKey(ev)]
	return ok && ev.At.Sub(last) < n.minInterval
}

// recordSent stamps the last-delivery time for (metric, resource) after a
// successful delivery, so the rate limiter counts deliveries rather than
// attempts: a failed delivery must not suppress the next firing.
func (n *Notifier) recordSent(w *webhookWorker, ev Event) {
	if n.minInterval <= 0 {
		return
	}
	w.lastSent[rateLimitKey(ev)] = ev.At
}

// rateLimitKey identifies the alert stream an event belongs to within one
// webhook. The NUL separator keeps metric and resource unambiguous.
func rateLimitKey(ev Event) string {
	return ev.Metric + "\x00" + ev.Resource
}

// deliver POSTs body to a webhook, retrying with exponential backoff on
// failure until it succeeds, exhausts maxRetries, hits a permanent error, or
// ctx is canceled. It reports whether delivery ultimately succeeded, and
// always returns without panicking so a dead endpoint can't crash the worker.
func (n *Notifier) deliver(ctx context.Context, wh webhook, body []byte) bool {
	backoff := n.backoff
	for attempt := 0; ; attempt++ {
		err := n.post(ctx, wh, body)
		switch {
		case err == nil:
			return true
		case !retryable(err):
			// A rejected request (bad URL, bad payload, revoked webhook) will
			// be rejected again identically, so burning the retry budget only
			// delays this webhook's remaining events for no benefit.
			n.log.Error("alert notification rejected, not retrying",
				"url", wh.url, "attempts", attempt+1, "error", err)
			return false
		case attempt >= n.maxRetries:
			n.log.Error("alert notification giving up after retries",
				"url", wh.url, "attempts", attempt+1, "error", err)
			return false
		default:
			n.log.Warn("alert notification delivery failed, will retry",
				"url", wh.url, "attempt", attempt+1, "error", err)
		}
		if !sleepCtx(ctx, backoff) {
			return false // context canceled during backoff
		}
		backoff *= 2
	}
}

// statusError is a non-2xx webhook response.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("webhook returned status %d", e.code) }

// retryable reports whether a failed delivery attempt is worth repeating.
// Transport failures (DNS, refused connections, timeouts) are transient by
// nature, so they always are. HTTP status errors only are when the server
// might answer differently for an identical request: 5xx (server-side
// trouble), plus 408 Request Timeout and 429 Too Many Requests, which
// explicitly invite a retry. Every other 4xx is a permanent rejection of this
// request.
func retryable(err error) bool {
	var se *statusError
	if !errors.As(err, &se) {
		return true
	}
	switch se.code {
	case http.StatusRequestTimeout, http.StatusTooManyRequests:
		return true
	}
	return se.code < 400 || se.code >= 500
}

// post performs a single delivery attempt, returning an error for a transport
// failure or a non-2xx response.
func (n *Notifier) post(ctx context.Context, wh webhook, body []byte) error {
	reqCtx, cancel := context.WithTimeout(ctx, wh.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, wh.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", wh.contentType)

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &statusError{code: resp.StatusCode}
	}
	return nil
}

// sleepCtx waits for d or until ctx is canceled, returning true if it slept
// the full duration and false if ctx was canceled first. A non-positive d
// returns true immediately.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// eventView is the data made available to a webhook template and the shape of
// the default JSON payload. Value keeps its native type and At marshals to
// RFC3339, matching the /api/v1/alerts event encoding.
type eventView struct {
	Metric   string    `json:"metric"`
	Resource string    `json:"resource,omitempty"`
	Kind     string    `json:"kind"`
	From     Level     `json:"from"`
	To       Level     `json:"to"`
	Value    float64   `json:"value"`
	At       time.Time `json:"at"`
	Message  string    `json:"message"`
}

// renderBody builds the request body for an event: the webhook's template if
// set, otherwise the default JSON payload.
func renderBody(wh webhook, ev Event) ([]byte, error) {
	view := makeView(ev)
	if wh.tmpl != nil {
		var buf bytes.Buffer
		if err := wh.tmpl.Execute(&buf, view); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	return json.Marshal(view)
}

// makeView adapts an Event into the template/JSON view, adding a
// human-readable one-line message.
func makeView(ev Event) eventView {
	return eventView{
		Metric:   ev.Metric,
		Resource: ev.Resource,
		Kind:     ev.Kind,
		From:     ev.From,
		To:       ev.To,
		Value:    ev.Value,
		At:       ev.At,
		Message:  formatMessage(ev),
	}
}

// formatMessage renders a short human-readable summary, e.g.
// "cpu fired: ok -> crit (98.0)" or "disk (/data) cleared: crit -> ok (60.0)".
func formatMessage(ev Event) string {
	resource := ""
	if ev.Resource != "" {
		resource = " (" + ev.Resource + ")"
	}
	return fmt.Sprintf("%s%s %s: %s -> %s (%.1f)",
		ev.Metric, resource, ev.Kind, ev.From, ev.To, ev.Value)
}

// eventReaches reports whether an event's severity reaches at least min. An
// event is relevant to a webhook when either side of the transition is at or
// above the webhook's minimum level, so a "min_level: crit" webhook still
// receives the cleared event when a metric drops out of crit.
func eventReaches(ev Event, min Level) bool {
	peak := severity(ev.From)
	if s := severity(ev.To); s > peak {
		peak = s
	}
	return peak >= severity(min)
}
