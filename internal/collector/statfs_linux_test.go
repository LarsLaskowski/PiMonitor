//go:build linux

package collector

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestPlatformStatfs(t *testing.T) {
	t.Run("AC4 reports the device ID of the path", func(t *testing.T) {
		dir := t.TempDir()
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		var st syscall.Stat_t
		if err := syscall.Stat(dir, &st); err != nil {
			t.Fatalf("stat: %v", err)
		}
		got, err := platformStatfs(dir)
		if err != nil {
			t.Fatalf("platformStatfs(dir): %v", err)
		}
		if got.Dev != uint64(st.Dev) {
			t.Errorf("Dev = %d, want st_dev %d", got.Dev, st.Dev)
		}
		gotSub, err := platformStatfs(sub)
		if err != nil {
			t.Fatalf("platformStatfs(sub): %v", err)
		}
		if gotSub.Dev != got.Dev {
			t.Errorf("subdirectory Dev = %d, want %d (same filesystem)", gotSub.Dev, got.Dev)
		}
	})
}
