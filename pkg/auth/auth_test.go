package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
	"github.com/myfoxit/goforge/pkg/mail"
	"github.com/myfoxit/goforge/pkg/perm"
	"github.com/myfoxit/goforge/pkg/security"
)

func newTestApp(t *testing.T) (*core.App, http.Handler) {
	t.Helper()
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.DB.DSN = "file:" + security.RandomID(8) + "?mode=memory&cache=shared"
	cfg.Log.Level = "error"
	app := core.New(cfg)
	app.Use(mail.Module{}, Module{}, MFAModule{}, OAuthModule{}, perm.Module{})
	if err := app.Bootstrap(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { app.DB().Close() })
	mail.ResetSentMessages()
	return app, core.Chain(app.Mux(), app.WithAuth())
}

func createUser(t *testing.T, app *core.App, email, password string, verified bool) string {
	t.Helper()
	hash, err := security.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	id := security.RandomID(15)
	now := db.Now()
	_, err = app.DB().Exec(context.Background(),
		`INSERT INTO users (id, created, updated, email, password, tokenKey, verified, name, avatar, roles) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', '[]')`,
		id, now, now, email, hash, security.RandomToken(24), verified, "Test User")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func postJSON(t *testing.T, h http.Handler, path string, body any, headers map[string]string) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path, strings.NewReader(string(raw)))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func TestPasswordLogin(t *testing.T) {
	app, h := newTestApp(t)
	createUser(t, app, "ada@x.dev", "correct-horse-battery", true)

	rec, out := postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "ada@x.dev", "password": "correct-horse-battery"}, nil)
	if rec.Code != 200 {
		t.Fatalf("login = %d: %s", rec.Code, rec.Body.String())
	}
	tok, _ := out["token"].(string)
	if tok == "" {
		t.Fatal("no token")
	}
	record := out["record"].(map[string]any)
	if record["email"] != "ada@x.dev" {
		t.Fatalf("record = %v", record)
	}
	if _, leaked := record["password"]; leaked {
		t.Fatal("password leaked in response")
	}
	if _, leaked := record["tokenKey"]; leaked {
		t.Fatal("tokenKey leaked in response")
	}

	// Case-insensitive identity + email alias field.
	rec, _ = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"email": "ADA@X.dev", "password": "correct-horse-battery"}, nil)
	if rec.Code != 200 {
		t.Fatalf("case-insensitive login = %d", rec.Code)
	}

	// Wrong password.
	rec, _ = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "ada@x.dev", "password": "wrong"}, nil)
	if rec.Code != 400 {
		t.Fatalf("wrong password = %d", rec.Code)
	}
	// Unknown user - same status (no enumeration).
	rec, _ = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "ghost@x.dev", "password": "whatever"}, nil)
	if rec.Code != 400 {
		t.Fatalf("unknown user = %d", rec.Code)
	}

	// Refresh with the token.
	rec, out = postJSON(t, h, "/api/collections/users/auth-refresh", nil,
		map[string]string{"Authorization": "Bearer " + tok})
	if rec.Code != 200 || out["token"] == "" {
		t.Fatalf("refresh = %d", rec.Code)
	}

	// Non-auth collection rejected.
	rec, _ = postJSON(t, h, "/api/collections/_mfa/auth-with-password",
		map[string]string{"identity": "x", "password": "y"}, nil)
	if rec.Code != 404 {
		t.Fatalf("non-auth collection = %d", rec.Code)
	}
}

func TestVerificationFlow(t *testing.T) {
	app, h := newTestApp(t)
	createUser(t, app, "eve@x.dev", "some-long-password", false)

	rec, _ := postJSON(t, h, "/api/collections/users/request-verification",
		map[string]string{"email": "eve@x.dev"}, nil)
	if rec.Code != 204 {
		t.Fatalf("request-verification = %d", rec.Code)
	}
	// unknown email → same response (no enumeration)
	rec, _ = postJSON(t, h, "/api/collections/users/request-verification",
		map[string]string{"email": "ghost@x.dev"}, nil)
	if rec.Code != 204 {
		t.Fatalf("request-verification unknown = %d", rec.Code)
	}

	tok := waitForMailToken(t, "verifyToken")
	rec, _ = postJSON(t, h, "/api/collections/users/confirm-verification",
		map[string]string{"token": tok}, nil)
	if rec.Code != 204 {
		t.Fatalf("confirm-verification = %d: %s", rec.Code, rec.Body.String())
	}
	row, _ := app.FindFirstRecord(context.Background(), "users", "email", "eve@x.dev")
	if !db.ToBool(row["verified"]) {
		t.Fatal("user not verified")
	}

	// Garbage token rejected.
	rec, _ = postJSON(t, h, "/api/collections/users/confirm-verification",
		map[string]string{"token": "garbage"}, nil)
	if rec.Code != 400 {
		t.Fatalf("garbage token = %d", rec.Code)
	}
}

