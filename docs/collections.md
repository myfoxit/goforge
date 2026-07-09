# Collections & the rules language

A **collection** is a dynamic table. You define it at runtime — in the admin UI,
through the collections API, or via MCP tools — and GoForge syncs a real database
table plus an instant REST API.

## Collection types

- **base** — a regular data table.
- **auth** — carries the system auth fields (`email`, `password`, `tokenKey`,
  `verified`, `name`) and powers the login endpoints.
- **view** — a read-only, stored `SELECT`. Output columns become fields.

## Field types

| Type | Stored as | Notes |
|---|---|---|
| `text`, `editor` | text | `min`, `max`, `pattern` options |
| `number` | float | `min`, `max`, `noDecimals` |
| `bool` | bool | |
| `email`, `url` | text | validated + normalized |
| `date` | sortable UTC text | accepts RFC3339 and common formats |
| `select` | text or JSON array | `values`, `maxSelect` |
| `relation` | id or JSON array | `collection`, `maxSelect` |
| `file` | filename or JSON array | `maxSelect`, `maxSize` |
| `json` | text | `maxSize` |
| `password` | argon2id hash | write-only, hidden |
| `autodate` | UTC text | system-managed |

Every record also has `id`, `created`, `updated`.

## Access rules

Each collection has five rules — `list`, `view`, `create`, `update`, `delete`.
Each is one of:

- **`nil` (locked)** — only superusers. This is the safe default.
- **`""` (empty)** — public; anyone may perform the action.
- **an expression** — evaluated per request.

Rules compile to a SQL `WHERE` clause, so listing a collection returns exactly
the records the caller may see, and view/update/delete only touch a record when
the rule matches it.

### Expression language

```
owner = @request.auth.id                       // record ownership
@request.auth.id != ''                          // any authenticated user
@request.auth.roles ~ 'admin'                   // role membership (~ = contains)
status = 'published' && created >= '2025-01-01' // combine conditions
published = true || owner = @request.auth.id    // or
author.plan = 'pro'                             // single-hop relation
org.members ~ @request.auth.id                  // membership via relation
```

**Operators:** `=`  `!=`  `>`  `>=`  `<`  `<=`  `~` (contains / list-includes)
`!~` (not contains)  `&&`  `||`, with `( )` for grouping.

**Placeholders:**

| Placeholder | Resolves to |
|---|---|
| `@request.auth.id` | caller's record id (empty when unauthenticated) |
| `@request.auth.roles` | JSON array of the caller's role names |
| `@request.auth.<field>` | any non-hidden field of the caller's record |
| `@request.data.<field>` | a field of the incoming create/update body |
| `@request.query.<param>` | a query-string parameter |
| `@now` | current UTC timestamp |

Field literals are compared with `= 'value'`; `null` and `''` both mean "empty"
and match `NULL` or empty string. Comparisons that involve only values (no
column) are folded at compile time — `@request.auth.id != ''` becomes `1=1` for
an authenticated caller and `1=0` for an anonymous one.

Only single-hop relations are allowed in dotted paths (`author.plan`), and hidden
fields (like `password`) are rejected in rules unless you are a superuser.

## Querying records

```
GET /api/collections/posts/records
      ?page=1&perPage=30
      &sort=-created,title            # - prefix = descending; @random supported
      &filter=views > 100 && published = true
      &expand=author,comments.user    # nested relation expansion (max depth 3)
      &skipTotal=1                    # skip the COUNT query for speed
```

`filter` uses the same expression language and is ANDed with the collection's
list rule — a client can never widen its own visibility. Expanded records respect
the target collection's view rule.

## Creating collections via API

```sh
curl -X POST /api/collections -H "Authorization: Bearer <superuser>" -d '{
  "name": "posts",
  "type": "base",
  "fields": [
    {"name": "title", "type": "text", "required": true},
    {"name": "owner", "type": "relation", "options": {"collection": "users"}}
  ],
  "listRule": "",
  "createRule": "@request.auth.id != '\'''\'' && owner = @request.auth.id",
  "updateRule": "owner = @request.auth.id"
}'
```

The same operation is available to admin MCP keys as the `collections_save`
tool — which is how an AI can build your schema.

## Views

```json
{
  "name": "post_stats",
  "type": "view",
  "options": { "query": "SELECT id, count(*) AS n FROM comments GROUP BY id" }
}
```

The query is validated (must be a `SELECT`) and its columns are introspected into
read-only fields. Views are queried like any collection but reject writes.
