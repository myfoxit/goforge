package orgs

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	"github.com/myfoxit/goforge/pkg/webhooks"
)

func newApp(t *testing.T) (*core.App, http.Handler) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DB.DSN = "file:" + security.RandomID(8) + "?mode=memory&cache=shared"
	cfg.Log.Level = "error"
	app := core.New(cfg)
	app.Use(mail.Module{}, auth.Module{}, apis.Module{}, Module{}, webhooks.Module{})
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB().Close() })
	mail.ResetSentMessages()
	return app, core.Chain(app.Mux(), app.WithAuth())
}

func request(t *testing.T, h http.Handler, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec.Code, out
}

func signup(t *testing.T, h http.Handler, email string) string {
	t.Helper()
	code, _ := request(t, h, "POST", "/api/collections/users/records", map[string]any{
		"email": email, "password": "long-enough-pass", "passwordConfirm": "long-enough-pass",
	}, "")
	if code != 200 {
		t.Fatalf("signup %s = %d", email, code)
	}
	code, out := request(t, h, "POST", "/api/collections/users/auth-with-password", map[string]any{
		"identity": email, "password": "long-enough-pass",
	}, "")
	if code != 200 {
		t.Fatalf("login %s = %d", email, code)
	}
	return out["token"].(string)
}

func TestOrgLifecycle(t *testing.T) {
	app, h := newApp(t)

	owner := signup(t, h, "owner@x.dev")
	invitee := signup(t, h, "member@x.dev")
	outsider := signup(t, h, "outsider@x.dev")

	// Create org.
	code, org := request(t, h, "POST", "/api/orgs", map[string]any{"name": "Acme Corp"}, owner)
	if code != 200 {
		t.Fatalf("create org = %d %v", code, org)
	}
	orgID := org["id"].(string)
	if org["slug"] != "acme-corp" {
		t.Fatalf("slug = %v", org["slug"])
	}

	// Owner sees it in the records API; outsider does not.
	code, list := request(t, h, "GET", "/api/collections/orgs/records", nil, owner)
	if code != 200 || int(list["totalItems"].(float64)) != 1 {
		t.Fatalf("owner org list = %d %v", code, list["totalItems"])
	}
	_, list = request(t, h, "GET", "/api/collections/orgs/records", nil, outsider)
	if int(list["totalItems"].(float64)) != 0 {
		t.Fatalf("outsider org list = %v", list["totalItems"])
	}

	// Invite flow.
	code, _ = request(t, h, "POST", "/api/orgs/"+orgID+"/invite", map[string]any{"email": "member@x.dev"}, owner)
	if code != 200 {
		t.Fatalf("invite = %d", code)
	}
	// Outsider cannot invite.
	code, _ = request(t, h, "POST", "/api/orgs/"+orgID+"/invite", map[string]any{"email": "x@x.dev"}, outsider)
	if code != 403 {
		t.Fatalf("outsider invite = %d", code)
	}

	tok := waitForInviteToken(t)
	// Wrong user cannot accept.
	code, _ = request(t, h, "POST", "/api/orgs/accept-invite", map[string]any{"token": tok}, outsider)
	if code != 403 {
		t.Fatalf("wrong-user accept = %d", code)
	}
	code, accepted := request(t, h, "POST", "/api/orgs/accept-invite", map[string]any{"token": tok}, invitee)
	if code != 200 {
		t.Fatalf("accept = %d %v", code, accepted)
	}
	members := accepted["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members = %v", members)
	}

	// Member now sees the org via rules (members ~ @request.auth.id).
	_, list = request(t, h, "GET", "/api/collections/orgs/records", nil, invitee)
	if int(list["totalItems"].(float64)) != 1 {
		t.Fatalf("member org list = %v", list["totalItems"])
	}

	// Tenant-scoped collection pattern.
	super := makeSuperuser(t, app)
	code, _ = request(t, h, "POST", "/api/collections", map[string]any{
		"name": "projects", "type": "base",
		"fields": []map[string]any{
			{"name": "name", "type": "text", "required": true},
			{"name": "org", "type": "relation", "options": map[string]any{"collection": "orgs"}},
		},
		"listRule":   "org.members ~ @request.auth.id",
		"viewRule":   "org.members ~ @request.auth.id",
		"createRule": "@request.auth.id != '' && org.members ~ @request.auth.id",
		"updateRule": "org.members ~ @request.auth.id",
	}, super)
	if code != 200 {
		t.Fatalf("projects collection = %d", code)
	}
	code, _ = request(t, h, "POST", "/api/collections/projects/records",
		map[string]any{"name": "Migration", "org": orgID}, owner)
	if code != 200 {
		t.Fatalf("project create = %d", code)
	}
	_, list = request(t, h, "GET", "/api/collections/projects/records", nil, invitee)
	if int(list["totalItems"].(float64)) != 1 {
		t.Fatalf("member project list = %v", list["totalItems"])
	}
	_, list = request(t, h, "GET", "/api/collections/projects/records", nil, outsider)
	if int(list["totalItems"].(float64)) != 0 {
		t.Fatalf("outsider project list = %v", list["totalItems"])
	}

	// Leave.
	code, _ = request(t, h, "POST", "/api/orgs/"+orgID+"/leave", nil, invitee)
	if code != 204 {
		t.Fatalf("leave = %d", code)
	}
	_, list = request(t, h, "GET", "/api/collections/projects/records", nil, invitee)
	if int(list["totalItems"].(float64)) != 0 {
		t.Fatalf("post-leave project list = %v", list["totalItems"])
	}
	// Owner cannot leave own org.
	code, _ = request(t, h, "POST", "/api/orgs/"+orgID+"/leave", nil, owner)
	if code != 400 {
		t.Fatalf("owner leave = %d", code)
	}
}

