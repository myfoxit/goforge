// Package adminui embeds and serves the GoForge admin dashboard at /_/.
// The dist directory contains the admin SPA (a dependency-free single-page
// app that talks to the same REST API).
package adminui

import (
	"bytes"
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"

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
	start := time.Now()

	serve := func(w http.ResponseWriter, r *http.Request, name string) {
		if name == "" || strings.HasSuffix(name, "/") {
			name += "index.html"
		}
		name = strings.TrimPrefix(path.Clean("/"+name), "/")

		data, err := fs.ReadFile(sub, name)
		if err != nil {
			// SPA fallback: unknown non-asset routes render the shell.
			data, err = fs.ReadFile(sub, "index.html")
			if err != nil {
				http.NotFound(w, r)
				return
			}
			name = "index.html"
		}

		ctype := mime.TypeByExtension(path.Ext(name))
		if ctype == "" {
			ctype = "application/octet-stream"
		}
		w.Header().Set("Content-Type", ctype)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if name == "index.html" {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=3600")
		}
		http.ServeContent(w, r, name, start, bytes.NewReader(data))
	}

	app.Mux().HandleFunc("/_/", func(w http.ResponseWriter, r *http.Request) {
		serve(w, r, strings.TrimPrefix(r.URL.Path, "/_/"))
	})
	app.Mux().HandleFunc("/_", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/_/", http.StatusMovedPermanently)
	})
	return nil
}
