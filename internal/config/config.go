// Package config loads PiMonitor's runtime configuration from defaults, an
// optional YAML file, and CLI flags, in that order of increasing
// precedence.
package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/netip"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Thresholds are the warn/critical cutoffs used both to color-code the web
// dashboard and to size the load-average gauges. It doubles as the JSON
// shape served by GET /api/v1/config (see internal/httpapi.ClientConfig),
// so the same field set backs the YAML config file and the REST API without
// a second, hand-copied struct.
type Thresholds struct {
	TemperatureWarnC  float64 `yaml:"temperature_warn_c" json:"temperature_warn_c"`
	TemperatureCritC  float64 `yaml:"temperature_crit_c" json:"temperature_crit_c"`
	CPUWarnPercent    float64 `yaml:"cpu_warn_percent" json:"cpu_warn_percent"`
	CPUCritPercent    float64 `yaml:"cpu_crit_percent" json:"cpu_crit_percent"`
	DiskWarnPercent   float64 `yaml:"disk_warn_percent" json:"disk_warn_percent"`
	DiskCritPercent   float64 `yaml:"disk_crit_percent" json:"disk_crit_percent"`
	SwapWarnPercent   float64 `yaml:"swap_warn_percent" json:"swap_warn_percent"`
	SwapCritPercent   float64 `yaml:"swap_crit_percent" json:"swap_crit_percent"`
	MemoryWarnPercent float64 `yaml:"memory_warn_percent" json:"memory_warn_percent"`
	MemoryCritPercent float64 `yaml:"memory_crit_percent" json:"memory_crit_percent"`

	OpenHABRSSWarnPercent    float64 `yaml:"openhab_rss_warn_percent" json:"openhab_rss_warn_percent"`
	OpenHABRSSCritPercent    float64 `yaml:"openhab_rss_crit_percent" json:"openhab_rss_crit_percent"`
	OpenHABThingsOfflineWarn int     `yaml:"openhab_things_offline_warn" json:"openhab_things_offline_warn"`
	OpenHABThingsOfflineCrit int     `yaml:"openhab_things_offline_crit" json:"openhab_things_offline_crit"`
}

// Alerts configures the server-side threshold alert engine, which maps each
// snapshot against Thresholds into per-metric alert states and transition
// events exposed via GET /api/v1/alerts.
type Alerts struct {
	// Enabled toggles the alert engine. When false, GET /api/v1/alerts
	// reports enabled=false with no states or events.
	Enabled bool `yaml:"enabled"`
	// ForSeconds is the debounce window: a threshold crossing must persist
	// for at least this long before it is reported as an alert, suppressing
	// short-lived spikes. Zero fires on the first crossing.
	ForSeconds float64 `yaml:"for_seconds"`
	// OpenHABForSeconds is the debounce window of the openHAB alerts, in
	// seconds.
	OpenHABForSeconds float64 `yaml:"openhab_for_seconds"`
	// Webhooks are HTTP endpoints that receive a JSON POST on every alert
	// fired/cleared transition. A single generic webhook covers Slack,
	// Discord, Home Assistant, ntfy, etc. via their incoming-webhook formats.
	// Delivery happens off the collection path, so a slow or failing webhook
	// never blocks metric collection.
	Webhooks []Webhook `yaml:"webhooks"`
	// NotifyMaxRetries is how many times a failed webhook delivery is retried
	// (with exponential backoff) before it is given up on. Zero means a single
	// attempt with no retries.
	NotifyMaxRetries int `yaml:"notify_max_retries"`
	// NotifyRetryBackoffSeconds is the base delay before the first retry; it
	// doubles on each subsequent attempt. Zero retries immediately.
	NotifyRetryBackoffSeconds float64 `yaml:"notify_retry_backoff_seconds"`
	// NotifyMinIntervalSeconds rate-limits fired events per metric and
	// resource, per webhook: a fired event less than this many seconds after
	// the last delivered fired event of the same metric and resource to the
	// same webhook is dropped. A cleared event is never rate-limited and ends
	// the window, so the next fired event is delivered. Zero disables
	// rate-limiting.
	NotifyMinIntervalSeconds float64 `yaml:"notify_min_interval_seconds"`
}

// Webhook is one HTTP notification destination for alert transition events.
type Webhook struct {
	// URL is the endpoint POSTed on each matching event. Required.
	URL string `yaml:"url"`
	// MinLevel filters which events are delivered to this webhook: only those
	// reaching at least this severity are sent. Valid values are "warn" and
	// "crit"; empty defaults to "warn" (every transition, since every event
	// touches at least the warn level).
	MinLevel string `yaml:"min_level"`
	// Template is an optional Go text/template rendered against the event to
	// build the request body (e.g. a Slack `{"text": "..."}` payload). When
	// empty, a default JSON object describing the event is sent.
	Template string `yaml:"template"`
	// ContentType sets the request's Content-Type header. Empty defaults to
	// "application/json"; override it when a custom Template renders a
	// non-JSON body (e.g. "text/plain").
	ContentType string `yaml:"content_type"`
	// TimeoutSeconds bounds a single delivery attempt. Zero uses a built-in
	// default.
	TimeoutSeconds float64 `yaml:"timeout_seconds"`
}

