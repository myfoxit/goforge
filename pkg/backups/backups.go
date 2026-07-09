// Package backups creates and restores application snapshots: a consistent
// SQLite copy (VACUUM INTO) plus the uploaded files, packed as tar.gz under
// DataDir/backups. For PostgreSQL/MySQL deployments use the native dump
// tools — this module then archives only the file storage.
package backups

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/myfoxit/goforge/pkg/cmd"
	"github.com/myfoxit/goforge/pkg/core"
)

// Module wires backups ("backups").
type Module struct{}

func (Module) ID() string { return "backups" }

func (Module) Register(app *core.App) error {
	cmd.Register(cmd.Command{
		Name:  "backup",
		Usage: "Create a backup archive in DataDir/backups",
		Run: func(app *core.App, args []string) error {
			name, err := Create(app)
			if err != nil {
				return err
			}
			fmt.Println("backup created:", name)
			return nil
		},
	})

	mux := app.Mux()
	mux.HandleFunc("GET /api/backups", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		items, err := List(app)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, map[string]any{"items": items})
	}))
	mux.HandleFunc("POST /api/backups", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		name, err := Create(app)
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		core.WriteJSON(w, 200, map[string]any{"name": name})
	}))
	mux.HandleFunc("GET /api/backups/{name}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		path, err := safePath(app, name)
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		f, err := os.Open(path)
		if err != nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		io.Copy(w, f)
	}))
	mux.HandleFunc("DELETE /api/backups/{name}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		path, err := safePath(app, r.PathValue("name"))
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		if err := os.Remove(path); err != nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	return nil
}

func backupsDir(app *core.App) string {
	return filepath.Join(app.Config().DataDir, "backups")
}

func safePath(app *core.App, name string) (string, error) {
	if strings.ContainsAny(name, "/\\") || !strings.HasSuffix(name, ".tar.gz") {
		return "", fmt.Errorf("invalid backup name")
	}
	return filepath.Join(backupsDir(app), name), nil
}

// List returns available backups (newest first).
func List(app *core.App) ([]map[string]any, error) {
	dir := backupsDir(app)
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return []map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".tar.gz") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		items = append(items, map[string]any{
			"name":    e.Name(),
			"size":    info.Size(),
			"created": info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i]["name"].(string) > items[j]["name"].(string)
	})
	return items, nil
}

// Create writes a new backup archive and returns its filename.
func Create(app *core.App) (string, error) {
	dir := backupsDir(app)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	stamp := time.Now().UTC().Format("20060102_150405")
	name := fmt.Sprintf("backup_%s.tar.gz", stamp)
	target := filepath.Join(dir, name)

	out, err := os.Create(target)
	if err != nil {
		return "", err
	}
	defer out.Close()
	gz := gzip.NewWriter(out)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()

	// 1. Database (SQLite only: consistent snapshot via VACUUM INTO).
	if app.DB().Dialect.Name() == "sqlite" {
		tmpDB := filepath.Join(dir, ".snapshot_"+stamp+".db")
		defer os.Remove(tmpDB)
		if _, err := app.DB().DB.Exec("VACUUM INTO ?", tmpDB); err != nil {
			return "", fmt.Errorf("sqlite snapshot: %w", err)
		}
		if err := addFile(tw, tmpDB, "data.db"); err != nil {
			return "", err
		}
	}

	// 2. Uploaded files (local storage).
	storageDir := filepath.Join(app.Config().DataDir, "storage")
	if _, err := os.Stat(storageDir); err == nil {
		err = filepath.Walk(storageDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}
			rel, err := filepath.Rel(storageDir, path)
			if err != nil {
				return err
			}
			return addFile(tw, path, filepath.ToSlash(filepath.Join("storage", rel)))
		})
		if err != nil {
			return "", err
		}
	}

	app.Log().Info("backup created", "name", name)
	return name, nil
}

func addFile(tw *tar.Writer, path, name string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	header := &tar.Header{
		Name: name, Mode: 0o644, Size: info.Size(), ModTime: info.ModTime(),
	}
	if err := tw.WriteHeader(header); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}
