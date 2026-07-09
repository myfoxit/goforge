package auth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
	"github.com/myfoxit/goforge/pkg/token"
)

// ExternalAuthsCollection links provider identities to auth records.
const ExternalAuthsCollection = "_externalAuths"

// OAuthUser is the normalized identity returned by a provider.
type OAuthUser struct {
	ID       string
	Email    string
	Verified bool
	Name     string
	Avatar   string
}

// Provider describes an OAuth2 / OIDC provider.
type Provider struct {
	Name        string
	DisplayName string
	AuthURL     string
	TokenURL    string
	UserInfoURL string
	Scopes      []string
	PKCE        bool
	// ExtraAuthParams appended to the authorization redirect.
	ExtraAuthParams map[string]string
	// MapUser converts the raw userinfo payload.
	MapUser func(raw map[string]any, client *http.Client, accessToken string) (OAuthUser, error)
}

func strOf(m map[string]any, key string) string { return db.ToString(m[key]) }

// providerCatalog holds the built-in providers.
var providerCatalog = map[string]Provider{
	"google": {
		Name: "google", DisplayName: "Google",
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		UserInfoURL: "https://openidconnect.googleapis.com/v1/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		PKCE:        true,
		MapUser: func(raw map[string]any, _ *http.Client, _ string) (OAuthUser, error) {
			return OAuthUser{
				ID: strOf(raw, "sub"), Email: strOf(raw, "email"),
				Verified: db.ToBool(raw["email_verified"]),
				Name:     strOf(raw, "name"), Avatar: strOf(raw, "picture"),
			}, nil
		},
	},
	"github": {
		Name: "github", DisplayName: "GitHub",
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		UserInfoURL: "https://api.github.com/user",
		Scopes:      []string{"read:user", "user:email"},
		PKCE:        false,
		MapUser: func(raw map[string]any, client *http.Client, accessToken string) (OAuthUser, error) {
			u := OAuthUser{
				ID:     strOf(raw, "id"),
				Email:  strOf(raw, "email"),
				Name:   firstNonEmpty(strOf(raw, "name"), strOf(raw, "login")),
				Avatar: strOf(raw, "avatar_url"),
			}
			if u.Email == "" { // fetch primary verified email
				req, _ := http.NewRequest("GET", "https://api.github.com/user/emails", nil)
				req.Header.Set("Authorization", "Bearer "+accessToken)
				resp, err := client.Do(req)
				if err == nil {
					defer resp.Body.Close()
					var emails []struct {
						Email    string `json:"email"`
						Primary  bool   `json:"primary"`
						Verified bool   `json:"verified"`
					}
					if json.NewDecoder(resp.Body).Decode(&emails) == nil {
						for _, e := range emails {
							if e.Primary && e.Verified {
								u.Email, u.Verified = e.Email, true
								break
							}
						}
					}
				}
			} else {
				u.Verified = true
			}
			return u, nil
		},
	},
	"microsoft": {
		Name: "microsoft", DisplayName: "Microsoft",
		AuthURL:     "https://login.microsoftonline.com/common/oauth2/v2.0/authorize",
		TokenURL:    "https://login.microsoftonline.com/common/oauth2/v2.0/token",
		UserInfoURL: "https://graph.microsoft.com/oidc/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		PKCE:        true,
		MapUser: func(raw map[string]any, _ *http.Client, _ string) (OAuthUser, error) {
			return OAuthUser{
				ID: strOf(raw, "sub"), Email: strOf(raw, "email"),
				Verified: strOf(raw, "email") != "",
				Name:     strOf(raw, "name"), Avatar: strOf(raw, "picture"),
			}, nil
		},
	},
	"gitlab": {
		Name: "gitlab", DisplayName: "GitLab",
		AuthURL:     "https://gitlab.com/oauth/authorize",
		TokenURL:    "https://gitlab.com/oauth/token",
		UserInfoURL: "https://gitlab.com/oauth/userinfo",
		Scopes:      []string{"openid", "email", "profile"},
		PKCE:        true,
		MapUser: func(raw map[string]any, _ *http.Client, _ string) (OAuthUser, error) {
			return OAuthUser{
				ID: strOf(raw, "sub"), Email: strOf(raw, "email"),
				Verified: db.ToBool(raw["email_verified"]),
				Name:     strOf(raw, "name"), Avatar: strOf(raw, "picture"),
			}, nil
		},
	},
	"discord": {
		Name: "discord", DisplayName: "Discord",
		AuthURL:     "https://discord.com/oauth2/authorize",
		TokenURL:    "https://discord.com/api/oauth2/token",
		UserInfoURL: "https://discord.com/api/users/@me",
		Scopes:      []string{"identify", "email"},
		PKCE:        true,
		MapUser: func(raw map[string]any, _ *http.Client, _ string) (OAuthUser, error) {
			u := OAuthUser{
				ID: strOf(raw, "id"), Email: strOf(raw, "email"),
				Verified: db.ToBool(raw["verified"]),
				Name:     firstNonEmpty(strOf(raw, "global_name"), strOf(raw, "username")),
			}
			if av := strOf(raw, "avatar"); av != "" && u.ID != "" {
				u.Avatar = fmt.Sprintf("https://cdn.discordapp.com/avatars/%s/%s.png", u.ID, av)
			}
			return u, nil
		},
	},
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// OAuthModule enables OAuth2/OIDC sign-in ("oauth").
type OAuthModule struct{}

func (OAuthModule) ID() string { return "oauth" }

func (OAuthModule) Register(app *core.App) error {
	fields := []core.SettingsField{}
	for _, name := range []string{"google", "github", "microsoft", "gitlab", "discord"} {
		p := providerCatalog[name]
		fields = append(fields,
			core.SettingsField{Key: "oauth." + name + ".enabled", Label: p.DisplayName + " enabled", Type: "bool", Default: false},
			core.SettingsField{Key: "oauth." + name + ".clientId", Label: p.DisplayName + " client id", Type: "text"},
			core.SettingsField{Key: "oauth." + name + ".clientSecret", Label: p.DisplayName + " client secret", Type: "secret"},
		)
	}
	fields = append(fields,
		core.SettingsField{Key: "oauth.oidc.enabled", Label: "Generic OIDC enabled", Type: "bool", Default: false},
		core.SettingsField{Key: "oauth.oidc.displayName", Label: "OIDC display name", Type: "text", Default: "SSO"},
		core.SettingsField{Key: "oauth.oidc.issuer", Label: "OIDC issuer URL", Type: "text",
			Help: "Endpoints are discovered from <issuer>/.well-known/openid-configuration."},
		core.SettingsField{Key: "oauth.oidc.clientId", Label: "OIDC client id", Type: "text"},
		core.SettingsField{Key: "oauth.oidc.clientSecret", Label: "OIDC client secret", Type: "secret"},
	)
	app.Settings().RegisterSection(core.SettingsSection{ID: "oauth", Title: "OAuth / SSO", Order: 11, Fields: fields})

	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		return ensureExternalAuths(e.App)
	})

	app.Mux().HandleFunc("GET /api/oauth2/{collection}/{provider}", func(w http.ResponseWriter, r *http.Request) {
		oauthRedirect(app, w, r)
	})
	app.Mux().HandleFunc("GET /api/oauth2/callback", func(w http.ResponseWriter, r *http.Request) {
		oauthCallback(app, w, r)
	})
	return nil
}

