package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/myfoxit/goforge/pkg/apis"
	"github.com/myfoxit/goforge/pkg/auth"
	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
	"github.com/myfoxit/goforge/pkg/mail"
	"github.com/myfoxit/goforge/pkg/security"
)

type env struct {
	app        *core.App
	h          http.Handler
	superToken string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DB.DSN = "file:" + security.RandomID(8) + "?mode=memory&cache=shared"
	cfg.Log.Level = "error"
	app := core.New(cfg)
	app.Use(mail.Module{}, auth.Module{}, apis.Module{}, Module{})
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB().Close() })

	hash, _ := security.HashPassword("super-secret-pass")
	id := security.RandomID(15)
	now := db.Now()
	app.DB().Exec(context.Background(),
		"INSERT INTO _superusers (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, now, now, "root@x.dev", hash, security.RandomToken(24), true, "Root")
	record, _ := app.FindRecordByID(context.Background(), core.SuperusersCollection, id)
	tok, _ := app.NewAuthToken(app.Schema().Get(core.SuperusersCollection), record, time.Hour)

	return &env{app: app, h: core.Chain(app.Mux(), app.WithAuth()), superToken: tok}
}

// rpc posts a JSON-RPC request to /api/mcp.
func (e *env) rpc(t *testing.T, token, method string, params any) (int, map[string]any) {
	t.Helper()
	body := map[string]any{"jsonrpc": "2.0", "id": 1, "method": method}
	if params != nil {
		body["params"] = params
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/mcp", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

// createKey provisions an API key via the management endpoint.
func (e *env) createKey(t *testing.T, body map[string]any) string {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/apikeys", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.superToken)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create key = %d: %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	key := out["key"].(string)
	if !strings.HasPrefix(key, KeyPrefix) {
		t.Fatalf("key = %s", key)
	}
	return key
}

func TestMCPHandshakeAndAuth(t *testing.T) {
	e := newEnv(t)

	// No credentials → 401.
	code, _ := e.rpc(t, "", "initialize", map[string]any{"protocolVersion": "2025-06-18"})
	if code != 401 {
		t.Fatalf("unauth mcp = %d", code)
	}

	key := e.createKey(t, map[string]any{"name": "test", "admin": true})
	code, out := e.rpc(t, key, "initialize", map[string]any{
		"protocolVersion": "2025-06-18",
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "test-client", "version": "1.0"},
	})
	if code != 200 {
		t.Fatalf("initialize = %d %v", code, out)
	}
	result := out["result"].(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocol = %v", result["protocolVersion"])
	}
	if result["serverInfo"].(map[string]any)["name"] == "" {
		t.Fatal("no server name")
	}

	// Old protocol version negotiated.
	_, out = e.rpc(t, key, "initialize", map[string]any{"protocolVersion": "2024-11-05"})
	if out["result"].(map[string]any)["protocolVersion"] != "2024-11-05" {
		t.Fatal("version negotiation failed")
	}

	// ping
	_, out = e.rpc(t, key, "ping", nil)
	if out["error"] != nil {
		t.Fatalf("ping error: %v", out["error"])
	}

	// unknown method
	_, out = e.rpc(t, key, "bogus/method", nil)
	if out["error"] == nil {
		t.Fatal("unknown method accepted")
	}
}

func TestMCPSchemaBuildingAndCRUD(t *testing.T) {
	e := newEnv(t)
	key := e.createKey(t, map[string]any{"name": "admin", "admin": true})

	// tools/list includes admin tools.
	_, out := e.rpc(t, key, "tools/list", nil)
	tools := out["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	for _, want := range []string{"collections_save", "collections_list", "settings_set", "users_list", "users_create"} {
		if !names[want] {
			t.Fatalf("missing tool %q in %v", want, names)
		}
	}

	// Build a collection via MCP (the "AI builds the app" path).
	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name": "collections_save",
		"arguments": map[string]any{
			"name": "tasks",
			"fields": []map[string]any{
				{"name": "title", "type": "text", "required": true},
				{"name": "done", "type": "bool"},
				{"name": "priority", "type": "select", "options": map[string]any{"values": []string{"low", "high"}}},
			},
			"listRule": "", "viewRule": "",
		},
	})
	callRes := out["result"].(map[string]any)
	if callRes["isError"] == true {
		t.Fatalf("collections_save error: %v", callRes)
	}
	if e.app.Schema().Get("tasks") == nil {
		t.Fatal("collection not created")
	}

	// New collection's tools appear.
	_, out = e.rpc(t, key, "tools/list", nil)
	tools = out["result"].(map[string]any)["tools"].([]any)
	found := false
	for _, tl := range tools {
		if tl.(map[string]any)["name"] == "tasks_create" {
			found = true
		}
	}
	if !found {
		t.Fatal("tasks tools missing after creation")
	}

	// CRUD via MCP.
	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name":      "tasks_create",
		"arguments": map[string]any{"data": map[string]any{"title": "write tests", "priority": "high"}},
	})
	text := out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var created map[string]any
	json.Unmarshal([]byte(text), &created)
	if created["title"] != "write tests" {
		t.Fatalf("created = %v", created)
	}

	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name":      "tasks_list",
		"arguments": map[string]any{"filter": "priority = 'high'"},
	})
	text = out["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	var list map[string]any
	json.Unmarshal([]byte(text), &list)
	if int(list["totalItems"].(float64)) != 1 {
		t.Fatalf("mcp list = %v", list["totalItems"])
	}

	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name":      "tasks_update",
		"arguments": map[string]any{"id": created["id"], "data": map[string]any{"done": true}},
	})
	if out["result"].(map[string]any)["isError"] == true {
		t.Fatalf("update failed: %v", out)
	}

	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name":      "tasks_delete",
		"arguments": map[string]any{"id": created["id"]},
	})
	if out["result"].(map[string]any)["isError"] == true {
		t.Fatalf("delete failed: %v", out)
	}

	// Invalid rule rejected with tool error.
	_, out = e.rpc(t, key, "tools/call", map[string]any{
		"name": "collections_save",
		"arguments": map[string]any{
			"name":     "bad",
			"listRule": "not a valid rule !!!",
		},
	})
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatal("invalid rule accepted")
	}
}

