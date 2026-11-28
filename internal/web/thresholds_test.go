package web

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/larslaskowski/pimonitor/internal/config"
)

// TestAppJS_ThresholdDefaultsMatchConfig guards against the silent-drift
// failure mode from issue #113: app.js hardcodes a fallback copy of the
// threshold keys (so the dashboard has something to render before
// GET /api/v1/config resolves), and nothing catches that copy falling out
// of sync with config.Thresholds — e.g. a new threshold added server-side
// but forgotten in the client default. This scans the client-side fallback
// object for its keys and asserts the set is exactly config.Thresholds's
// json tags, in either direction.
func TestAppJS_ThresholdDefaultsMatchConfig(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}

	start := regexp.MustCompile(`thresholds:\s*\{`).FindIndex(data)
	if start == nil {
		t.Fatal("app.js: could not find the client-side `thresholds: {` fallback default block")
	}
	end := regexp.MustCompile(`\}`).FindIndex(data[start[1]:])
	if end == nil {
		t.Fatal("app.js: unterminated `thresholds: {` block")
	}
	block := string(data[start[1] : start[1]+end[0]])

	keyPattern := regexp.MustCompile(`(\w+):\s*[\d.]+`)
	var jsKeys []string
	for _, m := range keyPattern.FindAllStringSubmatch(block, -1) {
		jsKeys = append(jsKeys, m[1])
	}
	sort.Strings(jsKeys)

	var goKeys []string
	rt := reflect.TypeOf(config.Thresholds{})
	for i := 0; i < rt.NumField(); i++ {
		tag := rt.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			t.Fatalf("config.Thresholds field %s has no json tag", rt.Field(i).Name)
		}
		goKeys = append(goKeys, tag)
	}
	sort.Strings(goKeys)

	if !reflect.DeepEqual(jsKeys, goKeys) {
		t.Fatalf("app.js threshold fallback keys != config.Thresholds json tags\napp.js:  %v\nconfig:  %v", jsKeys, goKeys)
	}
}

var (
	fallbackLiteral = regexp.MustCompile(`^-?\d+(\.\d+)?$`)
	fallbackEntry   = regexp.MustCompile(`(?m)^\s*(\w+):\s*([^,\n{]*?)\s*,?\s*$`)
)

// fallbackBlock returns the text of the `let config = {` fallback object of
// app.js up to its closing `\n  };`.
func fallbackBlock(t *testing.T, js string) string {
	t.Helper()
	start := strings.Index(js, "let config = {")
	if start == -1 {
		t.Fatal("app.js: could not find the `let config = {` fallback block")
	}
	end := strings.Index(js[start:], "\n  };")
	if end == -1 {
		t.Fatal("app.js: unterminated `let config = {` fallback block")
	}
	return js[start : start+end]
}

