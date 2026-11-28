package web

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// sparkDotReach is the distance from the newest point's centre to the edge of
// its ring: radius 4 plus a 3 px ring (plan.md, Approach, and Challenge notes,
// minor 1). The dot must stay inside the canvas, so the plot is inset by at
// least this much.
const sparkDotReach = 4 + 3

// TestChartJS_Sparkline_AC7 guards the sparkline style of feature 217: a 2 px
// round-joined accent line, a fill fading to transparent built without string
// concatenation on the colour, a dot with a card-coloured ring at the newest
// point, nothing drawn below two points, and a plot inset that keeps the dot
// inside the canvas.
func TestChartJS_Sparkline_AC7(t *testing.T) {
	js := readAsset(t, "chart.js")
	body := jsFunctionBody(js, "function drawSparkline(")
	if strings.TrimSpace(body) == "" {
		t.Fatal("chart.js: expected a function drawSparkline")
	}

	t.Run("AC7 line, gradient fade and dot", func(t *testing.T) {
		checkAssetParts(t, []assetCheck{
			{"drawSparkline", body, []string{
				"ctx.lineWidth = 2;", "ctx.lineJoin = 'round'", "createLinearGradient(",
				"destination-out", "source-over", "--spark-fill-alpha", "--card-bg", "arc(",
			}},
		})
		checkAssetPartsAbsent(t, []assetCheck{
			{"drawSparkline (fill works for any CSS colour)", body, []string{"color + '", "lineWidth = 1.5"}},
		})
		if n := strings.Count(body, "arc("); n < 2 {
			t.Errorf("chart.js drawSparkline: %d arc( calls, want at least 2 (ring and dot)", n)
		}
	})

	t.Run("AC7 fewer than two points draw nothing", func(t *testing.T) {
		guard := strings.Index(body, "points.length < 2")
		if guard < 0 {
			t.Fatal("chart.js drawSparkline: expected the points.length < 2 guard")
		}
		if !strings.Contains(body[guard:guard+80], "return") {
			t.Errorf("chart.js drawSparkline: the points.length < 2 guard does not return")
		}
		for _, call := range []string{"beginPath(", "stroke(", "fill(", "arc(", "createLinearGradient("} {
			if i := strings.Index(body, call); i >= 0 && i < guard {
				t.Errorf("chart.js drawSparkline: %s comes before the points.length < 2 guard", call)
			}
		}
	})

	t.Run("AC7 dot stays inside the canvas", func(t *testing.T) {
		m := regexp.MustCompile(`const pad\s*=\s*(\d+(?:\.\d+)?)\s*;`).FindStringSubmatch(body)
		if m == nil {
			t.Fatal("chart.js drawSparkline: expected const pad = <number>;")
		}
		pad, err := strconv.ParseFloat(m[1], 64)
		if err != nil {
			t.Fatalf("parse pad %q: %v", m[1], err)
		}
		if pad < sparkDotReach {
			t.Errorf("chart.js drawSparkline: pad = %v, want at least %d (dot radius 4 plus ring 3), so the dot centre is never closer to an edge than its reach", pad, sparkDotReach)
		}
		checkAssetParts(t, []assetCheck{
			{"drawSparkline x range uses pad", body, []string{"cssWidth - pad * 2", "cssHeight - pad * 2", "cssHeight - pad -"}},
			{"drawSparkline flat-series padding kept", body, []string{"min -= 1;", "max += 1;"}},
		})
	})
}

// TestChartJS_TimeChart_AC7 guards drawTimeChart (feature 183): the openHAB
// memory chart places points by time, not by index like drawSparkline, breaks
// the line across a gap, marks restarts and scales y from 0.
func TestChartJS_TimeChart_AC7(t *testing.T) {
	js := readAsset(t, "chart.js")
	body := jsFunctionBody(js, "function drawTimeChart(")

	checkAssetParts(t, []assetCheck{
		{"drawTimeChart", body, []string{
			"opts.from", "opts.to", "opts.gapMs", "opts.markers", "opts.markerColor",
			"moveTo(", "lineTo(", "setLineDash(", "stroke()", "p.t", "p.v",
		}},
	})
	checkAssetPartsAbsent(t, []assetCheck{
		{"drawTimeChart (no index spacing, no fill)", body, []string{"stepX", "fill()", "innerHTML"}},
	})
	patterns := []struct{ name, re string }{
		{"a break where neighbors are more than opts.gapMs apart", `>\s*(opts\.)?gapMs`},
		{"a y scale starting at 0", `\b\w*[mM]in\w*\s*=\s*0\b`},
		{"a y scale ending at the largest value", `Math\.max\(`},
	}
	for _, p := range patterns {
		if !regexp.MustCompile(p.re).MatchString(body) {
			t.Errorf("chart.js drawTimeChart: expected %s (pattern %s)", p.name, p.re)
		}
	}
}
