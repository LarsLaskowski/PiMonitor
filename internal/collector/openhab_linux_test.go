//go:build linux

package collector

import (
	"errors"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
)

const (
	procNetTCPHeader = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	// IPv4 LISTEN socket of uid 999 on 127.0.0.1:8080.
	procNetTCPListen = "   0: 0100007F:1F90 00000000:0000 0A 00000000:00000000 00:00000000 00000000   999        0 11111 1 0000000000000000 100 0 0 10 0\n"
	// Server side of 127.0.0.1:54321 -> 127.0.0.1:8080, uid 102.
	procNetTCPServer = "   1: 0100007F:1F90 0100007F:D431 01 00000000:00000000 00:00000000 00000000   102        0 22222 1 0000000000000000 20 4 30 10 -1\n"
	// Client side of the same connection, uid 0.
	procNetTCPClient = "   2: 0100007F:D431 0100007F:1F90 01 00000000:00000000 00:00000000 00000000     0        0 33333 1 0000000000000000 20 4 30 10 -1\n"
	// The server side's addresses in TIME_WAIT.
	procNetTCPTimeWait = "   3: 0100007F:1F90 0100007F:D431 06 00000000:00000000 03:00001770 00000000   102        0 0 3 0000000000000000\n"
	procNetTCPGarbage  = "   4: not a socket line\n"
	// The server side's entry with a uid that is not a number.
	procNetTCPBadUID = "   5: 0100007F:1F90 0100007F:D431 01 00000000:00000000 00:00000000 00000000   abc        0 22222 1 0000000000000000 20 4 30 10 -1\n"

	procNetTCP6Header = "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	// Dual-stack listener of uid 102 on [::]:8080.
	procNetTCP6Listen = "   0: 00000000000000000000000000000000:1F90 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000   102        0 44444 1 0000000000000000 100 0 0 10 0\n"
	// Server side of ::ffff:127.0.0.1:54321 -> ::ffff:127.0.0.1:8080, uid 102.
	procNetTCP6Mapped = "   1: 0000000000000000FFFF00000100007F:1F90 0000000000000000FFFF00000100007F:D431 01 00000000:00000000 00:00000000 00000000   102        0 55555 1 0000000000000000 20 4 30 10 -1\n"
	// Server side of [::1]:54321 -> [::1]:8080, uid 102.
	procNetTCP6Loopback = "   2: 00000000000000000000000001000000:1F90 00000000000000000000000001000000:D431 01 00000000:00000000 00:00000000 00000000   102        0 66666 1 0000000000000000 20 4 30 10 -1\n"
)

// writeProcNet writes net/tcp and net/tcp6 under a temp proc root.
func writeProcNet(t *testing.T, tcp, tcp6 string) string {
	t.Helper()
	procRoot := t.TempDir()
	mustMkdirAll(t, filepath.Join(procRoot, "net"))
	writeTempFile(t, filepath.Join(procRoot, "net"), "tcp", procNetTCPHeader+tcp)
	writeTempFile(t, filepath.Join(procRoot, "net"), "tcp6", procNetTCP6Header+tcp6)
	return procRoot
}

