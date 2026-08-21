package web

import (
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
