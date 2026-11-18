package collector

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/larslaskowski/pimonitor/internal/alert"
)

// Anonymized excerpts of openHAB 5.2.4 responses (docs/decisions/0015).
const (
	ohRootBody = `{"version":"8","locale":"en_GB","measurementSystem":"SI","timezone":"UTC",` +
		`"runtimeInfo":{"version":"5.2.4","buildString":"Release Build"},` +
		`"links":[{"type":"spec","url":"http://127.0.0.1:8080/rest/spec"},{"type":"items","url":"http://127.0.0.1:8080/rest/items"}]}`
	ohSystemInfoBody = `{"systemInfo":{"configFolder":"/etc/openhab","userdataFolder":"/var/lib/openhab",` +
		`"logFolder":"/var/log/openhab","javaVersion":"21.0.13","javaVendor":"Debian","osName":"Linux",` +
		`"osVersion":"6.1.0-test","osArchitecture":"aarch64","availableProcessors":4,` +
		`"freeMemory":65401520,"totalMemory":729808896,"uptime":362335,"startLevel":100}}`
	ohThingsBody = `[{"statusInfo":{"status":"ONLINE","statusDetail":"NONE"},"editable":true,"label":"Heating bridge",` +
		`"UID":"http:url:heating","thingTypeUID":"http:url"},` +
		`{"statusInfo":{"status":"OFFLINE","statusDetail":"NONE","description":"Can't find device"},"editable":true,` +
		`"label":"Lamp: hall!","bridgeUID":"hue:bridge-api2:b1","UID":"hue:device:b1:lamp_2D1",` +
		`"thingTypeUID":"hue:device","semanticEquipmentTag":"Lightbulb"},` +
		`{"statusInfo":{"status":"UNINITIALIZED","statusDetail":"DISABLED"},"editable":true,"label":"Old bridge",` +
		`"UID":"homematic:bridge:old","thingTypeUID":"homematic:bridge"}]`
	ohItemsNamedBody    = `[{"name":"Hall_Light"},{"name":"Hall_Temp"},{"name":"Kitchen_Switch"}]`
	ohItemsNamelessBody = `[{},{},{},{}]`
	ohAddonsBody        = `[{"uid":"binding-alpha","id":"alpha","label":"Alpha Binding","version":"5.2.4","compatible":true,` +
		`"installed":true,"type":"binding","connection":"local","properties":{},"loggerPackages":["org.openhab.binding.alpha"]},` +
		`{"uid":"binding-beta","id":"beta","label":"Beta Binding","version":"5.2.4","compatible":true,` +
		`"installed":false,"type":"binding","connection":"cloud","properties":{},"loggerPackages":[]},` +
		`{"uid":"persistence-gamma","id":"gamma","label":"Gamma Persistence","version":"5.2.4","compatible":true,` +
		`"installed":true,"type":"persistence","connection":"none","properties":{},"loggerPackages":[]}]`
	ohUnauthorizedBody = `{"error":{"message":"Authentication required","http-code":401}}`
)

// ohRESTPaths are the request URIs of one poll, in call order.
var ohRESTPaths = []string{"/rest/", "/rest/systeminfo", "/rest/things?summary=true", "/rest/items?fields=name", "/rest/addons"}

// ohJavaArgv is the anonymized command line of the openHAB JVM.
var ohJavaArgv = []string{
	"/usr/bin/java", "-XX:-UsePerfData", "-Dopenhab.home=/usr/share/openhab", "-Dopenhab.conf=/etc/openhab",
	"-Dopenhab.userdata=/var/lib/openhab", "-Dorg.osgi.service.http.port=8080", "-classpath",
	"/usr/share/openhab/runtime/system/main.jar", "org.apache.karaf.main.Main",
}

var ohNow = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// ohServer stands in for openHAB's REST API: one handler per path, every
// request recorded with its URI and Authorization header.
type ohServer struct {
	*httptest.Server
	mu          sync.Mutex
	routes      map[string]http.HandlerFunc
	uris        []string
	auths       []string
	inFlight    int
	maxInFlight int
}

func newOHServer(t *testing.T) *ohServer {
	t.Helper()
	s := &ohServer{routes: map[string]http.HandlerFunc{
		"/rest/":           ohJSON(http.StatusOK, ohRootBody),
		"/rest/systeminfo": ohJSON(http.StatusOK, ohSystemInfoBody),
		"/rest/things":     ohJSON(http.StatusOK, ohThingsBody),
		"/rest/items":      ohJSON(http.StatusOK, ohItemsNamedBody),
		"/rest/addons":     ohJSON(http.StatusOK, ohAddonsBody),
	}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Close)
	return s
}

func (s *ohServer) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.uris = append(s.uris, r.URL.RequestURI())
	s.auths = append(s.auths, r.Header.Get("Authorization"))
	s.inFlight++
	s.maxInFlight = max(s.maxInFlight, s.inFlight)
	h, ok := s.routes[r.URL.Path]
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
	}()
	if !ok {
		http.NotFound(w, r)
		return
	}
	h(w, r)
}

// route replaces the handler of path.
func (s *ohServer) route(path string, h http.HandlerFunc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.routes[path] = h
}

// failAll makes every REST path answer status.
func (s *ohServer) failAll(status int) {
	for _, p := range []string{"/rest/", "/rest/systeminfo", "/rest/things", "/rest/items", "/rest/addons"} {
		s.route(p, ohJSON(status, `{"error":{"message":"failure"}}`))
	}
}

func (s *ohServer) requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.uris)
}

func (s *ohServer) authorizations() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.auths)
}

func (s *ohServer) peakInFlight() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.maxInFlight
}

// ohJSON answers status with a JSON body.
func ohJSON(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// ohGzipJSON answers 200 with body gzip-encoded.
func ohGzipJSON(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = gz.Write([]byte(body))
		_ = gz.Close()
	}
}

// newOHRoot returns a fake file system root with an empty proc directory,
// a /proc/uptime of 10000.50 s and the given install paths.
func newOHRoot(t *testing.T, installPaths ...string) string {
	t.Helper()
	root := t.TempDir()
	for _, p := range append([]string{"proc"}, installPaths...) {
		if err := os.MkdirAll(filepath.Join(root, p), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", p, err)
		}
	}
	writeTempFile(t, filepath.Join(root, "proc"), "uptime", "10000.50 30000.00\n")
	return root
}

// writeOHProcess writes a fake /proc/<pid> with argv as its cmdline.
func writeOHProcess(t *testing.T, root string, pid int, argv []string, rssKB, startTicks uint64) {
	t.Helper()
	procDir := filepath.Join(root, "proc")
	writeFakeProcess(t, procDir, pid, fakeProcess{comm: filepath.Base(argv[0]), rssKB: rssKB, startTime: startTicks})
	writeTempFile(t, filepath.Join(procDir, strconv.Itoa(pid)), "cmdline", strings.Join(argv, "\x00")+"\x00")
}

// writeOHJava writes the openHAB JVM as pid 938 (RSS 1383952 kB, started
// 1000 s after boot).
func writeOHJava(t *testing.T, root string) {
	t.Helper()
	writeOHProcess(t, root, 938, ohJavaArgv, 1383952, 100000)
}

func withAlways(c *OpenHABConfig) { c.Always = true }

func withToken(token string) func(*OpenHABConfig) {
	return func(c *OpenHABConfig) { c.Token = token }
}

func withTimeout(d time.Duration) func(*OpenHABConfig) {
	return func(c *OpenHABConfig) { c.Timeout = d }
}

func withBaseURL(u string) func(*OpenHABConfig) {
	return func(c *OpenHABConfig) { c.BaseURL = u }
}

// newOHCollector builds an OpenHABCollector against srv and root, with the
// fixed clock ohNow and a capturing debug logger.
func newOHCollector(t *testing.T, srv *ohServer, root string, memTotal uint64, opts ...func(*OpenHABConfig)) (*OpenHABCollector, *bytes.Buffer) {
	t.Helper()
	cfg := OpenHABConfig{
		Enabled:            true,
		BaseURL:            srv.URL,
		Interval:           time.Minute,
		HistoryWindowHours: 24,
		HistoryCapacity:    1440,
		Root:               root,
		Timeout:            2 * time.Second,
		Now:                func() time.Time { return ohNow },
	}
	for _, o := range opts {
		o(&cfg)
	}
	log, buf := newCapturingLogger()
	return NewOpenHABCollector(cfg, func() uint64 { return memTotal }, log), buf
}

