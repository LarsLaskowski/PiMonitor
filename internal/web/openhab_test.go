package web

import (
	"regexp"
	"strings"
	"testing"
)

// The openHAB card (issue #181) has no JS test runner (docs/UNIT_TESTS.md),
// so these guards scan the committed assets for the polling, visibility and
// warning wiring the plan fixes.

// enclosingFunctionBody returns the body of the innermost `function`
// declaration of src that contains needle, or "" when there is none.
func enclosingFunctionBody(src, needle string) string {
	idx := strings.Index(src, needle)
	if idx < 0 {
		return ""
	}
	for pos := strings.LastIndex(src[:idx], "function "); pos >= 0; pos = strings.LastIndex(src[:pos], "function ") {
		if body := jsFunctionBody(src[pos:], "function "); strings.Contains(body, needle) {
			return body
		}
	}
	return ""
}

// assetCheck is one part of an asset that must contain every string of want.
type assetCheck struct {
	name string
	part string
	want []string
}

func checkAssetParts(t *testing.T, checks []assetCheck) {
	t.Helper()
	for _, c := range checks {
		if c.part == "" {
			t.Errorf("%s: not found", c.name)
			continue
		}
		for _, w := range c.want {
			if !strings.Contains(c.part, w) {
				t.Errorf("%s: expected %q", c.name, w)
			}
		}
	}
}

// checkAssetPartsAbsent fails for every string of want that a part contains.
func checkAssetPartsAbsent(t *testing.T, checks []assetCheck) {
	t.Helper()
	for _, c := range checks {
		for _, w := range c.want {
			if strings.Contains(c.part, w) {
				t.Errorf("%s: expected no %q", c.name, w)
			}
		}
	}
}

func TestAppJS_OpenHABPolling_AC12(t *testing.T) {
	js := readAsset(t, "app.js")

	checkAssetParts(t, []assetCheck{
		{"pollOpenHAB", jsFunctionBody(js, "async function pollOpenHAB("), []string{
			"if (openhabInFlight) return;", "openhabInFlight = true;", "finally", "openhabInFlight = false;",
			"fetchJSON('/api/v1/openhab')", "e.status === 401", "openAPIKeyModal()", "404",
		}},
		{"stopPolling", jsFunctionBody(js, "function stopPolling("), []string{"openhabTimer", "openhabTimer = null"}},
		{"wireVisibilityPolling", jsFunctionBody(js, "function wireVisibilityPolling("), []string{"void pollOpenHAB();"}},
		{"reloadAll", jsFunctionBody(js, "async function reloadAll("), []string{"pollOpenHAB()"}},
	})
	if !regexp.MustCompile(`openhabTimer = set(Timeout|Interval)\(`).MatchString(js) {
		t.Error("app.js: expected openhabTimer to be set with setTimeout or setInterval")
	}
}

func TestAppJS_OpenHABPendingRepoll_AC12(t *testing.T) {
	js := readAsset(t, "app.js")

	body := enclosingFunctionBody(js, "Math.max(60,")

	checkAssetParts(t, []assetCheck{
		{"openHAB poll interval (the function using Math.max(60, …))", body, []string{
			"'pending'", "config.poll_interval_seconds",
		}},
	})
}

func TestAppJS_OpenHABCardVisibility_AC12(t *testing.T) {
	js := readAsset(t, "app.js")
	html := readAsset(t, "index.html")
	cardDefs := ""
	if start := strings.Index(js, "const CARD_DEFS = ["); start >= 0 {
		cardDefs = js[start : start+strings.Index(js[start:], "];")]
	}

	checkAssetParts(t, []assetCheck{
		{"CARD_DEFS", cardDefs, []string{"{ id: 'openhab', domId: 'card-openhab', label: 'openHAB' }"}},
		{"applyLayout", jsFunctionBody(js, "function applyLayout("), []string{"'openhab'"}},
		{"applyOpenHABVisibility", jsFunctionBody(js, "function applyOpenHABVisibility("), []string{
			"e.id === 'openhab'", "'not_installed'", "'pending'", "'hidden'",
		}},
		{"setCardVisible", jsFunctionBody(js, "function setCardVisible("), []string{"applyOpenHABVisibility("}},
		{"resetLayout", jsFunctionBody(js, "function resetLayout("), []string{"applyOpenHABVisibility("}},
	})
	if last := strings.LastIndex(cardDefs, "id: '"); last < 0 || !strings.HasPrefix(cardDefs[last:], "id: 'openhab'") {
		t.Error("app.js: expected openhab to be the last entry of CARD_DEFS (last in the default order)")
	}
	if !regexp.MustCompile(`<section class="card[^"]*\bhidden\b[^"]*" id="card-openhab">`).MatchString(html) {
		t.Error(`index.html: expected a <section class="card … hidden" id="card-openhab">, hidden until a qualifying answer`)
	}
	if strings.Index(html, `id="card-openhab"`) < strings.Index(html, `id="card-network"`) {
		t.Error("index.html: expected the openHAB card after the network card")
	}
}