func TestProcNetTCPOwner_AC6(t *testing.T) {
	v4Client, v4Server := netip.MustParseAddrPort("127.0.0.1:54321"), netip.MustParseAddrPort("127.0.0.1:8080")
	mappedClient, mappedServer := netip.MustParseAddrPort("[::ffff:127.0.0.1]:54321"), netip.MustParseAddrPort("[::ffff:127.0.0.1]:8080")
	v6Client, v6Server := netip.MustParseAddrPort("[::1]:54321"), netip.MustParseAddrPort("[::1]:8080")
	tests := []struct {
		name          string
		tcp, tcp6     string
		local, remote netip.AddrPort
		wantUID       uint32
		wantErr       bool
	}{
		{"AC6 IPv4 server side", procNetTCPListen + procNetTCPClient + procNetTCPServer, "", v4Client, v4Server, 102, false},
		{"AC6 tcp6 mapped entry for an IPv4 connection", procNetTCPClient, procNetTCP6Listen + procNetTCP6Mapped, v4Client, v4Server, 102, false},
		{"AC6 tcp6 mapped entry for mapped addresses", "", procNetTCP6Mapped, mappedClient, mappedServer, 102, false},
		{"AC6 ::1 entry", "", procNetTCP6Listen + procNetTCP6Loopback, v6Client, v6Server, 102, false},
		{"AC6 malformed line skipped", procNetTCPGarbage + procNetTCPServer, "", v4Client, v4Server, 102, false},
		{"AC6 malformed uid skipped", procNetTCPBadUID, "", v4Client, v4Server, 0, true},
		{"AC6 malformed uid skipped for a later entry", procNetTCPBadUID + procNetTCPServer, "", v4Client, v4Server, 102, false},
		{"AC6 line longer than the scanner buffer", strings.Repeat("x", 70<<10) + "\n" + procNetTCPServer, "", v4Client, v4Server, 0, true},
		{"AC6 LISTEN entry on the same port ignored", procNetTCPListen, procNetTCP6Listen, v4Client, v4Server, 0, true},
		{"AC6 client side only", procNetTCPClient, "", v4Client, v4Server, 0, true},
		{"AC6 entry not established", procNetTCPTimeWait, "", v4Client, v4Server, 0, true},
		{"AC6 no entry", procNetTCPServer, procNetTCP6Mapped, v4Client, netip.MustParseAddrPort("127.0.0.1:9090"), 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner := ProcNetTCPOwner(writeProcNet(t, tc.tcp, tc.tcp6))

			uid, err := owner(tc.local, tc.remote)

			if (err != nil) != tc.wantErr || uid != tc.wantUID {
				t.Fatalf("owner = %d, %v, want %d, error %v", uid, err, tc.wantUID, tc.wantErr)
			}
		})
	}
}

// peerOwnerRecorder is a PeerOwnerFunc that records the remote address of
// every call and answers with uid and err.
type peerOwnerRecorder struct {
	mu      sync.Mutex
	remotes []netip.AddrPort
	uid     uint32
	err     error
}

func (p *peerOwnerRecorder) owner(_, remote netip.AddrPort) (uint32, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remotes = append(p.remotes, remote)
	return p.uid, p.err
}

func (p *peerOwnerRecorder) calls() []netip.AddrPort {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.remotes)
}

type peerOwnerCase struct {
	name         string
	paths        []string
	token        string
	peer         *peerOwnerRecorder
	defaultPeer  bool
	wantRequests int
	wantWarn     string
}

// runPeerOwnerCase polls once and returns the server, the recorder and the log.
func runPeerOwnerCase(t *testing.T, tc peerOwnerCase) (*ohServer, string) {
	t.Helper()
	srv := newOHServer(t)
	opts := []func(*OpenHABConfig){withToken(tc.token)}
	if !tc.defaultPeer {
		opts = append(opts, func(c *OpenHABConfig) { c.PeerOwner = tc.peer.owner })
	}
	c, logBuf := newOHCollector(t, srv, newOHRoot(t, tc.paths...), 1<<30, opts...)
	pollOnce(c)
	return srv, logBuf.String()
}

// checkPeerRemotes asserts every PeerOwner call named the server's address.
func checkPeerRemotes(t *testing.T, peer *peerOwnerRecorder, server string) {
	t.Helper()
	want := netip.MustParseAddrPort(server)
	for _, got := range peer.calls() {
		if got.Addr().Unmap() != want.Addr().Unmap() || got.Port() != want.Port() {
			t.Fatalf("PeerOwner remote = %v, want %v", got, want)
		}
	}
}

func TestOpenHABCollector_Poll_PeerOwner_AC6(t *testing.T) {
	self := uint32(os.Getuid())
	withDir := []string{"etc/openhab", "var/lib/openhab"}
	tests := []peerOwnerCase{
		{name: "AC6 owner of var/lib/openhab gets the token", paths: withDir, token: "tok",
			peer: &peerOwnerRecorder{uid: self}, wantRequests: len(ohRESTPaths)},
		{name: "AC6 E16 other owner gets no request", paths: withDir, token: "tok",
			peer: &peerOwnerRecorder{uid: self + 1}, wantWarn: "does not run as the owner of /var/lib/openhab"},
		{name: "AC6 E16 lookup error gets no request", paths: withDir, token: "tok",
			peer: &peerOwnerRecorder{err: errors.New("peer lookup sentinel failure")}, wantWarn: "peer lookup sentinel failure"},
		{name: "AC6 E16 missing var/lib/openhab gets no request", paths: []string{"etc/openhab"}, token: "tok",
			peer: &peerOwnerRecorder{uid: self}, wantWarn: "/var/lib/openhab"},
		{name: "AC6 E16 default PeerOwner without a matching entry gets no request", paths: withDir, token: "tok",
			defaultPeer: true, wantWarn: `msg="openhab root"`},
		{name: "AC6 without token PeerOwner not asked", paths: withDir,
			peer: &peerOwnerRecorder{uid: self + 1}, wantRequests: len(ohRESTPaths)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, out := runPeerOwnerCase(t, tc)

			if got := len(srv.requests()); got != tc.wantRequests {
				t.Fatalf("requests = %d, want %d", got, tc.wantRequests)
			}
			if tc.wantWarn != "" && countLogLines(out, "WARN", tc.wantWarn) == 0 {
				t.Fatalf("log = %q, want a warning with %q", out, tc.wantWarn)
			}
			checkPeerOwnerAuth(t, tc, srv)
		})
	}
}