func pollOnce(c *OpenHABCollector) OpenHABStatus {
	c.Poll(context.Background())
	return c.Status()
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

func ptr[T any](v T) *T { return &v }

func TestOpenHABCollector_Poll_NotInstalled_AC1(t *testing.T) {
	srv := newOHServer(t)
	c, logBuf := newOHCollector(t, srv, newOHRoot(t), 1<<30)

	c.Poll(context.Background())
	c.Poll(context.Background())

	want := `{"state":"not_installed","poll_interval_seconds":60,"history_window_hours":24,"api_token_configured":false}`
	if got := mustJSON(t, c.Status()); got != want {
		t.Fatalf("status = %s, want %s", got, want)
	}
	if got := srv.requests(); len(got) != 0 {
		t.Fatalf("requests to base_url = %v, want none", got)
	}
	if logBuf.Len() != 0 {
		t.Fatalf("log = %q, want nothing logged", logBuf.String())
	}
}

func TestOpenHABCollector_Status_PendingBeforeFirstPoll_AC2(t *testing.T) {
	srv := newOHServer(t)
	root := newOHRoot(t, "etc/openhab")
	c, _ := newOHCollector(t, srv, root, 1<<30, withToken("tok"), func(cfg *OpenHABConfig) { cfg.Interval = 2 * time.Minute })

	got := mustJSON(t, c.Status())

	want := `{"state":"pending","poll_interval_seconds":120,"history_window_hours":24,"api_token_configured":true}`
	if got != want {
		t.Fatalf("status before the first poll = %s, want %s", got, want)
	}
}

type ohStateCase struct {
	name                       string
	path, proc, rootOK, always bool
	want                       string
}

// pollStateCase polls once in the environment tc describes.
func pollStateCase(t *testing.T, tc ohStateCase) OpenHABStatus {
	t.Helper()
	var paths []string
	if tc.path {
		paths = append(paths, "usr/share/openhab")
	}
	root := newOHRoot(t, paths...)
	if tc.proc {
		writeOHJava(t, root)
	}
	srv := newOHServer(t)
	if !tc.rootOK {
		srv.failAll(http.StatusServiceUnavailable)
	}
	var opts []func(*OpenHABConfig)
	if tc.always {
		opts = append(opts, withAlways)
	}
	c, _ := newOHCollector(t, srv, root, 1<<30, opts...)
	return pollOnce(c)
}

// checkCollectedAt asserts collected_at is the clock's time in every state
// but not_installed, where it is absent.
func checkCollectedAt(t *testing.T, got OpenHABStatus) {
	t.Helper()
	if got.State == OpenHABStateNotInstalled {
		if got.CollectedAt != nil {
			t.Fatalf("collected_at = %v in state not_installed, want absent", *got.CollectedAt)
		}
		return
	}
	if got.CollectedAt == nil || !got.CollectedAt.Equal(ohNow) {
		t.Fatalf("collected_at = %v, want %v", got.CollectedAt, ohNow)
	}
}

func TestOpenHABCollector_Poll_States_AC2(t *testing.T) {
	tests := []ohStateCase{
		{"AC2 auto nothing found root ok", false, false, true, false, OpenHABStateNotInstalled},
		{"AC2 auto nothing found root failing", false, false, false, false, OpenHABStateNotInstalled},
		{"AC2 auto path root ok", true, false, true, false, OpenHABStateOK},
		{"AC2 auto path root failing", true, false, false, false, OpenHABStateNotRunning},
		{"AC2 auto process root ok", false, true, true, false, OpenHABStateOK},
		{"AC2 auto process root failing", false, true, false, false, OpenHABStateUnreachable},
		{"AC2 auto path and process root ok", true, true, true, false, OpenHABStateOK},
		{"AC2 auto path and process root failing", true, true, false, false, OpenHABStateUnreachable},
		{"AC2 true nothing found root ok", false, false, true, true, OpenHABStateOK},
		{"AC2 true nothing found root failing", false, false, false, true, OpenHABStateNotRunning},
		{"AC2 true path root ok", true, false, true, true, OpenHABStateOK},
		{"AC2 true path root failing", true, false, false, true, OpenHABStateNotRunning},
		{"AC2 true process root ok", false, true, true, true, OpenHABStateOK},
		{"AC2 true process root failing", false, true, false, true, OpenHABStateUnreachable},
		{"AC2 true path and process root ok", true, true, true, true, OpenHABStateOK},
		{"AC2 true path and process root failing", true, true, false, true, OpenHABStateUnreachable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := pollStateCase(t, tc)

			if got.State != tc.want {
				t.Fatalf("state = %q, want %q", got.State, tc.want)
			}
			checkCollectedAt(t, got)
		})
	}
}

func TestOpenHABCollector_Poll_RootNotOpenHAB_AC2(t *testing.T) {
	tests := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"AC2 E4 HTML page", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html><body>PiMonitor</body></html>"))
		}},
		{"AC2 E4 empty object", ohJSON(http.StatusOK, `{}`)},
		{"AC2 E4 array", ohJSON(http.StatusOK, `[]`)},
		{"AC2 E4 empty version", ohJSON(http.StatusOK, `{"runtimeInfo":{"version":""}}`)},
		{"AC2 E4 other JSON service", ohJSON(http.StatusOK, `{"status":"ok","version":"1.0.0"}`)},
		{"AC2 E4 not found", ohJSON(http.StatusNotFound, `{}`)},
		{"AC2 E4 server error with openHAB body", ohJSON(http.StatusInternalServerError, ohRootBody)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.route("/rest/", tc.handler)
			root := newOHRoot(t, "etc/openhab")
			writeOHJava(t, root)
			c, _ := newOHCollector(t, srv, root, 1<<30)

			got := pollOnce(c)

			if got.State != OpenHABStateUnreachable || got.Version != "" {
				t.Fatalf("state, version = %q, %q, want %q, empty", got.State, got.Version, OpenHABStateUnreachable)
			}
			if reqs := srv.requests(); !slices.Equal(reqs, []string{"/rest/"}) {
				t.Fatalf("requests = %v, want only the root call", reqs)
			}
		})
	}
}

func TestOpenHABCollector_Poll_ReevaluatesDetection_AC2(t *testing.T) {
	srv := newOHServer(t)
	root := newOHRoot(t, "usr/share/openhab")
	writeOHJava(t, root)
	c, _ := newOHCollector(t, srv, root, 1<<30)
	steps := []struct {
		name   string
		change func()
		want   string
	}{
		{"AC2 installed and running", func() {}, OpenHABStateOK},
		{"AC2 removed", func() {
			mustRemoveAll(t, filepath.Join(root, "usr"))
			mustRemoveAll(t, filepath.Join(root, "proc", "938"))
		}, OpenHABStateNotInstalled},
		{"AC2 installed again, stopped", func() {
			mustMkdirAll(t, filepath.Join(root, "var", "lib", "openhab"))
			srv.failAll(http.StatusServiceUnavailable)
		}, OpenHABStateNotRunning},
		{"AC2 started, not answering", func() { writeOHJava(t, root) }, OpenHABStateUnreachable},
	}
	for _, step := range steps {
		step.change()

		got := pollOnce(c)

		if got.State != step.want {
			t.Fatalf("%s: state = %q, want %q", step.name, got.State, step.want)
		}
	}
	notInstalled := `{"state":"not_installed","poll_interval_seconds":60,"history_window_hours":24,"api_token_configured":false}`
	srv.route("/rest/", ohJSON(http.StatusOK, ohRootBody))
	mustRemoveAll(t, filepath.Join(root, "var"))
	mustRemoveAll(t, filepath.Join(root, "proc", "938"))
	if got := mustJSON(t, pollOnce(c)); got != notInstalled {
		t.Fatalf("status after removal = %s, want %s (no value of an earlier poll)", got, notInstalled)
	}
}

func mustRemoveAll(t *testing.T, path string) {
	t.Helper()
	if err := os.RemoveAll(path); err != nil {
		t.Fatalf("remove %s: %v", path, err)
	}
}

func mustMkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

// writeOHProcessZoo writes two openHAB JVMs (PIDs 938 and 1200) and the
// processes that only mention openHAB (E1).
func writeOHProcessZoo(t *testing.T, root string) {
	t.Helper()
	writeOHProcess(t, root, 1200, ohJavaArgv, 2000000, 200000)
	writeOHJava(t, root)
	writeOHDecoys(t, root)
}

// writeOHDecoys writes processes whose command line mentions openHAB but
// that are not its JVM.
func writeOHDecoys(t *testing.T, root string) {
	t.Helper()
	writeOHProcess(t, root, 500, []string{"/bin/bash", "/usr/share/openhab/runtime/bin/karaf", "server"}, 3000, 1000)
	writeOHProcess(t, root, 600, []string{"/usr/bin/java", "-Dother.home=/opt/other", "-jar", "/opt/other/app.jar"}, 9000, 1000)
	writeOHProcess(t, root, 700, []string{"vim", "/etc/openhab/x.items"}, 5000, 1000)
	writeOHProcess(t, root, 800, []string{"/usr/bin/javac", "-Dopenhab.home=/usr/share/openhab"}, 7000, 1000)
	mustMkdirAll(t, filepath.Join(root, "proc", "self"))
}

// checkProcess compares got with want, the percent within 1e-9.
func checkProcess(t *testing.T, got, want *OpenHABProcess) {
	t.Helper()
	if want == nil || got == nil {
		if got != want {
			t.Fatalf("process = %+v, want %+v", got, want)
		}
		return
	}
	if got.PID != want.PID || got.RSSBytes != want.RSSBytes {
		t.Fatalf("process pid, rss = %d, %d, want %d, %d", got.PID, got.RSSBytes, want.PID, want.RSSBytes)
	}
	if (got.RSSPercentOfRAM == nil) != (want.RSSPercentOfRAM == nil) {
		t.Fatalf("rss_percent_of_ram = %v, want %v", got.RSSPercentOfRAM, want.RSSPercentOfRAM)
	}
	if want.RSSPercentOfRAM != nil && math.Abs(*got.RSSPercentOfRAM-*want.RSSPercentOfRAM) > 1e-9 {
		t.Fatalf("rss_percent_of_ram = %v, want %v", *got.RSSPercentOfRAM, *want.RSSPercentOfRAM)
	}
}

func TestOpenHABCollector_Poll_Process_AC3(t *testing.T) {
	const memTotal = 4 << 30
	const rss938 = 1383952 * 1024
	tests := []struct {
		name      string
		setup     func(t *testing.T, root string)
		memTotal  uint64
		failREST  bool
		wantState string
		wantProc  *OpenHABProcess
	}{
		{"AC3 E2 lowest openHAB java PID wins", writeOHProcessZoo, memTotal, false, OpenHABStateOK,
			&OpenHABProcess{PID: 938, RSSBytes: rss938, RSSPercentOfRAM: ptr(100 * float64(rss938) / float64(memTotal))}},
		{"AC3 E12 memory total 0 omits the percent", writeOHProcessZoo, 0, false, OpenHABStateOK,
			&OpenHABProcess{PID: 938, RSSBytes: rss938}},
		{"AC3 process reported when every REST call fails", writeOHProcessZoo, memTotal, true, OpenHABStateUnreachable,
			&OpenHABProcess{PID: 938, RSSBytes: rss938, RSSPercentOfRAM: ptr(100 * float64(rss938) / float64(memTotal))}},
		{"AC3 E1 processes that only mention openHAB are not openHAB", writeOHDecoys, memTotal, false, OpenHABStateNotInstalled, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := newOHRoot(t)
			tc.setup(t, root)
			srv := newOHServer(t)
			if tc.failREST {
				srv.failAll(http.StatusServiceUnavailable)
			}
			c, _ := newOHCollector(t, srv, root, tc.memTotal)

			got := pollOnce(c)

			if got.State != tc.wantState {
				t.Fatalf("state = %q, want %q", got.State, tc.wantState)
			}
			checkProcess(t, got.Process, tc.wantProc)
		})
	}
}

