package packaging

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	downloadLatestPath = "../../packaging/download-latest.sh"
	testVersion        = "v1.3.0"
	releaseAPIURL      = "https://api.github.com/repos/larslaskowski/pimonitor/releases/latest"
	releaseBaseURL     = "https://github.com/larslaskowski/pimonitor/releases/download/" + testVersion + "/"
	checksumsURL       = releaseBaseURL + "checksums.txt"

	// releaseJSON is the part of the GitHub release API answer the script reads.
	releaseJSON = "{\n  \"url\": \"https://api.github.com/repos/larslaskowski/pimonitor/releases/1\",\n" +
		"  \"tag_name\": \"v1.3.0\",\n  \"name\": \"v1.3.0\"\n}\n"

	// realChecksumsV130 is checksums.txt of release v1.3.0 exactly as GoReleaser published it.
	realChecksumsV130 = "bf6b049895294f4301c8d8188b7db2bc5bd88e37edfc4f6a6bf0e46c4d06d29a  pimonitor_1.3.0_linux_arm64.tar.gz\n" +
		"9ec3e35fd5fda91cef6d8fbc1e22c783a3cfe4f1b9496d6fe7564df16e004e73  pimonitor_1.3.0_linux_armv6.tar.gz\n"
	realArm64HashV130 = "bf6b049895294f4301c8d8188b7db2bc5bd88e37edfc4f6a6bf0e46c4d06d29a"

	scriptTimeout = 30 * time.Second
)

// scriptTools are the commands download-latest.sh and the stub curl run, besides curl itself.
var scriptTools = []string{"bash", "awk", "tar", "gzip", "sha256sum", "rm", "ls", "cat", "uname"}

// scriptEnv is a scratch environment for one run of download-latest.sh: a tool directory that is the whole PATH
// (links to the real tools plus the stub curl), a work directory the script runs in, and the files the stub serves.
type scriptEnv struct {
	bash     string
	script   string
	toolDir  string
	workDir  string
	home     string
	stubRoot string
	stubLog  string
}

type scriptResult struct {
	exitCode int
	stdout   string
	stderr   string
}

func archiveName(arch string) string {
	return "pimonitor_" + strings.TrimPrefix(testVersion, "v") + "_linux_" + arch + ".tar.gz"
}

func archiveURL(arch string) string {
	return releaseBaseURL + archiveName(arch)
}

// lookTool finds a real tool, preferring the system directories: a wrapper earlier on the test's PATH that runs
// "the next command on PATH" would find nothing once the PATH is the tool directory alone.
func lookTool(t *testing.T, name string) string {
	t.Helper()
	for _, dir := range []string{"/usr/bin", "/bin"} {
		path := filepath.Join(dir, name)
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return path
		}
	}
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("required tool %s not found on this Linux host: %v", name, err)
	}
	return path
}

// newScriptEnv builds a scriptEnv whose tool directory links every tool of scriptTools except those in without,
// and holds the stub curl of writeStubCurl. It skips the test on a platform other than Linux.
func newScriptEnv(t *testing.T, without ...string) *scriptEnv {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skipf("download-latest.sh targets Linux; skipped on %s", runtime.GOOS)
	}
	script, err := filepath.Abs(downloadLatestPath)
	if err != nil {
		t.Fatalf("resolve %s: %v", downloadLatestPath, err)
	}
	root := t.TempDir()
	e := &scriptEnv{
		bash:     lookTool(t, "bash"),
		script:   script,
		toolDir:  filepath.Join(root, "bin"),
		workDir:  filepath.Join(root, "work"),
		home:     filepath.Join(root, "home"),
		stubRoot: filepath.Join(root, "served"),
		stubLog:  filepath.Join(root, "curl.log"),
	}
	for _, dir := range []string{e.toolDir, e.workDir, e.home, e.stubRoot} {
		mustMkdir(t, dir)
	}
	for _, name := range scriptTools {
		if containsString(without, name) {
			continue
		}
		if err := os.Symlink(lookTool(t, name), filepath.Join(e.toolDir, name)); err != nil {
			t.Fatalf("link tool %s: %v", name, err)
		}
	}
	writeStubCurl(t, e)
	return e
}

