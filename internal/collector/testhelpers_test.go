package collector

import (
	"os"
	"path/filepath"
	"testing"
)

// writeTempFile writes content to name within dir and returns the full
// path, failing the test on error.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file %s: %v", path, err)
	}
	return path
}

// overwriteTempFile replaces the content of an existing temp file.
func overwriteTempFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("overwrite temp file %s: %v", path, err)
	}
}

// writeFakeVcgencmd writes an executable shell script standing in for the
// real vcgencmd binary (mirroring the "true"/"false" stub-command pattern
// used in updates_test.go) and returns its path. script is the script body,
// e.g. `echo "temp=42.8'C"` or `exit 1`, and runs regardless of the
// subcommand argument it's invoked with.
func writeFakeVcgencmd(t *testing.T, dir, name, script string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatalf("write fake vcgencmd %s: %v", path, err)
	}
	return path
}
