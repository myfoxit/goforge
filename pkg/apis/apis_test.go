package apis

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/myfoxit/goforge/pkg/auth"
	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
	"github.com/myfoxit/goforge/pkg/mail"
	"github.com/myfoxit/goforge/pkg/perm"
	"github.com/myfoxit/goforge/pkg/security"
)

type testEnv struct {
	app        *core.App
	h          http.Handler
	superToken string
}

func newEnv(t *testing.T) *testEnv {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DB.DSN = "file:" + security.RandomID(8) + "?mode=memory&cache=shared"
	cfg.Log.Level = "error"
	app := core.New(cfg)
	app.Use(mail.Module{}, auth.Module{}, perm.Module{}, Module{})
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB().Close() })

	// superuser
	hash, _ := security.HashPassword("super-secret-pass")
	id := security.RandomID(15)
	now := db.Now()
	if _, err := app.DB().Exec(context.Background(),
		"INSERT INTO _superusers (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, now, now, "root@x.dev", hash, security.RandomToken(24), true, "Root"); err != nil {
		t.Fatal(err)
	}
	record, _ := app.FindRecordByID(context.Background(), core.SuperusersCollection, id)
	tok, err := app.NewAuthToken(app.Schema().Get(core.SuperusersCollection), record, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	return &testEnv{
		app:        app,
		h:          core.Chain(app.Mux(), app.WithAuth()),
		superToken: tok,
	}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, token string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var reader io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (e *testEnv) mustStatus(t *testing.T, want int, method, path string, body any, token string) map[string]any {
	t.Helper()
	rec, out := e.do(t, method, path, body, token)
	if rec.Code != want {
		t.Fatalf("%s %s = %d (want %d): %s", method, path, rec.Code, want, rec.Body.String())
	}
	return out
}

func TestCollectionAdminAPI(t *testing.T) {
	e := newEnv(t)

	// Unauthenticated blocked.
	rec, _ := e.do(t, "GET", "/api/collections", nil, "")
	if rec.Code != 401 {
		t.Fatalf("unauth collections = %d", rec.Code)
	}

	// Create a posts collection.
	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "posts",
		"type": "base",
		"fields": []map[string]any{
			{"name": "title", "type": "text", "required": true},
			{"name": "views", "type": "number"},
			{"name": "owner", "type": "relation", "options": map[string]any{"collection": "users"}},
			{"name": "tags", "type": "select", "options": map[string]any{"values": []string{"go", "svelte", "sql"}, "maxSelect": 2}},
		},
		"listRule":   "",
		"viewRule":   "",
		"createRule": "@request.auth.id != ''",
		"updateRule": "owner = @request.auth.id",
		"deleteRule": "owner = @request.auth.id",
	}, e.superToken)

	// Duplicate name rejected.
	rec, _ = e.do(t, "POST", "/api/collections", map[string]any{"name": "posts", "type": "base"}, e.superToken)
	if rec.Code != 400 {
		t.Fatalf("duplicate collection = %d", rec.Code)
	}

	// Add a field via PATCH.
	got := e.mustStatus(t, 200, "GET", "/api/collections/posts", nil, e.superToken)
	fields := got["fields"].([]any)
	fields = append(fields, map[string]any{"name": "body", "type": "editor"})
	e.mustStatus(t, 200, "PATCH", "/api/collections/posts", map[string]any{"fields": fields}, e.superToken)
	got = e.mustStatus(t, 200, "GET", "/api/collections/posts", nil, e.superToken)
	if len(got["fields"].([]any)) != len(fields) {
		t.Fatal("field not added")
	}
}

