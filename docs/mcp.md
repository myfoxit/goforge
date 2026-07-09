# MCP — connect your app to an AI

Enable the `mcp` module and every GoForge app becomes a **Model Context Protocol
server**: each collection is exposed as typed tools an LLM can call, and admin
keys additionally get schema-building tools — so an AI can both *use* and *build*
your application.

The endpoint is streamable-HTTP JSON-RPC 2.0 at `POST /api/mcp`.

## Create an API key

In the admin UI → **API Keys & MCP** → New key. Choose:

- **Scopes** — `*`, or `collection:action` pairs like `posts:read,posts:create`.
- **Admin** — grants the schema/settings tools and bypasses collection rules.

The plaintext key (`forge_…`) is shown once. Keys are stored hashed.

## Connect a client

```sh
# Claude Code / Claude Desktop
claude mcp add --transport http myapp https://myapp.example.com/api/mcp \
  --header "Authorization: Bearer forge_YOURKEY"
```

Or in an `mcp.json`:

```json
{
  "mcpServers": {
    "myapp": {
      "type": "http",
      "url": "https://myapp.example.com/api/mcp",
      "headers": { "Authorization": "Bearer forge_YOURKEY" }
    }
  }
}
```

The admin UI's MCP screen shows both snippets prefilled for your instance.

## Tools

For every collection the caller can see:

| Tool | Action |
|---|---|
| `<collection>_list` | list with `filter`, `sort`, `page`, `perPage`, `expand` |
| `<collection>_get` | fetch one by `id` |
| `<collection>_create` | create (typed `data` schema per collection) |
| `<collection>_update` | patch by `id` |
| `<collection>_delete` | delete by `id` |

The `data` input schema is generated from the collection's fields, including
enums for `select` fields and relation-target hints, so the model gets accurate
structured-input validation.

Admin keys additionally get:

| Tool | Action |
|---|---|
| `collections_list` | read the full schema |
| `collections_save` | create / evolve a collection (fields, indexes, rules) |
| `collections_delete` | drop a collection |
| `settings_get` / `settings_set` | read / write app settings |

`filter` uses the same expression language as the REST API, and all tool calls go
through the identical rule + scope enforcement — the AI can never exceed the key's
permissions.

## The low-code / Lovable hook

Point an admin-keyed model at a fresh app and describe what you want:

> "Create a `tickets` collection with title, status (open/closed), priority
> (low/med/high) and an assignee relation to users. Only assignees can update
> their tickets."

The model calls `collections_save`, and the collection — with its REST API,
realtime, rules and its *own* MCP tools — exists immediately. This is how you
build applications with AI on top of GoForge.

## Verifying

`scripts/e2e.sh` includes an MCP round-trip: handshake, `collections_save` to
build a collection, and a REST check that the AI-built collection is live.
