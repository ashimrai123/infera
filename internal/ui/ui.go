// Package ui embeds the static chat interface and serves it at GET /.
//
// The web/ directory is baked into the binary at compile time using Go's
// embed package -- no files need to be present at runtime, and the final
// image size increases only by the size of the HTML.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

// files holds the contents of the web/ directory at compile time.
// The path is relative to this Go file, so web/ must live at internal/ui/web/.
//
//go:embed web
var files embed.FS

// Handler returns an http.Handler that serves the embedded web/ directory.
// Registering it on "/" in the router makes it a catch-all: any path not
// matched by a more specific route (e.g. /v1/chat/completions, /metrics)
// falls through to this handler.
func Handler() http.Handler {
	// Strip the "web" prefix so the root of the file system is web/.
	// A request for "/" maps to web/index.html.
	sub, err := fs.Sub(files, "web")
	if err != nil {
		// This can only happen if "web" doesn't exist in the embedded FS,
		// which would be a build-time mistake caught immediately.
		panic("ui: embedded web directory not found: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
