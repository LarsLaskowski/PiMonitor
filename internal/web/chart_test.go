package web

import (
	"regexp"
	"testing"
)

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
