package apis

import (
	"fmt"
	"net/http"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/schema"
)

// registerCollectionRoutes mounts the schema administration API
// (superuser only; also the surface behind the MCP schema tools).
func registerCollectionRoutes(app *core.App) {
	mux := app.Mux()

	mux.HandleFunc("GET /api/collections", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, 200, map[string]any{"items": app.Schema().All()})
	}))

	mux.HandleFunc("GET /api/collections/{collection}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		c := app.Schema().Get(r.PathValue("collection"))
		if c == nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		core.WriteJSON(w, 200, c)
	}))

	mux.HandleFunc("POST /api/collections", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		if app.Settings().Bool("app.hideControls") {
			core.WriteError(w, app.Log(), core.Forbidden("Schema changes are disabled (app.hideControls)."))
			return
		}
		var c schema.Collection
		if err := core.ReadJSON(r, &c); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		if existing := app.Schema().Get(c.Name); existing != nil {
			core.WriteError(w, app.Log(), core.ValidationError("name", "A collection with this name already exists."))
			return
		}
		c.ID = "" // ids are assigned server-side
		c.System = false
		if c.IsAuth() {
			ensureAuthFields(&c)
		}
		if err := app.Schema().Save(r.Context(), &c); err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		core.WriteJSON(w, 200, app.Schema().Get(c.Name))
	}))

	mux.HandleFunc("PATCH /api/collections/{collection}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		if app.Settings().Bool("app.hideControls") {
			core.WriteError(w, app.Log(), core.Forbidden("Schema changes are disabled (app.hideControls)."))
			return
		}
		existing := app.Schema().Get(r.PathValue("collection"))
		if existing == nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		upd := existing.Clone()
		if err := core.ReadJSON(r, upd); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		upd.ID = existing.ID
		if upd.IsAuth() {
			ensureAuthFields(upd)
		}
		if err := app.Schema().Save(r.Context(), upd); err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		core.WriteJSON(w, 200, app.Schema().Get(upd.Name))
	}))

	mux.HandleFunc("DELETE /api/collections/{collection}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		if app.Settings().Bool("app.hideControls") {
			core.WriteError(w, app.Log(), core.Forbidden("Schema changes are disabled (app.hideControls)."))
			return
		}
		if err := app.Schema().Delete(r.Context(), r.PathValue("collection")); err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	mux.HandleFunc("DELETE /api/collections/{collection}/truncate", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		c := app.Schema().Get(r.PathValue("collection"))
		if c == nil {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		if c.System || c.IsView() {
			core.WriteError(w, app.Log(), core.BadRequest("This collection cannot be truncated."))
			return
		}
		q := app.DB().Dialect.Quote
		if _, err := app.DB().Exec(r.Context(), fmt.Sprintf("DELETE FROM %s", q(c.Name))); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		if st, err := StorageFromApp(app); err == nil {
			st.DeletePrefix(r.Context(), c.Name+"/")
			st.DeletePrefix(r.Context(), ".thumbs/"+c.Name+"/")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	// Field type metadata for admin UI form builders.
	mux.HandleFunc("GET /api/collections-meta/field-types", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, 200, map[string]any{"types": schema.FieldTypes()})
	}))
}

// ensureAuthFields guarantees the system auth fields on user-defined auth
// collections.
func ensureAuthFields(c *schema.Collection) {
	for _, sys := range schema.BaseAuthFields() {
		if c.FieldByID(sys.ID) == nil && c.Field(sys.Name) == nil {
			c.Fields = append(c.Fields, sys)
		}
	}
}
