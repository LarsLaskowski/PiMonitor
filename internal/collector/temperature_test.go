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
	if diffFloat(got, 42.8) > 0.001 {
		t.Fatalf("celsius = %v, want 42.8", got)
	}
}

// TestParseVcgencmdTemp_PMICOutput documents that `vcgencmd measure_temp
// pmic` reports the same "temp=NN.N'C" form as the plain measure_temp, so
// both readings share one parser (issue #56).
func TestParseVcgencmdTemp_PMICOutput(t *testing.T) {
	got, err := parseVcgencmdTemp("temp=52.1'C\n")
	if err != nil {
		t.Fatalf("parseVcgencmdTemp: %v", err)
	}
	if diffFloat(got, 52.1) > 0.001 {
		t.Fatalf("celsius = %v, want 52.1", got)
	}
}

// TestParseVcgencmdTemp_UnsupportedSensor covers what firmware answers when
// the requested sensor does not exist on the board (a Pi 3 asked for the
// PMIC): an error line rather than a temperature, which must be reported as
// a parse error so the caller can omit the field.
func TestParseVcgencmdTemp_UnsupportedSensor(t *testing.T) {
	if _, err := parseVcgencmdTemp(`error=1 error_msg="Invalid arguments"`); err == nil {
		t.Fatal("expected error for an unsupported-sensor vcgencmd response")
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
	temp, gpuTemp, pmicTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp != nil {
		t.Fatalf("expected no GPU temp when vcgencmd is not configured, got %+v", gpuTemp)
	}
	if pmicTemp != nil {
		t.Fatalf("expected no PMIC temp when vcgencmd is not configured, got %+v", pmicTemp)
	}
}

func TestTemperatureCollector_Collect_WithGPUTemp(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	scriptDir := t.TempDir()
	path := writeFakeVcgencmd(t, scriptDir, "fake-vcgencmd", `echo "temp=42.8'C"`)

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	temp, gpuTemp, _, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp == nil || diffFloat(gpuTemp.Celsius, 42.8) > 0.001 {
		t.Fatalf("gpuTemp = %+v, want Celsius=42.8", gpuTemp)
	}
}

// pmicAwareVcgencmd writes a fake vcgencmd whose `measure_temp pmic`
// invocation answers differently from the plain `measure_temp`, mirroring a
// board where both sensors exist (Pi 4/5) or only the die sensor does
// (Pi 3 and earlier). pmicScript is the body run for the pmic variant.
func pmicAwareVcgencmd(t *testing.T, dir, pmicScript string) string {
	t.Helper()
	return writeFakeVcgencmd(t, dir, "fake-vcgencmd", `if [ "$2" = "pmic" ]; then
`+pmicScript+`
else
  echo "temp=42.8'C"
fi`)
}

func TestTemperatureCollector_Collect_WithPMICTemp(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, t.TempDir(), `  echo "temp=52.1'C"`)

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	temp, gpuTemp, pmicTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp == nil || diffFloat(gpuTemp.Celsius, 42.8) > 0.001 {
		t.Fatalf("gpuTemp = %+v, want Celsius=42.8", gpuTemp)
	}
	if pmicTemp == nil || diffFloat(pmicTemp.Celsius, 52.1) > 0.001 {
		t.Fatalf("pmicTemp = %+v, want Celsius=52.1", pmicTemp)
	}
}

// TestTemperatureCollector_Collect_PMICUnsupported covers a Pi 3 and
// earlier: `measure_temp` still answers, while `measure_temp pmic` reports
// an error line because the board has no PMIC sensor. The PMIC field must
// be omitted without disturbing the GPU/SoC reading or failing collection.
func TestTemperatureCollector_Collect_PMICUnsupported(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, t.TempDir(), `  echo 'error=1 error_msg="Invalid arguments"'`)

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	temp, gpuTemp, pmicTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp == nil || diffFloat(gpuTemp.Celsius, 42.8) > 0.001 {
		t.Fatalf("gpuTemp = %+v, want Celsius=42.8 (the die reading must survive a missing PMIC)", gpuTemp)
	}
	if pmicTemp != nil {
		t.Fatalf("expected no PMIC temp on a board without the sensor, got %+v", pmicTemp)
	}
}

