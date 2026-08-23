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
		// Only for paths that name a real asset: a 404 body has no stable
		// identity and must not carry a validator.
		if etag, ok := etags[etagKey(r.URL.Path)]; ok {
			w.Header().Set("Etag", etag)
		}
		fileServer.ServeHTTP(w, r)
	}), nil
}

// etagKey maps a request path to the asset key used in the etags map,
// resolving the directory index the same way http.FileServerFS does.
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
