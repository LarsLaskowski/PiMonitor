package web

import (
	"strings"
	"testing"
)

// TestIndexHTML_HasAlertBanner guards issue #11: a header banner reflecting
// GET /api/v1/alerts must exist, hidden by default, and be announced to
// assistive tech since it can appear without any user interaction.
func TestIndexHTML_HasAlertBanner(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	if !strings.Contains(html, `id="alert-banner"`) {
		t.Error(`index.html: expected an element with id="alert-banner"`)
	}
	if !strings.Contains(html, `class="alert-banner hidden"`) {
		t.Error(`index.html: expected #alert-banner to start hidden (no active alert on first paint)`)
	}
	if !strings.Contains(html, `id="alert-banner-text"`) {
		t.Error(`index.html: expected an id="alert-banner-text" element inside the banner for its message`)
	}
	if !strings.Contains(html, `role="alert"`) || !strings.Contains(html, `aria-live="polite"`) {
		t.Error(`index.html: expected #alert-banner to be announced via role="alert" aria-live="polite"`)
	}
}

// TestIndexHTML_HasPerCardAlertBadges guards issue #11's per-card badge
// requirement: each card whose metric the alert engine evaluates (cpu,
// temperature, memory+swap, disk) needs a badge element, hidden by default.
func TestIndexHTML_HasPerCardAlertBadges(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/index.html")
	if err != nil {
		t.Fatalf("read index.html: %v", err)
	}
	html := string(data)

	for _, id := range []string{"badge-cpu", "badge-temperature", "badge-memory", "badge-disks"} {
		want := `class="alert-badge hidden" id="` + id + `"`
		if !strings.Contains(html, want) {
			t.Errorf("index.html: expected %q (hidden by default)", want)
		}
	}
}

// TestAppJS_PollsAlerts guards issue #11: the dashboard must poll
// GET /api/v1/alerts on the same cadence/lifecycle as the other pollers
// (in-flight guarded, started/stopped with the rest, refreshed immediately
// on visibility return, and fetched as part of the initial load).
func TestAppJS_PollsAlerts(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	if !strings.Contains(js, "fetchJSON('/api/v1/alerts')") {
		t.Error("app.js: expected pollAlerts to fetch /api/v1/alerts via fetchJSON")
	}

	start := strings.Index(js, "async function pollAlerts(")
	if start == -1 {
		t.Fatal("app.js: expected an async function pollAlerts")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function pollAlerts")
	}
	body := js[start : start+end]
	if !strings.Contains(body, "alertsInFlight") {
		t.Error("app.js: pollAlerts should check/set an in-flight guard flag before awaiting its fetch")
	}
	if !strings.Contains(body, "finally") {
		t.Error("app.js: pollAlerts should reset its in-flight guard flag in a finally block")
	}
	if !strings.Contains(body, "renderAlerts(") {
		t.Error("app.js: pollAlerts should call renderAlerts with the fetched report")
	}

	for _, wireSite := range []string{"startPolling", "stopPolling", "wireVisibilityPolling", "reloadAll"} {
		fnStart := strings.Index(js, "function "+wireSite+"(")
		if fnStart == -1 {
			t.Fatalf("app.js: expected a function %s", wireSite)
		}
		fnEnd := strings.Index(js[fnStart:], "\n  }\n")
		if fnEnd == -1 {
			t.Fatalf("app.js: could not find end of function %s", wireSite)
		}
		body := js[fnStart : fnStart+fnEnd]
		if !strings.Contains(body, "pollAlerts") && !strings.Contains(body, "alertsTimer") {
			t.Errorf("app.js: expected %s to reference alerts polling (pollAlerts/alertsTimer)", wireSite)
		}
	}
}

// TestAppJS_RenderAlertsSkipsOKStates guards the "clearing the condition
// removes it" acceptance criterion from issue #11: renderAlerts must skip
// "ok" states when computing badge/banner levels, so a metric that returns
// to ok simply has nothing recorded for it and its badge/banner disappears
// on the very next render rather than needing separate clear-out logic that
// could fall out of sync.
func TestAppJS_RenderAlertsSkipsOKStates(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	start := strings.Index(js, "function renderAlerts(")
	if start == -1 {
		t.Fatal("app.js: expected a function renderAlerts")
	}
	end := strings.Index(js[start:], "\n  }\n")
	if end == -1 {
		t.Fatal("app.js: could not find end of function renderAlerts")
	}
	body := js[start : start+end]

	if !strings.Contains(body, "st.level === 'ok'") {
		t.Error("app.js: expected renderAlerts to skip states at level 'ok' rather than badging them")
	}
	if !strings.Contains(body, "report?.enabled") && !strings.Contains(body, "report.enabled") {
		t.Error("app.js: expected renderAlerts to check report.enabled (alerting disabled server-side means no badges/banner)")
	}
	if !strings.Contains(js, "renderAlertBanner(") {
		t.Error("app.js: expected a renderAlertBanner helper called from renderAlerts")
	}
}

// TestStyleCSS_AlertStylesReuseWarnCritVariables guards the issue's stated
// reuse point: the alert banner and per-card badges must reuse the existing
// --warn/--crit custom properties rather than introducing new colors.
func TestStyleCSS_AlertStylesReuseWarnCritVariables(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/style.css")
	if err != nil {
		t.Fatalf("read style.css: %v", err)
	}
	css := string(data)

	for _, selector := range []string{".alert-banner.metric-warn", ".alert-banner.metric-crit", ".alert-badge.metric-warn", ".alert-badge.metric-crit"} {
		if !strings.Contains(css, selector) {
			t.Errorf("style.css: expected a %q rule", selector)
		}
	}
	if !strings.Contains(css, "var(--warn)") || !strings.Contains(css, "var(--crit)") {
		t.Error("style.css: expected the alert styles to reuse var(--warn)/var(--crit) rather than hardcoded colors")
	}
}
