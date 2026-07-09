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
	"github.com/myfoxit/goforge/pkg/security"
	"github.com/myfoxit/goforge/pkg/token"
)

func newTokenKey() string { return security.RandomToken(24) }

// newPurposeToken issues a single-purpose token bound to the record's
// tokenKey (so it dies when the password changes).
func newPurposeToken(app *core.App, c *schema.Collection, record map[string]any, typ string, ttl time.Duration) (string, error) {
	return token.Sign(app.Secret(), typ, token.Claims{
		"sub":   db.ToString(record["id"]),
		"col":   c.Name,
		"email": db.ToString(record["email"]),
		"rk":    security.HashToken(db.ToString(record["tokenKey"]))[:16],
	}, ttl)
}

// verifyPurposeToken validates a purpose token and loads its record.
func verifyPurposeToken(app *core.App, raw, typ string) (*schema.Collection, map[string]any, error) {
	claims, err := token.Verify(app.Secret(), raw, typ)
	if err != nil {
		return nil, nil, core.BadRequest("Invalid or expired token.")
	}
	c, err := AuthCollection(app, claims.String("col"))
	if err != nil {
		return nil, nil, err
	}
	record, err := app.FindRecordByID(context.Background(), c.Name, claims.Subject())
	if err != nil || record == nil {
		return nil, nil, core.BadRequest("Invalid or expired token.")
	}
	rk := security.HashToken(db.ToString(record["tokenKey"]))[:16]
	if !security.Equal(rk, claims.String("rk")) {
		return nil, nil, core.BadRequest("Invalid or expired token.")
	}
	return c, record, nil
}

