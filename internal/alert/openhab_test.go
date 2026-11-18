package alert

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/larslaskowski/pimonitor/internal/config"
)

var ohStart = time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)

// ohThresholds are the openHAB cutoffs the tests use: RSS 50/75 %, Things
// 1 (warn) and 3 (crit).
func ohThresholds() config.Thresholds {
	th := testThresholds()
	th.OpenHABRSSWarnPercent = 50
	th.OpenHABRSSCritPercent = 75
	th.OpenHABThingsOfflineWarn = 1
	th.OpenHABThingsOfflineCrit = 3
	return th
}

func ohAt(offset time.Duration) time.Time { return ohStart.Add(offset) }

// ohDown is an installed-openHAB sample that only carries the down figure.
func ohDown(offset time.Duration, down bool) OpenHABSample {
	return OpenHABSample{Timestamp: ohAt(offset), Installed: true, Down: down}
}

// ohRSS is an installed sample with a valid RSS percentage.
func ohRSS(offset time.Duration, percent float64) OpenHABSample {
	return OpenHABSample{Timestamp: ohAt(offset), Installed: true, RSSPercent: percent, RSSValid: true}
}

// ohThings is an installed sample with a valid Things count and list.
func ohThings(offset time.Duration, count int, things []OpenHABThing) OpenHABSample {
	return OpenHABSample{Timestamp: ohAt(offset), Installed: true, ThingsOffline: count, ThingsValid: true, Things: things}
}

func ohThing(uid string) OpenHABThing {
	return OpenHABThing{UID: uid, Label: "Label " + uid, Status: "OFFLINE", StatusDetail: "COMMUNICATION_ERROR"}
}

func ohStates(r Report) map[string]State {
	out := map[string]State{}
	for _, st := range r.States {
		if strings.HasPrefix(st.Metric, "openhab_") {
			out[st.Metric] = st
		}
	}
	return out
}

func ohEvents(evs []Event) []Event {
	var out []Event
	for _, ev := range evs {
		if strings.HasPrefix(ev.Metric, "openhab_") {
			out = append(out, ev)
		}
	}
	return out
}

func eventMetrics(evs []Event) []string {
	out := make([]string, 0, len(evs))
	for _, ev := range evs {
		out = append(out, ev.Metric)
	}
	return out
}

// wantTransition describes one expected event.
type wantTransition struct {
	kind  string
	from  Level
	to    Level
	value float64
}

func checkOneEvent(t *testing.T, label string, got []Event, want *wantTransition) {
	t.Helper()
	if want == nil {
		if len(got) != 0 {
			t.Fatalf("%s: events = %+v, want none", label, got)
		}
		return
	}
	if len(got) != 1 {
		t.Fatalf("%s: got %d events (%+v), want 1", label, len(got), got)
	}
	ev := got[0]
	if ev.Kind != want.kind || ev.From != want.from || ev.To != want.to || ev.Value != want.value {
		t.Fatalf("%s: event = %s %s->%s (%v), want %s %s->%s (%v)", label,
			ev.Kind, ev.From, ev.To, ev.Value, want.kind, want.from, want.to, want.value)
	}
}

type ohStep struct {
	sample OpenHABSample
	want   *wantTransition
}

func runSteps(t *testing.T, e *Engine, steps []ohStep) {
	t.Helper()
	for i, s := range steps {
		got := e.EvaluateOpenHAB(s.sample)
		checkOneEvent(t, fmt.Sprintf("step %d", i), got, s.want)
		if s.want != nil && !got[0].At.Equal(s.sample.Timestamp) {
			t.Fatalf("step %d: event At = %v, want sample time %v", i, got[0].At, s.sample.Timestamp)
		}
	}
}