func TestAppJS_OpenHABWarnClassDefined_AC13(t *testing.T) {
	js := readAsset(t, "app.js")
	css := readAsset(t, "style.css")

	checkAssetParts(t, []assetCheck{
		{"openHAB renderer (the function using 'card-warn')", enclosingFunctionBody(js, "'card-warn'"), []string{
			"'installed_not_running'", "'running_unreachable'", "not_online", "metric-warn", "textContent",
		}},
	})
	if !regexp.MustCompile(`\.card-warn\s*\{[^}]*var\(--warn\)`).MatchString(css) {
		t.Error("style.css: expected a .card-warn rule using var(--warn)")
	}
}

func TestAppJS_OpenHABTexts_AC13(t *testing.T) {
	js := readAsset(t, "app.js")

	checkAssetParts(t, []assetCheck{
		{"app.js", js, []string{
			"needs api_token", "api_token refused", "n/a", "Not running", "Not reachable", "Starting (start level ",
			"Process memory", "JVM heap", "% of RAM", "'Things offline: '",
		}},
	})
	checkAssetPartsAbsent(t, []assetCheck{
		{"app.js", js, []string{"Not-online Things"}},
	})
}

func TestAppJS_OpenHABRootRefusedStateText_AC13(t *testing.T) {
	js := readAsset(t, "app.js")

	checkAssetParts(t, []assetCheck{
		{"openhabStateText", jsFunctionBody(js, "function openhabStateText("), []string{
			"includes('root')", "openhabRefusedHint(",
		}},
		{"openhabRefusedHint", jsFunctionBody(js, "function openhabRefusedHint("), []string{
			"'api_token refused'", "'needs api_token'", "api_token_configured",
		}},
	})
}

func TestAppJS_OpenHABThingsToggle_AC13(t *testing.T) {
	js := readAsset(t, "app.js")
	toggle := jsFunctionBody(js, "function openhabThingsToggle(")
	render := jsFunctionBody(js, "function renderOpenHAB(")

	if !strings.Contains(js, "const OPENHAB_THINGS_KEY = 'pimonitor-openhab-things';") {
		t.Error("app.js: expected const OPENHAB_THINGS_KEY = 'pimonitor-openhab-things';")
	}
	checkAssetParts(t, []assetCheck{
		{"openhabThingsToggle", toggle, []string{
			"createElement('details')", "'Things offline: '", "'toggle'", "t.label || t.uid",
		}},
		{"renderOpenHAB", render, []string{"openhabThingsToggle("}},
	})
	checkAssetPartsAbsent(t, []assetCheck{
		{"openhabThingsToggle", toggle, []string{".status", ".description", "innerHTML"}},
		{"renderOpenHAB", render, []string{"t.uid"}},
	})
}

func TestAppJS_OpenHABChart_AC7(t *testing.T) {
	js := readAsset(t, "app.js")
	render := jsFunctionBody(js, "function renderOpenHAB(")
	history := jsFunctionBody(js, "function renderHistory(")
	redraw := jsFunctionBody(js, "function redrawSparklines(")
	drawer := enclosingFunctionBody(js, "drawTimeChart(")

	checkAssetParts(t, []assetCheck{
		{"renderOpenHAB", render, []string{
			"openhab-history", "openhab-chart", "openhab-legend", "legend-swatch", "legend-rss", "legend-heap",
			"'RSS'", "'Heap used'", "'Memory history'", "fmtBytes(", "textContent", "openhabThingsToggle(",
		}},
		{"renderHistory", history, []string{"renderOpenHAB(latestOpenHAB)", "'pending'", "'not_installed'"}},
		{"drawTimeChart caller", drawer, []string{
			"openhab_rss_bytes", "openhab_heap_used_bytes", "openhab_restarts", "poll_interval_seconds", "1.5",
			"getComputedStyle", "--accent", "--muted", "--warn",
		}},
	})
	checkAssetPartsAbsent(t, []assetCheck{
		{"renderOpenHAB", render, []string{"innerHTML"}},
		{"drawTimeChart caller", drawer, []string{"innerHTML"}},
	})
	if !strings.Contains(strings.ToLower(redraw), "openhab") {
		t.Error("redrawSparklines: expected the openHAB chart to be redrawn there (history poll, theme, layout reveal)")
	}
	if !regexp.MustCompile(`length >= 2`).MatchString(js[strings.Index(js, "---- openHAB card"):]) {
		t.Error("app.js: expected the block to exist only with at least two points (length >= 2) in the openHAB section")
	}
	if !regexp.MustCompile(`legend-rss[\s\S]*legend-heap`).MatchString(render + drawer) {
		t.Error("app.js: expected the RSS legend entry before the Heap used entry")
	}
}
