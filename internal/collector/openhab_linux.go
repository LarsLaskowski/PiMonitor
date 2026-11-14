//go:build linux

package collector

import (
	"errors"
	"net/netip"
)

// ProcNetTCPOwner returns a PeerOwnerFunc that reads procRoot/net/tcp and
// procRoot/net/tcp6.
func ProcNetTCPOwner(procRoot string) PeerOwnerFunc {
	return func(local, remote netip.AddrPort) (uint32, error) {
		return 0, errors.New("not implemented")
	}
}
