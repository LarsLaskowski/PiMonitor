// Package web embeds the PiMonitor dashboard's static frontend assets
// (plain HTML/CSS/JS, no build step) into the compiled binary.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed assets
var assetsFS embed.FS

// Handler returns an http.Handler serving the embedded dashboard at "/".
func Handler() (http.Handler, error) {
	sub, err := fs.Sub(assetsFS, "assets")
	if err != nil {
		return nil, err
	}
	return http.FileServerFS(sub), nil
}
