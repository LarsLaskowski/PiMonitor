package collector

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// The apt fixtures under testdata/apt are whole records trimmed from a real
// Raspberry Pi OS Trixie arm64 host: trixie-arm64-status.txt is its
// /var/lib/dpkg/status, trixie-arm64-dumpavail.txt the matching `apt-cache
// dumpavail` output and trixie-arm64-upgradable.txt the `apt list
// --upgradable` output of the same host (the oracle).

// aptSampleUpgradeCount is the number of lines `apt list --upgradable`
// prints in trixie-arm64-upgradable.txt.
const aptSampleUpgradeCount = 28

const (
	errNoDpkgDumpavail = "apt-cache dumpavail has no dpkg record for the native architecture"
	errNoDpkgStatus    = "dpkg status has no installed dpkg record"
	errStateChanged    = "apt or dpkg state changed during the check"
	errDumpavailLine   = "apt-cache dumpavail: line exceeds 1 MiB"
	errStatusLine      = "dpkg status: line exceeds 1 MiB"
	errRunPrefix       = "run apt-cache dumpavail:"
)

// readAptFixture returns the content of testdata/apt/name with line ends
// normalized to \n.
func readAptFixture(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "apt", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.ReplaceAll(string(data), "\r\n", "\n")
}

// rec renders one control-format record from name/value pairs (a pair with
// an empty value is left out) and closes it with a blank line.
func rec(pairs ...string) string {
	var b strings.Builder
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			b.WriteString(pairs[i] + ": " + pairs[i+1] + "\n")
		}
	}
	b.WriteString("\n")
	return b.String()
}

// statusRec renders a dpkg status record.
func statusRec(pkg, status, version, arch string) string {
	return rec("Package", pkg, "Status", status, "Architecture", arch, "Version", version)
}

// installedRec renders the status record of an installed package.
func installedRec(pkg, version, arch string) string {
	return statusRec(pkg, "install ok installed", version, arch)
}

// availRec renders an apt-cache dumpavail record.
func availRec(pkg, version, arch string) string {
	return rec("Package", pkg, "Version", version, "Architecture", arch)
}

var (
	dpkgStatusFixture = installedRec("dpkg", "1.22.22", "arm64")
	dpkgAvailFixture  = availRec("dpkg", "1.22.22", "arm64")
)

// installedOf parses a dpkg status text, failing the test on an error.
func installedOf(t *testing.T, status string) installedSet {
	t.Helper()
	set, err := readInstalled(strings.NewReader(status))
	if err != nil {
		t.Fatalf("readInstalled: %v", err)
	}
	return set
}

// findIn runs findUpgrades on avail against the packages of status.
func findIn(t *testing.T, status, avail string) ([]PackageUpdate, error) {
	t.Helper()
	return findUpgrades(strings.NewReader(avail), installedOf(t, status))
}

// checkUpdates fails the test when got and want differ.
func checkUpdates(t *testing.T, got, want []PackageUpdate) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("updates = %+v, want %+v", got, want)
	}
}

// checkErrorText fails the test unless err's text is exactly want.
func checkErrorText(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want %q", want)
	}
	if err.Error() != want {
		t.Fatalf("error = %q, want %q", err.Error(), want)
	}
}

// checkErrorContains fails the test unless err's text contains want.
func checkErrorContains(t *testing.T, err error, want string) {
	t.Helper()
	if err == nil {
		t.Fatalf("error = nil, want one containing %q", want)
	}
	if !strings.Contains(err.Error(), want) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), want)
	}
}

var aptListLine = regexp.MustCompile(`^(\S+?)/\S+ (\S+) (\S+) \[upgradable from: (\S+)\]$`)

// parseAptOracle turns the output of `apt list --upgradable` into the
// expected updates, sorted by name and architecture.
func parseAptOracle(t *testing.T, text string) []PackageUpdate {
	t.Helper()
	var out []PackageUpdate
	for _, line := range strings.Split(strings.TrimSpace(text), "\n") {
		if line == "Listing..." {
			continue
		}
		m := aptListLine.FindStringSubmatch(line)
		if m == nil {
			t.Fatalf("oracle line %q does not match the apt list format", line)
		}
		out = append(out, PackageUpdate{Name: m[1], NewVersion: m[2], Arch: m[3], OldVersion: m[4]})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Arch < out[j].Arch
	})
	return out
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

