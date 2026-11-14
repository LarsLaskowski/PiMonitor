package collector

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
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

// Limits and fixed names of the openHAB poll.
const (
	// openHABDefaultTimeout bounds each REST request when the config has none.
	openHABDefaultTimeout = 5 * time.Second
	// openHABMaxBody is the most decoded bytes read from one response.
	openHABMaxBody = 8 << 20
	// openHABMaxCmdline is the most bytes read from a /proc/<pid>/cmdline.
	openHABMaxCmdline = 64 << 10
	// openHABMaxListedThings is the most not-ONLINE Things listed.
	openHABMaxListedThings = 10
	// openHABHomeFlag starts the command-line argument that marks the JVM.
	openHABHomeFlag = "-Dopenhab.home="
	// openHABDataDir is the directory whose owner runs openHAB.
	openHABDataDir = "/var/lib/openhab"
)

// openHABInstallPaths are the standard installation paths, relative to Root.
var openHABInstallPaths = []string{"usr/share/openhab", "etc/openhab", openHABDataDir}

// openHABThingStatuses are the Thing statuses counted in by_status.
var openHABThingStatuses = []string{"UNINITIALIZED", "INITIALIZING", "UNKNOWN", "ONLINE", "OFFLINE", "REMOVING", "REMOVED"}

// errNoOpenHABProcess is the failure of the process source.
var errNoOpenHABProcess = errors.New("no openHAB process found")

// openHABStatusError is a REST answer other than 200.
type openHABStatusError struct{ code int }

func (e *openHABStatusError) Error() string {
	if e.refused() {
		return "refused with status " + strconv.Itoa(e.code)
	}
	return "unexpected status " + strconv.Itoa(e.code)
}

// refused reports whether openHAB wants a (better) API token.
func (e *openHABStatusError) refused() bool {
	return e.code == http.StatusUnauthorized || e.code == http.StatusForbidden
}

// openHABProcessInfo is what the process source reads of the JVM.
type openHABProcessInfo struct {
	pid        int
	rssBytes   uint64
	startTicks uint64
}

// openHABSources holds one failure log per source.
type openHABSources struct {
	process, root, systemInfo, things, items, addons failureLog
}

// OpenHABCollector polls a local openHAB installation for its status.
type OpenHABCollector struct {
	cfg      OpenHABConfig
	memTotal func() uint64
	log      *slog.Logger
	sources  openHABSources

	mu     sync.RWMutex
	polled bool
	status OpenHABStatus
}

// NewOpenHABCollector stores its arguments; memTotal returns the total RAM in
// bytes (0 when unknown) and log may be nil.
func NewOpenHABCollector(cfg OpenHABConfig, memTotal func() uint64, log *slog.Logger) *OpenHABCollector {
	return &OpenHABCollector{cfg: cfg, memTotal: memTotal, log: log}
}

// Run polls once at once and then every Interval until ctx is done.
func (c *OpenHABCollector) Run(ctx context.Context) {
	c.Poll(ctx)
	interval := c.cfg.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.Poll(ctx)
		}
	}
}

// Poll performs one detection and REST round and stores the result.
func (c *OpenHABCollector) Poll(ctx context.Context) {
	status := c.poll(ctx)
	c.mu.Lock()
	c.status = status
	c.polled = true
	c.mu.Unlock()
}

// Status returns the latest stored status.
func (c *OpenHABCollector) Status() OpenHABStatus {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.polled {
		status := c.baseStatus()
		status.State = OpenHABStatePending
		return status
	}
	return c.status
}

// baseStatus holds the values every state carries.
func (c *OpenHABCollector) baseStatus() OpenHABStatus {
	return OpenHABStatus{
		PollIntervalSeconds: int(c.cfg.Interval / time.Second),
		APITokenConfigured:  c.cfg.Token != "",
	}
}

func (c *OpenHABCollector) logger() *slog.Logger {
	if c.log == nil {
		return slog.Default()
	}
	return c.log
}

func (c *OpenHABCollector) now() time.Time {
	if c.cfg.Now == nil {
		return time.Now()
	}
	return c.cfg.Now()
}