func TestOpenHABCollector_Poll_ProcessVanished_AC3(t *testing.T) {
	srv := newOHServer(t)
	srv.failAll(http.StatusServiceUnavailable)
	root := newOHRoot(t, "var/lib/openhab")
	writeOHJava(t, root)
	mustRemoveAll(t, filepath.Join(root, "proc", "938", "status"))
	mustRemoveAll(t, filepath.Join(root, "proc", "938", "stat"))
	c, _ := newOHCollector(t, srv, root, 4<<30)

	got := pollOnce(c)

	if got.Process != nil || got.State != OpenHABStateNotRunning {
		t.Fatalf("process, state = %+v, %q, want absent, %q (E3)", got.Process, got.State, OpenHABStateNotRunning)
	}
}

// ohFullStatus is the status of a poll of newOHServer's default answers
// with an install path and no process.
func ohFullStatus() OpenHABStatus {
	return OpenHABStatus{
		State:               OpenHABStateOK,
		PollIntervalSeconds: 60,
		HistoryWindowHours:  24,
		CollectedAt:         ptr(ohNow),
		Version:             "5.2.4",
		StartLevel:          ptr(100),
		UptimeSeconds:       ptr(uint64(362335)),
		JVM:                 &OpenHABJVM{HeapUsedBytes: 729808896 - 65401520, HeapTotalBytes: 729808896},
		Things: &OpenHABThings{
			Total:    3,
			ByStatus: map[string]int{"ONLINE": 1, "OFFLINE": 1, "UNINITIALIZED": 1},
			NotOnline: []OpenHABThing{{
				UID: "hue:device:b1:lamp_2D1", Label: "Lamp: hall!", Status: "OFFLINE", StatusDetail: "NONE",
				Description: "Can't find device",
			}},
		},
		ItemsCount:      ptr(3),
		AddonsInstalled: ptr(2),
	}
}

// ohThingStatuses are openHAB's Thing statuses, the only by_status keys.
var ohThingStatuses = []string{"UNINITIALIZED", "INITIALIZING", "UNKNOWN", "ONLINE", "OFFLINE", "REMOVING", "REMOVED"}

// checkThings compares got with want; a by_status key missing from either
// side counts as 0, and no key outside openHAB's statuses is allowed.
func checkThings(t *testing.T, got, want *OpenHABThings) {
	t.Helper()
	if want == nil || got == nil {
		if got != want {
			t.Fatalf("things = %+v, want %+v", got, want)
		}
		return
	}
	if got.Total != want.Total {
		t.Fatalf("things.total = %d, want %d", got.Total, want.Total)
	}
	for key := range got.ByStatus {
		if !slices.Contains(ohThingStatuses, key) {
			t.Fatalf("things.by_status has key %q, want only openHAB statuses", key)
		}
	}
	for _, s := range ohThingStatuses {
		if got.ByStatus[s] != want.ByStatus[s] {
			t.Fatalf("things.by_status[%s] = %d, want %d (got %v)", s, got.ByStatus[s], want.ByStatus[s], got.ByStatus)
		}
	}
	if gotList, wantList := mustJSONNoT(got.NotOnline), mustJSONNoT(want.NotOnline); gotList != wantList {
		t.Fatalf("things.not_online = %s, want %s", gotList, wantList)
	}
}

func mustJSONNoT(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "marshal error: " + err.Error()
	}
	return string(b)
}

// checkStatus compares got with want: Things through checkThings, the rest
// as JSON.
func checkStatus(t *testing.T, got, want OpenHABStatus) {
	t.Helper()
	gotThings, wantThings := got.Things, want.Things
	got.Things, want.Things = nil, nil
	if g, w := mustJSON(t, got), mustJSON(t, want); g != w {
		t.Fatalf("status = %s\nwant %s", g, w)
	}
	checkThings(t, gotThings, wantThings)
}

func TestOpenHABCollector_Poll_RESTValues_AC4(t *testing.T) {
	tests := []struct {
		name string
		path string
		h    http.HandlerFunc
		want func(*OpenHABStatus)
	}{
		{"AC4 all sources answer", "", nil, func(*OpenHABStatus) {}},
		{"AC4 systeminfo malformed", "/rest/systeminfo", ohJSON(200, `{"systemInfo":`), clearSystemInfo},
		{"AC4 systeminfo wrong shape", "/rest/systeminfo", ohJSON(200, `[]`), clearSystemInfo},
		{"AC4 systeminfo not nested", "/rest/systeminfo", ohJSON(200, `{"startLevel":100,"uptime":5}`), clearSystemInfo},
		{"AC4 systeminfo failing", "/rest/systeminfo", ohJSON(500, `{}`), clearSystemInfo},
		{"AC4 E14 systeminfo without heap fields", "/rest/systeminfo", ohJSON(200, `{"systemInfo":{"startLevel":80,"uptime":5}}`),
			func(s *OpenHABStatus) { s.JVM, s.StartLevel, s.UptimeSeconds = nil, ptr(80), ptr(uint64(5)) }},
		{"AC4 systeminfo free above total", "/rest/systeminfo",
			ohJSON(200, `{"systemInfo":{"freeMemory":900,"totalMemory":800,"uptime":5,"startLevel":100}}`),
			func(s *OpenHABStatus) { s.JVM, s.UptimeSeconds = nil, ptr(uint64(5)) }},
		{"AC4 E14 systeminfo without startLevel", "/rest/systeminfo",
			ohJSON(200, `{"systemInfo":{"freeMemory":100,"totalMemory":800,"uptime":5}}`),
			func(s *OpenHABStatus) {
				s.StartLevel, s.UptimeSeconds = nil, ptr(uint64(5))
				s.JVM = &OpenHABJVM{HeapUsedBytes: 700, HeapTotalBytes: 800}
			}},
		{"AC4 E14 systeminfo without uptime", "/rest/systeminfo",
			ohJSON(200, `{"systemInfo":{"freeMemory":100,"totalMemory":800,"startLevel":100}}`),
			func(s *OpenHABStatus) {
				s.UptimeSeconds = nil
				s.JVM = &OpenHABJVM{HeapUsedBytes: 700, HeapTotalBytes: 800}
			}},
		{"AC4 things malformed", "/rest/things", ohJSON(200, `[{"UID":`), func(s *OpenHABStatus) { s.Things = nil }},
		{"AC4 things wrong shape", "/rest/things", ohJSON(200, `{"UID":"a:b:c"}`), func(s *OpenHABStatus) { s.Things = nil }},
		{"AC4 things failing", "/rest/things", ohJSON(500, `[]`), func(s *OpenHABStatus) { s.Things = nil }},
		{"AC4 items malformed", "/rest/items", ohJSON(200, `[{},`), func(s *OpenHABStatus) { s.ItemsCount = nil }},
		{"AC4 items wrong shape", "/rest/items", ohJSON(200, `{"name":"x"}`), func(s *OpenHABStatus) { s.ItemsCount = nil }},
		{"AC4 E15 nameless items counted", "/rest/items", ohJSON(200, ohItemsNamelessBody), func(s *OpenHABStatus) { s.ItemsCount = ptr(4) }},
		{"AC4 no items", "/rest/items", ohJSON(200, `[]`), func(s *OpenHABStatus) { s.ItemsCount = ptr(0) }},
		{"AC4 addons malformed", "/rest/addons", ohJSON(200, `[{"installed":tru`), func(s *OpenHABStatus) { s.AddonsInstalled = nil }},
		{"AC4 addons wrong shape", "/rest/addons", ohJSON(200, `{"installed":true}`), func(s *OpenHABStatus) { s.AddonsInstalled = nil }},
		{"AC4 no addons installed", "/rest/addons", ohJSON(200, `[{"id":"a","installed":false}]`), func(s *OpenHABStatus) { s.AddonsInstalled = ptr(0) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			if tc.h != nil {
				srv.route(tc.path, tc.h)
			}
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)
			want := ohFullStatus()
			tc.want(&want)

			got := pollOnce(c)

			checkStatus(t, got, want)
		})
	}
}

func clearSystemInfo(s *OpenHABStatus) {
	s.JVM, s.StartLevel, s.UptimeSeconds = nil, nil, nil
}

func TestOpenHABCollector_Poll_Refused_AC4(t *testing.T) {
	unauthorized := ohJSON(http.StatusUnauthorized, ohUnauthorizedBody)
	forbidden := ohJSON(http.StatusForbidden, ohUnauthorizedBody)
	tests := []struct {
		name        string
		routes      map[string]http.HandlerFunc
		wantState   string
		wantRefused []string
		wantJSON    func(*OpenHABStatus)
	}{
		{"AC4 S4 default install without token",
			map[string]http.HandlerFunc{"/rest/systeminfo": unauthorized, "/rest/things": unauthorized,
				"/rest/addons": unauthorized, "/rest/items": ohJSON(200, ohItemsNamelessBody)},
			OpenHABStateOK, []string{"systeminfo", "things", "addons"},
			func(s *OpenHABStatus) {
				clearSystemInfo(s)
				s.Things, s.AddonsInstalled, s.ItemsCount = nil, nil, ptr(4)
				s.Refused = []string{"systeminfo", "things", "addons"}
			}},
		{"AC4 403 and 401 listed in call order",
			map[string]http.HandlerFunc{"/rest/things": unauthorized, "/rest/systeminfo": forbidden},
			OpenHABStateOK, []string{"systeminfo", "things"},
			func(s *OpenHABStatus) {
				clearSystemInfo(s)
				s.Things = nil
				s.Refused = []string{"systeminfo", "things"}
			}},
		{"AC4 refused root",
			map[string]http.HandlerFunc{"/rest/": unauthorized},
			OpenHABStateNotRunning, []string{"root"},
			func(s *OpenHABStatus) {
				*s = OpenHABStatus{State: OpenHABStateNotRunning, PollIntervalSeconds: 60, HistoryWindowHours: 24, CollectedAt: ptr(ohNow), Refused: []string{"root"}}
			}},
		{"AC4 nothing refused", nil, OpenHABStateOK, nil, func(*OpenHABStatus) {}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			for p, h := range tc.routes {
				srv.route(p, h)
			}
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)
			want := ohFullStatus()
			tc.wantJSON(&want)

			got := pollOnce(c)

			if got.State != tc.wantState || !slices.Equal(got.Refused, tc.wantRefused) {
				t.Fatalf("state, refused = %q, %v, want %q, %v", got.State, got.Refused, tc.wantState, tc.wantRefused)
			}
			checkStatus(t, got, want)
		})
	}
}

