# Building a monitoring app (Northplane / CMP style)

This walkthrough shows how GoForge's capabilities compose into a real product: a
multi-tenant monitoring tool where organizations register servers, agents push
metrics, users view dashboards, and instances self-update across a fleet. You
**don't** rebuild this from scratch — you assemble it from modules and collections.

## 1. Scaffold

```sh
forge init northplane
#   modules: auth, perm, orgs, mail, mcp, adminui, webhooks, jobs, metrics, update, logs
cd northplane
go run . superuser create you@example.com your-password
go run . serve
```

You now have auth, multi-tenancy, an admin UI, self-update and a job scheduler.

## 2. Model the domain (collections)

Create these in the admin UI (or have Claude build them over MCP). Multi-tenancy
comes from the `orgs` module: scope every collection to an org.

**`servers`** — a monitored host, owned by an org.

| field | type | options |
|---|---|---|
| name | text | required |
| hostname | text | required |
| org | relation | → `orgs` |
| status | select | up, down, degraded |
| lastSeen | date | |

```
listRule:   org.members ~ @request.auth.id
viewRule:   org.members ~ @request.auth.id
createRule: @request.auth.id != '' && org.members ~ @request.auth.id
updateRule: org.members ~ @request.auth.id
```

**`metrics`** — a time-series sample.

| field | type | options |
|---|---|---|
| server | relation | → `servers` |
| cpu | number | |
| memory | number | |
| disk | number | |
| recorded | date | |

```
listRule: server.org.members ~ @request.auth.id    // (use a view if you need 2 hops)
```

> Rules support single-hop relations. For a two-hop scope (metric → server → org),
> add an `org` relation directly on `metrics` and scope on `org.members ~ …`, or
> define a **view** that joins the scope in.

**`alerts`** — a threshold rule per org, plus an `incidents` collection for fired
alerts. Wire notifications with the `webhooks` module (Slack/PagerDuty) or a hook
that sends email.

## 3. Agents push data (API keys)

Each server runs an agent that authenticates with a **scoped API key**:

```sh
# in the admin UI: create a key scoped to metrics:create for one org
curl -X POST https://np.example.com/api/collections/metrics/records \
  -H "Authorization: Bearer forge_AGENTKEY" \
  -d '{"server":"srv_123","cpu":42.1,"memory":68.0,"disk":55.2,"recorded":"2026-07-01 12:00:00"}'
```

The key's scope (`metrics:create`) limits the agent to exactly that. Rotate or
revoke keys per agent from the admin UI.

## 4. Realtime dashboards (SvelteKit + SSE)

The frontend subscribes to a server's metrics topic and re-renders live:

```ts
import { forge } from "$lib/goforge";

let latest = $state<Record<string, unknown>[]>([]);
async function load(serverId: string) {
    latest = (await forge.list("metrics", {
        filter: `server = '${serverId}'`, sort: "-recorded", perPage: 60,
    })).items;
}
// push updates without polling
forge.realtime(["metrics"], () => load(serverId));
```

Build the UI with the vendored design system (`Card`, `Table`, `Badge`,
`ThemeToggle`, …). `forge dev` runs the API and frontend together.

## 5. Aggregation & alerting (jobs)

Use the `jobs` module for periodic work — roll up metrics, evaluate thresholds,
mark servers `down` when `lastSeen` is stale:

```go
j := jobs.New().
    Add("*/1 * * * *", "evaluate-alerts", func(ctx context.Context, app *core.App) error {
        // query metrics, compare to alert thresholds, create incidents,
        // fire webhooks. The full core.App is available here.
        return nil
    })
app.Use(j)
```

## 6. AI operations (MCP)

Give an on-call engineer an MCP connection with a read-scoped key:

> "Which servers in the Acme org have been degraded in the last hour, and what's
> their average CPU?"

The model calls `servers_list` and `metrics_list` with filters — no dashboard
required. An admin key could even add a new alert type by calling
`collections_save`.

## 7. Ship & update the fleet

Each customer runs their own instance (single binary + `forge_data`). Publish
updates centrally:

```sh
forge release keygen
forge release --version 2.1.0 --url https://downloads.example.com/np --key forge-release.key
```

Every instance polls the manifest and self-updates (verified by SHA-256 +
ed25519), from the admin UI or automatically — different servers, different
starting versions, converging safely. See [updates.md](updates.md).

## What you reused vs. built

| Reused from GoForge | You built |
|---|---|
| auth, orgs, RBAC, API keys | the collection schema (servers/metrics/alerts) |
| REST + realtime + MCP for every collection | threshold logic in one job |
| admin UI, settings, logs, metrics | dashboard components |
| self-update, backups | — |

The plumbing is the framework; the product is your data model and a little glue.