func TestEngine_EvaluateOpenHAB_Down_AC1(t *testing.T) {
	e := New(ohThresholds(), 0, 2*time.Minute)
	minute := time.Minute

	runSteps(t, e, []ohStep{
		{ohDown(0, false), nil},
		{ohDown(1*minute, true), nil},
		{ohDown(2*minute, true), nil},
		{ohDown(3*minute, true), &wantTransition{KindFired, LevelOK, LevelCrit, 1}},
		{ohDown(4*minute, false), nil},
		{ohDown(5*minute, false), nil},
		{ohDown(6*minute, false), &wantTransition{KindCleared, LevelCrit, LevelOK, 0}},
	})

	st, ok := ohStates(e.Report())[MetricOpenHABDown]
	if !ok || st.Level != LevelOK || st.Value != 0 || st.Resource != "" {
		t.Fatalf("openhab_down state = %+v (present=%v), want ok, value 0, no resource", st, ok)
	}
}

func TestEngine_EvaluateOpenHAB_DownHasNoWarnLevel_AC1(t *testing.T) {
	e := New(ohThresholds(), 0, 0)

	got := e.EvaluateOpenHAB(ohDown(0, true))

	checkOneEvent(t, "down", got, &wantTransition{KindFired, LevelOK, LevelCrit, 1})
}

func TestEngine_EvaluateOpenHAB_RSS_AC2(t *testing.T) {
	e := New(ohThresholds(), 0, 0)

	runSteps(t, e, []ohStep{
		{ohRSS(0, 49), nil},
		{ohRSS(1, 50), &wantTransition{KindFired, LevelOK, LevelWarn, 50}},
		{ohRSS(2, 80), &wantTransition{KindFired, LevelWarn, LevelCrit, 80}},
		{ohRSS(3, 60), &wantTransition{KindCleared, LevelCrit, LevelWarn, 60}},
		{ohRSS(4, 10), &wantTransition{KindCleared, LevelWarn, LevelOK, 10}},
	})
	invalid := OpenHABSample{Timestamp: ohAt(5), Installed: true, RSSPercent: 99, RSSValid: false}
	if got := e.EvaluateOpenHAB(invalid); len(got) != 0 {
		t.Fatalf("invalid RSS sample produced events %+v, want none", got)
	}
	st := ohStates(e.Report())[MetricOpenHABRSSPercent]
	if st.Level != LevelOK || st.Value != 10 {
		t.Fatalf("state after invalid sample = %+v, want level ok, value 10 unchanged", st)
	}
}

func TestEngine_EvaluateOpenHAB_ThingsOffline_AC3(t *testing.T) {
	e := New(ohThresholds(), 0, 0)
	two := []OpenHABThing{ohThing("a:1"), ohThing("b:2")}

	got := e.EvaluateOpenHAB(ohThings(0, 2, two))
	checkOneEvent(t, "warn", got, &wantTransition{KindFired, LevelOK, LevelWarn, 2})
	if !slices.Equal(got[0].Things, two) {
		t.Fatalf("warn event Things = %+v, want %+v", got[0].Things, two)
	}

	got = e.EvaluateOpenHAB(ohThings(1, 5, two))
	checkOneEvent(t, "crit", got, &wantTransition{KindFired, LevelWarn, LevelCrit, 5})

	failed := OpenHABSample{Timestamp: ohAt(2), Installed: true, ThingsOffline: 0, ThingsValid: false}
	if got := e.EvaluateOpenHAB(failed); len(got) != 0 {
		t.Fatalf("sample without valid Things produced %+v, want none", got)
	}
	if st := ohStates(e.Report())[MetricOpenHABThingsOffline]; st.Level != LevelCrit || st.Value != 5 {
		t.Fatalf("state after failed Things call = %+v, want crit with value 5", st)
	}

	got = e.EvaluateOpenHAB(ohThings(3, 0, nil))
	checkOneEvent(t, "cleared", got, &wantTransition{KindCleared, LevelCrit, LevelOK, 0})
	if got[0].Things == nil || len(got[0].Things) != 0 {
		t.Fatalf("cleared event Things = %#v, want a non-nil empty list", got[0].Things)
	}
}