func makeSuperuser(t *testing.T, app *core.App) string {
	t.Helper()
	hash, _ := security.HashPassword("super-secret-pass")
	id := security.RandomID(15)
	now := db.Now()
	app.DB().Exec(context.Background(),
		"INSERT INTO _superusers (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		id, now, now, "root@x.dev", hash, security.RandomToken(24), true, "Root")
	record, _ := app.FindRecordByID(context.Background(), core.SuperusersCollection, id)
	tok, _ := app.NewAuthToken(app.Schema().Get(core.SuperusersCollection), record, time.Hour)
	return tok
}

func waitForInviteToken(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range mail.SentMessages() {
			if idx := strings.Index(m.Text, "inviteToken="); idx >= 0 {
				rest := m.Text[idx+len("inviteToken="):]
				end := strings.IndexAny(rest, "\" \n<&")
				if end == -1 {
					end = len(rest)
				}
				tok, err := url.QueryUnescape(rest[:end])
				if err != nil {
					tok = rest[:end]
				}
				return tok
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("no invite mail captured")
	return ""
}

func TestWebhookDelivery(t *testing.T) {
	app, h := newApp(t)
	super := makeSuperuser(t, app)

	received := make(chan map[string]any, 4)
	var gotSig string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		json.NewDecoder(r.Body).Decode(&payload)
		gotSig = r.Header.Get("X-Forge-Signature")
		received <- payload
		w.WriteHeader(200)
	}))
	defer target.Close()

	// Register the webhook row directly.
	q := app.DB().Dialect.Quote
	now := db.Now()
	_, err := app.DB().Exec(context.Background(),
		"INSERT INTO "+q(webhooks.Collection)+" (id, created, updated, name, url, collections, actions, secret, enabled, "+q("lastStatus")+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')",
		security.RandomID(15), now, now, "test", target.URL, `["watched"]`, `["create"]`, "whsec", true)
	if err != nil {
		t.Fatal(err)
	}

	code, _ := request(t, h, "POST", "/api/collections", map[string]any{
		"name": "watched", "type": "base",
		"fields": []map[string]any{{"name": "v", "type": "text"}}, "createRule": "",
	}, super)
	if code != 200 {
		t.Fatalf("collection = %d", code)
	}
	code, _ = request(t, h, "POST", "/api/collections/watched/records", map[string]any{"v": "ping"}, "")
	if code != 200 {
		t.Fatalf("record = %d", code)
	}

	select {
	case payload := <-received:
		if payload["action"] != "create" || payload["collection"] != "watched" {
			t.Fatalf("payload = %v", payload)
		}
		if !strings.HasPrefix(gotSig, "sha256=") {
			t.Fatalf("signature = %s", gotSig)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("webhook not delivered")
	}
}