// TestTemperatureCollector_Collect_PMICExitsNonZero is the other shape of
// "no PMIC on this board": firmware that fails the invocation outright
// rather than printing an error line.
func TestTemperatureCollector_Collect_PMICExitsNonZero(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, t.TempDir(), "  exit 1")

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	_, gpuTemp, pmicTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if gpuTemp == nil {
		t.Fatal("expected the GPU/SoC reading to survive a failing PMIC invocation")
	}
	if pmicTemp != nil {
		t.Fatalf("expected no PMIC temp when the pmic invocation fails, got %+v", pmicTemp)
	}
}

func TestTemperatureCollector_Collect_VcgencmdExecFails(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	scriptDir := t.TempDir()
	path := writeFakeVcgencmd(t, scriptDir, "fake-vcgencmd", "exit 1")

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	temp, gpuTemp, pmicTemp, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	if gpuTemp != nil {
		t.Fatalf("expected no GPU temp when vcgencmd exec fails, got %+v", gpuTemp)
	}
	if pmicTemp != nil {
		t.Fatalf("expected no PMIC temp when vcgencmd exec fails, got %+v", pmicTemp)
	}
}

func TestTemperatureCollector_Collect_NoZoneDetected(t *testing.T) {
	c := &TemperatureCollector{}
	if _, _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error when no thermal zone was detected")
	}
}

func TestTemperatureCollector_Collect_RedetectsZone(t *testing.T) {
	root := t.TempDir()
	glob := filepath.Join(root, "thermal_zone*")

	now := time.Unix(1_700_000_000, 0)
	c := &TemperatureCollector{
		zoneGlob: glob,
		now:      func() time.Time { return now },
		// vcg left nil: skips vcgencmd entirely for this test.
	}

	// No zone exists yet: Collect must fail.
	if _, _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error when no thermal zone exists yet")
	}

	// The zone appears after start.
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "48000")

	// Still within the throttle window: re-detection is suppressed.
	now = now.Add(detectRetryInterval - time.Second)
	if _, _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected re-detection to be throttled within detectRetryInterval")
	}

	// Past the throttle window: the same collector now picks up the zone.
	now = now.Add(2 * time.Second)
	temp, _, _, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect after zone appeared: %v", err)
	}
	if temp.Zone != "cpu-thermal" || diffFloat(temp.Celsius, 48.0) > 0.001 {
		t.Fatalf("temp = %+v, want zone=cpu-thermal celsius=48.0", temp)
	}
}

func TestTemperatureCollector_Collect_RedetectsAfterZoneVanishes(t *testing.T) {
	root := t.TempDir()
	glob := filepath.Join(root, "thermal_zone*")

	now := time.Unix(1_700_000_000, 0)
	c := &TemperatureCollector{
		zoneGlob: glob,
		now:      func() time.Time { return now },
		// vcg left nil: skips vcgencmd entirely for this test.
	}

	// A zone exists at first and is cached by Collect.
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "40000")
	if temp, _, _, err := c.Collect(context.Background()); err != nil {
		t.Fatalf("initial Collect: %v", err)
	} else if temp.Zone != "cpu-thermal" {
		t.Fatalf("initial zone = %q, want cpu-thermal", temp.Zone)
	}

	// The cached zone disappears (driver/kernel change) and a differently
	// numbered/typed zone takes its place.
	if err := os.RemoveAll(filepath.Join(root, "thermal_zone0")); err != nil {
		t.Fatalf("remove zone: %v", err)
	}
	writeThermalZone(t, root, "thermal_zone1", "soc_thermal", "55000")

	// Within the throttle window the collector cannot re-detect yet, so the
	// stale path keeps failing (documents that throttling covers this path too).
	now = now.Add(detectRetryInterval - time.Second)
	if _, _, _, err := c.Collect(context.Background()); err == nil {
		t.Fatal("expected error while re-detection is throttled after the zone vanished")
	}

	// Past the window the same collector recovers onto the new zone.
	now = now.Add(2 * time.Second)
	temp, _, _, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect after zone moved: %v", err)
	}
	if temp.Zone != "soc_thermal" || diffFloat(temp.Celsius, 55.0) > 0.001 {
		t.Fatalf("temp = %+v, want zone=soc_thermal celsius=55.0", temp)
	}
}
