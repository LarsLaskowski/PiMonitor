package config

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.ListenAddr != ":8080" {
		t.Fatalf("ListenAddr = %q, want :8080", cfg.ListenAddr)
	}
	if !cfg.NetworkEnabled {
		t.Fatal("expected NetworkEnabled to default to true")
	}
	if !cfg.HwmonEnabled {
		t.Fatal("expected HwmonEnabled to default to true")
	}
	if cfg.PrometheusEnabled {
		t.Fatal("expected PrometheusEnabled to default to false")
	}
	if cfg.APIKey != "" {
		t.Fatal("expected APIKey to default to empty (no auth)")
	}
	if !cfg.AccessLogEnabled {
		t.Fatal("expected AccessLogEnabled to default to true")
	}
	if !cfg.HistoryPersistEnabled {
		t.Fatal("expected HistoryPersistEnabled to default to true")
	}
	if cfg.DataDir != "/var/lib/pimonitor" {
		t.Fatalf("DataDir = %q, want /var/lib/pimonitor", cfg.DataDir)
	}
	if !cfg.Alerts.Enabled {
		t.Fatal("expected Alerts.Enabled to default to true")
	}
	if cfg.Alerts.ForSeconds != 30 {
		t.Fatalf("Alerts.ForSeconds = %v, want 30", cfg.Alerts.ForSeconds)
	}
	if cfg.ProcessesEnabled {
		t.Fatal("expected ProcessesEnabled to default to false")
	}
	if cfg.ProcessesTopN != 5 {
		t.Fatalf("ProcessesTopN = %v, want 5", cfg.ProcessesTopN)
	}
}

func TestDurationHelpers(t *testing.T) {
	cfg := Default()
	cfg.PollIntervalSeconds = 5
	cfg.UpdatesCheckMinutes = 15
	cfg.UpdatesStaleThresholdMinutes = 720
	cfg.HistoryWindowMinutes = 60

	if cfg.FastInterval() != 5*time.Second {
		t.Fatalf("FastInterval = %v, want 5s", cfg.FastInterval())
	}
	if cfg.SlowInterval() != 15*time.Minute {
		t.Fatalf("SlowInterval = %v, want 15m", cfg.SlowInterval())
	}
	if cfg.UpdatesStaleThreshold() != 12*time.Hour {
		t.Fatalf("UpdatesStaleThreshold = %v, want 12h", cfg.UpdatesStaleThreshold())
	}
	if got, want := cfg.HistoryCapacity(), 720; got != want {
		t.Fatalf("HistoryCapacity = %d, want %d", got, want)
	}
	if cfg.HistoryWindow() != time.Hour {
		t.Fatalf("HistoryWindow = %v, want 1h", cfg.HistoryWindow())
	}
	cfg.Alerts.ForSeconds = 30
	if cfg.AlertFor() != 30*time.Second {
		t.Fatalf("AlertFor = %v, want 30s", cfg.AlertFor())
	}
}

func TestHealthzMaxStaleness(t *testing.T) {
	tests := []struct {
		name         string
		pollSeconds  float64
		staleSeconds float64
		tickOverhead time.Duration
		want         time.Duration
	}{
		{
			name:         "default derives from poll interval alone when tick overhead is zero",
			pollSeconds:  5,
			staleSeconds: 0,
			tickOverhead: 0,
			want:         15 * time.Second, // 3x poll interval
		},
		{
			name:         "default adds 2x tick overhead on top of the poll-interval multiple",
			pollSeconds:  5,
			staleSeconds: 0,
			tickOverhead: 12 * time.Second, // a non-zero overhead, as collector.WorstCaseTickOverhead is in production
			want:         15*time.Second + 24*time.Second,
		},
		{
			name:         "explicit value overrides the default and ignores tick overhead",
			pollSeconds:  5,
			staleSeconds: 42,
			tickOverhead: 12 * time.Second,
			want:         42 * time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.PollIntervalSeconds = tt.pollSeconds
			cfg.HealthzMaxStalenessSeconds = tt.staleSeconds

			if got := cfg.HealthzMaxStaleness(tt.tickOverhead); got != tt.want {
				t.Fatalf("HealthzMaxStaleness(%v) = %v, want %v", tt.tickOverhead, got, tt.want)
			}
		})
	}
}

func TestHistoryCapacity_ZeroPollInterval(t *testing.T) {
	cfg := Default()
	cfg.PollIntervalSeconds = 0
	if got := cfg.HistoryCapacity(); got != 1 {
		t.Fatalf("HistoryCapacity with zero poll interval = %d, want 1 (safe minimum)", got)
	}
}

func TestLoad_DefaultsWithNoArgs(t *testing.T) {
	result, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(result.Config, Default()) {
		t.Fatalf("Load with no args = %+v, want defaults %+v", result.Config, Default())
	}
	if result.VersionRequested {
		t.Fatal("VersionRequested should be false by default")
	}
}

func TestLoad_YAMLFilePartialOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yamlContent := `
listen_addr: ":9090"
network_enabled: false
hwmon_enabled: false
prometheus_enabled: true
thresholds:
  temperature_warn_c: 55
`
	writeFile(t, path, yamlContent)

	result, err := Load([]string{"-config", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	cfg := result.Config

	if cfg.ListenAddr != ":9090" {
		t.Fatalf("ListenAddr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.NetworkEnabled {
		t.Fatal("expected NetworkEnabled to be overridden to false")
	}
	if cfg.HwmonEnabled {
		t.Fatal("expected HwmonEnabled to be overridden to false")
	}
	if !cfg.PrometheusEnabled {
		t.Fatal("expected PrometheusEnabled to be overridden to true")
	}
	if cfg.Thresholds.TemperatureWarnC != 55 {
		t.Fatalf("Thresholds.TemperatureWarnC = %v, want 55", cfg.Thresholds.TemperatureWarnC)
	}
	// Fields not present in the YAML must retain their defaults.
	if cfg.Thresholds.TemperatureCritC != Default().Thresholds.TemperatureCritC {
		t.Fatalf("Thresholds.TemperatureCritC = %v, want default %v", cfg.Thresholds.TemperatureCritC, Default().Thresholds.TemperatureCritC)
	}
	if cfg.PollIntervalSeconds != Default().PollIntervalSeconds {
		t.Fatalf("PollIntervalSeconds = %v, want default %v", cfg.PollIntervalSeconds, Default().PollIntervalSeconds)
	}
}

func TestLoad_TLSFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "tls_cert: \"/etc/pimonitor/cert.pem\"\ntls_key: \"/etc/pimonitor/key.pem\"\n")

	result, err := Load([]string{"-config", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if result.Config.TLSCertFile != "/etc/pimonitor/cert.pem" {
		t.Fatalf("TLSCertFile = %q, want /etc/pimonitor/cert.pem", result.Config.TLSCertFile)
	}
	if result.Config.TLSKeyFile != "/etc/pimonitor/key.pem" {
		t.Fatalf("TLSKeyFile = %q, want /etc/pimonitor/key.pem", result.Config.TLSKeyFile)
	}
}

func TestLoad_AccessLogEnabledFromYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "access_log_enabled: false\n")

	result, err := Load([]string{"-config", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if result.Config.AccessLogEnabled {
		t.Fatal("expected AccessLogEnabled to be overridden to false")
	}
}

func TestLoad_TLSOnlyCertIsRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "tls_cert: \"/etc/pimonitor/cert.pem\"\n")

	if _, err := Load([]string{"-config", path}); err == nil {
		t.Fatal("expected Load to reject tls_cert set without tls_key")
	}
}

func TestLoad_FlagsOverrideFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, `listen_addr: ":9090"`+"\n")

	result, err := Load([]string{"-config", path, "-listen", ":7070"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Config.ListenAddr != ":7070" {
		t.Fatalf("ListenAddr = %q, want flag override :7070", result.Config.ListenAddr)
	}
}

func TestLoad_MissingConfigFileIsError(t *testing.T) {
	_, err := Load([]string{"-config", "/does/not/exist.yaml"})
	if err == nil {
		t.Fatal("expected error for explicitly specified but missing config file")
	}
}

func TestLoad_MalformedYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "not: valid: yaml: [")

	_, err := Load([]string{"-config", path})
	if err == nil {
		t.Fatal("expected error for malformed YAML")
	}
}

func TestLoad_UnknownTopLevelKeyIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// "api_kay" is a typo of "api_key"; it must not be silently ignored,
	// since that would leave APIKey at its default (no authentication).
	writeFile(t, path, "api_kay: \"secret123\"\n")

	_, err := Load([]string{"-config", path})
	if err == nil {
		t.Fatal("expected error for unknown top-level YAML key")
	}
}

func TestLoad_UnknownNestedKeyIsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "thresholds:\n  temperature_warn_percent: 55\n")

	_, err := Load([]string{"-config", path})
	if err == nil {
		t.Fatal("expected error for unknown nested YAML key")
	}
}