// Config is PiMonitor's full runtime configuration.
type Config struct {
	ListenAddr                   string  `yaml:"listen_addr"`
	LogLevel                     string  `yaml:"log_level"`
	PollIntervalSeconds          float64 `yaml:"poll_interval_seconds"`
	UpdatesCheckMinutes          float64 `yaml:"updates_check_minutes"`
	UpdatesStaleThresholdMinutes float64 `yaml:"updates_stale_threshold_minutes"`
	HistoryWindowMinutes         float64 `yaml:"history_window_minutes"`
	HistoryPersistEnabled        bool    `yaml:"history_persist_enabled"`
	DataDir                      string  `yaml:"data_dir"`
	NetworkEnabled               bool    `yaml:"network_enabled"`
	// HwmonEnabled toggles enumeration of the kernel hwmon subsystem
	// (Snapshot.sensors): the SoC sensor plus any other exposed sensor
	// (PoE-HAT fan controller, NVMe/SSD drive, attached I2C/1-Wire sensor,
	// ...). On by default - it's cheap sysfs reads, additive to the existing
	// temperature field.
	HwmonEnabled      bool `yaml:"hwmon_enabled"`
	DistroInfoEnabled bool `yaml:"distro_info_enabled"`
	PiModelEnabled    bool `yaml:"pi_model_enabled"`
	// HostnameEnabled toggles whether the host name is collected and exposed
	// as system.hostname.
	HostnameEnabled bool `yaml:"hostname_enabled"`
	// PrometheusEnabled toggles GET /metrics, a Prometheus text-exposition
	// rendering of the current snapshot and the openHAB status, served alongside the JSON
	// /api/v1/... API. It defaults to false — a distinct opt-in rather than
	// always-on avoids confusion between this path and /api/v1/metrics. When
	// api_key is set, GET /metrics honours it exactly like every other
	// endpoint (configure it as a Prometheus scrape bearer_token/authorization
	// header), so enabling this does not bypass existing API authentication.
	PrometheusEnabled bool   `yaml:"prometheus_enabled"`
	APIKey            string `yaml:"api_key"`
	// AccessLogEnabled toggles the per-request debug log line withLogging
	// emits for every HTTP request. It is on by default, matching the
	// pre-existing behaviour (that line has always been emitted, gated only
	// by log_level); set it to false to silence per-request logging
	// entirely, e.g. on a busy Pi where every dashboard poll would otherwise
	// add a debug line. In-memory request counters (GET /api/v1/serverstats)
	// keep working regardless of this setting.
	AccessLogEnabled bool `yaml:"access_log_enabled"`
	// TLSCertFile and TLSKeyFile, when both set, make the server listen with
	// HTTPS (ListenAndServeTLS) instead of plain HTTP, for a single-Pi setup
	// that doesn't want to stand up a separate reverse proxy for TLS. Leave
	// both empty (the default) to serve plain HTTP. Setting only one is a
	// config error (see Validate) rather than silently falling back to
	// plain HTTP.
	TLSCertFile string `yaml:"tls_cert"`
	TLSKeyFile  string `yaml:"tls_key"`
	// HealthzMaxStalenessSeconds bounds how old the latest collected
	// snapshot may be before GET /healthz reports unhealthy. Zero (the
	// default) computes the bound automatically as a multiple of
	// PollIntervalSeconds instead of a fixed value, so it stays sensible
	// across very different poll intervals — see HealthzMaxStaleness.
	HealthzMaxStalenessSeconds float64    `yaml:"healthz_max_staleness_seconds"`
	Thresholds                 Thresholds `yaml:"thresholds"`
	Alerts                     Alerts     `yaml:"alerts"`
	// ProcessesEnabled toggles GET /api/v1/processes: the top-N running
	// processes by CPU usage and by resident memory (RSS). Off by default —
	// walking every /proc/<pid> entry is more invasive than the other
	// metrics (it exposes what's running on the host, not just aggregate
	// load), so this is a deliberate opt-in. Recomputed on the slow tick
	// (updates_check_minutes), not the fast one — see
	// collector.Config.SlowInterval.
	ProcessesEnabled bool `yaml:"processes_enabled"`
	// ProcessesTopN is how many processes are reported per ranking (CPU,
	// memory) when processes_enabled is true. Validated regardless of
	// processes_enabled so a typo is caught at startup even while the
	// feature is off.
	ProcessesTopN int `yaml:"processes_top_n"`
	// OpenHAB configures the read-only status card for a local openHAB
	// installation (GET /api/v1/openhab).
	OpenHAB OpenHAB `yaml:"openhab"`
}

// OpenHAB is the openhab block of the configuration: whether and how PiMonitor
// polls a local openHAB instance for its status.
type OpenHAB struct {
	// Enabled is auto, true or false (case-insensitive); see Mode.
	Enabled string `yaml:"enabled"`
	// BaseURL is the openHAB root URL; it must be an http URL with a loopback host.
	BaseURL string `yaml:"base_url"`
	// APIToken is the optional openHAB API token; never logged or served.
	APIToken string `yaml:"api_token"`
	// PollIntervalSeconds is the interval between openHAB polls.
	PollIntervalSeconds int `yaml:"poll_interval_seconds"`
	// HistoryWindowHours is the retention window of the openHAB history series.
	HistoryWindowHours int `yaml:"history_window_hours"`
}

