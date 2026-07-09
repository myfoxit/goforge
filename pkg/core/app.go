// Package core contains the GoForge application kernel: the App container,
// module system, lifecycle hooks, settings store, HTTP middleware and server.
//
// A minimal GoForge app:
//
//	app := core.New(config.Default())
//	app.Use(auth.Module{}, apis.Module{})
//	app.Start()
package core

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/migrations"
	"github.com/myfoxit/goforge/pkg/rules"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
	"github.com/myfoxit/goforge/pkg/token"
)

// Version is the framework version, stamped by the release process.
var Version = "0.1.0-dev"

// Module is a pluggable unit of functionality. Register is called once when
// the module is added — before Bootstrap — and wires migrations, settings
// sections, hooks and routes.
type Module interface {
	ID() string
	Register(app *App) error
}

// App is the GoForge application container.
type App struct {
	Hooks

	cfg      *config.Config
	log      *slog.Logger
	db       *db.DB
	schema   *schema.Registry
	settings *Settings
	mux      *http.ServeMux
	runner   *migrations.Runner

	modules       []Module
	moduleIDs     map[string]bool
	authResolvers []AuthResolver
	bootstrapped  bool
}

// New creates an App from boot config.
func New(cfg *config.Config) *App {
	if cfg == nil {
		cfg = config.Default()
	}
	level := slog.LevelInfo
	switch cfg.Log.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	var handler slog.Handler
	if cfg.Log.JSON {
		handler = slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	} else {
		handler = slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	}
	return &App{
		cfg:       cfg,
		log:       slog.New(handler),
		mux:       http.NewServeMux(),
		moduleIDs: map[string]bool{},
	}
}

// Accessors.

func (a *App) Config() *config.Config         { return a.cfg }
func (a *App) Log() *slog.Logger              { return a.log }
func (a *App) DB() *db.DB                     { return a.db }
func (a *App) Schema() *schema.Registry       { return a.schema }
func (a *App) Settings() *Settings            { return a.settings }
func (a *App) Mux() *http.ServeMux            { return a.mux }
func (a *App) Migrations() *migrations.Runner { return a.runner }

// Secret returns the app master secret (available after Bootstrap).
func (a *App) Secret() string { return a.cfg.Secret }

// BaseURL returns the public app URL without a trailing slash.
func (a *App) BaseURL() string {
	u := a.settings.String("app.url")
	if u == "" {
		u = a.cfg.AppURL
	}
	for len(u) > 0 && u[len(u)-1] == '/' {
		u = u[:len(u)-1]
	}
	return u
}

// AppName returns the display name of the application.
func (a *App) AppName() string {
	if a.settings != nil {
		if n := a.settings.String("app.name"); n != "" {
			return n
		}
	}
	return a.cfg.AppName
}

// Use registers modules (deduplicated by ID).
func (a *App) Use(mods ...Module) *App {
	for _, m := range mods {
		if a.moduleIDs[m.ID()] {
			continue
		}
		a.moduleIDs[m.ID()] = true
		a.modules = append(a.modules, m)
	}
	return a
}

// HasModule reports whether a module ID has been registered.
func (a *App) HasModule(id string) bool { return a.moduleIDs[id] }

// Modules returns the registered module IDs.
func (a *App) Modules() []string {
	out := make([]string, 0, len(a.modules))
	for _, m := range a.modules {
		out = append(out, m.ID())
	}
	return out
}

// AddAuthResolver registers an additional credentials resolver
// (e.g. API keys). The built-in JWT resolver always runs first.
func (a *App) AddAuthResolver(fn AuthResolver) {
	a.authResolvers = append(a.authResolvers, fn)
}

