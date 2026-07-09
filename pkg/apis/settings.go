package apis

import (
	"net/http"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/mail"
)

func registerSettingsRoutes(app *core.App) {
	mux := app.Mux()

	mux.HandleFunc("GET /api/settings", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, 200, map[string]any{"sections": app.Settings().Export()})
	}))

	mux.HandleFunc("PATCH /api/settings", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := core.ReadJSON(r, &body); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		if len(body) == 0 {
			core.WriteError(w, app.Log(), core.BadRequest("Nothing to update."))
			return
		}
		if err := app.Settings().SetMany(r.Context(), body); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, map[string]any{"sections": app.Settings().Export()})
	}))

	mux.HandleFunc("POST /api/settings/test-email", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			To string `json:"to"`
		}
		if err := core.ReadJSON(r, &body); err != nil || body.To == "" {
			core.WriteError(w, app.Log(), core.BadRequest("Missing recipient."))
			return
		}
		err := mail.SendTemplate(r.Context(), app, mail.Address{Email: body.To}, "test", mail.TemplateData{})
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest("Send failed: "+err.Error()))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	// Instance info for the admin dashboard.
	mux.HandleFunc("GET /api/settings/info", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		counts := map[string]any{}
		q := app.DB().Dialect.Quote
		for _, c := range app.Schema().All() {
			if c.IsView() {
				continue
			}
			row, err := app.DB().QueryMap(r.Context(), "SELECT COUNT(*) AS n FROM "+q(c.Name))
			if err == nil && row != nil {
				counts[c.Name] = db.ToFloat(row["n"])
			}
		}
		core.WriteJSON(w, 200, map[string]any{
			"version":  core.Version,
			"dbDriver": app.DB().Dialect.Name(),
			"modules":  app.Modules(),
			"counts":   counts,
		})
	}))
}
