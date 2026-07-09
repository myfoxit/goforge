package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// NewTestApp bootstraps an in-memory app for tests.
func NewTestApp(t *testing.T) *App {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DB.DSN = "file:" + security.RandomID(8) + "?mode=memory&cache=shared"
	cfg.Log.Level = "error"
	app := New(cfg)
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB().Close() })
	return app
}

func TestBootstrapCreatesSystemState(t *testing.T) {
	app := NewTestApp(t)
	if app.Schema().Get(SuperusersCollection) == nil {
		t.Fatal("_superusers missing")
	}
	if app.Secret() == "" {
		t.Fatal("secret not generated")
	}
}

func TestSettingsRoundtrip(t *testing.T) {
	app := NewTestApp(t)
	ctx := context.Background()
	s := app.Settings()

	s.RegisterSection(SettingsSection{
		ID: "test", Title: "Test",
		Fields: []SettingsField{
			{Key: "test.plain", Label: "Plain", Type: "text", Default: "dflt"},
			{Key: "test.secret", Label: "Secret", Type: "secret"},
		},
	})
	if got := s.String("test.plain"); got != "dflt" {
		t.Fatalf("default = %q", got)
	}
	if err := s.SetMany(ctx, map[string]any{"test.plain": "v1", "test.secret": "hunter2"}); err != nil {
		t.Fatal(err)
	}
	if s.String("test.secret") != "hunter2" {
		t.Fatal("secret readback failed")
	}

	// Secrets must be encrypted at rest.
	row, err := app.DB().QueryMap(ctx, "SELECT value, encrypted FROM _params WHERE "+app.DB().Dialect.Quote("key")+" = ?", "test.secret")
	if err != nil || row == nil {
		t.Fatalf("param row: %v %v", row, err)
	}
	if !db.ToBool(row["encrypted"]) || db.ToString(row["value"]) == `"hunter2"` {
		t.Fatalf("secret stored in plaintext: %v", row)
	}

	// Mask writes are ignored.
	if err := s.Set(ctx, "test.secret", SecretMask); err != nil {
		t.Fatal(err)
	}
	if s.String("test.secret") != "hunter2" {
		t.Fatal("mask overwrote secret")
	}

	// Export masks secrets.
	for _, sec := range s.Export() {
		if sec["id"] != "test" {
			continue
		}
		for _, f := range sec["fields"].([]map[string]any) {
			if f["key"] == "test.secret" && f["value"] != SecretMask {
				t.Fatalf("export leaked secret: %v", f["value"])
			}
		}
	}
}

func TestAuthTokenLifecycle(t *testing.T) {
	app := NewTestApp(t)
	ctx := context.Background()

	// create a superuser row manually
	hash, _ := security.HashPassword("password123")
	id := security.RandomID(15)
	_, err := app.DB().Exec(ctx,
		"INSERT INTO _superusers (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, db.Now(), db.Now(), "admin@x.dev", hash, security.RandomToken(24), true, "Admin")
	if err != nil {
		t.Fatal(err)
	}

	su := app.Schema().Get(SuperusersCollection)
	record, _ := app.FindRecordByID(ctx, SuperusersCollection, id)
	tok, err := app.NewAuthToken(su, record, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	auth, err := app.VerifyAuthToken(ctx, tok)
	if err != nil {
		t.Fatal(err)
	}
	if !auth.IsSuperuser() || auth.ID() != id {
		t.Fatalf("auth = %+v", auth)
	}

	// Rotating tokenKey invalidates the token.
	if _, err := app.DB().Exec(ctx, "UPDATE _superusers SET tokenKey = ? WHERE id = ?", security.RandomToken(24), id); err != nil {
		t.Fatal(err)
	}
	if _, err := app.VerifyAuthToken(ctx, tok); err == nil {
		t.Fatal("stale token accepted after tokenKey rotation")
	}
}

func TestMiddlewareAuthAndGuards(t *testing.T) {
	app := NewTestApp(t)
	ctx := context.Background()

	hash, _ := security.HashPassword("password123")
	id := security.RandomID(15)
	app.DB().Exec(ctx,
		"INSERT INTO _superusers (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, db.Now(), db.Now(), "admin@x.dev", hash, security.RandomToken(24), true, "Admin")
	record, _ := app.FindRecordByID(ctx, SuperusersCollection, id)
	tok, _ := app.NewAuthToken(app.Schema().Get(SuperusersCollection), record, time.Hour)

	app.Mux().HandleFunc("GET /guarded", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, 200, map[string]string{"ok": "yes"})
	}))
	handler := Chain(app.Mux(), app.WithAuth())

	req := httptest.NewRequest("GET", "/guarded", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Fatalf("unauthenticated = %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/guarded", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("superuser = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuleVars(t *testing.T) {
	auth := &Auth{
		Record:     map[string]any{"id": "u1", "email": "a@b.c", "password": "hash"},
		Collection: &schema.Collection{Name: "users", Type: schema.TypeAuth, Fields: []*schema.Field{{Name: "password", Type: schema.FieldPassword, Hidden: true}}},
		Roles:      []string{"admin"},
	}
	vars := RuleVars(auth, map[string]any{"title": "x"}, nil)

	if v, _ := vars("@request.auth.id"); v != "u1" {
		t.Fatalf("auth.id = %v", v)
	}
	if v, _ := vars("@request.auth.roles"); v != `["admin"]` {
		t.Fatalf("roles = %v", v)
	}
	if v, _ := vars("@request.auth.password"); v != nil {
		t.Fatal("hidden field leaked")
	}
	if v, _ := vars("@request.data.title"); v != "x" {
		t.Fatalf("data.title = %v", v)
	}
	// unauthenticated
	anon := RuleVars(nil, nil, nil)
	if v, _ := anon("@request.auth.id"); v != nil {
		t.Fatalf("anon id = %v", v)
	}
}

func TestRateLimiter(t *testing.T) {
	rl := newRateLimiter(1, 2)
	if !rl.allow("a") || !rl.allow("a") {
		t.Fatal("burst rejected")
	}
	if rl.allow("a") {
		t.Fatal("over-burst allowed")
	}
	if !rl.allow("b") {
		t.Fatal("other key rejected")
	}
}
