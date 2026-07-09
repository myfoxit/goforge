// Package saml turns the app into a SAML 2.0 Service Provider for enterprise
// SSO (Okta, Entra ID, Keycloak, ...). Users arriving with a valid assertion
// are provisioned as local auth records by email.
//
// Endpoints:
//
//	GET  /api/saml/metadata  → SP metadata XML (upload/point your IdP at this)
//	GET  /api/saml/login     → start SP-initiated login
//	POST /api/saml/acs       → assertion consumer (IdP posts back here)
package saml

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	crewsaml "github.com/crewjam/saml"
	"github.com/crewjam/saml/samlsp"
	"github.com/myfoxit/goforge/pkg/auth"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// Module wires SAML SSO ("saml").
type Module struct{}

func (Module) ID() string { return "saml" }

func (Module) Register(app *core.App) error {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "saml", Title: "SAML SSO", Order: 13,
		Fields: []core.SettingsField{
			{Key: "saml.enabled", Label: "Enabled", Type: "bool", Default: false},
			{Key: "saml.idpMetadataURL", Label: "IdP metadata URL", Type: "text",
				Placeholder: "https://idp.example.com/metadata"},
			{Key: "saml.idpMetadataXML", Label: "IdP metadata XML (alternative to URL)", Type: "textarea"},
			{Key: "saml.emailAttr", Label: "Email attribute", Type: "text", Default: "email",
				Help: "Assertion attribute holding the email; NameID is used as fallback."},
			{Key: "saml.nameAttr", Label: "Display name attribute", Type: "text", Default: "displayName"},
			{Key: "saml.spCert", Label: "SP certificate (auto-generated)", Type: "textarea"},
			{Key: "saml.spKey", Label: "SP private key (auto-generated)", Type: "secret"},
		},
	})

	mux := app.Mux()
	mux.HandleFunc("GET /api/saml/metadata", func(w http.ResponseWriter, r *http.Request) {
		sp, err := serviceProvider(app)
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		w.Header().Set("Content-Type", "application/samlmetadata+xml")
		xml.NewEncoder(w).Encode(sp.Metadata())
	})
	mux.HandleFunc("GET /api/saml/login", func(w http.ResponseWriter, r *http.Request) {
		startLogin(app, w, r)
	})
	mux.HandleFunc("POST /api/saml/acs", func(w http.ResponseWriter, r *http.Request) {
		consumeAssertion(app, w, r)
	})
	return nil
}

// serviceProvider builds the crewjam SP from settings, generating a
// self-signed keypair on first use.
func serviceProvider(app *core.App) (*crewsaml.ServiceProvider, error) {
	s := app.Settings()
	if !s.Bool("saml.enabled") {
		return nil, fmt.Errorf("SAML is not enabled")
	}
	certPEM, keyPEM := s.String("saml.spCert"), s.String("saml.spKey")
	if certPEM == "" || keyPEM == "" {
		var err error
		certPEM, keyPEM, err = generateKeypair(app.AppName())
		if err != nil {
			return nil, err
		}
		if err := s.SetMany(context.Background(), map[string]any{
			"saml.spCert": certPEM, "saml.spKey": keyPEM,
		}); err != nil {
			return nil, err
		}
	}
	keyPair, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("saml keypair: %w", err)
	}
	keyPair.Leaf, err = x509.ParseCertificate(keyPair.Certificate[0])
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(app.BaseURL())
	if err != nil {
		return nil, err
	}
	acsURL := *base
	acsURL.Path = "/api/saml/acs"
	metadataURL := *base
	metadataURL.Path = "/api/saml/metadata"

	sp := &crewsaml.ServiceProvider{
		EntityID:          metadataURL.String(),
		Key:               keyPair.PrivateKey.(*rsa.PrivateKey),
		Certificate:       keyPair.Leaf,
		AcsURL:            acsURL,
		MetadataURL:       metadataURL,
		AllowIDPInitiated: true,
	}

	// IdP metadata: URL or inline XML.
	var metadataRaw []byte
	if xmlStr := s.String("saml.idpMetadataXML"); strings.TrimSpace(xmlStr) != "" {
		metadataRaw = []byte(xmlStr)
	} else if mdURL := s.String("saml.idpMetadataURL"); mdURL != "" {
		parsed, err := url.Parse(mdURL)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		metadata, err := samlsp.FetchMetadata(ctx, http.DefaultClient, *parsed)
		if err != nil {
			return nil, fmt.Errorf("fetch idp metadata: %w", err)
		}
		sp.IDPMetadata = metadata
		return sp, nil
	} else {
		return nil, fmt.Errorf("configure saml.idpMetadataURL or saml.idpMetadataXML")
	}
	metadata, err := samlsp.ParseMetadata(metadataRaw)
	if err != nil {
		return nil, fmt.Errorf("parse idp metadata: %w", err)
	}
	sp.IDPMetadata = metadata
	return sp, nil
}

