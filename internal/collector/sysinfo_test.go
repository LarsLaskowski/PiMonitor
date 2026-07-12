package collector

import (
	"path/filepath"
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

func TestKernelRelease_NonEmpty(t *testing.T) {
	// This exercises the real syscall.Uname on the test host; it should
	// always succeed on Linux and return a non-empty release string.
	if got := kernelRelease(); got == "" {
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
	if info.Distribution == "" || info.PiModel == "" || info.KernelVersion == "" {
		t.Fatalf("expected all fields populated, got %+v", info)
	}
}
