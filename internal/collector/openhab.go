package collector

import (
	"context"
	"log/slog"
	"net/netip"
	"time"
)

// States of the openHAB status; OpenHABStatus.State holds exactly one.
const (
	// OpenHABStatePending means no poll has finished yet.
	OpenHABStatePending = "pending"
	// OpenHABStateNotInstalled means neither an install path nor a process was found.
	OpenHABStateNotInstalled = "not_installed"
	// OpenHABStateNotRunning means openHAB is installed but its process is absent.
	OpenHABStateNotRunning = "installed_not_running"
	// OpenHABStateUnreachable means the process runs but the REST root does not answer.
	OpenHABStateUnreachable = "running_unreachable"
	// OpenHABStateOK means the REST root answered.
	OpenHABStateOK = "ok"
)

// PeerOwnerFunc returns the uid that owns the server side of the loopback
// connection from remote to local, as the kernel lists it.
type PeerOwnerFunc func(local, remote netip.AddrPort) (uint32, error)

// OpenHABConfig configures an OpenHABCollector.
type OpenHABConfig struct {
	// Enabled toggles the collector entirely.
	Enabled bool
	// Always reports a state other than not_installed even when nothing is
	// detected (openhab.enabled true).
	Always bool
	// BaseURL is the openHAB root URL; it must be loopback http.
	BaseURL string
	// Token is the optional API token; it is never logged.
	Token string
	// Interval is the time between polls.
	Interval time.Duration
	// Root is the file system root for the install paths and /proc; empty means "/".
	Root string
	// Timeout bounds each REST request; zero means 5 s.
	Timeout time.Duration
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
	// PeerOwner resolves the owner of the connection's server side; nil means
	// ProcNetTCPOwner of Root plus /proc.
	PeerOwner PeerOwnerFunc
}

// OpenHABStatus is the status served by GET /api/v1/openhab.
type OpenHABStatus struct {
	State               string          `json:"state"`
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	APITokenConfigured  bool            `json:"api_token_configured"`
	CollectedAt         *time.Time      `json:"collected_at,omitempty"`
	Version             string          `json:"version,omitempty"`
	StartLevel          *int            `json:"start_level,omitempty"`
	UptimeSeconds       *uint64         `json:"uptime_seconds,omitempty"`
	Process             *OpenHABProcess `json:"process,omitempty"`
	JVM                 *OpenHABJVM     `json:"jvm,omitempty"`
	Things              *OpenHABThings  `json:"things,omitempty"`
	ItemsCount          *int            `json:"items_count,omitempty"`
	AddonsInstalled     *int            `json:"addons_installed,omitempty"`
	Refused             []string        `json:"refused,omitempty"`
}

// OpenHABProcess describes the detected openHAB JVM process.
type OpenHABProcess struct {
	PID             int      `json:"pid"`
	RSSBytes        uint64   `json:"rss_bytes"`
	RSSPercentOfRAM *float64 `json:"rss_percent_of_ram,omitempty"`
}

// OpenHABJVM holds the JVM heap figures reported by openHAB.
type OpenHABJVM struct {
	HeapUsedBytes  uint64 `json:"heap_used_bytes"`
	HeapTotalBytes uint64 `json:"heap_total_bytes"`
}

// OpenHABThings summarizes the Things: the total, the count per status and
// the Things that are not ONLINE.
type OpenHABThings struct {
	Total     int            `json:"total"`
	ByStatus  map[string]int `json:"by_status"`
	NotOnline []OpenHABThing `json:"not_online"`
}

// OpenHABThing is one Thing that is not ONLINE.
type OpenHABThing struct {
	UID          string `json:"uid"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	StatusDetail string `json:"status_detail"`
	Description  string `json:"description,omitempty"`
}

// OpenHABCollector polls a local openHAB installation for its status.
type OpenHABCollector struct {
	cfg      OpenHABConfig
	memTotal func() uint64
	log      *slog.Logger
}

// NewOpenHABCollector stores its arguments; memTotal returns the total RAM in
// bytes (0 when unknown) and log may be nil.
func NewOpenHABCollector(cfg OpenHABConfig, memTotal func() uint64, log *slog.Logger) *OpenHABCollector {
	return &OpenHABCollector{cfg: cfg, memTotal: memTotal, log: log}
}

// Run polls once at once and then every Interval until ctx is done.
func (c *OpenHABCollector) Run(ctx context.Context) {}

// Poll performs one detection and REST round and stores the result.
func (c *OpenHABCollector) Poll(ctx context.Context) {}

// Status returns the latest stored status.
func (c *OpenHABCollector) Status() OpenHABStatus {
	return OpenHABStatus{}
}
