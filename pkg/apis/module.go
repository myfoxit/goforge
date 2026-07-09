package apis

import (
	"encoding/json"
	"mime/multipart"
	"net/http"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
)

// Module mounts the REST API ("apis").
type Module struct{}

func (Module) ID() string { return "apis" }

func (Module) Register(app *core.App) error {
	registerStorageSettings(app)
	svc := NewRecords(app)
	mux := app.Mux()

	// Health.
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, 200, map[string]any{
			"status":  "ok",
			"name":    app.AppName(),
			"version": core.Version,
		})
	})

	// Records CRUD.
	mux.HandleFunc("GET /api/collections/{collection}/records", func(w http.ResponseWriter, r *http.Request) {
		listRecords(app, svc, w, r)
	})
	mux.HandleFunc("GET /api/collections/{collection}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		req := requestFrom(r, nil)
		record, err := svc.View(r.Context(), r.PathValue("collection"), r.PathValue("id"), req, r.URL.Query().Get("expand"))
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, record)
	})
	mux.HandleFunc("POST /api/collections/{collection}/records", func(w http.ResponseWriter, r *http.Request) {
		req, err := parseBody(r)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		record, err := svc.Create(r.Context(), r.PathValue("collection"), req)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, record)
	})
	mux.HandleFunc("PATCH /api/collections/{collection}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		req, err := parseBody(r)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		record, err := svc.Update(r.Context(), r.PathValue("collection"), r.PathValue("id"), req)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, record)
	})
	mux.HandleFunc("DELETE /api/collections/{collection}/records/{id}", func(w http.ResponseWriter, r *http.Request) {
		req := requestFrom(r, nil)
		if err := svc.Delete(r.Context(), r.PathValue("collection"), r.PathValue("id"), req); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	registerCollectionRoutes(app)
	registerSettingsRoutes(app)
	registerFileRoutes(app, svc)
	registerRealtime(app, svc)
	return nil
}

func listRecords(app *core.App, svc *Records, w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	req := requestFrom(r, nil)
	result, err := svc.List(r.Context(), r.PathValue("collection"), req, ListOptions{
		Page:      atoiDefault(q.Get("page"), 1),
		PerPage:   atoiDefault(q.Get("perPage"), 30),
		Sort:      q.Get("sort"),
		Filter:    q.Get("filter"),
		Expand:    q.Get("expand"),
		SkipTotal: q.Get("skipTotal") == "1" || q.Get("skipTotal") == "true",
	})
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	core.WriteJSON(w, 200, result)
}

// requestFrom builds the service request context from an HTTP request.
func requestFrom(r *http.Request, data map[string]any) *Request {
	if data == nil {
		data = map[string]any{}
	}
	return &Request{
		Auth:  core.AuthFromContext(r.Context()),
		Query: r.URL.Query(),
		Data:  data,
		HTTP:  r,
	}
}

// parseBody reads a JSON or multipart request into a service Request.
func parseBody(r *http.Request) (*Request, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return nil, core.BadRequest("Invalid multipart form: " + err.Error())
		}
		data := map[string]any{}
		for key, vals := range r.MultipartForm.Value {
			if len(vals) == 1 {
				// JSON-encoded values (arrays/objects) are transparently decoded.
				var decoded any
				if json.Unmarshal([]byte(vals[0]), &decoded) == nil {
					switch decoded.(type) {
					case []any, map[string]any, bool, float64:
						data[key] = decoded
						continue
					}
				}
				data[key] = vals[0]
			} else {
				data[key] = vals
			}
		}
		req := requestFrom(r, data)
		req.Files = map[string][]*multipart.FileHeader{}
		for key, fhs := range r.MultipartForm.File {
			req.Files[key] = fhs
		}
		return req, nil
	}

	data := map[string]any{}
	if err := core.ReadJSON(r, &data); err != nil {
		return nil, err
	}
	return requestFrom(r, data), nil
}