func ensureExternalAuths(app *core.App) error {
	if app.Schema().Get(ExternalAuthsCollection) != nil {
		return nil
	}
	return app.Schema().Save(context.Background(), &schema.Collection{
		Name: ExternalAuthsCollection, Type: schema.TypeBase, System: true,
		Fields: []*schema.Field{
			{Name: "collection", Type: schema.FieldText, Required: true, System: true},
			{Name: "recordId", Type: schema.FieldText, Required: true, System: true},
			{Name: "provider", Type: schema.FieldText, Required: true, System: true},
			{Name: "providerId", Type: schema.FieldText, Required: true, System: true},
		},
		Indexes: []schema.Index{{
			Name: "ux_externalauths_key", Columns: []string{"provider", "providerId", "collection"}, Unique: true,
		}},
	})
}

// EnabledOAuthProviders returns providers switched on in settings.
func EnabledOAuthProviders(app *core.App) []Provider {
	if !app.HasModule("oauth") {
		return nil
	}
	s := app.Settings()
	var out []Provider
	for _, name := range []string{"google", "github", "microsoft", "gitlab", "discord"} {
		if s.Bool("oauth."+name+".enabled") && s.String("oauth."+name+".clientId") != "" {
			out = append(out, providerCatalog[name])
		}
	}
	if s.Bool("oauth.oidc.enabled") && s.String("oauth.oidc.issuer") != "" {
		if p, err := discoverOIDC(app, s.String("oauth.oidc.issuer"), s.String("oauth.oidc.displayName")); err == nil {
			out = append(out, p)
		} else {
			app.Log().Warn("oidc discovery failed", "err", err)
		}
	}
	return out
}

