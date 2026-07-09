# Permissions

GoForge has two complementary layers:

1. **Collection rules** — declarative, per-record access control compiled to SQL
   (see [collections.md](collections.md)). This is the primary mechanism.
2. **Roles (RBAC)** — named roles you reference from rules and from route guards.

## Roles

Enable the `perm` module. It creates a `_roles` collection and adds a `roles`
multi-relation field to `users`, seeded with an `admin` role.

Assign roles in the admin UI, via the API, or programmatically:

```go
perm.AssignRole(ctx, app, userID, "editor")
```

Reference them in rules through `@request.auth.roles` (a JSON array of names):

```
@request.auth.roles ~ 'admin'                       // is an admin
@request.auth.roles ~ 'editor' || owner = @request.auth.id
```

## Route guards

For custom endpoints that aren't collection CRUD, guard handlers directly:

```go
mux := app.Mux()

// any authenticated user
mux.HandleFunc("GET /api/me", app.RequireAuth(handler))

// superusers only
mux.HandleFunc("POST /api/admin/rebuild", app.RequireSuperuser(handler))

// specific roles (superusers always allowed)
mux.HandleFunc("GET /api/reports", app.RequireRole("admin", "analyst")(handler))
```

`perm.HasRole(auth, "admin")` is available for inline checks.

## Sensible defaults

- New collections are **locked** (superuser-only) until you set rules — you never
  accidentally expose data.
- Auth collections hide `password` and `tokenKey` from every response and from
  rule expressions.
- `create` rules are checked against the **stored row** after insert, so a client
  can't create a record it wouldn't be allowed to see; a failing check rolls the
  insert back.
- `update`/`delete` first re-select the target row through the rule, returning
  `404` (not `403`) when it doesn't match — so existence isn't leaked.

## API keys & scopes

API keys (from the `mcp` module) carry scopes of the form `collection:action`:

```
*                     full access
posts:*               all actions on posts
posts:read            list + view only
posts:read,posts:create
```

Scopes bind even an "admin" key — a scoped key is limited to its collections on
both the REST API and the MCP tool surface. See [mcp.md](mcp.md).

## Patterns

**Owner-only**

```
listRule:   owner = @request.auth.id
createRule: @request.auth.id != '' && owner = @request.auth.id
updateRule: owner = @request.auth.id
deleteRule: owner = @request.auth.id
```

**Public read, owner write**

```
listRule:   ""
viewRule:   ""
createRule: @request.auth.id != '' && owner = @request.auth.id
updateRule: owner = @request.auth.id
```

**Role-gated**

```
createRule: @request.auth.roles ~ 'editor'
deleteRule: @request.auth.roles ~ 'admin'
```

**Tenant-scoped** (with the `orgs` module) — see
[building-a-monitoring-app.md](building-a-monitoring-app.md):

```
listRule:   org.members ~ @request.auth.id
createRule: @request.auth.id != '' && org.members ~ @request.auth.id
```
