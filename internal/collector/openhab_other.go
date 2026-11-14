//go:build !linux

package collector

import (
	"errors"
	"net/netip"
)

// ProcNetTCPOwner returns a PeerOwnerFunc that always fails: the owner of a
// connection is only readable from /proc on Linux.
func ProcNetTCPOwner(procRoot string) PeerOwnerFunc {
	return func(local, remote netip.AddrPort) (uint32, error) {
		return 0, errors.New("not implemented")
	}
}
