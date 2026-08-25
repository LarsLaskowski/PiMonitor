//go:build !linux

package collector

import "errors"

// platformStatfs is not supported on non-Linux platforms. PiMonitor only
// ever runs on Linux (Raspberry Pi OS); collectDisk already treats a statfs
// error as "skip this mountpoint", so this stub exists solely to make the
// package compile for local development on macOS/Windows.
func platformStatfs(path string) (fsStats, error) {
	return fsStats{}, errors.New("statfs not supported on this platform")
}
