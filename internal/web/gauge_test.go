package web

import (
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// TestAppJS_LoadGaugeUsesCPUThresholds guards against regressing issue #67:
// the load-average gauge color scale must derive from the configured
// cpu_warn_percent/cpu_crit_percent thresholds (relative to core count), as
// documented in packaging/pimonitor.example.yaml, rather than a hardcoded
// 70 %/100 % of core count.
func TestAppJS_LoadGaugeUsesCPUThresholds(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	js := string(data)

	if strings.Contains(js, "lastCPUCount * 0.7") || strings.Contains(js, "lastCPUCount * 1.0") {
		t.Error("app.js: found hardcoded 0.7/1.0 core-count gauge thresholds; the load gauges must use the configured cpu_warn_percent/cpu_crit_percent instead")
	}
	if !strings.Contains(js, "lastCPUCount * warnPercent / 100") || !strings.Contains(js, "lastCPUCount * critPercent / 100") {
		t.Error("app.js: expected renderGauge to scale cpu_warn_percent/cpu_crit_percent by lastCPUCount")
	}
	for _, call := range []string{
		"renderGauge('gauge-load1', 'load1-value', snap.load_average.load1, t.cpu_warn_percent, t.cpu_crit_percent);",
		"renderGauge('gauge-load5', 'load5-value', snap.load_average.load5, t.cpu_warn_percent, t.cpu_crit_percent);",
		"renderGauge('gauge-load15', 'load15-value', snap.load_average.load15, t.cpu_warn_percent, t.cpu_crit_percent);",
	} {
		if !strings.Contains(js, call) {
			t.Errorf("app.js: expected call %q", call)
		}
	}
}

// gaugeCanvasHeight is the canvas height of .gauge canvas in style.css.
const gaugeCanvasHeight = 70.0

// gaugeNumber returns the first capture group of re in src as a number.
func gaugeNumber(t *testing.T, where, src, re string) float64 {
	t.Helper()
	m := regexp.MustCompile(re).FindStringSubmatch(src)
	if m == nil {
		t.Fatalf("%s: expected a match for %s", where, re)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		t.Fatalf("%s: parse %q: %v", where, m[1], err)
	}
	return v
}

// gaugeValueDecls returns the declarations of the .gauge-value rule, whether
// it is written as `.gauge .gauge-value` or `.gauge-value`.
func gaugeValueDecls(css string) map[string]string {
	decls := cssDeclarations(css, ".gauge .gauge-value")
	for k, v := range cssDeclarations(css, ".gauge-value") {
		decls[k] = v
	}
	return decls
}

// gaugeArc mirrors the radius bound of gauge.js for a canvas of width w:
// the arc centre cy and the radius.
func gaugeArc(w, lineWidth, margin float64) (cy, radius float64) {
	cy = gaugeCanvasHeight - margin - lineWidth/2
	radius = math.Max(4, math.Min(w/2-lineWidth/2-margin, cy-lineWidth/2-margin))
	return cy, radius
}

// TestGaugeJS_ArcNeverClipped_AC8 guards the load gauge of feature 217: a 9 px
// arc on the --track token whose radius bound is kept, so the arc (stroke and
// round caps) lies inside the canvas at every gauge width from 72 px, and the
// value NN.NN, positioned by CSS inside the arc, fits the arc's inner chord at
// the top edge of the value box (the tightest point, not the baseline).
func TestGaugeJS_ArcNeverClipped_AC8(t *testing.T) {
	js := readAsset(t, "gauge.js")
	css := readAsset(t, "style.css")
	body := jsFunctionBody(js, "function drawGauge(")
	if strings.TrimSpace(body) == "" {
		t.Fatal("gauge.js: expected a function drawGauge")
	}

	t.Run("AC8 stroke, track token and radius bound", func(t *testing.T) {
		checkAssetParts(t, []assetCheck{
			{"drawGauge", body, []string{
				"const lineWidth = 9;", "'--track'",
				"Math.min(cssWidth / 2 - lineWidth / 2 - margin, cy - lineWidth / 2 - margin)",
			}},
		})
		checkAssetPartsAbsent(t, []assetCheck{{"drawGauge", body, []string{"--gauge-track"}}})
	})

	t.Run("AC8 CSS positions the value inside the arc", func(t *testing.T) {
		requireCSS(t, css, ".gauge", map[string]string{"container-type": "inline-size"})
		requireCSS(t, css, ".gauge-dial", map[string]string{"position": "relative"})
		requireCSS(t, css, ".gauge canvas", map[string]string{"height": "70px"})
		decls := gaugeValueDecls(css)
		for prop, want := range map[string]string{"position": "absolute", "text-align": "center", "line-height": "1"} {
			if decls[prop] != want {
				t.Errorf("style.css: .gauge-value { %s } = %q, want %q", prop, decls[prop], want)
			}
		}
		if _, ok := decls["bottom"]; !ok {
			t.Error("style.css: .gauge-value sets no bottom offset")
		}
	})

	lineWidth := gaugeNumber(t, "gauge.js", body, `const lineWidth\s*=\s*(\d+(?:\.\d+)?)\s*;`)
	margin := gaugeNumber(t, "gauge.js", body, `const margin\s*=\s*(\d+(?:\.\d+)?)\s*;`)

	for _, w := range []float64{72, 100, 150, 250, 400} {
		t.Run("AC8 arc inside canvas at "+strconv.FormatFloat(w, 'f', -1, 64)+" px", func(t *testing.T) {
			cy, r := gaugeArc(w, lineWidth, margin)
			half := lineWidth / 2
			if left, right := w/2-r-half, w/2+r+half; left < 0 || right > w {
				t.Errorf("arc spans x %.2f..%.2f, want inside 0..%.0f", left, right, w)
			}
			if top := cy - r - half; top < 0 {
				t.Errorf("arc top at y %.2f, want at least 0", top)
			}
			if bottom := cy + half; bottom > gaugeCanvasHeight {
				t.Errorf("arc end caps reach y %.2f, want at most %.0f", bottom, gaugeCanvasHeight)
			}
		})
	}

	t.Run("AC8 value NN.NN fits the inner chord at 72 px", func(t *testing.T) {
		const w = 72.0
		decls := gaugeValueDecls(css)
		m := regexp.MustCompile(`^clamp\((\d+(?:\.\d+)?)px,\s*(\d+(?:\.\d+)?)cqi,\s*(\d+(?:\.\d+)?)px\)$`).FindStringSubmatch(decls["font-size"])
		if m == nil {
			t.Fatalf("style.css: .gauge-value font-size = %q, want clamp(<min>px, <n>cqi, <max>px)", decls["font-size"])
		}
		lo, _ := strconv.ParseFloat(m[1], 64)
		cqi, _ := strconv.ParseFloat(m[2], 64)
		hi, _ := strconv.ParseFloat(m[3], 64)
		font := math.Min(hi, math.Max(lo, cqi*w/100)) // 1cqi is 1 % of the .gauge width
		bottom, err := strconv.ParseFloat(strings.TrimSuffix(decls["bottom"], "px"), 64)
		if err != nil {
			t.Fatalf("style.css: .gauge-value bottom = %q, want a px value", decls["bottom"])
		}
		lineHeight, err := strconv.ParseFloat(decls["line-height"], 64)
		if err != nil {
			t.Fatalf("style.css: .gauge-value line-height = %q, want a number", decls["line-height"])
		}

		cy, r := gaugeArc(w, lineWidth, margin)
		inner := r - lineWidth/2
		top := gaugeCanvasHeight - bottom - font*lineHeight // top edge of the value box
		dy := cy - top
		if dy >= inner {
			t.Fatalf("value box top at y %.2f is outside the inner circle (cy %.2f, inner radius %.2f)", top, cy, inner)
		}
		chord := 2 * math.Sqrt(inner*inner-dy*dy)
		// 88.88 in bold system-ui measures 2.89 em (plan.md, fact 7).
		if width := 2.89 * font; width > chord {
			t.Errorf("value width %.2f px (font %.2f px) exceeds the inner chord %.2f px at the top of the value box", width, font, chord)
		}
	})
}
