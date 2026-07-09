# GoForge — repository guide for Claude

GoForge is a modular SaaS framework in Go ("PocketBase meets shadcn") plus a CLI.
Apps import the framework as a versioned dependency and select modules; everything
compiles to a single binary running on SQLite, Postgres or MySQL.

## Layout

- `pkg/` — the framework, in dependency order:
  - `security`, `token`, `config` — primitives.
  - `db` + `db/drivers/*` — `sql.DB` wrapper and the sqlite/postgres/mysql dialects.
  - `schema` — dynamic collections & fields, DDL sync.
  - `rules` — the access/filter language, compiled to parameterized SQL.
  - `core` — the `App` kernel: hooks, settings, HTTP mux, middleware, auth context.
  - `apis` — REST endpoints + the `Records` service (used by REST and MCP alike).
  - `auth`, `perm` — authentication and RBAC. `mail`, `files`, `awsig` — adapters.
  - `mcp` — MCP server + API keys. `adminui` — embedded admin SPA.
  - `update`, `jobs`, `metrics`, `webhooks`, `orgs`, `ldap`, `saml`, `backups`, `logs`.
  - `cmd` — the subcommand layer for built app binaries.
- `forge.go` — root facade package (`forge.New`, `forge.Run`).
- `internal/cli` + `cmd/forge` — the CLI (catalog, scaffold, ui vendoring, release).
- `ui/registry` — the design system (Svelte 5 components + tokens + `registry.json`).
- `templates/app` — the `forge init` scaffold (`.tmpl` files rendered with Go
  `text/template`; note Go uses `{{ }}`, Svelte uses `{ }`, so they don't collide).
  Frontend flavors live under `ui/src/_variants/<template>/…` and are overlaid onto
  `ui/src/…` for the chosen template (`minimal`/`demo`/`saas`), defined in
  `internal/cli/catalog.go` (`Templates`). A `.tmpl` that renders to only
  whitespace is skipped, so files can be fully guarded by `{{if eq .Template …}}`.
- `docs/`, `scripts/e2e.sh`.

## Conventions

- Module = `ID() string` + `Register(app *core.App) error`. Register wires
  settings sections, routes, migrations and hooks. Keep new features as modules.
- All record access goes through `apis.Records`, which enforces rules + scopes.
  Don't add endpoints that touch collection tables without rule checks.
- Rules and filters are the same language (`pkg/rules`); never string-concatenate
  user input into SQL — compile it or parameterize it.
- Secrets in settings use `Type: "secret"` (encrypted at rest, masked in the API).
- Timestamps: `db.Now()` (UTC, sortable text). IDs: `security.RandomID(15)`.
- MySQL reserves `key`; quote identifiers with `app.DB().Dialect.Quote(...)`.

## Working here

- Build: `go build ./...`  · Test: `go test ./...`  · Format: `gofmt -w .`
- Full-stack check: `bash scripts/e2e.sh` (scaffolds an app, builds it, exercises
  auth/collections/rules/MCP/metrics over HTTP).
- The admin UI is a dependency-free SPA in `pkg/adminui/dist` (edit `app.js` /
  `index.html` directly; no build step).
- After changing `ui/registry`, run `go test ./internal/cli` — a test verifies
  every `registry.json` entry maps to an embedded file.
- Adding a module: create `pkg/<mod>`, add it to `internal/cli/catalog.go`, and it
  becomes available to `forge init`/`forge add`.

## Testing notes

- Tests run against in-memory SQLite (`file:...?mode=memory&cache=shared`).
- `core_test.go` exposes `NewTestApp`; other packages build an app with the
  modules they need and drive it via `httptest`.
- Emails in tests use the `log` mail adapter; inspect with `mail.SentMessages()`.
