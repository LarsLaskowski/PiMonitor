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

// namedLine is a line of the install script with a label for failure messages.
type namedLine struct {
	name string
	idx  int
}

// checkOrder reports every line of group that is not strictly before (wantBefore) or after the refresh line.
func checkOrder(t *testing.T, group []namedLine, refreshIf int, wantBefore bool) {
	t.Helper()
	for _, l := range group {
		if wantBefore && l.idx >= refreshIf {
			t.Errorf("%s at line %d, want it before the refresh at line %d", l.name, l.idx, refreshIf)
		}
		if !wantBefore && l.idx <= refreshIf {
			t.Errorf("%s at line %d, want it after the refresh at line %d", l.name, l.idx, refreshIf)
		}
	}
}

// hasRecoveryEcho reports whether one of lines is an echo that contains recovery.
func hasRecoveryEcho(lines []string, recovery string) bool {
	for _, line := range lines {
		if strings.HasPrefix(line, "echo ") && strings.Contains(line, recovery) {
			return true
		}
	}
	return false
}

func TestInstallScript_RefreshBeforeServiceStart_AC6(t *testing.T) {
	lines := installScriptLines(t)

	enableTimer := onlyIndex(t, lines, "systemctl enable --now pimonitor-apt-update.timer")
	restartTimer := onlyIndex(t, lines, "systemctl restart pimonitor-apt-update.timer")
	refreshIf := onlyIndex(t, lines, "if ! systemctl start pimonitor-apt-update.service; then")
	enableService := onlyIndex(t, lines, "systemctl enable --now pimonitor.service")
	restartService := onlyIndex(t, lines, "systemctl restart pimonitor.service")

	t.Run("AC6 refresh runs after the timer and before the service", func(t *testing.T) {
		checkOrder(t, []namedLine{{"enable of the timer", enableTimer}, {"restart of the timer", restartTimer}}, refreshIf, true)
		checkOrder(t, []namedLine{{"enable of the service", enableService}, {"restart of the service", restartService}}, refreshIf, false)
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
		if !hasRecoveryEcho(lines[last+1:refreshIf], recovery) {
			t.Errorf("no echo line between the timer lines and the refresh contains %q", recovery)
		}
	})
}