// writeStubCurl writes a curl stand-in to the tool directory. It ignores the flags, takes the last argument as the
// URL and appends it to $STUB_LOG, refuses a non-https URL (exit 1), and serves $STUB_ROOT/<URL without https://>
// to the -o file or standard output; a missing file exits 22 silently, like curl -fsS without a message. When
// <file>.partial exists, its bytes (the first part of the download) go to the output and the stub exits 18, curl's
// code for a partial file.
func writeStubCurl(t *testing.T, e *scriptEnv) {
	t.Helper()
	stub := "#!" + e.bash + `
out=/dev/stdout
url=''
while (($#)); do
  case "$1" in
    -o) out=$2; shift 2 ;;
    *) url=$1; shift ;;
  esac
done
printf '%s\n' "$url" >>"$STUB_LOG"
[[ $url == https://* ]] || exit 1
file="$STUB_ROOT/${url#https://}"
if [[ -f "$file.partial" ]]; then
  cat -- "$file.partial" >"$out"
  exit 18
fi
[[ -f "$file" ]] || exit 22
cat -- "$file" >"$out"
`
	if err := os.WriteFile(filepath.Join(e.toolDir, "curl"), []byte(stub), 0o755); err != nil {
		t.Fatalf("write stub curl: %v", err)
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
}

func containsString(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}

// serve makes the stub curl answer url with content.
func (e *scriptEnv) serve(t *testing.T, url string, content []byte) {
	t.Helper()
	path := filepath.Join(e.stubRoot, strings.TrimPrefix(url, "https://"))
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write served file %s: %v", path, err)
	}
}

// serveRelease serves the release API answer and checksums.txt with the given content.
func (e *scriptEnv) serveRelease(t *testing.T, checksums string) {
	t.Helper()
	e.serve(t, releaseAPIURL, []byte(releaseJSON))
	e.serve(t, checksumsURL, []byte(checksums))
}

// writeReleaseArchive builds the release archive for arch, holding <dir>/pimonitor, serves it and returns its
// bytes and SHA-256 in lower-case hex.
func writeReleaseArchive(t *testing.T, e *scriptEnv, arch string) ([]byte, string) {
	t.Helper()
	dir := strings.TrimSuffix(archiveName(arch), ".tar.gz")
	binary := []byte("#!/bin/sh\necho pimonitor " + arch + "\n")
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	headers := []*tar.Header{
		{Name: dir + "/", Typeflag: tar.TypeDir, Mode: 0o755},
		{Name: dir + "/pimonitor", Typeflag: tar.TypeReg, Mode: 0o755, Size: int64(len(binary))},
	}
	for _, h := range headers {
		if err := tw.WriteHeader(h); err != nil {
			t.Fatalf("write tar header %s: %v", h.Name, err)
		}
	}
	if _, err := tw.Write(binary); err != nil {
		t.Fatalf("write tar body: %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	data := buf.Bytes()
	sum := sha256.Sum256(data)
	e.serve(t, archiveURL(arch), data)
	return data, hex.EncodeToString(sum[:])
}

// run runs download-latest.sh with ARCH=arch in the work directory and the tool directory as the whole PATH.
func (e *scriptEnv) run(t *testing.T, arch string) scriptResult {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), scriptTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, e.bash, e.script)
	cmd.Dir = e.workDir
	cmd.Env = []string{
		"PATH=" + e.toolDir,
		"HOME=" + e.home,
		"ARCH=" + arch,
		"STUB_ROOT=" + e.stubRoot,
		"STUB_LOG=" + e.stubLog,
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	res := scriptResult{stdout: stdout.String(), stderr: stderr.String()}
	var exitErr *exec.ExitError
	switch {
	case errors.As(err, &exitErr):
		res.exitCode = exitErr.ExitCode()
	case err != nil:
		t.Fatalf("run %s: %v", e.script, err)
	}
	if ctx.Err() != nil {
		t.Fatalf("%s did not finish within %v", e.script, scriptTimeout)
	}
	return res
}

// requests returns the URLs the stub curl was called with, in order.
func (e *scriptEnv) requests(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(e.stubLog)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatalf("read stub log: %v", err)
	}
	return strings.Fields(string(data))
}

