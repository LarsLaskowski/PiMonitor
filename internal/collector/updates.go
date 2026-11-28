package collector

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
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

// updatesRetryDelay is the pause before the one repeat of a check whose apt or
// dpkg state changed while it ran.
const updatesRetryDelay = 2 * time.Second

// errAptStateChanged reports that the apt lists or the dpkg status file
// changed while a check ran.
var errAptStateChanged = errors.New("apt or dpkg state changed during the check")

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
	// retryAfter returns a channel that fires after the given pause; nil
	// means time.After. Tests set it.
	retryAfter func(time.Duration) <-chan time.Time
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
// Any failure (unreadable or unrecognised input, a failing child process, a
// state change while it ran) fails the check; no count is fabricated.
func (c *UpdatesCollector) Collect(ctx context.Context) (Updates, error) {
	timeout := c.timeout
	if timeout <= 0 {
		timeout = updatesTimeout
	}
	maxOutput := c.maxOutput
	if maxOutput <= 0 {
		maxOutput = maxAptOutputBytes
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	before := aptStateStamp(c.listsDir, c.statusPath)
	installed, err := c.loadInstalled()
	if err != nil {
		return Updates{}, err
	}
	packages, err := c.runDumpavail(ctx, cancel, installed, maxOutput)
	if err != nil {
		return Updates{}, err
	}
	if aptStateStamp(c.listsDir, c.statusPath) != before {
		return Updates{}, errAptStateChanged
	}

	now := c.now()
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

// loadInstalled reads the installed packages from the dpkg status file.
func (c *UpdatesCollector) loadInstalled() (installedSet, error) {
	f, err := os.Open(c.statusPath)
	if err != nil {
		return installedSet{}, fmt.Errorf("read dpkg status: %w", err)
	}
	defer func() { _ = f.Close() }()
	return readInstalled(f)
}

// runDumpavail streams `apt-cache dumpavail` into findUpgrades. It always
// waits for the child; a failing child or an expired context fails the check
// even when the output parsed.
func (c *UpdatesCollector) runDumpavail(ctx context.Context, cancel context.CancelFunc, installed installedSet, maxOutput int64) ([]PackageUpdate, error) {
	cmd := exec.CommandContext(ctx, c.aptCachePath, "dumpavail")
	// apt-cache localises messages via gettext; pin the locale rather than
	// inheriting the operator's. LC_ALL beats LANG and LANGUAGE, but all
	// three are set so no inherited value can leak through. PATH must be
	// preserved because aptCachePath is typically the bare string
	// "apt-cache", resolved via PATH.
	cmd.Env = []string{
		"LC_ALL=C",
		"LANG=C",
		"LANGUAGE=",
		"DEBIAN_FRONTEND=noninteractive",
		"PATH=" + os.Getenv("PATH"),
	}
	cmd.WaitDelay = time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("run apt-cache dumpavail: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("run apt-cache dumpavail: %w", err)
	}

	packages, parseErr := findUpgrades(newCappedReader(stdout, maxOutput), installed)
	// An expired context before our own cancel means the child was killed
	// mid-output; the run error is the cause, not what the cut-off looked like.
	ctxErr := ctx.Err()
	if parseErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	switch {
	case ctxErr != nil:
		return nil, fmt.Errorf("run apt-cache dumpavail: %w", ctxErr)
	case parseErr != nil:
		return nil, parseErr
	case waitErr != nil:
		return nil, fmt.Errorf("run apt-cache dumpavail: %w", waitErr)
	}
	return packages, nil
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
	out := debianVersion{raw: v}
	rest := v
	if i := strings.IndexByte(rest, ':'); i >= 0 {
		epoch, err := strconv.ParseUint(rest[:i], 10, 32)
		if err != nil {
			return debianVersion{}, fmt.Errorf("invalid epoch in version %q", v)
		}
		out.epoch = uint32(epoch)
		rest = rest[i+1:]
	}
	if i := strings.LastIndexByte(rest, '-'); i >= 0 {
		out.revision = rest[i+1:]
		rest = rest[:i]
		if out.revision == "" || !allBytes(out.revision, isRevisionByte) {
			return debianVersion{}, fmt.Errorf("invalid revision in version %q", v)
		}
	}
	out.upstream = rest
	if rest == "" || rest[0] < '0' || rest[0] > '9' || !allBytes(rest, isUpstreamByte) {
		return debianVersion{}, fmt.Errorf("invalid upstream version in %q", v)
	}
	return out, nil
}

func isAlnum(b byte) bool {
	return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isRevisionByte(b byte) bool { return isAlnum(b) || b == '.' || b == '+' || b == '~' }

func isUpstreamByte(b byte) bool { return isRevisionByte(b) || b == '-' }

// allBytes reports whether every byte of s satisfies ok.
func allBytes(s string, ok func(byte) bool) bool {
	for i := 0; i < len(s); i++ {
		if !ok(s[i]) {
			return false
		}
	}
	return true
}

// compareDebianVersions orders a and b: negative, zero or positive as a is
// older than, equal to or newer than b.
func compareDebianVersions(a, b debianVersion) int {
	switch {
	case a.epoch < b.epoch:
		return -1
	case a.epoch > b.epoch:
		return 1
	}
	if c := compareVersionPart(a.upstream, b.upstream); c != 0 {
		return c
	}
	return compareVersionPart(a.revision, b.revision)
}

// compareVersionPart compares an upstream or revision string by dpkg's rules:
// alternating runs of non-digits and digits.
func compareVersionPart(a, b string) int {
	for a != "" || b != "" {
		na, nb := nonDigitLen(a), nonDigitLen(b)
		if c := compareNonDigits(a[:na], b[:nb]); c != 0 {
			return c
		}
		a, b = a[na:], b[nb:]
		da, db := digitLen(a), digitLen(b)
		if c := compareDigits(a[:da], b[:db]); c != 0 {
			return c
		}
		a, b = a[da:], b[db:]
	}
	return 0
}

func nonDigitLen(s string) int {
	n := 0
	for n < len(s) && (s[n] < '0' || s[n] > '9') {
		n++
	}
	return n
}

func digitLen(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// versionOrder is dpkg's character weight: '~' sorts before the end of the
// string, which sorts before letters, which sort before other characters.
func versionOrder(s string, i int) int {
	if i >= len(s) {
		return 0
	}
	c := s[i]
	switch {
	case c == '~':
		return -1
	case isAlnum(c):
		return int(c)
	}
	return int(c) + 256
}

// compareNonDigits compares two runs without digits.
func compareNonDigits(a, b string) int {
	n := max(len(a), len(b))
	for i := 0; i < n; i++ {
		if d := versionOrder(a, i) - versionOrder(b, i); d != 0 {
			return d
		}
	}
	return 0
}

// compareDigits compares two digit runs as numbers of any length.
func compareDigits(a, b string) int {
	a, b = strings.TrimLeft(a, "0"), strings.TrimLeft(b, "0")
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	return strings.Compare(a, b)
}

// controlRecord is one record of a control-format stream.
type controlRecord struct {
	index   int
	pkg     string
	version string
	arch    string
	status  string
}

// recordError is a failure inside record index (counted from 1).
type recordError struct {
	index  int
	reason string
}

func (e *recordError) Error() string {
	return "record " + strconv.Itoa(e.index) + ": " + e.reason
}

// controlParser holds the state of one scanControlRecords run.
type controlParser struct {
	rec      controlRecord
	seen     [4]bool
	inRecord bool
	hasField bool
	count    int
}

// fail builds the error of the record the current line belongs to.
func (p *controlParser) fail(reason string) error {
	idx := p.count
	if !p.inRecord {
		idx++
	}
	return &recordError{index: idx, reason: reason}
}

// line handles one non-blank line.
func (p *controlParser) line(line []byte) error {
	if line[0] == ' ' || line[0] == '\t' {
		if !p.hasField {
			return p.fail("malformed line")
		}
		return nil
	}
	i := bytes.IndexByte(line, ':')
	if i <= 0 || bytes.ContainsAny(line[:i], " \t") {
		return p.fail("malformed line")
	}
	if !p.inRecord {
		p.inRecord = true
		p.count++
		p.rec = controlRecord{index: p.count}
		p.seen = [4]bool{}
	}
	p.hasField = true
	return p.store(string(line[:i]), line[i+1:])
}

// store keeps the value of the four fields the readers use.
func (p *controlParser) store(name string, value []byte) error {
	var slot int
	var dst *string
	switch name {
	case "Package":
		slot, dst = 0, &p.rec.pkg
	case "Version":
		slot, dst = 1, &p.rec.version
	case "Architecture":
		slot, dst = 2, &p.rec.arch
	case "Status":
		slot, dst = 3, &p.rec.status
	default:
		return nil
	}
	if p.seen[slot] {
		return p.fail("duplicate field")
	}
	p.seen[slot] = true
	*dst = string(bytes.TrimSpace(value))
	return nil
}

// scanControlRecords reads control-format records from r and calls visit for each.
func scanControlRecords(r io.Reader, visit func(rec controlRecord) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), maxAptLineBytes+1)
	var p controlParser
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			if p.inRecord {
				p.inRecord, p.hasField = false, false
				if err := visit(p.rec); err != nil {
					return err
				}
			}
			continue
		}
		if err := p.line(line); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}
	if p.inRecord {
		return p.fail("truncated record")
	}
	return nil
}