// HistoryCapacity returns the number of openHAB history points that cover
// HistoryWindowHours at PollIntervalSeconds resolution.
// The count is at least 1 and at most maxHistoryCapacity.
func (o OpenHAB) HistoryCapacity() int {
	if o.PollIntervalSeconds <= 0 || o.HistoryWindowHours <= 0 {
		return 1
	}
	capacity := int64(o.HistoryWindowHours) * 3600 / int64(o.PollIntervalSeconds)
	return int(min(max(capacity, 1), maxHistoryCapacity))
}

// OpenHABTokenEnvVar is the environment variable that overrides
// openhab.api_token. It is read and unset in load, like APIKeyEnvVar.
const OpenHABTokenEnvVar = "PIMONITOR_OPENHAB_TOKEN"

// Mode returns Enabled lower-cased: auto, true or false.
func (o OpenHAB) Mode() string {
	return strings.ToLower(o.Enabled)
}

// PollInterval returns PollIntervalSeconds as a duration.
func (o OpenHAB) PollInterval() time.Duration {
	return time.Duration(o.PollIntervalSeconds) * time.Second
}

// Bounds of openhab.poll_interval_seconds.
const (
	minOpenHABPollSeconds = 60
	maxOpenHABPollSeconds = 86400

	minOpenHABHistoryHours = 1
	maxOpenHABHistoryHours = 168
)

// validate checks the openhab block. It runs regardless of Enabled so a typo
// is caught at startup; the base URL and the token are never echoed.
func (o OpenHAB) validate() error {
	switch o.Mode() {
	case "auto", "true", "false":
	default:
		return fmt.Errorf("openhab.enabled must be auto, true or false (got %q)", o.Enabled)
	}
	if !loopbackHTTPURL(o.BaseURL) {
		return errors.New("openhab.base_url must be an http URL with a loopback host (127.0.0.0/8, ::1 or localhost) and no user, path, query or fragment")
	}
	if o.PollIntervalSeconds < minOpenHABPollSeconds || o.PollIntervalSeconds > maxOpenHABPollSeconds {
		return fmt.Errorf("openhab.poll_interval_seconds must be between %d and %d (got %v)", minOpenHABPollSeconds, maxOpenHABPollSeconds, o.PollIntervalSeconds)
	}
	if o.HistoryWindowHours < minOpenHABHistoryHours || o.HistoryWindowHours > maxOpenHABHistoryHours {
		return fmt.Errorf("openhab.history_window_hours must be between %d and %d (got %v)", minOpenHABHistoryHours, maxOpenHABHistoryHours, o.HistoryWindowHours)
	}
	if strings.IndexFunc(o.APIToken, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return errors.New("openhab.api_token must not contain whitespace or control characters")
	}
	return nil
}

// loopbackHTTPURL reports whether raw is a plain http URL whose host is
// localhost or a loopback IP literal, without user info, path, query or
// fragment.
func loopbackHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" || u.Opaque != "" || u.User != nil {
		return false
	}
	if (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return false
		}
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.Zone() == "" && addr.IsLoopback()
}

// Default returns PiMonitor's built-in default configuration.
func Default() Config {
	return Config{
		ListenAddr:                   ":8080",
		LogLevel:                     "info",
		PollIntervalSeconds:          5,
		UpdatesCheckMinutes:          15,
		UpdatesStaleThresholdMinutes: 12 * 60,
		HistoryWindowMinutes:         60,
		HistoryPersistEnabled:        true,
		DataDir:                      "/var/lib/pimonitor",
		NetworkEnabled:               true,
		HwmonEnabled:                 true,
		DistroInfoEnabled:            true,
		PiModelEnabled:               true,
		HostnameEnabled:              true,
		PrometheusEnabled:            false,
		APIKey:                       "",
		AccessLogEnabled:             true,
		HealthzMaxStalenessSeconds:   0,
		Thresholds: Thresholds{
			TemperatureWarnC:  60,
			TemperatureCritC:  75,
			CPUWarnPercent:    80,
			CPUCritPercent:    95,
			DiskWarnPercent:   80,
			DiskCritPercent:   95,
			SwapWarnPercent:   50,
			SwapCritPercent:   90,
			MemoryWarnPercent: 80,
			MemoryCritPercent: 95,

			OpenHABRSSWarnPercent:    50,
			OpenHABRSSCritPercent:    75,
			OpenHABThingsOfflineWarn: 1,
			OpenHABThingsOfflineCrit: 0,
		},
		Alerts: Alerts{
			Enabled:                   true,
			ForSeconds:                30,
			OpenHABForSeconds:         300,
			Webhooks:                  nil,
			NotifyMaxRetries:          3,
			NotifyRetryBackoffSeconds: 1,
			NotifyMinIntervalSeconds:  5,
		},
		ProcessesEnabled: false,
		ProcessesTopN:    5,
		OpenHAB: OpenHAB{
			Enabled:             "auto",
			BaseURL:             "http://127.0.0.1:8080",
			PollIntervalSeconds: 60,
			HistoryWindowHours:  24,
		},
	}
}

// AlertFor is the debounce window before a threshold crossing is reported as
// an alert.
func (c Config) AlertFor() time.Duration {
	return time.Duration(c.Alerts.ForSeconds * float64(time.Second))
}

