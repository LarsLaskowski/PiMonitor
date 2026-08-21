// Package web embeds the PiMonitor dashboard's static frontend assets
// (plain HTML/CSS/JS, no build step) into the compiled binary.
package web

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"
)

//go:embed assets
var assetsFS embed.FS

// Handler returns an http.Handler serving the embedded dashboard at "/".
//
// Embedded files carry a zero ModTime, so http.FileServerFS would otherwise
// emit no Last-Modified/ETag and no Cache-Control, forcing a full refetch of
// every asset on every page load. Handler sets a version-derived ETag and a
// Cache-Control: no-cache directive instead, so browsers revalidate cheaply
// (a 304 on an unchanged version) and still pick up new assets immediately
// after an upgrade changes version.
func Handler(version string) (http.Handler, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, err
	}
	fileServer := http.FileServerFS(sub)
	etag := fmt.Sprintf(`"%s"`, version)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Etag", etag)
		fileServer.ServeHTTP(w, r)
	}), nil
}
