//go:build !linux

package collector

// kernelRelease is a no-op stub on non-Linux platforms. PiMonitor only ever
// runs on Linux (Raspberry Pi OS); this exists so the package compiles for
// local development on macOS/Windows, where the kernel version simply
// reports as empty rather than failing the build.
func kernelRelease() string {
	return ""
}
