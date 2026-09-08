package collector

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// writeFakeProcess writes a synthetic /proc/<pid> directory (stat and
// status files) under root, for ProcessCollector tests that need a fixture
// pid set without touching real /proc. utime/stime are the raw jiffie
// counters /proc/<pid>/stat reports; rssKB is the value written on the
// VmRSS line of /proc/<pid>/status.
func writeFakeProcess(t *testing.T, root string, pid int, comm string, utime, stime, rssKB uint64) {
	t.Helper()
	dir := filepath.Join(root, strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	// Field layout mirrors a real /proc/<pid>/stat line: pid (comm) state
	// ppid pgrp session tty_nr tpgid flags minflt cminflt majflt cmajflt
	// utime stime ... — only utime/stime are ever read by the collector, so
	// every other field is a fixed placeholder.
	stat := fmt.Sprintf("%d (%s) S 1 1 1 0 -1 0 0 0 0 0 %d %d 0 0 20 0 1 0 0 0 0\n", pid, comm, utime, stime)
	writeTempFile(t, dir, "stat", stat)
	status := fmt.Sprintf("Name:\t%s\nVmRSS:\t%d kB\n", comm, rssKB)
	writeTempFile(t, dir, "status", status)
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