func lookupProvider(app *core.App, name string) (Provider, bool) {
	for _, p := range EnabledOAuthProviders(app) {
		if p.Name == name {
			return p, true
		}
	}
	return Provider{}, false
}

var oidcCache = struct {
	issuer string
	p      Provider
	at     time.Time
}{}

// discoverOIDC loads endpoints from the issuer discovery document (cached).
func discoverOIDC(app *core.App, issuer, displayName string) (Provider, error) {
	if oidcCache.issuer == issuer && time.Since(oidcCache.at) < time.Hour {
		p := oidcCache.p
		p.DisplayName = firstNonEmpty(displayName, p.DisplayName)
		return p, nil
	}
	resp, err := httpClient().Get(strings.TrimSuffix(issuer, "/") + "/.well-known/openid-configuration")
	if err != nil {
		return Provider{}, err
	}
	defer resp.Body.Close()
	var doc struct {
		AuthorizationEndpoint string `json:"authorization_endpoint"`
		TokenEndpoint         string `json:"token_endpoint"`
		UserinfoEndpoint      string `json:"userinfo_endpoint"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&doc); err != nil {
		return Provider{}, err
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return Provider{}, fmt.Errorf("auth: incomplete oidc discovery document")
	}
	p := Provider{
		Name: "oidc", DisplayName: firstNonEmpty(displayName, "SSO"),
		AuthURL: doc.AuthorizationEndpoint, TokenURL: doc.TokenEndpoint,
		UserInfoURL: doc.UserinfoEndpoint,
		Scopes:      []string{"openid", "email", "profile"},
		PKCE:        true,
		MapUser: func(raw map[string]any, _ *http.Client, _ string) (OAuthUser, error) {
			return OAuthUser{
				ID: strOf(raw, "sub"), Email: strOf(raw, "email"),
				Verified: db.ToBool(raw["email_verified"]),
				Name:     firstNonEmpty(strOf(raw, "name"), strOf(raw, "preferred_username")),
				Avatar:   strOf(raw, "picture"),
			}, nil
		},
	}
	oidcCache.issuer, oidcCache.p, oidcCache.at = issuer, p, time.Now()
	return p, nil
}

func httpClient() *http.Client { return &http.Client{Timeout: 20 * time.Second} }

const oauthCookie = "gf_oauth"

func oauthRedirect(app *core.App, w http.ResponseWriter, r *http.Request) {
	colName := r.PathValue("collection")
	if _, err := AuthCollection(app, colName); err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	provider, ok := lookupProvider(app, r.PathValue("provider"))
	if !ok {
		core.WriteError(w, app.Log(), core.NotFound("Unknown or disabled provider."))
		return
	}
	redirect := r.URL.Query().Get("redirect")
	if redirect == "" || !strings.HasPrefix(redirect, "/") || strings.HasPrefix(redirect, "//") {
		redirect = "/"
	}

	verifier := security.RandomToken(32)
	challenge := base64.RawURLEncoding.EncodeToString(sha256Sum(verifier))
	state, err := token.Sign(app.Secret(), "oauthState", token.Claims{
		"col":      colName,
		"provider": provider.Name,
		"redirect": redirect,
		"cv":       security.HashToken(verifier)[:24],
	}, 10*time.Minute)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: oauthCookie, Value: verifier, Path: "/api/oauth2",
		MaxAge: 600, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(app.BaseURL(), "https://"),
	})

	q := url.Values{
		"client_id":     {app.Settings().String("oauth." + provider.Name + ".clientId")},
		"redirect_uri":  {app.BaseURL() + "/api/oauth2/callback"},
		"response_type": {"code"},
		"scope":         {strings.Join(provider.Scopes, " ")},
		"state":         {state},
	}
	if provider.PKCE {
		q.Set("code_challenge", challenge)
		q.Set("code_challenge_method", "S256")
	}
	for k, v := range provider.ExtraAuthParams {
		q.Set(k, v)
	}
	http.Redirect(w, r, provider.AuthURL+"?"+q.Encode(), http.StatusFound)
}

func sha256Sum(s string) []byte {
	sum := sha256.Sum256([]byte(s))
	return sum[:]
}

func oauthCallback(app *core.App, w http.ResponseWriter, r *http.Request) {
	fail := func(msg string) {
		http.Redirect(w, r, "/?oauthError="+url.QueryEscape(msg), http.StatusSeeOther)
	}
	q := r.URL.Query()
	if errMsg := q.Get("error"); errMsg != "" {
		fail(firstNonEmpty(q.Get("error_description"), errMsg))
		return
	}
	claims, err := token.Verify(app.Secret(), q.Get("state"), "oauthState")
	if err != nil {
		fail("Invalid or expired state.")
		return
	}
	cookie, err := r.Cookie(oauthCookie)
	if err != nil || !security.Equal(security.HashToken(cookie.Value)[:24], claims.String("cv")) {
		fail("Missing PKCE verifier — please retry the sign-in.")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: oauthCookie, Path: "/api/oauth2", MaxAge: -1})

	provider, ok := lookupProvider(app, claims.String("provider"))
	if !ok {
		fail("Provider is no longer enabled.")
		return
	}
	c, err := AuthCollection(app, claims.String("col"))
	if err != nil {
		fail("Unknown collection.")
		return
	}

	user, err := exchangeAndFetch(app, provider, q.Get("code"), cookie.Value)
	if err != nil {
		app.Log().Warn("oauth exchange failed", "provider", provider.Name, "err", err)
		fail("Sign-in with " + provider.DisplayName + " failed.")
		return
	}
	if user.ID == "" {
		fail("Provider returned no user id.")
		return
	}

	record, err := findOrCreateOAuthUser(app, c, provider, user)
	if err != nil {
		app.Log().Warn("oauth user resolution failed", "err", err)
		fail(err.Error())
		return
	}

	app.OnAuth.Trigger(&core.AuthEvent{App: app, Collection: c, Record: record, Method: "oauth2:" + provider.Name, Request: r})
	authToken, err := app.NewAuthToken(c, record, TokenTTL(app))
	if err != nil {
		fail("Token issue failed.")
		return
	}
	// Fragment keeps the token out of server logs and Referer headers.
	http.Redirect(w, r, claims.String("redirect")+"#oauthToken="+authToken, http.StatusSeeOther)
}

func exchangeAndFetch(app *core.App, provider Provider, code, verifier string) (OAuthUser, error) {
	s := app.Settings()
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {app.BaseURL() + "/api/oauth2/callback"},
		"client_id":     {s.String("oauth." + provider.Name + ".clientId")},
		"client_secret": {s.String("oauth." + provider.Name + ".clientSecret")},
	}
	if provider.PKCE {
		form.Set("code_verifier", verifier)
	}
	req, err := http.NewRequest("POST", provider.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthUser{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return OAuthUser{}, err
	}
	defer resp.Body.Close()
	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tokenResp); err != nil {
		return OAuthUser{}, err
	}
	if tokenResp.AccessToken == "" {
		return OAuthUser{}, fmt.Errorf("token exchange failed: %s", firstNonEmpty(tokenResp.Error, resp.Status))
	}

	uiReq, err := http.NewRequest("GET", provider.UserInfoURL, nil)
	if err != nil {
		return OAuthUser{}, err
	}
	uiReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	uiReq.Header.Set("Accept", "application/json")
	uiResp, err := httpClient().Do(uiReq)
	if err != nil {
		return OAuthUser{}, err
	}
	defer uiResp.Body.Close()
	var raw map[string]any
	if err := json.NewDecoder(io.LimitReader(uiResp.Body, 1<<20)).Decode(&raw); err != nil {
		return OAuthUser{}, err
	}
	return provider.MapUser(raw, httpClient(), tokenResp.AccessToken)
}

func findOrCreateOAuthUser(app *core.App, c *schema.Collection, provider Provider, user OAuthUser) (map[string]any, error) {
	ctx := context.Background()
	d := app.DB()
	q := d.Dialect.Quote

	// 1. Existing external link.
	row, err := d.QueryMap(ctx, fmt.Sprintf(
		"SELECT * FROM %s WHERE provider = ? AND %s = ? AND %s = ? LIMIT 1",
		q(ExternalAuthsCollection), q("providerId"), q("collection")),
		provider.Name, user.ID, c.Name)
	if err != nil {
		return nil, err
	}
	if row != nil {
		record, err := app.FindRecordByID(ctx, c.Name, db.ToString(row["recordId"]))
		if err != nil || record == nil {
			return nil, fmt.Errorf("linked account no longer exists")
		}
		return record, nil
	}

	// 2. Link by verified email.
	var record map[string]any
	if user.Email != "" {
		record, err = app.FindFirstRecord(ctx, c.Name, "email", strings.ToLower(user.Email))
		if err != nil {
			return nil, err
		}
	}

	// 3. Create a fresh account.
	if record == nil {
		if user.Email == "" {
			return nil, fmt.Errorf("provider returned no email address")
		}
		hash, err := security.HashPassword(security.RandomToken(24))
		if err != nil {
			return nil, err
		}
		id := security.RandomID(15)
		now := db.Now()
		_, err = d.Exec(ctx, fmt.Sprintf(
			"INSERT INTO %s (id, created, updated, email, password, %s, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			q(c.Name), q("tokenKey")),
			id, now, now, strings.ToLower(user.Email), hash, newTokenKey(), user.Verified, user.Name)
		if err != nil {
			return nil, fmt.Errorf("account creation failed")
		}
		record, err = app.FindRecordByID(ctx, c.Name, id)
		if err != nil {
			return nil, err
		}
		app.OnRecordAfterCreate.Trigger(&core.RecordEvent{
			App: app, Action: "create", Collection: c, Record: record,
		})
	}

	// Persist the external link.
	now := db.Now()
	_, err = d.Exec(ctx, fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, %s, %s, provider, %s) VALUES (?, ?, ?, ?, ?, ?, ?)",
		q(ExternalAuthsCollection), q("collection"), q("recordId"), q("providerId")),
		security.RandomID(15), now, now, c.Name, db.ToString(record["id"]), provider.Name, user.ID)
	if err != nil {
		app.Log().Warn("external auth link insert failed", "err", err)
	}
	return record, nil
}
