// Package ldap adds LDAP / Active Directory authentication: users bind with
// their directory credentials and are provisioned as local auth records on
// first login.
package ldap

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"strings"

	goldap "github.com/go-ldap/ldap/v3"
	"github.com/myfoxit/goforge/pkg/auth"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// Module wires LDAP login ("ldap").
type Module struct{}

func (Module) ID() string { return "ldap" }

func (Module) Register(app *core.App) error {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "ldap", Title: "LDAP / Active Directory", Order: 12,
		Fields: []core.SettingsField{
			{Key: "ldap.enabled", Label: "Enabled", Type: "bool", Default: false},
			{Key: "ldap.url", Label: "Server URL", Type: "text", Placeholder: "ldaps://ldap.example.com:636"},
			{Key: "ldap.skipTLSVerify", Label: "Skip TLS verification (testing only!)", Type: "bool", Default: false},
			{Key: "ldap.bindDN", Label: "Service bind DN", Type: "text", Placeholder: "cn=readonly,dc=example,dc=com",
				Help: "Leave empty for anonymous search or direct user binds."},
			{Key: "ldap.bindPassword", Label: "Service bind password", Type: "secret"},
			{Key: "ldap.baseDN", Label: "User search base", Type: "text", Placeholder: "ou=people,dc=example,dc=com"},
			{Key: "ldap.userFilter", Label: "User filter", Type: "text", Default: "(|(uid=%s)(mail=%s))",
				Help: "%s is replaced with the login name."},
			{Key: "ldap.emailAttr", Label: "Email attribute", Type: "text", Default: "mail"},
			{Key: "ldap.nameAttr", Label: "Display name attribute", Type: "text", Default: "cn"},
		},
	})

	limit := app.RateLimit(3, 10)
	app.Mux().HandleFunc("POST /api/auth/ldap", limit(func(w http.ResponseWriter, r *http.Request) {
		login(app, w, r)
	}))
	return nil
}

func login(app *core.App, w http.ResponseWriter, r *http.Request) {
	s := app.Settings()
	if !s.Bool("ldap.enabled") {
		core.WriteError(w, app.Log(), core.NotFound("LDAP login is not enabled."))
		return
	}
	var body struct {
		Collection string `json:"collection"`
		Username   string `json:"username"`
		Password   string `json:"password"`
	}
	if err := core.ReadJSON(r, &body); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	if body.Username == "" || body.Password == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Missing username or password."))
		return
	}
	if body.Collection == "" {
		body.Collection = auth.UsersCollection
	}
	c, err := auth.AuthCollection(app, body.Collection)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}

	user, err := authenticate(app, body.Username, body.Password)
	if err != nil {
		app.Log().Info("ldap login failed", "user", body.Username, "err", err)
		core.WriteError(w, app.Log(), core.BadRequest("Invalid credentials."))
		return
	}

	record, err := findOrProvision(app, c, user)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	app.OnAuth.Trigger(&core.AuthEvent{App: app, Collection: c, Record: record, Method: "ldap", Request: r})
	auth.AuthResponse(app, w, c, record)
}

type ldapUser struct {
	DN    string
	Email string
	Name  string
}

// authenticate performs (optional service-bind +) search + user bind.
func authenticate(app *core.App, username, password string) (*ldapUser, error) {
	s := app.Settings()
	url := s.String("ldap.url")
	if url == "" {
		return nil, fmt.Errorf("ldap url not configured")
	}
	opts := []goldap.DialOpt{}
	if s.Bool("ldap.skipTLSVerify") {
		opts = append(opts, goldap.DialWithTLSConfig(&tls.Config{InsecureSkipVerify: true}))
	}
	conn, err := goldap.DialURL(url, opts...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if bindDN := s.String("ldap.bindDN"); bindDN != "" {
		if err := conn.Bind(bindDN, s.String("ldap.bindPassword")); err != nil {
			return nil, fmt.Errorf("service bind: %w", err)
		}
	}

	filter := s.String("ldap.userFilter")
	escaped := goldap.EscapeFilter(username)
	filter = strings.ReplaceAll(filter, "%s", escaped)
	emailAttr := s.String("ldap.emailAttr")
	nameAttr := s.String("ldap.nameAttr")

	search := goldap.NewSearchRequest(
		s.String("ldap.baseDN"),
		goldap.ScopeWholeSubtree, goldap.NeverDerefAliases, 2, 10, false,
		filter,
		[]string{"dn", emailAttr, nameAttr},
		nil,
	)
	result, err := conn.Search(search)
	if err != nil {
		return nil, err
	}
	if len(result.Entries) != 1 {
		return nil, fmt.Errorf("user not found (or ambiguous: %d entries)", len(result.Entries))
	}
	entry := result.Entries[0]

	// The actual credential check: bind as the user.
	if err := conn.Bind(entry.DN, password); err != nil {
		return nil, fmt.Errorf("user bind failed")
	}

	email := strings.ToLower(entry.GetAttributeValue(emailAttr))
	if email == "" {
		return nil, fmt.Errorf("directory entry has no %s attribute", emailAttr)
	}
	return &ldapUser{DN: entry.DN, Email: email, Name: entry.GetAttributeValue(nameAttr)}, nil
}

// findOrProvision maps the directory identity onto a local auth record.
func findOrProvision(app *core.App, c *schema.Collection, user *ldapUser) (map[string]any, error) {
	ctx := context.Background()
	record, err := app.FindFirstRecord(ctx, c.Name, "email", user.Email)
	if err != nil {
		return nil, err
	}
	if record != nil {
		return record, nil
	}
	hash, err := security.HashPassword(security.RandomToken(24))
	if err != nil {
		return nil, err
	}
	q := app.DB().Dialect.Quote
	id := security.RandomID(15)
	now := db.Now()
	_, err = app.DB().Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, email, password, %s, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		q(c.Name), q("tokenKey")),
		id, now, now, user.Email, hash, security.RandomToken(24), true, user.Name)
	if err != nil {
		return nil, fmt.Errorf("ldap provisioning failed: %w", err)
	}
	record, err = app.FindRecordByID(ctx, c.Name, id)
	if err == nil && record != nil {
		app.OnRecordAfterCreate.Trigger(&core.RecordEvent{App: app, Action: "create", Collection: c, Record: record})
	}
	return record, err
}