// Bootstrap opens the database, runs migrations, loads the schema registry
// and settings, ensures system collections and calls module registration.
func (a *App) Bootstrap(ctx context.Context) error {
	if a.bootstrapped {
		return nil
	}
	if _, err := a.cfg.EnsureSecret(security.RandomToken); err != nil {
		return fmt.Errorf("core: ensure secret: %w", err)
	}

	dsn := a.cfg.DB.DSN
	if a.cfg.DB.Driver == "sqlite" {
		if err := os.MkdirAll(a.cfg.DataDir, 0o755); err != nil {
			return err
		}
		dsn = a.cfg.SQLiteDSN()
	}
	d, err := db.Open(a.cfg.DB.Driver, dsn)
	if err != nil {
		return err
	}
	if a.cfg.DB.MaxOpenConns > 0 {
		d.SetMaxOpenConns(a.cfg.DB.MaxOpenConns)
	}
	if a.cfg.DB.MaxIdleConns > 0 {
		d.SetMaxIdleConns(a.cfg.DB.MaxIdleConns)
	}
	a.db = d

	a.runner = migrations.NewRunner(d, a.log)
	a.schema = schema.NewRegistry(d, a.log)
	a.schema.SetRuleCheck(func(c *schema.Collection, rule string) error {
		_, _, err := rules.CompileRule(rule, &rules.Context{
			Dialect:       d.Dialect,
			Collection:    c,
			Vars:          func(string) (any, bool) { return nil, true },
			Relations:     func(name string) *schema.Collection { return a.schema.Get(name) },
			HiddenAllowed: true, // admins may reference hidden fields in rules
		})
		return err
	})
	a.settings = newSettings(d, a.cfg.Secret)
	a.registerCoreSettings()

	// Module registration (may add migrations, settings sections, routes, hooks).
	for _, m := range a.modules {
		if err := m.Register(a); err != nil {
			return fmt.Errorf("core: register module %q: %w", m.ID(), err)
		}
	}

	if err := a.runner.Run(ctx); err != nil {
		return err
	}
	if err := a.schema.Init(ctx); err != nil {
		return err
	}
	if err := a.settings.init(ctx); err != nil {
		return err
	}
	if err := a.ensureSystemCollections(ctx); err != nil {
		return err
	}

	a.bootstrapped = true
	if err := a.OnBootstrap.Trigger(&BootstrapEvent{App: a}); err != nil {
		return err
	}
	return nil
}

// ensureSystemCollections creates _superusers on first run.
func (a *App) ensureSystemCollections(ctx context.Context) error {
	if a.schema.Get(SuperusersCollection) == nil {
		superusers := &schema.Collection{
			Name:   SuperusersCollection,
			Type:   schema.TypeAuth,
			System: true,
			Fields: schema.BaseAuthFields(),
		}
		if err := a.schema.Save(ctx, superusers); err != nil {
			return fmt.Errorf("core: create %s: %w", SuperusersCollection, err)
		}
	}
	return nil
}

// registerCoreSettings declares the "Application" settings section.
func (a *App) registerCoreSettings() {
	a.settings.RegisterSection(SettingsSection{
		ID: "app", Title: "Application", Order: 0,
		Fields: []SettingsField{
			{Key: "app.name", Label: "Application name", Type: "text", Default: a.cfg.AppName},
			{Key: "app.url", Label: "Application URL", Type: "text", Default: a.cfg.AppURL,
				Help: "Public base URL used in emails and OAuth redirects."},
			{Key: "app.hideControls", Label: "Hide collection controls", Type: "bool", Default: false,
				Help: "Prevent schema changes from the admin UI (production hardening)."},
		},
	})
}

// --- raw record helpers (rule-free; the apis package enforces rules) ---

