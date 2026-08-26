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
