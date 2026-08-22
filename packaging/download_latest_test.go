package packaging

import (
	"os"
	"strings"
	"testing"
)

func TestDownloadLatestScript_MapsRaspberryPiArchitectures(t *testing.T) {
	script, err := os.ReadFile("download-latest.sh")
	if err != nil {
		t.Fatalf("read download-latest.sh: %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{"64-bit Raspberry Pi OS", "aarch64|arm64)\n      ARCH='arm64'"},
		{"32-bit Raspberry Pi OS", "armv6l|armv7l|armv8l)\n      # The armv6 binary also runs on newer 32-bit ARM Raspberry Pi OS.\n      ARCH='armv6'"},
		{"manual override", "if [[ -z \"${ARCH:-}\" ]]; then"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(string(script), tt.want) {
				t.Fatalf("download-latest.sh is missing the %s architecture rule", tt.name)
			}
		})
	}
}
