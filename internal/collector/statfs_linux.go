//go:build linux

package collector

import "syscall"

// platformStatfs wraps syscall.Statfs into the platform-neutral fsStats
// shape. Statfs_t.Bsize is int64 on amd64/arm64 but int32 on arm, so the
// conversion to uint64 happens here rather than at every call site.
func platformStatfs(path string) (fsStats, error) {
	var buf syscall.Statfs_t
	if err := syscall.Statfs(path, &buf); err != nil {
		return fsStats{}, err
	}
	return fsStats{
		Blocks: uint64(buf.Blocks),
		Bfree:  uint64(buf.Bfree),
		Bavail: uint64(buf.Bavail),
		Bsize:  uint64(buf.Bsize),
	}, nil
}
