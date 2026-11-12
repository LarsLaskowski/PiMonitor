package collector

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// TestParseVcgencmdTemp covers every output shape parseVcgencmdTemp must
// handle: the plain GPU/SoC die reading, the PMIC reading (issue #56,
// identical "temp=NN.N'C" form so both share this parser), and the two
// ways firmware reports "not a temperature" — an explicit error line when
// the requested sensor doesn't exist on this board, and output that is
// simply unparseable. Both error cases must be errVcgencmdUnsupportedOutput
// so TemperatureCollector.Collect can tell "no such sensor" (latch, stop
// asking) apart from a transient exec/timeout failure (keep retrying).
func TestParseVcgencmdTemp(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantTemp float64
		wantErr  bool
	}{
		{name: "GPU/SoC die reading", output: "temp=42.8'C\n", wantTemp: 42.8},
		{name: "PMIC reading, same form as the die reading", output: "temp=52.1'C\n", wantTemp: 52.1},
		{name: "unsupported sensor (Pi 3 asked for pmic)", output: `error=1 error_msg="Invalid arguments"`, wantErr: true},
		{name: "malformed output", output: "garbage output", wantErr: true},
		{name: "temp= prefix with an unparseable number", output: "temp=N/A'C", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVcgencmdTemp(tt.output)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected an error, got nil")
				}
				if !errors.Is(err, errVcgencmdUnsupportedOutput) {
					t.Fatalf("error = %v, want errVcgencmdUnsupportedOutput", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVcgencmdTemp: %v", err)
			}
			if diffFloat(got, tt.wantTemp) > 0.001 {
				t.Fatalf("celsius = %v, want %v", got, tt.wantTemp)
			}
		})
	}
}

func TestTemperatureCollector_Collect(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")

	c := &TemperatureCollector{zonePath: filepath.Join(root, "thermal_zone0"), zoneType: "cpu-thermal"}
	temp, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	gpuTemp, gpuErr := c.CollectGPU(context.Background())
	if gpuErr != nil || gpuTemp != nil {
		t.Fatalf("CollectGPU = %+v, %v; want nil, nil when vcgencmd is not configured", gpuTemp, gpuErr)
	}
	pmicTemp, pmicErr := c.CollectPMIC(context.Background())
	if pmicErr != nil || pmicTemp != nil {
		t.Fatalf("CollectPMIC = %+v, %v; want nil, nil when vcgencmd is not configured", pmicTemp, pmicErr)
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
	temp, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	gpuTemp, err := c.CollectGPU(context.Background())
	if err != nil {
		t.Fatalf("CollectGPU: %v", err)
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

// TestTemperatureCollector_Collect_PMIC covers every way `measure_temp
// pmic` can answer: a genuine reading (Pi 4/5), the "sensor not present"
// error line (Pi 3 and earlier), and the invocation simply failing outright
// (a firmware that fails the exec rather than printing an error line). In
// every case the GPU/SoC reading and the overall collection must survive
// undisturbed; only the PMIC field's presence differs.
func TestTemperatureCollector_Collect_PMIC(t *testing.T) {
	tests := []struct {
		name       string
		pmicScript string
		wantPMIC   bool
		wantPMICC  float64
	}{
		{
			name:       "PMIC sensor present (Pi 4/5)",
			pmicScript: `  echo "temp=52.1'C"`,
			wantPMIC:   true,
			wantPMICC:  52.1,
		},
		{
			name:       "PMIC unsupported: error line (Pi 3 and earlier)",
			pmicScript: `  echo 'error=1 error_msg="Invalid arguments"'`,
			wantPMIC:   false,
		},
		{
			name:       "PMIC invocation exits non-zero",
			pmicScript: "  exit 1",
			wantPMIC:   false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPMICCollect(t, tt.pmicScript, tt.wantPMIC, tt.wantPMICC)
		})
	}
}

// assertPMICCollect runs Collect against a fake vcgencmd whose `measure_temp
// pmic` invocation is scripted by pmicScript, and checks the three things
// every TestTemperatureCollector_Collect_PMIC case cares about: the die
// reading is unaffected, the GPU/SoC reading always survives whatever the
// PMIC invocation did, and the PMIC field itself is present with wantPMICC
// (if wantPMIC) or absent (if not). Split out of the table-driven test's
// t.Run closure to keep that function's (and this one's) cognitive
// complexity down — SonarQube counts a closure's branching into its
// enclosing function, and the combined for-loop-plus-closure had crept past
// the default threshold of 15.
func assertPMICCollect(t *testing.T, pmicScript string, wantPMIC bool, wantPMICC float64) {
	t.Helper()
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, t.TempDir(), pmicScript)

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}
	temp, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	gpuTemp, err := c.CollectGPU(context.Background())
	if err != nil {
		t.Fatalf("CollectGPU: %v", err)
	}
	pmicTemp, pmicErr := c.CollectPMIC(context.Background())
	if wantPMIC && pmicErr != nil {
		t.Fatalf("CollectPMIC: %v", pmicErr)
	}
	if !wantPMIC && pmicErr == nil {
		t.Fatal("CollectPMIC: expected an error when the PMIC reading is unavailable")
	}
	if gpuTemp == nil || diffFloat(gpuTemp.Celsius, 42.8) > 0.001 {
		t.Fatalf("gpuTemp = %+v, want Celsius=42.8 (the die reading must survive whatever the PMIC invocation did)", gpuTemp)
	}
	if !wantPMIC {
		if pmicTemp != nil {
			t.Fatalf("pmicTemp = %+v, want nil", pmicTemp)
		}
		return
	}
	if pmicTemp == nil || diffFloat(pmicTemp.Celsius, wantPMICC) > 0.001 {
		t.Fatalf("pmicTemp = %+v, want Celsius=%v", pmicTemp, wantPMICC)
	}
}

