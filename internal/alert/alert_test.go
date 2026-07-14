package alert

import (
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// testThresholds mirrors the built-in defaults closely enough for the state
// machine tests: temperature warns at 60/crits at 75, cpu at 80/95, etc.
func testThresholds() config.Thresholds {
	return config.Thresholds{
		TemperatureWarnC: 60,
		TemperatureCritC: 75,
		CPUWarnPercent:   80,
		CPUCritPercent:   95,
		DiskWarnPercent:  80,
		DiskCritPercent:  95,
		SwapWarnPercent:  50,
		SwapCritPercent:  90,
	}
}

// feed evaluates a series of CPU values, one per second starting at a fixed
// epoch, so the debounce window can be reasoned about in whole samples.
func feed(e *Engine, start time.Time, cpuValues ...float64) {
	for i, v := range cpuValues {
		e.Evaluate(Sample{
			Timestamp:  start.Add(time.Duration(i) * time.Second),
			CPUPercent: v,
		})
	}
}

func cpuEvents(r Report) []Event {
	var out []Event
	for _, ev := range r.Events {
		if ev.Metric == "cpu" {
			out = append(out, ev)
		}
	}
	return out
}

// A sustained crossing into crit and back to ok must emit exactly one fired
// event on entry and one cleared event on exit.
func TestEvaluate_FiredOnEntryClearedOnExit(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 2*time.Second)

	// ok, ok, crit (t=2 start), crit, crit (promoted at t=4), then back to
	// ok (t=5 start), ok, ok (cleared at t=7).
	feed(e, start, 10, 10, 99, 99, 99, 10, 10, 10)

	evs := cpuEvents(e.Report())
	if len(evs) != 2 {
		t.Fatalf("expected 2 cpu events, got %d: %+v", len(evs), evs)
	}
	if evs[0].Kind != KindFired || evs[0].To != LevelCrit {
		t.Fatalf("first event = %+v, want fired->crit", evs[0])
	}
	if evs[1].Kind != KindCleared || evs[1].To != LevelOK {
		t.Fatalf("second event = %+v, want cleared->ok", evs[1])
	}

	// The reported cpu state settles back to ok.
	if st := cpuState(t, e.Report()); st.Level != LevelOK {
		t.Fatalf("expected cpu state to settle back to ok, got %s", st.Level)
	}
}

// cpuState returns the cpu entry from a report, failing if absent.
func cpuState(t *testing.T, r Report) State {
	t.Helper()
	for _, st := range r.States {
		if st.Metric == "cpu" {
			return st
		}
	}
	t.Fatalf("no cpu state in report: %+v", r.States)
	return State{}
}

// A spike shorter than the "for" window must not fire any event.
func TestEvaluate_DebounceSuppressesShortSpike(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 2*time.Second)

	// Single-sample spike to crit at t=2, back to ok at t=3 — never held
	// for the 2s window.
	feed(e, start, 10, 10, 99, 10, 10)

	if evs := cpuEvents(e.Report()); len(evs) != 0 {
		t.Fatalf("expected no events for a single-sample spike, got %+v", evs)
	}
	if st := cpuState(t, e.Report()); st.Level != LevelOK {
		t.Fatalf("expected cpu state to stay ok, got %s", st.Level)
	}
}

// A zero "for" window fires on the first crossing (no debounce).
func TestEvaluate_ZeroForFiresImmediately(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 0)

	feed(e, start, 10, 99)

	evs := cpuEvents(e.Report())
	if len(evs) != 1 || evs[0].Kind != KindFired || evs[0].To != LevelCrit {
		t.Fatalf("expected one immediate fired->crit event, got %+v", evs)
	}
}

// Escalating ok->warn->crit emits a fired event per step; the state tracks
// the most recent level.
func TestEvaluate_Escalation(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 0)

	feed(e, start, 10, 85, 99)

	evs := cpuEvents(e.Report())
	if len(evs) != 2 {
		t.Fatalf("expected 2 events, got %+v", evs)
	}
	if evs[0].From != LevelOK || evs[0].To != LevelWarn || evs[0].Kind != KindFired {
		t.Fatalf("event[0] = %+v, want ok->warn fired", evs[0])
	}
	if evs[1].From != LevelWarn || evs[1].To != LevelCrit || evs[1].Kind != KindFired {
		t.Fatalf("event[1] = %+v, want warn->crit fired", evs[1])
	}
}

// A momentary dip to ok during a sustained crit must not clear the alert
// (the debounce is symmetric).
func TestEvaluate_SymmetricDebounceKeepsAlert(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 2*time.Second)

	// Establish crit, then a one-sample dip to ok, then back to crit.
	feed(e, start, 99, 99, 99, 10, 99, 99, 99)

	evs := cpuEvents(e.Report())
	if len(evs) != 1 || evs[0].Kind != KindFired {
		t.Fatalf("expected only the initial fired event, got %+v", evs)
	}
	if st := cpuState(t, e.Report()); st.Level != LevelCrit {
		t.Fatalf("expected cpu to remain crit, got %s", st.Level)
	}
}

// Disk alerts are tracked per mountpoint, independently.
func TestEvaluate_PerDiskState(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 0)

	e.Evaluate(Sample{
		Timestamp: start,
		Disks: []DiskSample{
			{Mountpoint: "/", UsedPercent: 10},
			{Mountpoint: "/data", UsedPercent: 99},
		},
	})

	r := e.Report()
	var root, data *State
	for i := range r.States {
		switch r.States[i].Resource {
		case "/":
			root = &r.States[i]
		case "/data":
			data = &r.States[i]
		}
	}
	if root == nil || root.Level != LevelOK {
		t.Fatalf("expected / to be ok, got %+v", root)
	}
	if data == nil || data.Level != LevelCrit {
		t.Fatalf("expected /data to be crit, got %+v", data)
	}
	if len(r.Events) != 1 || r.Events[0].Resource != "/data" {
		t.Fatalf("expected a single /data event, got %+v", r.Events)
	}
}

// The rolling event history is bounded to maxEvents, dropping the oldest.
func TestEvaluate_EventHistoryBounded(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	e := New(testThresholds(), 0)
	e.maxEvents = 3

	// Alternate ok/crit each second; every step is a transition, producing
	// far more than maxEvents events.
	for i := 0; i < 20; i++ {
		v := 10.0
		if i%2 == 1 {
			v = 99.0
		}
		e.Evaluate(Sample{Timestamp: start.Add(time.Duration(i) * time.Second), CPUPercent: v})
	}

	if got := len(e.Report().Events); got != 3 {
		t.Fatalf("expected event history bounded to 3, got %d", got)
	}
}

// A disabled/never-evaluated engine still reports a well-formed empty report
// once evaluated; Report on a fresh engine has no states or events.
func TestReport_FreshEngineEmpty(t *testing.T) {
	e := New(testThresholds(), time.Second)
	r := e.Report()
	if !r.Enabled {
		t.Fatal("Report().Enabled should be true for a constructed engine")
	}
	if len(r.States) != 0 || len(r.Events) != 0 {
		t.Fatalf("expected empty report, got %+v", r)
	}
}
