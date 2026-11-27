package web

import (
	"regexp"
	"strings"
	"testing"
)

// appFunction returns the body of the named function of app.js, failing the
// test when it is missing.
func appFunction(t *testing.T, name string) string {
	t.Helper()
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	body := jsFunctionBody(string(data), "function "+name+"(")
	if body == "" {
		t.Fatalf("app.js defines no function %s", name)
	}
	return body
}

// requireAll reports every want that body lacks.
func requireAll(t *testing.T, where, body string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(body, want) {
			t.Errorf("%s lacks %q", where, want)
		}
	}
}

// TestAppJS_UpdatesCardFollowsCheckStatus guards the dashboard's Updates card
// per updates.check_status. There is no JS runner, so it scans the source of
// renderUpdates and aptCheckedLine.
func TestAppJS_UpdatesCardFollowsCheckStatus(t *testing.T) {
	t.Run("AC4 count only when ok", func(t *testing.T) {
		body := appFunction(t, "renderUpdates")
		requireAll(t, "renderUpdates", body,
			"updates.check_status === 'ok'",
			"setText('updates-count', String(updates.count));")
	})

	t.Run("AC4 state texts", func(t *testing.T) {
		requireAll(t, "renderUpdates", appFunction(t, "renderUpdates"),
			"'Not checked yet'", "'Check failed'")
		requireAll(t, "aptCheckedLine", appFunction(t, "aptCheckedLine"),
			"'Last successful check '", "'Checked '")
	})

	t.Run("AC4 zero checked_at shows no line", func(t *testing.T) {
		body := appFunction(t, "renderUpdates")
		requireAll(t, "renderUpdates", body, "setText('updates-checked', aptCheckedLine(updates))")
		if regexp.MustCompile(`if\s*\(\s*(snap\.)?updates\.checked_at`).MatchString(body) {
			t.Errorf("renderUpdates still guards the Checked line with an if on checked_at; it must be set on every render")
		}
		requireAll(t, "aptCheckedLine", appFunction(t, "aptCheckedLine"), "Date.parse", "NaN", "<= 0")
	})

	t.Run("AC4 stale and button only when ok", func(t *testing.T) {
		body := appFunction(t, "renderUpdates")
		requireAll(t, "renderUpdates", body,
			"classList.toggle('hidden', !(ok && updates.stale))",
			"classList.toggle('hidden', !ok || latestPackages.length === 0)")
	})

	t.Run("AC4 open list closes outside ok", func(t *testing.T) {
		body := appFunction(t, "renderUpdates")
		requireAll(t, "renderUpdates", body,
			"latestPackages = ok ? (updates.packages || []) : [];",
			"dialog.open",
			"dialog.close()",
			"renderUpdatesTable()")
	})
}
