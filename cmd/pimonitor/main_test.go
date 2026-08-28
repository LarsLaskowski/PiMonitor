package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"testing"

	"github.com/larslaskowski/pimonitor/internal/alert"
	"github.com/larslaskowski/pimonitor/internal/config"
)

func newTestNotifier(t *testing.T) *alert.Notifier {
	t.Helper()
	n, err := alert.NewNotifier(config.Alerts{
		Webhooks: []config.Webhook{{URL: "http://example.invalid/webhook"}},
	}, nil)
	if err != nil {
		t.Fatalf("NewNotifier: %v", err)
	}
	if n == nil {
		t.Fatal("expected a non-nil notifier for a configured webhook")
	}
	return n
}

// TestServerConfig_MapsFields guards the config.Config -> httpapi.Config
// mapping, in particular that TLSCertFile/TLSKeyFile (issue #41) are wired
// through to the HTTP layer rather than silently dropped.
func TestServerConfig_MapsFields(t *testing.T) {
	cfg := config.Config{
		ListenAddr:                 ":9443",
		APIKey:                     "secret",
		TLSCertFile:                "/etc/pimonitor/cert.pem",
		TLSKeyFile:                 "/etc/pimonitor/key.pem",
		PollIntervalSeconds:        5,
		HealthzMaxStalenessSeconds: 30,
	}

	got := serverConfig(cfg, "v1.2.3")

	if got.ListenAddr != ":9443" {
		t.Errorf("ListenAddr = %q, want :9443", got.ListenAddr)
	}
	if got.APIKey != "secret" {
		t.Errorf("APIKey = %q, want secret", got.APIKey)
	}
	if got.TLSCertFile != "/etc/pimonitor/cert.pem" {
		t.Errorf("TLSCertFile = %q, want /etc/pimonitor/cert.pem", got.TLSCertFile)
	}
	if got.TLSKeyFile != "/etc/pimonitor/key.pem" {
		t.Errorf("TLSKeyFile = %q, want /etc/pimonitor/key.pem", got.TLSKeyFile)
	}
	if got.Client.Version != "v1.2.3" {
		t.Errorf("Client.Version = %q, want v1.2.3", got.Client.Version)
	}
}

// TestClientConfig_ThresholdsRoundTrip guards against a field-by-field
// mapping mistake (e.g. a copy-paste swap like assigning SwapWarnPercent to
// DiskWarnPercent) going unnoticed between config.Thresholds and the JSON
// served by GET /api/v1/config. Every threshold is given a distinct,
// non-default value so a swapped pair fails the assertion for both
// affected keys rather than passing by coincidence (issue #113).
func TestClientConfig_ThresholdsRoundTrip(t *testing.T) {
	cfg := config.Config{
		PollIntervalSeconds: 5,
		NetworkEnabled:      true,
		Thresholds: config.Thresholds{
			TemperatureWarnC:  1,
			TemperatureCritC:  2,
			CPUWarnPercent:    3,
			CPUCritPercent:    4,
			DiskWarnPercent:   5,
			DiskCritPercent:   6,
			SwapWarnPercent:   7,
			SwapCritPercent:   8,
			MemoryWarnPercent: 9,
			MemoryCritPercent: 10,
		},
	}

	data, err := json.Marshal(clientConfig(cfg, "v9.9.9"))
	if err != nil {
		t.Fatalf("marshal ClientConfig: %v", err)
	}

	var decoded struct {
		Version    string             `json:"version"`
		Thresholds map[string]float64 `json:"thresholds"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	want := map[string]float64{
		"temperature_warn_c":  1,
		"temperature_crit_c":  2,
		"cpu_warn_percent":    3,
		"cpu_crit_percent":    4,
		"disk_warn_percent":   5,
		"disk_crit_percent":   6,
		"swap_warn_percent":   7,
		"swap_crit_percent":   8,
		"memory_warn_percent": 9,
		"memory_crit_percent": 10,
	}
	for key, wantVal := range want {
		if gotVal, ok := decoded.Thresholds[key]; !ok || gotVal != wantVal {
			t.Errorf("thresholds[%q] = %v (present=%v), want %v", key, gotVal, ok, wantVal)
		}
	}
	if len(decoded.Thresholds) != len(want) {
		t.Errorf("thresholds has %d keys, want %d: %v", len(decoded.Thresholds), len(want), decoded.Thresholds)
	}
	if decoded.Version != "v9.9.9" {
		t.Errorf("Version = %q, want %q", decoded.Version, "v9.9.9")
	}
}

// TestClientConfig_ExposesHistoryWindow covers the value the dashboard
// needs to bound the history window it accumulates locally from ?since=
// deltas (issue #112): served from the same configuration the collector
// retains history with, not hardcoded in the frontend.
func TestClientConfig_ExposesHistoryWindow(t *testing.T) {
	cfg := config.Config{
		PollIntervalSeconds:  5,
		HistoryWindowMinutes: 42,
	}

	data, err := json.Marshal(clientConfig(cfg, "v9.9.9"))
	if err != nil {
		t.Fatalf("marshal ClientConfig: %v", err)
	}

	var decoded struct {
		HistoryWindowMinutes float64 `json:"history_window_minutes"`
		PollIntervalSeconds  float64 `json:"poll_interval_seconds"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.HistoryWindowMinutes != 42 {
		t.Errorf("history_window_minutes = %v, want 42", decoded.HistoryWindowMinutes)
	}
	if decoded.PollIntervalSeconds != 5 {
		t.Errorf("poll_interval_seconds = %v, want 5", decoded.PollIntervalSeconds)
	}
}

func TestWarnIfNotifierInert(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		withNotif   bool
		wantWarning bool
	}{
		{"no webhooks configured, alerts disabled", false, false, false},
		{"webhooks configured, alerts enabled", true, true, false},
		{"webhooks configured, alerts disabled", false, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var notifier *alert.Notifier
			if tt.withNotif {
				notifier = newTestNotifier(t)
			}

			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))

			warnIfNotifierInert(log, config.Alerts{Enabled: tt.enabled}, notifier)

			gotWarning := bytes.Contains(buf.Bytes(), []byte("no notifications will be sent"))
			if gotWarning != tt.wantWarning {
				t.Fatalf("warnIfNotifierInert: got warning=%v, want %v (log: %q)", gotWarning, tt.wantWarning, buf.String())
			}
		})
	}
}