func (c *OpenHABCollector) root() string {
	if c.cfg.Root == "" {
		return "/"
	}
	return c.cfg.Root
}

// poll runs one detection and REST round and returns the resulting status.
func (c *OpenHABCollector) poll(ctx context.Context) OpenHABStatus {
	status := c.baseStatus()
	proc, procErr := c.findProcess()
	if procErr != nil && !c.cfg.Always && !c.installed() {
		status.State = OpenHABStateNotInstalled
		return status
	}
	c.sources.process.observeAt(c.logger(), "openhab process", procErr, slog.LevelWarn)

	var procUptime *uint64
	if procErr == nil {
		status.Process = c.processStatus(proc)
		procUptime = c.processUptime(proc)
	}
	rootOK := c.pollREST(ctx, &status)
	switch {
	case rootOK:
		status.State = OpenHABStateOK
	case procErr == nil:
		status.State = OpenHABStateUnreachable
	default:
		status.State = OpenHABStateNotRunning
	}
	if procUptime != nil {
		status.UptimeSeconds = procUptime
	}
	collectedAt := c.now()
	status.CollectedAt = &collectedAt
	return status
}

// installed reports whether one of openHAB's standard paths exists.
func (c *OpenHABCollector) installed() bool {
	for _, p := range openHABInstallPaths {
		if _, err := os.Stat(filepath.Join(c.root(), p)); err == nil {
			return true
		}
	}
	return false
}

// findProcess returns the openHAB JVM with the lowest PID whose stat and
// status files are readable.
func (c *OpenHABCollector) findProcess() (openHABProcessInfo, error) {
	procDir := filepath.Join(c.root(), "proc")
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return openHABProcessInfo{}, fmt.Errorf("list %s: %w", procDir, err)
	}
	var pids []int
	for _, e := range entries {
		if pid, convErr := strconv.Atoi(e.Name()); convErr == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	sort.Ints(pids)
	for _, pid := range pids {
		dir := filepath.Join(procDir, strconv.Itoa(pid))
		if !isOpenHABCmdline(readLimited(filepath.Join(dir, "cmdline"), openHABMaxCmdline)) {
			continue
		}
		info, ok := readOpenHABProcess(dir)
		if ok {
			info.pid = pid
			return info, nil
		}
	}
	return openHABProcessInfo{}, errNoOpenHABProcess
}

// readLimited returns at most limit bytes of the file at path, nil when it
// cannot be read.
func readLimited(path string, limit int64) []byte {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil
	}
	return data
}

// isOpenHABCmdline reports whether a NUL-separated command line is the
// openHAB JVM: a java binary with an -Dopenhab.home= argument.
func isOpenHABCmdline(data []byte) bool {
	args := strings.Split(string(data), "\x00")
	if len(args) < 2 || filepath.Base(args[0]) != "java" {
		return false
	}
	for _, arg := range args[1:] {
		if strings.HasPrefix(arg, openHABHomeFlag) {
			return true
		}
	}
	return false
}

// readOpenHABProcess reads RSS and start time of the process in dir; ok is
// false when a file vanished or is malformed.
func readOpenHABProcess(dir string) (info openHABProcessInfo, ok bool) {
	statData, err := os.ReadFile(filepath.Join(dir, "stat"))
	if err != nil {
		return info, false
	}
	stat, err := parseProcPidStat(string(statData))
	if err != nil {
		return info, false
	}
	statusData, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return info, false
	}
	rss, err := parseProcPidStatusRSS(string(statusData))
	if err != nil {
		return info, false
	}
	return openHABProcessInfo{rssBytes: rss, startTicks: stat.startTimeTicks}, true
}

// processStatus converts the process reading, adding the percent of RAM.
func (c *OpenHABCollector) processStatus(info openHABProcessInfo) *OpenHABProcess {
	proc := &OpenHABProcess{PID: info.pid, RSSBytes: info.rssBytes}
	if c.memTotal != nil {
		if total := c.memTotal(); total > 0 {
			percent := 100 * float64(info.rssBytes) / float64(total)
			proc.RSSPercentOfRAM = &percent
		}
	}
	return proc
}

