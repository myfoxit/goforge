// Package auth implements authentication for auth collections: password
// login with verification and reset flows, token refresh, optional TOTP MFA
// and (via the oauth submodule) OAuth2 / OIDC sign-in.
//
// Passwords are hashed with argon2id. Every credential response returns a
// purpose-scoped JWT whose validity is tied to the record's tokenKey, so a
// password change invalidates all existing sessions.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/mail"
	"github.com/myfoxit/goforge/pkg/schema"
)

// UsersCollection is the default end-user auth collection.
const UsersCollection = "users"

// Module wires password auth routes and the default users collection.
type Module struct{}

func (Module) ID() string { return "auth" }

func (Module) Register(app *core.App) error {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "auth", Title: "Authentication", Order: 10,
		Fields: []core.SettingsField{
			{Key: "auth.tokenTTLHours", Label: "Auth token TTL (hours)", Type: "number", Default: float64(168)},
			{Key: "auth.sendVerification", Label: "Send verification email on signup", Type: "bool", Default: true},
			{Key: "auth.appLoginURL", Label: "App login path", Type: "text", Default: "/login",
				Help: "Where email links (verify, reset) send users to complete the flow."},
		},
	})

	// Ensure the default users collection exists.
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		return EnsureUsersCollection(e.App)
	})

	// Send the verification email after a fresh signup.
	app.OnRecordAfterCreate.Add(func(e *core.RecordEvent) error {
		if !e.Collection.IsAuth() || e.Collection.Name == core.SuperusersCollection {
			return nil
		}
		if !e.App.Settings().Bool("auth.sendVerification") || db.ToBool(e.Record["verified"]) {
			return nil
		}
		go sendVerificationMail(e.App, e.Collection, e.Record)
		return nil
	})

	registerRoutes(app)
	return nil
}

// EnsureUsersCollection creates the default "users" auth collection.
func EnsureUsersCollection(app *core.App) error {
	if app.Schema().Get(UsersCollection) != nil {
		return nil
	}
	owner := "id = @request.auth.id"
	public := ""
	users := &schema.Collection{
		Name:   UsersCollection,
		Type:   schema.TypeAuth,
		System: true,
		Fields: append(schema.BaseAuthFields(),
			&schema.Field{ID: "sys_avatar", Name: "avatar", Type: schema.FieldFile, System: true,
				Options: map[string]any{"maxSelect": float64(1)}},
		),
		ListRule:   &owner,
		ViewRule:   &owner,
		CreateRule: &public, // open registration by default; tighten in admin
		UpdateRule: &owner,
		// DeleteRule nil → only superusers delete accounts
		Options: map[string]any{"identityFields": []any{"email"}, "minPasswordLength": float64(10)},
	}
	return app.Schema().Save(context.Background(), users)
}

// AuthCollection resolves + validates an auth collection by name.
func AuthCollection(app *core.App, name string) (*schema.Collection, error) {
	c := app.Schema().Get(name)
	if c == nil || !c.IsAuth() {
		return nil, core.NotFound("Missing or invalid auth collection.")
	}
	return c, nil
}

// TokenTTL returns the configured auth token lifetime.
func TokenTTL(app *core.App) time.Duration {
	hours := app.Settings().Int("auth.tokenTTLHours")
	if hours <= 0 {
		hours = 168
	}
	return time.Duration(hours) * time.Hour
}

// AuthResponse issues a token and renders the standard auth payload.
func AuthResponse(app *core.App, w http.ResponseWriter, c *schema.Collection, record map[string]any) {
	tok, err := app.NewAuthToken(c, record, TokenTTL(app))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{
		"token":  tok,
		"record": PublicRecord(c, record),
	})
}

// PublicRecord strips hidden fields and converts values to API shape.
func PublicRecord(c *schema.Collection, record map[string]any) map[string]any {
	out := map[string]any{
		"id":             db.ToString(record["id"]),
		"created":        db.ToString(record["created"]),
		"updated":        db.ToString(record["updated"]),
		"collectionName": c.Name,
	}
	for _, f := range c.Fields {
		if f.Hidden {
			continue
		}
		out[f.Name] = f.APIValue(record[f.Name])
	}
	return out
}

func appActionURL(app *core.App, path string, query map[string]string) string {
	u := app.BaseURL() + path
	sep := "?"
	for k, v := range query {
		u += sep + k + "=" + v
		sep = "&"
	}
	return u
}

func sendVerificationMail(app *core.App, c *schema.Collection, record map[string]any) {
	tok, err := newPurposeToken(app, c, record, "verification", 24*time.Hour)
	if err != nil {
		app.Log().Error("verification token", "err", err)
		return
	}
	loginPath := app.Settings().String("auth.appLoginURL")
	action := appActionURL(app, loginPath, map[string]string{"verifyToken": tok})
	err = mail.SendTemplate(context.Background(), app, mail.Address{
		Email: db.ToString(record["email"]),
		Name:  db.ToString(record["name"]),
	}, "verification", mail.TemplateData{
		ActionURL: action,
		Name:      db.ToString(record["name"]),
		Email:     db.ToString(record["email"]),
	})
	if err != nil {
		app.Log().Error("send verification mail", "err", err)
	}
}

func fetchByIdentity(app *core.App, c *schema.Collection, identity string) (map[string]any, error) {
	opts := c.AuthOptions()
	for _, field := range opts.IdentityFields {
		if !c.HasColumn(field) {
			continue
		}
		value := identity
		if field == "email" {
			f := schema.Field{Name: "email", Type: schema.FieldEmail}
			norm, err := f.NormalizeValue(identity)
			if err != nil {
				continue
			}
			value = db.ToString(norm)
		}
		record, err := app.FindFirstRecord(context.Background(), c.Name, field, value)
		if err != nil {
			return nil, err
		}
		if record != nil {
			return record, nil
		}
	}
	return nil, nil
}

func rotateTokenKey(app *core.App, c *schema.Collection, id string) error {
	q := app.DB().Dialect.Quote
	_, err := app.DB().Exec(context.Background(),
		fmt.Sprintf("UPDATE %s SET %s = ?, updated = ? WHERE id = ?", q(c.Name), q("tokenKey")),
		newTokenKey(), db.Now(), id)
	return err
}