func TestLoad_EmptyConfigFileIsNotError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "# just a comment, no keys\n")

	result, err := Load([]string{"-config", path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(result.Config, Default()) {
		t.Fatalf("Load with empty/comment-only file = %+v, want defaults %+v", result.Config, Default())
	}
}

func TestLoad_VersionFlag(t *testing.T) {
	result, err := Load([]string{"-version"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !result.VersionRequested {
		t.Fatal("expected VersionRequested to be true")
	}
}

func TestLoad_APIKeyOverride(t *testing.T) {
	result, err := Load([]string{"-api-key", "secret123"})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Config.APIKey != "secret123" {
		t.Fatalf("APIKey = %q, want secret123", result.Config.APIKey)
	}
}

// envFunc builds a lookupEnv stub for load() from a fixed environment.
func envFunc(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

// noopUnsetEnv stands in for load()'s unsetEnv parameter in tests that use
// envFunc's fake lookupEnv rather than the real process environment, since
// there is nothing real for it to unset.
func noopUnsetEnv(string) {}

func TestLoad_APIKeyFromEnv(t *testing.T) {
	result, err := load(nil, envFunc(map[string]string{APIKeyEnvVar: "env-secret"}), noopUnsetEnv)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.APIKey != "env-secret" {
		t.Fatalf("APIKey = %q, want env-secret", result.Config.APIKey)
	}
}

func TestLoad_APIKeyEnvOverridesConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "api_key: \"file-secret\"\n")

	result, err := load([]string{"-config", path}, envFunc(map[string]string{APIKeyEnvVar: "env-secret"}), noopUnsetEnv)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.APIKey != "env-secret" {
		t.Fatalf("APIKey = %q, want env-secret (env must override the config file)", result.Config.APIKey)
	}
}

func TestLoad_APIKeyFlagOverridesEnv(t *testing.T) {
	result, err := load([]string{"-api-key", "flag-secret"}, envFunc(map[string]string{APIKeyEnvVar: "env-secret"}), noopUnsetEnv)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.APIKey != "flag-secret" {
		t.Fatalf("APIKey = %q, want flag-secret (flag must override the env)", result.Config.APIKey)
	}
}

func TestLoad_APIKeyEmptyEnvKeepsConfigFileValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "api_key: \"file-secret\"\n")

	// An exported-but-empty PIMONITOR_API_KEY must not silently disable the
	// authentication configured in the file.
	result, err := load([]string{"-config", path}, envFunc(map[string]string{APIKeyEnvVar: ""}), noopUnsetEnv)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if result.Config.APIKey != "file-secret" {
		t.Fatalf("APIKey = %q, want file-secret", result.Config.APIKey)
	}
}

func TestLoad_UnsetsAPIKeyEnvVar(t *testing.T) {
	tests := []struct {
		name      string
		env       map[string]string
		wantUnset bool
	}{
		{"absent from env: not unset (nothing to unset)", map[string]string{}, false},
		{"present but empty: unset even though APIKey is untouched", map[string]string{APIKeyEnvVar: ""}, true},
		{"present and non-empty: unset after being read into cfg", map[string]string{APIKeyEnvVar: "env-secret"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var unset []string
			_, err := load(nil, envFunc(tt.env), func(key string) { unset = append(unset, key) })
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			gotUnset := len(unset) == 1 && unset[0] == APIKeyEnvVar
			if gotUnset != tt.wantUnset {
				t.Fatalf("unsetEnv calls = %v, want unset=%v", unset, tt.wantUnset)
			}
		})
	}
}

// TestLoad_ReadsAPIKeyEnvVar covers the wiring of the exported Load to the
// real process environment, which the load() tests above deliberately bypass.
func TestLoad_ReadsAPIKeyEnvVar(t *testing.T) {
	t.Setenv(APIKeyEnvVar, "env-secret")

	result, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Config.APIKey != "env-secret" {
		t.Fatalf("APIKey = %q, want env-secret", result.Config.APIKey)
	}
}

// TestLoad_UnsetsAPIKeyEnvVarInRealEnvironment covers the wiring of the
// exported Load to the real os.Unsetenv, which the load() tests above
// deliberately bypass (see noopUnsetEnv). It must not call t.Parallel(): the
// testing package's t.Setenv panics if used by a parallel (sub)test, since
// t.Setenv captures and restores the pre-test value on Cleanup, which is not
// safe to interleave with other tests touching the same process-wide
// environment.
//
// t.Setenv's own Cleanup runs after this test's body, restoring
// PIMONITOR_API_KEY to its pre-test state (unset) via os.Unsetenv again;
// that happens regardless of whether Load already unset it, so the two
// unsets don't conflict.
func TestLoad_UnsetsAPIKeyEnvVarInRealEnvironment(t *testing.T) {
	t.Setenv(APIKeyEnvVar, "env-secret")

	result, err := Load(nil)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Config.APIKey != "env-secret" {
		t.Fatalf("APIKey = %q, want env-secret", result.Config.APIKey)
	}

	if v, ok := os.LookupEnv(APIKeyEnvVar); ok {
		t.Fatalf("expected %s to be unset from the process environment after Load, still found %q", APIKeyEnvVar, v)
	}
}

func TestValidate_DefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() must pass Validate(): %v", err)
	}
}

func TestValidate_RejectsBadValues(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{"zero poll interval", func(c *Config) { c.PollIntervalSeconds = 0 }},
		{"negative poll interval", func(c *Config) { c.PollIntervalSeconds = -1 }},
		{"zero updates check", func(c *Config) { c.UpdatesCheckMinutes = 0 }},
		{"negative updates stale threshold", func(c *Config) { c.UpdatesStaleThresholdMinutes = -1 }},
		{"zero history window", func(c *Config) { c.HistoryWindowMinutes = 0 }},
		{"persistence enabled with empty data dir", func(c *Config) { c.HistoryPersistEnabled = true; c.DataDir = "" }},
		{"empty listen addr", func(c *Config) { c.ListenAddr = "" }},
		{"unknown log level", func(c *Config) { c.LogLevel = "verbose" }},
		{"negative healthz max staleness", func(c *Config) { c.HealthzMaxStalenessSeconds = -1 }},
		{"healthz max staleness below poll interval", func(c *Config) {
			c.PollIntervalSeconds = 5
			c.HealthzMaxStalenessSeconds = 2
		}},
		{"negative temperature warn", func(c *Config) { c.Thresholds.TemperatureWarnC = -1 }},
		{"temperature warn above crit", func(c *Config) { c.Thresholds.TemperatureWarnC = 90 }},
		{"cpu warn above crit", func(c *Config) { c.Thresholds.CPUWarnPercent = 99 }},
		{"disk warn above crit", func(c *Config) { c.Thresholds.DiskWarnPercent = 99 }},
		{"swap warn above crit", func(c *Config) { c.Thresholds.SwapWarnPercent = 99 }},
		{"memory warn above crit", func(c *Config) { c.Thresholds.MemoryWarnPercent = 99 }},
		{"history capacity exceeds sanity cap", func(c *Config) {
			c.HistoryWindowMinutes = 525600
			c.PollIntervalSeconds = 1
		}},
		{"negative alerts for_seconds", func(c *Config) { c.Alerts.ForSeconds = -1 }},
		{"negative notify max retries", func(c *Config) { c.Alerts.NotifyMaxRetries = -1 }},
		{"negative notify backoff", func(c *Config) { c.Alerts.NotifyRetryBackoffSeconds = -1 }},
		{"negative notify min interval", func(c *Config) { c.Alerts.NotifyMinIntervalSeconds = -1 }},
		{"zero processes top n", func(c *Config) { c.ProcessesTopN = 0 }},
		{"negative processes top n", func(c *Config) { c.ProcessesTopN = -1 }},
		{"processes top n exceeds sanity cap", func(c *Config) { c.ProcessesTopN = maxProcessesTopN + 1 }},
		{"webhook with empty url", func(c *Config) { c.Alerts.Webhooks = []Webhook{{URL: ""}} }},
		{"webhook with bad min_level", func(c *Config) {
			c.Alerts.Webhooks = []Webhook{{URL: "http://x", MinLevel: "info"}}
		}},
		{"webhook with negative timeout", func(c *Config) {
			c.Alerts.Webhooks = []Webhook{{URL: "http://x", TimeoutSeconds: -1}}
		}},
		{"tls_cert set without tls_key", func(c *Config) { c.TLSCertFile = "cert.pem" }},
		{"tls_key set without tls_cert", func(c *Config) { c.TLSKeyFile = "key.pem" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() accepted invalid config (%s)", tt.name)
			}
		})
	}
}