// checkPeerOwnerAuth asserts the Authorization header of every request and
// whether PeerOwner was asked: with a token it is asked and the token is
// sent, without one neither happens.
func checkPeerOwnerAuth(t *testing.T, tc peerOwnerCase, srv *ohServer) {
	t.Helper()
	wantAuth := ""
	if tc.token != "" {
		wantAuth = "Bearer " + tc.token
	}
	for _, a := range srv.authorizations() {
		if a != wantAuth {
			t.Fatalf("Authorization = %q, want %q", a, wantAuth)
		}
	}
	if tc.peer == nil {
		return
	}
	calls := len(tc.peer.calls())
	if tc.token == "" && calls != 0 {
		t.Fatalf("PeerOwner asked %d times without a token, want never", calls)
	}
	if tc.token != "" && tc.wantRequests > 0 && calls == 0 {
		t.Fatal("token sent without asking PeerOwner")
	}
	checkPeerRemotes(t, tc.peer, srv.Listener.Addr().String())
}

func TestOpenHABCollector_Poll_PeerOwnerMismatchNamesServer_AC6(t *testing.T) {
	srv := newOHServer(t)
	peer := &peerOwnerRecorder{uid: uint32(os.Getuid()) + 1}
	c, logBuf := newOHCollector(t, srv, newOHRoot(t, "var/lib/openhab"), 1<<30,
		withToken("tok"), func(c *OpenHABConfig) { c.PeerOwner = peer.owner })

	pollOnce(c)

	want := "openHAB server on " + srv.Listener.Addr().String() + " does not run as the owner of /var/lib/openhab"
	if !strings.Contains(logBuf.String(), want) {
		t.Fatalf("log = %q, want %q", logBuf.String(), want)
	}
}

func TestOpenHABCollector_Logging_RefusedWithToken_AC10(t *testing.T) {
	srv := newOHServer(t)
	srv.route("/rest/systeminfo", ohJSON(http.StatusUnauthorized, ohUnauthorizedBody))
	c, logBuf := newOHCollector(t, srv, newOHRoot(t, "var/lib/openhab"), 1<<30, withToken("tok"), allowPeerOwner)

	pollTimes(c, 3)

	out := logBuf.String()
	checkLogCount(t, out, "WARN", `msg="openhab systeminfo"`, 1)
	checkLogCount(t, out, "INFO", `msg="openhab systeminfo"`, 0)
}

func TestParseProcNetAddr_AC6(t *testing.T) {
	tests := []struct {
		name   string
		text   string
		want   netip.AddrPort
		wantOK bool
	}{
		{"AC6 IPv4 entry", "0100007F:1F90", netip.MustParseAddrPort("127.0.0.1:8080"), true},
		{"AC6 IPv6 entry", "00000000000000000000000001000000:1F90", netip.MustParseAddrPort("[::1]:8080"), true},
		{"AC6 no port separator", "0100007F", netip.AddrPort{}, false},
		{"AC6 port not hex", "0100007F:ZZZZ", netip.AddrPort{}, false},
		{"AC6 port above 16 bits", "0100007F:10000", netip.AddrPort{}, false},
		{"AC6 address not hex", "0100007G:1F90", netip.AddrPort{}, false},
		{"AC6 address of 6 bytes", "0100007F0000:1F90", netip.AddrPort{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseProcNetAddr(tc.text)

			if ok != tc.wantOK || got != tc.want {
				t.Fatalf("parseProcNetAddr(%q) = %v, %v, want %v, %v", tc.text, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
