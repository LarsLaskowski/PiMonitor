package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseUptime(t *testing.T) {
	got, err := parseUptime("12345.67 89012.34\n")
	if err != nil {
		t.Fatalf("parseUptime: %v", err)
	}
	if diffFloat(got, 12345.67) > 0.001 {
		t.Fatalf("parseUptime = %v, want 12345.67", got)
	}
}

func TestParseUptime_Malformed(t *testing.T) {
	if _, err := parseUptime("not-a-number rest"); err == nil {
		t.Fatal("expected error for non-numeric uptime")
	}
	if _, err := parseUptime(""); err == nil {
		t.Fatal("expected error for empty /proc/uptime content")
	}
}

func TestUptimeCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uptime")
	if err := os.WriteFile(path, []byte("100.5 200.0\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := &UptimeCollector{path: path}
	got, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if diffFloat(got, 100.5) > 0.001 {
		t.Fatalf("Collect = %v, want 100.5", got)
	}
}

func TestParseUptime_NonFinite_AC2(t *testing.T) {
	for _, form := range []string{"nan", "NaN", "inf", "+Inf", "-Infinity"} {
		t.Run("AC2 "+form, func(t *testing.T) {
			content := form + " 100.00\n"

			got, err := parseUptime(content)

			if got != 0 {
				t.Errorf("parseUptime(%q) = %v, want 0", content, got)
			}
			want := fmt.Sprintf("parse uptime: non-finite value %q", form)
			if err == nil || err.Error() != want {
				t.Errorf("parseUptime(%q) error = %v, want %q", content, err, want)
			}
		})
	}
}
