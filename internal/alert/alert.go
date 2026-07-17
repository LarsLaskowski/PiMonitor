// Package alert evaluates collected metric snapshots against the configured
// warn/critical thresholds and turns sustained threshold crossings into
// per-metric alert states (ok/warn/crit) plus a rolling list of transition
// events (fired/cleared).
//
// A debounce ("for" duration) suppresses short-lived spikes: a raw level
// must persist for at least that long before it is promoted to the reported
// state, so a single-sample spike shorter than the window never fires an
// event. The debounce is applied symmetrically, so a momentary dip back to
// ok during a sustained alert does not clear it either.
//
// The engine keeps everything in memory and is safe for concurrent use: the
// collector calls Evaluate on each fast tick while the HTTP layer calls
// Report to serve GET /api/v1/alerts.
package alert

import (
	"sort"
	"sync"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// defaultMaxEvents bounds the rolling in-memory event history. Older events
// are dropped once this many have accumulated.
const defaultMaxEvents = 100

// Level is a metric's alert severity.
type Level string

const (
	LevelOK   Level = "ok"
	LevelWarn Level = "warn"
	LevelCrit Level = "crit"
)

// severity orders levels so transitions can be classified as escalations
// (fired) or de-escalations (cleared).
func severity(l Level) int {
	switch l {
	case LevelCrit:
		return 2
	case LevelWarn:
		return 1
	default:
		return 0
	}
}

// The value-to-level mapping uses the same >= cutoffs the dashboard uses to
// color-code cards (see levelClass in app.js) — a value is crit when
// value >= crit, warn when value >= warn — so the server-side alert state
// matches what the frontend renders. evalMetric applies these boundaries
// directly (aboveWarn/aboveCrit) so it can debounce each cutoff separately.

// Kinds of transition event.
const (
	KindFired   = "fired"
	KindCleared = "cleared"
)

// DiskSample is one filesystem's usage within a Sample.
type DiskSample struct {
	Mountpoint  string
	UsedPercent float64
}

// Sample is the subset of a collector snapshot the engine evaluates. The
// collector builds one per fast tick; keeping it independent of the
// collector's Snapshot type avoids an import cycle (collector imports this
// package).
//
// Each metric carries a *Valid flag: when false the metric's collection
// failed this tick and it is skipped entirely, so a transient sensor read
// error (feeding a bogus zero) can never spuriously clear a real alert or be
// reported as a healthy "0". A skipped metric keeps its previous state.
type Sample struct {
	Timestamp time.Time

	CPUPercent float64
	CPUValid   bool

	TemperatureC     float64
	TemperatureValid bool

	MemoryPercent float64
	MemoryValid   bool

	SwapPercent float64
	SwapValid   bool

	// Disks is the current filesystem list. When DisksValid is false (disk
	// collection failed), disk states are left untouched — neither evaluated
	// nor pruned — so a failed read doesn't wipe existing disk alerts.
	Disks      []DiskSample
	DisksValid bool
}

// State is the current debounced alert state of one metric.
type State struct {
	Metric   string    `json:"metric"`
	Resource string    `json:"resource,omitempty"`
	Level    Level     `json:"level"`
	Value    float64   `json:"value"`
	Since    time.Time `json:"since"`
}

// Event is a confirmed transition of one metric's alert state.
type Event struct {
	Metric   string    `json:"metric"`
	Resource string    `json:"resource,omitempty"`
	Kind     string    `json:"kind"`
	From     Level     `json:"from"`
	To       Level     `json:"to"`
	Value    float64   `json:"value"`
	At       time.Time `json:"at"`
}

// Report is the response body for GET /api/v1/alerts: the current per-metric
// states plus the recent event list.
type Report struct {
	Enabled bool    `json:"enabled"`
	States  []State `json:"states"`
	Events  []Event `json:"events"`
}

// metricState tracks the debounce bookkeeping for a single metric.
type metricState struct {
	metric   string
	resource string

	// active is the reported (debounced) level; activeSince is when it was
	// entered.
	active      Level
	activeSince time.Time
	lastValue   float64

	// Each threshold boundary is tracked independently: warnAbove/critAbove
	// is which side of the warn/crit cutoff the value is currently on, and
	// warnSince/critSince is when it last entered that side. Tracking the two
	// cutoffs separately (rather than a single raw ok/warn/crit band) means
	// "value has been >= warn continuously for the debounce window" holds
	// even while the value oscillates in and out of the crit band, so a
	// metric flapping around the crit cutoff still fires a warn alert instead
	// of resetting the window every tick.
	warnAbove bool
	warnSince time.Time
	critAbove bool
	critSince time.Time

	initialized bool
}

// Engine holds alert state across ticks. The zero value is not usable; call
// New.
type Engine struct {
	thresholds config.Thresholds
	forDur     time.Duration
	maxEvents  int

	mu     sync.Mutex
	states map[string]*metricState
	events []Event
}

// New returns an Engine that evaluates against thresholds and only promotes
// a threshold crossing to an alert after it has persisted for forDur. A
// zero forDur disables the debounce (crossings fire on the first sample).
func New(thresholds config.Thresholds, forDur time.Duration) *Engine {
	return &Engine{
		thresholds: thresholds,
		forDur:     forDur,
		maxEvents:  defaultMaxEvents,
		states:     make(map[string]*metricState),
	}
}

// Evaluate folds one sample into the engine's state, emitting transition
// events for any metric whose debounced level changed. Metrics flagged
// invalid (their collection failed this tick) are skipped and keep their
// previous state. It returns the events emitted during this call (in
// evaluation order) so the caller can forward them to notifiers; the returned
// slice is nil when nothing changed.
func (e *Engine) Evaluate(s Sample) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	var emitted []Event
	record := func(ev *Event) {
		if ev != nil {
			emitted = append(emitted, *ev)
		}
	}

	t := e.thresholds
	if s.CPUValid {
		record(e.evalMetric("cpu", "", s.CPUPercent, t.CPUWarnPercent, t.CPUCritPercent, s.Timestamp))
	}
	if s.TemperatureValid {
		record(e.evalMetric("temperature", "", s.TemperatureC, t.TemperatureWarnC, t.TemperatureCritC, s.Timestamp))
	}
	if s.MemoryValid {
		record(e.evalMetric("memory", "", s.MemoryPercent, t.MemoryWarnPercent, t.MemoryCritPercent, s.Timestamp))
	}
	if s.SwapValid {
		record(e.evalMetric("swap", "", s.SwapPercent, t.SwapWarnPercent, t.SwapCritPercent, s.Timestamp))
	}
	if s.DisksValid {
		present := make(map[string]struct{}, len(s.Disks))
		for _, d := range s.Disks {
			present[d.Mountpoint] = struct{}{}
			record(e.evalMetric("disk", d.Mountpoint, d.UsedPercent, t.DiskWarnPercent, t.DiskCritPercent, s.Timestamp))
		}
		emitted = append(emitted, e.pruneDisks(present, s.Timestamp)...)
	}
	return emitted
}

