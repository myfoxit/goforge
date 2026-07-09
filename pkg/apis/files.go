package apis

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/files"
	"github.com/myfoxit/goforge/pkg/rules"
	"github.com/myfoxit/goforge/pkg/token"
)

// registerFileRoutes serves stored files with rule-based access control.
func registerFileRoutes(app *core.App, svc *Records) {
	mux := app.Mux()

	// Short-lived token for accessing protected files from <img> tags.
	mux.HandleFunc("POST /api/files/token", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		auth := core.AuthFromContext(r.Context())
		tok, err := token.Sign(app.Secret(), token.TypeFile, token.Claims{
			"sub": auth.ID(),
			"col": auth.Collection.Name,
		}, 3*time.Minute)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, map[string]string{"token": tok})
	}))

	mux.HandleFunc("GET /api/files/{collection}/{recordId}/{filename}", func(w http.ResponseWriter, r *http.Request) {
		serveFile(app, svc, w, r)
	})
}

func serveFile(app *core.App, svc *Records, w http.ResponseWriter, r *http.Request) {
	colName := r.PathValue("collection")
	recordID := r.PathValue("recordId")
	filename := r.PathValue("filename")

	c := app.Schema().Get(colName)
	if c == nil {
		core.WriteError(w, app.Log(), core.NotFound(""))
		return
	}

	// Resolve the acting identity: header auth or ?token= file token.
	auth := core.AuthFromContext(r.Context())
	if auth == nil {
		if raw := r.URL.Query().Get("token"); raw != "" {
			if claims, err := token.Verify(app.Secret(), raw, token.TypeFile); err == nil {
				if record, err := app.FindRecordByID(r.Context(), claims.String("col"), claims.Subject()); err == nil && record != nil {
					ac := app.Schema().Get(claims.String("col"))
					auth = &core.Auth{
						Record: record, Collection: ac,
						Superuser: claims.String("col") == core.SuperusersCollection,
						Method:    "filetoken",
					}
					auth.Roles = app.ResolveRoles(r.Context(), ac, record)
				}
			}
		}
	}

	// The file is visible when its record is visible (view rule).
	if !auth.IsSuperuser() {
		rule := c.ViewRule
		if rule == nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		req := &Request{Auth: auth, Query: r.URL.Query(), Data: map[string]any{}, HTTP: r}
		where, args, err := rules.CompileRule(*rule, svc.ruleContext(c, req))
		if err != nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		q := app.DB().Dialect.Quote
		row, err := app.DB().QueryMap(r.Context(), fmt.Sprintf(
			"SELECT 1 AS ok FROM %s WHERE %s.%s = ? AND (%s) LIMIT 1",
			svc.tableExpr(c), q(c.Name), q("id"), where),
			append([]any{recordID}, args...)...)
		if err != nil || row == nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
	}

	// Confirm the filename actually belongs to the record (no key probing).
	record, err := app.FindRecordByID(r.Context(), colName, recordID)
	if err != nil || record == nil {
		core.WriteError(w, app.Log(), core.NotFound(""))
		return
	}
	owned := false
	for _, f := range c.Fields {
		if f.Type != "file" {
			continue
		}
		for _, name := range db.ToJSONList(record[f.Name]) {
			if name == filename {
				owned = true
				break
			}
		}
	}
	if !owned {
		core.WriteError(w, app.Log(), core.NotFound(""))
		return
	}

	st, err := StorageFromApp(app)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}

	var rc io.ReadCloser
	var info *files.FileInfo
	if thumb := r.URL.Query().Get("thumb"); thumb != "" && files.IsImage(filename) {
		rc, info, err = files.Thumb(r.Context(), st, colName, recordID, filename, thumb)
	} else {
		rc, info, err = st.Get(r.Context(), files.Key(colName, recordID, filename))
	}
	if err != nil {
		core.WriteError(w, app.Log(), core.NotFound(""))
		return
	}
	defer rc.Close()

	w.Header().Set("Content-Type", info.ContentType)
	if info.Size > 0 {
		w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	}
	if info.ETag != "" {
		w.Header().Set("ETag", `"`+info.ETag+`"`)
		if match := r.Header.Get("If-None-Match"); match == `"`+info.ETag+`"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	io.Copy(w, rc)
}
