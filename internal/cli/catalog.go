package cli

// ModuleDef describes one installable backend module.
type ModuleDef struct {
	ID       string
	Label    string
	Desc     string
	Import   string // package under the framework module
	Expr     string // constructor expression
	Default  bool
	Requires []string
	Hidden   bool // always installed, not shown in pickers
}

// Catalog is the module registry used by `forge init` / `forge add`.
var Catalog = []ModuleDef{
	{ID: "apis", Label: "REST API", Desc: "Records CRUD, collections admin, files, realtime SSE", Import: "pkg/apis", Expr: "apis.Module{}", Default: true, Hidden: true},
	{ID: "auth", Label: "Authentication", Desc: "Register, login, verify, password reset (argon2id + JWT)", Import: "pkg/auth", Expr: "auth.Module{}", Default: true},
	{ID: "perm", Label: "Roles & permissions", Desc: "Role-based access control on top of collection rules", Import: "pkg/perm", Expr: "perm.Module{}", Default: true, Requires: []string{"auth"}},
	{ID: "mail", Label: "Email", Desc: "SMTP, sendmail, Resend, SES — switchable at runtime", Import: "pkg/mail", Expr: "mail.Module{}", Default: true},
	{ID: "adminui", Label: "Admin dashboard", Desc: "Embedded PocketBase-style admin at /_/", Import: "pkg/adminui", Expr: "adminui.Module{}", Default: true},
	{ID: "mcp", Label: "MCP server", Desc: "Expose collections as AI tools + API keys (/api/mcp)", Import: "pkg/mcp", Expr: "mcp.Module{}", Default: true},
	{ID: "logs", Label: "Request logs", Desc: "Persisted request logs with retention", Import: "pkg/logs", Expr: "logs.Module{}", Default: true},
	{ID: "update", Label: "Self-update", Desc: "Caddy-style binary updates from a release manifest", Import: "pkg/update", Expr: "update.Module{}", Default: true},
	{ID: "oauth", Label: "OAuth2 / OIDC", Desc: "Google, GitHub, Microsoft, GitLab, Discord + generic OIDC SSO", Import: "pkg/auth", Expr: "auth.OAuthModule{}", Requires: []string{"auth"}},
	{ID: "mfa", Label: "MFA (TOTP)", Desc: "Two-factor login with authenticator apps", Import: "pkg/auth", Expr: "auth.MFAModule{}", Requires: []string{"auth"}},
	{ID: "ldap", Label: "LDAP / AD", Desc: "Directory login with auto-provisioning", Import: "pkg/ldap", Expr: "ldap.Module{}", Requires: []string{"auth"}},
	{ID: "saml", Label: "SAML SSO", Desc: "SAML 2.0 service provider (Okta, Entra, Keycloak)", Import: "pkg/saml", Expr: "saml.Module{}", Requires: []string{"auth"}},
	{ID: "orgs", Label: "Organizations", Desc: "Multi-tenancy: orgs, members, roles, email invites", Import: "pkg/orgs", Expr: "orgs.Module{}", Requires: []string{"auth", "mail"}},
	{ID: "webhooks", Label: "Webhooks", Desc: "Signed outgoing webhooks on record events", Import: "pkg/webhooks", Expr: "webhooks.Module{}"},
	{ID: "jobs", Label: "Cron jobs", Desc: "Scheduled background work inside the app", Import: "pkg/jobs", Expr: "jobs.New()"},
	{ID: "metrics", Label: "Metrics", Desc: "Prometheus /metrics endpoint", Import: "pkg/metrics", Expr: "metrics.Module{}"},
	{ID: "backups", Label: "Backups", Desc: "One-click data + files snapshots", Import: "pkg/backups", Expr: "backups.Module{}"},
}

// CatalogByID indexes the catalog.
func CatalogByID() map[string]ModuleDef {
	out := make(map[string]ModuleDef, len(Catalog))
	for _, m := range Catalog {
		out[m.ID] = m
	}
	return out
}

// ResolveModules expands requirements and hidden defaults, returning a
// stable, deduplicated module list.
func ResolveModules(selected []string) []string {
	byID := CatalogByID()
	set := map[string]bool{}
	var visit func(id string)
	visit = func(id string) {
		if set[id] {
			return
		}
		def, ok := byID[id]
		if !ok {
			return
		}
		set[id] = true
		for _, req := range def.Requires {
			visit(req)
		}
	}
	for _, m := range Catalog {
		if m.Hidden {
			visit(m.ID)
		}
	}
	for _, id := range selected {
		visit(id)
	}
	// stable order: catalog order
	var out []string
	for _, m := range Catalog {
		if set[m.ID] {
			out = append(out, m.ID)
		}
	}
	return out
}

// DBDrivers lists supported database choices.
var DBDrivers = []struct {
	ID    string
	Label string
	Desc  string
}{
	{"sqlite", "SQLite", "Zero-config single file, perfect default (CGO-free driver)"},
	{"postgres", "PostgreSQL", "Set FORGE_DB_DSN, e.g. postgres://user:pass@localhost/app"},
	{"mysql", "MySQL / MariaDB", "Set FORGE_DB_DSN, e.g. user:pass@tcp(localhost:3306)/app"},
}