func TestEngine_EvaluateOpenHAB_ThingsAreCopied_AC3(t *testing.T) {
	e := New(ohThresholds(), 0, 0)
	list := []OpenHABThing{ohThing("a:1")}

	got := e.EvaluateOpenHAB(ohThings(0, 1, list))
	if len(got) != 1 {
		t.Fatalf("got %d events, want 1", len(got))
	}
	list[0].UID = "mutated"
	got[0].Things[0].Label = "mutated too"

	evs := e.Report().Events
	if len(evs) != 1 || len(evs[0].Things) != 1 || evs[0].Things[0].UID != "a:1" || evs[0].Things[0].Label != "Label a:1" {
		t.Fatalf("stored event Things = %+v, want the original a:1 entry unaffected by caller changes", evs)
	}
}

func TestEngine_EvaluateOpenHAB_ZeroThresholdOff_AC4(t *testing.T) {
	t.Run("AC4 crit 0 reaches at most warn", func(t *testing.T) {
		th := ohThresholds()
		th.OpenHABThingsOfflineCrit = 0
		e := New(th, 0, 0)

		runSteps(t, e, []ohStep{
			{ohThings(0, 100, nil), &wantTransition{KindFired, LevelOK, LevelWarn, 100}},
			{ohThings(1, 200, nil), nil},
		})
	})
	t.Run("AC4 warn 0 with crit goes ok to crit and back", func(t *testing.T) {
		th := ohThresholds()
		th.OpenHABRSSWarnPercent = 0
		th.OpenHABRSSCritPercent = 40
		e := New(th, 0, time.Minute)
		minute := time.Minute

		runSteps(t, e, []ohStep{
			{ohRSS(0, 10), nil},
			{ohRSS(1*minute, 45), nil},
			{ohRSS(2*minute, 45), &wantTransition{KindFired, LevelOK, LevelCrit, 45}},
			{ohRSS(3*minute, 10), nil},
			{ohRSS(4*minute, 10), &wantTransition{KindCleared, LevelCrit, LevelOK, 10}},
		})
		if st := ohStates(e.Report())[MetricOpenHABRSSPercent]; st.Level != LevelOK {
			t.Fatalf("level after clearing = %s, want ok (not held by the off warn cutoff)", st.Level)
		}
	})
	t.Run("AC4 both 0 leaves no state and no event", func(t *testing.T) {
		th := ohThresholds()
		th.OpenHABRSSWarnPercent, th.OpenHABRSSCritPercent = 0, 0
		th.OpenHABThingsOfflineWarn, th.OpenHABThingsOfflineCrit = 0, 0
		e := New(th, 0, 0)

		for i := 0; i < 3; i++ {
			s := ohRSS(time.Duration(i), 100)
			s.ThingsOffline, s.ThingsValid = 50, true
			if got := e.EvaluateOpenHAB(s); len(got) != 0 {
				t.Fatalf("sample %d produced %+v, want none", i, got)
			}
		}
		if got := ohStates(e.Report()); len(got) != 1 {
			t.Fatalf("openhab states = %+v, want only openhab_down", got)
		}
	})
}