// TestTemperatureCollector_Collect_PMICUnsupportedLatches guards the "stop
// trying" latch: once vcgencmd has answered `measure_temp pmic` with a
// non-temperature response, a board without a PMIC sensor must not pay
// another such invocation on every subsequent fast tick for the rest of
// the process's life — the condition cannot change at runtime.
func TestTemperatureCollector_Collect_PMICUnsupportedLatches(t *testing.T) {
	scriptDir := t.TempDir()
	logFile := filepath.Join(scriptDir, "pmic-invocations")
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, scriptDir, `  echo "$@" >> `+logFile+`
  echo 'error=1 error_msg="Invalid arguments"'`)

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}

	const ticks = 3
	for i := 0; i < ticks; i++ {
		pmicTemp, err := c.CollectPMIC(context.Background())
		if !errors.Is(err, errVcgencmdUnsupportedOutput) {
			t.Fatalf("CollectPMIC #%d: err = %v, want errVcgencmdUnsupportedOutput", i, err)
		}
		if pmicTemp != nil {
			t.Fatalf("CollectPMIC #%d: pmicTemp = %+v, want nil", i, pmicTemp)
		}
	}

	if c.pmicUnsupported == nil {
		t.Fatal("expected pmicUnsupported to latch true after an unsupported-output response")
	}

	logged, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("read pmic invocation log (expected exactly one invocation): %v", err)
	}
	if invocations := len(strings.Split(strings.TrimSpace(string(logged)), "\n")); invocations != 1 {
		t.Fatalf("measure_temp pmic was invoked %d times across %d Collect() calls, want 1 (the latch should suppress the rest):\n%s",
			invocations, ticks, logged)
	}
}

// TestTemperatureCollector_Collect_PMICTransientFailureKeepsRetrying is the
// counterpart to the latch test above: a failed exec (as opposed to a
// successful one with unsupported output) must not latch, since it may
// well succeed on the next tick.
func TestTemperatureCollector_Collect_PMICTransientFailureKeepsRetrying(t *testing.T) {
	root := t.TempDir()
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "50000")
	path := pmicAwareVcgencmd(t, t.TempDir(), "  exit 1")

	c := &TemperatureCollector{
		zonePath: filepath.Join(root, "thermal_zone0"),
		zoneType: "cpu-thermal",
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}

	for i := 0; i < 2; i++ {
		if _, err := c.CollectPMIC(context.Background()); err == nil || errors.Is(err, errVcgencmdUnsupportedOutput) {
			t.Fatalf("CollectPMIC #%d: err = %v, want a transient error that is not errVcgencmdUnsupportedOutput", i, err)
		}
	}

	if c.pmicUnsupported != nil {
		t.Fatal("a failed exec must not latch pmicUnsupported: the same board may still have a PMIC sensor")
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
	temp, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(temp.Celsius, 50.0) > 0.001 {
		t.Fatalf("Celsius = %v, want 50.0", temp.Celsius)
	}
	gpuTemp, gpuErr := c.CollectGPU(context.Background())
	if gpuErr == nil || gpuTemp != nil {
		t.Fatalf("CollectGPU = %+v, %v; want nil reading and an error when vcgencmd exec fails", gpuTemp, gpuErr)
	}
	pmicTemp, pmicErr := c.CollectPMIC(context.Background())
	if pmicErr == nil || pmicTemp != nil {
		t.Fatalf("CollectPMIC = %+v, %v; want nil reading and an error when vcgencmd exec fails", pmicTemp, pmicErr)
	}
}