func startLogin(app *core.App, w http.ResponseWriter, r *http.Request) {
	sp, err := serviceProvider(app)
	if err != nil {
		core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
		return
	}
	authURL, err := sp.MakeRedirectAuthenticationRequest("")
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	http.Redirect(w, r, authURL.String(), http.StatusFound)
}

func consumeAssertion(app *core.App, w http.ResponseWriter, r *http.Request) {
	sp, err := serviceProvider(app)
	if err != nil {
		core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
		return
	}
	if err := r.ParseForm(); err != nil {
		core.WriteError(w, app.Log(), core.BadRequest("Invalid form."))
		return
	}
	// AllowIDPInitiated is set, so no request IDs need tracking.
	assertion, err := sp.ParseResponse(r, nil)
	if err != nil {
		detail := err.Error()
		if ie, ok := err.(*crewsaml.InvalidResponseError); ok {
			detail = ie.PrivateErr.Error()
		}
		app.Log().Warn("saml assertion rejected", "err", detail)
		core.WriteError(w, app.Log(), core.BadRequest("SAML assertion rejected."))
		return
	}

	email, name := extractIdentity(app, assertion)
	if email == "" {
		core.WriteError(w, app.Log(), core.BadRequest("Assertion carries no email."))
		return
	}

	c, err := auth.AuthCollection(app, auth.UsersCollection)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	record, err := findOrProvision(app, c, email, name)
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	app.OnAuth.Trigger(&core.AuthEvent{App: app, Collection: c, Record: record, Method: "saml", Request: r})
	tok, err := app.NewAuthToken(c, record, auth.TokenTTL(app))
	if err != nil {
		core.WriteError(w, app.Log(), err)
		return
	}
	http.Redirect(w, r, "/#oauthToken="+tok, http.StatusSeeOther)
}

func extractIdentity(app *core.App, assertion *crewsaml.Assertion) (email, name string) {
	emailAttr := app.Settings().String("saml.emailAttr")
	nameAttr := app.Settings().String("saml.nameAttr")
	for _, stmt := range assertion.AttributeStatements {
		for _, attr := range stmt.Attributes {
			key := attr.FriendlyName
			if key == "" {
				key = attr.Name
			}
			for _, v := range attr.Values {
				switch {
				case strings.EqualFold(key, emailAttr) || strings.HasSuffix(key, "emailaddress"):
					if email == "" {
						email = strings.ToLower(strings.TrimSpace(v.Value))
					}
				case strings.EqualFold(key, nameAttr) || strings.HasSuffix(key, "displayname"):
					if name == "" {
						name = strings.TrimSpace(v.Value)
					}
				}
			}
		}
	}
	if email == "" && assertion.Subject != nil && assertion.Subject.NameID != nil {
		nameID := strings.TrimSpace(assertion.Subject.NameID.Value)
		if strings.Contains(nameID, "@") {
			email = strings.ToLower(nameID)
		}
	}
	return email, name
}

func findOrProvision(app *core.App, c *schema.Collection, email, name string) (map[string]any, error) {
	ctx := context.Background()
	record, err := app.FindFirstRecord(ctx, c.Name, "email", email)
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
		id, now, now, email, hash, security.RandomToken(24), true, name)
	if err != nil {
		return nil, fmt.Errorf("saml provisioning failed: %w", err)
	}
	record, err = app.FindRecordByID(ctx, c.Name, id)
	if err == nil && record != nil {
		app.OnRecordAfterCreate.Trigger(&core.RecordEvent{App: app, Action: "create", Collection: c, Record: record})
	}
	return record, err
}

// generateKeypair creates a self-signed cert for SAML signing.
func generateKeypair(appName string) (certPEM, keyPEM string, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", err
	}
	template := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: appName + " SAML SP"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	certPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
	return certPEM, keyPEM, nil
}