// OpenHABAlertFor is the debounce window before an openHAB threshold crossing
// is reported as an alert.
func (c Config) OpenHABAlertFor() time.Duration {
	return time.Duration(c.Alerts.OpenHABForSeconds * float64(time.Second))
}

// FastInterval is how often live metrics (CPU, load, temperature,
// memory/swap, disk, network) are sampled.
func (c Config) FastInterval() time.Duration {
	return time.Duration(c.PollIntervalSeconds * float64(time.Second))
}

// SlowInterval is how often available apt updates are checked.
func (c Config) SlowInterval() time.Duration {
	return time.Duration(c.UpdatesCheckMinutes * float64(time.Minute))
}

// UpdatesStaleThreshold is how old the apt cache may be before it is
// flagged as stale.
func (c Config) UpdatesStaleThreshold() time.Duration {
	return time.Duration(c.UpdatesStaleThresholdMinutes * float64(time.Minute))
}

// healthzStalenessMultiple is how many fast-poll intervals GET /healthz
// tolerates before reporting unhealthy when HealthzMaxStalenessSeconds is
// left at its default (0). Three ticks allows for one missed/slow
// collection cycle without flapping the health check, while still catching
// a genuinely stalled collector goroutine well before an operator would
// notice via stale dashboard data.
const healthzStalenessMultiple = 3

// HealthzMaxStaleness is how old the latest collected snapshot may be
// before GET /healthz reports unhealthy instead of the static 200 ok.
// HealthzMaxStalenessSeconds set to a positive value is used as-is
// (Validate requires it be >= PollIntervalSeconds). Left at its default of
// 0, the bound is derived instead of a fixed constant: healthzStalenessMultiple
// intervals plus 2*tickOverhead. The 2x accounts for how the collector
// publishes its snapshot timestamp only when a tick completes (not when it
// starts), so immediately before a tick publishes, the visible age can
// reach one in-flight tick's duration plus the one still queued behind it.
// tickOverhead should be collector.WorstCaseTickOverhead in production, the
// most a single tick may legitimately run over instant reads (hung
// firmware calls, a stalled mount) without the collector actually being
// stuck; this package cannot import collector directly (collector already
// imports config for Thresholds), so the caller supplies it. Tests that
// don't care about tick timing may pass 0.
func (c Config) HealthzMaxStaleness(tickOverhead time.Duration) time.Duration {
	if c.HealthzMaxStalenessSeconds > 0 {
		return time.Duration(c.HealthzMaxStalenessSeconds * float64(time.Second))
	}
	return healthzStalenessMultiple*c.FastInterval() + 2*tickOverhead
}

// HistoryWindow is the rolling window of history retained per metric
// time series; when persistence is enabled, restored points older than
// this are dropped on load.
func (c Config) HistoryWindow() time.Duration {
	return time.Duration(c.HistoryWindowMinutes * float64(time.Minute))
}

// HistoryCapacity is the number of samples retained per metric time series
// to cover HistoryWindowMinutes at PollIntervalSeconds resolution.
func (c Config) HistoryCapacity() int {
	if c.PollIntervalSeconds <= 0 {
		return 1
	}
	capacity := int(c.HistoryWindowMinutes * 60 / c.PollIntervalSeconds)
	if capacity < 1 {
		return 1
	}
	return capacity
}

// maxHistoryCapacity bounds HistoryCapacity(). NewRingBuffer allocates its
// capacity eagerly for each of the 7 scalar history series, so an
// unreasonably large window/interval ratio (e.g. a config typo like
// history_window_minutes: 525600 with poll_interval_seconds: 1) would
// otherwise allocate on the order of gigabytes at startup and OOM a Pi. One
// million points per series (~7M points, well under 1 GiB total) is far more
// than any real dashboard use case needs.
const maxHistoryCapacity = 1_000_000

// maxProcessesTopN bounds ProcessesTopN, so a config typo (or an
// intentionally huge value) can't make GET /api/v1/processes return an
// unreasonably large payload — a real host rarely has more than a few
// hundred processes anyway, so this is a generous ceiling, not a
// realistic setting.
const maxProcessesTopN = 100

// validLogLevels are the log levels newLogger understands; any other value
// silently falls back to info, so we reject it here instead.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// maxAlertWindowSeconds is the largest accepted alert debounce window (7 days).
// A larger value makes the float-to-Duration conversion in AlertFor and
// OpenHABAlertFor overflow to a platform-dependent, possibly negative duration,
// which would confirm every alert on its first sample (decision 0034).
const maxAlertWindowSeconds = 604800

// validateAlertWindow checks one alert debounce window, in seconds, for key.
// The value is already known to be finite.
func validateAlertWindow(key string, seconds float64) error {
	if seconds < 0 {
		return fmt.Errorf("%s must be >= 0 (got %v)", key, seconds)
	}
	return atMost(key, seconds, maxAlertWindowSeconds)
}

