// Package adminui embeds and serves the GoForge admin dashboard at /_/.
// The dist directory contains the built SvelteKit app (see ui/admin).
package adminui

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
)

//go:embed all:dist
var distFS embed.FS

// Module serves the embedded admin UI ("adminui").
type Module struct{}

func (Module) ID() string { return "adminui" }

func (Module) Register(app *core.App) error {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return err
	}
	fileServer := http.FileServer(http.FS(sub))

	app.Mux().HandleFunc("/_/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/_")
		path = strings.TrimPrefix(path, "/")
		if path == "" {
			path = "index.html"
		}
		// SPA fallback: unknown paths render the shell.
		if _, err := fs.Stat(sub, path); err != nil {
			path = "index.html"
		}
		w.Header().Set("X-Frame-Options", "SAMEORIGIN")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(path, "_app/") || strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".css") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		r2 := r.Clone(r.Context())
		r2.URL.Path = "/" + path
		fileServer.ServeHTTP(w, r2)
	})
	app.Mux().HandleFunc("/_", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/_/", http.StatusMovedPermanently)
	})
	return nil
}