// processUptime is the seconds since the process started, nil when
// /proc/uptime is unreadable.
func (c *OpenHABCollector) processUptime(info openHABProcessInfo) *uint64 {
	data, err := os.ReadFile(filepath.Join(c.root(), "proc", "uptime"))
	if err != nil {
		return nil
	}
	boot, err := parseUptime(string(data))
	if err != nil {
		return nil
	}
	seconds := math.Floor(boot - float64(info.startTicks)/processClockTicksPerSecond)
	uptime := uint64(0)
	if seconds > 0 {
		uptime = uint64(seconds)
	}
	return &uptime
}

// openHABRootResponse is the part of GET /rest/ that identifies openHAB.
type openHABRootResponse struct {
	RuntimeInfo struct {
		Version string `json:"version"`
	} `json:"runtimeInfo"`
}

// openHABSystemInfoResponse is the part of GET /rest/systeminfo read.
type openHABSystemInfoResponse struct {
	SystemInfo *struct {
		FreeMemory  *uint64  `json:"freeMemory"`
		TotalMemory *uint64  `json:"totalMemory"`
		Uptime      *float64 `json:"uptime"`
		StartLevel  *int     `json:"startLevel"`
	} `json:"systemInfo"`
}

// openHABThingResponse is one entry of GET /rest/things?summary=true.
type openHABThingResponse struct {
	UID        string `json:"UID"`
	Label      string `json:"label"`
	StatusInfo struct {
		Status       string `json:"status"`
		StatusDetail string `json:"statusDetail"`
		Description  string `json:"description"`
	} `json:"statusInfo"`
}

// pollREST makes the REST calls in order and fills status; it reports
// whether the root call answered as openHAB. The other calls are skipped
// when it did not.
func (c *OpenHABCollector) pollREST(ctx context.Context, status *OpenHABStatus) bool {
	client := c.newClient()
	defer client.CloseIdleConnections()

	var root openHABRootResponse
	err := c.get(ctx, client, "/rest/", &root)
	if err == nil && root.RuntimeInfo.Version == "" {
		err = errors.New("response is not an openHAB root response")
	}
	c.record(&c.sources.root, "root", err, status)
	if err != nil {
		return false
	}
	status.Version = root.RuntimeInfo.Version

	var info openHABSystemInfoResponse
	err = c.get(ctx, client, "/rest/systeminfo", &info)
	if err == nil && info.SystemInfo == nil {
		err = errors.New("response has no systemInfo")
	}
	if c.record(&c.sources.systemInfo, "systeminfo", err, status) {
		applySystemInfo(status, info)
	}

	var things []openHABThingResponse
	err = c.get(ctx, client, "/rest/things?summary=true", &things)
	if c.record(&c.sources.things, "things", err, status) {
		status.Things = summarizeThings(things)
	}

	var items []struct{}
	err = c.get(ctx, client, "/rest/items?fields=name", &items)
	if c.record(&c.sources.items, "items", err, status) {
		count := len(items)
		status.ItemsCount = &count
	}

	var addons []struct {
		Installed bool `json:"installed"`
	}
	err = c.get(ctx, client, "/rest/addons", &addons)
	if c.record(&c.sources.addons, "addons", err, status) {
		count := 0
		for _, a := range addons {
			if a.Installed {
				count++
			}
		}
		status.AddonsInstalled = &count
	}
	return true
}

// applySystemInfo copies the values systeminfo reports; each is optional.
func applySystemInfo(status *OpenHABStatus, info openHABSystemInfoResponse) {
	si := info.SystemInfo
	status.StartLevel = si.StartLevel
	if si.Uptime != nil && *si.Uptime >= 0 {
		uptime := uint64(*si.Uptime)
		status.UptimeSeconds = &uptime
	}
	if si.FreeMemory != nil && si.TotalMemory != nil && *si.FreeMemory <= *si.TotalMemory {
		status.JVM = &OpenHABJVM{HeapUsedBytes: *si.TotalMemory - *si.FreeMemory, HeapTotalBytes: *si.TotalMemory}
	}
}

