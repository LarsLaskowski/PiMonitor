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

// Config is PiMonitor's full runtime configuration.
type Config struct {
	ListenAddr                   string     `yaml:"listen_addr"`
	LogLevel                     string     `yaml:"log_level"`
	PollIntervalSeconds          float64    `yaml:"poll_interval_seconds"`
	UpdatesCheckMinutes          float64    `yaml:"updates_check_minutes"`
	UpdatesStaleThresholdMinutes float64    `yaml:"updates_stale_threshold_minutes"`
	HistoryWindowMinutes         float64    `yaml:"history_window_minutes"`
	NetworkEnabled               bool       `yaml:"network_enabled"`
	DistroInfoEnabled            bool       `yaml:"distro_info_enabled"`
	PiModelEnabled               bool       `yaml:"pi_model_enabled"`
	APIKey                       string     `yaml:"api_key"`
	Thresholds                   Thresholds `yaml:"thresholds"`
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
	}
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

	return Result{Config: cfg, VersionRequested: *showVersion}, nil
}