// describeScanError prefixes err with the name of the source it came from.
func describeScanError(source string, err error) error {
	switch {
	case errors.Is(err, bufio.ErrTooLong):
		return fmt.Errorf("%s: line exceeds 1 MiB", source)
	case errors.Is(err, errOutputTooLarge):
		return err
	}
	return fmt.Errorf("%s: %w", source, err)
}

// validName reports whether s is a Debian package name.
func validName(s string) bool {
	if len(s) < 2 || !isLowerAlnum(s[0]) {
		return false
	}
	return allBytes(s[1:], func(b byte) bool { return isLowerAlnum(b) || b == '.' || b == '+' || b == '-' })
}

// validArch reports whether s is a Debian architecture name.
func validArch(s string) bool {
	if s == "" || !isLowerAlnum(s[0]) {
		return false
	}
	return allBytes(s[1:], func(b byte) bool { return isLowerAlnum(b) || b == '-' })
}

func isLowerAlnum(b byte) bool { return b >= '0' && b <= '9' || b >= 'a' && b <= 'z' }

// checkPackage validates the Package, Version and Architecture of rec and
// parses its version.
func checkPackage(rec controlRecord) (debianVersion, error) {
	switch {
	case rec.pkg == "":
		return debianVersion{}, &recordError{rec.index, "missing Package"}
	case rec.version == "":
		return debianVersion{}, &recordError{rec.index, "missing Version"}
	case rec.arch == "":
		return debianVersion{}, &recordError{rec.index, "missing Architecture"}
	case !validName(rec.pkg):
		return debianVersion{}, &recordError{rec.index, "invalid Package"}
	case !validArch(rec.arch):
		return debianVersion{}, &recordError{rec.index, "invalid Architecture"}
	}
	v, err := parseDebianVersion(rec.version)
	if err != nil {
		return debianVersion{}, &recordError{rec.index, "invalid Version"}
	}
	return v, nil
}

