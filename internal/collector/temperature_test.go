package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeThermalZone(t *testing.T, root, zoneName, zoneType, tempMilliC string) {
	t.Helper()
	zoneDir := filepath.Join(root, zoneName)
	if err := os.MkdirAll(zoneDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", zoneDir, err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "type"), []byte(zoneType+"\n"), 0o644); err != nil {
		t.Fatalf("write type: %v", err)
	}
	if err := os.WriteFile(filepath.Join(zoneDir, "temp"), []byte(tempMilliC+"\n"), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
}

func TestFindCPUThermalZone_PrefersKnownType(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "some-other-sensor", "30000")
	writeThermalZone(t, root, "thermal_zone1", "cpu-thermal", "45000")

	zonePath, zoneType, err := findCPUThermalZone(filepath.Join(root, "thermal_zone*"))
	if err != nil {
		t.Fatalf("findCPUThermalZone: %v", err)
	}
	if zoneType != "cpu-thermal" {
		t.Fatalf("expected cpu-thermal zone to be preferred, got %q (%s)", zoneType, zonePath)
	}
}

func TestFindCPUThermalZone_FallsBackWhenNoKnownType(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "unknown-sensor", "30000")

	zonePath, zoneType, err := findCPUThermalZone(filepath.Join(root, "thermal_zone*"))
	if err != nil {
		t.Fatalf("findCPUThermalZone: %v", err)
	}
	if zonePath == "" || zoneType != "unknown-sensor" {
		t.Fatalf("expected fallback to first zone, got path=%q type=%q", zonePath, zoneType)
	}
}

func TestFindCPUThermalZone_NoZones(t *testing.T) {
	root := t.TempDir()
	if _, _, err := findCPUThermalZone(filepath.Join(root, "thermal_zone*")); err == nil {
		t.Fatal("expected error when no thermal zones exist")
	}
}

func TestReadThermalZoneMilliC(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "42800")

	celsius, err := readThermalZoneMilliC(filepath.Join(root, "thermal_zone0"))
	if err != nil {
		t.Fatalf("readThermalZoneMilliC: %v", err)
	}
	if diffFloat(celsius, 42.8) > 0.001 {
		t.Fatalf("celsius = %v, want 42.8", celsius)
	}
}

func TestParseVcgencmdTemp(t *testing.T) {
	got, err := parseVcgencmdTemp("temp=42.8'C\n")
	if err != nil {
		t.Fatalf("parseVcgencmdTemp: %v", err)
	}
	if diffFloat(got.Celsius, 42.8) > 0.001 {
		t.Fatalf("Celsius = %v, want 42.8", got.Celsius)
	}
}

func TestParseVcgencmdTemp_Malformed(t *testing.T) {
	if _, err := parseVcgencmdTemp("garbage output"); err == nil {
		t.Fatal("expected error for malformed vcgencmd output")
	}
}

func TestTemperatureCollector_Collect(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")

	c := &TemperatureCollector{zonePath: filepath.Join(root, "thermal_zone0"), zoneType: "cpu-thermal"}
	temp, gpuTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp != nil {
		t.Fatalf("expected no GPU temp when vcgencmd is not configured, got %+v", gpuTemp)
	}
}

func TestTemperatureCollector_Collect_NoZoneDetected(t *testing.T) {
	c := &TemperatureCollector{}
	if _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error when no thermal zone was detected")
	}
}

func TestTemperatureCollector_Collect_RedetectsZone(t *testing.T) {
	root := t.TempDir()
	glob := filepath.Join(root, "thermal_zone*")

	now := time.Unix(1_700_000_000, 0)
	c := &TemperatureCollector{
		zoneGlob:         glob,
		now:              func() time.Time { return now },
		vcgencmdDetected: true, // skip vcgencmd lookup in this test
	}

	// No zone exists yet: Collect must fail.
	if _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error when no thermal zone exists yet")
	}

	// The zone appears after start.
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "48000")

	// Still within the throttle window: re-detection is suppressed.
	now = now.Add(detectRetryInterval - time.Second)
	if _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected re-detection to be throttled within detectRetryInterval")
	}

	// Past the throttle window: the same collector now picks up the zone.
	now = now.Add(2 * time.Second)
	temp, _, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect after zone appeared: %v", err)
	}
	if temp.Zone != "cpu-thermal" || diffFloat(temp.Celsius, 48.0) > 0.001 {
		t.Fatalf("temp = %+v, want zone=cpu-thermal celsius=48.0", temp)
	}
}
