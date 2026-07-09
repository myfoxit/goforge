// Package perm provides role-based access control on top of the rules
// engine: a _roles collection, a multi-relation "roles" field on the users
// collection and rule helpers like `@request.auth.roles ~ 'admin'`.
//
// Route-level guards for custom modules:
//
//	mux.HandleFunc("GET /api/reports", app.RequireRole("admin")(handler))
package perm

import (
	"context"
	"fmt"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// RolesCollection stores role definitions.
const RolesCollection = "_roles"

// Module wires the RBAC pieces ("perm").
type Module struct{}

func (Module) ID() string { return "perm" }

func (Module) Register(app *core.App) error {
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		if err := ensureRolesCollection(e.App); err != nil {
			return err
		}
		return ensureUsersRolesField(e.App)
	})
	return nil
}

func ensureRolesCollection(app *core.App) error {
	if app.Schema().Get(RolesCollection) != nil {
		return nil
	}
	err := app.Schema().Save(context.Background(), &schema.Collection{
		Name: RolesCollection, Type: schema.TypeBase, System: true,
		Fields: []*schema.Field{
			{Name: "name", Type: schema.FieldText, Required: true, Unique: true, System: true,
				Options: map[string]any{"pattern": "^[a-z0-9_-]+$", "max": float64(50)}},
			{Name: "description", Type: schema.FieldText},
		},
		// nil rules → superuser-managed via admin UI / API
	})
	if err != nil {
		return err
	}
	// Seed a default admin role.
	q := app.DB().Dialect.Quote
	now := db.Now()
	_, err = app.DB().Exec(context.Background(), fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, name, description) VALUES (?, ?, ?, ?, ?)",
		q(RolesCollection)),
		security.RandomID(15), now, now, "admin", "Full application access")
	return err
}

// ensureUsersRolesField adds the roles relation to the users collection.
func ensureUsersRolesField(app *core.App) error {
	users := app.Schema().Get("users")
	if users == nil || users.Field("roles") != nil {
		return nil
	}
	upd := users.Clone()
	upd.Fields = append(upd.Fields, &schema.Field{
		Name: "roles", Type: schema.FieldRelation, System: true,
		Options: map[string]any{"collection": RolesCollection, "maxSelect": float64(20)},
	})
	return app.Schema().Save(context.Background(), upd)
}

// HasRole reports whether the identity carries a role (superusers always do).
func HasRole(auth *core.Auth, role string) bool {
	if auth.IsSuperuser() {
		return true
	}
	if auth == nil {
		return false
	}
	for _, r := range auth.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// AssignRole adds a role (by name) to a user record.
func AssignRole(ctx context.Context, app *core.App, userID, roleName string) error {
	role, err := app.FindFirstRecord(ctx, RolesCollection, "name", roleName)
	if err != nil {
		return err
	}
	if role == nil {
		return fmt.Errorf("perm: role %q not found", roleName)
	}
	user, err := app.FindRecordByID(ctx, "users", userID)
	if err != nil {
		return err
	}
	if user == nil {
		return fmt.Errorf("perm: user %q not found", userID)
	}
	roles := db.ToJSONList(user["roles"])
	roleID := db.ToString(role["id"])
	for _, id := range roles {
		if id == roleID {
			return nil
		}
	}
	roles = append(roles, roleID)
	f := schema.Field{Name: "roles", Type: schema.FieldRelation,
		Options: map[string]any{"collection": RolesCollection, "maxSelect": float64(20)}}
	stored, err := f.NormalizeValue(roles)
	if err != nil {
		return err
	}
	q := app.DB().Dialect.Quote
	_, err = app.DB().Exec(ctx, fmt.Sprintf(
		"UPDATE %s SET roles = ?, updated = ? WHERE id = ?", q("users")),
		stored, db.Now(), userID)
	return err
}
