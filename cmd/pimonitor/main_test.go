package main

import (
	"bytes"
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
