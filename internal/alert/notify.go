package alert

// notify.go delivers alert transition events to configured HTTP webhooks.
//
// Delivery is fully decoupled from metric collection: Notify only enqueues
// events onto a bounded channel and returns immediately, and a single
// background worker drains the queue and performs the (potentially slow,
// retrying) HTTP POSTs. This guarantees that a hung or failing webhook can
// never block the collector's fast tick.
//
// A single generic webhook — a URL plus an optional Go text/template body —
// is enough to target Slack, Discord, Home Assistant, ntfy, and similar
// incoming-webhook endpoints.

import (
	"bytes"
	"context"
	"encoding/json"
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
	defaultNotifyTimeout   = 10 * time.Second
	defaultNotifyQueueSize = 256
)

// webhook is a single resolved delivery destination: the config values with
// the template pre-parsed and defaults applied.
type webhook struct {
	url      string
	minLevel Level
	tmpl     *template.Template // nil for the default JSON payload
	timeout  time.Duration
}

// Notifier POSTs alert transition events to configured webhooks. Construct it
// with NewNotifier, call Start once to launch its worker, and feed it events
// via Notify. It is safe for concurrent use.
type Notifier struct {
	webhooks    []webhook
	client      *http.Client
	maxRetries  int
	backoff     time.Duration
	minInterval time.Duration
	log         *slog.Logger

	queue chan Event
	wg    sync.WaitGroup

	// lastSent tracks the last delivery time per webhook URL for rate
	// limiting. Only the worker goroutine touches it, so it needs no lock.
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

	webhooks := make([]webhook, 0, len(cfg.Webhooks))
	for i, w := range cfg.Webhooks {
		wh := webhook{
			url:      w.URL,
			minLevel: parseMinLevel(w.MinLevel),
			timeout:  time.Duration(w.TimeoutSeconds * float64(time.Second)),
		}
		if wh.timeout <= 0 {
			wh.timeout = defaultNotifyTimeout
		}
		if w.Template != "" {
			tmpl, err := template.New(fmt.Sprintf("webhook[%d]", i)).Parse(w.Template)
			if err != nil {
				return nil, fmt.Errorf("alerts.webhooks[%d].template: %w", i, err)
			}
			wh.tmpl = tmpl
		}
		webhooks = append(webhooks, wh)
	}

	return &Notifier{
		webhooks:    webhooks,
		client:      &http.Client{},
		maxRetries:  cfg.NotifyMaxRetries,
		backoff:     time.Duration(cfg.NotifyRetryBackoffSeconds * float64(time.Second)),
		minInterval: time.Duration(cfg.NotifyMinIntervalSeconds * float64(time.Second)),
		log:         log,
		queue:       make(chan Event, defaultNotifyQueueSize),
		lastSent:    make(map[string]time.Time),
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

// Start launches the background delivery worker. It returns immediately; the
// worker runs until ctx is canceled, at which point in-flight retries stop
// promptly and any queued-but-undelivered events are dropped. Call Start at
// most once.
func (n *Notifier) Start(ctx context.Context) {
	n.wg.Add(1)
	go func() {
		defer n.wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-n.queue:
				n.dispatch(ctx, ev)
			}
		}
	}()
}

// Notify enqueues events for asynchronous delivery. It never blocks: if the
// queue is full (a backlog of slow deliveries), the event is dropped with a
// warning rather than stalling the collector.
func (n *Notifier) Notify(events []Event) {
	for _, ev := range events {
		select {
		case n.queue <- ev:
		default:
			n.log.Warn("alert notification queue full, dropping event",
				"metric", ev.Metric, "resource", ev.Resource, "kind", ev.Kind)
		}
	}
}

// dispatch delivers one event to every webhook it matches, applying the
// per-webhook severity filter and rate limit.
func (n *Notifier) dispatch(ctx context.Context, ev Event) {
	for _, wh := range n.webhooks {
		if !eventReaches(ev, wh.minLevel) {
			continue
		}
		if n.rateLimited(wh.url, ev) {
			n.log.Warn("alert notification rate-limited, dropping event",
				"url", wh.url, "metric", ev.Metric, "resource", ev.Resource, "kind", ev.Kind)
			continue
		}
		body, err := renderBody(wh, ev)
		if err != nil {
			n.log.Error("alert notification render failed", "url", wh.url, "error", err)
			continue
		}
		n.deliver(ctx, wh, body)
	}
}

// rateLimited reports whether delivering ev to url should be suppressed
// because the previous delivery of the same metric to the same URL was too
// recent, and records the time otherwise. The rate limit is keyed per
// (url, metric, resource) so a fast-flapping metric can't flood a webhook,
// while a distinct metric alerting in the same tick is still delivered. The
// event timestamp (not wall clock) drives the decision so it is deterministic
// and testable.
func (n *Notifier) rateLimited(url string, ev Event) bool {
	if n.minInterval <= 0 {
		return false
	}
	key := url + "\x00" + ev.Metric + "\x00" + ev.Resource
	if last, ok := n.lastSent[key]; ok && ev.At.Sub(last) < n.minInterval {
		return true
	}
	n.lastSent[key] = ev.At
	return false
}

// deliver POSTs body to a webhook, retrying with exponential backoff on
// failure until it succeeds, exhausts maxRetries, or ctx is canceled. It
// always returns without panicking so a dead endpoint can't crash the worker.
func (n *Notifier) deliver(ctx context.Context, wh webhook, body []byte) {
	backoff := n.backoff
	for attempt := 0; ; attempt++ {
		if err := n.post(ctx, wh, body); err == nil {
			return
		} else if attempt >= n.maxRetries {
			n.log.Error("alert notification giving up after retries",
				"url", wh.url, "attempts", attempt+1, "error", err)
			return
		} else {
			n.log.Warn("alert notification delivery failed, will retry",
				"url", wh.url, "attempt", attempt+1, "error", err)
		}
		if !sleepCtx(ctx, backoff) {
			return // context canceled during backoff
		}
		backoff *= 2
	}
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
	req.Header.Set("Content-Type", "application/json")

	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
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