func TestTemperatureCollector_Collect_NoZoneDetected(t *testing.T) {
	c := &TemperatureCollector{}
	if _, err := c.Collect(); err == nil {
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
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected error when no thermal zone exists yet")
	}

	// The zone appears after start.
	writeThermalZone(t, root, "thermal_zone0", "cpu-thermal", "48000")

	// Still within the throttle window: re-detection is suppressed.
	now = now.Add(detectRetryInterval - time.Second)
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected re-detection to be throttled within detectRetryInterval")
	}

	// Past the throttle window: the same collector now picks up the zone.
	now = now.Add(2 * time.Second)
	temp, err := c.Collect()
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
	if temp, err := c.Collect(); err != nil {
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
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected error while re-detection is throttled after the zone vanished")
	}

	// Past the window the same collector recovers onto the new zone.
	now = now.Add(2 * time.Second)
	temp, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect after zone moved: %v", err)
	}
	if temp.Zone != "soc_thermal" || diffFloat(temp.Celsius, 55.0) > 0.001 {
		t.Fatalf("temp = %+v, want zone=soc_thermal celsius=55.0", temp)
	}
}

// TestTemperatureCollector_CollectGPU_NoVcgencmd_AC2 pins the missing-vcgencmd
// error of the GPU reading (logged once by the collector, issue #197) and that
// a nil runner stays a silent, disabled reading.
func TestTemperatureCollector_CollectGPU_NoVcgencmd_AC2(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)

	t.Run("AC2 runner without vcgencmd", func(t *testing.T) {
		c := &TemperatureCollector{vcg: &vcgencmdRunner{now: func() time.Time { return now }, lastDetect: now}}

		gpu, err := c.CollectGPU(context.Background())

		if !errors.Is(err, errVcgencmdUnavailable) {
			t.Fatalf("CollectGPU error = %v, want errVcgencmdUnavailable", err)
		}
		if gpu != nil {
			t.Fatalf("CollectGPU = %+v, want nil", gpu)
		}
	})

	t.Run("AC2 runner without vcgencmd for PMIC", func(t *testing.T) {
		c := &TemperatureCollector{vcg: &vcgencmdRunner{now: func() time.Time { return now }, lastDetect: now}}

		pmic, err := c.CollectPMIC(context.Background())

		if !errors.Is(err, errVcgencmdUnavailable) {
			t.Fatalf("CollectPMIC error = %v, want errVcgencmdUnavailable", err)
		}
		if pmic != nil {
			t.Fatalf("CollectPMIC = %+v, want nil", pmic)
		}
		if c.pmicUnsupported != nil {
			t.Fatalf("pmicUnsupported = %v, want a missing vcgencmd not to latch", c.pmicUnsupported)
		}
	})

	t.Run("AC2 nil runner disables both readings", func(t *testing.T) {
		c := &TemperatureCollector{}

		gpu, gpuErr := c.CollectGPU(context.Background())
		pmic, pmicErr := c.CollectPMIC(context.Background())

		if gpu != nil || gpuErr != nil || pmic != nil || pmicErr != nil {
			t.Fatalf("nil runner: gpu=(%+v, %v) pmic=(%+v, %v), want (nil, nil) for both", gpu, gpuErr, pmic, pmicErr)
		}
	})
}

// TestTemperatureCollector_CollectGPUAndPMIC_WithoutThermalZone_AC3 pins that
// the vcgencmd readings do not depend on a readable thermal zone.
func TestTemperatureCollector_CollectGPUAndPMIC_WithoutThermalZone_AC3(t *testing.T) {
	path := pmicAwareVcgencmd(t, t.TempDir(), "  echo \"temp=52.1'C\"")
	c := &TemperatureCollector{
		zoneGlob: filepath.Join(t.TempDir(), "thermal_zone*"),
		now:      time.Now,
		vcg:      &vcgencmdRunner{detected: true, path: path},
	}

	_, zoneErr := c.Collect()
	gpu, gpuErr := c.CollectGPU(context.Background())
	pmic, pmicErr := c.CollectPMIC(context.Background())

	if zoneErr == nil {
		t.Fatal("Collect error = nil, want an error without a thermal zone")
	}
	if gpuErr != nil || gpu == nil || diffFloat(gpu.Celsius, 42.8) > 0.001 {
		t.Fatalf("CollectGPU = (%+v, %v), want 42.8 C and no error", gpu, gpuErr)
	}
	if pmicErr != nil || pmic == nil || diffFloat(pmic.Celsius, 52.1) > 0.001 {
		t.Fatalf("CollectPMIC = (%+v, %v), want 52.1 C and no error", pmic, pmicErr)
	}
}

