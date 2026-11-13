package collector

import (
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"testing"
)

const osReleaseFixture = `PRETTY_NAME="Raspberry Pi OS Bookworm (Debian 12)"
NAME="Debian GNU/Linux"
VERSION_ID="12"
VERSION="12 (bookworm)"
ID=debian
`

func TestParseOSRelease(t *testing.T) {
	fields := parseOSRelease(osReleaseFixture)
	if fields["PRETTY_NAME"] != "Raspberry Pi OS Bookworm (Debian 12)" {
		t.Fatalf("PRETTY_NAME = %q", fields["PRETTY_NAME"])
	}
	if fields["ID"] != "debian" {
		t.Fatalf("ID = %q", fields["ID"])
	}
}

func TestDistributionName_PrefersPrettyName(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "os-release", osReleaseFixture)
	if got := distributionName(path); got != "Raspberry Pi OS Bookworm (Debian 12)" {
		t.Fatalf("distributionName = %q", got)
	}
}

func TestDistributionName_FallsBackToIDAndVersion(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "os-release", "ID=debian\nVERSION_ID=\"12\"\n")
	if got := distributionName(path); got != "debian 12" {
		t.Fatalf("distributionName = %q, want %q", got, "debian 12")
	}
}

func TestDistributionName_MissingFile(t *testing.T) {
	if got := distributionName(filepath.Join(t.TempDir(), "does-not-exist")); got != "" {
		t.Fatalf("expected empty string for missing os-release, got %q", got)
	}
}

func TestPiModel_FromDeviceTree(t *testing.T) {
	dir := t.TempDir()
	dtPath := writeTempFile(t, dir, "model", "Raspberry Pi 4 Model B Rev 1.4\x00")
	got := piModel(dtPath, filepath.Join(dir, "cpuinfo-unused"))
	if got != "Raspberry Pi 4 Model B Rev 1.4" {
		t.Fatalf("piModel = %q", got)
	}
}

func TestPiModel_FallsBackToCPUInfo(t *testing.T) {
	dir := t.TempDir()
	cpuinfo := "processor\t: 0\nModel\t\t: Raspberry Pi 3 Model B Plus Rev 1.3\n"
	cpuinfoPath := writeTempFile(t, dir, "cpuinfo", cpuinfo)
	missingDT := filepath.Join(dir, "does-not-exist")

	got := piModel(missingDT, cpuinfoPath)
	if got != "Raspberry Pi 3 Model B Plus Rev 1.3" {
		t.Fatalf("piModel = %q", got)
	}
}

func TestPiModel_NoSourceAvailable(t *testing.T) {
	dir := t.TempDir()
	got := piModel(filepath.Join(dir, "missing-dt"), filepath.Join(dir, "missing-cpuinfo"))
	if got != "" {
		t.Fatalf("expected empty string when no source is available, got %q", got)
	}
}

func TestCPUModel_FromCPUInfo(t *testing.T) {
	dir := t.TempDir()
	cpuinfo := "processor\t: 0\nmodel name\t: ARMv8 Processor rev 1 (v8l)\nBogoMIPS\t: 108.00\n"
	path := writeTempFile(t, dir, "cpuinfo", cpuinfo)
	if got := cpuModel(path); got != "ARMv8 Processor rev 1 (v8l)" {
		t.Fatalf("cpuModel = %q", got)
	}
}

func TestCPUModel_MissingField(t *testing.T) {
	dir := t.TempDir()
	// Some Raspberry Pi kernels omit "model name" entirely.
	path := writeTempFile(t, dir, "cpuinfo", "processor\t: 0\nHardware\t: BCM2835\n")
	if got := cpuModel(path); got != "" {
		t.Fatalf("expected empty string when model name is absent, got %q", got)
	}
}

func TestCPUModel_MissingFile(t *testing.T) {
	if got := cpuModel(filepath.Join(t.TempDir(), "nope")); got != "" {
		t.Fatalf("expected empty string for missing cpuinfo, got %q", got)
	}
}

func TestKernelRelease_NonEmpty(t *testing.T) {
	// kernelRelease uses the real syscall.Uname on Linux (kernel_linux.go)
	// and always succeeds there; on other platforms (kernel_other.go) it is
	// a documented no-op stub so the package compiles for local development
	// on macOS/Windows.
	if runtime.GOOS != "linux" {
		t.Skip("kernelRelease is Linux-only, see kernel_other.go")
	}
	if kernelRelease() == "" {
		t.Fatal("expected non-empty kernel release on Linux")
	}
}

func TestSysInfoCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	osReleasePath := writeTempFile(t, dir, "os-release", osReleaseFixture)
	dtPath := writeTempFile(t, dir, "model", "Raspberry Pi 4 Model B\x00")

	c := &SysInfoCollector{
		osReleasePath:       osReleasePath,
		deviceTreeModelPath: dtPath,
		cpuinfoPath:         filepath.Join(dir, "unused-cpuinfo"),
	}
	info := c.Collect()
	// Distribution and PiModel come from fixture files and must populate on
	// every platform; KernelVersion is Linux-only, see TestKernelRelease_NonEmpty.
	if info.Distribution == "" || info.PiModel == "" {
		t.Fatalf("expected fixture-sourced fields populated, got %+v", info)
	}
}

func TestReadHostname(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"AC1 trailing newline trimmed", "raspberrypi\n", "raspberrypi"},
		{"AC1 surrounding whitespace trimmed", " \t raspberrypi \t\n", "raspberrypi"},
		{"AC1 original case kept", "RaspberryPi-Lab\n", "RaspberryPi-Lab"},
		{"AC2 empty file", "", ""},
		{"AC2 whitespace only", " \t\n\n", ""},
		{"AC2 kernel placeholder (none)", "(none)\n", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempFile(t, t.TempDir(), "hostname", tt.content)
			got, err := readHostname(path)
			if err != nil {
				t.Fatalf("readHostname(%q) error = %v, want nil", tt.content, err)
			}
			if got != tt.want {
				t.Fatalf("readHostname(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}

	t.Run("AC2 missing file returns an error", func(t *testing.T) {
		got, err := readHostname(filepath.Join(t.TempDir(), "missing"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("readHostname(missing) error = %v, want one wrapping fs.ErrNotExist", err)
		}
		if got != "" {
			t.Fatalf("readHostname(missing) = %q, want empty", got)
		}
	})
}
