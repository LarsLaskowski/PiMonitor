package alert

import "time"

// Metric names of the openHAB alerts.
const (
	// MetricOpenHABDown is the alert metric of an installed openHAB that is
	// not reachable.
	MetricOpenHABDown = "openhab_down"
	// MetricOpenHABRSSPercent is the alert metric of the openHAB process's
	// resident memory as a percentage of the host's memory.
	MetricOpenHABRSSPercent = "openhab_rss_percent"
	// MetricOpenHABThingsOffline is the alert metric of the number of
	// openHAB things that are not online.
	MetricOpenHABThingsOffline = "openhab_things_offline"
)

// OpenHABThing is one openHAB thing that is not online.
type OpenHABThing struct {
	UID          string `json:"uid"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	StatusDetail string `json:"status_detail"`
}

// OpenHABSample is the openHAB part of one snapshot, as the alert engine
// evaluates it.
type OpenHABSample struct {
	Timestamp     time.Time
	Installed     bool
	Down          bool
	RSSPercent    float64
	RSSValid      bool
	ThingsOffline int
	ThingsValid   bool
	Things        []OpenHABThing
}

// EvaluateOpenHAB folds one openHAB sample into the engine's state and returns
// the transition events it caused, in the fixed order openhab_down,
// openhab_rss_percent, openhab_things_offline. The openHAB metrics use their
// own debounce window and are driven only by this method, once per poll.
//
// A sample of an openHAB that is not installed clears and removes the
// states of all three metrics. A figure that is not valid keeps its state.
// A cutoff of 0 is off; both cutoffs of a metric at 0 leave it without a state.
func (e *Engine) EvaluateOpenHAB(s OpenHABSample) []Event {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !s.Installed {
		return e.pruneOpenHAB(s.Timestamp)
	}

	var emitted []Event
	t := e.thresholds
	down := 0.0
	if s.Down {
		down = 1
	}
	if ev := e.evalMetric(MetricOpenHABDown, "", down, 1, 1, s.Timestamp, e.openHABFor, true); ev != nil {
		emitted = append(emitted, *ev)
	}
	if s.RSSValid && (t.OpenHABRSSWarnPercent > 0 || t.OpenHABRSSCritPercent > 0) {
		if ev := e.evalMetric(MetricOpenHABRSSPercent, "", s.RSSPercent,
			t.OpenHABRSSWarnPercent, t.OpenHABRSSCritPercent, s.Timestamp, e.openHABFor, true); ev != nil {
			emitted = append(emitted, *ev)
		}
	}
	if s.ThingsValid && (t.OpenHABThingsOfflineWarn > 0 || t.OpenHABThingsOfflineCrit > 0) {
		things := copyThings(s.Things)
		ev := e.evalMetric(MetricOpenHABThingsOffline, "", float64(s.ThingsOffline),
			float64(t.OpenHABThingsOfflineWarn), float64(t.OpenHABThingsOfflineCrit), s.Timestamp, e.openHABFor, true)
		e.states[MetricOpenHABThingsOffline+"\x00"].lastThings = things
		if ev != nil {
			ev.Things = copyThings(things)
			e.events[len(e.events)-1].Things = copyThings(things)
			emitted = append(emitted, *ev)
		}
	}
	return emitted
}

// pruneOpenHAB removes the openHAB states, emitting a cleared event at now for
// each one that was still alerting. Callers must hold e.mu.
func (e *Engine) pruneOpenHAB(now time.Time) []Event {
	var emitted []Event
	for _, metric := range []string{MetricOpenHABDown, MetricOpenHABRSSPercent, MetricOpenHABThingsOffline} {
		key := metric + "\x00"
		st := e.states[key]
		if st == nil {
			continue
		}
		if st.active != LevelOK {
			ev := e.recordEvent(st, st.active, LevelOK, st.lastValue, now)
			if metric == MetricOpenHABThingsOffline {
				ev.Things = copyThings(st.lastThings)
				e.events[len(e.events)-1].Things = copyThings(st.lastThings)
			}
			emitted = append(emitted, ev)
		}
		delete(e.states, key)
	}
	return emitted
}

// copyThings returns a copy of things that is never nil, so an event's list
// encodes as [] rather than being omitted.
func copyThings(things []OpenHABThing) []OpenHABThing {
	return append([]OpenHABThing{}, things...)
}