func TestOpenHABCollector_Poll_Uptime_AC4(t *testing.T) {
	tests := []struct {
		name       string
		process    bool
		systemInfo http.HandlerFunc
		want       *uint64
	}{
		{"AC4 from the process start time", true, ohJSON(200, ohSystemInfoBody), ptr(uint64(9000))},
		{"AC4 from the process when systeminfo is refused", true, ohJSON(401, ohUnauthorizedBody), ptr(uint64(9000))},
		{"AC4 from systeminfo without a process", false, ohJSON(200, ohSystemInfoBody), ptr(uint64(362335))},
		{"AC4 absent without a process and without systeminfo", false, ohJSON(401, ohUnauthorizedBody), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.route("/rest/systeminfo", tc.systemInfo)
			root := newOHRoot(t, "etc/openhab")
			if tc.process {
				writeOHJava(t, root)
			}
			c, _ := newOHCollector(t, srv, root, 1<<30)

			got := pollOnce(c)

			if mustJSON(t, got.UptimeSeconds) != mustJSON(t, tc.want) {
				t.Fatalf("uptime_seconds = %s, want %s", mustJSON(t, got.UptimeSeconds), mustJSON(t, tc.want))
			}
		})
	}
}

// ohThing is the JSON shape of one Thing of GET /rest/things?summary=true.
type ohThing struct {
	StatusInfo struct {
		Status       string `json:"status"`
		StatusDetail string `json:"statusDetail"`
		Description  string `json:"description,omitempty"`
	} `json:"statusInfo"`
	Editable     bool   `json:"editable"`
	Label        string `json:"label"`
	UID          string `json:"UID"`
	ThingTypeUID string `json:"thingTypeUID"`
}

func newOHThing(uid, label, status, detail, description string) ohThing {
	th := ohThing{Editable: true, Label: label, UID: uid, ThingTypeUID: "test:type"}
	th.StatusInfo.Status, th.StatusInfo.StatusDetail, th.StatusInfo.Description = status, detail, description
	return th
}

// ohManyThingsBody holds 12 OFFLINE Things in shuffled UID order, a
// disabled one, an UNKNOWN one, one with a hostile status, one that lacks
// its handler and 3 ONLINE ones.
func ohManyThingsBody(t *testing.T) string {
	t.Helper()
	things := []ohThing{
		newOHThing("a:disabled", "Disabled", "UNINITIALIZED", "DISABLED", ""),
		newOHThing("a:online:1", "On 1", "ONLINE", "NONE", ""),
	}
	for _, n := range []int{7, 3, 11, 1, 12, 5, 9, 2, 10, 4, 8, 6} {
		things = append(things, newOHThing(fmt.Sprintf("zwave:device:c1:node%02d", n), fmt.Sprintf("Node %02d", n),
			"OFFLINE", "COMMUNICATION_ERROR", ""))
	}
	things = append(things,
		newOHThing("zwave:device:c1:node50", "Unknown", "UNKNOWN", "NONE", ""),
		newOHThing("zwave:device:c1:node60", "Hostile", "<script>", "NONE", ""),
		newOHThing("zwave:device:c1:node70", "No handler", "UNINITIALIZED", "HANDLER_MISSING_ERROR", ""),
		newOHThing("a:online:2", "On 2", "ONLINE", "NONE", ""),
		newOHThing("a:online:3", "On 3", "ONLINE", "NONE", ""),
	)
	return mustJSON(t, things)
}

func ohOfflineNodes(from, to int) []OpenHABThing {
	var out []OpenHABThing
	for n := from; n <= to; n++ {
		out = append(out, OpenHABThing{UID: fmt.Sprintf("zwave:device:c1:node%02d", n), Label: fmt.Sprintf("Node %02d", n),
			Status: "OFFLINE", StatusDetail: "COMMUNICATION_ERROR"})
	}
	return out
}

func TestOpenHABCollector_Poll_Things_AC5(t *testing.T) {
	hostileLabel := `<b>x</b> & "q"`
	small := mustJSON(t, []ohThing{
		newOHThing("b:on", "On", "ONLINE", "NONE", ""),
		newOHThing("x:off1", hostileLabel, "OFFLINE", "NONE", "Can't find device"),
		newOHThing("c:off2", "Off 2", "OFFLINE", "COMMUNICATION_ERROR", ""),
		newOHThing("d:unk", "Unknown", "UNKNOWN", "NONE", ""),
		newOHThing("a:dis", "Disabled", "UNINITIALIZED", "DISABLED", ""),
		newOHThing("e:handler", "No handler", "UNINITIALIZED", "HANDLER_MISSING_ERROR", ""),
	})
	tests := []struct {
		name string
		body string
		want *OpenHABThings
	}{
		{"AC5 E9 E10 first 10 by UID, disabled and ONLINE not listed, hostile status not counted", ohManyThingsBody(t),
			&OpenHABThings{Total: 19, ByStatus: map[string]int{"OFFLINE": 12, "UNKNOWN": 1, "UNINITIALIZED": 2, "ONLINE": 3},
				NotOnline: ohOfflineNodes(1, 10)}},
		{"AC5 E8 fields, description only when given, sorted by UID", small,
			&OpenHABThings{Total: 6, ByStatus: map[string]int{"ONLINE": 1, "OFFLINE": 2, "UNKNOWN": 1, "UNINITIALIZED": 2},
				NotOnline: []OpenHABThing{
					{UID: "c:off2", Label: "Off 2", Status: "OFFLINE", StatusDetail: "COMMUNICATION_ERROR"},
					{UID: "d:unk", Label: "Unknown", Status: "UNKNOWN", StatusDetail: "NONE"},
					{UID: "e:handler", Label: "No handler", Status: "UNINITIALIZED", StatusDetail: "HANDLER_MISSING_ERROR"},
					{UID: "x:off1", Label: hostileLabel, Status: "OFFLINE", StatusDetail: "NONE", Description: "Can't find device"},
				}}},
		{"AC5 all ONLINE gives an empty list", mustJSON(t, []ohThing{newOHThing("b:on", "On", "ONLINE", "NONE", "")}),
			&OpenHABThings{Total: 1, ByStatus: map[string]int{"ONLINE": 1}, NotOnline: []OpenHABThing{}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.route("/rest/things", ohJSON(http.StatusOK, tc.body))
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

			got := pollOnce(c)

			checkThings(t, got.Things, tc.want)
		})
	}
}

func TestOpenHABCollector_Poll_Requests_AC6(t *testing.T) {
	tests := []struct {
		name    string
		baseURL func(srvURL string) string
	}{
		{"AC6 base_url without slash", func(u string) string { return u }},
		{"AC6 base_url with trailing slash", func(u string) string { return u + "/" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30, withBaseURL(tc.baseURL(srv.URL)))

			got := pollOnce(c)

			if reqs := srv.requests(); got.State != OpenHABStateOK || !slices.Equal(reqs, ohRESTPaths) {
				t.Fatalf("state, requests = %q, %v, want %q, %v", got.State, reqs, OpenHABStateOK, ohRESTPaths)
			}
			if peak := srv.peakInFlight(); peak != 1 {
				t.Fatalf("concurrent requests = %d, want 1", peak)
			}
			if auths := srv.authorizations(); slices.ContainsFunc(auths, func(a string) bool { return a != "" }) {
				t.Fatalf("Authorization headers = %q, want none without a token", auths)
			}
		})
	}
}

func TestOpenHABCollector_Poll_RedirectNotFollowed_AC6(t *testing.T) {
	var targetHits int
	var mu sync.Mutex
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		targetHits++
		mu.Unlock()
		_, _ = w.Write([]byte(ohThingsBody))
	}))
	t.Cleanup(target.Close)
	srv := newOHServer(t)
	srv.route("/rest/things", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/rest/things", http.StatusFound)
	})
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

	got := pollOnce(c)

	mu.Lock()
	hits := targetHits
	mu.Unlock()
	if hits != 0 || got.Things != nil {
		t.Fatalf("redirect target hits, things = %d, %+v, want 0, absent", hits, got.Things)
	}
	if got.ItemsCount == nil || got.AddonsInstalled == nil || got.JVM == nil {
		t.Fatalf("items, addons, jvm = %v, %v, %v, want the other values present", got.ItemsCount, got.AddonsInstalled, got.JVM)
	}
}

func TestOpenHABCollector_Poll_Timeout_AC6(t *testing.T) {
	srv := newOHServer(t)
	srv.route("/rest/systeminfo", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30, withTimeout(50*time.Millisecond))

	got := pollOnce(c)

	if got.JVM != nil || got.StartLevel != nil {
		t.Fatalf("jvm, start_level = %+v, %v, want absent after the timeout", got.JVM, got.StartLevel)
	}
	if got.Things == nil || got.ItemsCount == nil || got.AddonsInstalled == nil {
		t.Fatalf("things, items, addons = %+v, %v, %v, want the later calls still made", got.Things, got.ItemsCount, got.AddonsInstalled)
	}
}

