// Package config loads PiMonitor's runtime configuration from defaults, an
// optional YAML file, and CLI flags, in that order of increasing
// precedence.
package config

import (
	"flag"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

// Thresholds are the warn/critical cutoffs used both to color-code the web
// dashboard and to size the load-average gauges.
type Thresholds struct {
	TemperatureWarnC float64 `yaml:"temperature_warn_c"`
	TemperatureCritC float64 `yaml:"temperature_crit_c"`
	CPUWarnPercent   float64 `yaml:"cpu_warn_percent"`
	CPUCritPercent   float64 `yaml:"cpu_crit_percent"`
	DiskWarnPercent  float64 `yaml:"disk_warn_percent"`
	DiskCritPercent  float64 `yaml:"disk_crit_percent"`
	SwapWarnPercent  float64 `yaml:"swap_warn_percent"`
	SwapCritPercent  float64 `yaml:"swap_crit_percent"`
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
			TemperatureWarnC: 60,
			TemperatureCritC: 75,
			CPUWarnPercent:   80,
			CPUCritPercent:   95,
			DiskWarnPercent:  80,
			DiskCritPercent:  95,
			SwapWarnPercent:  50,
			SwapCritPercent:  90,
		},
		Alerts: Alerts{
			Enabled:    true,
			ForSeconds: 30,
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
// untouched, since yaml.Unmarshal only writes fields it finds in the
// document.
func loadYAMLFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
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