func TestValidate_ThresholdErrorsNameYAMLKey(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Thresholds)
		want   string
	}{
		{"AC1 temperature_warn_c", func(th *Thresholds) { th.TemperatureWarnC = -1 },
			"thresholds.temperature_warn_c must be >= 0 (got -1)"},
		{"AC1 cpu_warn_percent", func(th *Thresholds) { th.CPUWarnPercent = -1 },
			"thresholds.cpu_warn_percent must be >= 0 (got -1)"},
		{"AC1 disk_warn_percent", func(th *Thresholds) { th.DiskWarnPercent = -1 },
			"thresholds.disk_warn_percent must be >= 0 (got -1)"},
		{"AC1 swap_warn_percent", func(th *Thresholds) { th.SwapWarnPercent = -1 },
			"thresholds.swap_warn_percent must be >= 0 (got -1)"},
		{"AC1 memory_warn_percent", func(th *Thresholds) { th.MemoryWarnPercent = -1 },
			"thresholds.memory_warn_percent must be >= 0 (got -1)"},
		{"AC2 temperature_crit_c", func(th *Thresholds) { th.TemperatureCritC = -1 },
			"thresholds.temperature_crit_c must be >= 0 (got -1)"},
		{"AC2 cpu_crit_percent", func(th *Thresholds) { th.CPUCritPercent = -1 },
			"thresholds.cpu_crit_percent must be >= 0 (got -1)"},
		{"AC2 disk_crit_percent", func(th *Thresholds) { th.DiskCritPercent = -1 },
			"thresholds.disk_crit_percent must be >= 0 (got -1)"},
		{"AC2 swap_crit_percent", func(th *Thresholds) { th.SwapCritPercent = -1 },
			"thresholds.swap_crit_percent must be >= 0 (got -1)"},
		{"AC2 memory_crit_percent", func(th *Thresholds) { th.MemoryCritPercent = -1 },
			"thresholds.memory_crit_percent must be >= 0 (got -1)"},
		{"AC3 temperature_warn_c", func(th *Thresholds) { th.TemperatureWarnC = 1000 },
			"thresholds.temperature_warn_c (1000) must be <= thresholds.temperature_crit_c (75)"},
		{"AC3 cpu_warn_percent", func(th *Thresholds) { th.CPUWarnPercent = 1000 },
			"thresholds.cpu_warn_percent (1000) must be <= thresholds.cpu_crit_percent (95)"},
		{"AC3 disk_warn_percent", func(th *Thresholds) { th.DiskWarnPercent = 1000 },
			"thresholds.disk_warn_percent (1000) must be <= thresholds.disk_crit_percent (95)"},
		{"AC3 swap_warn_percent", func(th *Thresholds) { th.SwapWarnPercent = 1000 },
			"thresholds.swap_warn_percent (1000) must be <= thresholds.swap_crit_percent (90)"},
		{"AC3 memory_warn_percent", func(th *Thresholds) { th.MemoryWarnPercent = 1000 },
			"thresholds.memory_warn_percent (1000) must be <= thresholds.memory_crit_percent (95)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg.Thresholds)

			err := cfg.Validate()

			if err == nil {
				t.Fatalf("Validate() = nil, want error %q", tt.want)
			}
			if err.Error() != tt.want {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}

func TestValidate_TLS(t *testing.T) {
	tests := []struct {
		name        string
		certFile    string
		keyFile     string
		wantErr     bool
		description string
	}{
		{"both set", "cert.pem", "key.pem", false, "cert and key both configured enables TLS and is valid"},
		{"neither set", "", "", false, "the default (plain HTTP) is valid"},
		{"only cert set", "cert.pem", "", true, "cert without key is a config error"},
		{"only key set", "", "key.pem", true, "key without cert is a config error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.TLSCertFile = tt.certFile
			cfg.TLSKeyFile = tt.keyFile

			err := cfg.Validate()

			if tt.wantErr && err == nil {
				t.Fatalf("Validate() = nil, want error (%s)", tt.description)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Validate() = %v, want nil (%s)", err, tt.description)
			}
		})
	}
}

func TestValidate_HistoryCapacityBoundary(t *testing.T) {
	cfg := Default()
	// With poll_interval_seconds: 60, HistoryCapacity() equals
	// history_window_minutes exactly, giving clean integer boundaries.
	// With poll_interval_seconds: 30, HistoryCapacity() is twice the window in
	// minutes: 500000 minutes give exactly 1,000,000 points, and the window
	// stays under its own upper bound (history_window_minutes <= 525600).
	cfg.PollIntervalSeconds = 30

	cfg.HistoryWindowMinutes = maxHistoryCapacity / 2 // exactly at the cap
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected history capacity exactly at the cap: %v", err)
	}

	cfg.HistoryWindowMinutes = maxHistoryCapacity/2 + 0.5 // one point over
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() accepted history capacity one point over the cap")
	}
}

func TestValidate_AcceptsValidEdgeCases(t *testing.T) {
	cfg := Default()
	// Warn equal to crit and a zero stale threshold are both allowed.
	cfg.Thresholds.TemperatureWarnC = cfg.Thresholds.TemperatureCritC
	cfg.UpdatesStaleThresholdMinutes = 0
	// An empty data_dir is fine as long as persistence is disabled.
	cfg.HistoryPersistEnabled = false
	cfg.DataDir = ""
	// A well-formed webhook with an empty min_level (defaults to warn) is valid.
	cfg.Alerts.Webhooks = []Webhook{{URL: "https://example.com/hook"}}
	// healthz_max_staleness_seconds exactly equal to poll_interval_seconds is
	// the smallest accepted value, not rejected as "below" it.
	cfg.HealthzMaxStalenessSeconds = cfg.PollIntervalSeconds
	// processes_top_n exactly at the cap is allowed, whether or not the
	// feature is enabled.
	cfg.ProcessesEnabled = true
	cfg.ProcessesTopN = maxProcessesTopN
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected a valid edge-case config: %v", err)
	}
}

func TestLoad_RejectsZeroPollInterval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "poll_interval_seconds: 0\n")

	_, err := Load([]string{"-config", path})
	if err == nil {
		t.Fatal("expected Load to reject poll_interval_seconds: 0")
	}
}

func TestLoad_VersionFlagSkipsValidation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	writeFile(t, path, "poll_interval_seconds: 0\n")

	result, err := Load([]string{"-config", path, "-version"})
	if err != nil {
		t.Fatalf("expected -version to bypass validation, got: %v", err)
	}
	if !result.VersionRequested {
		t.Fatal("expected VersionRequested to be true")
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDefault_HostnameEnabled(t *testing.T) {
	t.Run("AC3 defaults to true", func(t *testing.T) {
		if !Default().HostnameEnabled {
			t.Fatal("Default().HostnameEnabled = false, want true")
		}
	})
}

func TestLoad_HostnameEnabledFromYAML(t *testing.T) {
	t.Run("AC3 a file without the key keeps true", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeFile(t, path, "pi_model_enabled: false\n")

		result, err := Load([]string{"-config", path})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if !result.Config.HostnameEnabled {
			t.Fatal("HostnameEnabled = false for a file without hostname_enabled, want true (default)")
		}
	})
	t.Run("AC3 false is parsed", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		writeFile(t, path, "hostname_enabled: false\n")

		result, err := Load([]string{"-config", path})
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if result.Config.HostnameEnabled {
			t.Fatal("HostnameEnabled = true for hostname_enabled: false, want false")
		}
	})
}

// loadYAMLText writes content to a config file, loads it with the extra args
// and returns the result and error.
func loadYAMLText(t *testing.T, content string, extraArgs ...string) (Result, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, content)
	return Load(append([]string{"-config", path}, extraArgs...))
}

// numberOptionYAML maps each option of AC1/AC4 to a YAML document with %s as
// the value, and the option name the error must use.
var numberOptionYAML = []struct {
	option string
	doc    string
}{
	{"poll_interval_seconds", "poll_interval_seconds: %s\n"},
	{"thresholds.cpu_warn_percent", "thresholds:\n  cpu_warn_percent: %s\n"},
	{"alerts.webhooks[0].timeout_seconds", "alerts:\n  webhooks:\n    - url: http://example.invalid/hook\n      min_level: warn\n      timeout_seconds: %s\n"},
	{"processes_top_n", "processes_top_n: %s\n"},
	{"alerts.notify_max_retries", "alerts:\n  notify_max_retries: %s\n"},
}

func TestLoad_RejectsNonFiniteNumbers(t *testing.T) {
	literals := []string{
		".nan", ".NaN", ".NAN",
		".inf", ".Inf", ".INF",
		"+.inf", "+.Inf", "+.INF",
		"-.inf", "-.Inf", "-.INF",
	}
	for _, opt := range numberOptionYAML {
		for _, lit := range literals {
			t.Run(fmt.Sprintf("AC1 AC4 %s %s", opt.option, lit), func(t *testing.T) {
				_, err := loadYAMLText(t, fmt.Sprintf(opt.doc, lit))

				want := opt.option + " must be a finite number"
				if err == nil || err.Error() != want {
					t.Fatalf("Load error = %v, want %q", err, want)
				}
			})
		}
	}
}

