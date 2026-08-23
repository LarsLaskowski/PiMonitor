// Package web embeds the PiMonitor dashboard's static frontend assets
// (plain HTML/CSS/JS, no build step) into the compiled binary.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// Handler returns an http.Handler serving the embedded dashboard at "/".
//
// Embedded files carry a zero ModTime, so http.FileServerFS would otherwise
// emit no Last-Modified/ETag and no Cache-Control, forcing a full refetch of
// every asset on every page load. Handler sets Cache-Control: no-cache plus a
// per-asset ETag derived from that asset's contents, so browsers revalidate
// cheaply (a 304 while the file is unchanged) and pick up an edited asset
// immediately — including across builds that share a version string, which is
// every unversioned "dev" build from `make run`.
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServerFS(sub)

	// Hashed once at construction: the asset set is fixed at compile time and
	// tiny, so this costs microseconds and no per-request work.
	etags, err := assetETags(sub)
	if err != nil {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		// Set before delegating, because http.ServeContent answers a
		// conditional request from the header already on the writer. Naming an
		// asset is not the same as serving one, though, so etagWriter takes the
		// validator back off any response that is not that asset's bytes.
		if etag, ok := etags[etagKey(r.URL.Path)]; ok {
			w.Header().Set("Etag", etag)
			w = &etagWriter{ResponseWriter: w}
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

// etagWriter strips a pre-set Etag once the file server has settled on a status
// code that is not the asset's own representation.
//
// Two shapes reach here having matched a real asset yet never serving it:
// http.FileServerFS redirects "*/index.html" to "./" and a trailing slash on a
// file to its base, and both branch before serveContent runs. A 301 is
// cacheable by default, so leaving the header set would attach a strong
// validator to an empty body describing a different representation — the same
// defect as emitting one on a 404.
type etagWriter struct {
	http.ResponseWriter
	wroteHeader bool
}

func (w *etagWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	switch code {
	// 200 and 206 carry the asset's bytes; a 304 must repeat the validator
	// that matched (RFC 9110 15.4.5). Everything else — redirects, 404s, a
	// range the file could not satisfy — gets none.
	case http.StatusOK, http.StatusPartialContent, http.StatusNotModified:
	default:
		w.Header().Del("Etag")
	}
	w.ResponseWriter.WriteHeader(code)
}

// etagKey maps a request path to the asset key used in the etags map,
// resolving the directory index the same way http.FileServerFS does. A hit
// only means the path names an asset — whether that asset is what gets served
// is decided by the file server and enforced by etagWriter.
func etagKey(p string) string {
	p = strings.TrimPrefix(path.Clean("/"+p), "/")
	if p == "" {
		p = "index.html"
	}
	return p
}

// assetETags walks the embedded asset tree and returns a strong ETag per file,
// derived from a SHA-256 of its contents.
func assetETags(fsys fs.FS) (map[string]string, error) {
	etags := make(map[string]string)
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		etags[p] = `"` + hex.EncodeToString(sum[:16]) + `"`
		return nil
	})
	if err != nil {
		return nil, err
	}
	return etags, nil
}