func TestRecordsCRUDWithRules(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "notes",
		"type": "base",
		"fields": []map[string]any{
			{"name": "text", "type": "text", "required": true},
			{"name": "owner", "type": "relation", "options": map[string]any{"collection": "users"}},
			{"name": "public", "type": "bool"},
		},
		"listRule":   "public = true || owner = @request.auth.id",
		"viewRule":   "public = true || owner = @request.auth.id",
		"createRule": "@request.auth.id != '' && owner = @request.auth.id",
		"updateRule": "owner = @request.auth.id",
		"deleteRule": "owner = @request.auth.id",
	}, e.superToken)

	// Two users via the records API (public registration).
	u1 := e.mustStatus(t, 200, "POST", "/api/collections/users/records", map[string]any{
		"email": "u1@x.dev", "password": "password-user-1", "passwordConfirm": "password-user-1", "name": "U1",
	}, "")
	u2 := e.mustStatus(t, 200, "POST", "/api/collections/users/records", map[string]any{
		"email": "u2@x.dev", "password": "password-user-2", "passwordConfirm": "password-user-2", "name": "U2",
	}, "")
	u1ID, u2ID := u1["id"].(string), u2["id"].(string)
	if u1["password"] != nil || u1["tokenKey"] != nil {
		t.Fatal("auth secrets leaked on register")
	}
	// stored password must be hashed
	row, _ := e.app.FindRecordByID(ctx, "users", u1ID)
	if !strings.HasPrefix(db.ToString(row["password"]), "$argon2id$") {
		t.Fatal("password stored unhashed")
	}

	login := func(email, pw string) string {
		out := e.mustStatus(t, 200, "POST", "/api/collections/users/auth-with-password",
			map[string]string{"identity": email, "password": pw}, "")
		return out["token"].(string)
	}
	t1, t2 := login("u1@x.dev", "password-user-1"), login("u2@x.dev", "password-user-2")

	// Create: unauthenticated rejected (rule), authenticated w/ own owner ok.
	rec, _ := e.do(t, "POST", "/api/collections/notes/records", map[string]any{"text": "x", "owner": u1ID}, "")
	if rec.Code != 403 && rec.Code != 400 {
		t.Fatalf("unauth create = %d", rec.Code)
	}
	// Wrong owner → create rule fails post-insert.
	rec, _ = e.do(t, "POST", "/api/collections/notes/records", map[string]any{"text": "x", "owner": u2ID}, t1)
	if rec.Code != 400 {
		t.Fatalf("foreign owner create = %d", rec.Code)
	}
	n1 := e.mustStatus(t, 200, "POST", "/api/collections/notes/records",
		map[string]any{"text": "private note u1", "owner": u1ID, "public": false}, t1)
	e.mustStatus(t, 200, "POST", "/api/collections/notes/records",
		map[string]any{"text": "public note u1", "owner": u1ID, "public": true}, t1)
	e.mustStatus(t, 200, "POST", "/api/collections/notes/records",
		map[string]any{"text": "private note u2", "owner": u2ID, "public": false}, t2)

	// Validation error.
	rec, out := e.do(t, "POST", "/api/collections/notes/records", map[string]any{"owner": u1ID}, t1)
	if rec.Code != 400 || out["data"] == nil {
		t.Fatalf("missing required = %d %v", rec.Code, out)
	}

	// List: u1 sees own + public (3 of 3 for u1: 2 own + 1 public-of-others? u2's note is private → 2)
	list := e.mustStatus(t, 200, "GET", "/api/collections/notes/records", nil, t1)
	if int(list["totalItems"].(float64)) != 2 {
		t.Fatalf("u1 list = %v", list["totalItems"])
	}
	// u2 sees own + u1's public note = 2
	list = e.mustStatus(t, 200, "GET", "/api/collections/notes/records", nil, t2)
	if int(list["totalItems"].(float64)) != 2 {
		t.Fatalf("u2 list = %v", list["totalItems"])
	}
	// Anonymous sees only public = 1
	list = e.mustStatus(t, 200, "GET", "/api/collections/notes/records", nil, "")
	if int(list["totalItems"].(float64)) != 1 {
		t.Fatalf("anon list = %v", list["totalItems"])
	}
	// Superuser sees all = 3
	list = e.mustStatus(t, 200, "GET", "/api/collections/notes/records", nil, e.superToken)
	if int(list["totalItems"].(float64)) != 3 {
		t.Fatalf("super list = %v", list["totalItems"])
	}

	// Filter + sort + expand.
	list = e.mustStatus(t, 200, "GET",
		"/api/collections/notes/records?filter="+urlq("text ~ 'public'")+"&sort=-created&expand=owner", nil, t1)
	items := list["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("filtered = %d", len(items))
	}
	first := items[0].(map[string]any)
	exp := first["expand"].(map[string]any)["owner"].(map[string]any)
	if exp["name"] != "U1" {
		t.Fatalf("expand owner = %v", exp)
	}
	if exp["password"] != nil || exp["tokenKey"] != nil {
		t.Fatal("expand leaked auth secrets")
	}

	// Malicious filter rejected.
	rec, _ = e.do(t, "GET", "/api/collections/notes/records?filter="+urlq("tokenKey != ''"), nil, t1)
	if rec.Code != 400 {
		t.Fatalf("hidden-field filter = %d", rec.Code)
	}
	rec, _ = e.do(t, "GET", "/api/collections/notes/records?filter="+urlq("1;DROP TABLE notes"), nil, t1)
	if rec.Code != 400 {
		t.Fatalf("injection filter = %d", rec.Code)
	}

	// View / update / delete rules.
	n1ID := n1["id"].(string)
	rec, _ = e.do(t, "GET", "/api/collections/notes/records/"+n1ID, nil, t2)
	if rec.Code != 404 {
		t.Fatalf("foreign private view = %d", rec.Code)
	}
	e.mustStatus(t, 200, "GET", "/api/collections/notes/records/"+n1ID, nil, t1)

	rec, _ = e.do(t, "PATCH", "/api/collections/notes/records/"+n1ID, map[string]any{"text": "hacked"}, t2)
	if rec.Code != 404 {
		t.Fatalf("foreign update = %d", rec.Code)
	}
	upd := e.mustStatus(t, 200, "PATCH", "/api/collections/notes/records/"+n1ID, map[string]any{"text": "edited"}, t1)
	if upd["text"] != "edited" {
		t.Fatalf("update = %v", upd["text"])
	}

	rec, _ = e.do(t, "DELETE", "/api/collections/notes/records/"+n1ID, nil, t2)
	if rec.Code != 404 {
		t.Fatalf("foreign delete = %d", rec.Code)
	}
	e.mustStatus(t, 204, "DELETE", "/api/collections/notes/records/"+n1ID, nil, t1)

	// Password change requires oldPassword.
	rec, _ = e.do(t, "PATCH", "/api/collections/users/records/"+u1ID,
		map[string]any{"password": "brand-new-pass-1", "passwordConfirm": "brand-new-pass-1"}, t1)
	if rec.Code != 400 {
		t.Fatalf("password change without oldPassword = %d", rec.Code)
	}
	e.mustStatus(t, 200, "PATCH", "/api/collections/users/records/"+u1ID,
		map[string]any{"password": "brand-new-pass-1", "passwordConfirm": "brand-new-pass-1", "oldPassword": "password-user-1"}, t1)
	login("u1@x.dev", "brand-new-pass-1")
}

