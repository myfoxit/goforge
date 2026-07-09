package mcp

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// APIKeysCollection stores hashed API keys.
const APIKeysCollection = "_apikeys"

// KeyPrefix marks GoForge API keys.
const KeyPrefix = "forge_"

func ensureAPIKeysCollection(app *core.App) error {
	if app.Schema().Get(APIKeysCollection) != nil {
		return nil
	}
	return app.Schema().Save(context.Background(), &schema.Collection{
		Name: APIKeysCollection, Type: schema.TypeBase, System: true,
		Fields: []*schema.Field{
			{Name: "name", Type: schema.FieldText, Required: true, System: true},
			{Name: "keyHash", Type: schema.FieldText, Required: true, Hidden: true, System: true},
			{Name: "keyTail", Type: schema.FieldText, System: true}, // last 4 chars for display
			{Name: "scopes", Type: schema.FieldJSON, System: true},  // ["*", "posts:read", ...]
			{Name: "admin", Type: schema.FieldBool, System: true},   // schema/settings tools + rule bypass
			{Name: "identityCollection", Type: schema.FieldText, System: true},
			{Name: "identityRecord", Type: schema.FieldText, System: true},
			{Name: "lastUsed", Type: schema.FieldDate, System: true},
		},
		Indexes: []schema.Index{{Name: "ux_apikeys_hash", Columns: []string{"keyHash"}, Unique: true}},
	})
}

// registerAPIKeyRoutes mounts superuser key management.
func registerAPIKeyRoutes(app *core.App) {
	mux := app.Mux()

	mux.HandleFunc("GET /api/apikeys", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		rows, err := app.DB().QueryMaps(r.Context(),
			"SELECT * FROM "+app.DB().Dialect.Quote(APIKeysCollection)+" ORDER BY created DESC")
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		items := make([]map[string]any, 0, len(rows))
		for _, row := range rows {
			items = append(items, map[string]any{
				"id":       db.ToString(row["id"]),
				"name":     db.ToString(row["name"]),
				"keyTail":  db.ToString(row["keyTail"]),
				"scopes":   db.ToJSONList(row["scopes"]),
				"admin":    db.ToBool(row["admin"]),
				"identity": db.ToString(row["identityRecord"]),
				"lastUsed": db.ToString(row["lastUsed"]),
				"created":  db.ToString(row["created"]),
			})
		}
		core.WriteJSON(w, 200, map[string]any{"items": items})
	}))

	mux.HandleFunc("POST /api/apikeys", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Name               string   `json:"name"`
			Scopes             []string `json:"scopes"`
			Admin              bool     `json:"admin"`
			IdentityCollection string   `json:"identityCollection"`
			IdentityRecord     string   `json:"identityRecord"`
		}
		if err := core.ReadJSON(r, &body); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		if body.Name == "" {
			core.WriteError(w, app.Log(), core.ValidationError("name", "Name is required."))
			return
		}
		if len(body.Scopes) == 0 {
			body.Scopes = []string{"*"}
		}
		if body.IdentityRecord != "" {
			c := app.Schema().Get(body.IdentityCollection)
			if c == nil || !c.IsAuth() {
				core.WriteError(w, app.Log(), core.ValidationError("identityCollection", "Identity must reference an auth collection."))
				return
			}
			record, err := app.FindRecordByID(r.Context(), c.Name, body.IdentityRecord)
			if err != nil || record == nil {
				core.WriteError(w, app.Log(), core.ValidationError("identityRecord", "Identity record not found."))
				return
			}
		}

		plain := KeyPrefix + security.RandomToken(32)
		scopesField := schema.Field{Name: "scopes", Type: schema.FieldJSON}
		scopesVal, _ := scopesField.NormalizeValue(body.Scopes)
		q := app.DB().Dialect.Quote
		now := db.Now()
		id := security.RandomID(15)
		_, err := app.DB().Exec(r.Context(), fmt.Sprintf(
			"INSERT INTO %s (id, created, updated, name, %s, %s, scopes, %s, %s, %s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			q(APIKeysCollection), q("keyHash"), q("keyTail"), q("admin"), q("identityCollection"), q("identityRecord")),
			id, now, now, body.Name, security.HashToken(plain), plain[len(plain)-4:],
			scopesVal, body.Admin, body.IdentityCollection, body.IdentityRecord)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, 200, map[string]any{
			"id":   id,
			"name": body.Name,
			// The plaintext key is returned exactly once.
			"key": plain,
		})
	}))

	mux.HandleFunc("DELETE /api/apikeys/{id}", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		q := app.DB().Dialect.Quote
		res, err := app.DB().Exec(r.Context(), fmt.Sprintf(
			"DELETE FROM %s WHERE id = ?", q(APIKeysCollection)), r.PathValue("id"))
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		if n, _ := res.RowsAffected(); n == 0 {
			core.WriteError(w, app.Log(), core.NotFound(""))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
}

// apiKeyResolver authenticates requests carrying a forge_ API key via
// Authorization: Bearer or X-Api-Key.
func apiKeyResolver(app *core.App) core.AuthResolver {
	return func(r *http.Request) (*core.Auth, error) {
		raw := strings.TrimSpace(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if !strings.HasPrefix(raw, KeyPrefix) {
			raw = strings.TrimSpace(r.Header.Get("X-Api-Key"))
		}
		if !strings.HasPrefix(raw, KeyPrefix) {
			return nil, nil
		}
		if app.Schema().Get(APIKeysCollection) == nil {
			return nil, nil
		}
		q := app.DB().Dialect.Quote
		row, err := app.DB().QueryMap(r.Context(), fmt.Sprintf(
			"SELECT * FROM %s WHERE %s = ? LIMIT 1", q(APIKeysCollection), q("keyHash")),
			security.HashToken(raw))
		if err != nil || row == nil {
			return nil, nil
		}
		go app.DB().Exec(context.Background(), fmt.Sprintf(
			"UPDATE %s SET %s = ? WHERE id = ?", q(APIKeysCollection), q("lastUsed")),
			db.Now(), row["id"])

		auth := &core.Auth{
			Method: "apikey",
			Scopes: db.ToJSONList(row["scopes"]),
		}
		if identity := db.ToString(row["identityRecord"]); identity != "" {
			c := app.Schema().Get(db.ToString(row["identityCollection"]))
			if c != nil {
				if record, err := app.FindRecordByID(r.Context(), c.Name, identity); err == nil && record != nil {
					auth.Record = record
					auth.Collection = c
					auth.Roles = app.ResolveRoles(r.Context(), c, record)
					auth.Superuser = c.Name == core.SuperusersCollection
				}
			}
			return auth, nil
		}
		if db.ToBool(row["admin"]) {
			auth.Superuser = true
		}
		return auth, nil
	}
}
