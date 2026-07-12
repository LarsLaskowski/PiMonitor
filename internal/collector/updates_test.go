package collector

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const aptListFixture = `Listing...
curl/stable 7.88.1-10+deb12u5 arm64 [upgradable from: 7.88.1-10+deb12u4]
libssl3/stable,now 3.0.11-1+deb12u2 arm64 [upgradable from: 3.0.11-1+deb12u1]
`

func TestParseAptListUpgradable(t *testing.T) {
	updates := parseAptListUpgradable(aptListFixture)
	if len(updates) != 2 {
		t.Fatalf("expected 2 updates, got %d: %+v", len(updates), updates)
	}
	if updates[0].Name != "curl" || updates[0].NewVersion != "7.88.1-10+deb12u5" || updates[0].OldVersion != "7.88.1-10+deb12u4" {
		t.Fatalf("unexpected first update: %+v", updates[0])
	}
	if updates[0].Arch != "arm64" {
		t.Fatalf("Arch = %q, want arm64", updates[0].Arch)
	}
}

func TestParseAptListUpgradable_NoUpdates(t *testing.T) {
	updates := parseAptListUpgradable("Listing...\n")
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates, got %d", len(updates))
	}
}

func TestParseAptListUpgradable_IgnoresUnrelatedLines(t *testing.T) {
	updates := parseAptListUpgradable("Listing...\nsome-warning: something happened\n")
	if len(updates) != 0 {
		t.Fatalf("expected 0 updates for unrelated lines, got %d: %+v", len(updates), updates)
	}
}

func TestAptCacheAge(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "somepkg_Packages", "irrelevant content")

	now := time.Now().Add(2 * time.Hour)
	age, err := aptCacheAge(dir, now)
	if err != nil {
		t.Fatalf("aptCacheAge: %v", err)
	}
	if age < time.Hour {
		t.Fatalf("expected age >= ~2h, got %v", age)
	}
}

func TestAptCacheAge_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	if _, err := aptCacheAge(dir, time.Now()); err == nil {
		t.Fatal("expected error for empty lists dir")
	}
}

func TestAptCacheAge_MissingDir(t *testing.T) {
	if _, err := aptCacheAge(filepath.Join(t.TempDir(), "missing"), time.Now()); err == nil {
		t.Fatal("expected error for missing lists dir")
	}
}

func TestUpdatesCollector_Collect_StalenessDetection(t *testing.T) {
	dir := t.TempDir()
	writeTempFile(t, dir, "somepkg_Packages", "irrelevant content")

	// Backdate the file so it looks like the cache is very old.
	oldTime := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(filepath.Join(dir, "somepkg_Packages"), oldTime, oldTime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	c := &UpdatesCollector{
		aptPath:        "true", // stub command producing no stdout, always succeeds
		listsDir:       dir,
		staleThreshold: 6 * time.Hour,
		now:            time.Now,
	}

	updates, err := c.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !updates.Stale {
		t.Fatalf("expected Stale=true for a 48h old cache with a 6h threshold, got %+v", updates)
	}
}