// evalMetric runs the debounce state machine for a single metric. Callers
// must hold e.mu.
//
// It confirms a level only once the value has held on the relevant side of a
// threshold for the debounce window: "at/above crit for forDur" escalates to
// crit, "at/above warn for forDur" escalates to warn, and the symmetric
// "below crit/warn for forDur" de-escalates. Because the warn and crit
// boundaries are timed independently, a value continuously >= warn fires a
// warn alert even while it dips in and out of crit.
//
// It returns the emitted transition event, or nil when the debounced level is
// unchanged.
func (e *Engine) evalMetric(metric, resource string, value, warn, crit float64, now time.Time) *Event {
	key := metric + "\x00" + resource
	st := e.states[key]
	if st == nil {
		st = &metricState{
			metric:      metric,
			resource:    resource,
			active:      LevelOK,
			activeSince: now,
		}
		e.states[key] = st
	}
	st.lastValue = value

	aboveWarn := value >= warn
	aboveCrit := value >= crit
	if !st.initialized || st.warnAbove != aboveWarn {
		st.warnAbove = aboveWarn
		st.warnSince = now
	}
	if !st.initialized || st.critAbove != aboveCrit {
		st.critAbove = aboveCrit
		st.critSince = now
	}
	st.initialized = true

	held := func(since time.Time) bool { return now.Sub(since) >= e.forDur }
	critConfirmed := st.critAbove && held(st.critSince)
	warnConfirmed := st.warnAbove && held(st.warnSince)
	belowCritConfirmed := !st.critAbove && held(st.critSince)
	belowWarnConfirmed := !st.warnAbove && held(st.warnSince)

	next := st.active
	switch st.active {
	case LevelOK:
		switch {
		case critConfirmed:
			next = LevelCrit
		case warnConfirmed:
			next = LevelWarn
		}
	case LevelWarn:
		switch {
		case critConfirmed:
			next = LevelCrit
		case belowWarnConfirmed:
			next = LevelOK
		}
	case LevelCrit:
		if belowCritConfirmed {
			if belowWarnConfirmed {
				next = LevelOK
			} else {
				next = LevelWarn
			}
		}
	}

	if next != st.active {
		prev := st.active
		st.active = next
		st.activeSince = now
		ev := e.recordEvent(st, prev, next, value, now)
		return &ev
	}
	return nil
}

