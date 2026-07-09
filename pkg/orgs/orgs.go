// Package orgs adds multi-tenancy: organizations, memberships with roles and
// an email invite flow — the backbone of a typical B2B SaaS.
//
// Tenant-scoping pattern for your own collections: add an "org" relation
// field and use rules like
//
//	org.members ~ @request.auth.id
//
// (single-hop relation to orgs, whose members field is a multi-relation).
package orgs

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/mail"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
	"github.com/myfoxit/goforge/pkg/token"
)

// Collection names.
const (
	OrgsCollection    = "orgs"
	MembersCollection = "org_members"
)

// Module wires multi-tenancy ("orgs").
type Module struct{}

func (Module) ID() string { return "orgs" }

func (Module) Register(app *core.App) error {
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		return ensureCollections(e.App)
	})

	limit := app.RateLimit(5, 15)
	mux := app.Mux()
	mux.HandleFunc("POST /api/orgs", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		createOrg(app, w, r)
	}))
	mux.HandleFunc("POST /api/orgs/{id}/invite", app.RequireAuth(limit(func(w http.ResponseWriter, r *http.Request) {
		inviteMember(app, w, r)
	})))
	mux.HandleFunc("POST /api/orgs/accept-invite", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		acceptInvite(app, w, r)
	}))
	mux.HandleFunc("POST /api/orgs/{id}/leave", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		leaveOrg(app, w, r)
	}))
	return nil
}

func ensureCollections(app *core.App) error {
	ctx := context.Background()
	if app.Schema().Get(OrgsCollection) == nil {
		memberOrOwner := "members ~ @request.auth.id || owner = @request.auth.id"
		ownerOnly := "owner = @request.auth.id"
		err := app.Schema().Save(ctx, &schema.Collection{
			Name: OrgsCollection, Type: schema.TypeBase,
			Fields: []*schema.Field{
				{Name: "name", Type: schema.FieldText, Required: true, System: true,
					Options: map[string]any{"min": float64(2), "max": float64(80)}},
				{Name: "slug", Type: schema.FieldText, Unique: true, System: true,
					Options: map[string]any{"pattern": "^[a-z0-9-]+$", "max": float64(50)}},
				{Name: "owner", Type: schema.FieldRelation, System: true,
					Options: map[string]any{"collection": "users"}},
				{Name: "members", Type: schema.FieldRelation, System: true,
					Options: map[string]any{"collection": "users", "maxSelect": float64(10000)}},
			},
			ListRule: &memberOrOwner, ViewRule: &memberOrOwner,
			UpdateRule: &ownerOnly, DeleteRule: &ownerOnly,
			// CreateRule nil: orgs are created through POST /api/orgs
		})
		if err != nil {
			return err
		}
	}
	if app.Schema().Get(MembersCollection) == nil {
		viaOrg := "org.members ~ @request.auth.id || org.owner = @request.auth.id"
		err := app.Schema().Save(ctx, &schema.Collection{
			Name: MembersCollection, Type: schema.TypeBase,
			Fields: []*schema.Field{
				{Name: "org", Type: schema.FieldRelation, Required: true, System: true,
					Options: map[string]any{"collection": OrgsCollection}},
				{Name: "user", Type: schema.FieldRelation, Required: true, System: true,
					Options: map[string]any{"collection": "users"}},
				{Name: "role", Type: schema.FieldSelect, System: true,
					Options: map[string]any{"values": []any{"owner", "admin", "member"}}},
			},
			Indexes:  []schema.Index{{Name: "ux_org_members", Columns: []string{"org", "user"}, Unique: true}},
			ListRule: &viaOrg, ViewRule: &viaOrg,
			// writes go through the org endpoints / superusers
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func createOrg(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	var body struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	c := app.Schema().Get(OrgsCollection)
	nameField := c.Field("name")
	name, err := nameField.NormalizeValue(body.Name)
	if err != nil {
		core.WriteError(w, app.Log(), core.ValidationError("name", err.Error()))
		return
	}
	slug := body.Slug
	if slug == "" {
		slug = slugify(body.Name)
	}
	if _, err := c.Field("slug").NormalizeValue(slug); err != nil {
		core.WriteError(w, app.Log(), core.ValidationError("slug", err.Error()))
		return
	}
	if existing, _ := app.FindFirstRecord(r.Context(), OrgsCollection, "slug", slug); existing != nil {
		core.WriteError(w, app.Log(), core.ValidationError("slug", "Slug is already taken."))
		return
	}

	q := app.DB().Dialect.Quote
	orgID := security.RandomID(15)
	now := db.Now()
	membersField := c.Field("members")
	members, _ := membersField.NormalizeValue([]string{auth.ID()})
	if _, err := app.DB().Exec(r.Context(), fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, name, slug, owner, members) VALUES (?, ?, ?, ?, ?, ?, ?)",
		q(OrgsCollection)),
		orgID, now, now, name, slug, auth.ID(), members); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if _, err := app.DB().Exec(r.Context(), fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, org, %s, role) VALUES (?, ?, ?, ?, ?, ?)",
		q(MembersCollection), q("user")),
		security.RandomID(15), now, now, orgID, auth.ID(), "owner"); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	record, _ := app.FindRecordByID(r.Context(), OrgsCollection, orgID)
	core.WriteJSON(w, 200, publicOrg(app, record))
}

