<div align="center">

# ⚒ GoForge

**A modular SaaS builder — PocketBase meets shadcn, in Go.**

Dynamic collections with instant REST + MCP APIs · pluggable auth (password / OAuth / OIDC / LDAP / SAML / MFA) · a rules-based permission system · adapter-based mail & storage · realtime subscriptions · an embedded admin dashboard · Caddy-style self-updates — all compiled into **a single Go binary** on SQLite, PostgreSQL or MySQL, with a SvelteKit frontend and a shadcn-style, dependency-free design system.

</div>

---

## Why

You want to build several SaaS products without rebuilding the plumbing each time. GoForge is the shared base: define your data model at runtime, get a secure API for free, pick only the modules you need, and update every app you ship by bumping one dependency. Every table is instantly available as a REST endpoint **and** as MCP tools — so you can point Claude (or any LLM) at your app to query data or even build the schema itself, Lovable-style.

## Quickstart

```sh
# 1. Install the CLI
go install github.com/myfoxit/goforge/cmd/forge@latest

# 2. Scaffold an app (interactive: pick a template, modules and DB)
forge init myapp            # choose "Full SaaS starter" for a ready base app
cd myapp

# 3. Create an admin and run it
go run . superuser create you@example.com your-password
go run . serve

# 4. Open the admin dashboard
open http://localhost:8090/_/
```

That's a single self-contained binary serving a REST API, an admin UI, an MCP
server, realtime SSE and (optionally) your SvelteKit frontend.

`forge init` offers three **templates**:

| Template | Frontend |
| --- | --- |
| **Minimal** | API + admin only — no frontend |
| **Demo** | Landing + auth pages + a realtime notes demo |
| **Full SaaS starter** | A complete base app: app shell (sidebar + account menu), full auth suite (login/register/reset/verify/**OAuth**/**MFA**), account & profile, user management, **organizations/teams** with invites, a billing surface, and a searchable, sortable, paginated **data view** (table + list) over a seeded example collection |

Pass `--template saas` (or `minimal`/`demo`) to skip the prompt.

## The pitch in one screen

```go
// main.go — your entire backend
package main

import (
    forge "github.com/myfoxit/goforge"
    _ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
)

func main() {
    app := forge.New()
    app.Use(forgeModules()...) // generated from forge.json by the CLI
    forge.Run(app)
}
```

Define a `posts` collection in the admin UI with a rule like
`owner = @request.auth.id`, and you immediately have:

```sh
GET    /api/collections/posts/records?filter=published=true&sort=-created&expand=owner
POST   /api/collections/posts/records
PATCH  /api/collections/posts/records/{id}
DELETE /api/collections/posts/records/{id}
# ...enforced by the rule, on every driver, plus realtime + MCP tools for the same data.
```

## Module matrix

| Module | What you get | Requires |
|---|---|---|
| `apis` | Records CRUD, collections admin, file serving, realtime SSE | — (always on) |
| `auth` | Register / login / verify / password-reset, argon2id, JWT | — |
| `perm` | Roles + RBAC on top of collection rules | `auth` |
| `oauth` | Google, GitHub, Microsoft, GitLab, Discord + generic OIDC | `auth` |
| `mfa` | TOTP two-factor login | `auth` |
| `ldap` | LDAP / Active Directory login with auto-provisioning | `auth` |
| `saml` | SAML 2.0 service provider (Okta, Entra, Keycloak) | `auth` |
| `mail` | SMTP, sendmail, Resend, SES — switchable at runtime | — |
| `mcp` | Every collection as AI tools + scoped API keys | — |
| `adminui` | Embedded PocketBase-style admin at `/_/` — record search/sort/paginate, bulk actions, export, relation/file/date widgets, schema + index editor, backups, superusers | — |
| `orgs` | Multi-tenancy: organizations, members, email invites | `auth`, `mail` |
| `webhooks` | Signed outgoing webhooks on record events | — |
| `jobs` | In-process cron scheduler | — |
| `metrics` | Prometheus `/metrics` | — |
| `logs` | Persisted request logs with retention | — |
| `update` | Caddy-style self-update from a signed manifest | — |
| `backups` | One-click data + files snapshots | — |

Add or remove any of them later with `forge add <module>` / `forge remove <module>`.

## Databases

One dialect abstraction, three engines. Switch with a config value; the dynamic
schema engine emits the right DDL for each:

```yaml
db:
  driver: sqlite     # or postgres, or mysql
  dsn: ""            # sqlite: defaults to <dataDir>/data.db
```

SQLite uses a CGO-free driver, so cross-compilation stays trivial.

## AI-native (MCP)

Every app is a Model Context Protocol server. Create an API key in the admin UI,
then connect it:

```sh
claude mcp add --transport http myapp https://myapp.example.com/api/mcp \
  --header "Authorization: Bearer forge_..."
```

- **Scoped keys** limit an AI to specific collections and actions.
- **Admin keys** additionally expose `collections_*` and `settings_*` tools, so
  the model can *build and evolve the application itself* — the low-code / Lovable hook.

## Design system

A shadcn-look, **dependency-free** Svelte 5 component set (23 components, CSS-variable
tokens, light + dark). Components are *vendored into your app*, not installed from
npm — the code is yours to edit. Keep them updated with a hash-aware lockfile:

```sh
forge ui add table dialog toast   # copy components into your UI
forge ui update                    # pull framework updates, skipping files you edited
```

## Updating a fleet

Northplane-style: many instances on many servers, different versions.
`forge release` cross-compiles and writes a signed update manifest; each instance
polls it and can self-update (from the admin UI, the `app update` command, or
automatically), verifying SHA-256 + ed25519 before an atomic binary swap.

```sh
forge release keygen                                    # once: signing key
forge release --version 1.2.0 --url https://cdn/... --key forge-release.key
```

## The CLI

```
forge init [dir]            Create an app (template + module + DB picker)
                            --template minimal|demo|saas
forge add / remove          Toggle backend modules
forge ui add / update       Vendor & refresh design-system components
forge dev                   Run the API + SvelteKit dev server together
forge build                 Single production binary (embeds the UI)
forge release               Cross-compile + signed self-update manifest
forge module new <name>     Scaffold a custom Go module
```

## Documentation

- [Architecture](docs/architecture.md) — how the pieces fit together
- [Collections & the rules language](docs/collections.md)
- [Authentication](docs/auth.md) · [Permissions](docs/permissions.md)
- [Databases](docs/databases.md) · [MCP](docs/mcp.md) · [Updates](docs/updates.md)
- [Modules & the frontend](docs/modules.md)
- [Building a monitoring app (Northplane-style)](docs/building-a-monitoring-app.md)

## Status & license

GoForge is young but exercised end to end (see `scripts/e2e.sh` and the package
tests). MIT licensed — see [LICENSE](LICENSE).