// FindRecordByID fetches a row by id from a collection table.
// Returns (nil, nil) when missing.
func (a *App) FindRecordByID(ctx context.Context, collection, id string) (map[string]any, error) {
	c := a.schema.Get(collection)
	if c == nil {
		return nil, fmt.Errorf("core: unknown collection %q", collection)
	}
	q := a.db.Dialect.Quote
	if c.IsView() {
		return a.db.QueryMap(ctx,
			fmt.Sprintf("SELECT * FROM %s WHERE %s = ? LIMIT 1", schema.ViewQuery(c), q("id")), id)
	}
	return a.db.QueryMap(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE %s = ? LIMIT 1", q(c.Name), q("id")), id)
}

// FindFirstRecord fetches the first row where field = value.
func (a *App) FindFirstRecord(ctx context.Context, collection, field string, value any) (map[string]any, error) {
	c := a.schema.Get(collection)
	if c == nil {
		return nil, fmt.Errorf("core: unknown collection %q", collection)
	}
	if !c.HasColumn(field) {
		return nil, fmt.Errorf("core: unknown column %q", field)
	}
	q := a.db.Dialect.Quote
	return a.db.QueryMap(ctx,
		fmt.Sprintf("SELECT * FROM %s WHERE %s = ? LIMIT 1", q(c.Name), q(field)), value)
}

// ResolveRoles loads role names for a record's "roles" relation field.
func (a *App) ResolveRoles(ctx context.Context, c *schema.Collection, record map[string]any) []string {
	if c == nil || record == nil {
		return nil
	}
	f := c.Field("roles")
	if f == nil {
		return nil
	}
	ids := db.ToJSONList(record["roles"])
	if len(ids) == 0 {
		return nil
	}
	target := f.RelationCollection()
	if f.Type == schema.FieldSelect || target == "" {
		return ids // select fields store the names directly
	}
	rc := a.schema.Get(target)
	if rc == nil || rc.Field("name") == nil {
		return ids
	}
	q := a.db.Dialect.Quote
	placeholders := ""
	args := make([]any, len(ids))
	for i, id := range ids {
		if i > 0 {
			placeholders += ","
		}
		placeholders += "?"
		args[i] = id
	}
	rows, err := a.db.QueryMaps(ctx,
		fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", q("name"), q(rc.Name), q("id"), placeholders), args...)
	if err != nil {
		a.log.Warn("resolve roles failed", "err", err)
		return nil
	}
	names := make([]string, 0, len(rows))
	for _, row := range rows {
		names = append(names, db.ToString(row["name"]))
	}
	return names
}

// NewAuthToken issues an auth JWT for a record of an auth collection.
// The token embeds a fingerprint of the record's tokenKey so that changing
// the password (which rotates tokenKey) invalidates existing tokens.
func (a *App) NewAuthToken(c *schema.Collection, record map[string]any, ttl time.Duration) (string, error) {
	typ := token.TypeAuth
	if c.Name == SuperusersCollection {
		typ = token.TypeAdmin
	}
	return token.Sign(a.cfg.Secret, typ, token.Claims{
		"sub": db.ToString(record["id"]),
		"col": c.Name,
		"rk":  security.HashToken(db.ToString(record["tokenKey"]))[:16],
	}, ttl)
}

// VerifyAuthToken validates an auth/admin JWT and loads its record.
func (a *App) VerifyAuthToken(ctx context.Context, raw string) (*Auth, error) {
	var claims token.Claims
	var err error
	claims, err = token.Verify(a.cfg.Secret, raw, token.TypeAuth)
	if err != nil {
		claims, err = token.Verify(a.cfg.Secret, raw, token.TypeAdmin)
		if err != nil {
			return nil, err
		}
	}
	colName := claims.String("col")
	c := a.schema.Get(colName)
	if c == nil || !c.IsAuth() {
		return nil, token.ErrInvalid
	}
	record, err := a.FindRecordByID(ctx, colName, claims.Subject())
	if err != nil || record == nil {
		return nil, token.ErrInvalid
	}
	rk := security.HashToken(db.ToString(record["tokenKey"]))[:16]
	if !security.Equal(rk, claims.String("rk")) {
		return nil, token.ErrInvalid
	}
	auth := &Auth{
		Record:     record,
		Collection: c,
		Superuser:  c.Name == SuperusersCollection,
		Method:     "token",
	}
	auth.Roles = a.ResolveRoles(ctx, c, record)
	return auth, nil
}