// summarizeThings counts the Things and lists up to ten that are neither
// ONLINE nor disabled, sorted by UID.
func summarizeThings(things []openHABThingResponse) *OpenHABThings {
	out := &OpenHABThings{Total: len(things), ByStatus: map[string]int{}, NotOnline: []OpenHABThing{}}
	var listed []OpenHABThing
	for _, th := range things {
		info := th.StatusInfo
		if slices.Contains(openHABThingStatuses, info.Status) {
			out.ByStatus[info.Status]++
		}
		disabled := info.Status == "UNINITIALIZED" && info.StatusDetail == "DISABLED"
		if info.Status == "ONLINE" || disabled {
			continue
		}
		listed = append(listed, OpenHABThing{UID: th.UID, Label: th.Label, Status: info.Status,
			StatusDetail: info.StatusDetail, Description: info.Description})
	}
	slices.SortFunc(listed, func(a, b OpenHABThing) int { return strings.Compare(a.UID, b.UID) })
	out.NotOnline = append(out.NotOnline, listed[:min(len(listed), openHABMaxListedThings)]...)
	return out
}

// record logs the outcome of one source, adds it to the refused list when
// openHAB refused it, and reports whether the call succeeded.
func (c *OpenHABCollector) record(src *failureLog, name string, err error, status *OpenHABStatus) bool {
	level := slog.LevelWarn
	var statusErr *openHABStatusError
	if errors.As(err, &statusErr) && statusErr.refused() {
		status.Refused = append(status.Refused, name)
		if c.cfg.Token == "" {
			level = slog.LevelInfo
		}
	}
	src.observeAt(c.logger(), "openhab "+name, err, level)
	return err == nil
}

// get requests path from the base URL and decodes the JSON answer into out.
// The error carries the status or the transport error, never a body.
func (c *OpenHABCollector) get(ctx context.Context, client *http.Client, path string, out any) error {
	timeout := c.cfg.Timeout
	if timeout <= 0 {
		timeout = openHABDefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.cfg.BaseURL, "/")+path, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return &openHABStatusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, openHABMaxBody+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(body) > openHABMaxBody {
		return fmt.Errorf("response is larger than %d bytes", openHABMaxBody)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// newClient builds an HTTP client that uses no proxy, follows no redirect,
// connects to loopback addresses only and, while a token is configured,
// only to a server owned by the owner of the openHAB data directory.
func (c *OpenHABCollector) newClient() *http.Client {
	dialer := &net.Dialer{Control: requireLoopback}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := dialer.DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			if c.cfg.Token != "" {
				if err := c.checkServerOwner(conn); err != nil {
					_ = conn.Close()
					return nil, err
				}
			}
			return conn, nil
		},
	}
	return &http.Client{
		Transport:     transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

// requireLoopback is a net.Dialer.Control that refuses any address that is
// not a loopback IP.
func requireLoopback(_, address string, _ syscall.RawConn) error {
	addr, err := netip.ParseAddrPort(address)
	if err != nil || !addr.Addr().Unmap().IsLoopback() {
		return fmt.Errorf("address %s is not a loopback address", address)
	}
	return nil
}

// checkServerOwner verifies that the server side of conn runs as the owner
// of the openHAB data directory; nothing has been written to conn yet.
func (c *OpenHABCollector) checkServerOwner(conn net.Conn) error {
	want, err := pathOwner(filepath.Join(c.root(), openHABDataDir))
	if err != nil {
		return fmt.Errorf("owner of %s: %w", openHABDataDir, err)
	}
	local, err := netip.ParseAddrPort(conn.LocalAddr().String())
	if err != nil {
		return fmt.Errorf("local address of the openHAB connection: %w", err)
	}
	remote, err := netip.ParseAddrPort(conn.RemoteAddr().String())
	if err != nil {
		return fmt.Errorf("remote address of the openHAB connection: %w", err)
	}
	peerOwner := c.cfg.PeerOwner
	if peerOwner == nil {
		peerOwner = ProcNetTCPOwner(filepath.Join(c.root(), "proc"))
	}
	got, err := peerOwner(local, remote)
	if err != nil {
		return fmt.Errorf("owner of the openHAB server %s: %w", remote, err)
	}
	if got != want {
		return fmt.Errorf("openHAB server on %s does not run as the owner of %s", remote, openHABDataDir)
	}
	return nil
}