func TestLoad_RejectsFractionalIntegerOptions(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		args []string
		want string
	}{
		{"AC2 plain processes_top_n", "processes_top_n: 2.5\n", nil,
			"processes_top_n must be a whole number (got 2.5)"},
		{"AC2 plain notify_max_retries", "alerts:\n  notify_max_retries: 0.5\n", nil,
			"alerts.notify_max_retries must be a whole number (got 0.5)"},
		{"AC2 alias into processes_top_n", "poll_interval_seconds: &n 2.5\nprocesses_top_n: *n\n", nil,
			"processes_top_n must be a whole number (got 2.5)"},
		{"AC2 alias into notify_max_retries", "poll_interval_seconds: &n 2.5\nalerts: {notify_max_retries: *n}\n", nil,
			"alerts.notify_max_retries must be a whole number (got 2.5)"},
		{"AC2 merge key", "alerts: {<<: {notify_max_retries: 1.5}}\n", nil,
			"alerts.notify_max_retries must be a whole number (got 1.5)"},
		{"AC2 float tag", "processes_top_n: !!float 2.5\n", nil,
			"processes_top_n must be a whole number (got 2.5)"},
		{"AC2 with -version processes_top_n", "processes_top_n: 2.5\n", []string{"-version"},
			"processes_top_n must be a whole number (got 2.5)"},
		{"AC2 with -version notify_max_retries", "alerts:\n  notify_max_retries: 0.5\n", []string{"-version"},
			"alerts.notify_max_retries must be a whole number (got 0.5)"},
		{"AC1 AC2 with -version non-finite integer option", "processes_top_n: .nan\n", []string{"-version"},
			"processes_top_n must be a finite number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadYAMLText(t, tt.yaml, tt.args...)

			if err == nil || err.Error() != tt.want {
				t.Fatalf("Load error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoad_AcceptsWholeNumbersWrittenAsDecimals(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		get  func(Config) float64
		want float64
	}{
		{"AC3 processes_top_n 5", "processes_top_n: 5\n", func(c Config) float64 { return float64(c.ProcessesTopN) }, 5},
		{"AC3 processes_top_n 5.0", "processes_top_n: 5.0\n", func(c Config) float64 { return float64(c.ProcessesTopN) }, 5},
		{"AC3 processes_top_n 1e1", "processes_top_n: 1e1\n", func(c Config) float64 { return float64(c.ProcessesTopN) }, 10},
		{"AC3 processes_top_n float tag", "processes_top_n: !!float 5\n", func(c Config) float64 { return float64(c.ProcessesTopN) }, 5},
		{"AC3 notify_max_retries 3.0", "alerts:\n  notify_max_retries: 3.0\n", func(c Config) float64 { return float64(c.Alerts.NotifyMaxRetries) }, 3},
		{"AC3 poll_interval_seconds 5", "poll_interval_seconds: 5\n", func(c Config) float64 { return c.PollIntervalSeconds }, 5},
		{"AC3 poll_interval_seconds 5.0", "poll_interval_seconds: 5.0\n", func(c Config) float64 { return c.PollIntervalSeconds }, 5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := loadYAMLText(t, tt.yaml)

			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tt.get(result.Config); got != tt.want {
				t.Fatalf("value = %v, want %v", got, tt.want)
			}
		})
	}
}

// floatOptionSetters set every float option of AC4 on a Config; the webhook
// options set the webhook at index 1.
var floatOptionSetters = []struct {
	option string
	set    func(*Config, float64)
}{
	{"poll_interval_seconds", func(c *Config, v float64) { c.PollIntervalSeconds = v }},
	{"updates_check_minutes", func(c *Config, v float64) { c.UpdatesCheckMinutes = v }},
	{"updates_stale_threshold_minutes", func(c *Config, v float64) { c.UpdatesStaleThresholdMinutes = v }},
	{"history_window_minutes", func(c *Config, v float64) { c.HistoryWindowMinutes = v }},
	{"healthz_max_staleness_seconds", func(c *Config, v float64) { c.HealthzMaxStalenessSeconds = v }},
	{"thresholds.temperature_warn_c", func(c *Config, v float64) { c.Thresholds.TemperatureWarnC = v }},
	{"thresholds.temperature_crit_c", func(c *Config, v float64) { c.Thresholds.TemperatureCritC = v }},
	{"thresholds.cpu_warn_percent", func(c *Config, v float64) { c.Thresholds.CPUWarnPercent = v }},
	{"thresholds.cpu_crit_percent", func(c *Config, v float64) { c.Thresholds.CPUCritPercent = v }},
	{"thresholds.disk_warn_percent", func(c *Config, v float64) { c.Thresholds.DiskWarnPercent = v }},
	{"thresholds.disk_crit_percent", func(c *Config, v float64) { c.Thresholds.DiskCritPercent = v }},
	{"thresholds.swap_warn_percent", func(c *Config, v float64) { c.Thresholds.SwapWarnPercent = v }},
	{"thresholds.swap_crit_percent", func(c *Config, v float64) { c.Thresholds.SwapCritPercent = v }},
	{"thresholds.memory_warn_percent", func(c *Config, v float64) { c.Thresholds.MemoryWarnPercent = v }},
	{"thresholds.memory_crit_percent", func(c *Config, v float64) { c.Thresholds.MemoryCritPercent = v }},
	{"alerts.for_seconds", func(c *Config, v float64) { c.Alerts.ForSeconds = v }},
	{"alerts.notify_retry_backoff_seconds", func(c *Config, v float64) { c.Alerts.NotifyRetryBackoffSeconds = v }},
	{"alerts.notify_min_interval_seconds", func(c *Config, v float64) { c.Alerts.NotifyMinIntervalSeconds = v }},
	{"alerts.webhooks[1].timeout_seconds", func(c *Config, v float64) { c.Alerts.Webhooks[1].TimeoutSeconds = v }},
	{"alerts.webhooks[0].timeout_seconds", func(c *Config, v float64) { c.Alerts.Webhooks[0].TimeoutSeconds = v }},
}

func TestValidate_RejectsNonFiniteNumbers(t *testing.T) {
	values := map[string]float64{"NaN": math.NaN(), "+Inf": math.Inf(1), "-Inf": math.Inf(-1)}
	for _, opt := range floatOptionSetters {
		for label, v := range values {
			t.Run(fmt.Sprintf("AC4 %s %s", opt.option, label), func(t *testing.T) {
				cfg := Default()
				cfg.Alerts.Webhooks = []Webhook{
					{URL: "http://example.invalid/a", MinLevel: "warn"},
					{URL: "http://example.invalid/b", MinLevel: "warn"},
				}
				opt.set(&cfg, v)

				err := cfg.Validate()

				want := opt.option + " must be a finite number"
				if err == nil || err.Error() != want {
					t.Fatalf("Validate() error = %v, want %q", err, want)
				}
			})
		}
	}
}

// intOptionPaths collects the yaml key path of every int-kind field of typ.
func intOptionPaths(typ reflect.Type, prefix string) []string {
	var paths []string
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		key := strings.Split(f.Tag.Get("yaml"), ",")[0]
		if key == "" {
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Slice {
			ft = ft.Elem()
			key += "[]"
		}
		switch ft.Kind() {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			paths = append(paths, prefix+key)
		case reflect.Struct:
			paths = append(paths, intOptionPaths(ft, prefix+key+".")...)
		}
	}
	return paths
}

func TestCheckIntegerOptions_CoversEveryIntField_AC4(t *testing.T) {
	got := intOptionPaths(reflect.TypeOf(Config{}), "")
	want := slices.Clone(integerOptionNames)
	slices.Sort(got)
	slices.Sort(want)

	if !slices.Equal(got, want) {
		t.Fatalf("integerOptionNames = %v, want every int field of Config = %v", want, got)
	}
}

func TestDefault_OpenHAB_AC7(t *testing.T) {
	want := OpenHAB{Enabled: "auto", BaseURL: "http://127.0.0.1:8080", APIToken: "", PollIntervalSeconds: 60, HistoryWindowHours: 24}

	got := Default().OpenHAB

	if got != want {
		t.Fatalf("Default().OpenHAB = %+v, want %+v", got, want)
	}
	if mode := got.Mode(); mode != "auto" {
		t.Fatalf("Default().OpenHAB.Mode() = %q, want auto", mode)
	}
	if interval := got.PollInterval(); interval != 60*time.Second {
		t.Fatalf("Default().OpenHAB.PollInterval() = %v, want 1m0s", interval)
	}
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default().Validate() = %v, want nil", err)
	}
}

func TestDefault_OpenHABHistoryWindowHours_AC3(t *testing.T) {
	if got := Default().OpenHAB.HistoryWindowHours; got != 24 {
		t.Fatalf("Default().OpenHAB.HistoryWindowHours = %d, want 24", got)
	}
}

func TestValidate_OpenHABHistoryWindowHours_AC3(t *testing.T) {
	hours := func(v int) func(*OpenHAB) { return func(o *OpenHAB) { o.HistoryWindowHours = v } }
	disabled := func(v int) func(*OpenHAB) {
		return func(o *OpenHAB) {
			o.Enabled = "false"
			o.HistoryWindowHours = v
		}
	}
	tests := []struct {
		name string
		set  func(*OpenHAB)
		want string
	}{
		{"AC3 hours 1", hours(1), ""},
		{"AC3 hours 24", hours(24), ""},
		{"AC3 hours 168", hours(168), ""},
		{"AC3 hours 0", hours(0), "openhab.history_window_hours must be between 1 and 168 (got 0)"},
		{"AC3 hours 169", hours(169), "openhab.history_window_hours must be between 1 and 168 (got 169)"},
		{"AC3 hours -1", hours(-1), "openhab.history_window_hours must be between 1 and 168 (got -1)"},
		{"AC3 hours 0 while openhab.enabled is false", disabled(0), "openhab.history_window_hours must be between 1 and 168 (got 0)"},
		{"AC3 hours 169 while openhab.enabled is false", disabled(169), "openhab.history_window_hours must be between 1 and 168 (got 169)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checkErrText(t, validateOpenHAB(tt.set), tt.want)
		})
	}
}

func TestLoad_OpenHABHistoryWindowHours_AC3(t *testing.T) {
	tests := []struct {
		name      string
		yaml      string
		wantHours int
		wantErr   string
	}{
		{"AC3 absent keeps 24", "openhab:\n  enabled: auto\n", 24, ""},
		{"AC3 168", "openhab:\n  history_window_hours: 168\n", 168, ""},
		{"AC3 whole number written as decimal", "openhab:\n  history_window_hours: 12.0\n", 12, ""},
		{"AC3 fraction", "openhab:\n  history_window_hours: 1.5\n", 0,
			"openhab.history_window_hours must be a whole number (got 1.5)"},
		{"AC3 fraction through alias", "hours: &n 2.5\nopenhab: {history_window_hours: *n}\n", 0,
			"openhab.history_window_hours must be a whole number (got 2.5)"},
		{"AC3 not finite", "openhab:\n  history_window_hours: .nan\n", 0,
			"openhab.history_window_hours must be a finite number"},
		{"AC3 below range from file", "openhab:\n  history_window_hours: 0\n", 0,
			"openhab.history_window_hours must be between 1 and 168 (got 0)"},
		{"AC3 above range from file", "openhab:\n  history_window_hours: 169\n", 0,
			"openhab.history_window_hours must be between 1 and 168 (got 169)"},
		{"AC3 above range while openhab.enabled is false", "openhab:\n  enabled: false\n  history_window_hours: 500\n", 0,
			"openhab.history_window_hours must be between 1 and 168 (got 500)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := loadYAMLText(t, tt.yaml)

			checkErrText(t, err, tt.wantErr)
			if got := result.Config.OpenHAB.HistoryWindowHours; got != tt.wantHours {
				t.Fatalf("HistoryWindowHours = %d, want %d", got, tt.wantHours)
			}
		})
	}
}