// Upper bounds of the float options that are multiplied into a time.Duration
// (decision 0035): a larger value overflows the conversion to a
// platform-dependent, possibly negative duration.
const (
	// maxWaitSeconds is the largest accepted interval, backoff, rate-limit
	// window or webhook timeout, in seconds (7 days).
	maxWaitSeconds = maxAlertWindowSeconds
	// maxWaitMinutes is the largest accepted updates check interval (7 days).
	maxWaitMinutes = 10080
	// maxAgeSeconds is the largest accepted staleness limit in seconds (365 days).
	maxAgeSeconds = 31536000
	// maxAgeMinutes is the largest accepted stale threshold or history window
	// in minutes (365 days).
	maxAgeMinutes = 525600
)

// Lower bounds of the two collection intervals (decision 0036): a smaller
// value truncates to a zero time.Duration or makes the collector tick
// faster than anything reads it.
const (
	// minPollIntervalSeconds is the smallest accepted poll_interval_seconds.
	minPollIntervalSeconds = 1
	// minUpdatesCheckMinutes is the smallest accepted updates_check_minutes.
	minUpdatesCheckMinutes = 1
)

// atMost returns an error naming key when value exceeds limit.
func atMost(key string, value float64, limit int) error {
	if value > float64(limit) {
		return fmt.Errorf("%s must be <= %d (got %v)", key, limit, value)
	}
	return nil
}

// atLeast returns an error naming key when value is below limit.
func atLeast(key string, value float64, limit int) error {
	if value < float64(limit) {
		return fmt.Errorf("%s must be >= %d (got %v)", key, limit, value)
	}
	return nil
}

// validateUpperBounds rejects values whose conversion to a time.Duration would
// overflow. It runs right after validateFinite, so the key-naming bound wins
// over every other range check.
func (c Config) validateUpperBounds() error {
	checks := []struct {
		name  string
		value float64
		limit int
	}{
		{"poll_interval_seconds", c.PollIntervalSeconds, maxWaitSeconds},
		{"updates_check_minutes", c.UpdatesCheckMinutes, maxWaitMinutes},
		{"updates_stale_threshold_minutes", c.UpdatesStaleThresholdMinutes, maxAgeMinutes},
		{"healthz_max_staleness_seconds", c.HealthzMaxStalenessSeconds, maxAgeSeconds},
		{"history_window_minutes", c.HistoryWindowMinutes, maxAgeMinutes},
		{"alerts.notify_retry_backoff_seconds", c.Alerts.NotifyRetryBackoffSeconds, maxWaitSeconds},
		{"alerts.notify_min_interval_seconds", c.Alerts.NotifyMinIntervalSeconds, maxWaitSeconds},
	}
	for _, ck := range checks {
		if err := atMost(ck.name, ck.value, ck.limit); err != nil {
			return err
		}
	}
	for i, w := range c.Alerts.Webhooks {
		if err := atMost(fmt.Sprintf("alerts.webhooks[%d].timeout_seconds", i), w.TimeoutSeconds, maxWaitSeconds); err != nil {
			return err
		}
	}
	return nil
}

// Validate checks the resolved configuration for values that would crash the
// service (e.g. a zero poll interval panics time.NewTicker) or silently make
// it misbehave. It returns a descriptive error for the first violation so a
// daemon fails fast at startup rather than later at runtime.
func (c Config) Validate() error {
	if err := c.validateFinite(); err != nil {
		return err
	}
	if err := c.validateUpperBounds(); err != nil {
		return err
	}
	if err := c.validateTiming(); err != nil {
		return err
	}
	if err := c.validateServer(); err != nil {
		return err
	}
	if err := c.Thresholds.validate(); err != nil {
		return err
	}
	if err := validateAlertWindow("alerts.for_seconds", c.Alerts.ForSeconds); err != nil {
		return err
	}
	if err := validateAlertWindow("alerts.openhab_for_seconds", c.Alerts.OpenHABForSeconds); err != nil {
		return err
	}
	if err := c.Alerts.validate(); err != nil {
		return err
	}
	if c.ProcessesTopN < 1 || c.ProcessesTopN > maxProcessesTopN {
		return fmt.Errorf("processes_top_n must be between 1 and %d (got %v)", maxProcessesTopN, c.ProcessesTopN)
	}
	return c.OpenHAB.validate()
}

// validateFinite rejects NaN and infinite values for every number option.
// Every range check is a comparison, which NaN passes and +Inf mostly passes,
// so this runs before any of them.
func (c Config) validateFinite() error {
	t := c.Thresholds
	checks := []struct {
		name  string
		value float64
	}{
		{"poll_interval_seconds", c.PollIntervalSeconds},
		{"updates_check_minutes", c.UpdatesCheckMinutes},
		{"updates_stale_threshold_minutes", c.UpdatesStaleThresholdMinutes},
		{"history_window_minutes", c.HistoryWindowMinutes},
		{"healthz_max_staleness_seconds", c.HealthzMaxStalenessSeconds},
		{"thresholds.temperature_warn_c", t.TemperatureWarnC},
		{"thresholds.temperature_crit_c", t.TemperatureCritC},
		{"thresholds.cpu_warn_percent", t.CPUWarnPercent},
		{"thresholds.cpu_crit_percent", t.CPUCritPercent},
		{"thresholds.disk_warn_percent", t.DiskWarnPercent},
		{"thresholds.disk_crit_percent", t.DiskCritPercent},
		{"thresholds.swap_warn_percent", t.SwapWarnPercent},
		{"thresholds.swap_crit_percent", t.SwapCritPercent},
		{"thresholds.memory_warn_percent", t.MemoryWarnPercent},
		{"thresholds.memory_crit_percent", t.MemoryCritPercent},
		{"thresholds.openhab_rss_warn_percent", t.OpenHABRSSWarnPercent},
		{"thresholds.openhab_rss_crit_percent", t.OpenHABRSSCritPercent},
		{"alerts.for_seconds", c.Alerts.ForSeconds},
		{"alerts.openhab_for_seconds", c.Alerts.OpenHABForSeconds},
		{"alerts.notify_retry_backoff_seconds", c.Alerts.NotifyRetryBackoffSeconds},
		{"alerts.notify_min_interval_seconds", c.Alerts.NotifyMinIntervalSeconds},
	}
	for _, ck := range checks {
		if !isFinite(ck.value) {
			return fmt.Errorf("%s must be a finite number", ck.name)
		}
	}
	for i, w := range c.Alerts.Webhooks {
		if !isFinite(w.TimeoutSeconds) {
			return fmt.Errorf("alerts.webhooks[%d].timeout_seconds must be a finite number", i)
		}
	}
	return nil
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}