func TestParseDebianVersion(t *testing.T) {
	valid := []struct {
		in   string
		want debianVersion
	}{
		{"1.0", debianVersion{raw: "1.0", upstream: "1.0"}},
		{"1:2.3-4", debianVersion{raw: "1:2.3-4", epoch: 1, upstream: "2.3", revision: "4"}},
		{"0:1.0", debianVersion{raw: "0:1.0", upstream: "1.0"}},
		{"1.2-3-4", debianVersion{raw: "1.2-3-4", upstream: "1.2-3", revision: "4"}},
		{"3.6-5+deb13u1", debianVersion{raw: "3.6-5+deb13u1", upstream: "3.6", revision: "5+deb13u1"}},
		{"2.3.3-14~deb13u3", debianVersion{raw: "2.3.3-14~deb13u3", upstream: "2.3.3", revision: "14~deb13u3"}},
		{"4294967295:1", debianVersion{raw: "4294967295:1", epoch: 4294967295, upstream: "1"}},
	}
	for _, tt := range valid {
		t.Run("AC1 valid "+tt.in, func(t *testing.T) {
			got, err := parseDebianVersion(tt.in)
			if err != nil {
				t.Fatalf("parseDebianVersion(%q): %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("parseDebianVersion(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseDebianVersion_Invalid(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", "invalid upstream version"},
		{"letter first", "a1.0", "invalid upstream version"},
		{"bad character", "1.0_1", "invalid upstream version"},
		{"space", "1.0 1", "invalid upstream version"},
		{"colon after epoch", "1:2:3", "invalid upstream version"},
		{"colon in upstream", "1.0:2", "invalid epoch"},
		{"non-numeric epoch", "x:1.0", "invalid epoch"},
		{"empty epoch", ":1.0", "invalid epoch"},
		{"overflowing epoch", "4294967296:1.0", "invalid epoch"},
		{"empty revision", "1.0-", "invalid revision"},
		{"empty upstream", "1:", "invalid upstream version"},
		{"tilde first", "~1.0", "invalid upstream version"},
	}
	for _, tt := range tests {
		t.Run("AC3 "+tt.name, func(t *testing.T) {
			got, err := parseDebianVersion(tt.in)
			if err == nil {
				t.Fatalf("parseDebianVersion(%q) = %+v, want an error", tt.in, got)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("parseDebianVersion(%q) error = %q, want it to contain %q", tt.in, err, tt.want)
			}
			if got != (debianVersion{}) {
				t.Fatalf("parseDebianVersion(%q) = %+v, want the zero value with an error", tt.in, got)
			}
		})
	}
}

func TestCompareDebianVersions(t *testing.T) {
	// want is the sign of dpkg --compare-versions a b: -1 older, 0 equal, 1 newer.
	tests := []struct {
		name string
		a, b string
		want int
	}{
		{"equal", "1.0", "1.0", 0},
		{"numeric", "1.2", "1.10", -1},
		{"epoch beats upstream", "1:0.5", "9.9", 1},
		{"epoch zero equals no epoch", "0:1.0", "1.0", 0},
		{"tilde before end", "1.0~rc1", "1.0", -1},
		{"tilde runs", "1.0~~", "1.0~", -1},
		{"tilde before letters", "1.0~", "1.0a", -1},
		{"tilde rc order", "1.0~rc1", "1.0~rc2", -1},
		{"end before letters", "1.0", "1.0a", -1},
		{"letters before non-letters", "1.0a", "1.0+", -1},
		{"letters in ASCII order", "1.0Z", "1.0a", -1},
		{"plus after letters", "1.0+b1", "1.0a1", 1},
		{"missing revision equals zero revision", "1.0", "1.0-0", 0},
		{"revision order", "1.0-1", "1.0-2", -1},
		{"leading zeros", "1.01", "1.1", 0},
		{"leading zeros compare as numbers", "1.002", "1.10", -1},
		{"long digit runs", "1.99999999999999999999", "1.100000000000000000000", -1},
		{"very long digit runs equal", "1.00000000000000000000001", "1.1", 0},
		{"debian security revision", "3.6-5+deb13u1", "3.6-5", 1},
		{"backport tilde revision", "2.3.3-14~deb13u3", "2.3.3-14~deb13u1", 1},
		{"dfsg upstream", "1.5.7+dfsg-1+deb13u1", "1.5.7+dfsg-1", 1},
		{"epoch with revision", "1:4.17.4-2+deb13u1", "1:4.17.4-2", 1},
		{"date style upstream", "2026.09.03-1~deb13u1", "2026.05.30-1~deb13u1", 1},
	}
	for _, tt := range tests {
		t.Run("AC1 "+tt.name, func(t *testing.T) {
			a, err := parseDebianVersion(tt.a)
			if err != nil {
				t.Fatalf("parseDebianVersion(%q): %v", tt.a, err)
			}
			b, err := parseDebianVersion(tt.b)
			if err != nil {
				t.Fatalf("parseDebianVersion(%q): %v", tt.b, err)
			}

			got, reverse := compareDebianVersions(a, b), compareDebianVersions(b, a)

			if sign(got) != tt.want {
				t.Fatalf("compareDebianVersions(%q, %q) = %d, want sign %d", tt.a, tt.b, got, tt.want)
			}
			if sign(reverse) != -tt.want {
				t.Fatalf("compareDebianVersions(%q, %q) = %d, want sign %d", tt.b, tt.a, reverse, -tt.want)
			}
		})
	}
}

// sign returns -1, 0 or 1 for the sign of n.
func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

func TestScanControlRecords(t *testing.T) {
	scan := func(in string) ([]controlRecord, error) {
		var got []controlRecord
		err := scanControlRecords(strings.NewReader(in), func(r controlRecord) error {
			got = append(got, r)
			return nil
		})
		return got, err
	}

	t.Run("AC1 empty field value accepted", func(t *testing.T) {
		in := "Package: a\nVersion: 1\nArchitecture: x\nStatus: install ok installed\nConffiles:\n /etc/a md5\n\t/etc/b md5\nDescription: d\n more\n\n" +
			"Package: b\nVersion: 2\nConffiles:\n\n"

		got, err := scan(in)

		if err != nil {
			t.Fatalf("scanControlRecords: %v", err)
		}
		want := []controlRecord{
			{index: 1, pkg: "a", version: "1", arch: "x", status: "install ok installed"},
			{index: 2, pkg: "b", version: "2"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("records = %+v, want %+v", got, want)
		}
	})

	failures := []struct{ name, in, want string }{
		{"AC3 malformed line", "Package: a\nthis is no field\n\n", "malformed line"},
		{"AC3 malformed field name", "Package: a\nBad Name: x\n\n", "malformed line"},
		{"AC3 duplicate field", "Package: a\nVersion: 1\nVersion: 2\n\n", "duplicate field"},
		{"AC3 duplicate package field", "Package: a\nPackage: b\n\n", "duplicate field"},
		{"AC3 continuation before a field", " continued\nPackage: a\n\n", "malformed line"},
		{"AC3 truncated record", "Package: a\nVersion: 1\n", "truncated record"},
		{"AC3 truncated record without line end", "Package: a\nVersion: 1", "truncated record"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			_, err := scan(tt.in)

			checkErrorContains(t, err, tt.want)
		})
	}
}

func TestReadInstalled(t *testing.T) {
	t.Run("AC1 versions keyed by name and architecture with all mapped to native", func(t *testing.T) {
		status := dpkgStatusFixture +
			installedRec("docs-all", "1.0", "all") +
			installedRec("libfoo1", "2:3.0-1", "armhf") +
			installedRec("libfoo1", "2:3.0-1", "arm64") +
			statusRec("old-rc", "deinstall ok config-files", "0.9", "arm64") +
			rec("Package", "never-installed", "Status", "purge ok not-installed", "Architecture", "arm64")

		set := installedOf(t, status)

		if set.native != "arm64" {
			t.Fatalf("native = %q, want arm64", set.native)
		}
		wantKeys := []pkgKey{{"dpkg", "arm64"}, {"docs-all", "arm64"}, {"libfoo1", "armhf"}, {"libfoo1", "arm64"}}
		if len(set.versions) != len(wantKeys) {
			t.Fatalf("installed keys = %v, want exactly %v (config-files and not-installed skipped)", set.versions, wantKeys)
		}
		for _, k := range wantKeys {
			if _, ok := set.versions[k]; !ok {
				t.Fatalf("installed keys = %v, missing %v", set.versions, k)
			}
		}
		if got := set.versions[pkgKey{"libfoo1", "armhf"}]; got.epoch != 2 || got.raw != "2:3.0-1" {
			t.Fatalf("version of libfoo1 armhf = %+v, want epoch 2, raw 2:3.0-1", got)
		}
	})

	t.Run("AC2 no installed dpkg record fails", func(t *testing.T) {
		tests := map[string]string{
			"no dpkg at all":         installedRec("other", "1.0", "arm64"),
			"dpkg only config-files": statusRec("dpkg", "deinstall ok config-files", "1.22.22", "arm64"),
			"empty status":           "",
		}
		for name, status := range tests {
			t.Run(name, func(t *testing.T) {
				_, err := readInstalled(strings.NewReader(status))

				checkErrorText(t, err, errNoDpkgStatus)
			})
		}
	})

	failures := []struct{ name, bad, want string }{
		{"AC3 malformed Status", statusRec("pkg-a", "install ok", "1.0", "arm64"), "malformed Status"},
		{"AC3 Status with a fourth word", statusRec("pkg-a", "install ok installed extra", "1.0", "arm64"), "malformed Status"},
		{"AC3 unknown status word", statusRec("pkg-a", "install ok installd", "1.0", "arm64"), "malformed Status"},
		{"AC3 unknown want word", statusRec("pkg-a", "bogus ok installed", "1.0", "arm64"), "malformed Status"},
		{"AC3 unknown flag word", statusRec("pkg-a", "install weird installed", "1.0", "arm64"), "malformed Status"},
		{"AC3 installed record without Version", statusRec("pkg-a", "install ok installed", "", "arm64"), "missing Version"},
		{"AC3 installed record without Architecture", statusRec("pkg-a", "install ok installed", "1.0", ""), "missing Architecture"},
		{"AC3 record without Status", rec("Package", "pkg-a", "Architecture", "arm64", "Version", "1.0"), "missing Status"},
		{"AC3 record without Package", rec("Status", "install ok installed", "Architecture", "arm64", "Version", "1.0"), "missing Package"},
		{"AC3 invalid Package", installedRec("Bad_Name", "1.0", "arm64"), "invalid Package"},
		{"AC3 invalid Version", installedRec("pkg-a", "1 0", "arm64"), "invalid Version"},
		{"AC3 invalid Architecture", installedRec("pkg-a", "1.0", "ARM64"), "invalid Architecture"},
		{"AC3 malformed line", "Package: pkg-a\nno field here\n\n", "malformed line"},
		{"AC3 duplicate field", "Package: pkg-a\nPackage: pkg-b\n\n", "duplicate field"},
		{"AC3 truncated record", "Package: pkg-a\nStatus: install ok installed\n", "truncated record"},
	}
	for _, tt := range failures {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readInstalled(strings.NewReader(dpkgStatusFixture + tt.bad))

			checkErrorText(t, err, "dpkg status: record 2: "+tt.want)
		})
	}

	t.Run("AC3 line over 1 MiB fails", func(t *testing.T) {
		long := rec("Package", "pkg-a", "Description", strings.Repeat("a", maxAptLineBytes+1))

		_, err := readInstalled(strings.NewReader(dpkgStatusFixture + long))

		checkErrorText(t, err, errStatusLine)
	})
}

func TestFindUpgrades(t *testing.T) {
	cases := []struct {
		name   string
		status string
		avail  string
		want   []PackageUpdate
	}{
		{
			name:   "AC1 held package",
			status: dpkgStatusFixture + statusRec("held-pkg", "hold ok installed", "1.0-1", "arm64"),
			avail:  dpkgAvailFixture + availRec("held-pkg", "1.1-1", "arm64"),
			want:   []PackageUpdate{{Name: "held-pkg", NewVersion: "1.1-1", OldVersion: "1.0-1", Arch: "arm64"}},
		},
		{
			name:   "AC1 foreign architecture installed",
			status: dpkgStatusFixture + installedRec("libfoo1", "1.0", "armhf"),
			avail:  dpkgAvailFixture + availRec("libfoo1", "9.0", "arm64") + availRec("libfoo1", "1.1", "armhf"),
			want:   []PackageUpdate{{Name: "libfoo1", NewVersion: "1.1", OldVersion: "1.0", Arch: "armhf"}},
		},
		{
			name:   "AC1 package installed for two architectures",
			status: dpkgStatusFixture + installedRec("libbar2", "1.0", "arm64") + installedRec("libbar2", "1.0", "armhf"),
			avail:  dpkgAvailFixture + availRec("libbar2", "1.2", "armhf") + availRec("libbar2", "1.3", "arm64"),
			want: []PackageUpdate{
				{Name: "libbar2", NewVersion: "1.3", OldVersion: "1.0", Arch: "arm64"},
				{Name: "libbar2", NewVersion: "1.2", OldVersion: "1.0", Arch: "armhf"},
			},
		},
		{
			name:   "AC1 all mapped to native architecture",
			status: dpkgStatusFixture + installedRec("docs-all", "1.0", "all"),
			avail:  dpkgAvailFixture + availRec("docs-all", "1.1", "all"),
			want:   []PackageUpdate{{Name: "docs-all", NewVersion: "1.1", OldVersion: "1.0", Arch: "all"}},
		},
		{
			name:   "AC1 architecture changes all to native",
			status: dpkgStatusFixture + installedRec("morph", "1.0", "all"),
			avail:  dpkgAvailFixture + availRec("morph", "2.0", "arm64"),
			want:   []PackageUpdate{{Name: "morph", NewVersion: "2.0", OldVersion: "1.0", Arch: "arm64"}},
		},
		{
			name:   "AC1 architecture changes native to all",
			status: dpkgStatusFixture + installedRec("morph", "1.0", "arm64"),
			avail:  dpkgAvailFixture + availRec("morph", "2.0", "all"),
			want:   []PackageUpdate{{Name: "morph", NewVersion: "2.0", OldVersion: "1.0", Arch: "all"}},
		},
		{
			name: "AC1 candidate older than installed not reported",
			status: dpkgStatusFixture + installedRec("older", "2.0", "arm64") + installedRec("epoched", "1:1.0", "arm64") +
				installedRec("same", "3.0-1", "arm64"),
			avail: dpkgAvailFixture + availRec("older", "1.9", "arm64") + availRec("epoched", "9.0", "arm64") +
				availRec("same", "3.0-1", "arm64"),
		},
		{
			name:   "AC1 package in no list not reported",
			status: dpkgStatusFixture + installedRec("local-only", "1.0", "arm64"),
			avail:  dpkgAvailFixture,
		},
		{
			name:   "AC1 status record of the installed candidate parsed, not reported",
			status: dpkgStatusFixture + installedRec("statpkg", "1.0", "arm64") + installedRec("after-stat", "1.0", "arm64"),
			avail: dpkgAvailFixture +
				"Package: statpkg\nVersion: 1.0\nArchitecture: arm64\nConffiles:\n /etc/stat.conf 0123456789abcdef\n /etc/stat.d/x 0123456789abcdef\nDescription: x\n y\n\n" +
				availRec("after-stat", "1.1", "arm64"),
			want: []PackageUpdate{{Name: "after-stat", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"}},
		},
		{
			name: "AC1 config-files and not-installed entries skipped",
			status: dpkgStatusFixture + statusRec("rc-pkg", "deinstall ok config-files", "1.0", "arm64") +
				rec("Package", "gone-pkg", "Status", "purge ok not-installed", "Architecture", "arm64"),
			avail: dpkgAvailFixture + availRec("rc-pkg", "2.0", "arm64") + availRec("gone-pkg", "2.0", "arm64"),
		},
		{
			name: "AC1 half-installed and unpacked count as installed",
			status: dpkgStatusFixture + statusRec("pkg-half-installed", "install ok half-installed", "1.0", "arm64") +
				statusRec("pkg-unpacked", "install ok unpacked", "1.0", "arm64") +
				statusRec("pkg-half-configured", "install ok half-configured", "1.0", "arm64") +
				statusRec("pkg-triggers-awaited", "install ok triggers-awaited", "1.0", "arm64") +
				statusRec("pkg-triggers-pending", "install ok triggers-pending", "1.0", "arm64"),
			avail: dpkgAvailFixture + availRec("pkg-half-installed", "1.1", "arm64") + availRec("pkg-unpacked", "1.1", "arm64") +
				availRec("pkg-half-configured", "1.1", "arm64") + availRec("pkg-triggers-awaited", "1.1", "arm64") +
				availRec("pkg-triggers-pending", "1.1", "arm64"),
			want: []PackageUpdate{
				{Name: "pkg-half-configured", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"},
				{Name: "pkg-half-installed", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"},
				{Name: "pkg-triggers-awaited", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"},
				{Name: "pkg-triggers-pending", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"},
				{Name: "pkg-unpacked", NewVersion: "1.1", OldVersion: "1.0", Arch: "arm64"},
			},
		},
		{
			name: "AC1 sorted by name then architecture",
			status: dpkgStatusFixture + installedRec("zeta", "1.0", "arm64") + installedRec("alpha", "1.0", "armhf") +
				installedRec("alpha", "1.0", "arm64") + installedRec("alpha-b", "1.0", "arm64"),
			avail: dpkgAvailFixture + availRec("zeta", "2.0", "arm64") + availRec("alpha", "2.0", "armhf") +
				availRec("alpha-b", "2.0", "arm64") + availRec("alpha", "2.0", "arm64"),
			want: []PackageUpdate{
				{Name: "alpha", NewVersion: "2.0", OldVersion: "1.0", Arch: "arm64"},
				{Name: "alpha", NewVersion: "2.0", OldVersion: "1.0", Arch: "armhf"},
				{Name: "alpha-b", NewVersion: "2.0", OldVersion: "1.0", Arch: "arm64"},
				{Name: "zeta", NewVersion: "2.0", OldVersion: "1.0", Arch: "arm64"},
			},
		},
		{
			name:   "AC2 no upgradable package",
			status: dpkgStatusFixture + installedRec("curl", "7.88.1-10", "arm64"),
			avail:  dpkgAvailFixture + availRec("curl", "7.88.1-10", "arm64"),
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findIn(t, tt.status, tt.avail)

			if err != nil {
				t.Fatalf("findUpgrades: %v", err)
			}
			checkUpdates(t, got, tt.want)
		})
	}

	t.Run("AC1 Trixie arm64 sample matches apt list --upgradable", func(t *testing.T) {
		status := readAptFixture(t, "trixie-arm64-status.txt")
		avail := readAptFixture(t, "trixie-arm64-dumpavail.txt")
		want := parseAptOracle(t, readAptFixture(t, "trixie-arm64-upgradable.txt"))
		if len(want) != aptSampleUpgradeCount {
			t.Fatalf("oracle holds %d updates, want %d", len(want), aptSampleUpgradeCount)
		}

		got, err := findIn(t, status, avail)

		if err != nil {
			t.Fatalf("findUpgrades: %v", err)
		}
		checkUpdates(t, got, want)
	})
}

func TestFindUpgrades_Failures(t *testing.T) {
	installedFoo := dpkgStatusFixture + installedRec("foo-pkg", "1.0", "arm64")

	cases := []struct {
		name   string
		status string
		avail  string
		want   string
	}{
		{"AC2 no dpkg record fails", installedFoo, availRec("foo-pkg", "1.1", "arm64"), errNoDpkgDumpavail},
		{"AC2 empty dumpavail fails", installedFoo, "", errNoDpkgDumpavail},
		{"AC2 dpkg record of a foreign architecture only fails", installedFoo, availRec("dpkg", "1.22.22", "armhf") + availRec("foo-pkg", "1.1", "arm64"), errNoDpkgDumpavail},
		{
			"AC3 renamed fields fail", installedFoo,
			rec("Pkg", "dpkg", "Ver", "1.22.22", "Arch", "arm64") + rec("Pkg", "foo-pkg", "Ver", "1.1", "Arch", "arm64"),
			"apt-cache dumpavail: record 1: missing Package",
		},
		{
			"AC3 invalid version among valid records", installedFoo,
			dpkgAvailFixture + availRec("foo-pkg", "1.1", "arm64") + availRec("other-pkg", "1 .0", "arm64") + availRec("last-pkg", "1.0", "arm64"),
			"apt-cache dumpavail: record 3: invalid Version",
		},
		{
			"AC3 truncated last record", installedFoo,
			dpkgAvailFixture + "Package: foo-pkg\nArchitecture: arm64\nVersion: 1.1",
			"apt-cache dumpavail: record 2: truncated record",
		},
		{
			"AC3 duplicate package", installedFoo,
			dpkgAvailFixture + availRec("foo-pkg", "1.1", "arm64") + availRec("foo-pkg", "1.2", "arm64"),
			"apt-cache dumpavail: record 3: duplicate package",
		},
		{
			"AC3 missing Package", installedFoo,
			dpkgAvailFixture + rec("Version", "1.0", "Architecture", "arm64"),
			"apt-cache dumpavail: record 2: missing Package",
		},
		{
			"AC3 missing Version", installedFoo,
			dpkgAvailFixture + rec("Package", "foo-pkg", "Architecture", "arm64"),
			"apt-cache dumpavail: record 2: missing Version",
		},
		{
			"AC3 missing Architecture", installedFoo,
			dpkgAvailFixture + rec("Package", "foo-pkg", "Version", "1.1"),
			"apt-cache dumpavail: record 2: missing Architecture",
		},
		{
			"AC3 invalid Package", installedFoo,
			dpkgAvailFixture + availRec("Bad_Name", "1.1", "arm64"),
			"apt-cache dumpavail: record 2: invalid Package",
		},
		{
			"AC3 invalid Architecture", installedFoo,
			dpkgAvailFixture + availRec("foo-pkg", "1.1", "ARM64"),
			"apt-cache dumpavail: record 2: invalid Architecture",
		},
		{
			"AC3 malformed line", installedFoo,
			dpkgAvailFixture + "Package: foo-pkg\nthis is no field\n\n",
			"apt-cache dumpavail: record 2: malformed line",
		},
		{
			"AC3 duplicate field", installedFoo,
			dpkgAvailFixture + "Package: foo-pkg\nVersion: 1.1\nVersion: 1.2\nArchitecture: arm64\n\n",
			"apt-cache dumpavail: record 2: duplicate field",
		},
		{
			"AC3 line over 1 MiB", installedFoo,
			dpkgAvailFixture + rec("Package", "foo-pkg", "Description", strings.Repeat("a", maxAptLineBytes+1)),
			errDumpavailLine,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := findIn(t, tt.status, tt.avail)

			checkErrorText(t, err, tt.want)
			if len(got) != 0 {
				t.Fatalf("updates = %+v on a failed check, want none", got)
			}
		})
	}
}

// TestFindUpgrades_RealSample compares the result for the unmodified real
// files of a host with that host's `apt list --upgradable`. It reports counts
// only, never package data. Set PIMONITOR_APT_SAMPLE_DIR to a directory with
// dpkg-status.txt, dumpavail.txt and upgradable.txt.
func TestFindUpgrades_RealSample(t *testing.T) {
	dir := os.Getenv("PIMONITOR_APT_SAMPLE_DIR")
	if dir == "" {
		t.Skip("PIMONITOR_APT_SAMPLE_DIR not set")
	}
	open := func(name string) *os.File {
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("open sample file %s: %v", name, err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	oracle, err := io.ReadAll(open("upgradable.txt"))
	if err != nil {
		t.Fatalf("read upgradable.txt: %v", err)
	}
	want := parseAptOracle(t, string(oracle))
	installed, err := readInstalled(open("dpkg-status.txt"))
	if err != nil {
		t.Fatalf("readInstalled on the sample failed: %v", err)
	}

	got, err := findUpgrades(open("dumpavail.txt"), installed)

	if err != nil {
		t.Fatalf("findUpgrades on the sample failed: %v", err)
	}
	t.Logf("sample: %d installed packages, %d updates reported, %d expected", len(installed.versions), len(got), len(want))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("AC1 sample mismatch: %d updates reported, %d expected", len(got), len(want))
	}
}

// heapProbeReader serves a head and then one record repeated up to limit
// bytes; its final read forces a GC and records the live heap.
type heapProbeReader struct {
	head, rec []byte
	limit     int
	served    int
	off       int
	headDone  bool
	done      bool
	heapAlloc uint64
}

func (r *heapProbeReader) Read(p []byte) (int, error) {
	if r.served >= r.limit {
		if !r.done {
			runtime.GC()
			var m runtime.MemStats
			runtime.ReadMemStats(&m)
			r.heapAlloc = m.HeapAlloc
			r.done = true
		}
		return 0, io.EOF
	}
	n := 0
	for n < len(p) && r.served < r.limit {
		src := r.rec
		if !r.headDone {
			src = r.head
		}
		c := copy(p[n:], src[r.off:])
		n += c
		r.off += c
		r.served += c
		if r.off == len(src) {
			r.off = 0
			r.headDone = true
		}
	}
	return n, nil
}

func TestFindUpgrades_BoundedHeap(t *testing.T) {
	const (
		inputBytes = 64 << 20
		heapBound  = 8 << 20
	)
	t.Run("AC5 live heap stays far below the input size", func(t *testing.T) {
		var desc strings.Builder
		for i := 0; i < 50; i++ {
			desc.WriteString(" " + strings.Repeat("d", 70) + "\n")
		}
		record := []byte("Package: not-installed-pkg\nVersion: 1.0\nArchitecture: arm64\nDescription: a long text\n" + desc.String() + "\n")
		head := []byte(dpkgAvailFixture)
		count := inputBytes / len(record)
		r := &heapProbeReader{head: head, rec: record, limit: len(head) + count*len(record)}
		installed := installedOf(t, dpkgStatusFixture)

		got, err := findUpgrades(r, installed)

		if err != nil {
			t.Fatalf("findUpgrades: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("updates = %+v, want none for not-installed packages", got)
		}
		if !r.done {
			t.Fatalf("findUpgrades returned before reading all of the %d input bytes (read %d)", r.limit, r.served)
		}
		if r.heapAlloc >= heapBound {
			t.Fatalf("live heap after %d input bytes = %d bytes, want below %d", r.limit, r.heapAlloc, heapBound)
		}
	})
}

// aptTestEnv is a scratch host for UpdatesCollector tests: the trimmed Trixie
// status file, a lists directory with two list files and a dumpavail fixture
// file that the stub apt-cache prints.
type aptTestEnv struct {
	dir           string
	statusPath    string
	listsDir      string
	dumpavailPath string
	olderList     string
	newerList     string
}

func newAptTestEnv(t *testing.T) *aptTestEnv {
	t.Helper()
	dir := t.TempDir()
	e := &aptTestEnv{dir: dir, listsDir: filepath.Join(dir, "lists")}
	if err := os.Mkdir(e.listsDir, 0o755); err != nil {
		t.Fatalf("mkdir lists: %v", err)
	}
	e.statusPath = writeTempFile(t, dir, "status", readAptFixture(t, "trixie-arm64-status.txt"))
	e.dumpavailPath = writeTempFile(t, dir, "dumpavail.txt", readAptFixture(t, "trixie-arm64-dumpavail.txt"))
	// A real lists directory has a partial/ subdirectory; it must exist before
	// setListTimes, which fixes the directory's mtime.
	if err := os.Mkdir(filepath.Join(e.listsDir, "partial"), 0o755); err != nil {
		t.Fatalf("create partial dir: %v", err)
	}
	e.olderList = writeTempFile(t, e.listsDir, "a_Packages", strings.Repeat("a", 32))
	e.newerList = writeTempFile(t, e.listsDir, "b_Packages", strings.Repeat("b", 32))
	e.setListTimes(t, time.Now().Add(-time.Hour), time.Now().Add(-30*time.Minute), time.Now().Add(-2*time.Hour))
	return e
}

// setListTimes sets the modification times of the two list files and of the
// lists directory.
func (e *aptTestEnv) setListTimes(t *testing.T, older, newer, dirTime time.Time) {
	t.Helper()
	for path, ts := range map[string]time.Time{e.olderList: older, e.newerList: newer, e.listsDir: dirTime} {
		if err := os.Chtimes(path, ts, ts); err != nil {
			t.Fatalf("Chtimes %s: %v", path, err)
		}
	}
}

// printFixture is a stub script body that prints the dumpavail fixture.
func (e *aptTestEnv) printFixture() string {
	return fmt.Sprintf("cat %q", e.dumpavailPath)
}

// stub writes an executable stub apt-cache running script and returns its path.
func (e *aptTestEnv) stub(t *testing.T, script string) string {
	t.Helper()
	return writeFakeVcgencmd(t, e.dir, "apt-cache", script)
}

// collector returns an UpdatesCollector reading the environment through stub.
func (e *aptTestEnv) collector(stub string) *UpdatesCollector {
	return &UpdatesCollector{
		aptCachePath:   stub,
		statusPath:     e.statusPath,
		listsDir:       e.listsDir,
		staleThreshold: 6 * time.Hour,
		now:            time.Now,
	}
}

func TestUpdatesCollector_Collect_StalenessDetection(t *testing.T) {
	e := newAptTestEnv(t)
	old := time.Now().Add(-48 * time.Hour)
	e.setListTimes(t, old, old, old)
	c := e.collector(e.stub(t, e.printFixture()))

	updates, err := c.Collect(context.Background())

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !updates.Stale {
		t.Fatalf("expected Stale=true for a 48h old cache with a 6h threshold, got %+v", updates)
	}
	if updates.CacheAgeSeconds < 47*3600 {
		t.Fatalf("CacheAgeSeconds = %v, want about 48h", updates.CacheAgeSeconds)
	}
	if updates.Count != aptSampleUpgradeCount || len(updates.Packages) != aptSampleUpgradeCount || updates.CheckedAt.IsZero() {
		t.Fatalf("Count = %d, %d packages, CheckedAt %v; want %d packages from the sample and CheckedAt set",
			updates.Count, len(updates.Packages), updates.CheckedAt, aptSampleUpgradeCount)
	}
}

func TestUpdatesCollector_Collect_PinsLocale(t *testing.T) {
	e := newAptTestEnv(t)
	envFile := filepath.Join(e.dir, "env.out")
	c := e.collector(e.stub(t, fmt.Sprintf("env > %q; %s", envFile, e.printFixture())))

	updates, err := c.Collect(context.Background())

	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if updates.Count != aptSampleUpgradeCount {
		t.Fatalf("AC1 Count = %d, want %d with the pinned environment", updates.Count, aptSampleUpgradeCount)
	}
	out, err := os.ReadFile(envFile)
	if err != nil {
		t.Fatalf("read captured child environment: %v", err)
	}
	env := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		env[k] = v
	}

	if env["LC_ALL"] != "C" {
		t.Errorf("LC_ALL = %q, want C", env["LC_ALL"])
	}
	if env["LANG"] != "C" {
		t.Errorf("LANG = %q, want C", env["LANG"])
	}
	if v, ok := env["LANGUAGE"]; ok && v != "" {
		t.Errorf("LANGUAGE = %q, want empty", v)
	}
	// Regression guard: PATH must survive, otherwise "apt-cache" (a bare
	// command name, resolved via PATH) would not be executable at all.
	if env["PATH"] == "" {
		t.Error("PATH not present in child environment; apt-cache would not be resolvable")
	}
}

func TestUpdatesCollector_Collect_Arguments(t *testing.T) {
	t.Run("AC5 dumpavail is the only argument", func(t *testing.T) {
		e := newAptTestEnv(t)
		argsFile := filepath.Join(e.dir, "args.out")
		c := e.collector(e.stub(t, fmt.Sprintf("printf '%%s\\n' \"$#\" \"$@\" > %q; %s", argsFile, e.printFixture())))

		if _, err := c.Collect(context.Background()); err != nil {
			t.Fatalf("Collect: %v", err)
		}

		got, err := os.ReadFile(argsFile)
		if err != nil {
			t.Fatalf("read recorded arguments: %v", err)
		}
		if string(got) != "1\ndumpavail\n" {
			t.Fatalf("recorded argument count and arguments = %q, want %q", got, "1\ndumpavail\n")
		}
	})
}

func TestUpdatesCollector_Collect_EmptyDumpavailFails(t *testing.T) {
	t.Run("AC2 empty dumpavail output fails instead of reporting zero", func(t *testing.T) {
		e := newAptTestEnv(t)
		c := e.collector(e.stub(t, "true"))

		updates, err := c.Collect(context.Background())

		checkErrorContains(t, err, errNoDpkgDumpavail)
		if updates.Count != 0 || updates.Packages != nil {
			t.Fatalf("Updates = %+v on a failed check, want the zero value", updates)
		}
	})
}

func TestUpdatesCollector_Collect_Failures(t *testing.T) {
	longLine := strings.Repeat("a", maxAptLineBytes+1)
	cases := []struct {
		name  string
		build func(t *testing.T, e *aptTestEnv) *UpdatesCollector
		want  string
	}{
		{"AC4 missing status file", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			c := e.collector(e.stub(t, e.printFixture()))
			c.statusPath = filepath.Join(e.dir, "no-such-status")
			return c
		}, "read dpkg status:"},
		{"AC4 missing apt-cache", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			return e.collector(filepath.Join(e.dir, "no-such-apt-cache"))
		}, errRunPrefix},
		{"AC4 apt-cache exits 1", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			return e.collector(e.stub(t, e.printFixture()+"; exit 1"))
		}, errRunPrefix + " exit status 1"},
		{"AC4 timeout", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			c := e.collector(e.stub(t, e.printFixture()+"; exec sleep 30"))
			c.timeout = 50 * time.Millisecond
			return c
		}, errRunPrefix + " context deadline exceeded"},
		{"AC4 output over the cap", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			c := e.collector(e.stub(t, e.printFixture()))
			c.maxOutput = 1024
			return c
		}, errOutputTooLarge.Error()},
		{"AC4 line over 1 MiB", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			overwriteTempFile(t, e.statusPath, dpkgStatusFixture+rec("Package", "pkg-a", "Description", longLine))
			return e.collector(e.stub(t, e.printFixture()))
		}, errStatusLine},
		{"AC4 dumpavail line over 1 MiB", func(t *testing.T, e *aptTestEnv) *UpdatesCollector {
			overwriteTempFile(t, e.dumpavailPath, dpkgAvailFixture+rec("Package", "pkg-a", "Description", longLine))
			return e.collector(e.stub(t, e.printFixture()))
		}, errDumpavailLine},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := newAptTestEnv(t)
			c := tt.build(t, e)

			updates, err := c.Collect(context.Background())

			if strings.HasPrefix(tt.want, errRunPrefix+" ") {
				checkErrorText(t, err, tt.want)
			} else {
				checkErrorContains(t, err, tt.want)
			}
			if updates.Count != 0 || updates.Packages != nil || !updates.CheckedAt.IsZero() {
				t.Fatalf("Updates = %+v on a failed check, want the zero value", updates)
			}
		})
	}
}

func TestUpdatesCollector_Collect_StateChangeFails(t *testing.T) {
	cases := []struct {
		name   string
		script func(t *testing.T, e *aptTestEnv) string
	}{
		{"AC5 status file rewritten", func(t *testing.T, e *aptTestEnv) string {
			return fmt.Sprintf("echo '# changed' >> %q; %s", e.statusPath, e.printFixture())
		}},
		{"AC5 list file replaced by one with an older mtime", func(t *testing.T, e *aptTestEnv) string {
			// Same size as the list it replaces, and older than both lists, so
			// only the directory's modification time can show the change.
			replacement := writeTempFile(t, e.dir, "replacement", strings.Repeat("r", 32))
			old := time.Now().Add(-3 * time.Hour)
			if err := os.Chtimes(replacement, old, old); err != nil {
				t.Fatalf("Chtimes: %v", err)
			}
			return fmt.Sprintf("mv %q %q; %s", replacement, e.olderList, e.printFixture())
		}},
		{"AC5 list file added", func(t *testing.T, e *aptTestEnv) string {
			return fmt.Sprintf("echo x > %q; %s", filepath.Join(e.listsDir, "c_Packages"), e.printFixture())
		}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			e := newAptTestEnv(t)
			c := e.collector(e.stub(t, tt.script(t, e)))

			updates, err := c.Collect(context.Background())

			checkErrorContains(t, err, errStateChanged)
			if updates.Count != 0 || updates.Packages != nil {
				t.Fatalf("Updates = %+v on a failed check, want the zero value", updates)
			}
		})
	}
}