func TestOpenHAB_HistoryCapacity_AC3(t *testing.T) {
	tests := []struct {
		name         string
		hours, every int
		want         int
	}{
		{"AC3 24 h at 60 s", 24, 60, 1440},
		{"AC3 168 h at 60 s", 168, 60, 10080},
		{"AC3 1 h at 86400 s keeps one point", 1, 86400, 1},
		{"AC3 fraction is floored", 1, 7000, 1},
		{"AC3 2 h at 90 s", 2, 90, 80},
		{"AC3 non-positive interval gives 1", 24, 0, 1},
		{"AC3 negative interval gives 1", 24, -5, 1},
		{"AC3 capped at the history maximum", 1000, 1, maxHistoryCapacity},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := OpenHAB{HistoryWindowHours: tt.hours, PollIntervalSeconds: tt.every}

			if got := o.HistoryCapacity(); got != tt.want {
				t.Fatalf("HistoryCapacity() = %d, want %d", got, tt.want)
			}
		})
	}
}

const (
	errOpenHABBaseURL  = "openhab.base_url must be an http URL with a loopback host (127.0.0.0/8, ::1 or localhost) and no user, path, query or fragment"
	errOpenHABAPIToken = "openhab.api_token must not contain whitespace or control characters"
)

// validateOpenHAB validates the default configuration with its openhab block
// changed by set.
func validateOpenHAB(set func(*OpenHAB)) error {
	cfg := Default()
	set(&cfg.OpenHAB)
	return cfg.Validate()
}

// checkErrText fails t unless err is nil for an empty want, or has exactly
// the text want.
func checkErrText(t *testing.T, err error, want string) {
	t.Helper()
	if want == "" {
		if err != nil {
			t.Fatalf("error = %q, want nil", err)
		}
		return
	}
	if err == nil || err.Error() != want {
		t.Fatalf("error = %v, want %q", err, want)
	}
}

func TestValidate_OpenHAB_AC7(t *testing.T) {
	enabled := func(v string) func(*OpenHAB) { return func(o *OpenHAB) { o.Enabled = v } }
	baseURL := func(v string) func(*OpenHAB) { return func(o *OpenHAB) { o.BaseURL = v } }
	token := func(v string) func(*OpenHAB) { return func(o *OpenHAB) { o.APIToken = v } }
	interval := func(v int) func(*OpenHAB) { return func(o *OpenHAB) { o.PollIntervalSeconds = v } }
	tests := []struct {
		name string
		set  func(*OpenHAB)
		want string
	}{
		{"AC7 enabled auto", enabled("auto"), ""},
		{"AC7 enabled AUTO", enabled("AUTO"), ""},
		{"AC7 enabled True", enabled("True"), ""},
		{"AC7 enabled FALSE", enabled("FALSE"), ""},
		{"AC7 enabled yes", enabled("yes"), `openhab.enabled must be auto, true or false (got "yes")`},
		{"AC7 enabled empty", enabled(""), `openhab.enabled must be auto, true or false (got "")`},
		{"AC7 enabled with space", enabled(" auto"), `openhab.enabled must be auto, true or false (got " auto")`},

		{"AC7 base_url default", baseURL("http://127.0.0.1:8080"), ""},
		{"AC7 base_url trailing slash", baseURL("http://127.0.0.1:8080/"), ""},
		{"AC7 base_url upper-case scheme", baseURL("HTTP://127.0.0.1:8080"), ""},
		{"AC7 base_url no port", baseURL("http://127.0.0.1"), ""},
		{"AC7 base_url port 1", baseURL("http://127.0.0.1:1"), ""},
		{"AC7 base_url port 65535", baseURL("http://127.0.0.1:65535"), ""},
		{"AC7 base_url other 127/8 address", baseURL("http://127.1.2.3:8080"), ""},
		{"AC7 base_url localhost", baseURL("http://localhost:8080"), ""},
		{"AC7 base_url LocalHost", baseURL("http://LocalHost:8080"), ""},
		{"AC7 base_url ::1", baseURL("http://[::1]:8080"), ""},
		{"AC7 base_url mapped IPv4 loopback", baseURL("http://[::ffff:127.0.0.1]:8080"), ""},
		{"AC7 base_url empty", baseURL(""), errOpenHABBaseURL},
		{"AC7 base_url https", baseURL("https://127.0.0.1:8443"), errOpenHABBaseURL},
		{"AC7 base_url ftp", baseURL("ftp://127.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url no scheme", baseURL("127.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url scheme-relative", baseURL("//127.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url opaque", baseURL("http:127.0.0.1"), errOpenHABBaseURL},
		{"AC7 base_url user and password", baseURL("http://u:p@127.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url userinfo hides host", baseURL("http://127.0.0.1:80@evil"), errOpenHABBaseURL},
		{"AC7 base_url path", baseURL("http://127.0.0.1:8080/rest"), errOpenHABBaseURL},
		{"AC7 base_url query", baseURL("http://127.0.0.1:8080?x=1"), errOpenHABBaseURL},
		{"AC7 base_url empty query", baseURL("http://127.0.0.1:8080?"), errOpenHABBaseURL},
		{"AC7 base_url fragment", baseURL("http://127.0.0.1:8080#f"), errOpenHABBaseURL},
		{"AC7 base_url port 0", baseURL("http://127.0.0.1:0"), errOpenHABBaseURL},
		{"AC7 base_url port 65536", baseURL("http://127.0.0.1:65536"), errOpenHABBaseURL},
		{"AC7 base_url port not numeric", baseURL("http://127.0.0.1:http"), errOpenHABBaseURL},
		{"AC7 base_url localhost trailing dot", baseURL("http://localhost.:8080"), errOpenHABBaseURL},
		{"AC7 base_url short 127.1", baseURL("http://127.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url decimal address", baseURL("http://2130706433:8080"), errOpenHABBaseURL},
		{"AC7 base_url hex octet", baseURL("http://0x7f.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url leading zero octet", baseURL("http://0127.0.0.1:8080"), errOpenHABBaseURL},
		{"AC7 base_url loopback-looking name", baseURL("http://127.0.0.1.nip.io:8080"), errOpenHABBaseURL},
		{"AC7 base_url zone", baseURL("http://[::1%25lo]:8080"), errOpenHABBaseURL},
		{"AC7 base_url LAN address", baseURL("http://192.168.1.2:8080"), errOpenHABBaseURL},
		{"AC7 base_url remote name", baseURL("http://example.com:8080"), errOpenHABBaseURL},
		{"AC7 base_url empty host", baseURL("http://:8080"), errOpenHABBaseURL},
		{"AC7 base_url no host", baseURL("http://"), errOpenHABBaseURL},
		{"AC7 base_url space in authority", baseURL("http://127.0.0.1 :8080"), errOpenHABBaseURL},
		{"AC7 base_url backslash in authority", baseURL(`http://127.0.0.1\:8080`), errOpenHABBaseURL},

		{"AC7 poll_interval_seconds 59", interval(59), "openhab.poll_interval_seconds must be between 60 and 86400 (got 59)"},
		{"AC7 poll_interval_seconds 60", interval(60), ""},
		{"AC7 poll_interval_seconds 86400", interval(86400), ""},
		{"AC7 poll_interval_seconds 86401", interval(86401), "openhab.poll_interval_seconds must be between 60 and 86400 (got 86401)"},
		{"AC7 poll_interval_seconds 0", interval(0), "openhab.poll_interval_seconds must be between 60 and 86400 (got 0)"},

		{"AC7 api_token plain", token("oh.pimonitor.AbC123-_xyz"), ""},
		{"AC7 api_token space", token("oh.pimonitor abc"), errOpenHABAPIToken},
		{"AC7 api_token tab", token("abc\tdef"), errOpenHABAPIToken},
		{"AC7 api_token trailing newline", token("abcdef\n"), errOpenHABAPIToken},
		{"AC7 api_token carriage return", token("abc\rdef"), errOpenHABAPIToken},
		{"AC7 api_token NUL", token("abc\x00def"), errOpenHABAPIToken},
		{"AC7 api_token DEL", token("abc\x7fdef"), errOpenHABAPIToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateOpenHAB(tt.set)

			checkErrText(t, err, tt.want)
		})
	}
}

func TestLoad_OpenHABEnabled_AC7(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		wantMode string
		wantErr  string
	}{
		{"AC7 auto", "openhab:\n  enabled: auto\n", "auto", ""},
		{"AC7 AUTO", "openhab:\n  enabled: AUTO\n", "auto", ""},
		{"AC7 True unquoted", "openhab:\n  enabled: True\n", "true", ""},
		{"AC7 true unquoted", "openhab:\n  enabled: true\n", "true", ""},
		{"AC7 false quoted", "openhab:\n  enabled: \"false\"\n", "false", ""},
		{"AC7 FALSE unquoted", "openhab:\n  enabled: FALSE\n", "false", ""},
		{"AC7 null keeps auto", "openhab:\n  enabled:\n", "auto", ""},
		{"AC7 tilde keeps auto", "openhab:\n  enabled: ~\n", "auto", ""},
		{"AC7 yes", "openhab:\n  enabled: yes\n", "", `openhab.enabled must be auto, true or false (got "yes")`},
		{"AC7 no", "openhab:\n  enabled: no\n", "", `openhab.enabled must be auto, true or false (got "no")`},
		{"AC7 on", "openhab:\n  enabled: on\n", "", `openhab.enabled must be auto, true or false (got "on")`},
		{"AC7 off", "openhab:\n  enabled: off\n", "", `openhab.enabled must be auto, true or false (got "off")`},
		{"AC7 1", "openhab:\n  enabled: 1\n", "", `openhab.enabled must be auto, true or false (got "1")`},
		{"AC7 0", "openhab:\n  enabled: 0\n", "", `openhab.enabled must be auto, true or false (got "0")`},
		{"AC7 quoted empty", "openhab:\n  enabled: \"\"\n", "", `openhab.enabled must be auto, true or false (got "")`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := loadYAMLText(t, tt.yaml)

			checkErrText(t, err, tt.wantErr)
			if got := result.Config.OpenHAB.Mode(); got != tt.wantMode {
				t.Fatalf("Mode() = %q, want %q", got, tt.wantMode)
			}
		})
	}
}

func TestLoad_OpenHABPollIntervalFraction_AC7(t *testing.T) {
	tests := []struct {
		name         string
		yaml         string
		wantInterval time.Duration
		wantErr      string
	}{
		{"AC7 fraction", "openhab:\n  poll_interval_seconds: 60.5\n", 0,
			"openhab.poll_interval_seconds must be a whole number (got 60.5)"},
		{"AC7 fraction through alias", "poll_interval_seconds: &n 90.5\nopenhab: {poll_interval_seconds: *n}\n", 0,
			"openhab.poll_interval_seconds must be a whole number (got 90.5)"},
		{"AC7 not finite", "openhab:\n  poll_interval_seconds: .nan\n", 0,
			"openhab.poll_interval_seconds must be a finite number"},
		{"AC7 whole number written as decimal", "openhab:\n  poll_interval_seconds: 120.0\n", 120 * time.Second, ""},
		{"AC7 whole number", "openhab:\n  poll_interval_seconds: 86400\n", 86400 * time.Second, ""},
		{"AC7 below range from file", "openhab:\n  poll_interval_seconds: 59\n", 0,
			"openhab.poll_interval_seconds must be between 60 and 86400 (got 59)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := loadYAMLText(t, tt.yaml)

			checkErrText(t, err, tt.wantErr)
			if got := result.Config.OpenHAB.PollInterval(); got != tt.wantInterval {
				t.Fatalf("PollInterval() = %v, want %v", got, tt.wantInterval)
			}
		})
	}
}

// loadWithOpenHABEnv loads a config file holding fileYAML with env as the
// environment and returns the result, the names unset and the error.
func loadWithOpenHABEnv(t *testing.T, fileYAML string, env map[string]string) (Result, []string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	writeFile(t, path, fileYAML)
	var unset []string
	result, err := load([]string{"-config", path}, envFunc(env), func(key string) { unset = append(unset, key) })
	return result, unset, err
}

func TestLoad_OpenHABTokenEnv_AC8(t *testing.T) {
	const fileToken = "openhab:\n  api_token: file-token\n"
	tests := []struct {
		name      string
		yaml      string
		env       map[string]string
		wantToken string
		wantUnset []string
		wantErr   string
	}{
		{"AC8 env overrides file", fileToken,
			map[string]string{OpenHABTokenEnvVar: "env-token"}, "env-token", []string{OpenHABTokenEnvVar}, ""},
		{"AC8 env without file token", "",
			map[string]string{OpenHABTokenEnvVar: "env-token"}, "env-token", []string{OpenHABTokenEnvVar}, ""},
		{"AC8 empty env keeps file token and is unset", fileToken,
			map[string]string{OpenHABTokenEnvVar: ""}, "file-token", []string{OpenHABTokenEnvVar}, ""},
		{"AC8 absent env keeps file token, nothing unset", fileToken,
			map[string]string{}, "file-token", nil, ""},
		{"AC8 both env vars unset", fileToken,
			map[string]string{APIKeyEnvVar: "key", OpenHABTokenEnvVar: "env-token"}, "env-token",
			[]string{APIKeyEnvVar, OpenHABTokenEnvVar}, ""},
		{"AC8 env token is validated", fileToken,
			map[string]string{OpenHABTokenEnvVar: "env token"}, "", []string{OpenHABTokenEnvVar}, errOpenHABAPIToken},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, unset, err := loadWithOpenHABEnv(t, tt.yaml, tt.env)

			checkErrText(t, err, tt.wantErr)
			if result.Config.OpenHAB.APIToken != tt.wantToken {
				t.Fatalf("OpenHAB.APIToken = %q, want %q", result.Config.OpenHAB.APIToken, tt.wantToken)
			}
			slices.Sort(unset)
			if !slices.Equal(unset, tt.wantUnset) {
				t.Fatalf("unsetEnv calls = %v, want %v", unset, tt.wantUnset)
			}
		})
	}
}

