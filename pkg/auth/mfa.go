package auth

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
	"github.com/myfoxit/goforge/pkg/token"
)

// MFACollection stores TOTP enrollments.
const MFACollection = "_mfa"

// MFAModule adds TOTP two-factor authentication ("mfa").
type MFAModule struct{}

func (MFAModule) ID() string { return "mfa" }

func (MFAModule) Register(app *core.App) error {
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		if e.App.Schema().Get(MFACollection) != nil {
			return nil
		}
		return e.App.Schema().Save(context.Background(), &schema.Collection{
			Name: MFACollection, Type: schema.TypeBase, System: true,
			Fields: []*schema.Field{
				{Name: "collection", Type: schema.FieldText, Required: true, System: true},
				{Name: "recordId", Type: schema.FieldText, Required: true, System: true},
				{Name: "secret", Type: schema.FieldText, Required: true, System: true, Hidden: true},
				{Name: "enabled", Type: schema.FieldBool, System: true},
			},
			Indexes: []schema.Index{{
				Name: "ux_mfa_record", Columns: []string{"collection", "recordId"}, Unique: true,
			}},
		})
	})

	limit := app.RateLimit(5, 15)
	mux := app.Mux()
	mux.HandleFunc("POST /api/mfa/setup", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		mfaSetup(app, w, r)
	}))
	mux.HandleFunc("POST /api/mfa/activate", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		mfaActivate(app, w, r)
	}))
	mux.HandleFunc("POST /api/mfa/disable", app.RequireAuth(func(w http.ResponseWriter, r *http.Request) {
		mfaDisable(app, w, r)
	}))
	mux.HandleFunc("POST /api/mfa/verify", limit(func(w http.ResponseWriter, r *http.Request) {
		mfaVerify(app, w, r)
	}))
	return nil
}

func mfaRow(app *core.App, collection, recordID string) map[string]any {
	if app.Schema().Get(MFACollection) == nil {
		return nil
	}
	q := app.DB().Dialect.Quote
	row, err := app.DB().QueryMap(context.Background(), fmt.Sprintf(
		"SELECT * FROM %s WHERE %s = ? AND %s = ? LIMIT 1",
		q(MFACollection), q("collection"), q("recordId")), collection, recordID)
	if err != nil {
		return nil
	}
	return row
}

// HasActiveMFA reports whether the record completed TOTP enrollment.
func HasActiveMFA(app *core.App, collection, recordID string) bool {
	if !app.HasModule("mfa") {
		return false
	}
	row := mfaRow(app, collection, recordID)
	return row != nil && db.ToBool(row["enabled"])
}

func mfaSetup(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	if auth.Collection == nil {
		core.WriteError(w, app.Log(), core.Forbidden(""))
		return
	}
	secret := NewTOTPSecret()
	d := app.DB()
	q := d.Dialect.Quote
	now := db.Now()

	if row := mfaRow(app, auth.Collection.Name, auth.ID()); row != nil {
		if db.ToBool(row["enabled"]) {
			core.WriteError(w, app.Log(), core.BadRequest("MFA is already active. Disable it first."))
			return
		}
		if _, err := d.Exec(r.Context(), fmt.Sprintf(
			"UPDATE %s SET secret = ?, updated = ? WHERE id = ?", q(MFACollection)),
			secret, now, row["id"]); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
	} else {
		if _, err := d.Exec(r.Context(), fmt.Sprintf(
			"INSERT INTO %s (id, created, updated, %s, %s, secret, enabled) VALUES (?, ?, ?, ?, ?, ?, ?)",
			q(MFACollection), q("collection"), q("recordId")),
			security.RandomID(15), now, now, auth.Collection.Name, auth.ID(), secret, false); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
	}
	core.WriteJSON(w, http.StatusOK, map[string]any{
		"secret":     secret,
		"otpauthURL": TOTPURL(app.AppName(), db.ToString(auth.Record["email"]), secret),
	})
}

func mfaActivate(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	var body struct {
		Code string `json:"code"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	row := mfaRow(app, auth.Collection.Name, auth.ID())
	if row == nil {
		core.WriteError(w, app.Log(), core.BadRequest("Run /api/mfa/setup first."))
		return
	}
	if !VerifyTOTP(db.ToString(row["secret"]), body.Code, time.Now()) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid code."))
		return
	}
	q := app.DB().Dialect.Quote
	if _, err := app.DB().Exec(r.Context(), fmt.Sprintf(
		"UPDATE %s SET enabled = ?, updated = ? WHERE id = ?", q(MFACollection)),
		true, db.Now(), row["id"]); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func mfaDisable(app *core.App, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	var body struct {
		Password string `json:"password"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if !security.VerifyPassword(db.ToString(auth.Record["password"]), body.Password) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid password."))
		return
	}
	q := app.DB().Dialect.Quote
	if _, err := app.DB().Exec(r.Context(), fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ? AND %s = ?", q(MFACollection), q("collection"), q("recordId")),
		auth.Collection.Name, auth.ID()); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mfaVerify completes a password login that answered with mfaRequired.
func mfaVerify(app *core.App, w http.ResponseWriter, r *http.Request) {
	var body struct {
		MFAToken string `json:"mfaToken"`
		Code     string `json:"code"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	c, record, err := verifyPurposeToken(app, body.MFAToken, token.TypeMFA)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	row := mfaRow(app, c.Name, db.ToString(record["id"]))
	if row == nil || !db.ToBool(row["enabled"]) {
		core.WriteError(w, app.Log(), core.BadRequest("MFA is not active for this account."))
		return
	}
	if !VerifyTOTP(db.ToString(row["secret"]), body.Code, time.Now()) {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid code."))
		return
	}
	app.OnAuth.Trigger(&core.AuthEvent{App: app, Collection: c, Record: record, Method: "password+totp", Request: r})
	AuthResponse(app, w, c, record)
}