// ohAddonsOfSize is a JSON array of one installed add-on padded with
// whitespace to exactly n bytes.
func ohAddonsOfSize(n int) string {
	head := `[{"installed":true}`
	return head + strings.Repeat(" ", n-len(head)-1) + "]"
}

func TestOpenHABCollector_Poll_BodyLimit_AC6(t *testing.T) {
	const limit = 8 << 20
	tests := []struct {
		name string
		h    http.HandlerFunc
		want *int
	}{
		{"AC6 exactly 8 MiB accepted", ohJSON(http.StatusOK, ohAddonsOfSize(limit)), ptr(1)},
		{"AC6 8 MiB plus one byte rejected", ohJSON(http.StatusOK, ohAddonsOfSize(limit+1)), nil},
		{"AC6 gzip decoding to exactly 8 MiB accepted", ohGzipJSON(ohAddonsOfSize(limit)), ptr(1)},
		{"AC6 gzip decoding to 8 MiB plus one byte rejected", ohGzipJSON(ohAddonsOfSize(limit + 1)), nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.route("/rest/addons", tc.h)
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

			got := pollOnce(c)

			if mustJSON(t, got.AddonsInstalled) != mustJSON(t, tc.want) || got.ItemsCount == nil {
				t.Fatalf("addons_installed, items_count = %s, %v, want %s, present",
					mustJSON(t, got.AddonsInstalled), got.ItemsCount, mustJSON(t, tc.want))
			}
		})
	}
}

func TestOpenHABCollector_Poll_NonLoopbackDialRefused_AC6(t *testing.T) {
	srv := newOHServer(t)
	c, logBuf := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30,
		withBaseURL("http://192.0.2.1:9"), withTimeout(30*time.Second))
	done := make(chan OpenHABStatus, 1)

	go func() { done <- pollOnce(c) }()

	select {
	case got := <-done:
		if got.State != OpenHABStateNotRunning {
			t.Fatalf("state = %q, want %q", got.State, OpenHABStateNotRunning)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("poll of a non-loopback base_url still running after 10 s, want the dial refused before connecting")
	}
	if countLogLines(logBuf.String(), "WARN", `msg="openhab root"`) != 1 {
		t.Fatalf("log = %q, want one warning for the root call", logBuf.String())
	}
}

func TestOpenHABCollector_TokenNeverLogged_AC8(t *testing.T) {
	const sentinel = "oh.sentinelTOKENvalue.4711"
	tests := []struct {
		name  string
		setup func(srv *ohServer)
		opts  []func(*OpenHABConfig)
	}{
		{"AC8 failing", func(srv *ohServer) { srv.failAll(http.StatusInternalServerError) }, nil},
		{"AC8 refused with token", func(srv *ohServer) { srv.failAll(http.StatusUnauthorized) }, nil},
		{"AC8 timed out", func(srv *ohServer) {
			srv.route("/rest/systeminfo", func(_ http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
		}, []func(*OpenHABConfig){withTimeout(50 * time.Millisecond)}},
		{"AC8 redirected", func(srv *ohServer) {
			srv.route("/rest/things", func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, "http://127.0.0.1:1/elsewhere", http.StatusFound)
			})
		}, nil},
		{"AC8 peer owner mismatch", func(*ohServer) {}, []func(*OpenHABConfig){func(c *OpenHABConfig) {
			c.PeerOwner = func(_, _ netip.AddrPort) (uint32, error) { return uint32(os.Getuid()) + 1, nil }
		}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			tc.setup(srv)
			opts := append([]func(*OpenHABConfig){withToken(sentinel), allowPeerOwner}, tc.opts...)
			c, logBuf := newOHCollector(t, srv, newOHRoot(t, "var/lib/openhab"), 1<<30, opts...)

			c.Poll(context.Background())
			c.Poll(context.Background())

			if !strings.Contains(logBuf.String(), "level=WARN") {
				t.Fatalf("log = %q, want the failing poll to log a warning", logBuf.String())
			}
			if out := logBuf.String() + mustJSON(t, c.Status()); strings.Contains(out, sentinel) {
				t.Fatalf("token found in log or status: %q", out)
			}
		})
	}
}

// allowPeerOwner reports the test's own uid, the owner of the temp
// var/lib/openhab, as the owner of every connection.
func allowPeerOwner(c *OpenHABConfig) {
	c.PeerOwner = func(_, _ netip.AddrPort) (uint32, error) { return uint32(os.Getuid()), nil }
}

func TestOpenHABCollector_Logging_AC10(t *testing.T) {
	t.Run("AC10 failure warns once, repeats at debug, recovers at info", func(t *testing.T) {
		srv := newOHServer(t)
		srv.route("/rest/", ohJSON(http.StatusServiceUnavailable, `{}`))
		c, logBuf := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

		pollTimes(c, 3)
		srv.route("/rest/", ohJSON(http.StatusOK, ohRootBody))
		pollTimes(c, 2)

		out := logBuf.String()
		checkLogCount(t, out, "WARN", `msg="openhab root"`, 1)
		checkLogCount(t, out, "DEBUG", `msg="openhab root"`, 2)
		checkLogCount(t, out, "INFO", `msg="openhab root: recovered"`, 1)
	})
	t.Run("AC10 refused without token logs info once", func(t *testing.T) {
		srv := newOHServer(t)
		srv.route("/rest/systeminfo", ohJSON(http.StatusUnauthorized, ohUnauthorizedBody))
		c, logBuf := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

		pollTimes(c, 3)

		out := logBuf.String()
		checkLogCount(t, out, "INFO", `msg="openhab systeminfo"`, 1)
		checkLogCount(t, out, "WARN", `msg="openhab systeminfo"`, 0)
	})
	t.Run("AC10 installed without process warns once", func(t *testing.T) {
		srv := newOHServer(t)
		c, logBuf := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

		pollTimes(c, 3)

		out := logBuf.String()
		checkLogCount(t, out, "WARN", `msg="openhab process"`, 1)
		checkLogCount(t, out, "WARN", "no openHAB process found", 1)
	})
	t.Run("AC10 not_installed logs nothing", func(t *testing.T) {
		srv := newOHServer(t)
		srv.failAll(http.StatusServiceUnavailable)
		c, logBuf := newOHCollector(t, srv, newOHRoot(t), 1<<30)

		pollTimes(c, 3)

		if logBuf.Len() != 0 {
			t.Fatalf("log = %q, want nothing", logBuf.String())
		}
	})
}

func pollTimes(c *OpenHABCollector, n int) {
	for range n {
		c.Poll(context.Background())
	}
}

func checkLogCount(t *testing.T, out, level, substr string, want int) {
	t.Helper()
	if got := countLogLines(out, level, substr); got != want {
		t.Fatalf("%s lines with %s = %d, want %d; log:\n%s", level, substr, got, want, out)
	}
}

// ohSampleBody returns the body of a `curl -si` capture: everything after
// the first empty line.
func ohSampleBody(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sample: %v", err)
	}
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	_, body, found := strings.Cut(text, "\n\n")
	if !found {
		t.Fatal("sample has no empty line after its headers")
	}
	return body
}

func TestOpenHABCollector_RealSamples_AC4(t *testing.T) {
	path := os.Getenv("PIMONITOR_OPENHAB_ADDONS_SAMPLE")
	if path == "" {
		t.Skip("PIMONITOR_OPENHAB_ADDONS_SAMPLE not set")
	}
	body := ohSampleBody(t, path)
	want := strings.Count(body, `"installed":true`)
	srv := newOHServer(t)
	srv.route("/rest/addons", ohJSON(http.StatusOK, body))
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

	got := pollOnce(c)

	t.Logf("sample: %d bytes, %d installed", len(body), want)
	if got.AddonsInstalled == nil || *got.AddonsInstalled != want {
		t.Fatalf("addons_installed = %v, want %d", got.AddonsInstalled, want)
	}
}

// breakOHJava replaces one file of the openHAB JVM written by writeOHJava.
type breakOHJava func(t *testing.T, pidDir string)

func removeOHFile(name string) breakOHJava {
	return func(t *testing.T, pidDir string) {
		t.Helper()
		mustRemoveAll(t, filepath.Join(pidDir, name))
	}
}

func replaceOHFile(name, content string) breakOHJava {
	return func(t *testing.T, pidDir string) {
		t.Helper()
		writeTempFile(t, pidDir, name, content)
	}
}

func dirAsOHFile(name string) breakOHJava {
	return func(t *testing.T, pidDir string) {
		t.Helper()
		mustRemoveAll(t, filepath.Join(pidDir, name))
		mustMkdirAll(t, filepath.Join(pidDir, name))
	}
}

func TestOpenHABCollector_Poll_UnreadableProcessFiles_AC3(t *testing.T) {
	tests := []struct {
		name   string
		mutate breakOHJava
	}{
		{"AC3 cmdline missing", removeOHFile("cmdline")},
		{"AC3 cmdline unreadable", dirAsOHFile("cmdline")},
		{"AC3 stat malformed", replaceOHFile("stat", "938 java\n")},
		{"AC3 status missing", removeOHFile("status")},
		{"AC3 status VmRSS malformed", replaceOHFile("status", "Name:\tjava\nVmRSS:\tmany kB\n")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.failAll(http.StatusServiceUnavailable)
			root := newOHRoot(t, "var/lib/openhab")
			writeOHJava(t, root)
			tc.mutate(t, filepath.Join(root, "proc", "938"))
			c, _ := newOHCollector(t, srv, root, 4<<30)

			got := pollOnce(c)

			if got.Process != nil || got.State != OpenHABStateNotRunning {
				t.Fatalf("process, state = %+v, %q, want absent, %q", got.Process, got.State, OpenHABStateNotRunning)
			}
		})
	}
}