// validateTiming checks the poll/update/history interval and window fields.
func (c Config) validateTiming() error {
	if err := atLeast("poll_interval_seconds", c.PollIntervalSeconds, minPollIntervalSeconds); err != nil {
		return err
	}
	if err := atLeast("updates_check_minutes", c.UpdatesCheckMinutes, minUpdatesCheckMinutes); err != nil {
		return err
	}
	if c.UpdatesStaleThresholdMinutes < 0 {
		return fmt.Errorf("updates_stale_threshold_minutes must be >= 0 (got %v)", c.UpdatesStaleThresholdMinutes)
	}
	if c.HistoryWindowMinutes <= 0 {
		return fmt.Errorf("history_window_minutes must be > 0 (got %v)", c.HistoryWindowMinutes)
	}
	// Compare the ratio in float64 before any int conversion: converting an
	// out-of-range float64 to int is implementation-specific in Go and
	// differs by architecture (e.g. amd64 wraps to math.MinInt64, arm64
	// saturates to math.MaxInt64), so calling HistoryCapacity() first and
	// comparing the resulting int could let an extreme ratio slip past this
	// check on some platforms but not others.
	if ratio := c.HistoryWindowMinutes * 60 / c.PollIntervalSeconds; ratio > maxHistoryCapacity {
		return fmt.Errorf("history_window_minutes (%v) / poll_interval_seconds (%v) implies %v history points per series, exceeding the %d limit; increase poll_interval_seconds or reduce history_window_minutes", c.HistoryWindowMinutes, c.PollIntervalSeconds, ratio, maxHistoryCapacity)
	}
	return nil
}

// validateServer checks the HTTP server, persistence, logging, and healthz
// fields.
func (c Config) validateServer() error {
	if c.HistoryPersistEnabled && c.DataDir == "" {
		return fmt.Errorf("data_dir must not be empty when history_persist_enabled is true")
	}
	if c.ListenAddr == "" {
		return fmt.Errorf("listen_addr must not be empty")
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("log_level must be one of debug, info, warn, error (got %q)", c.LogLevel)
	}
	if c.HealthzMaxStalenessSeconds < 0 {
		return fmt.Errorf("healthz_max_staleness_seconds must be >= 0 (got %v)", c.HealthzMaxStalenessSeconds)
	}
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("tls_cert and tls_key must both be set to enable HTTPS, or both left empty for plain HTTP")
	}
	if c.HealthzMaxStalenessSeconds > 0 && c.HealthzMaxStalenessSeconds < c.PollIntervalSeconds {
		return fmt.Errorf("healthz_max_staleness_seconds (%v) must be >= poll_interval_seconds (%v), or /healthz reports unhealthy permanently", c.HealthzMaxStalenessSeconds, c.PollIntervalSeconds)
	}
	return nil
}

// validAlertMinLevels are the severities a webhook may filter on. "ok" is
// intentionally excluded: an alert transition never rests at ok on both sides,
// so filtering on it would be meaningless.
var validAlertMinLevels = map[string]bool{
	"":     true, // defaults to "warn"
	"warn": true,
	"crit": true,
}

// validate checks the notifier tuning and each configured webhook. It runs
// regardless of Enabled so a typo in an inert config is still caught at
// startup rather than silently ignored.
func (a Alerts) validate() error {
	if a.NotifyMaxRetries < 0 {
		return fmt.Errorf("alerts.notify_max_retries must be >= 0 (got %v)", a.NotifyMaxRetries)
	}
	if a.NotifyRetryBackoffSeconds < 0 {
		return fmt.Errorf("alerts.notify_retry_backoff_seconds must be >= 0 (got %v)", a.NotifyRetryBackoffSeconds)
	}
	if a.NotifyMinIntervalSeconds < 0 {
		return fmt.Errorf("alerts.notify_min_interval_seconds must be >= 0 (got %v)", a.NotifyMinIntervalSeconds)
	}
	for i, w := range a.Webhooks {
		if w.URL == "" {
			return fmt.Errorf("alerts.webhooks[%d].url must not be empty", i)
		}
		if !validAlertMinLevels[w.MinLevel] {
			return fmt.Errorf("alerts.webhooks[%d].min_level must be one of warn, crit (got %q)", i, w.MinLevel)
		}
		if w.TimeoutSeconds < 0 {
			return fmt.Errorf("alerts.webhooks[%d].timeout_seconds must be >= 0 (got %v)", i, w.TimeoutSeconds)
		}
	}
	return nil
}

