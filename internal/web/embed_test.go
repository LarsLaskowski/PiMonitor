package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func newHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := Handler()
	if err != nil {
		t.Fatalf("Handler: %v", err)
	}
	return h
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandler_ServesIndex(t *testing.T) {
	rec := get(t, newHandler(t), "/")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "PiMonitor") {
		t.Fatalf("expected index.html to contain %q", "PiMonitor")
	}
}

func TestHandler_ServesStaticAssets(t *testing.T) {
	h := newHandler(t)

	for _, path := range []string{"/style.css", "/app.js", "/chart.js", "/gauge.js", "/theme-init.js"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status = %d, want 200", path, rec.Code)
		}
	}
}

func TestHandler_ServesThemeToggle(t *testing.T) {
	h := newHandler(t)

	body := get(t, h, "/").Body.String()
	if !strings.Contains(body, `id="theme-toggle"`) {
		t.Errorf("expected index.html to contain the theme toggle button")
	}
	// The pre-paint theme script is an external file (inline scripts are
	// blocked by the Content-Security-Policy) and must load in <head>.
	if !strings.Contains(body, `<script src="theme-init.js"></script>`) {
		t.Errorf("expected index.html to load theme-init.js")
	}

	// The pre-paint script must key persistence off the same localStorage key
	// app.js uses, so a stored choice survives a reload without a flash.
	if !strings.Contains(get(t, h, "/theme-init.js").Body.String(), "pimonitor-theme") {
		t.Errorf("expected theme-init.js to reference the pimonitor-theme storage key")
	}
}

func TestHandler_UnknownPath404s(t *testing.T) {
	rec := get(t, newHandler(t), "/does-not-exist.js")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestHandler_SetsCacheHeaders(t *testing.T) {
	rec := get(t, newHandler(t), "/")

	if got, want := rec.Header().Get("Cache-Control"), "no-cache"; got != want {
		t.Errorf("Cache-Control = %q, want %q", got, want)
	}
	// The value is a content hash, so assert its shape rather than a literal:
	// a non-empty, double-quoted strong validator.
	etag := rec.Header().Get("Etag")
	if len(etag) < 3 || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
		t.Errorf("Etag = %q, want a non-empty double-quoted validator", etag)
	}
}

// Each asset is a distinct resource and must therefore carry its own
// validator. A single shared ETag (previously the build version) also meant an
// edited asset kept revalidating as unchanged across "dev" builds.
func TestHandler_ETagDiffersPerAsset(t *testing.T) {
	h := newHandler(t)

	seen := make(map[string]string)
	for _, path := range []string{"/", "/style.css", "/app.js", "/chart.js", "/gauge.js", "/theme-init.js"} {
		etag := get(t, h, path).Header().Get("Etag")
		if etag == "" {
			t.Errorf("GET %s: no Etag header", path)
			continue
		}
		if other, ok := seen[etag]; ok {
			t.Errorf("GET %s and GET %s share the Etag %q", other, path, etag)
			continue
		}
		seen[etag] = path
	}
}

// The hash is deterministic, so a restarted process must not invalidate a
// client's cached copy of an unchanged asset.
func TestHandler_ETagStableAcrossHandlers(t *testing.T) {
	first, second := newHandler(t), newHandler(t)

	for _, path := range []string{"/", "/app.js", "/style.css"} {
		a := get(t, first, path).Header().Get("Etag")
		b := get(t, second, path).Header().Get("Etag")
		if a != b {
			t.Errorf("GET %s: Etag = %q on one handler, %q on another", path, a, b)
		}
	}
}

func TestHandler_RevalidatesOnMatchingETag(t *testing.T) {
	h := newHandler(t)

	etag := get(t, h, "/app.js").Header().Get("Etag")
	if etag == "" {
		t.Fatal("GET /app.js: no Etag header")
	}

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want %d for a matching If-None-Match", rec.Code, http.StatusNotModified)
	}
}

func TestHandler_RefetchesOnStaleETag(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	// The old version-derived validator, which every "dev" build emitted.
	req.Header.Set("If-None-Match", `"dev"`)
	rec := httptest.NewRecorder()
	newHandler(t).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d when the client's cached ETag is stale", rec.Code, http.StatusOK)
	}
	if rec.Body.Len() == 0 {
		t.Error("expected a body when the client's cached ETag is stale")
	}
}

// A 404 body has no stable identity, so it must not carry a validator.
func TestHandler_UnknownPathHasNoETag(t *testing.T) {
	rec := get(t, newHandler(t), "/does-not-exist.js")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if etag := rec.Header().Get("Etag"); etag != "" {
		t.Errorf("Etag = %q on a 404, want no Etag header", etag)
	}
}