func TestOpenHABCollector_Poll_NoProcDirectory_AC3(t *testing.T) {
	srv := newOHServer(t)
	srv.failAll(http.StatusServiceUnavailable)
	root := newOHRoot(t, "var/lib/openhab")
	mustRemoveAll(t, filepath.Join(root, "proc"))
	c, buf := newOHCollector(t, srv, root, 4<<30)

	got := pollOnce(c)

	if got.Process != nil || got.State != OpenHABStateNotRunning {
		t.Fatalf("process, state = %+v, %q, want absent, %q", got.Process, got.State, OpenHABStateNotRunning)
	}
	if n := countLogLines(buf.String(), "WARN", "list "+filepath.Join(root, "proc")); n != 1 {
		t.Fatalf("warnings naming the unreadable proc directory = %d, want 1; log:\n%s", n, buf.String())
	}
}

func TestOpenHABCollector_Poll_ProcessUptimeUnreadable_AC4(t *testing.T) {
	tests := []struct {
		name   string
		uptime string // "" removes /proc/uptime
	}{
		{"AC4 /proc/uptime missing", ""},
		{"AC4 /proc/uptime malformed", "soon\n"},
		{"AC5 /proc/uptime non-finite", "inf 30000.00\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			srv.failAll(http.StatusServiceUnavailable)
			root := newOHRoot(t, "var/lib/openhab")
			writeOHJava(t, root)
			if tc.uptime == "" {
				mustRemoveAll(t, filepath.Join(root, "proc", "uptime"))
			} else {
				writeTempFile(t, filepath.Join(root, "proc"), "uptime", tc.uptime)
			}
			c, _ := newOHCollector(t, srv, root, 4<<30)

			got := pollOnce(c)

			if got.State != OpenHABStateUnreachable || got.Process == nil || got.Process.PID != 938 {
				t.Fatalf("state, process = %q, %+v, want %q with pid 938", got.State, got.Process, OpenHABStateUnreachable)
			}
			if got.UptimeSeconds != nil {
				t.Fatalf("uptime_seconds = %d, want absent", *got.UptimeSeconds)
			}
		})
	}
}

func TestOpenHABCollector_Poll_DefaultTimeout_AC6(t *testing.T) {
	srv := newOHServer(t)
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 4<<30, withTimeout(0))

	got := pollOnce(c)

	if got.State != OpenHABStateOK || len(srv.requests()) != len(ohRESTPaths) {
		t.Fatalf("state, requests = %q, %v, want %q, %v with the default timeout", got.State, srv.requests(), OpenHABStateOK, ohRESTPaths)
	}
}

func TestOpenHABCollector_Poll_InvalidBaseURL_AC6(t *testing.T) {
	srv := newOHServer(t)
	c, buf := newOHCollector(t, srv, newOHRoot(t), 4<<30, withAlways, withBaseURL("http://127.0.0.1:8080/\x7f"))

	got := pollOnce(c)

	if got.State != OpenHABStateNotRunning || len(srv.requests()) != 0 {
		t.Fatalf("state, requests = %q, %v, want %q, none", got.State, srv.requests(), OpenHABStateNotRunning)
	}
	if n := countLogLines(buf.String(), "WARN", "build request"); n != 1 {
		t.Fatalf("warnings naming the request build = %d, want 1; log:\n%s", n, buf.String())
	}
}

func TestOpenHABCollector_Poll_TruncatedBody_AC6(t *testing.T) {
	srv := newOHServer(t)
	srv.route("/rest/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Length", strconv.Itoa(len(ohRootBody)))
		_, _ = w.Write([]byte(ohRootBody[:10]))
	})
	c, buf := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 4<<30)

	got := pollOnce(c)

	if got.State != OpenHABStateNotRunning || got.Version != "" {
		t.Fatalf("state, version = %q, %q, want %q, empty", got.State, got.Version, OpenHABStateNotRunning)
	}
	if n := countLogLines(buf.String(), "WARN", "read response"); n != 1 {
		t.Fatalf("warnings naming the truncated read = %d, want 1; log:\n%s", n, buf.String())
	}
}

func TestOpenHABCollector_Run_PollsEveryInterval_AC11(t *testing.T) {
	tests := []struct {
		name      string
		interval  time.Duration
		wantPolls func(n int) bool
		want      string
	}{
		{"AC11 interval of 20 ms polls again", 20 * time.Millisecond, func(n int) bool { return n >= 2 }, "at least 2"},
		{"AC11 non-positive interval falls back to a minute", 0, func(n int) bool { return n == 1 }, "exactly 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			c := NewOpenHABCollector(OpenHABConfig{Enabled: true, BaseURL: srv.URL, Interval: tc.interval,
				Root: newOHRoot(t, "etc/openhab"), Timeout: 2 * time.Second}, nil, nil)

			polls := runOHFor(c, srv, 200*time.Millisecond)

			if !tc.wantPolls(polls) {
				t.Fatalf("polls in 200 ms = %d, want %s", polls, tc.want)
			}
		})
	}
}

// runOHFor runs c for d, waits for Run to return and counts the root calls
// srv received, one per poll.
func runOHFor(c *OpenHABCollector, srv *ohServer, d time.Duration) int {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	c.Run(ctx)
	n := 0
	for _, uri := range srv.requests() {
		if uri == "/rest/" {
			n++
		}
	}
	return n
}

func TestOpenHABCollector_RootDefault_AC1(t *testing.T) {
	c := NewOpenHABCollector(OpenHABConfig{}, nil, nil)

	if got := c.root(); got != "/" {
		t.Fatalf("root() with an empty Root = %q, want %q", got, "/")
	}
}

func TestOpenHABCollector_LoggerDefault_AC10(t *testing.T) {
	c := NewOpenHABCollector(OpenHABConfig{}, nil, nil)

	if got := c.logger(); got != slog.Default() {
		t.Fatalf("logger() with a nil logger = %p, want slog.Default() %p", got, slog.Default())
	}
}

func TestOpenHABThingStatuses(t *testing.T) {
	want := []string{"UNINITIALIZED", "INITIALIZING", "UNKNOWN", "ONLINE", "OFFLINE", "REMOVING", "REMOVED"}
	t.Run("AC4 fixed order", func(t *testing.T) {
		if got := OpenHABThingStatuses(); !slices.Equal(got, want) {
			t.Fatalf("OpenHABThingStatuses() = %v, want %v", got, want)
		}
	})
	t.Run("AC4 returns a copy", func(t *testing.T) {
		got := OpenHABThingStatuses()
		if len(got) == 0 {
			t.Fatal("OpenHABThingStatuses() is empty")
		}
		got[0] = "CHANGED"
		if again := OpenHABThingStatuses(); !slices.Equal(again, want) {
			t.Fatalf("after modifying the result, OpenHABThingStatuses() = %v, want %v", again, want)
		}
	})
}

// Values of the fixture openHAB JVM (writeOHJava) and of the default
// systeminfo answer, as the history series hold them.
const (
	ohJavaRSSBytes     = 1383952 * 1024
	ohHeapUsedBytes    = 729808896 - 65401520
	ohOtherPID         = 2000
	ohThirdPID         = 3000
	ohStartTicks       = 100000
	ohOtherStartTicks  = 200000
	ohChangedStartTick = 300000
)

// ohClock is an OpenHABConfig.Now whose time a test sets.
type ohClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *ohClock) set(at time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.at = at
}

func (c *ohClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.at
}

// ohCallClock is an OpenHABConfig.Now that returns early on its first call
// (the end of the poll) and late on every later one (the hand-over of the
// sample to the history).
type ohCallClock struct {
	mu          sync.Mutex
	calls       int
	early, late time.Time
}

func (c *ohCallClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	if c.calls == 1 {
		return c.early
	}
	return c.late
}

// ohProc is the openHAB process of one poll; the zero value is no process.
type ohProc struct {
	pid        int
	startTicks uint64
}

// setOHProcess makes root hold exactly the process p (none for the zero value).
func setOHProcess(t *testing.T, root string, p ohProc) {
	t.Helper()
	for _, pid := range []int{938, ohOtherPID, ohThirdPID} {
		mustRemoveAll(t, filepath.Join(root, "proc", strconv.Itoa(pid)))
	}
	if p.pid != 0 {
		writeOHProcess(t, root, p.pid, ohJavaArgv, 1383952, p.startTicks)
	}
}

// withOHHistory sets the openHAB history window and capacity of a Config.
func withOHHistory(hours, capacity int) func(*Config) {
	return func(cfg *Config) {
		cfg.OpenHAB.HistoryWindowHours = hours
		cfg.OpenHAB.HistoryCapacity = capacity
	}
}

// newOHHistoryCollector builds a Collector whose openHAB poll runs against
// srv and root with the clock now (nil: the real one), the fixture meminfo
// and a 24 h window of 1,440 points, changed by opts.
func newOHHistoryCollector(t *testing.T, srv *ohServer, root string, now func() time.Time, opts ...func(*Config)) *Collector {
	t.Helper()
	cfg := Config{
		FastInterval:    time.Second,
		SlowInterval:    time.Hour,
		HistoryCapacity: 10,
		OpenHAB: OpenHABConfig{
			Enabled:            true,
			BaseURL:            srv.URL,
			Interval:           time.Minute,
			HistoryWindowHours: 24,
			HistoryCapacity:    1440,
			Root:               root,
			Timeout:            2 * time.Second,
			Now:                now,
		},
	}
	for _, o := range opts {
		o(&cfg)
	}
	c := New(cfg, nil)
	c.memory = &MemoryCollector{path: writeTempFile(t, t.TempDir(), "meminfo", meminfoFixture)}
	return c
}

// ohHistoryPoint is the point a series is expected to hold.
func ohHistoryPoint(at time.Time, v float64) HistoryPoint {
	return HistoryPoint{Timestamp: at, Value: v}
}