// requireOrgAdmin loads the org and verifies the caller is owner/admin.
func requireOrgAdmin(app *core.App, r *http.Request) (map[string]any, *core.Auth, error) {
	auth := core.AuthFromContext(r.Context())
	org, err := app.FindRecordByID(r.Context(), OrgsCollection, r.PathValue("id"))
	if err != nil || org == nil {
		return nil, nil, core.NotFound("")
	}
	if auth.IsSuperuser() || db.ToString(org["owner"]) == auth.ID() {
		return org, auth, nil
	}
	q := app.DB().Dialect.Quote
	row, _ := app.DB().QueryMap(r.Context(), fmt.Sprintf(
		"SELECT role FROM %s WHERE org = ? AND %s = ? LIMIT 1", q(MembersCollection), q("user")),
		org["id"], auth.ID())
	if row != nil && (db.ToString(row["role"]) == "admin" || db.ToString(row["role"]) == "owner") {
		return org, auth, nil
	}
	return nil, nil, core.Forbidden("Requires org owner or admin.")
}

func inviteMember(app *core.App, w http.ResponseWriter, r *http.Request) {
	org, _, err := requireOrgAdmin(app, r)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	var body struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if body.Role == "" {
		body.Role = "member"
	}
	if body.Role != "member" && body.Role != "admin" {
		core.WriteError(w, app.Log(), core.ValidationError("role", "Role must be member or admin."))
		return
	}
	emailField := schema.Field{Name: "email", Type: schema.FieldEmail, Required: true}
	norm, err := emailField.NormalizeValue(body.Email)
	if err != nil {
		core.WriteError(w, app.Log(), core.ValidationError("email", "Invalid email."))
		return
	}
	email := db.ToString(norm)

	invite, err := token.Sign(app.Secret(), token.TypeInvite, token.Claims{
		"sub":   email,
		"org":   db.ToString(org["id"]),
		"role":  body.Role,
		"email": email,
	}, 7*24*time.Hour)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	loginPath := app.Settings().String("auth.appLoginURL")
	action := app.BaseURL() + loginPath + "?inviteToken=" + invite
	go mail.SendTemplate(context.Background(), app, mail.Address{Email: email}, "invite", mail.TemplateData{
		ActionURL: action,
		Extra:     map[string]any{"org": db.ToString(org["name"])},
	})
	core.WriteJSON(w, 200, map[string]any{"sent": true, "email": email})
}

