package collector

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// piholeVersionsUpToDate is a verbatim /etc/pihole/versions from a Pi-hole
// v6 installation with no pending updates. Note CORE_HASH and
// GITHUB_CORE_HASH differ even though Core is current: on master the
// installed hash is the release tag's, not the branch head's, which is why
// hashes must not be compared there.
const piholeVersionsUpToDate = `CORE_VERSION=v6.5
CORE_BRANCH=master
CORE_HASH=f503a65d
GITHUB_CORE_VERSION=v6.5
GITHUB_CORE_HASH=259d8f9e
WEB_VERSION=v6.6.1
WEB_BRANCH=master
WEB_HASH=fb699c4e
GITHUB_WEB_VERSION=v6.6.1
GITHUB_WEB_HASH=fb699c4e
FTL_VERSION=v6.8
FTL_BRANCH=master
FTL_HASH=29c8b13f
GITHUB_FTL_VERSION=v6.8
GITHUB_FTL_HASH=29c8b13f
`

// piholeVersionsCoreAndFTLUpdate has newer Core and FTL releases available.
const piholeVersionsCoreAndFTLUpdate = `CORE_VERSION=v6.5
CORE_BRANCH=master
CORE_HASH=f503a65d
GITHUB_CORE_VERSION=v6.6
GITHUB_CORE_HASH=11111111
WEB_VERSION=v6.6.1
WEB_BRANCH=master
WEB_HASH=fb699c4e
GITHUB_WEB_VERSION=v6.6.1
GITHUB_WEB_HASH=fb699c4e
FTL_VERSION=v6.8
FTL_BRANCH=master
FTL_HASH=29c8b13f
GITHUB_FTL_VERSION=v6.9
GITHUB_FTL_HASH=22222222
`