func TestOpenHABCollector_Poll_HistorySample(t *testing.T) {
	at := time.Now().Add(-time.Minute)
	tests := []struct {
		name        string
		install     bool
		proc        ohProc
		systemInfo  http.HandlerFunc
		wantRSS     []HistoryPoint
		wantHeap    []HistoryPoint
		wantRestart []HistoryPoint
	}{
		{"AC1 process and heap give both values at collected_at", true, ohProc{938, ohStartTicks}, nil,
			[]HistoryPoint{ohHistoryPoint(at, ohJavaRSSBytes)}, []HistoryPoint{ohHistoryPoint(at, ohHeapUsedBytes)}, nil},
		{"AC1 no process gives no RSS value", true, ohProc{}, nil,
			nil, []HistoryPoint{ohHistoryPoint(at, ohHeapUsedBytes)}, nil},
		{"AC1 refused systeminfo gives no heap value", true, ohProc{938, ohStartTicks},
			ohJSON(http.StatusUnauthorized, ohUnauthorizedBody),
			[]HistoryPoint{ohHistoryPoint(at, ohJavaRSSBytes)}, nil, nil},
		{"AC1 not_installed gives no sample", false, ohProc{}, nil, nil, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			if tc.systemInfo != nil {
				srv.route("/rest/systeminfo", tc.systemInfo)
			}
			root := newOHRoot(t)
			if tc.install {
				mustMkdirAll(t, filepath.Join(root, "etc", "openhab"))
			}
			setOHProcess(t, root, tc.proc)
			clock := &ohClock{at: at}
			c := newOHHistoryCollector(t, srv, root, clock.now)

			c.openhab.Poll(context.Background())
			c.fastTick(context.Background())

			h := c.History()
			pointsEqual(t, "OpenHABRSSBytes", h.OpenHABRSSBytes, tc.wantRSS)
			pointsEqual(t, "OpenHABHeapUsedBytes", h.OpenHABHeapUsedBytes, tc.wantHeap)
			pointsEqual(t, "OpenHABRestarts", h.OpenHABRestarts, tc.wantRestart)
		})
	}
	t.Run("AC1 requests unchanged", func(t *testing.T) {
		srv := newOHServer(t)
		root := newOHRoot(t, "etc/openhab")
		writeOHJava(t, root)
		c := newOHHistoryCollector(t, srv, root, nil)

		c.openhab.Poll(context.Background())
		c.fastTick(context.Background())

		if got := srv.requests(); !slices.Equal(got, ohRESTPaths) {
			t.Fatalf("requests of a poll feeding the history = %v, want today's %v", got, ohRESTPaths)
		}
		if got := len(c.History().OpenHABRSSBytes); got != 1 {
			t.Fatalf("RSS points after the poll = %d, want 1 (the poll feeds the history)", got)
		}
	})
}

func TestOpenHABCollector_Poll_RestartSample(t *testing.T) {
	a := ohProc{938, ohStartTicks}
	b := ohProc{ohOtherPID, ohOtherStartTicks}
	c3 := ohProc{ohThirdPID, ohOtherStartTicks}
	aNew := ohProc{938, ohChangedStartTick}
	none := ohProc{}
	tests := []struct {
		name  string
		polls []ohProc
		want  []int // indexes of the polls that give a restart point
		pids  []int // the PID of each restart point
	}{
		{"AC2 first process gives no restart", []ohProc{a}, nil, nil},
		{"AC2 same PID and start time give none", []ohProc{a, a, a}, nil, nil},
		{"AC2 new PID gives the new PID", []ohProc{a, b}, []int{1}, []int{ohOtherPID}},
		{"AC2 same PID with new start time gives a restart", []ohProc{a, aNew}, []int{1}, []int{938}},
		{"AC2 A then no process then B gives one restart at B's poll", []ohProc{a, none, none, b}, []int{3}, []int{ohOtherPID}},
		{"AC2 A then no process then A gives none", []ohProc{a, none, a}, nil, nil},
		{"AC2 no process first then A gives none", []ohProc{none, none, a}, nil, nil},
		{"AC2 two restarts give two points", []ohProc{a, b, b, c3}, []int{1, 3}, []int{ohOtherPID, ohThirdPID}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			root := newOHRoot(t, "etc/openhab")
			clock := &ohClock{}
			c := newOHHistoryCollector(t, srv, root, clock.now)
			base := time.Now().Add(-time.Hour)
			var wantRSS, wantRestarts []HistoryPoint
			for i, p := range tc.polls {
				at := base.Add(time.Duration(i) * time.Minute)
				setOHProcess(t, root, p)
				clock.set(at)

				c.openhab.Poll(context.Background())

				if p.pid != 0 {
					wantRSS = append(wantRSS, ohHistoryPoint(at, ohJavaRSSBytes))
				}
				if k := slices.Index(tc.want, i); k >= 0 {
					wantRestarts = append(wantRestarts, ohHistoryPoint(at, float64(tc.pids[k])))
				}
			}
			c.fastTick(context.Background())

			h := c.History()
			pointsEqual(t, "OpenHABRSSBytes", h.OpenHABRSSBytes, wantRSS)
			pointsEqual(t, "OpenHABRestarts", h.OpenHABRestarts, wantRestarts)
		})
	}
}

func TestOpenHABCollector_Poll_CollectedAtFromHook_AC5(t *testing.T) {
	srv := newOHServer(t)
	root := newOHRoot(t, "etc/openhab")
	writeOHJava(t, root)
	now := time.Now()
	clock := &ohCallClock{early: now.Add(-time.Hour), late: now.Add(-30 * time.Minute)}
	c := newOHHistoryCollector(t, srv, root, clock.now)

	c.openhab.Poll(context.Background())
	c.fastTick(context.Background())

	got := c.OpenHAB().CollectedAt
	if got == nil || !got.Equal(clock.late) {
		t.Fatalf("collected_at = %v, want %v (the time the sample was handed to the history)", got, clock.late)
	}
	pointsEqual(t, "OpenHABRSSBytes", c.History().OpenHABRSSBytes, []HistoryPoint{ohHistoryPoint(clock.late, ohJavaRSSBytes)})
}

// newOHWindowCollector builds an OpenHABCollector with a 48 h window against a
// root with an install path and, with proc, the openHAB process.
func newOHWindowCollector(t *testing.T, install, proc bool) *OpenHABCollector {
	t.Helper()
	var paths []string
	if install {
		paths = append(paths, "etc/openhab")
	}
	root := newOHRoot(t, paths...)
	if proc {
		writeOHJava(t, root)
	}
	c, _ := newOHCollector(t, newOHServer(t), root, 1<<30, func(cfg *OpenHABConfig) { cfg.HistoryWindowHours = 48 })
	return c
}

func TestOpenHABCollector_Status_HistoryWindowHours_AC9(t *testing.T) {
	const want = `"poll_interval_seconds":60,"history_window_hours":48,`
	tests := []struct {
		name          string
		install, proc bool
		polled        bool
		wantState     string
	}{
		{"AC9 pending", true, false, false, OpenHABStatePending},
		{"AC9 not_installed", false, false, true, OpenHABStateNotInstalled},
		{"AC9 ok without process", true, false, true, OpenHABStateOK},
		{"AC9 ok with process", true, true, true, OpenHABStateOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := newOHWindowCollector(t, tc.install, tc.proc)
			if tc.polled {
				c.Poll(context.Background())
			}

			got := c.Status()

			body := mustJSON(t, got)
			if got.State != tc.wantState || got.HistoryWindowHours != 48 || !strings.Contains(body, want) {
				t.Fatalf("state, window = %q, %d in %s, want %q, 48 and %s directly after the poll interval",
					got.State, got.HistoryWindowHours, body, tc.wantState, want)
			}
		})
	}
}

// ohAlertRecorder is an OpenHABCollector.onAlert hook that keeps every sample.
type ohAlertRecorder struct {
	mu      sync.Mutex
	samples []alert.OpenHABSample
}

func (r *ohAlertRecorder) hook(s alert.OpenHABSample) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.samples = append(r.samples, s)
}

func (r *ohAlertRecorder) all() []alert.OpenHABSample {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.samples)
}

// pollAlert polls c once with a recording onAlert hook and returns the one
// sample the poll handed to it.
func pollAlert(t *testing.T, c *OpenHABCollector) alert.OpenHABSample {
	t.Helper()
	rec := &ohAlertRecorder{}
	c.onAlert = rec.hook

	c.Poll(context.Background())

	got := rec.all()
	if len(got) != 1 {
		t.Fatalf("onAlert called %d times by one poll, want once", len(got))
	}
	return got[0]
}

func TestOpenHABCollector_Poll_AlertSample_AC1(t *testing.T) {
	const notOpenHAB = `{"hello":"world"}`
	tests := []struct {
		name          string
		install, proc bool
		always        bool
		rootStatus    int
		rootBody      string
		token         string
		wantInstalled bool
		wantDown      bool
	}{
		{name: "AC1 ok with process", install: true, proc: true, wantInstalled: true},
		{name: "AC1 ok without process", install: true, wantInstalled: true},
		{name: "AC1 installed not running", install: true, rootStatus: http.StatusServiceUnavailable, wantInstalled: true, wantDown: true},
		{name: "AC1 running unreachable on 503", install: true, proc: true, rootStatus: http.StatusServiceUnavailable, wantInstalled: true, wantDown: true},
		{name: "AC1 E11 running unreachable on a refused token", install: true, proc: true, token: "tok",
			rootStatus: http.StatusUnauthorized, wantInstalled: true, wantDown: true},
		{name: "AC1 E11 running unreachable on a forbidden token", install: true, proc: true, token: "tok",
			rootStatus: http.StatusForbidden, wantInstalled: true, wantDown: true},
		{name: "AC1 E11 running unreachable on a non-openHAB root", install: true, proc: true,
			rootStatus: http.StatusOK, rootBody: notOpenHAB, wantInstalled: true, wantDown: true},
		{name: "AC1 enabled true without process and unreachable root is down", always: true,
			rootStatus: http.StatusServiceUnavailable, wantInstalled: true, wantDown: true},
		{name: "AC1 enabled true without process and answering root is not down", always: true, wantInstalled: true},
		{name: "AC1 not installed", rootStatus: http.StatusServiceUnavailable},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			if tc.install {
				paths = append(paths, "etc/openhab", "var/lib/openhab")
			}
			root := newOHRoot(t, paths...)
			if tc.proc {
				writeOHJava(t, root)
			}
			srv := newOHServer(t)
			if tc.rootStatus != 0 {
				body := tc.rootBody
				if body == "" {
					body = `{"error":{"message":"failure"}}`
				}
				srv.route("/rest/", ohJSON(tc.rootStatus, body))
			}
			opts := []func(*OpenHABConfig){allowPeerOwner}
			if tc.always {
				opts = append(opts, withAlways)
			}
			if tc.token != "" {
				opts = append(opts, withToken(tc.token))
			}
			c, _ := newOHCollector(t, srv, root, 1<<30, opts...)

			got := pollAlert(t, c)

			if got.Installed != tc.wantInstalled || got.Down != tc.wantDown {
				t.Fatalf("sample Installed, Down = %v, %v, want %v, %v", got.Installed, got.Down, tc.wantInstalled, tc.wantDown)
			}
		})
	}
}