func acceptInvite(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	var body struct {
		Token string `json:"token"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	claims, err := token.Verify(app.Secret(), body.Token, token.TypeInvite)
	if err != nil {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid or expired invite."))
		return
	}
	if db.ToString(auth.Record["email"]) != claims.String("email") {
		core.WriteError(w, app.Log(), core.Forbidden("This invite was issued for "+claims.String("email")+"."))
		return
	}
	org, err := app.FindRecordByID(r.Context(), OrgsCollection, claims.String("org"))
	if err != nil || org == nil {
		core.WriteError(w, app.Log(), core.NotFound("The organization no longer exists."))
		return
	}

	q := app.DB().Dialect.Quote
	now := db.Now()
	// Membership row (idempotent via unique index).
	app.DB().Exec(r.Context(), fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, org, %s, role) VALUES (?, ?, ?, ?, ?, ?)",
		q(MembersCollection), q("user")),
		security.RandomID(15), now, now, org["id"], auth.ID(), claims.String("role"))

	// Append to the members multi-relation.
	members := db.ToJSONList(org["members"])
	present := false
	for _, m := range members {
		if m == auth.ID() {
			present = true
		}
	}
	if !present {
		members = append(members, auth.ID())
		f := app.Schema().Get(OrgsCollection).Field("members")
		stored, _ := f.NormalizeValue(members)
		app.DB().Exec(r.Context(), fmt.Sprintf(
			"UPDATE %s SET members = ?, updated = ? WHERE id = ?", q(OrgsCollection)),
			stored, now, org["id"])
	}
	record, _ := app.FindRecordByID(r.Context(), OrgsCollection, db.ToString(org["id"]))
	core.WriteJSON(w, 200, publicOrg(app, record))
}

func leaveOrg(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	org, err := app.FindRecordByID(r.Context(), OrgsCollection, r.PathValue("id"))
	if err != nil || org == nil {
		core.WriteError(w, app.Log(), core.NotFound(""))
		return
	}
	if db.ToString(org["owner"]) == auth.ID() {
		core.WriteError(w, app.Log(), core.BadRequest("The owner cannot leave — transfer ownership or delete the org."))
		return
	}
	q := app.DB().Dialect.Quote
	app.DB().Exec(r.Context(), fmt.Sprintf(
		"DELETE FROM %s WHERE org = ? AND %s = ?", q(MembersCollection), q("user")),
		org["id"], auth.ID())

	members := db.ToJSONList(org["members"])
	kept := members[:0]
	for _, m := range members {
		if m != auth.ID() {
			kept = append(kept, m)
		}
	}
	f := app.Schema().Get(OrgsCollection).Field("members")
	stored, _ := f.NormalizeValue(kept)
	app.DB().Exec(r.Context(), fmt.Sprintf(
		"UPDATE %s SET members = ?, updated = ? WHERE id = ?", q(OrgsCollection)),
		stored, db.Now(), org["id"])
	w.WriteHeader(http.StatusNoContent)
}

func publicOrg(app *core.App, record map[string]any) map[string]any {
	c := app.Schema().Get(OrgsCollection)
	out := map[string]any{"id": db.ToString(record["id"]), "created": db.ToString(record["created"])}
	for _, f := range c.Fields {
		if !f.Hidden {
			out[f.Name] = f.APIValue(record[f.Name])
		}
	}
	return out
}

func slugify(s string) string {
	out := make([]rune, 0, len(s))
	prevDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			out = append(out, r)
			prevDash = false
		case r >= 'A' && r <= 'Z':
			out = append(out, r+32)
			prevDash = false
		default:
			if !prevDash && len(out) > 0 {
				out = append(out, '-')
				prevDash = true
			}
		}
	}
	for len(out) > 0 && out[len(out)-1] == '-' {
		out = out[:len(out)-1]
	}
	if len(out) == 0 {
		return "org-" + security.RandomID(6)
	}
	return string(out)
}