// http.FileServerFS redirects "*/index.html" to "./" and a trailing slash on a
// file to its base. Both paths name a real asset but never serve it, and a 301
// is cacheable by default — so the empty redirect body must not inherit that
// asset's validator.
func TestHandler_RedirectsHaveNoETag(t *testing.T) {
	h := newHandler(t)

	for _, path := range []string{"/index.html", "/app.js/", "/style.css/"} {
		rec := get(t, h, path)
		if rec.Code != http.StatusMovedPermanently {
			t.Errorf("GET %s: status = %d, want %d", path, rec.Code, http.StatusMovedPermanently)
			continue
		}
		if etag := rec.Header().Get("Etag"); etag != "" {
			t.Errorf("GET %s: Etag = %q on a %d redirect, want no Etag header", path, etag, rec.Code)
		}
	}
}

// A stale validator on a redirected path must not turn into a 304: the client
// would keep a cached body for a URL the server is redirecting away from.
func TestHandler_RedirectIgnoresConditionalRequest(t *testing.T) {
	h := newHandler(t)

	etag := get(t, h, "/").Header().Get("Etag") // index.html's validator
	if etag == "" {
		t.Fatal("GET /: no Etag header")
	}

	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	req.Header.Set("If-None-Match", etag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMovedPermanently {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMovedPermanently)
	}
	if got := rec.Header().Get("Etag"); got != "" {
		t.Errorf("Etag = %q on a redirect, want no Etag header", got)
	}
}

func TestETagKey_ResolvesRequestPaths(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"root serves the directory index", "/", "index.html"},
		{"plain file", "/app.js", "app.js"},
		{"index by name", "/index.html", "index.html"},
		{"trailing slash on a file", "/app.js/", "app.js"},
		{"dot segment", "/./app.js", "app.js"},
		{"parent segments cannot escape the root", "/../../etc/passwd", "etc/passwd"},
		{"nested path keeps its directory", "/sub/app.js", "sub/app.js"},
		{"unknown file maps to its own key", "/does-not-exist.js", "does-not-exist.js"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := etagKey(tt.path); got != tt.want {
				t.Errorf("etagKey(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestAssetETags_HashesFileContents(t *testing.T) {
	etags, err := assetETags(fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<h1>one</h1>")},
		"app.js":     &fstest.MapFile{Data: []byte("console.log(1);")},
		"copy.js":    &fstest.MapFile{Data: []byte("console.log(1);")},
		"sub/x.css":  &fstest.MapFile{Data: []byte("body{}")},
	})
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}

	want := []string{"index.html", "app.js", "copy.js", "sub/x.css"}
	if len(etags) != len(want) {
		t.Errorf("got %d entries (%v), want %d", len(etags), etags, len(want))
	}
	for _, key := range want {
		etag, ok := etags[key]
		if !ok {
			t.Errorf("no ETag for %q", key)
			continue
		}
		if len(etag) < 3 || !strings.HasPrefix(etag, `"`) || !strings.HasSuffix(etag, `"`) {
			t.Errorf("ETag for %q = %q, want a non-empty double-quoted validator", key, etag)
		}
	}

	// Derived from contents, not from the name: differing bytes must differ,
	// identical bytes must match.
	if etags["app.js"] == etags["index.html"] {
		t.Errorf("files with different contents share the ETag %q", etags["app.js"])
	}
	if etags["app.js"] != etags["copy.js"] {
		t.Errorf("byte-identical files got different ETags: %q vs %q", etags["app.js"], etags["copy.js"])
	}
}

// Editing an asset must change its validator — the whole point of hashing
// contents rather than the build version.
func TestAssetETags_ChangesWithContent(t *testing.T) {
	before, err := assetETags(fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("console.log(1);")}})
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}
	after, err := assetETags(fstest.MapFS{"app.js": &fstest.MapFile{Data: []byte("console.log(1);\nconsole.log('changed');")}})
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}

	if before["app.js"] == after["app.js"] {
		t.Errorf("an edited asset kept the ETag %q", before["app.js"])
	}
}

func TestAssetETags_SkipsDirectories(t *testing.T) {
	etags, err := assetETags(fstest.MapFS{"sub/x.css": &fstest.MapFile{Data: []byte("body{}")}})
	if err != nil {
		t.Fatalf("assetETags: %v", err)
	}

	for _, key := range []string{".", "sub"} {
		if etag, ok := etags[key]; ok {
			t.Errorf("directory %q got an ETag %q", key, etag)
		}
	}
}