func TestOpenHABCollector_Poll_AlertSample_AC2(t *testing.T) {
	const total = 4 << 30
	wantPercent := 100 * float64(ohJavaRSSBytes) / float64(total)
	tests := []struct {
		name      string
		proc      bool
		memTotal  uint64
		wantValid bool
	}{
		{"AC2 process and total RAM give the percentage", true, total, true},
		{"AC2 no process gives no percentage", false, total, false},
		{"AC2 total RAM 0 gives no percentage", true, 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := newOHRoot(t, "etc/openhab")
			if tc.proc {
				writeOHJava(t, root)
			}
			c, _ := newOHCollector(t, newOHServer(t), root, tc.memTotal)

			got := pollAlert(t, c)

			if got.RSSValid != tc.wantValid {
				t.Fatalf("RSSValid = %v, want %v", got.RSSValid, tc.wantValid)
			}
			if tc.wantValid && got.RSSPercent != wantPercent {
				t.Fatalf("RSSPercent = %v, want %v (RSS %d of %d bytes)", got.RSSPercent, wantPercent, ohJavaRSSBytes, total)
			}
		})
	}
}

// ohAlertThings converts an expected served Thing to its alert form.
func ohAlertThings(in []OpenHABThing) []alert.OpenHABThing {
	out := make([]alert.OpenHABThing, 0, len(in))
	for _, th := range in {
		out = append(out, alert.OpenHABThing{UID: th.UID, Label: th.Label, Status: th.Status, StatusDetail: th.StatusDetail})
	}
	return out
}

func ohSystemInfoWithStartLevel(level int) string {
	return fmt.Sprintf(`{"systemInfo":{"freeMemory":65401520,"totalMemory":729808896,"uptime":362335,"startLevel":%d}}`, level)
}

func TestOpenHABCollector_Poll_AlertThings_AC3(t *testing.T) {
	refused := ohJSON(http.StatusUnauthorized, ohUnauthorizedBody)
	tests := []struct {
		name       string
		things     string
		systemInfo http.HandlerFunc
		thingsCall http.HandlerFunc
		wantValid  bool
		wantCount  int
		wantList   []alert.OpenHABThing
	}{
		{name: "AC3 E9 start level 100 counts UNKNOWN, hostile and handler-missing Things", things: ohManyThingsBody(t),
			wantValid: true, wantCount: 15, wantList: ohAlertThings(ohOfflineNodes(1, 10))},
		{name: "AC3 E2 start level 80 leaves UNKNOWN out", things: ohManyThingsBody(t),
			systemInfo: ohJSON(http.StatusOK, ohSystemInfoWithStartLevel(80)),
			wantValid:  true, wantCount: 14, wantList: ohAlertThings(ohOfflineNodes(1, 10))},
		{name: "AC3 E2 refused systeminfo counts UNKNOWN", things: ohManyThingsBody(t), systemInfo: refused,
			wantValid: true, wantCount: 15, wantList: ohAlertThings(ohOfflineNodes(1, 10))},
		{name: "AC3 E3 disabled and ONLINE Things are not counted", things: ohThingsBody,
			wantValid: true, wantCount: 1,
			wantList: []alert.OpenHABThing{{UID: "hue:device:b1:lamp_2D1", Label: "Lamp: hall!", Status: "OFFLINE", StatusDetail: "NONE"}}},
		{name: "AC3 all ONLINE gives count 0 and no list", things: mustJSON(t, []ohThing{newOHThing("b:on", "On", "ONLINE", "NONE", "")}),
			wantValid: true},
		{name: "AC3 E1 refused Things call is not valid", thingsCall: refused, wantValid: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := newOHServer(t)
			if tc.things != "" {
				srv.route("/rest/things", ohJSON(http.StatusOK, tc.things))
			}
			if tc.thingsCall != nil {
				srv.route("/rest/things", tc.thingsCall)
			}
			if tc.systemInfo != nil {
				srv.route("/rest/systeminfo", tc.systemInfo)
			}
			c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

			got := pollAlert(t, c)

			if got.ThingsValid != tc.wantValid {
				t.Fatalf("ThingsValid = %v, want %v", got.ThingsValid, tc.wantValid)
			}
			if got.ThingsOffline != tc.wantCount || !slices.Equal(got.Things, tc.wantList) {
				t.Fatalf("ThingsOffline, Things = %d, %+v, want %d, %+v", got.ThingsOffline, got.Things, tc.wantCount, tc.wantList)
			}
		})
	}
}

func TestOpenHABCollector_Poll_AlertThingsRootFailing_AC3(t *testing.T) {
	srv := newOHServer(t)
	srv.failAll(http.StatusServiceUnavailable)
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

	got := pollAlert(t, c)

	if got.ThingsValid || got.ThingsOffline != 0 || len(got.Things) != 0 {
		t.Fatalf("sample = %+v, want no valid Things when the root call failed", got)
	}
}

func TestOpenHABCollector_Poll_AlertThingsCapped_AC8(t *testing.T) {
	const maxBytes = 256
	label := strings.Repeat("a", 255) + strings.Repeat("é", 23)
	uid := "x:" + strings.Repeat("u", 400)
	status := strings.Repeat("S", 300)
	detail := strings.Repeat("é", 150)
	body := mustJSON(t, []ohThing{newOHThing(uid, label, status, detail, "")})
	srv := newOHServer(t)
	srv.route("/rest/things", ohJSON(http.StatusOK, body))
	c, _ := newOHCollector(t, srv, newOHRoot(t, "etc/openhab"), 1<<30)

	rec := &ohAlertRecorder{}
	c.onAlert = rec.hook
	c.Poll(context.Background())

	samples := rec.all()
	if len(samples) != 1 || len(samples[0].Things) != 1 {
		t.Fatalf("samples = %+v, want one sample with one Thing", samples)
	}
	got := samples[0].Things[0]
	tests := []struct {
		field, got, full string
		wantLen          int
	}{
		{"label", got.Label, label, 255},
		{"uid", got.UID, uid, maxBytes},
		{"status", got.Status, status, maxBytes},
		{"status_detail", got.StatusDetail, detail, maxBytes},
	}
	for _, tc := range tests {
		t.Run("AC8 "+tc.field, func(t *testing.T) {
			if !utf8.ValidString(tc.got) || !strings.HasPrefix(tc.full, tc.got) || len(tc.got) != tc.wantLen {
				t.Fatalf("%s = %d bytes (valid UTF-8 %v, prefix %v), want a valid prefix of %d bytes",
					tc.field, len(tc.got), utf8.ValidString(tc.got), strings.HasPrefix(tc.full, tc.got), tc.wantLen)
			}
		})
	}
	served := c.Status().Things.NotOnline
	if len(served) != 1 || served[0].UID != uid || served[0].Label != label || served[0].Status != status || served[0].StatusDetail != detail {
		t.Fatalf("served NotOnline = %+v, want the full unchanged strings", served)
	}
}

func TestOpenHABCollector_Poll_AlertHook_AC5(t *testing.T) {
	srv := newOHServer(t)
	root := newOHRoot(t, "etc/openhab")
	writeOHJava(t, root)
	c, _ := newOHCollector(t, srv, root, 1<<30)
	stamped := ohNow.Add(-90 * time.Minute)
	c.onSample = func(openHABSample) time.Time { return stamped }
	rec := &ohAlertRecorder{}
	c.onAlert = func(s alert.OpenHABSample) {
		_ = c.Status() // takes the read lock: the hook runs holding no lock
		rec.hook(s)
	}

	c.Poll(context.Background())
	c.Poll(context.Background())

	got := rec.all()
	if len(got) != 2 {
		t.Fatalf("onAlert called %d times by two polls, want 2", len(got))
	}
	collected := c.Status().CollectedAt
	if collected == nil || !collected.Equal(stamped) {
		t.Fatalf("collected_at = %v, want %v", collected, stamped)
	}
	for i, s := range got {
		if !s.Timestamp.Equal(stamped) {
			t.Fatalf("sample %d Timestamp = %v, want collected_at %v", i, s.Timestamp, stamped)
		}
	}
}

func TestOpenHABCollector_Poll_AlertHookNotInstalled_AC6(t *testing.T) {
	c, _ := newOHCollector(t, newOHServer(t), newOHRoot(t), 1<<30)
	c.onSample = func(openHABSample) time.Time {
		t.Fatal("onSample called for not_installed")
		return time.Time{}
	}

	got := pollAlert(t, c)

	if got.Installed || got.Down || got.RSSValid || got.ThingsValid {
		t.Fatalf("sample = %+v, want Installed false and no valid figure", got)
	}
	if !got.Timestamp.Equal(ohNow) {
		t.Fatalf("Timestamp = %v, want the clock's %v (no collected_at exists)", got.Timestamp, ohNow)
	}
}