// validate checks that every threshold is non-negative and that each warn
// cutoff does not exceed its critical counterpart.
func (t Thresholds) validate() error {
	pairs := []struct {
		warnKey, critKey string
		warn, crit       float64
	}{
		{"temperature_warn_c", "temperature_crit_c", t.TemperatureWarnC, t.TemperatureCritC},
		{"cpu_warn_percent", "cpu_crit_percent", t.CPUWarnPercent, t.CPUCritPercent},
		{"disk_warn_percent", "disk_crit_percent", t.DiskWarnPercent, t.DiskCritPercent},
		{"swap_warn_percent", "swap_crit_percent", t.SwapWarnPercent, t.SwapCritPercent},
		{"memory_warn_percent", "memory_crit_percent", t.MemoryWarnPercent, t.MemoryCritPercent},
	}
	for _, p := range pairs {
		if p.warn < 0 {
			return fmt.Errorf("thresholds.%s must be >= 0 (got %v)", p.warnKey, p.warn)
		}
		if p.crit < 0 {
			return fmt.Errorf("thresholds.%s must be >= 0 (got %v)", p.critKey, p.crit)
		}
		if p.warn > p.crit {
			return fmt.Errorf("thresholds.%s (%v) must be <= thresholds.%s (%v)", p.warnKey, p.warn, p.critKey, p.crit)
		}
	}
	// The openHAB options treat 0 as "off", so warn <= crit only applies
	// when both are set.
	openHABPairs := []struct {
		warnKey, critKey string
		warn, crit       float64
	}{
		{"openhab_rss_warn_percent", "openhab_rss_crit_percent", t.OpenHABRSSWarnPercent, t.OpenHABRSSCritPercent},
		{"openhab_things_offline_warn", "openhab_things_offline_crit", float64(t.OpenHABThingsOfflineWarn), float64(t.OpenHABThingsOfflineCrit)},
	}
	for _, p := range openHABPairs {
		if p.warn < 0 {
			return fmt.Errorf("thresholds.%s must be >= 0 (got %v)", p.warnKey, p.warn)
		}
		if p.crit < 0 {
			return fmt.Errorf("thresholds.%s must be >= 0 (got %v)", p.critKey, p.crit)
		}
		if p.warn != 0 && p.crit != 0 && p.warn > p.crit {
			return fmt.Errorf("thresholds.%s (%v) must be <= thresholds.%s (%v)", p.warnKey, p.warn, p.critKey, p.crit)
		}
	}
	return nil
}