// TestLoad_OpenHABTokenEnvRealEnvironment_AC8 covers the wiring of the
// exported Load to the real environment; like
// TestLoad_UnsetsAPIKeyEnvVarInRealEnvironment it must not run in parallel.
func TestLoad_OpenHABTokenEnvRealEnvironment_AC8(t *testing.T) {
	t.Setenv(OpenHABTokenEnvVar, "env-token")

	result, err := Load(nil)

	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if result.Config.OpenHAB.APIToken != "env-token" {
		t.Fatalf("OpenHAB.APIToken = %q, want env-token", result.Config.OpenHAB.APIToken)
	}
	if result.Config.APIKey != "" {
		t.Fatalf("APIKey = %q, want empty (the openHAB token must not set the API key)", result.Config.APIKey)
	}
	if v, ok := os.LookupEnv(OpenHABTokenEnvVar); ok {
		t.Fatalf("%s still set to %q after Load, want unset", OpenHABTokenEnvVar, v)
	}
}

// openHABAlertErrorCases are the E13 texts of the five openHAB alert options,
// each with the YAML document that triggers it.
var openHABAlertErrorCases = []struct {
	name string
	yaml string
	want string
}{
	{"rss warn NaN", "thresholds:\n  openhab_rss_warn_percent: .nan\n",
		"thresholds.openhab_rss_warn_percent must be a finite number"},
	{"rss crit +Inf", "thresholds:\n  openhab_rss_crit_percent: .inf\n",
		"thresholds.openhab_rss_crit_percent must be a finite number"},
	{"rss warn -Inf", "thresholds:\n  openhab_rss_warn_percent: -.inf\n",
		"thresholds.openhab_rss_warn_percent must be a finite number"},
	{"for_seconds NaN", "alerts:\n  openhab_for_seconds: .nan\n",
		"alerts.openhab_for_seconds must be a finite number"},
	{"for_seconds +Inf", "alerts:\n  openhab_for_seconds: .inf\n",
		"alerts.openhab_for_seconds must be a finite number"},
	{"rss warn negative", "thresholds:\n  openhab_rss_warn_percent: -1\n",
		"thresholds.openhab_rss_warn_percent must be >= 0 (got -1)"},
	{"rss crit negative", "thresholds:\n  openhab_rss_crit_percent: -2\n",
		"thresholds.openhab_rss_crit_percent must be >= 0 (got -2)"},
	{"things warn negative", "thresholds:\n  openhab_things_offline_warn: -1\n",
		"thresholds.openhab_things_offline_warn must be >= 0 (got -1)"},
	{"things crit negative", "thresholds:\n  openhab_things_offline_crit: -3\n",
		"thresholds.openhab_things_offline_crit must be >= 0 (got -3)"},
	{"rss warn above crit", "thresholds:\n  openhab_rss_warn_percent: 80\n  openhab_rss_crit_percent: 70\n",
		"thresholds.openhab_rss_warn_percent (80) must be <= thresholds.openhab_rss_crit_percent (70)"},
	{"things warn above crit", "thresholds:\n  openhab_things_offline_warn: 5\n  openhab_things_offline_crit: 3\n",
		"thresholds.openhab_things_offline_warn (5) must be <= thresholds.openhab_things_offline_crit (3)"},
	{"things warn fractional", "thresholds:\n  openhab_things_offline_warn: 1.5\n",
		"thresholds.openhab_things_offline_warn must be a whole number (got 1.5)"},
	{"things crit fractional", "thresholds:\n  openhab_things_offline_crit: 2.5\n",
		"thresholds.openhab_things_offline_crit must be a whole number (got 2.5)"},
	{"for_seconds negative", "alerts:\n  openhab_for_seconds: -1\n",
		"alerts.openhab_for_seconds must be >= 0 (got -1)"},
}

func TestLoad_OpenHABAlertOptions_AC7(t *testing.T) {
	t.Run("AC7 defaults", checkOpenHABAlertDefaultsAC7)
	t.Run("AC7 file without the keys keeps the defaults", checkOpenHABAlertKeysAbsentAC7)
	t.Run("AC7 explicit values are read", checkOpenHABAlertExplicitAC7)
	t.Run("AC7 zero is accepted for every option", checkOpenHABAlertZeroAC7)
	t.Run("AC7 warn 0 with crit set is accepted", checkOpenHABAlertWarnZeroAC7)
	for _, enabled := range []string{"true", "false"} {
		for _, tc := range openHABAlertErrorCases {
			t.Run(fmt.Sprintf("AC7 E13 alerts.enabled %s %s", enabled, tc.name), func(t *testing.T) {
				checkAlertsEnabledErrorAC7(t, withAlertsEnabled(tc.yaml, enabled), tc.want)
			})
		}
	}
}

