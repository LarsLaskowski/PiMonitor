package collector

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// piholeVersionsPath is written by Pi-hole's own update checker
	// (`pihole updatechecker`, run from /etc/cron.d/pihole daily and on
	// reboot). It is world-readable (0644 pihole:pihole).
	piholeVersionsPath = "/etc/pihole/versions"
	// piholeFTLPath is the Pi-hole v6 FTL binary. Requiring it alongside
	// the versions file keeps a leftover /etc/pihole from an uninstall, or
	// a Docker volume mounted at /etc/pihole on the host, from being
	// reported as a native installation.
	piholeFTLPath = "/usr/bin/pihole-FTL"
	// piholeStaleThreshold is how old the versions file may be before
	// Pihole.Stale is set. Pi-hole refreshes it once a day, so two days
	// means at least one refresh was missed.
	piholeStaleThreshold = 48 * time.Hour
	// piholeMinMajorVersion is the oldest Pi-hole major version detected.
	piholeMinMajorVersion = 6
	// maxPiholeVersionsSize bounds how much of the versions file is read.
	// The real file is a few hundred bytes.
	maxPiholeVersionsSize = 64 * 1024
)

// piholeComponents lists the components in the order they are reported,
// with the key prefix each one uses in the versions file.
var piholeComponents = []struct {
	name   string
	prefix string
}{
	{name: "core", prefix: "CORE"},
	{name: "web", prefix: "WEB"},
	{name: "ftl", prefix: "FTL"},
}

// parsePiholeVersions parses the KEY=VALUE contents of /etc/pihole/versions
// into one PiholeComponent per component that has an installed version.
// It returns an error when no Core version is present, because without it
// the file cannot be from a working Pi-hole installation.
func parsePiholeVersions(content string) ([]PiholeComponent, error) {
	values := map[string]string{}
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("parse pihole versions: %w", err)
	}
	if values["CORE_VERSION"] == "" {
		return nil, errors.New("parse pihole versions: no CORE_VERSION")
	}

	var components []PiholeComponent
	for _, comp := range piholeComponents {
		installed := values[comp.prefix+"_VERSION"]
		if installed == "" {
			continue
		}
		c := PiholeComponent{
			Name:             comp.name,
			InstalledVersion: installed,
			LatestVersion:    values["GITHUB_"+comp.prefix+"_VERSION"],
			Branch:           values[comp.prefix+"_BRANCH"],
		}
		c.UpdateAvailable = piholeUpdateAvailable(c,
			values[comp.prefix+"_HASH"], values["GITHUB_"+comp.prefix+"_HASH"])
		components = append(components, c)
	}
	return components, nil
}

// piholeUpdateAvailable mirrors how Pi-hole itself decides whether an
// update is available. On the release branch (master, or no branch
// recorded) only the version matters: the installed hash is that of the
// release tag and legitimately differs from the branch head's hash. On any
// other branch versions do not move, so the hashes are compared instead.
// An empty "latest" value means the remote check failed and is never
// reported as an update.
func piholeUpdateAvailable(c PiholeComponent, installedHash, latestHash string) bool {
	if c.Branch == "" || c.Branch == "master" {
		return c.LatestVersion != "" && c.InstalledVersion != c.LatestVersion
	}
	return installedHash != "" && latestHash != "" && installedHash != latestHash
}

// piholeMajorVersion extracts the major version from a version string such
// as "v6.5". ok is false when the string does not start with a number
// (e.g. a development build named after its branch).
func piholeMajorVersion(version string) (major int, ok bool) {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	end := strings.IndexFunc(version, func(r rune) bool { return r < '0' || r > '9' })
	if end == 0 {
		return 0, false
	}
	if end > 0 {
		version = version[:end]
	}
	major, err := strconv.Atoi(version)
	if err != nil {
		return 0, false
	}
	return major, true
}

// PiholeCollector reports whether a native Pi-hole v6+ installation is
// present and whether it has updates available, by reading the versions
// file Pi-hole's own update checker maintains. It never contacts a server,
// runs a command, or needs elevated privileges. Docker installations are
// not detected.
type PiholeCollector struct {
	versionsPath   string
	ftlPath        string
	staleThreshold time.Duration
	now            func() time.Time
}

// NewPiholeCollector creates a PiholeCollector for the standard Pi-hole
// file locations.
func NewPiholeCollector() *PiholeCollector {
	return &PiholeCollector{
		versionsPath:   piholeVersionsPath,
		ftlPath:        piholeFTLPath,
		staleThreshold: piholeStaleThreshold,
		now:            time.Now,
	}
}

// Collect reads the Pi-hole versions file. A host without Pi-hole, or
// with a Pi-hole older than v6, is not an error: it reports
// Installed=false. An error means Pi-hole looks installed but its state
// could not be read (e.g. a permission problem or a malformed file).
func (c *PiholeCollector) Collect() (Pihole, error) {
	now := c.now()
	notInstalled := Pihole{CheckedAt: now}

	if _, err := os.Stat(c.ftlPath); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return notInstalled, nil
		}
		return Pihole{}, fmt.Errorf("stat %s: %w", c.ftlPath, err)
	}

	f, err := os.Open(c.versionsPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return notInstalled, nil
		}
		return Pihole{}, fmt.Errorf("open %s: %w", c.versionsPath, err)
	}
	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return Pihole{}, fmt.Errorf("stat %s: %w", c.versionsPath, err)
	}
	content, err := io.ReadAll(io.LimitReader(f, maxPiholeVersionsSize))
	if err != nil {
		return Pihole{}, fmt.Errorf("read %s: %w", c.versionsPath, err)
	}

	components, err := parsePiholeVersions(string(content))
	if err != nil {
		return Pihole{}, fmt.Errorf("%s: %w", c.versionsPath, err)
	}
	if major, ok := piholeMajorVersion(components[0].InstalledVersion); ok && major < piholeMinMajorVersion {
		return notInstalled, nil
	}

	p := Pihole{
		Installed:  true,
		Components: components,
		CheckedAt:  now,
	}
	for _, comp := range components {
		if comp.UpdateAvailable {
			p.UpdateAvailable = true
		}
	}
	age := now.Sub(info.ModTime())
	if age < 0 {
		age = 0
	}
	p.CacheAgeSeconds = age.Seconds()
	p.Stale = c.staleThreshold > 0 && age > c.staleThreshold
	return p, nil
}
