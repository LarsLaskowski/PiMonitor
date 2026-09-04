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
	"os"
	"time"

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
	// NotifyMinIntervalSeconds rate-limits deliveries per webhook: an event
	// arriving within this window of the previous delivery to the same URL is
	// dropped, so an alert flapping faster than the debounce can't flood a
	// destination. Zero disables rate-limiting.
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
	DistroInfoEnabled            bool    `yaml:"distro_info_enabled"`
	PiModelEnabled               bool    `yaml:"pi_model_enabled"`
	// PrometheusEnabled toggles GET /metrics, a Prometheus text-exposition
	// rendering of the current snapshot, served alongside the JSON
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
		DistroInfoEnabled:            true,
		PiModelEnabled:               true,
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
		},
		Alerts: Alerts{
			Enabled:                   true,
			ForSeconds:                30,
			Webhooks:                  nil,
			NotifyMaxRetries:          3,
			NotifyRetryBackoffSeconds: 1,
			NotifyMinIntervalSeconds:  5,
		},
	}
}

// AlertFor is the debounce window before a threshold crossing is reported as
// an alert.
func (c Config) AlertFor() time.Duration {
	return time.Duration(c.Alerts.ForSeconds * float64(time.Second))
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
// history_window_minutes: 525600 with poll_interval_seconds: 0.05) would
// otherwise allocate on the order of gigabytes at startup and OOM a Pi. One
// million points per series (~7M points, well under 1 GiB total) is far more
// than any real dashboard use case needs.
const maxHistoryCapacity = 1_000_000

// validLogLevels are the log levels newLogger understands; any other value
// silently falls back to info, so we reject it here instead.
var validLogLevels = map[string]bool{
	"debug": true,
	"info":  true,
	"warn":  true,
	"error": true,
}

// Validate checks the resolved configuration for values that would crash the
// service (e.g. a zero poll interval panics time.NewTicker) or silently make
// it misbehave. It returns a descriptive error for the first violation so a
// daemon fails fast at startup rather than later at runtime.
func (c Config) Validate() error {
	if c.PollIntervalSeconds <= 0 {
		return fmt.Errorf("poll_interval_seconds must be > 0 (got %v)", c.PollIntervalSeconds)
	}
	if c.UpdatesCheckMinutes <= 0 {
		return fmt.Errorf("updates_check_minutes must be > 0 (got %v)", c.UpdatesCheckMinutes)
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
	if err := c.Thresholds.validate(); err != nil {
		return err
	}
	if c.Alerts.ForSeconds < 0 {
		return fmt.Errorf("alerts.for_seconds must be >= 0 (got %v)", c.Alerts.ForSeconds)
	}
	if err := c.Alerts.validate(); err != nil {
		return err
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
		name       string
		warn, crit float64
	}{
		{"temperature", t.TemperatureWarnC, t.TemperatureCritC},
		{"cpu", t.CPUWarnPercent, t.CPUCritPercent},
		{"disk", t.DiskWarnPercent, t.DiskCritPercent},
		{"swap", t.SwapWarnPercent, t.SwapCritPercent},
		{"memory", t.MemoryWarnPercent, t.MemoryCritPercent},
	}
	for _, p := range pairs {
		if p.warn < 0 {
			return fmt.Errorf("thresholds.%s_warn must be >= 0 (got %v)", p.name, p.warn)
		}
		if p.crit < 0 {
			return fmt.Errorf("thresholds.%s_crit must be >= 0 (got %v)", p.name, p.crit)
		}
		if p.warn > p.crit {
			return fmt.Errorf("thresholds.%s_warn (%v) must be <= %s_crit (%v)", p.name, p.warn, p.name, p.crit)
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
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	// An empty or comment-only file has nothing to decode; Decode reports
	// that as io.EOF, but it's not an error here since cfg is left as-is.
	if err := dec.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("parse %s: %w", path, err)
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