func findPiholeComponent(t *testing.T, components []PiholeComponent, name string) PiholeComponent {
	t.Helper()
	for _, c := range components {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("component %q not found in %+v", name, components)
	return PiholeComponent{}
}

func TestParsePiholeVersions_UpToDate(t *testing.T) {
	components, err := parsePiholeVersions(piholeVersionsUpToDate)
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	if len(components) != 3 {
		t.Fatalf("expected 3 components, got %d: %+v", len(components), components)
	}
	want := []PiholeComponent{
		{Name: "core", InstalledVersion: "v6.5", LatestVersion: "v6.5", Branch: "master"},
		{Name: "web", InstalledVersion: "v6.6.1", LatestVersion: "v6.6.1", Branch: "master"},
		{Name: "ftl", InstalledVersion: "v6.8", LatestVersion: "v6.8", Branch: "master"},
	}
	for i, w := range want {
		if components[i] != w {
			t.Errorf("components[%d] = %+v, want %+v", i, components[i], w)
		}
	}
}

func TestParsePiholeVersions_UpdatesAvailable(t *testing.T) {
	components, err := parsePiholeVersions(piholeVersionsCoreAndFTLUpdate)
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	if c := findPiholeComponent(t, components, "core"); !c.UpdateAvailable || c.LatestVersion != "v6.6" {
		t.Errorf("core = %+v, want update to v6.6", c)
	}
	if c := findPiholeComponent(t, components, "web"); c.UpdateAvailable {
		t.Errorf("web = %+v, want no update", c)
	}
	if c := findPiholeComponent(t, components, "ftl"); !c.UpdateAvailable || c.LatestVersion != "v6.9" {
		t.Errorf("ftl = %+v, want update to v6.9", c)
	}
}

func TestParsePiholeVersions_DevelopmentBranchComparesHashes(t *testing.T) {
	// updatecheck.sh always writes GITHUB_*_VERSION=null on non-master
	// branches.
	content := `CORE_VERSION=v6.5-3-gaaaaaaaa
CORE_BRANCH=development
CORE_HASH=aaaaaaaa
GITHUB_CORE_VERSION=null
GITHUB_CORE_HASH=bbbbbbbb
FTL_VERSION=v6.8
FTL_BRANCH=development
FTL_HASH=cccccccc
GITHUB_FTL_VERSION=null
GITHUB_FTL_HASH=cccccccc
`
	components, err := parsePiholeVersions(content)
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	if c := findPiholeComponent(t, components, "core"); !c.UpdateAvailable || c.LatestVersion != "" {
		t.Errorf("core = %+v, want update (hashes differ on a non-master branch) and no \"null\" latest version", c)
	}
	if c := findPiholeComponent(t, components, "ftl"); c.UpdateAvailable {
		t.Errorf("ftl = %+v, want no update (hashes equal on a non-master branch)", c)
	}
}

func TestParsePiholeVersions_EmptyLatestIsNotAnUpdate(t *testing.T) {
	// A failed remote check leaves the GITHUB_* values empty.
	content := `CORE_VERSION=v6.5
CORE_BRANCH=master
CORE_HASH=f503a65d
GITHUB_CORE_VERSION=
GITHUB_CORE_HASH=
FTL_VERSION=v6.8
FTL_BRANCH=feature/x
FTL_HASH=29c8b13f
GITHUB_FTL_HASH=
`
	components, err := parsePiholeVersions(content)
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	for _, c := range components {
		if c.UpdateAvailable {
			t.Errorf("%s = %+v, want no update when the latest value is unknown", c.Name, c)
		}
	}
}

// TestParsePiholeVersions_NullMeansUnknown covers the literal "null" that
// Pi-hole's updatecheck.sh writes when a lookup fails (offline, GitHub
// rate limit, git error): it must never be reported as a version or as an
// update.
func TestParsePiholeVersions_NullMeansUnknown(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name: "master, remote check failed",
			content: `CORE_VERSION=v6.5
CORE_BRANCH=master
CORE_HASH=f503a65d
GITHUB_CORE_VERSION=null
GITHUB_CORE_HASH=null
FTL_VERSION=v6.8
FTL_BRANCH=master
FTL_HASH=29c8b13f
GITHUB_FTL_VERSION=null
GITHUB_FTL_HASH=null
`,
		},
		{
			name: "development, remote hash failed",
			content: `CORE_VERSION=v6.5-3-gaaaaaaaa
CORE_BRANCH=development
CORE_HASH=aaaaaaaa
GITHUB_CORE_VERSION=null
GITHUB_CORE_HASH=null
`,
		},
		{
			name: "development, local hash failed",
			content: `CORE_VERSION=v6.5-3-gaaaaaaaa
CORE_BRANCH=development
CORE_HASH=null
GITHUB_CORE_VERSION=null
GITHUB_CORE_HASH=bbbbbbbb
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			components, err := parsePiholeVersions(tc.content)
			if err != nil {
				t.Fatalf("parsePiholeVersions: %v", err)
			}
			for _, c := range components {
				if c.UpdateAvailable {
					t.Errorf("%s = %+v, want no update when a value is \"null\"", c.Name, c)
				}
				if c.LatestVersion != "" {
					t.Errorf("%s LatestVersion = %q, want \"null\" reported as empty", c.Name, c.LatestVersion)
				}
			}
		})
	}
}

func TestParsePiholeVersions_NullLocalVersionSkipsComponent(t *testing.T) {
	components, err := parsePiholeVersions("CORE_VERSION=v6.5\nWEB_VERSION=null\n")
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	if len(components) != 1 || components[0].Name != "core" {
		t.Fatalf("components = %+v, want only core (web version unknown)", components)
	}
	if _, err := parsePiholeVersions("CORE_VERSION=null\n"); err == nil {
		t.Fatal("expected an error for CORE_VERSION=null")
	}
}

func TestParsePiholeVersions_SkipsMissingComponentAndJunk(t *testing.T) {
	content := `# comment
not a key value line
CORE_VERSION="v6.5"
CORE_BRANCH=master
GITHUB_CORE_VERSION='v6.5'
`
	components, err := parsePiholeVersions(content)
	if err != nil {
		t.Fatalf("parsePiholeVersions: %v", err)
	}
	if len(components) != 1 {
		t.Fatalf("expected only core, got %+v", components)
	}
	if components[0].InstalledVersion != "v6.5" || components[0].LatestVersion != "v6.5" || components[0].UpdateAvailable {
		t.Fatalf("core = %+v, want quotes stripped and no update", components[0])
	}
}

func TestParsePiholeVersions_NoCoreVersion(t *testing.T) {
	for _, content := range []string{"", "FTL_VERSION=v6.8\n", "CORE_VERSION=\n"} {
		if _, err := parsePiholeVersions(content); err == nil {
			t.Errorf("parsePiholeVersions(%q): expected an error", content)
		}
	}
}

func TestPiholeMajorVersion(t *testing.T) {
	cases := []struct {
		in    string
		major int
		ok    bool
	}{
		{"v6.5", 6, true},
		{"v5.18.3", 5, true},
		{"v10", 10, true},
		{"6.1", 6, true},
		{"vDev", 0, false},
		{"development", 0, false},
		{"", 0, false},
	}
	for _, tc := range cases {
		major, ok := piholeMajorVersion(tc.in)
		if major != tc.major || ok != tc.ok {
			t.Errorf("piholeMajorVersion(%q) = (%d, %v), want (%d, %v)", tc.in, major, ok, tc.major, tc.ok)
		}
	}
}

// newTestPiholeCollector returns a collector pointed at a fixture
// directory. ftl and versions are written only when non-empty.
func newTestPiholeCollector(t *testing.T, ftl bool, versions string, now time.Time) *PiholeCollector {
	t.Helper()
	dir := t.TempDir()
	c := &PiholeCollector{
		versionsPath:   filepath.Join(dir, "versions"),
		ftlPath:        filepath.Join(dir, "pihole-FTL"),
		staleThreshold: piholeStaleThreshold,
		now:            func() time.Time { return now },
	}
	if ftl {
		writeTempFile(t, dir, "pihole-FTL", "")
	}
	if versions != "" {
		writeTempFile(t, dir, "versions", versions)
	}
	return c
}

func TestPiholeCollector_Collect_NotInstalled(t *testing.T) {
	now := time.Now()
	cases := map[string]*PiholeCollector{
		"nothing present":    newTestPiholeCollector(t, false, "", now),
		"no FTL binary":      newTestPiholeCollector(t, false, piholeVersionsUpToDate, now),
		"no versions file":   newTestPiholeCollector(t, true, "", now),
		"pi-hole v5 version": newTestPiholeCollector(t, true, "CORE_VERSION=v5.18.3\nGITHUB_CORE_VERSION=v5.18.4\n", now),
	}
	for name, c := range cases {
		p, err := c.Collect()
		if err != nil {
			t.Errorf("%s: Collect: %v", name, err)
			continue
		}
		if p.Installed || p.UpdateAvailable || len(p.Components) != 0 {
			t.Errorf("%s: got %+v, want not installed", name, p)
		}
		if !p.CheckedAt.Equal(now) {
			t.Errorf("%s: CheckedAt = %v, want %v", name, p.CheckedAt, now)
		}
	}
}

func TestPiholeCollector_Collect_InstalledUpToDate(t *testing.T) {
	c := newTestPiholeCollector(t, true, piholeVersionsUpToDate, time.Now())
	p, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !p.Installed || p.UpdateAvailable || len(p.Components) != 3 || p.Stale {
		t.Fatalf("got %+v, want installed, 3 components, no update, not stale", p)
	}
}

func TestPiholeCollector_Collect_UpdateAvailable(t *testing.T) {
	c := newTestPiholeCollector(t, true, piholeVersionsCoreAndFTLUpdate, time.Now())
	p, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !p.Installed || !p.UpdateAvailable {
		t.Fatalf("got %+v, want installed with an update available", p)
	}
}

func TestPiholeCollector_Collect_Stale(t *testing.T) {
	c := newTestPiholeCollector(t, true, piholeVersionsUpToDate, time.Now())
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(c.versionsPath, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	p, err := c.Collect()
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !p.Stale {
		t.Fatalf("got %+v, want Stale for a 72h old versions file", p)
	}
	if p.CacheAgeSeconds < (71 * time.Hour).Seconds() {
		t.Fatalf("CacheAgeSeconds = %v, want about 72h", p.CacheAgeSeconds)
	}
}

func TestPiholeCollector_Collect_MalformedFileIsAnError(t *testing.T) {
	c := newTestPiholeCollector(t, true, "garbage\n", time.Now())
	if _, err := c.Collect(); err == nil {
		t.Fatal("expected an error for a versions file without CORE_VERSION")
	}
}

func TestPiholeCollector_Collect_UnreadableFileIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	c := newTestPiholeCollector(t, true, piholeVersionsUpToDate, time.Now())
	if err := os.Chmod(c.versionsPath, 0); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	_, err := c.Collect()
	if err == nil || !strings.Contains(err.Error(), "open") {
		t.Fatalf("Collect error = %v, want an open error", err)
	}
}
