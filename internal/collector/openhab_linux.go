//go:build linux

package collector

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// tcpStateEstablished is the state column of an established connection.
const tcpStateEstablished = "01"

// ProcNetTCPOwner returns a PeerOwnerFunc that reads procRoot/net/tcp and
// procRoot/net/tcp6, line by line and stopping at the matching entry.
func ProcNetTCPOwner(procRoot string) PeerOwnerFunc {
	return func(local, remote netip.AddrPort) (uint32, error) {
		for _, name := range []string{"tcp", "tcp6"} {
			uid, found, err := scanProcNetTCP(filepath.Join(procRoot, "net", name), local, remote)
			if err != nil {
				return 0, err
			}
			if found {
				return uid, nil
			}
		}
		return 0, fmt.Errorf("no established connection from %s to %s in %s/net", local, remote, procRoot)
	}
}

// scanProcNetTCP looks in one net/tcp file for the established entry whose
// local side is the connection's remote side and vice versa, that is the
// server side, and returns its uid.
func scanProcNetTCP(path string, local, remote netip.AddrPort) (uid uint32, found bool, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, false, fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 8 || fields[3] != tcpStateEstablished {
			continue
		}
		entryLocal, okLocal := parseProcNetAddr(fields[1])
		entryRemote, okRemote := parseProcNetAddr(fields[2])
		if !okLocal || !okRemote || !sameAddrPort(entryLocal, remote) || !sameAddrPort(entryRemote, local) {
			continue
		}
		value, convErr := strconv.ParseUint(fields[7], 10, 32)
		if convErr != nil {
			continue
		}
		return uint32(value), true, nil
	}
	if err := scanner.Err(); err != nil {
		return 0, false, fmt.Errorf("read %s: %w", path, err)
	}
	return 0, false, nil
}

// sameAddrPort compares two endpoints, an IPv4-mapped address equal to its
// IPv4 form.
func sameAddrPort(a, b netip.AddrPort) bool {
	return a.Addr().Unmap() == b.Addr().Unmap() && a.Port() == b.Port()
}

// parseProcNetAddr parses "ADDR:PORT" of a net/tcp line: the address as 32-bit
// words in host byte order, the port as big-endian hex.
func parseProcNetAddr(text string) (netip.AddrPort, bool) {
	addrHex, portHex, found := strings.Cut(text, ":")
	if !found {
		return netip.AddrPort{}, false
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		return netip.AddrPort{}, false
	}
	raw, err := hex.DecodeString(addrHex)
	if err != nil || (len(raw) != 4 && len(raw) != 16) {
		return netip.AddrPort{}, false
	}
	for i := 0; i < len(raw); i += 4 {
		binary.NativeEndian.PutUint32(raw[i:], binary.BigEndian.Uint32(raw[i:]))
	}
	addr, ok := netip.AddrFromSlice(raw)
	if !ok {
		return netip.AddrPort{}, false
	}
	return netip.AddrPortFrom(addr, uint16(port)), true
}

// pathOwner returns the uid that owns path.
func pathOwner(path string) (uint32, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("no owner information")
	}
	return stat.Uid, nil
}
