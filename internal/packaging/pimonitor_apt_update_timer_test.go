// Package packaging holds the tests of the scripts and systemd units under packaging/, which ship in the release
// archive and therefore cannot carry their own test files (decision 0033).
package packaging

import (
	"os"
	"strings"
	"testing"
)

const aptUpdateTimerPath = "../../packaging/pimonitor-apt-update.timer"

// timerLines returns the trimmed, non-empty lines of the unit file at path, each with the section it belongs to.
func timerLines(t *testing.T, path string) (lines, sections []string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	section := ""
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section = line
			continue
		}
		lines = append(lines, line)
		sections = append(sections, section)
	}
	return lines, sections
}

func TestAptUpdateTimer_Schedule_AC1(t *testing.T) {
	lines, sections := timerLines(t, aptUpdateTimerPath)

	persistent := 0
	counts := map[string]int{}
	for i, line := range lines {
		if strings.HasPrefix(line, "Persistent=") {
			persistent++
		}
		if sections[i] == "[Timer]" {
			counts[line]++
		}
	}

	if persistent != 0 {
		t.Errorf("Persistent= lines = %d, want 0 (it only applies to OnCalendar= timers)", persistent)
	}
	for _, want := range []string{"OnBootSec=10min", "OnUnitActiveSec=6h"} {
		if counts[want] != 1 {
			t.Errorf("%q lines in [Timer] = %d, want 1", want, counts[want])
		}
	}
}
