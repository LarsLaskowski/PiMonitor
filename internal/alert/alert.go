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

// classify maps a value to a level using the same >= cutoffs the dashboard
// uses to color-code cards (see levelClass in app.js), so the server-side
// alert state matches what the frontend renders.
func classify(value, warn, crit float64) Level {
	switch {
	case value >= crit:
		return LevelCrit
	case value >= warn:
		return LevelWarn
	default:
		return LevelOK
	}
}

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
type Sample struct {
	Timestamp    time.Time
	CPUPercent   float64
	TemperatureC float64
	SwapPercent  float64
	Disks        []DiskSample
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

	// active is the reported (debounced) level; candidate is the raw level
	// currently being observed, promoted to active once it has held for the
	// configured "for" duration.
	active         Level
	candidate      Level
	candidateSince time.Time
	activeSince    time.Time
	lastValue      float64
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
// events for any metric whose debounced level changed.
func (e *Engine) Evaluate(s Sample) {
	e.mu.Lock()
	defer e.mu.Unlock()

	t := e.thresholds
	e.evalMetric("cpu", "", s.CPUPercent, t.CPUWarnPercent, t.CPUCritPercent, s.Timestamp)
	e.evalMetric("temperature", "", s.TemperatureC, t.TemperatureWarnC, t.TemperatureCritC, s.Timestamp)
	e.evalMetric("swap", "", s.SwapPercent, t.SwapWarnPercent, t.SwapCritPercent, s.Timestamp)
	for _, d := range s.Disks {
		e.evalMetric("disk", d.Mountpoint, d.UsedPercent, t.DiskWarnPercent, t.DiskCritPercent, s.Timestamp)
	}
}

// evalMetric runs the debounce state machine for a single metric. Callers
// must hold e.mu.
func (e *Engine) evalMetric(metric, resource string, value, warn, crit float64, now time.Time) {
	key := metric + "\x00" + resource
	st := e.states[key]
	if st == nil {
		st = &metricState{
			metric:         metric,
			resource:       resource,
			active:         LevelOK,
			candidate:      LevelOK,
			candidateSince: now,
			activeSince:    now,
		}
		e.states[key] = st
	}
	st.lastValue = value

	raw := classify(value, warn, crit)
	if raw == st.active {
		// Back in agreement with the reported state; drop any pending
		// candidate so a later crossing starts its "for" window fresh.
		st.candidate = raw
		st.candidateSince = now
		return
	}
	if raw != st.candidate {
		// A newly observed level: (re)start the debounce window.
		st.candidate = raw
		st.candidateSince = now
	}
	if now.Sub(st.candidateSince) >= e.forDur {
		prev := st.active
		st.active = raw
		st.activeSince = now
		e.recordEvent(st, prev, raw, value, now)
	}
}

// recordEvent appends a transition event, trimming the history to maxEvents.
// Callers must hold e.mu.
func (e *Engine) recordEvent(st *metricState, from, to Level, value float64, now time.Time) {
	kind := KindFired
	if severity(to) < severity(from) {
		kind = KindCleared
	}
	e.events = append(e.events, Event{
		Metric:   st.metric,
		Resource: st.resource,
		Kind:     kind,
		From:     from,
		To:       to,
		Value:    value,
		At:       now,
	})
	if len(e.events) > e.maxEvents {
		e.events = e.events[len(e.events)-e.maxEvents:]
	}
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
