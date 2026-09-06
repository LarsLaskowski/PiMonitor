package collector

import (
	"os"
	"path/filepath"
	"testing"
)

const wirelessFixture = `Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE
 face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22
 wlan0: 0000   70.  -40.  -256        0      0      0      0      0        0
`

const wirelessMultiFixture = `Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE
 face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22
 wlan1: 0000   45.  -70.  -256        0      0      0      0      0        0
 wlan0: 0000   70.  -40.  -256        0      0      0      0      0        0
`

const wirelessEmptyFixture = `Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE
 face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22
`

func writeWirelessFixture(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "wireless")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func TestParseWireless(t *testing.T) {
	got, err := parseWireless(wirelessFixture)
	if err != nil {
		t.Fatalf("parseWireless: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 interface, got %d: %+v", len(got), got)
	}
	want := Wireless{Interface: "wlan0", LinkQuality: 70, SignalDBm: -40}
	if got[0] != want {
		t.Fatalf("got %+v, want %+v", got[0], want)
	}
}

func TestParseWireless_SortedByName(t *testing.T) {
	got, err := parseWireless(wirelessMultiFixture)
	if err != nil {
		t.Fatalf("parseWireless: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 interfaces, got %d: %+v", len(got), got)
	}
	if got[0].Interface != "wlan0" || got[1].Interface != "wlan1" {
		t.Fatalf("interfaces not sorted by name: %+v", got)
	}
}

func TestParseWireless_NoInterfaces(t *testing.T) {
	got, err := parseWireless(wirelessEmptyFixture)
	if err != nil {
		t.Fatalf("parseWireless: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no interfaces, got %d: %+v", len(got), got)
	}
}

func TestParseWireless_MalformedLine(t *testing.T) {
	tests := []struct {
		name string
		line string
	}{
		{"missing level field", " wlan0: 0000   70.\n"},
		{"non-numeric link", " wlan0: 0000   abc.  -40.  -256        0      0      0      0      0        0\n"},
		{"non-numeric level", " wlan0: 0000   70.  abc.  -256        0      0      0      0      0        0\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := "Inter-| sta-|   Quality        |   Discarded packets               | Missed | WE\n" +
				" face | tus | link level noise |  nwid  crypt   frag  retry   misc | beacon | 22\n" + tt.line
			if tt.name == "missing level field" {
				if _, err := parseWireless(data); err != nil {
					t.Fatalf("parseWireless should skip a too-short line rather than error, got %v", err)
				}
				return
			}
			if _, err := parseWireless(data); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			}
		})
	}
}

func TestWirelessCollector_Collect(t *testing.T) {
	path := writeWirelessFixture(t, wirelessMultiFixture)
	c := &WirelessCollector{path: path}

	got, err := c.Collect()

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 interfaces, got %d: %+v", len(got), got)
	}
	if got[0].Interface != "wlan0" || got[1].Interface != "wlan1" {
		t.Fatalf("interfaces not sorted by name: %+v", got)
	}
}

func TestWirelessCollector_Collect_NoWirelessFile(t *testing.T) {
	c := &WirelessCollector{path: filepath.Join(t.TempDir(), "does-not-exist")}

	got, err := c.Collect()

	if err != nil {
		t.Fatalf("Collect should not error when /proc/net/wireless is absent (no wireless hardware), got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no interfaces, got %d: %+v", len(got), got)
	}
}