// pruneDisks drops disk states whose mountpoint is absent from the latest
// sample (e.g. an unplugged USB drive or a transient mount), so a vanished
// filesystem doesn't linger forever in the report — worst case, a drive
// unmounted while at crit would otherwise report a frozen, never-cleared
// alert. A pruned state that was still alerting emits a final cleared event,
// which is returned so the caller can forward it to notifiers. Callers must
// hold e.mu, and must only call this when the disk list is authoritative
// (collection succeeded).
func (e *Engine) pruneDisks(present map[string]struct{}, now time.Time) []Event {
	var emitted []Event
	for key, st := range e.states {
		if st.metric != "disk" {
			continue
		}
		if _, ok := present[st.resource]; ok {
			continue
		}
		if st.active != LevelOK {
			emitted = append(emitted, e.recordEvent(st, st.active, LevelOK, st.lastValue, now))
		}
		delete(e.states, key)
	}
	return emitted
}

// recordEvent appends a transition event, trimming the history to maxEvents,
// and returns the event so callers can forward it to notifiers. Callers must
// hold e.mu.
func (e *Engine) recordEvent(st *metricState, from, to Level, value float64, now time.Time) Event {
	kind := KindFired
	if severity(to) < severity(from) {
		kind = KindCleared
	}
	ev := Event{
		Metric:   st.metric,
		Resource: st.resource,
		Kind:     kind,
		From:     from,
		To:       to,
		Value:    value,
		At:       now,
	}
	e.events = append(e.events, ev)
	if len(e.events) > e.maxEvents {
		e.events = e.events[len(e.events)-e.maxEvents:]
	}
	return ev
}

// Report returns a snapshot of the current per-metric states (sorted for a
// stable response) and a copy of the recent event list.
func (e *Engine) Report() Report {
	e.mu.Lock()
	defer e.mu.Unlock()

	states := make([]State, 0, len(e.states))
	for _, st := range e.states {
		states = append(states, State{
			Metric:   st.metric,
			Resource: st.resource,
			Level:    st.active,
			Value:    st.lastValue,
			Since:    st.activeSince,
		})
	}
	sort.Slice(states, func(i, j int) bool {
		if states[i].Metric != states[j].Metric {
			return states[i].Metric < states[j].Metric
		}
		return states[i].Resource < states[j].Resource
	})

	events := make([]Event, len(e.events))
	copy(events, e.events)

	return Report{Enabled: true, States: states, Events: events}
}
