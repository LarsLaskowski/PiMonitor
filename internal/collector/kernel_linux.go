//go:build linux

package collector

import "syscall"

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
