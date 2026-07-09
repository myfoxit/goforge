# Architecture

GoForge is a framework (a set of Go packages) plus a CLI. Your application
imports the framework as a versioned dependency and wires up the modules it
needs. Everything compiles into one binary.

```
┌──────────────────────────────────────────────────────────────┐
│  your app binary                                             │
│                                                              │
│  main.go → forge.Run(app)                                    │
│                                                              │
│  ┌────────────┐   modules (opt-in)                           │
│  │ core.App   │   auth · perm · oauth · mfa · ldap · saml    │
│  │            │   mail · mcp · adminui · orgs · webhooks      │
│  │ Hooks      │   jobs · metrics · logs · update · backups   │
│  │ Settings   │                                              │
│  │ HTTP mux   │   ┌──────────────┐   ┌──────────────┐        │
│  └─────┬──────┘   │ schema.Registry│  │ rules engine │       │
│        │          │ (collections)  │  │ (→ SQL)      │        │
│        │          └───────┬────────┘  └──────┬───────┘        │
│        │                  │                  │               │
│        └──────────────────┴──────────────────┘               │
│                           │                                  │
│                   ┌───────┴────────┐                         │
│                   │ db.DB + Dialect│  sqlite │ postgres │ mysql │
│                   └────────────────┘                         │
└──────────────────────────────────────────────────────────────┘
```

## The kernel: `core.App`

`core.App` is the container. It owns:

- the database handle (`db.DB`) and its dialect,
- the schema registry (dynamic collections),
- the settings store (runtime configuration, encrypted secrets),
- the HTTP `ServeMux` and middleware chain,
- the migration runner,
- lifecycle and record **hooks**.

A **module** is anything implementing:

```go
type Module interface {
    ID() string
    Register(app *App) error
}
```

`Register` is called once at bootstrap. Modules add settings sections, HTTP
routes, migrations, auth resolvers and hook handlers. Because modules are just
values you pass to `app.Use(...)`, the set is chosen per app — the CLI generates
that list into `modules_gen.go` from `forge.json`.

## Request lifecycle

```
request
  → recover → request-id → CORS → auth-resolve → logger    (middleware)
  → ServeMux route
      → handler builds a Request{Auth, Data, Query}
      → rules compiled to a parameterized SQL WHERE clause
      → db query on the collection's real table
      → hooks fire (OnRecordBefore/After…)
  → JSON response
```

Authentication is resolved once per request into a `*core.Auth` on the context.
The default resolver validates the bearer JWT; modules can register more (the
`mcp` module adds an API-key resolver).

## Dynamic schema

Collections are rows in `_collections`, cached in memory by `schema.Registry`.
When you save a collection, the registry diffs it against the stored definition
and emits `CREATE`/`ALTER`/`DROP` through the dialect. Field renames are tracked
by a stable field `id`, so renaming a field preserves its column and data.

Higher-level field types (`email`, `url`, `select`, `relation`, `file`, …) all
reduce to one of six storage kinds (`ColID`, `ColText`, `ColNumber`, `ColBool`,
`ColJSON`, `ColDateTime`), which is what makes one definition work identically on
all three engines.

## The rules engine

Access rules and query filters share one small expression language
(`pkg/rules`). Expressions are parsed to an AST and compiled to **parameterized
SQL** scoped to the collection's table, so a rule like `owner = @request.auth.id`
becomes `WHERE "posts"."owner" = ?` and runs in the database with consistent
semantics everywhere. Value-only comparisons (`@request.auth.id != ''`) fold to
`1=1` / `1=0` at compile time. See [collections.md](collections.md).

## Hooks

Modules and app code extend behavior through typed hooks:

```go
app.OnRecordAfterCreate.Add(func(e *core.RecordEvent) error {
    // e.Collection, e.Record, e.Auth, e.Request
    return nil
})
```

`Before*` hooks can mutate the record or return an error to abort the operation;
`After*` hooks react to committed changes (this is how realtime, webhooks and the
verification-email flow are wired).

## Packages

| Package | Responsibility |
|---|---|
| `pkg/security` | argon2id, AES-GCM, random ids/tokens |
| `pkg/token` | compact HS256 JWTs, purpose-scoped |
| `pkg/config` | boot config (file + `FORGE_*` env) |
| `pkg/db` | `sql.DB` wrapper + dialect abstraction |
| `pkg/db/drivers/*` | pluggable sqlite/postgres/mysql drivers |
| `pkg/migrations` | forward-only code migrations |
| `pkg/schema` | collections, fields, DDL sync |
| `pkg/rules` | rule/filter language → SQL |
| `pkg/core` | the App kernel, hooks, settings, HTTP |
| `pkg/apis` | REST endpoints + the records service |
| `pkg/auth`, `pkg/perm` | authentication & RBAC |
| `pkg/mail`, `pkg/files`, `pkg/awsig` | mail adapters, storage, SigV4 |
| `pkg/mcp` | MCP server + API keys |
| `pkg/update`, `pkg/jobs`, `pkg/metrics`, … | operational modules |
| `pkg/adminui` | embedded admin dashboard |
| `internal/cli`, `cmd/forge` | the CLI |
| `ui/registry` | the design system |
| `templates/app` | the `forge init` scaffold |
