//go:build !linux

package collector

import (
	"errors"
	"net/netip"
)

// errOwnerUnsupported is returned where owners cannot be read.
var errOwnerUnsupported = errors.New("owners of files and connections are only readable on Linux")

// ProcNetTCPOwner returns a PeerOwnerFunc that always fails: the owner of a
// connection is only readable from /proc on Linux.
func ProcNetTCPOwner(string) PeerOwnerFunc {
	return func(_, _ netip.AddrPort) (uint32, error) {
		return 0, errOwnerUnsupported
	}
}

// pathOwner always fails: the owner of a path is only read on Linux, so off
// Linux the API token is never sent.
func pathOwner(string) (uint32, error) {
	return 0, errOwnerUnsupported
}
