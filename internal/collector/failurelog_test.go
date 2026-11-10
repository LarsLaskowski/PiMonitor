package collector

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

// newCapturingLogger returns a debug-level text logger writing into the
// returned buffer, so tests can assert which lines were (not) logged.
func newCapturingLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})), &buf
}

// countLogLines counts the lines in out that are logged at level (e.g.
// "WARN") and contain substr.
func countLogLines(out, level, substr string) int {
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "level="+level) && strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

func TestFailureLog_RepeatedIdenticalFailureWarnsOnce(t *testing.T) {
	log, buf := newCapturingLogger()
	var f failureLog
	err := errors.New("run vcgencmd get_throttled: exit status 255")

	for range 5 {
		f.observe(log, "throttled state collection failed", err)
	}

	out := buf.String()
	if got := countLogLines(out, "WARN", "throttled state collection failed"); got != 1 {
		t.Fatalf("WARN lines = %d, want 1; log:\n%s", got, out)
	}
	if got := countLogLines(out, "DEBUG", "repeated=true"); got != 4 {
		t.Fatalf("DEBUG repeat lines = %d, want 4; log:\n%s", got, out)
	}
}

func TestFailureLog_ChangedErrorWarnsAgain(t *testing.T) {
	log, buf := newCapturingLogger()
	var f failureLog

	f.observe(log, "m", errors.New("first"))
	f.observe(log, "m", errors.New("first"))
	f.observe(log, "m", errors.New("second"))

	out := buf.String()
	if got := countLogLines(out, "WARN", "error=first"); got != 1 {
		t.Fatalf("WARN lines for first error = %d, want 1; log:\n%s", got, out)
	}
	if got := countLogLines(out, "WARN", "error=second"); got != 1 {
		t.Fatalf("WARN lines for changed error = %d, want 1; log:\n%s", got, out)
	}
}

func TestFailureLog_RecoveryLogsOnceThenFailureWarnsAgain(t *testing.T) {
	log, buf := newCapturingLogger()
	var f failureLog
	err := errors.New("boom")

	f.observe(log, "m", err)
	f.observe(log, "m", nil)
	f.observe(log, "m", nil)
	f.observe(log, "m", err)

	out := buf.String()
	if got := countLogLines(out, "INFO", "recovered"); got != 1 {
		t.Fatalf("INFO recovery lines = %d, want 1; log:\n%s", got, out)
	}
	if !strings.Contains(out, "previous_error=boom") {
		t.Fatalf("recovery line should name the previous error; log:\n%s", out)
	}
	if got := countLogLines(out, "WARN", "error=boom"); got != 2 {
		t.Fatalf("WARN lines = %d, want 2 (before and after recovery); log:\n%s", got, out)
	}
}

func TestFailureLog_SuccessWithoutPriorFailureLogsNothing(t *testing.T) {
	log, buf := newCapturingLogger()
	var f failureLog

	f.observe(log, "m", nil)

	if buf.Len() != 0 {
		t.Fatalf("expected no log output, got:\n%s", buf.String())
	}
}
