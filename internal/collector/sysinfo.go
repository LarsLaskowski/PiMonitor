package collector

import (
	"bufio"
	"os"
	"strings"
	"syscall"
)

const (
	osReleasePath       = "/etc/os-release"
	deviceTreeModelPath = "/proc/device-tree/model"
	cpuinfoPath         = "/proc/cpuinfo"
)

// kernelRelease returns the running kernel version (uname -r equivalent)
// via a syscall rather than shelling out to the uname binary.
func kernelRelease() string {
	var uts syscall.Utsname
	if err := syscall.Uname(&uts); err != nil {
		return ""
	}
	return utsnameToString(uts.Release[:])
}

// utsnameToString converts a NUL-terminated int8/uint8 array (the
// platform-specific element type of syscall.Utsname fields) into a Go
// string.
func utsnameToString(field any) string {
	var b []byte
	switch f := field.(type) {
	case []int8:
		b = make([]byte, len(f))
		for i, c := range f {
			b[i] = byte(c)
		}
	case []uint8:
		b = f
	}
	if i := indexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return string(b)
}

func indexByte(b []byte, c byte) int {
	for i, v := range b {
		if v == c {
			return i
		}
	}
	return -1
}

// parseOSRelease parses /etc/os-release (or /usr/lib/os-release) content
// into a key->value map, per the freedesktop.org os-release format used
// by Debian, Raspberry Pi OS, and Ubuntu.
func parseOSRelease(data string) map[string]string {
	result := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		val = strings.Trim(val, `"`)
		result[key] = val
	}
	return result
}

// distributionName returns a human-readable distribution string, e.g.
// "Raspberry Pi OS Bookworm (Debian 12)", or the ID/VERSION_ID as a
// fallback, or "" if /etc/os-release is unreadable.
func distributionName(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	fields := parseOSRelease(string(data))
	if pretty := fields["PRETTY_NAME"]; pretty != "" {
		return pretty
	}
	if id, ver := fields["ID"], fields["VERSION_ID"]; id != "" {
		if ver != "" {
			return id + " " + ver
		}
		return id
	}
	return ""
}

// piModel returns the board model string, e.g. "Raspberry Pi 4 Model B
// Rev 1.4", read from the device tree. Falls back to /proc/cpuinfo's
// "Model" field (present on Raspberry Pi OS kernels) if the device tree
// path is unavailable, e.g. when running locally on non-ARM hardware for
// development.
func piModel(deviceTreePath, cpuinfoFallbackPath string) string {
	if data, err := os.ReadFile(deviceTreePath); err == nil {
		return strings.TrimRight(string(data), "\x00\n")
	}

	data, err := os.ReadFile(cpuinfoFallbackPath)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		key, val, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "Model" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// cpuModel returns a human-readable CPU model string from /proc/cpuinfo's
// "model name" field (first occurrence). This field is present on x86 and
// many ARM kernels but not guaranteed on every Raspberry Pi kernel, in
// which case an empty string is returned and the caller shows just the
// core count.
func cpuModel(cpuinfoPath string) string {
	data, err := os.ReadFile(cpuinfoPath)
	if err != nil {
		return ""
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		key, val, ok := strings.Cut(scanner.Text(), ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "model name" {
			return strings.TrimSpace(val)
		}
	}
	return ""
}

// SysInfoCollector reads system identity information that only changes on
// reboot/OS upgrade, so it is collected once rather than on every tick.
type SysInfoCollector struct {
	osReleasePath       string
	deviceTreeModelPath string
	cpuinfoPath         string
}

// NewSysInfoCollector creates a SysInfoCollector using the standard Linux
// paths.
func NewSysInfoCollector() *SysInfoCollector {
	return &SysInfoCollector{
		osReleasePath:       osReleasePath,
		deviceTreeModelPath: deviceTreeModelPath,
		cpuinfoPath:         cpuinfoPath,
	}
}

// Collect returns the current kernel version, distribution, Pi model, and
// CPU model. Any individual value that cannot be determined is left as an
// empty string rather than causing an error.
func (c *SysInfoCollector) Collect() SystemInfo {
	return SystemInfo{
		KernelVersion: kernelRelease(),
		Distribution:  distributionName(c.osReleasePath),
		PiModel:       piModel(c.deviceTreeModelPath, c.cpuinfoPath),
		CPUModel:      cpuModel(c.cpuinfoPath),
	}
}
