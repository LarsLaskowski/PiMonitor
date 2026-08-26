package web

import (
	"reflect"
	"regexp"
	"sort"
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
