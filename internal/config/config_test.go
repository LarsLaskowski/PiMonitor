package config

import (
	"os"
	"path/filepath"
	"reflect"
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
	if cfg.APIKey != "" {
		t.Fatal("expected APIKey to default to empty (no auth)")
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
		{"negative temperature warn", func(c *Config) { c.Thresholds.TemperatureWarnC = -1 }},
		{"temperature warn above crit", func(c *Config) { c.Thresholds.TemperatureWarnC = 90 }},
		{"cpu warn above crit", func(c *Config) { c.Thresholds.CPUWarnPercent = 99 }},
		{"disk warn above crit", func(c *Config) { c.Thresholds.DiskWarnPercent = 99 }},
		{"swap warn above crit", func(c *Config) { c.Thresholds.SwapWarnPercent = 99 }},
		{"memory warn above crit", func(c *Config) { c.Thresholds.MemoryWarnPercent = 99 }},
		{"history capacity exceeds sanity cap", func(c *Config) {
			c.HistoryWindowMinutes = 525600
			c.PollIntervalSeconds = 0.05
		}},
		{"negative alerts for_seconds", func(c *Config) { c.Alerts.ForSeconds = -1 }},
		{"negative notify max retries", func(c *Config) { c.Alerts.NotifyMaxRetries = -1 }},
		{"negative notify backoff", func(c *Config) { c.Alerts.NotifyRetryBackoffSeconds = -1 }},
		{"negative notify min interval", func(c *Config) { c.Alerts.NotifyMinIntervalSeconds = -1 }},
		{"webhook with empty url", func(c *Config) { c.Alerts.Webhooks = []Webhook{{URL: ""}} }},
		{"webhook with bad min_level", func(c *Config) {
			c.Alerts.Webhooks = []Webhook{{URL: "http://x", MinLevel: "info"}}
		}},
		{"webhook with negative timeout", func(c *Config) {
			c.Alerts.Webhooks = []Webhook{{URL: "http://x", TimeoutSeconds: -1}}
		}},
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

func TestValidate_HistoryCapacityBoundary(t *testing.T) {
	cfg := Default()
	// With poll_interval_seconds: 60, HistoryCapacity() equals
	// history_window_minutes exactly, giving clean integer boundaries.
	cfg.PollIntervalSeconds = 60

	cfg.HistoryWindowMinutes = maxHistoryCapacity // exactly at the cap
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() rejected history capacity exactly at the cap: %v", err)
	}

	cfg.HistoryWindowMinutes = maxHistoryCapacity + 1 // one point over
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
