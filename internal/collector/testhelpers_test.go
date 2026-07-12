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