func registerRoutes(app *core.App) {
	limit := app.RateLimit(5, 20) // per-IP: sustained 5 rps, burst 20
	mux := app.Mux()

	mux.HandleFunc("POST /api/collections/{collection}/auth-with-password", limit(func(w http.ResponseWriter, r *http.Request) {
		authWithPassword(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/auth-refresh", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		authRefresh(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/request-verification", limit(func(w http.ResponseWriter, r *http.Request) {
		requestVerification(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/confirm-verification", limit(func(w http.ResponseWriter, r *http.Request) {
		confirmVerification(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/request-password-reset", limit(func(w http.ResponseWriter, r *http.Request) {
		requestPasswordReset(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/confirm-password-reset", limit(func(w http.ResponseWriter, r *http.Request) {
		confirmPasswordReset(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/request-email-change", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		requestEmailChange(app, w, r)
	}))
	mux.HandleFunc("POST /api/collections/{collection}/confirm-email-change", limit(func(w http.ResponseWriter, r *http.Request) {
		confirmEmailChange(app, w, r)
	}))
	mux.HandleFunc("GET /api/collections/{collection}/auth-methods", func(w http.ResponseWriter, r *http.Request) {
		authMethods(app, w, r)
	})
}

func authWithPassword(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	var body struct {
		Identity string `json:"identity"`
		Email    string `json:"email"` // convenience alias
		Password string `json:"password"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if body.Identity == "" {
		body.Identity = body.Email
	}
	if body.Identity == "" || body.Password == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Missing identity or password."))
		return
	}

	record, err := fetchByIdentity(app, c, body.Identity)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	// Constant-shape failure: verify against a dummy hash when unknown.
	if record == nil {
		security.VerifyPassword("$argon2id$v=19$m=65536,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA", body.Password)
		core.WriteError(w, app.Log(), core.BadRequest("Invalid credentials."))
		return
	}
	if !security.VerifyPassword(db.ToString(record["password"]), body.Password) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid credentials."))
		return
	}
	if c.AuthOptions().OnlyVerified && !db.ToBool(record["verified"]) {
		core.WriteError(w, app.Log(), core.Forbidden("Please verify your email address first."))
		return
	}

	// MFA challenge?
	if HasActiveMFA(app, c.Name, db.ToString(record["id"])) {
		mfaTok, err := newPurposeToken(app, c, record, token.TypeMFA, 5*time.Minute)
		if err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		core.WriteJSON(w, http.StatusUnauthorized, map[string]any{
			"mfaRequired": true,
			"mfaToken":    mfaTok,
		})
		return
	}

	app.OnAuth.Trigger(&core.AuthEvent{App: app, Collection: c, Record: record, Method: "password", Request: r})
	AuthResponse(app, w, c, record)
}

func authRefresh(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	auth := core.AuthFromContext(r.Context())
	if auth.Collection == nil || auth.Collection.Name != c.Name {
		core.WriteError(w, app.Log(), core.Forbidden("Token does not belong to this collection."))
		return
	}
	AuthResponse(app, w, c, auth.Record)
}

func requestVerification(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := core.ReadJSON(r, &body); err != nil || body.Email == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Missing email."))
		return
	}
	record, _ := fetchByIdentity(app, c, body.Email)
	if record != nil && !db.ToBool(record["verified"]) {
		go sendVerificationMail(app, c, record)
	}
	w.WriteHeader(http.StatusNoContent) // no account enumeration
}

func confirmVerification(app *core.App, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := core.ReadJSON(r, &body); err != nil || body.Token == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Missing token."))
		return
	}
	c, record, err := verifyPurposeToken(app, body.Token, token.TypeVerification)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	q := app.DB().Dialect.Quote
	if _, err := app.DB().Exec(r.Context(),
		fmt.Sprintf("UPDATE %s SET verified = ?, updated = ? WHERE id = ?", q(c.Name)),
		true, db.Now(), record["id"]); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestPasswordReset(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	var body struct {
		Email string `json:"email"`
	}
	if err := core.ReadJSON(r, &body); err != nil || body.Email == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Missing email."))
		return
	}
	record, _ := fetchByIdentity(app, c, body.Email)
	if record != nil {
		go func() {
			tok, err := newPurposeToken(app, c, record, token.TypePasswordReset, 30*time.Minute)
			if err != nil {
				return
			}
			loginPath := app.Settings().String("auth.appLoginURL")
			action := appActionURL(app, loginPath, map[string]string{"resetToken": tok})
			mail.SendTemplate(context.Background(), app, mail.Address{
				Email: db.ToString(record["email"]), Name: db.ToString(record["name"]),
			}, "password-reset", mail.TemplateData{
				ActionURL: action,
				Name:      db.ToString(record["name"]),
			})
		}()
	}
	w.WriteHeader(http.StatusNoContent)
}

func confirmPasswordReset(app *core.App, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token           string `json:"token"`
		Password        string `json:"password"`
		PasswordConfirm string `json:"passwordConfirm"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if body.Password == "" || body.Password != body.PasswordConfirm {
		core.WriteError(w, app.Log(), core.ValidationError("passwordConfirm", "Passwords do not match."))
		return
	}
	c, record, err := verifyPurposeToken(app, body.Token, token.TypePasswordReset)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if len(body.Password) < c.AuthOptions().MinPasswordLength {
		core.WriteError(w, app.Log(), core.ValidationError("password",
			fmt.Sprintf("Password must be at least %d characters.", c.AuthOptions().MinPasswordLength)))
		return
	}
	hash, err := security.HashPassword(body.Password)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	q := app.DB().Dialect.Quote
	// Rotating tokenKey invalidates all sessions and outstanding tokens.
	if _, err := app.DB().Exec(r.Context(),
		fmt.Sprintf("UPDATE %s SET password = ?, %s = ?, updated = ? WHERE id = ?", q(c.Name), q("tokenKey")),
		hash, newTokenKey(), db.Now(), record["id"]); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func requestEmailChange(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	auth := core.AuthFromContext(r.Context())
	if auth.Collection == nil || auth.Collection.Name != c.Name {
		core.WriteError(w, app.Log(), core.Forbidden(""))
		return
	}
	var body struct {
		NewEmail string `json:"newEmail"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	f := schema.Field{Name: "email", Type: schema.FieldEmail, Required: true}
	norm, err := f.NormalizeValue(body.NewEmail)
	if err != nil {
		core.WriteError(w, app.Log(), core.ValidationError("newEmail", "Invalid email address."))
		return
	}
	newEmail := db.ToString(norm)
	if existing, _ := app.FindFirstRecord(r.Context(), c.Name, "email", newEmail); existing != nil {
		core.WriteError(w, app.Log(), core.ValidationError("newEmail", "Email is already in use."))
		return
	}
	tok, err := token.Sign(app.Secret(), token.TypeEmailChange, token.Claims{
		"sub":      auth.ID(),
		"col":      c.Name,
		"newEmail": newEmail,
		"rk":       security.HashToken(db.ToString(auth.Record["tokenKey"]))[:16],
	}, 30*time.Minute)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	loginPath := app.Settings().String("auth.appLoginURL")
	action := appActionURL(app, loginPath, map[string]string{"emailChangeToken": tok})
	go mail.SendTemplate(context.Background(), app, mail.Address{Email: newEmail},
		"email-change", mail.TemplateData{ActionURL: action, Name: db.ToString(auth.Record["name"])})
	w.WriteHeader(http.StatusNoContent)
}

func confirmEmailChange(app *core.App, w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	claims, err := token.Verify(app.Secret(), body.Token, token.TypeEmailChange)
	if err != nil {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid or expired token."))
		return
	}
	c, err := AuthCollection(app, claims.String("col"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	record, err := app.FindRecordByID(r.Context(), c.Name, claims.Subject())
	if err != nil || record == nil {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid or expired token."))
		return
	}
	rk := security.HashToken(db.ToString(record["tokenKey"]))[:16]
	if !security.Equal(rk, claims.String("rk")) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid or expired token."))
		return
	}
	if !security.VerifyPassword(db.ToString(record["password"]), body.Password) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid password."))
		return
	}
	q := app.DB().Dialect.Quote
	if _, err := app.DB().Exec(r.Context(),
		fmt.Sprintf("UPDATE %s SET email = ?, %s = ?, updated = ? WHERE id = ?", q(c.Name), q("tokenKey")),
		claims.String("newEmail"), newTokenKey(), db.Now(), record["id"]); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func authMethods(app *core.App, w http.ResponseWriter, r *http.Request) {
	c, err := AuthCollection(app, r.PathValue("collection"))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	opts := c.AuthOptions()
	methods := map[string]any{
		"password": map[string]any{
			"enabled":        true,
			"identityFields": opts.IdentityFields,
		},
		"oauth2": map[string]any{
			"enabled":   false,
			"providers": []any{},
		},
		"mfa": map[string]any{"enabled": app.HasModule("mfa")},
	}
	if providers := EnabledOAuthProviders(app); len(providers) > 0 {
		list := make([]map[string]any, 0, len(providers))
		for _, p := range providers {
			list = append(list, map[string]any{
				"name":        p.Name,
				"displayName": p.DisplayName,
				"authURL":     fmt.Sprintf("%s/api/oauth2/%s/%s", app.BaseURL(), c.Name, p.Name),
			})
		}
		methods["oauth2"] = map[string]any{"enabled": true, "providers": list}
	}
	core.WriteJSON(w, http.StatusOK, methods)
}