func TestMCPScopedKeys(t *testing.T) {
	e := newEnv(t)
	adminKey := e.createKey(t, map[string]any{"name": "admin", "admin": true})

	// Create two collections.
	for _, name := range []string{"alpha", "beta"} {
		_, out := e.rpc(t, adminKey, "tools/call", map[string]any{
			"name": "collections_save",
			"arguments": map[string]any{
				"name":   name,
				"fields": []map[string]any{{"name": "v", "type": "text"}},
			},
		})
		if out["result"].(map[string]any)["isError"] == true {
			t.Fatalf("setup %s failed: %v", name, out)
		}
	}

	// Scoped key: read-only on alpha.
	scoped := e.createKey(t, map[string]any{"name": "ro", "admin": true, "scopes": []string{"alpha:read"}})

	_, out := e.rpc(t, scoped, "tools/list", nil)
	tools := out["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, tl := range tools {
		names[tl.(map[string]any)["name"].(string)] = true
	}
	if !names["alpha_list"] || names["alpha_create"] || names["beta_list"] {
		t.Fatalf("scoped tools wrong: %v", names)
	}

	// Enforcement at call time too.
	_, out = e.rpc(t, scoped, "tools/call", map[string]any{
		"name": "alpha_list", "arguments": map[string]any{},
	})
	if out["result"].(map[string]any)["isError"] == true {
		t.Fatalf("scoped read blocked: %v", out)
	}
	_, out = e.rpc(t, scoped, "tools/call", map[string]any{
		"name": "alpha_create", "arguments": map[string]any{"data": map[string]any{"v": "x"}},
	})
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatal("scoped create allowed")
	}
	_, out = e.rpc(t, scoped, "tools/call", map[string]any{
		"name": "beta_list", "arguments": map[string]any{},
	})
	if out["result"].(map[string]any)["isError"] != true {
		t.Fatal("out-of-scope read allowed")
	}
}

func TestAPIKeyOnREST(t *testing.T) {
	e := newEnv(t)
	adminKey := e.createKey(t, map[string]any{"name": "admin", "admin": true})

	// API keys work on the plain REST API as well (superuser-gated route).
	req := httptest.NewRequest("GET", "/api/collections", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("apikey REST = %d: %s", rec.Code, rec.Body.String())
	}

	// Deleted key stops working.
	req = httptest.NewRequest("GET", "/api/apikeys", nil)
	req.Header.Set("Authorization", "Bearer "+e.superToken)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	keyID := out["items"].([]any)[0].(map[string]any)["id"].(string)

	req = httptest.NewRequest("DELETE", "/api/apikeys/"+keyID, nil)
	req.Header.Set("Authorization", "Bearer "+e.superToken)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("delete key = %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/collections", nil)
	req.Header.Set("Authorization", "Bearer "+adminKey)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code == 200 {
		t.Fatal("revoked key still works")
	}
}
