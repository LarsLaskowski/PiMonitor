package collector

import (
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
