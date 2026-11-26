package collector

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const aptListsDir = "/var/lib/apt/lists"

// dpkgStatusPath is the dpkg database of installed packages.
const dpkgStatusPath = "/var/lib/dpkg/status"

// maxAptOutputBytes bounds the bytes read from the apt-cache child process.
const maxAptOutputBytes int64 = 256 << 20

// maxAptLineBytes bounds one line of the dpkg status file or apt-cache output.
const maxAptLineBytes = 1 << 20

// updatesTimeout bounds one update check.
const updatesTimeout = 10 * time.Second

// newestListModTime returns the newest modification time among the files
// (not directories) of listsDir. It fails when the directory cannot be read
// or holds no readable file.
func newestListModTime(listsDir string) (time.Time, error) {
	entries, err := os.ReadDir(listsDir)
	if err != nil {
		return time.Time{}, fmt.Errorf("read %s: %w", listsDir, err)
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
		return time.Time{}, fmt.Errorf("no apt list files found in %s", listsDir)
	}
	return newest, nil
}

// aptCacheAge returns how long ago the apt package lists were last
// refreshed, based on the newest list-file modification time. It returns an
// error if the directory can't be read or contains no list files, in which
// case staleness cannot be determined.
func aptCacheAge(listsDir string, now time.Time) (time.Duration, error) {
	newest, err := newestListModTime(listsDir)
	if err != nil {
		return 0, err
	}
	return now.Sub(newest), nil
}

// errOutputTooLarge reports that the apt-cache child process wrote more than
// the allowed number of bytes.
var errOutputTooLarge = errors.New("apt-cache dumpavail output exceeds 256 MiB")

// UpdatesCollector reports available apt package updates by comparing the
// dpkg status file with the candidates of `apt-cache dumpavail`. It never
// triggers a cache refresh itself (that is the job of a separate,
// root-privileged systemd timer).
type UpdatesCollector struct {
	aptCachePath   string
	statusPath     string
	listsDir       string
	staleThreshold time.Duration
	timeout        time.Duration
	maxOutput      int64
	now            func() time.Time
}

// NewUpdatesCollector creates an UpdatesCollector. staleThreshold should
// typically be a small multiple of the apt-update timer's interval, so a
// missed/delayed refresh is surfaced to the user rather than silently
// showing outdated counts.
func NewUpdatesCollector(staleThreshold time.Duration) *UpdatesCollector {
	return &UpdatesCollector{
		aptCachePath:   "apt-cache",
		statusPath:     dpkgStatusPath,
		listsDir:       aptListsDir,
		staleThreshold: staleThreshold,
		now:            time.Now,
	}
}

// Collect reports the available updates alongside apt cache staleness.
func (c *UpdatesCollector) Collect(ctx context.Context) (Updates, error) {
	return Updates{}, errors.New("not implemented")
}

// pkgKey identifies a package by name and architecture.
type pkgKey struct {
	name string
	arch string
}

// installedSet holds the installed package versions and the native architecture.
type installedSet struct {
	native   string
	versions map[pkgKey]debianVersion
}

// debianVersion is a parsed Debian package version.
type debianVersion struct {
	raw      string
	epoch    uint32
	upstream string
	revision string
}

// parseDebianVersion splits v into epoch, upstream version and revision.
func parseDebianVersion(v string) (debianVersion, error) {
	return debianVersion{}, errors.New("not implemented")
}

// compareDebianVersions orders a and b: negative, zero or positive as a is
// older than, equal to or newer than b.
func compareDebianVersions(a, b debianVersion) int {
	return 0
}

// compareVersionPart compares an upstream or revision string by dpkg's rules.
func compareVersionPart(a, b string) int {
	return 0
}

// controlRecord is one record of a control-format stream.
type controlRecord struct {
	index   int
	pkg     string
	version string
	arch    string
	status  string
}

// scanControlRecords reads control-format records from r and calls visit for each.
func scanControlRecords(r io.Reader, visit func(rec controlRecord) error) error {
	return errors.New("not implemented")
}

// readInstalled collects the installed packages of a dpkg status stream.
func readInstalled(r io.Reader) (installedSet, error) {
	return installedSet{}, errors.New("not implemented")
}

// findUpgrades reads an apt-cache dumpavail stream and returns the packages
// whose candidate is newer than the installed version.
func findUpgrades(r io.Reader, installed installedSet) ([]PackageUpdate, error) {
	return nil, errors.New("not implemented")
}

// newCappedReader returns a reader that fails with errOutputTooLarge once
// more than limit bytes have been read from r.
func newCappedReader(r io.Reader, limit int64) io.Reader {
	return r
}

// stateStamp is a fingerprint of the apt lists and the dpkg status file.
type stateStamp struct {
	listsDirModTime int64
	listsCount      int
	listsSize       int64
	listsNewest     int64
	statusModTime   int64
	statusSize      int64
}

// aptStateStamp returns the current fingerprint; an unreadable part is zero.
func aptStateStamp(listsDir, statusPath string) stateStamp {
	return stateStamp{}
}