var (
	statusWants = []string{"unknown", "install", "hold", "deinstall", "purge"}
	statusFlags = []string{"ok", "reinstreq", "hold", "hold-reinstreq"}
	statusState = []string{"not-installed", "config-files", "half-installed", "unpacked", "half-configured", "triggers-awaited", "triggers-pending", "installed"}
)

// parseStatus splits a Status value and reports whether the package counts as
// installed (any state but not-installed and config-files).
func parseStatus(s string) (installed, ok bool) {
	f := strings.Fields(s)
	if len(f) != 3 || !containsString(statusWants, f[0]) || !containsString(statusFlags, f[1]) || !containsString(statusState, f[2]) {
		return false, false
	}
	return f[2] != "not-installed" && f[2] != "config-files", true
}

func containsString(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// installedVersion validates a dpkg status record. It reports whether the
// package counts as installed and, if so, its version.
func installedVersion(rec controlRecord) (debianVersion, bool, error) {
	if rec.pkg == "" {
		return debianVersion{}, false, &recordError{rec.index, "missing Package"}
	}
	if !validName(rec.pkg) {
		return debianVersion{}, false, &recordError{rec.index, "invalid Package"}
	}
	if rec.status == "" {
		return debianVersion{}, false, &recordError{rec.index, "missing Status"}
	}
	installed, ok := parseStatus(rec.status)
	if !ok {
		return debianVersion{}, false, &recordError{rec.index, "malformed Status"}
	}
	if !installed {
		return debianVersion{}, false, nil
	}
	v, err := checkPackage(rec)
	return v, err == nil, err
}

// readInstalled collects the installed packages of a dpkg status stream.
func readInstalled(r io.Reader) (installedSet, error) {
	set := installedSet{versions: map[pkgKey]debianVersion{}}
	err := scanControlRecords(r, func(rec controlRecord) error {
		v, installed, err := installedVersion(rec)
		if err != nil || !installed {
			return err
		}
		if rec.pkg == "dpkg" && set.native == "" && rec.arch != "all" {
			set.native = rec.arch
		}
		set.versions[pkgKey{rec.pkg, rec.arch}] = v
		return nil
	})
	if err != nil {
		return installedSet{}, describeScanError("dpkg status", err)
	}
	if set.native == "" {
		return installedSet{}, errors.New("dpkg status has no installed dpkg record")
	}
	mapAllToNative(&set)
	return set, nil
}

// mapAllToNative re-keys the architecture-independent packages to the native
// architecture, as apt does.
func mapAllToNative(set *installedSet) {
	for k, v := range set.versions {
		if k.arch == "all" {
			delete(set.versions, k)
			set.versions[pkgKey{k.name, set.native}] = v
		}
	}
}

// upgradeFinder accumulates the upgradable packages of a dumpavail stream.
type upgradeFinder struct {
	installed  installedSet
	seen       map[pkgKey]struct{}
	nativeDpkg bool
	result     []PackageUpdate
}

// visit handles one dumpavail record.
func (f *upgradeFinder) visit(rec controlRecord) error {
	cand, err := checkPackage(rec)
	if err != nil {
		return err
	}
	if rec.pkg == "dpkg" && rec.arch == f.installed.native {
		f.nativeDpkg = true
	}
	key := pkgKey{rec.pkg, rec.arch}
	if key.arch == "all" {
		key.arch = f.installed.native
	}
	have, ok := f.installed.versions[key]
	if !ok {
		return nil
	}
	if _, dup := f.seen[key]; dup {
		return &recordError{rec.index, "duplicate package"}
	}
	f.seen[key] = struct{}{}
	if compareDebianVersions(cand, have) > 0 {
		f.result = append(f.result, PackageUpdate{Name: rec.pkg, NewVersion: rec.version, OldVersion: have.raw, Arch: rec.arch})
	}
	return nil
}

// findUpgrades reads an apt-cache dumpavail stream and returns the packages
// whose candidate is newer than the installed version, sorted by name and
// architecture. It fails unless a dpkg record of the native architecture was
// seen.
func findUpgrades(r io.Reader, installed installedSet) ([]PackageUpdate, error) {
	f := &upgradeFinder{installed: installed, seen: map[pkgKey]struct{}{}}
	if err := scanControlRecords(r, f.visit); err != nil {
		return nil, describeScanError("apt-cache dumpavail", err)
	}
	if !f.nativeDpkg {
		return nil, errors.New("apt-cache dumpavail has no dpkg record for the native architecture")
	}
	sort.Slice(f.result, func(i, j int) bool {
		if f.result[i].Name != f.result[j].Name {
			return f.result[i].Name < f.result[j].Name
		}
		return f.result[i].Arch < f.result[j].Arch
	})
	return f.result, nil
}

// cappedReader fails once more than limit bytes have been read.
type cappedReader struct {
	r         io.Reader
	remaining int64
}

func (c *cappedReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.remaining -= int64(n)
	if c.remaining < 0 && err == nil {
		err = errOutputTooLarge
	}
	return n, err
}

// newCappedReader returns a reader that fails with errOutputTooLarge once
// more than limit bytes have been read from r.
func newCappedReader(r io.Reader, limit int64) io.Reader {
	return &cappedReader{r: r, remaining: limit}
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
	var s stateStamp
	if info, err := os.Stat(listsDir); err == nil {
		s.listsDirModTime = info.ModTime().UnixNano()
	}
	if entries, err := os.ReadDir(listsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if info, err := e.Info(); err == nil {
				s.listsCount++
				s.listsSize += info.Size()
			}
		}
	}
	if newest, err := newestListModTime(listsDir); err == nil {
		s.listsNewest = newest.UnixNano()
	}
	if info, err := os.Stat(statusPath); err == nil {
		s.statusModTime = info.ModTime().UnixNano()
		s.statusSize = info.Size()
	}
	return s
}
