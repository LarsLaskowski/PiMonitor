package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeHwmonChannel writes one hwmon "temp<idx>_input" file (millidegrees
// Celsius) and, when label is non-empty, its sibling "temp<idx>_label" file,
// under chipDir.
func writeHwmonChannel(t *testing.T, chipDir string, idx int, milliC, label string) {
	t.Helper()
	writeTempFile(t, chipDir, fmt.Sprintf("temp%d_input", idx), milliC+"\n")
	if label != "" {
		writeTempFile(t, chipDir, fmt.Sprintf("temp%d_label", idx), label+"\n")
	}
}

// writeHwmonChip creates a synthetic hwmon chip directory (root/name) with a
// "name" file (skipped when chipName is empty, to test the no-name
// fallback), returning the chip directory path.
func writeHwmonChip(t *testing.T, root, name, chipName string) string {
	t.Helper()
	chipDir := filepath.Join(root, name)
	if err := os.MkdirAll(chipDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", chipDir, err)
	}
	if chipName != "" {
		writeTempFile(t, chipDir, "name", chipName+"\n")
	}
	return chipDir
}

func TestReadHwmonSensors_MultipleChipsAndChannels(t *testing.T) {
	root := t.TempDir()
	cpu := writeHwmonChip(t, root, "hwmon0", "cpu_thermal")
	writeHwmonChannel(t, cpu, 1, "48600", "")
	nvme := writeHwmonChip(t, root, "hwmon1", "nvme")
	writeHwmonChannel(t, nvme, 1, "34900", "Composite")
	writeHwmonChannel(t, nvme, 2, "36000", "Sensor 1")

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors: %v", err)
	}
	want := []TemperatureSensor{
		{Chip: "cpu_thermal", Label: "cpu_thermal temp1", Hwmon: "hwmon0", Celsius: 48.6},
		{Chip: "nvme", Label: "Composite", Hwmon: "hwmon1", Celsius: 34.9},
		{Chip: "nvme", Label: "Sensor 1", Hwmon: "hwmon1", Celsius: 36.0},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d sensors, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sensor[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestReadHwmonSensors_DisambiguatesIdenticalChips covers two chips that
// report the same Chip name and Label (e.g. two identical NVMe drives, each
// exposing a "Composite" channel): Chip/Label alone cannot tell them apart,
// so Hwmon (the sysfs hwmon<N> directory each reading came from) must differ
// between them.
func TestReadHwmonSensors_DisambiguatesIdenticalChips(t *testing.T) {
	root := t.TempDir()
	nvme0 := writeHwmonChip(t, root, "hwmon3", "nvme")
	writeHwmonChannel(t, nvme0, 1, "34900", "Composite")
	nvme1 := writeHwmonChip(t, root, "hwmon4", "nvme")
	writeHwmonChannel(t, nvme1, 1, "36200", "Composite")

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 sensors, got %d: %+v", len(got), got)
	}
	if got[0].Chip != got[1].Chip || got[0].Label != got[1].Label {
		t.Fatalf("expected identical Chip/Label to set up the ambiguity this test checks, got %+v and %+v", got[0], got[1])
	}
	if got[0].Hwmon == got[1].Hwmon {
		t.Fatalf("expected distinct Hwmon directories to disambiguate identical chips, both got %q", got[0].Hwmon)
	}
	if got[0].Hwmon != "hwmon3" || got[1].Hwmon != "hwmon4" {
		t.Fatalf("Hwmon = %q, %q; want hwmon3, hwmon4", got[0].Hwmon, got[1].Hwmon)
	}
}

func TestReadHwmonSensors_MissingLabelFallsBackToChipNameAndIndex(t *testing.T) {
	root := t.TempDir()
	chip := writeHwmonChip(t, root, "hwmon0", "unknown_chip")
	writeHwmonChannel(t, chip, 3, "50000", "")

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 sensor, got %d: %+v", len(got), got)
	}
	want := TemperatureSensor{Chip: "unknown_chip", Label: "unknown_chip temp3", Hwmon: "hwmon0", Celsius: 50.0}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

// TestReadHwmonSensors_NoChipName covers a chip directory with no readable
// "name" file: the channel is still reported, with the fallback label
// degrading further to just "temp<N>" (no leading chip name).
func TestReadHwmonSensors_NoChipName(t *testing.T) {
	root := t.TempDir()
	chip := writeHwmonChip(t, root, "hwmon0", "")
	writeHwmonChannel(t, chip, 1, "20000", "")

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 sensor, got %d: %+v", len(got), got)
	}
	want := TemperatureSensor{Chip: "", Label: "temp1", Hwmon: "hwmon0", Celsius: 20.0}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

func TestReadHwmonSensors_NoHwmonDirectory(t *testing.T) {
	root := t.TempDir()

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors should not error when no hwmon directory exists, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no sensors, got %d: %+v", len(got), got)
	}
}

// TestReadHwmonSensors_MalformedChannelSkipped guards that one chip's
// unreadable/malformed temperature channel doesn't fail the whole
// collection - it is simply skipped, like a stalled mount or missing driver
// elsewhere in this package degrades rather than errors.
func TestReadHwmonSensors_MalformedChannelSkipped(t *testing.T) {
	root := t.TempDir()
	chip := writeHwmonChip(t, root, "hwmon0", "weird_chip")
	writeTempFile(t, chip, "temp1_input", "not-a-number\n")
	writeHwmonChannel(t, chip, 2, "42000", "")

	got, err := readHwmonSensors(filepath.Join(root, "hwmon*"))

	if err != nil {
		t.Fatalf("readHwmonSensors: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected the malformed channel to be skipped, got %d sensors: %+v", len(got), got)
	}
	if got[0].Celsius != 42.0 {
		t.Fatalf("Celsius = %v, want 42.0", got[0].Celsius)
	}
}

func TestHwmonCollector_Collect(t *testing.T) {
	root := t.TempDir()
	chip := writeHwmonChip(t, root, "hwmon0", "cpu_thermal")
	writeHwmonChannel(t, chip, 1, "45000", "")
	c := &HwmonCollector{glob: filepath.Join(root, "hwmon*")}

	got, err := c.Collect()

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 1 || got[0].Celsius != 45.0 {
		t.Fatalf("got %+v, want one sensor at 45.0C", got)
	}
}
