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
// dashboard and to size the load-average gauges.
type Thresholds struct {
	TemperatureWarnC  float64 `yaml:"temperature_warn_c"`
	TemperatureCritC  float64 `yaml:"temperature_crit_c"`
	CPUWarnPercent    float64 `yaml:"cpu_warn_percent"`
	CPUCritPercent    float64 `yaml:"cpu_crit_percent"`
	DiskWarnPercent   float64 `yaml:"disk_warn_percent"`
	DiskCritPercent   float64 `yaml:"disk_crit_percent"`
	SwapWarnPercent   float64 `yaml:"swap_warn_percent"`
	SwapCritPercent   float64 `yaml:"swap_crit_percent"`
	MemoryWarnPercent float64 `yaml:"memory_warn_percent"`
	MemoryCritPercent float64 `yaml:"memory_crit_percent"`
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
	ListenAddr                   string     `yaml:"listen_addr"`
	LogLevel                     string     `yaml:"log_level"`
	PollIntervalSeconds          float64    `yaml:"poll_interval_seconds"`
	UpdatesCheckMinutes          float64    `yaml:"updates_check_minutes"`
	UpdatesStaleThresholdMinutes float64    `yaml:"updates_stale_threshold_minutes"`
	HistoryWindowMinutes         float64    `yaml:"history_window_minutes"`
	HistoryPersistEnabled        bool       `yaml:"history_persist_enabled"`
	DataDir                      string     `yaml:"data_dir"`
	NetworkEnabled               bool       `yaml:"network_enabled"`
	DistroInfoEnabled            bool       `yaml:"distro_info_enabled"`
	PiModelEnabled               bool       `yaml:"pi_model_enabled"`
	APIKey                       string     `yaml:"api_key"`
	Thresholds                   Thresholds `yaml:"thresholds"`
	Alerts                       Alerts     `yaml:"alerts"`
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
		APIKey:                       "",
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
	if c.HistoryPersistEnabled && c.DataDir == "" {
		return fmt.Errorf("data_dir must not be empty when history_persist_enabled is true")
	}
	if c.ListenAddr == "" {
		return fmt.Errorf("listen_addr must not be empty")
	}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("log_level must be one of debug, info, warn, error (got %q)", c.LogLevel)
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

// Load resolves configuration from defaults, an optional YAML file
// (-config), and flag overrides, in that order of increasing precedence.
func Load(args []string) (Result, error) {
	cfg := Default()

	fs := flag.NewFlagSet("pimonitor", flag.ContinueOnError)
	configPath := fs.String("config", "", "path to YAML config file")
	listenAddr := fs.String("listen", "", "override listen address, e.g. :8080")
	logLevel := fs.String("log-level", "", "override log level (debug, info, warn, error)")
	apiKey := fs.String("api-key", "", "override REST API key")
	showVersion := fs.Bool("version", false, "print version and exit")

	if err := fs.Parse(args); err != nil {
		return Result{}, err
	}

	if *configPath != "" {
		if err := loadYAMLFile(*configPath, &cfg); err != nil {
			return Result{}, err
		}
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
