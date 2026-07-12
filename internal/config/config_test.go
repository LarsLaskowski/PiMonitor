package config

import (
	"os"
	"path/filepath"
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
	if result.Config != Default() {
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

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