func TestPaginationAndViewCollections(t *testing.T) {
	e := newEnv(t)
	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "items", "type": "base",
		"fields":   []map[string]any{{"name": "n", "type": "number"}},
		"listRule": "", "viewRule": "",
	}, e.superToken)
	for i := range 25 {
		e.mustStatus(t, 200, "POST", "/api/collections/items/records", map[string]any{"n": i}, e.superToken)
	}
	list := e.mustStatus(t, 200, "GET", "/api/collections/items/records?perPage=10&page=3&sort=n", nil, "")
	if int(list["totalItems"].(float64)) != 25 || int(list["totalPages"].(float64)) != 3 {
		t.Fatalf("pagination meta = %v", list)
	}
	items := list["items"].([]any)
	if len(items) != 5 {
		t.Fatalf("page 3 size = %d", len(items))
	}
	if items[0].(map[string]any)["n"].(float64) != 20 {
		t.Fatalf("page 3 first = %v", items[0])
	}

	// View collection over items.
	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "items_stats", "type": "view",
		"options":  map[string]any{"query": "SELECT id, n * 10 AS big FROM items"},
		"listRule": "",
	}, e.superToken)
	list = e.mustStatus(t, 200, "GET", "/api/collections/items_stats/records?perPage=5&sort=-big", nil, "")
	first := list["items"].([]any)[0].(map[string]any)
	if db.ToFloat(first["big"]) != 240 {
		t.Fatalf("view first = %v", first)
	}

	// Writes to views rejected.
	rec, _ := e.do(t, "POST", "/api/collections/items_stats/records", map[string]any{"big": 1}, e.superToken)
	if rec.Code != 400 {
		t.Fatalf("view write = %d", rec.Code)
	}
}