func assertRequests(t *testing.T, e *scriptEnv, want ...string) {
	t.Helper()
	got := e.requests(t)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("curl requests = %q, want %q", got, want)
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("%s exists (Lstat error %v), want it removed or never created", path, err)
	}
}

// assertFailedCleanly checks that a run ended with status 1, wrote exactly wantStderr, and left neither the archive
// nor the extracted directory in the work directory.
func assertFailedCleanly(t *testing.T, e *scriptEnv, res scriptResult, arch, wantStderr string) {
	t.Helper()
	if res.exitCode != 1 {
		t.Errorf("exit status = %d, want 1 (stdout %q)", res.exitCode, res.stdout)
	}
	if res.stderr != wantStderr {
		t.Errorf("stderr = %q, want %q", res.stderr, wantStderr)
	}
	assertMissing(t, filepath.Join(e.workDir, archiveName(arch)))
	assertMissing(t, filepath.Join(e.workDir, strings.TrimSuffix(archiveName(arch), ".tar.gz")))
}

func TestDownloadLatest_ChecksumsUnavailable_AC2(t *testing.T) {
	e := newScriptEnv(t)
	e.serve(t, releaseAPIURL, []byte(releaseJSON))
	writeReleaseArchive(t, e, "arm64")

	res := e.run(t, "arm64")

	assertFailedCleanly(t, e, res, "arm64", "Could not download checksums.txt of PiMonitor v1.3.0.\n")
	assertRequests(t, e, releaseAPIURL, checksumsURL)
}

func TestDownloadLatest_NoUsableEntry_AC2(t *testing.T) {
	const arch = "arm64"
	name := archiveName(arch)
	wantStderr := "No checksum for " + name + " in checksums.txt of PiMonitor v1.3.0.\n"
	tests := []struct {
		name      string
		checksums func(hash string) string
		// mustHave is a substring the case's input must still contain, so a lost escape in this source fails.
		mustHave string
	}{
		{name: "only the armv6 line", checksums: func(hash string) string { return hash + "  " + archiveName("armv6") + "\n" }},
		{name: "arm64 line of another version", checksums: func(hash string) string { return hash + "  pimonitor_1.2.0_linux_arm64.tar.gz\n" }},
		{
			name:      "CRLF line ending",
			checksums: func(hash string) string { return hash + "  " + name + "\r\n" },
			mustHave:  "\x0d\x0a",
		},
		{name: "binary form", checksums: func(hash string) string { return hash + " *" + name + "\n" }},
		{name: "entry twice with the same hash", checksums: func(hash string) string { return strings.Repeat(hash+"  "+name+"\n", 2) }},
		{name: "63-character hash", checksums: func(hash string) string { return hash[:63] + "  " + name + "\n" }},
		{name: "upper-case hash", checksums: func(hash string) string { return strings.ToUpper(hash) + "  " + name + "\n" }},
		{name: "BSD form", checksums: func(hash string) string { return "SHA256 (" + name + ") = " + hash + "\n" }},
		{name: "empty file", checksums: func(string) string { return "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newScriptEnv(t)
			_, hash := writeReleaseArchive(t, e, arch)
			checksums := tt.checksums(hash)
			if !strings.Contains(checksums, tt.mustHave) {
				t.Fatalf("case input %q lacks %q; an escape in the test source was lost", checksums, tt.mustHave)
			}
			e.serveRelease(t, checksums)

			res := e.run(t, arch)

			assertFailedCleanly(t, e, res, arch, wantStderr)
			assertRequests(t, e, releaseAPIURL, checksumsURL)
		})
	}
}