func TestPasswordResetFlow(t *testing.T) {
	app, h := newTestApp(t)
	createUser(t, app, "resetme@x.dev", "old-password-123", true)

	// Grab a token before reset to prove invalidation later.
	_, loginOut := postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "resetme@x.dev", "password": "old-password-123"}, nil)
	oldToken := loginOut["token"].(string)

	rec, _ := postJSON(t, h, "/api/collections/users/request-password-reset",
		map[string]string{"email": "resetme@x.dev"}, nil)
	if rec.Code != 204 {
		t.Fatalf("request-reset = %d", rec.Code)
	}
	tok := waitForMailToken(t, "resetToken")

	// Mismatched confirm rejected.
	rec, _ = postJSON(t, h, "/api/collections/users/confirm-password-reset",
		map[string]string{"token": tok, "password": "new-password-456", "passwordConfirm": "nope"}, nil)
	if rec.Code != 400 {
		t.Fatalf("mismatch = %d", rec.Code)
	}
	// Too short rejected.
	rec, _ = postJSON(t, h, "/api/collections/users/confirm-password-reset",
		map[string]string{"token": tok, "password": "short", "passwordConfirm": "short"}, nil)
	if rec.Code != 400 {
		t.Fatalf("short = %d", rec.Code)
	}

	rec, _ = postJSON(t, h, "/api/collections/users/confirm-password-reset",
		map[string]string{"token": tok, "password": "new-password-456", "passwordConfirm": "new-password-456"}, nil)
	if rec.Code != 204 {
		t.Fatalf("confirm-reset = %d: %s", rec.Code, rec.Body.String())
	}

	// Old password dead, new works.
	rec, _ = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "resetme@x.dev", "password": "old-password-123"}, nil)
	if rec.Code != 400 {
		t.Fatalf("old password still valid = %d", rec.Code)
	}
	rec, _ = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "resetme@x.dev", "password": "new-password-456"}, nil)
	if rec.Code != 200 {
		t.Fatalf("new password = %d", rec.Code)
	}

	// Old auth token invalidated by tokenKey rotation.
	rec, _ = postJSON(t, h, "/api/collections/users/auth-refresh", nil,
		map[string]string{"Authorization": "Bearer " + oldToken})
	if rec.Code != 401 {
		t.Fatalf("stale token after reset = %d", rec.Code)
	}
	// Reset token single-use (tokenKey rotated).
	rec, _ = postJSON(t, h, "/api/collections/users/confirm-password-reset",
		map[string]string{"token": tok, "password": "another-pass-789", "passwordConfirm": "another-pass-789"}, nil)
	if rec.Code != 400 {
		t.Fatalf("reset token reuse = %d", rec.Code)
	}
}

func TestMFAFlow(t *testing.T) {
	app, h := newTestApp(t)
	createUser(t, app, "mfa@x.dev", "some-long-password", true)

	_, out := postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "mfa@x.dev", "password": "some-long-password"}, nil)
	tok := out["token"].(string)
	authHeader := map[string]string{"Authorization": "Bearer " + tok}

	// Setup + activate.
	rec, out := postJSON(t, h, "/api/mfa/setup", nil, authHeader)
	if rec.Code != 200 {
		t.Fatalf("mfa setup = %d: %s", rec.Code, rec.Body.String())
	}
	secret := out["secret"].(string)
	if !strings.Contains(out["otpauthURL"].(string), "otpauth://totp/") {
		t.Fatalf("otpauth url = %v", out["otpauthURL"])
	}
	code, _ := totpCode(secret, uint64(time.Now().Unix()/30))
	rec, _ = postJSON(t, h, "/api/mfa/activate", map[string]string{"code": code}, authHeader)
	if rec.Code != 204 {
		t.Fatalf("mfa activate = %d: %s", rec.Code, rec.Body.String())
	}

	// Login now requires the second factor.
	rec, out = postJSON(t, h, "/api/collections/users/auth-with-password",
		map[string]string{"identity": "mfa@x.dev", "password": "some-long-password"}, nil)
	if rec.Code != 401 || out["mfaRequired"] != true {
		t.Fatalf("mfa challenge = %d %v", rec.Code, out)
	}
	mfaToken := out["mfaToken"].(string)

	// Wrong code rejected, right code completes.
	rec, _ = postJSON(t, h, "/api/mfa/verify", map[string]string{"mfaToken": mfaToken, "code": "000000"}, nil)
	if rec.Code != 400 {
		t.Fatalf("wrong code = %d", rec.Code)
	}
	code, _ = totpCode(secret, uint64(time.Now().Unix()/30))
	rec, out = postJSON(t, h, "/api/mfa/verify", map[string]string{"mfaToken": mfaToken, "code": code}, nil)
	if rec.Code != 200 || out["token"] == "" {
		t.Fatalf("mfa verify = %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTOTPPrimitive(t *testing.T) {
	secret := NewTOTPSecret()
	now := time.Now()
	code, err := totpCode(secret, uint64(now.Unix()/30))
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyTOTP(secret, code, now) {
		t.Fatal("valid code rejected")
	}
	if VerifyTOTP(secret, "123456", now) && code != "123456" {
		t.Fatal("bogus code accepted")
	}
	// previous window accepted (clock skew)
	prev, _ := totpCode(secret, uint64(now.Unix()/30)-1)
	if !VerifyTOTP(secret, prev, now) {
		t.Fatal("previous window rejected")
	}
}

func TestAuthMethodsEndpoint(t *testing.T) {
	_, h := newTestApp(t)
	req := httptest.NewRequest("GET", "/api/collections/users/auth-methods", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("auth-methods = %d", rec.Code)
	}
	var out map[string]any
	json.Unmarshal(rec.Body.Bytes(), &out)
	if out["password"].(map[string]any)["enabled"] != true {
		t.Fatalf("methods = %v", out)
	}
}

// waitForMailToken polls the captured log-mailer messages for a link token
// (emails are sent from goroutines).
func waitForMailToken(t *testing.T, param string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		for _, m := range mail.SentMessages() {
			if idx := strings.Index(m.Text, param+"="); idx >= 0 {
				rest := m.Text[idx+len(param)+1:]
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
	for i, m := range mail.SentMessages() {
		fmt.Printf("mail[%d]: %s\n%s\n", i, m.Subject, m.Text)
	}
	t.Fatalf("no mail with %s captured", param)
	return ""
}