func checkOpenHABAlertDefaultsAC7(t *testing.T) {
	cfg := Default()

	if cfg.Thresholds.OpenHABRSSWarnPercent != 50 || cfg.Thresholds.OpenHABRSSCritPercent != 75 {
		t.Errorf("rss thresholds = %v/%v, want 50/75", cfg.Thresholds.OpenHABRSSWarnPercent, cfg.Thresholds.OpenHABRSSCritPercent)
	}
	if cfg.Thresholds.OpenHABThingsOfflineWarn != 1 || cfg.Thresholds.OpenHABThingsOfflineCrit != 0 {
		t.Errorf("things thresholds = %d/%d, want 1/0", cfg.Thresholds.OpenHABThingsOfflineWarn, cfg.Thresholds.OpenHABThingsOfflineCrit)
	}
	if cfg.Alerts.OpenHABForSeconds != 300 {
		t.Errorf("Alerts.OpenHABForSeconds = %v, want 300", cfg.Alerts.OpenHABForSeconds)
	}
	if got := cfg.OpenHABAlertFor(); got != 5*time.Minute {
		t.Errorf("OpenHABAlertFor() = %v, want 5m0s", got)
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("Default().Validate() = %v, want nil", err)
	}
}

func checkOpenHABAlertKeysAbsentAC7(t *testing.T) {
	res, err := loadYAMLText(t, "poll_interval_seconds: 7\n")
	if err != nil {
		t.Fatalf("Load error = %v, want nil", err)
	}

	want := Default()
	if res.Config.Thresholds != want.Thresholds {
		t.Errorf("Thresholds = %+v, want %+v", res.Config.Thresholds, want.Thresholds)
	}
	if res.Config.Alerts.OpenHABForSeconds != 300 {
		t.Errorf("Alerts.OpenHABForSeconds = %v, want 300", res.Config.Alerts.OpenHABForSeconds)
	}
}

func checkOpenHABAlertExplicitAC7(t *testing.T) {
	doc := "thresholds:\n  openhab_rss_warn_percent: 40.5\n  openhab_rss_crit_percent: 60\n" +
		"  openhab_things_offline_warn: 2\n  openhab_things_offline_crit: 4\nalerts:\n  openhab_for_seconds: 90\n"

	res, err := loadYAMLText(t, doc)

	if err != nil {
		t.Fatalf("Load error = %v, want nil", err)
	}
	th := res.Config.Thresholds
	if th.OpenHABRSSWarnPercent != 40.5 || th.OpenHABRSSCritPercent != 60 || th.OpenHABThingsOfflineWarn != 2 || th.OpenHABThingsOfflineCrit != 4 {
		t.Errorf("thresholds = %+v, want 40.5/60/2/4", th)
	}
	if got := res.Config.OpenHABAlertFor(); got != 90*time.Second {
		t.Errorf("OpenHABAlertFor() = %v, want 1m30s", got)
	}
}

func checkOpenHABAlertZeroAC7(t *testing.T) {
	doc := "thresholds:\n  openhab_rss_warn_percent: 0\n  openhab_rss_crit_percent: 0\n" +
		"  openhab_things_offline_warn: 0\n  openhab_things_offline_crit: 0\nalerts:\n  openhab_for_seconds: 0\n"

	if _, err := loadYAMLText(t, doc); err != nil {
		t.Fatalf("Load error = %v, want nil", err)
	}
}

func checkOpenHABAlertWarnZeroAC7(t *testing.T) {
	doc := "thresholds:\n  openhab_rss_warn_percent: 0\n  openhab_rss_crit_percent: 70\n" +
		"  openhab_things_offline_warn: 0\n  openhab_things_offline_crit: 3\n"

	if _, err := loadYAMLText(t, doc); err != nil {
		t.Fatalf("Load error = %v, want nil", err)
	}
}

// checkAlertsEnabledErrorAC7 asserts that loading doc fails with exactly want.
func checkAlertsEnabledErrorAC7(t *testing.T, doc, want string) {
	t.Helper()
	_, err := loadYAMLText(t, doc)

	if err == nil || err.Error() != want {
		t.Fatalf("Load error = %v, want %q", err, want)
	}
}

// withAlertsEnabled returns doc with alerts.enabled set to enabled, merging
// into an existing alerts block of doc.
func withAlertsEnabled(doc, enabled string) string {
	if strings.Contains(doc, "alerts:\n") {
		return strings.Replace(doc, "alerts:\n", "alerts:\n  enabled: "+enabled+"\n", 1)
	}
	return doc + "alerts:\n  enabled: " + enabled + "\n"
}