// fallbackValues maps every `key: literal` line of a fallback block to its
// literal text (the `thresholds: {` line has no literal and is skipped).
func fallbackValues(block string) map[string]string {
	out := map[string]string{}
	for _, m := range fallbackEntry.FindAllStringSubmatch(block, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// defaultFallbackValues returns the expected value of every compared
// fallback key from config.Default(), as float64 or bool. Thresholds are
// enumerated by reflection over their json tags, so a new threshold is
// compared without editing the test.
func defaultFallbackValues() map[string]any {
	d := config.Default()
	want := map[string]any{
		"poll_interval_seconds":  d.PollIntervalSeconds,
		"history_window_minutes": d.HistoryWindowMinutes,
		"network_enabled":        d.NetworkEnabled,
	}
	rv := reflect.ValueOf(d.Thresholds)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		f := rv.Field(i)
		if f.CanInt() {
			want[rt.Field(i).Tag.Get("json")] = float64(f.Int())
		} else {
			want[rt.Field(i).Tag.Get("json")] = f.Float()
		}
	}
	return want
}

// compareFallback returns one message per key whose fallback literal in got
// is missing, unreadable or different from its default in want.
func compareFallback(got map[string]string, want map[string]any) []string {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var msgs []string
	for _, k := range keys {
		lit, ok := got[k]
		if !ok {
			msgs = append(msgs, k+": missing from the app.js fallback")
			continue
		}
		msgs = append(msgs, compareFallbackKey(k, lit, want[k]))
	}
	out := msgs[:0]
	for _, m := range msgs {
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}

func compareFallbackKey(key, lit string, want any) string {
	if b, isBool := want.(bool); isBool {
		if lit != "true" && lit != "false" {
			return key + ": app.js has " + strconv.Quote(lit) + ", not a true/false literal"
		}
		if (lit == "true") != b {
			return key + ": app.js has " + lit + ", config.Default() has " + strconv.FormatBool(b)
		}
		return ""
	}
	w := want.(float64)
	if !fallbackLiteral.MatchString(lit) {
		return key + ": app.js has " + strconv.Quote(lit) + ", not a number literal"
	}
	v, err := strconv.ParseFloat(lit, 64)
	if err != nil || v != w {
		return key + ": app.js has " + lit + ", config.Default() has " + strconv.FormatFloat(w, 'g', -1, 64)
	}
	return ""
}

// TestAppJS_FallbackValuesMatchConfigDefaults guards issue #203: the
// client-side fallback used until GET /api/v1/config answers must hold the
// server defaults, not only the same threshold keys. Every threshold,
// poll_interval_seconds, history_window_minutes and network_enabled is
// compared with config.Default(); a value that is not a plain literal fails
// naming its key. version has no server default and is not compared.
func TestAppJS_FallbackValuesMatchConfigDefaults(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	got := fallbackValues(fallbackBlock(t, string(data)))
	want := defaultFallbackValues()

	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		t.Run("AC3 "+k, func(t *testing.T) {
			for _, m := range compareFallback(got, map[string]any{k: want[k]}) {
				t.Error(m)
			}
		})
	}

	const valid = `let config = {
    version: 'dev',
    poll_interval_seconds: 5,
    history_window_minutes: 60,
    network_enabled: true,
    thresholds: {
      cpu_warn_percent: 80,
    },
  `
	one := func(block string) []string {
		return compareFallback(fallbackValues(block), map[string]any{
			"poll_interval_seconds":  5.0,
			"history_window_minutes": 60.0,
			"network_enabled":        true,
			"cpu_warn_percent":       80.0,
		})
	}
	negatives := []struct{ name, block, wantKey, wantText string }{
		{"AC3 a changed number is reported with both values", strings.Replace(valid, "cpu_warn_percent: 80", "cpu_warn_percent: 81", 1), "cpu_warn_percent", "has 81, config.Default() has 80"},
		{"AC3 an expression is reported as unreadable", strings.Replace(valid, "history_window_minutes: 60", "history_window_minutes: 12 * 5", 1), "history_window_minutes", "not a number literal"},
		{"AC3 a missing key is reported", strings.Replace(valid, "    poll_interval_seconds: 5,\n", "", 1), "poll_interval_seconds", "missing"},
		{"AC3 network_enabled 1 is reported as unreadable", strings.Replace(valid, "network_enabled: true", "network_enabled: 1", 1), "network_enabled", "not a true/false literal"},
		{"AC3 a changed boolean is reported with both values", strings.Replace(valid, "network_enabled: true", "network_enabled: false", 1), "network_enabled", "has false, config.Default() has true"},
	}
	for _, c := range negatives {
		t.Run(c.name, func(t *testing.T) {
			msgs := one(c.block)
			if len(msgs) != 1 || !strings.HasPrefix(msgs[0], c.wantKey+":") || !strings.Contains(msgs[0], c.wantText) {
				t.Errorf("compare = %q, want one message for %s containing %q", msgs, c.wantKey, c.wantText)
			}
		})
	}
	t.Run("AC3 the valid block has no difference", func(t *testing.T) {
		if msgs := one(valid); len(msgs) != 0 {
			t.Errorf("compare = %q, want none", msgs)
		}
	})
}
