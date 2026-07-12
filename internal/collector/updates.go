package collector

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const aptListsDir = "/var/lib/apt/lists"

// packageUpdateLine matches a line of `apt list --upgradable` output, e.g.:
//
//	curl/stable 7.88.1-10+deb12u5 arm64 [upgradable from: 7.88.1-10+deb12u4]
var packageUpdateLine = regexp.MustCompile(`^(\S+)/\S+\s+(\S+)\s+(\S+)\s+\[upgradable from:\s*([^\]]+)\]`)

// parseAptListUpgradable parses the stdout of `apt list --upgradable`.
// The first "Listing..." line (if present) is ignored; an empty result
// means no packages are upgradable.
func parseAptListUpgradable(output string) []PackageUpdate {
	var updates []PackageUpdate
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "Listing...") {
			continue
		}
		m := packageUpdateLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		updates = append(updates, PackageUpdate{
			Name:       m[1],
			NewVersion: m[2],
			Arch:       m[3],
			OldVersion: strings.TrimSpace(m[4]),
		})
	}
	return updates
}

// aptCacheAge returns how long ago the apt package lists were last
// refreshed, based on the newest mtime among files in
// /var/lib/apt/lists/*_Packages (or similar). Returns an error if the
// directory can't be read or contains no list files, in which case
// staleness cannot be determined.
func aptCacheAge(listsDir string, now time.Time) (time.Duration, error) {
	entries, err := os.ReadDir(listsDir)
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", listsDir, err)
	}
	var newest time.Time
	found := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.ModTime().After(newest) {
			newest = info.ModTime()
			found = true
		}
	}
	if !found {
		return 0, fmt.Errorf("no apt list files found in %s", listsDir)
	}
	return now.Sub(newest), nil
}

// UpdatesCollector reports available apt package updates by reading the
// existing apt cache. It never triggers a cache refresh itself (that is
// the job of a separate, root-privileged systemd timer) - it only shells
// out to the read-only `apt list --upgradable`, which never contacts a
// server or requires elevated privileges.
type UpdatesCollector struct {
	aptPath        string
	listsDir       string
	staleThreshold time.Duration
	now            func() time.Time
}

// NewUpdatesCollector creates an UpdatesCollector. staleThreshold should
// typically be a small multiple of the apt-update timer's interval, so a
// missed/delayed refresh is surfaced to the user rather than silently
// showing outdated counts.
func NewUpdatesCollector(staleThreshold time.Duration) *UpdatesCollector {
	return &UpdatesCollector{
		aptPath:        "apt",
		listsDir:       aptListsDir,
		staleThreshold: staleThreshold,
		now:            time.Now,
	}
}

// Collect runs `apt list --upgradable` and reports the result alongside
// apt cache staleness.
func (c *UpdatesCollector) Collect(ctx context.Context) (Updates, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.aptPath, "list", "--upgradable")
	cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.Output()
	if err != nil {
		return Updates{}, fmt.Errorf("run apt list --upgradable: %w", err)
	}

	now := c.now()
	packages := parseAptListUpgradable(string(out))

	updates := Updates{
		Count:     len(packages),
		Packages:  packages,
		CheckedAt: now,
	}

	if age, err := aptCacheAge(c.listsDir, now); err == nil {
		updates.CacheAgeSeconds = age.Seconds()
		updates.Stale = c.staleThreshold > 0 && age > c.staleThreshold
	}

	return updates, nil
}