func TestEngine_EvaluateOpenHAB_WindowAndOrder_AC5(t *testing.T) {
	minute := time.Minute
	t.Run("AC5 window is openHABFor not forDur", func(t *testing.T) {
		e := New(ohThresholds(), time.Hour, 0)

		got := e.EvaluateOpenHAB(ohDown(0, true))

		checkOneEvent(t, "down", got, &wantTransition{KindFired, LevelOK, LevelCrit, 1})
	})
	t.Run("AC5 events of one poll come in fixed order", func(t *testing.T) {
		e := New(ohThresholds(), 0, 0)
		s := OpenHABSample{
			Timestamp: ohAt(0), Installed: true, Down: true,
			RSSPercent: 80, RSSValid: true,
			ThingsOffline: 2, ThingsValid: true, Things: []OpenHABThing{ohThing("a:1")},
		}

		got := e.EvaluateOpenHAB(s)

		want := []string{MetricOpenHABDown, MetricOpenHABRSSPercent, MetricOpenHABThingsOffline}
		if !slices.Equal(eventMetrics(got), want) {
			t.Fatalf("returned metrics = %v, want %v", eventMetrics(got), want)
		}
		if !slices.Equal(eventMetrics(e.Report().Events), want) {
			t.Fatalf("report event metrics = %v, want %v", eventMetrics(e.Report().Events), want)
		}
	})
	t.Run("AC5 fast Evaluate leaves openhab states unchanged", func(t *testing.T) {
		e := New(ohThresholds(), 0, minute)
		e.EvaluateOpenHAB(ohDown(0, true))

		for i := 1; i <= 3; i++ {
			got := e.Evaluate(Sample{Timestamp: ohAt(time.Duration(i) * 10 * minute), CPUPercent: 10, CPUValid: true})
			if len(ohEvents(got)) != 0 {
				t.Fatalf("fast tick %d produced openhab events %+v", i, got)
			}
		}
		if st := ohStates(e.Report())[MetricOpenHABDown]; st.Level != LevelOK {
			t.Fatalf("openhab_down after fast ticks = %s, want ok (no poll confirmed it)", st.Level)
		}
		got := e.EvaluateOpenHAB(ohDown(minute, true))
		checkOneEvent(t, "second poll", got, &wantTransition{KindFired, LevelOK, LevelCrit, 1})
	})
}

func TestEngine_EvaluateOpenHAB_NotInstalledPrunes_AC6(t *testing.T) {
	minute := time.Minute
	thingList := []OpenHABThing{ohThing("a:1"), ohThing("b:2"), ohThing("c:3")}
	allCrit := func(offset time.Duration) OpenHABSample {
		return OpenHABSample{
			Timestamp: ohAt(offset), Installed: true, Down: true,
			RSSPercent: 80, RSSValid: true,
			ThingsOffline: 3, ThingsValid: true, Things: thingList,
		}
	}

	t.Run("AC6 all three cleared in fixed order with last values", func(t *testing.T) {
		e := New(ohThresholds(), 0, minute)
		e.EvaluateOpenHAB(allCrit(0))
		if got := e.EvaluateOpenHAB(allCrit(minute)); len(got) != 3 {
			t.Fatalf("setup produced %d events, want 3", len(got))
		}

		got := e.EvaluateOpenHAB(OpenHABSample{Timestamp: ohAt(2 * minute)})

		want := []string{MetricOpenHABDown, MetricOpenHABRSSPercent, MetricOpenHABThingsOffline}
		if !slices.Equal(eventMetrics(got), want) {
			t.Fatalf("cleared metrics = %v, want %v", eventMetrics(got), want)
		}
		values := []float64{1, 80, 3}
		for i, ev := range got {
			if ev.Kind != KindCleared || ev.From != LevelCrit || ev.To != LevelOK || ev.Value != values[i] || !ev.At.Equal(ohAt(2*minute)) {
				t.Fatalf("event %d = %+v, want cleared crit->ok value %v at the sample time", i, ev, values[i])
			}
		}
		if !slices.Equal(got[2].Things, thingList) {
			t.Fatalf("things event list = %+v, want the last list %+v", got[2].Things, thingList)
		}
		if len(got[0].Things) != 0 || len(got[1].Things) != 0 {
			t.Fatalf("down/rss events carry things: %+v %+v", got[0].Things, got[1].Things)
		}
		if states := ohStates(e.Report()); len(states) != 0 {
			t.Fatalf("states after not_installed = %+v, want none", states)
		}
	})
	t.Run("AC6 only non-ok states produce an event", func(t *testing.T) {
		e := New(ohThresholds(), 0, 0)
		e.EvaluateOpenHAB(OpenHABSample{Timestamp: ohAt(0), Installed: true, RSSPercent: 60, RSSValid: true})

		got := e.EvaluateOpenHAB(OpenHABSample{Timestamp: ohAt(minute)})

		if len(got) != 1 || got[0].Metric != MetricOpenHABRSSPercent || got[0].From != LevelWarn || got[0].Value != 60 {
			t.Fatalf("events = %+v, want one cleared warn->ok openhab_rss_percent (60)", got)
		}
	})
	t.Run("AC6 reinstall starts again at ok with fresh timers", func(t *testing.T) {
		e := New(ohThresholds(), 0, minute)
		e.EvaluateOpenHAB(allCrit(0))
		e.EvaluateOpenHAB(allCrit(minute))
		e.EvaluateOpenHAB(OpenHABSample{Timestamp: ohAt(2 * minute)})

		if got := e.EvaluateOpenHAB(allCrit(3 * minute)); len(got) != 0 {
			t.Fatalf("first poll after reinstall produced %+v, want none", got)
		}
		if got := e.EvaluateOpenHAB(allCrit(4 * minute)); len(got) != 3 {
			t.Fatalf("second poll after reinstall produced %d events, want 3 fired", len(got))
		}
	})
}