// loadYAMLFile merges the YAML file at path into cfg. Only keys present in
// the file override cfg's existing (default) values; absent keys are left
// untouched, since the decoder only writes fields it finds in the document.
// KnownFields(true) rejects any key that doesn't map to a Config field, so a
// typo (e.g. "api_kay") fails fast at startup instead of silently falling
// back to the default (e.g. no authentication).
func loadYAMLFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := checkIntegerOptions(data); err != nil {
		return err
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// An empty or comment-only file has nothing to decode; Decode reports
	// that as io.EOF, but it's not an error here since cfg is left as-is.
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

// integerOptionNames are the YAML key paths of the int-typed options. The
// decoder truncates a fractional value for them, so checkIntegerOptions
// inspects each one before the real decode.
var integerOptionNames = []string{"processes_top_n", "alerts.notify_max_retries", "openhab.poll_interval_seconds", "openhab.history_window_hours", "thresholds.openhab_things_offline_warn", "thresholds.openhab_things_offline_crit"}

// checkIntegerOptions decodes data a second time, with the same decoder and
// so the same alias, merge-key and tag resolution, into float64 fields for the
// integer options, and rejects a non-finite or fractional value. A decode
// error is ignored: the real decode fails on the same input and reports it.
func checkIntegerOptions(data []byte) error {
	var shadow struct {
		ProcessesTopN *float64 `yaml:"processes_top_n"`
		Alerts        struct {
			NotifyMaxRetries *float64 `yaml:"notify_max_retries"`
		} `yaml:"alerts"`
		Thresholds struct {
			OpenHABThingsOfflineWarn *float64 `yaml:"openhab_things_offline_warn"`
			OpenHABThingsOfflineCrit *float64 `yaml:"openhab_things_offline_crit"`
		} `yaml:"thresholds"`
		OpenHAB struct {
			PollIntervalSeconds *float64 `yaml:"poll_interval_seconds"`
			HistoryWindowHours  *float64 `yaml:"history_window_hours"`
		} `yaml:"openhab"`
	}
	if err := yaml.NewDecoder(bytes.NewReader(data)).Decode(&shadow); err != nil {
		// Not this check's error to report; see the doc comment.
		return nil //nolint:nilerr
	}
	values := map[string]*float64{
		"processes_top_n":                        shadow.ProcessesTopN,
		"alerts.notify_max_retries":              shadow.Alerts.NotifyMaxRetries,
		"thresholds.openhab_things_offline_warn": shadow.Thresholds.OpenHABThingsOfflineWarn,
		"thresholds.openhab_things_offline_crit": shadow.Thresholds.OpenHABThingsOfflineCrit,
		"openhab.poll_interval_seconds":          shadow.OpenHAB.PollIntervalSeconds,
		"openhab.history_window_hours":           shadow.OpenHAB.HistoryWindowHours,
	}
	for _, name := range integerOptionNames {
		v := values[name]
		if v == nil {
			continue
		}
		if !isFinite(*v) {
			return fmt.Errorf("%s must be a finite number", name)
		}
		if *v != math.Trunc(*v) {
			return fmt.Errorf("%s must be a whole number (got %v)", name, *v)
		}
	}
	return nil
}

// Result is the outcome of Load: the resolved configuration, plus whether
// the caller asked to print the version and exit rather than run.
type Result struct {
	Config           Config
	VersionRequested bool
}

// APIKeyEnvVar is the environment variable that overrides the REST API key.
// It exists because the -api-key flag leaks the secret into the process
// list (any local user can read /proc/<pid>/cmdline), while an environment
// variable can be supplied by systemd via EnvironmentFile= from a
// root-readable file. The config file remains the primary mechanism.
const APIKeyEnvVar = "PIMONITOR_API_KEY"

// Load resolves configuration from defaults, an optional YAML file
// (-config), the PIMONITOR_API_KEY environment variable, and flag
// overrides, in that order of increasing precedence. As a side effect, once
// PIMONITOR_API_KEY has been read, Load removes it from the process
// environment (see the unsetEnv call in load, below, for what that does and
// does not guarantee) — a second Load call in the same process will not see
// it again.
func Load(args []string) (Result, error) {
	return load(args, os.LookupEnv, unsetenv)
}

// unsetenv removes key from the process's environment via os.Unsetenv,
// discarding the returned error: os.Unsetenv's doc comment declares no
// failure mode, and on Unix (syscall.Unsetenv) it unconditionally returns
// nil, so there is nothing for the fixed APIKeyEnvVar constant this is
// called with to fail on.
func unsetenv(key string) {
	_ = os.Unsetenv(key)
}

// load is Load with the environment injected, so tests can exercise the
// precedence rules without mutating the process environment.
func load(args []string, lookupEnv func(string) (string, bool), unsetEnv func(string)) (Result, error) {
	cfg := Default()

	fs := flag.NewFlagSet("pimonitor", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to YAML config file")
	listenAddr := fs.String("listen", "", "override listen address, e.g. :8080")
	logLevel := fs.String("log-level", "", "override log level (debug, info, warn, error)")
	apiKey := fs.String("api-key", "", "override REST API key (development only: the value is visible in the process list to every local user; use api_key in the config file or "+APIKeyEnvVar+" instead)")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		return Result{}, err
	}

	if *configPath != "" {
		if err := loadYAMLFile(*configPath, &cfg); err != nil {
			return Result{}, err
		}
	}

	// The environment sits between the config file and the flags: it is the
	// recommended override for deployments (systemd EnvironmentFile=), while
	// an explicit -api-key on the command line still wins. An empty or unset
	// variable changes nothing, mirroring how the empty flag default is
	// treated, so exporting PIMONITOR_API_KEY="" cannot accidentally disable
	// an api_key configured in the file.
	if v, ok := lookupEnv(APIKeyEnvVar); ok {
		if v != "" {
			cfg.APIKey = v
		}
		// Remove the key from the process's own environment once it has
		// been read into cfg, so nothing later in this process (a future
		// code path, a library, a panic handler) can read it back out via
		// os.Getenv/os.Environ, and so it can never be copied into a child
		// process that (unlike internal/collector's apt/vcgencmd calls,
		// which already build an explicit, minimal cmd.Env) inherits the
		// full environment.
		//
		// This is weaker than it sounds: on Linux, os.Unsetenv only updates
		// the Go runtime's in-process copy of the environment (so
		// os.LookupEnv stops finding it here on out) — it does not rewrite
		// /proc/self/environ, which the kernel populates once, from the
		// process's initial exec() argv/envp block, and never regenerates
		// from later setenv/unsetenv calls. A local user able to read this
		// process's /proc/<pid>/environ (i.e. root, or the pimonitor
		// service user itself) can still recover the key for the life of
		// the process regardless of this call. See SECURITY.md.
		unsetEnv(APIKeyEnvVar)
	}

	// The openHAB token follows the same rules as the API key: a non-empty
	// value overrides the file, and the variable is removed once read.
	if v, ok := lookupEnv(OpenHABTokenEnvVar); ok {
		if v != "" {
			cfg.OpenHAB.APIToken = v
		}
		unsetEnv(OpenHABTokenEnvVar)
	}

	if *listenAddr != "" {
		cfg.ListenAddr = *listenAddr
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}
	if *apiKey != "" {
		cfg.APIKey = *apiKey
	}

	// A -version request short-circuits before validation so `pimonitor
	// -version` still works even against an otherwise invalid config.
	if *showVersion {
		return Result{Config: cfg, VersionRequested: true}, nil
	}

	if err := cfg.Validate(); err != nil {
		return Result{}, err
	}

	return Result{Config: cfg, VersionRequested: false}, nil
}