func TestFileUploadAndServe(t *testing.T) {
	e := newEnv(t)
	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "docs", "type": "base",
		"fields": []map[string]any{
			{"name": "title", "type": "text"},
			{"name": "attachment", "type": "file", "options": map[string]any{"maxSelect": 2}},
		},
		"listRule": "", "viewRule": "", "createRule": "", "updateRule": "", "deleteRule": "",
	}, e.superToken)

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	mw.WriteField("title", "with file")
	fw, _ := mw.CreateFormFile("attachment", "hello.txt")
	fw.Write([]byte("file-content-here"))
	mw.Close()

	req := httptest.NewRequest("POST", "/api/collections/docs/records", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("multipart create = %d: %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	json.Unmarshal(rec.Body.Bytes(), &created)
	names := created["attachment"].([]any)
	if len(names) != 1 {
		t.Fatalf("attachment = %v", created["attachment"])
	}
	fname := names[0].(string)
	if !strings.HasSuffix(fname, ".txt") {
		t.Fatalf("sanitized name = %s", fname)
	}

	// Serve.
	fileURL := fmt.Sprintf("/api/files/docs/%s/%s", created["id"], fname)
	req = httptest.NewRequest("GET", fileURL, nil)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 200 || rec.Body.String() != "file-content-here" {
		t.Fatalf("file serve = %d %q", rec.Code, rec.Body.String())
	}

	// Unknown filename 404s.
	req = httptest.NewRequest("GET", fmt.Sprintf("/api/files/docs/%s/ghost.txt", created["id"]), nil)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("ghost file = %d", rec.Code)
	}

	// Record delete removes the file.
	e.mustStatus(t, 204, "DELETE", fmt.Sprintf("/api/collections/docs/records/%s", created["id"]), nil, e.superToken)
	req = httptest.NewRequest("GET", fileURL, nil)
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	if rec.Code != 404 {
		t.Fatalf("file after delete = %d", rec.Code)
	}
}

func TestSettingsAPI(t *testing.T) {
	e := newEnv(t)
	rec, _ := e.do(t, "GET", "/api/settings", nil, "")
	if rec.Code != 401 {
		t.Fatalf("unauth settings = %d", rec.Code)
	}
	out := e.mustStatus(t, 200, "GET", "/api/settings", nil, e.superToken)
	if out["sections"] == nil {
		t.Fatal("no sections")
	}
	e.mustStatus(t, 200, "PATCH", "/api/settings", map[string]any{"app.name": "Renamed"}, e.superToken)
	if e.app.AppName() != "Renamed" {
		t.Fatalf("app name = %s", e.app.AppName())
	}
}

func TestRealtimeSSE(t *testing.T) {
	e := newEnv(t)
	e.mustStatus(t, 200, "POST", "/api/collections", map[string]any{
		"name": "events", "type": "base",
		"fields":   []map[string]any{{"name": "msg", "type": "text"}},
		"listRule": "", "viewRule": "", "createRule": "",
	}, e.superToken)

	srv := httptest.NewServer(e.h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/realtime")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	reader := bufio.NewReader(resp.Body)

	// First frame: GF_CONNECT with clientId.
	clientID := ""
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data:") {
			var payload map[string]string
			json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(line), "data:")), &payload)
			clientID = payload["clientId"]
			break
		}
	}
	if clientID == "" {
		t.Fatal("no clientId received")
	}

	// Subscribe to the collection topic.
	subBody, _ := json.Marshal(map[string]any{"clientId": clientID, "subscriptions": []string{"events"}})
	subResp, err := http.Post(srv.URL+"/api/realtime", "application/json", bytes.NewReader(subBody))
	if err != nil || subResp.StatusCode != 204 {
		t.Fatalf("subscribe failed: %v %d", err, subResp.StatusCode)
	}

	// Trigger a create and expect the event.
	e.mustStatus(t, 200, "POST", "/api/collections/events/records", map[string]any{"msg": "hello rt"}, "")

	got := make(chan string, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return
			}
			if strings.HasPrefix(line, "data:") && strings.Contains(line, "hello rt") {
				got <- line
				return
			}
		}
	}()
	select {
	case line := <-got:
		if !strings.Contains(line, `"action":"create"`) {
			t.Fatalf("event = %s", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no realtime event received")
	}
}

func urlq(s string) string {
	r := strings.NewReplacer(" ", "%20", "'", "%27", "!", "%21", "~", "%7E", ";", "%3B", "=", "%3D", "&", "%26", "+", "%2B")
	return r.Replace(s)
}