func TestDownloadLatest_ChecksumMismatch_AC2(t *testing.T) {
	const arch = "arm64"
	e := newScriptEnv(t)
	_, actual := writeReleaseArchive(t, e, arch)
	e.serveRelease(t, realChecksumsV130)
	if actual == realArm64HashV130 {
		t.Fatalf("generated archive hash %s equals the real v1.3.0 hash; the mismatch case needs a different one", actual)
	}
	name := archiveName(arch)
	wantStderr := "Checksum mismatch for " + name + ": expected " + realArm64HashV130 + ", got " + actual + ".\n" +
		"Removed " + name + "; nothing was extracted.\n"

	res := e.run(t, arch)

	assertFailedCleanly(t, e, res, arch, wantStderr)
	assertRequests(t, e, releaseAPIURL, checksumsURL, archiveURL(arch))
}

func TestDownloadLatest_ArchiveDownloadFails_AC2(t *testing.T) {
	const arch = "arm64"
	e := newScriptEnv(t)
	data, hash := writeReleaseArchive(t, e, arch)
	e.serveRelease(t, hash+"  "+archiveName(arch)+"\n")
	e.serve(t, archiveURL(arch)+".partial", data[:len(data)/2])

	res := e.run(t, arch)

	assertFailedCleanly(t, e, res, arch, "Could not download "+archiveName(arch)+" of PiMonitor v1.3.0.\n")
	assertRequests(t, e, releaseAPIURL, checksumsURL, archiveURL(arch))
}

func TestDownloadLatest_MissingSha256sum_AC2(t *testing.T) {
	const arch = "arm64"
	e := newScriptEnv(t, "sha256sum")
	_, hash := writeReleaseArchive(t, e, arch)
	e.serveRelease(t, hash+"  "+archiveName(arch)+"\n")

	res := e.run(t, arch)

	assertFailedCleanly(t, e, res, arch, "Required command not found: sha256sum\n")
	assertRequests(t, e)
}

func TestDownloadLatest_VerifiedArchive_AC3(t *testing.T) {
	for _, arch := range []string{"arm64", "armv6"} {
		t.Run(arch, func(t *testing.T) {
			e := newScriptEnv(t)
			_, armHash := writeReleaseArchive(t, e, "arm64")
			_, armv6Hash := writeReleaseArchive(t, e, "armv6")
			e.serveRelease(t, armHash+"  "+archiveName("arm64")+"\n"+armv6Hash+"  "+archiveName("armv6")+"\n")
			name := archiveName(arch)
			dir := filepath.Join(e.workDir, strings.TrimSuffix(name, ".tar.gz"))

			res := e.run(t, arch)

			if res.exitCode != 0 || res.stderr != "" {
				t.Fatalf("exit status = %d, stderr = %q; want 0 and no error output", res.exitCode, res.stderr)
			}
			assertVerifiedOutput(t, res.stdout, arch)
			assertRegularFile(t, filepath.Join(dir, "pimonitor"))
			assertRegularFile(t, filepath.Join(e.workDir, name))
			assertRequests(t, e, releaseAPIURL, checksumsURL, archiveURL(arch))
		})
	}
}

func TestDownloadLatest_Syntax_AC4(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skipf("download-latest.sh targets Linux; skipped on %s", runtime.GOOS)
	}
	script, err := filepath.Abs(downloadLatestPath)
	if err != nil {
		t.Fatalf("resolve %s: %v", downloadLatestPath, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), scriptTimeout)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, lookTool(t, "bash"), "-n", script)
	cmd.Stderr = &stderr

	err = cmd.Run()

	if err != nil || stderr.Len() != 0 {
		t.Errorf("bash -n %s: error %v, stderr %q; want no error and no output", script, err, stderr.String())
	}
}

// assertVerifiedOutput checks that stdout starts with the download, verification and extraction lines, in order.
func assertVerifiedOutput(t *testing.T, stdout, arch string) {
	t.Helper()
	name := archiveName(arch)
	want := []string{
		"Downloading PiMonitor v1.3.0 for " + arch + " ...",
		"Checksum of " + name + " verified.",
		"Extracting " + name + " ...",
	}
	got := strings.Split(stdout, "\n")
	if len(got) < len(want) || strings.Join(got[:len(want)], "\n") != strings.Join(want, "\n") {
		t.Errorf("stdout = %q, want it to start with the lines %q", stdout, want)
	}
}

func assertRegularFile(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		t.Errorf("%s: stat error %v, want a regular file", path, err)
	}
}
