package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLoadAvg(t *testing.T) {
	got, err := parseLoadAvg("0.52 0.58 0.59 1/523 12345\n")
	if err != nil {
		t.Fatalf("parseLoadAvg: %v", err)
	}
	want := LoadAverage{Load1: 0.52, Load5: 0.58, Load15: 0.59}
	if got != want {
		t.Fatalf("parseLoadAvg = %+v, want %+v", got, want)
	}
}

func TestParseLoadAvg_Malformed(t *testing.T) {
	if _, err := parseLoadAvg("not enough fields"); err == nil {
		t.Fatal("expected error for malformed /proc/loadavg content")
	}
	if _, err := parseLoadAvg("abc 0.58 0.59 1/523 12345"); err == nil {
		t.Fatal("expected error for non-numeric load1")
	}
}

func TestLoadAvgCollector_Collect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "loadavg")
	if err := os.WriteFile(path, []byte("1.00 2.00 3.00 1/1 1\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	c := &LoadAvgCollector{path: path}
	got, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	want := LoadAverage{Load1: 1.00, Load5: 2.00, Load15: 3.00}
	if got != want {
		t.Fatalf("Collect = %+v, want %+v", got, want)
	}
}

func TestParseLoadAvg_NonFinite_AC1(t *testing.T) {
	forms := []string{"nan", "NaN", "inf", "+Inf", "-Infinity"}
	names := []string{"load1", "load5", "load15"}
	for pos, name := range names {
		for _, form := range forms {
			t.Run(fmt.Sprintf("AC1 %s %s", name, form), func(t *testing.T) {
				fields := []string{"0.50", "0.60", "0.70"}
				fields[pos] = form
				content := fields[0] + " " + fields[1] + " " + fields[2] + " 1/523 12345\n"

				got, err := parseLoadAvg(content)

				if got != (LoadAverage{}) {
					t.Errorf("parseLoadAvg(%q) = %+v, want the zero LoadAverage", content, got)
				}
				want := fmt.Sprintf("parse %s: non-finite value %q", name, form)
				if err == nil || err.Error() != want {
					t.Errorf("parseLoadAvg(%q) error = %v, want %q", content, err, want)
				}
			})
		}
	}
}