func TestValidate_OpenHABAlertOptions_AC7(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"AC7 rss warn NaN", func(c *Config) { c.Thresholds.OpenHABRSSWarnPercent = math.NaN() },
			"thresholds.openhab_rss_warn_percent must be a finite number"},
		{"AC7 rss crit Inf", func(c *Config) { c.Thresholds.OpenHABRSSCritPercent = math.Inf(1) },
			"thresholds.openhab_rss_crit_percent must be a finite number"},
		{"AC7 for_seconds -Inf", func(c *Config) { c.Alerts.OpenHABForSeconds = math.Inf(-1) },
			"alerts.openhab_for_seconds must be a finite number"},
		{"AC7 for_seconds negative", func(c *Config) { c.Alerts.OpenHABForSeconds = -5 },
			"alerts.openhab_for_seconds must be >= 0 (got -5)"},
		{"AC7 things warn negative", func(c *Config) { c.Thresholds.OpenHABThingsOfflineWarn = -1 },
			"thresholds.openhab_things_offline_warn must be >= 0 (got -1)"},
		{"AC7 things crit negative", func(c *Config) { c.Thresholds.OpenHABThingsOfflineCrit = -1 },
			"thresholds.openhab_things_offline_crit must be >= 0 (got -1)"},
		{"AC7 rss warn above crit", func(c *Config) { c.Thresholds.OpenHABRSSWarnPercent = 90 },
			"thresholds.openhab_rss_warn_percent (90) must be <= thresholds.openhab_rss_crit_percent (75)"},
		{"AC7 things warn above crit", func(c *Config) {
			c.Thresholds.OpenHABThingsOfflineWarn = 4
			c.Thresholds.OpenHABThingsOfflineCrit = 2
		}, "thresholds.openhab_things_offline_warn (4) must be <= thresholds.openhab_things_offline_crit (2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			cfg.Alerts.Enabled = false
			tt.mutate(&cfg)

			err := cfg.Validate()

			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidate_AlertWindowUpperBound(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string // exact error text; empty means Validate must pass
	}{
		{"AC1 for_seconds 604800.5", func(c *Config) { c.Alerts.ForSeconds = 604800.5 },
			"alerts.for_seconds must be <= 604800 (got 604800.5)"},
		{"AC1 for_seconds 1e12", func(c *Config) { c.Alerts.ForSeconds = 1e12 },
			"alerts.for_seconds must be <= 604800 (got 1e+12)"},
		{"AC1 for_seconds -1 keeps its text", func(c *Config) { c.Alerts.ForSeconds = -1 },
			"alerts.for_seconds must be >= 0 (got -1)"},
		{"AC1 for_seconds NaN keeps its text", func(c *Config) { c.Alerts.ForSeconds = math.NaN() },
			"alerts.for_seconds must be a finite number"},
		{"AC1 for_seconds 604800 passes", func(c *Config) { c.Alerts.ForSeconds = 604800 }, ""},
		{"AC2 openhab_for_seconds 604800.5", func(c *Config) { c.Alerts.OpenHABForSeconds = 604800.5 },
			"alerts.openhab_for_seconds must be <= 604800 (got 604800.5)"},
		{"AC2 openhab_for_seconds 604800.5 with alerts and openhab disabled", func(c *Config) {
			c.Alerts.Enabled = false
			c.OpenHAB.Enabled = "false"
			c.Alerts.OpenHABForSeconds = 604800.5
		}, "alerts.openhab_for_seconds must be <= 604800 (got 604800.5)"},
		{"AC2 openhab_for_seconds -5 keeps its text", func(c *Config) { c.Alerts.OpenHABForSeconds = -5 },
			"alerts.openhab_for_seconds must be >= 0 (got -5)"},
		{"AC2 openhab_for_seconds -Inf keeps its text", func(c *Config) { c.Alerts.OpenHABForSeconds = math.Inf(-1) },
			"alerts.openhab_for_seconds must be a finite number"},
		{"AC2 openhab_for_seconds 604800 passes", func(c *Config) { c.Alerts.OpenHABForSeconds = 604800 }, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAlertFor_AtUpperBound_AC1(t *testing.T) {
	c := Default()
	c.Alerts.ForSeconds = 604800
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() with for_seconds 604800 = %v, want nil", err)
	}
	if got, want := c.AlertFor(), 168*time.Hour; got != want {
		t.Fatalf("AlertFor() = %v, want %v", got, want)
	}
}

func TestOpenHABAlertFor_AtUpperBound_AC2(t *testing.T) {
	c := Default()
	c.Alerts.OpenHABForSeconds = 604800
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() with openhab_for_seconds 604800 = %v, want nil", err)
	}
	if got, want := c.OpenHABAlertFor(), 168*time.Hour; got != want {
		t.Fatalf("OpenHABAlertFor() = %v, want %v", got, want)
	}
}

func TestValidate_DurationUpperBounds(t *testing.T) {
	over := func(key string, v float64, limit int) string {
		return fmt.Sprintf("%s must be <= %d (got %v)", key, limit, v)
	}
	webhook := func(timeouts ...float64) []Webhook {
		var ws []Webhook
		for _, to := range timeouts {
			ws = append(ws, Webhook{URL: "http://127.0.0.1:9/hook", MinLevel: "warn", TimeoutSeconds: to})
		}
		return ws
	}
	alertsOff := func(c *Config) { c.Alerts.Enabled = false }
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string // exact error text; empty means Validate must pass
	}{
		{"AC1 poll_interval_seconds 604800.5", func(c *Config) { c.PollIntervalSeconds = 604800.5 },
			over("poll_interval_seconds", 604800.5, 604800)},
		{"AC1 poll_interval_seconds 1e12", func(c *Config) { c.PollIntervalSeconds = 1e12 },
			over("poll_interval_seconds", 1e12, 604800)},
		{"AC1 poll_interval_seconds 604800 passes", func(c *Config) { c.PollIntervalSeconds = 604800 }, ""},
		{"AC1 poll_interval_seconds 0 is below the minimum", func(c *Config) { c.PollIntervalSeconds = 0 },
			"poll_interval_seconds must be >= 1 (got 0)"},
		{"AC2 updates_check_minutes 10080.5", func(c *Config) { c.UpdatesCheckMinutes = 10080.5 },
			over("updates_check_minutes", 10080.5, 10080)},
		{"AC2 updates_check_minutes 10080 passes", func(c *Config) { c.UpdatesCheckMinutes = 10080 }, ""},
		{"AC2 updates_check_minutes 0 is below the minimum", func(c *Config) { c.UpdatesCheckMinutes = 0 },
			"updates_check_minutes must be >= 1 (got 0)"},
		{"AC3 updates_stale_threshold_minutes 525600.5", func(c *Config) { c.UpdatesStaleThresholdMinutes = 525600.5 },
			over("updates_stale_threshold_minutes", 525600.5, 525600)},
		{"AC3 updates_stale_threshold_minutes 525600 passes", func(c *Config) { c.UpdatesStaleThresholdMinutes = 525600 }, ""},
		{"AC3 updates_stale_threshold_minutes -1 keeps its text", func(c *Config) { c.UpdatesStaleThresholdMinutes = -1 },
			"updates_stale_threshold_minutes must be >= 0 (got -1)"},
		{"AC4 healthz_max_staleness_seconds 31536000.5", func(c *Config) { c.HealthzMaxStalenessSeconds = 31536000.5 },
			over("healthz_max_staleness_seconds", 31536000.5, 31536000)},
		{"AC4 healthz_max_staleness_seconds 31536000 passes", func(c *Config) { c.HealthzMaxStalenessSeconds = 31536000 }, ""},
		{"AC4 healthz_max_staleness_seconds 2 below poll interval keeps its text", func(c *Config) {
			c.PollIntervalSeconds = 10
			c.HealthzMaxStalenessSeconds = 2
		}, "healthz_max_staleness_seconds (2) must be >= poll_interval_seconds (10), or /healthz reports unhealthy permanently"},
		{"AC5 history_window_minutes 525600.5 with poll 60", func(c *Config) {
			c.PollIntervalSeconds = 60
			c.HistoryWindowMinutes = 525600.5
		}, over("history_window_minutes", 525600.5, 525600)},
		{"AC5 history_window_minutes 1e12 at poll 5 names the key", func(c *Config) {
			c.PollIntervalSeconds = 5
			c.HistoryWindowMinutes = 1e12
		}, over("history_window_minutes", 1e12, 525600)},
		{"AC5 history_window_minutes 525600 with poll 60 passes", func(c *Config) {
			c.PollIntervalSeconds = 60
			c.HistoryWindowMinutes = 525600
		}, ""},
		{"AC5 history_window_minutes NaN keeps its text", func(c *Config) { c.HistoryWindowMinutes = math.NaN() },
			"history_window_minutes must be a finite number"},
		{"AC6 notify_retry_backoff_seconds 604800.5", func(c *Config) { c.Alerts.NotifyRetryBackoffSeconds = 604800.5 },
			over("alerts.notify_retry_backoff_seconds", 604800.5, 604800)},
		{"AC6 notify_retry_backoff_seconds 604800.5 with alerts disabled", func(c *Config) {
			alertsOff(c)
			c.Alerts.NotifyRetryBackoffSeconds = 604800.5
		}, over("alerts.notify_retry_backoff_seconds", 604800.5, 604800)},
		{"AC6 notify_retry_backoff_seconds 604800 passes", func(c *Config) { c.Alerts.NotifyRetryBackoffSeconds = 604800 }, ""},
		{"AC6 notify_retry_backoff_seconds -1 keeps its text", func(c *Config) { c.Alerts.NotifyRetryBackoffSeconds = -1 },
			"alerts.notify_retry_backoff_seconds must be >= 0 (got -1)"},
		{"AC7 notify_min_interval_seconds 604800.5", func(c *Config) { c.Alerts.NotifyMinIntervalSeconds = 604800.5 },
			over("alerts.notify_min_interval_seconds", 604800.5, 604800)},
		{"AC7 notify_min_interval_seconds 604800.5 with alerts disabled", func(c *Config) {
			alertsOff(c)
			c.Alerts.NotifyMinIntervalSeconds = 604800.5
		}, over("alerts.notify_min_interval_seconds", 604800.5, 604800)},
		{"AC7 notify_min_interval_seconds 604800 passes", func(c *Config) { c.Alerts.NotifyMinIntervalSeconds = 604800 }, ""},
		{"AC8 second webhook timeout 604800.5 names index 1", func(c *Config) {
			c.Alerts.Webhooks = webhook(10, 604800.5)
		}, over("alerts.webhooks[1].timeout_seconds", 604800.5, 604800)},
		{"AC8 second webhook timeout 604800.5 with alerts disabled", func(c *Config) {
			alertsOff(c)
			c.Alerts.Webhooks = webhook(10, 604800.5)
		}, over("alerts.webhooks[1].timeout_seconds", 604800.5, 604800)},
		{"AC8 webhook timeout 604800 passes", func(c *Config) { c.Alerts.Webhooks = webhook(604800) }, ""},
		{"AC8 webhook timeout -1 keeps its text", func(c *Config) { c.Alerts.Webhooks = webhook(-1) },
			"alerts.webhooks[0].timeout_seconds must be >= 0 (got -1)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestValidate_IntervalLowerBounds(t *testing.T) {
	below := func(key string, v float64) string {
		return fmt.Sprintf("%s must be >= 1 (got %v)", key, v)
	}
	tests := []struct {
		name   string
		mutate func(*Config)
		want   string // exact error text; empty means Validate must pass
	}{
		{"AC1 poll_interval_seconds 0.999", func(c *Config) { c.PollIntervalSeconds = 0.999 },
			below("poll_interval_seconds", 0.999)},
		{"AC1 poll_interval_seconds 0.001", func(c *Config) { c.PollIntervalSeconds = 0.001 },
			below("poll_interval_seconds", 0.001)},
		{"AC1 poll_interval_seconds 1e-10", func(c *Config) { c.PollIntervalSeconds = 1e-10 },
			below("poll_interval_seconds", 1e-10)},
		{"AC1 poll_interval_seconds 0", func(c *Config) { c.PollIntervalSeconds = 0 },
			below("poll_interval_seconds", 0)},
		{"AC1 poll_interval_seconds -1", func(c *Config) { c.PollIntervalSeconds = -1 },
			below("poll_interval_seconds", -1)},
		{"AC1 poll_interval_seconds 1 passes", func(c *Config) { c.PollIntervalSeconds = 1 }, ""},
		{"AC2 updates_check_minutes 0.999", func(c *Config) { c.UpdatesCheckMinutes = 0.999 },
			below("updates_check_minutes", 0.999)},
		{"AC2 updates_check_minutes 0.001", func(c *Config) { c.UpdatesCheckMinutes = 0.001 },
			below("updates_check_minutes", 0.001)},
		{"AC2 updates_check_minutes 1e-12", func(c *Config) { c.UpdatesCheckMinutes = 1e-12 },
			below("updates_check_minutes", 1e-12)},
		{"AC2 updates_check_minutes 0", func(c *Config) { c.UpdatesCheckMinutes = 0 },
			below("updates_check_minutes", 0)},
		{"AC2 updates_check_minutes -1", func(c *Config) { c.UpdatesCheckMinutes = -1 },
			below("updates_check_minutes", -1)},
		{"AC2 updates_check_minutes 1 passes", func(c *Config) { c.UpdatesCheckMinutes = 1 }, ""},
		// 525600 min * 60 s / 1 s = 3.1536e+07 samples (Go %v), above the ratio limit.
		{"AC4 ratio at poll 1", func(c *Config) {
			c.PollIntervalSeconds = 1
			c.HistoryWindowMinutes = 525600
		}, "history_window_minutes (525600) / poll_interval_seconds (1) implies 3.1536e+07 history points per series, exceeding the 1000000 limit; increase poll_interval_seconds or reduce history_window_minutes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := Default()
			tt.mutate(&c)
			err := c.Validate()
			if tt.want == "" {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tt.want {
				t.Fatalf("Validate() = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestLoad_RejectsIntervalBelowMinimum(t *testing.T) {
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{"AC3 poll_interval_seconds 0.001", "poll_interval_seconds: 0.001\n",
			"poll_interval_seconds must be >= 1 (got 0.001)"},
		{"AC3 updates_check_minutes 0.5", "updates_check_minutes: 0.5\n",
			"updates_check_minutes must be >= 1 (got 0.5)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := loadYAMLText(t, tt.yaml)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Load() error = %v, want it to contain %q", err, tt.want)
			}
		})
	}
}
