package packaging

import (
	"os"
	"strings"
	"testing"
)

const installScriptPath = "../../packaging/install.sh"

// installScriptLines returns the trimmed lines of install.sh without blank and comment lines, with \r\n normalized.
func installScriptLines(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(installScriptPath)
	if err != nil {
		t.Fatalf("read %s: %v", installScriptPath, err)
	}
	var lines []string
	for _, line := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// lineIndexes returns the indexes of the lines equal to want.
func lineIndexes(lines []string, want string) []int {
	var idx []int
	for i, line := range lines {
		if line == want {
			idx = append(idx, i)
		}
	}
	return idx
}

// onlyIndex returns the index of the one line equal to want and fails the test when there are none or several.
func onlyIndex(t *testing.T, lines []string, want string) int {
	t.Helper()
	idx := lineIndexes(lines, want)
	if len(idx) != 1 {
		t.Fatalf("lines equal to %q = %d, want 1", want, len(idx))
	}
	return idx[0]
}

func TestInstallScript_RefreshBeforeServiceStart_AC6(t *testing.T) {
	lines := installScriptLines(t)

	enableTimer := onlyIndex(t, lines, "systemctl enable --now pimonitor-apt-update.timer")
	restartTimer := onlyIndex(t, lines, "systemctl restart pimonitor-apt-update.timer")
	refreshIf := onlyIndex(t, lines, "if ! systemctl start pimonitor-apt-update.service; then")
	enableService := onlyIndex(t, lines, "systemctl enable --now pimonitor.service")
	restartService := onlyIndex(t, lines, "systemctl restart pimonitor.service")

	t.Run("AC6 refresh runs after the timer and before the service", func(t *testing.T) {
		for _, before := range []struct {
			name string
			idx  int
		}{{"enable of the timer", enableTimer}, {"restart of the timer", restartTimer}} {
			if before.idx >= refreshIf {
				t.Errorf("%s at line %d, want it before the refresh at line %d", before.name, before.idx, refreshIf)
			}
		}
		for _, after := range []struct {
			name string
			idx  int
		}{{"enable of the service", enableService}, {"restart of the service", restartService}} {
			if after.idx <= refreshIf {
				t.Errorf("%s at line %d, want it after the refresh at line %d", after.name, after.idx, refreshIf)
			}
		}
	})

	t.Run("AC6 a failed refresh only warns", func(t *testing.T) {
		if refreshIf+2 >= len(lines) {
			t.Fatalf("refresh at line %d is the end of the script, want a warning and fi after it", refreshIf)
		}
		if !strings.HasPrefix(lines[refreshIf+1], "echo ") || !strings.Contains(lines[refreshIf+1], "warning") {
			t.Errorf("line after the refresh = %q, want an echo with the warning", lines[refreshIf+1])
		}
		if lines[refreshIf+2] != "fi" {
			t.Errorf("second line after the refresh = %q, want fi (no exit in the failure branch)", lines[refreshIf+2])
		}
	})

	t.Run("AC6 an echo before the refresh names the recovery command", func(t *testing.T) {
		const recovery = "sudo systemctl restart pimonitor.service"
		last := max(enableTimer, restartTimer)
		found := false
		for _, line := range lines[last+1 : refreshIf] {
			if strings.HasPrefix(line, "echo ") && strings.Contains(line, recovery) {
				found = true
			}
		}
		if !found {
			t.Errorf("no echo line between the timer lines and the refresh contains %q", recovery)
		}
	})
}