// TestParseVcgencmdTemp_ErrorText pins the exact text of the parse errors:
// the quoted output is bounded to one line of at most vcgencmdDetailMaxLen
// bytes and appears once (the strconv cause is reduced to its reason).
func TestParseVcgencmdTemp_ErrorText(t *testing.T) {
	const unsupported = "vcgencmd: output is not a temperature reading: "
	tests := []struct {
		name    string
		output  string
		want    string
		wantIs  error
		wantNum error
	}{
		{
			name:   "AC1 long output is bounded",
			output: strings.Repeat("x", 250),
			want:   unsupported + "unexpected vcgencmd output: " + strconv.Quote(strings.Repeat("x", 200)+"..."),
		},
		{
			name:   "AC1 multi-line output on one line",
			output: "error=1\nerror_msg=x",
			want:   unsupported + `unexpected vcgencmd output: "error=1 error_msg=x"`,
		},
		{
			name:   "AC1 short output unchanged",
			output: "garbage output",
			want:   unsupported + `unexpected vcgencmd output: "garbage output"`,
		},
		{
			name:    "AC2 long unparsable number is bounded",
			output:  "temp=" + strings.Repeat("x", 250),
			want:    unsupported + "parse vcgencmd temp " + strconv.Quote("temp="+strings.Repeat("x", 195)+"...") + ": invalid syntax",
			wantNum: strconv.ErrSyntax,
		},
		{
			name:    "AC2 out-of-range number",
			output:  "temp=1e999'C",
			want:    unsupported + `parse vcgencmd temp "temp=1e999'C": value out of range`,
			wantNum: strconv.ErrRange,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseVcgencmdTemp(tt.output)
			if err == nil {
				t.Fatalf("parseVcgencmdTemp(%q) error = nil, want %q", tt.output, tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("error text = %q, want %q", err.Error(), tt.want)
			}
			if !errors.Is(err, errVcgencmdUnsupportedOutput) {
				t.Errorf("errors.Is(err, errVcgencmdUnsupportedOutput) = false, err = %v", err)
			}
			if tt.wantNum != nil && !errors.Is(err, tt.wantNum) {
				t.Errorf("errors.Is(err, %v) = false, err = %v", tt.wantNum, err)
			}
		})
	}
}

// TestTemperatureCollector_CollectPMIC_LongOutputBounded_AC5 checks the
// bounded text end to end through a fake vcgencmd and that the latched error
// is returned again without running the script a second time.
func TestTemperatureCollector_CollectPMIC_LongOutputBounded_AC5(t *testing.T) {
	dir := t.TempDir()
	countFile := filepath.Join(dir, "invocations")
	// Shell builtins only: the child runs with an empty environment.
	path := writeFakeVcgencmd(t, dir, "fake-vcgencmd", `echo "$@" >> `+countFile+`
i=0
while [ "$i" -lt 5000 ]; do
  printf x
  i=$((i+1))
done
echo`)
	c := &TemperatureCollector{vcg: &vcgencmdRunner{detected: true, path: path}}

	want := "vcgencmd: output is not a temperature reading: unexpected vcgencmd output: " +
		strconv.Quote(strings.Repeat("x", 200)+"...")

	_, err1 := c.CollectPMIC(context.Background())
	if err1 == nil || err1.Error() != want {
		t.Fatalf("first CollectPMIC error = %v, want %q", err1, want)
	}
	if !errors.Is(err1, errVcgencmdUnsupportedOutput) {
		t.Errorf("first error does not wrap errVcgencmdUnsupportedOutput: %v", err1)
	}
	_, err2 := c.CollectPMIC(context.Background())
	if err2 == nil || err2.Error() != want {
		t.Fatalf("second CollectPMIC error = %v, want the same latched %q", err2, want)
	}

	logged, err := os.ReadFile(countFile)
	if err != nil {
		t.Fatalf("read invocation log: %v", err)
	}
	if n := len(strings.Split(strings.TrimSpace(string(logged)), "\n")); n != 1 {
		t.Errorf("fake vcgencmd ran %d times across 2 calls, want 1:\n%s", n, logged)
	}
}
