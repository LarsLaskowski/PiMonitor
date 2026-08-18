package web

import (
	"strings"
	"testing"
)

// TestAppJS_TemperatureNAUsesZoneNotCelsius guards against regressing issue
// #64: a legitimate 0.0 °C reading must not render as "n/a". A failed
// collection is distinguished by an empty zone (Temperature.Zone is always
// set on a successful reading), not by the celsius value's truthiness —
// 0 is a valid, falsy reading that a `snap.temperature?.celsius` check would
// wrongly treat as missing.
func TestAppJS_TemperatureNAUsesZoneNotCelsius(t *testing.T) {
	data, err := assetsFS.ReadFile("assets/app.js")
	if err != nil {
		t.Fatalf("read app.js: %v", err)
	}
	src := string(data)

	if strings.Contains(src, "snap.temperature?.celsius)") || strings.Contains(src, "snap.temperature && snap.temperature.celsius)") {
		t.Errorf("app.js must not gate the temperature display on celsius truthiness (0 °C is a valid reading)")
	}
	if !strings.Contains(src, "snap.temperature?.zone") {
		t.Errorf("expected app.js to key the temperature \"n/a\" fallback off snap.temperature?.zone")
	}
}