func TestEngine_EvaluateOpenHAB_DefaultRSSSampleStaysOK_AC7(t *testing.T) {
	cfg := config.Default()
	e := New(cfg.Thresholds, cfg.AlertFor(), cfg.OpenHABAlertFor())
	percent := 100 * 1383952.0 / (4 * 1024 * 1024)
	if math.Abs(percent-33) > 0.1 {
		t.Fatalf("sample percent = %v, want about 33", percent)
	}

	for i := 0; i < 4; i++ {
		got := e.EvaluateOpenHAB(ohRSS(time.Duration(i)*10*time.Minute, percent))
		if len(got) != 0 {
			t.Fatalf("poll %d produced %+v with default thresholds, want none", i, got)
		}
	}
	st, ok := ohStates(e.Report())[MetricOpenHABRSSPercent]
	if !ok || st.Level != LevelOK || st.Value != percent {
		t.Fatalf("state = %+v (present=%v), want ok with value %v", st, ok, percent)
	}
}

func TestReport_JSONOpenHABThings_AC8(t *testing.T) {
	e := New(ohThresholds(), 0, 0)
	things := []OpenHABThing{ohThing("a:1")}
	e.EvaluateOpenHAB(OpenHABSample{
		Timestamp: ohAt(0), Installed: true, Down: true,
		RSSPercent: 80, RSSValid: true,
		ThingsOffline: 1, ThingsValid: true, Things: things,
	})
	e.EvaluateOpenHAB(ohThings(1, 0, nil))

	data, err := json.Marshal(e.Report())
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}

	var decoded struct {
		Events []map[string]json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal report: %v", err)
	}
	if len(decoded.Events) != 4 {
		t.Fatalf("report has %d events, want 4 (down, rss, things fired, things cleared): %s", len(decoded.Events), data)
	}
	for _, i := range []int{0, 1} {
		if _, has := decoded.Events[i]["things"]; has {
			t.Errorf("event %d (%s) has a things key, want none", i, decoded.Events[i]["metric"])
		}
	}
	var fired []map[string]string
	if err := json.Unmarshal(decoded.Events[2]["things"], &fired); err != nil || len(fired) != 1 {
		t.Fatalf("fired things = %s (err %v), want one entry", decoded.Events[2]["things"], err)
	}
	for _, key := range []string{"uid", "label", "status", "status_detail"} {
		if fired[0][key] == "" {
			t.Errorf("things entry lacks key %q: %v", key, fired[0])
		}
	}
	if got := string(decoded.Events[3]["things"]); got != "[]" {
		t.Errorf("cleared event things = %q, want []", got)
	}
}
