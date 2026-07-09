# Modules & the frontend

## Backend modules

Modules are opt-in units of functionality. The CLI records your selection in
`forge.json` and generates `modules_gen.go`; add or remove them anytime:

```sh
forge modules            # list everything, with install markers
forge add webhooks jobs  # add modules (interactive if no args)
forge remove metrics
```

See the [module matrix](../README.md#module-matrix) for the full list. Each module
that has runtime configuration registers a **settings section**, which the admin
UI renders automatically — so new modules get a config screen for free.

### Writing a custom module

```sh
forge module new billing
```

scaffolds `modules/billing/billing.go`:

```go
package billing

import (
    "net/http"
    "github.com/myfoxit/goforge/pkg/core"
)

type Module struct{}

func (Module) ID() string { return "billing" }

func (Module) Register(app *core.App) error {
    // settings section (optional) — rendered in the admin UI
    app.Settings().RegisterSection(core.SettingsSection{
        ID: "billing", Title: "Billing", Order: 50,
        Fields: []core.SettingsField{
            {Key: "billing.stripeKey", Label: "Stripe secret", Type: "secret"},
        },
    })

    // routes
    app.Mux().HandleFunc("POST /api/billing/webhook", func(w http.ResponseWriter, r *http.Request) {
        // ...
    })

    // react to data changes
    app.OnRecordAfterCreate.Add(func(e *core.RecordEvent) error {
        if e.Collection.Name == "subscriptions" { /* ... */ }
        return nil
    })
    return nil
}
```

Wire it up:

```go
import "example.com/myapp/modules/billing"
app.Use(billing.Module{})
```

Custom modules have the full `core.App` surface: the database, schema registry,
settings, hooks, auth context, mailer and storage — everything the built-in
modules use.

## The frontend

`forge init --ui` scaffolds a **SvelteKit** app under `ui/`:

```
ui/
  src/
    routes/          +layout, landing, /login, /register, /app (realtime demo)
    lib/
      goforge.ts     typed API client (auth, records, files, realtime SSE)
      tokens.css     design-system tokens (vendored)
      components/ui/ the design system (vendored, yours to edit)
```

### The client

```ts
import { forge } from "$lib/goforge";

await forge.login("users", email, password);
const posts = await forge.list("posts", { filter: "published = true", expand: "author" });
await forge.create("posts", { title, owner: forge.record!.id });

// realtime — re-render on any change to the collection
const stop = forge.realtime(["posts"], (e) => { /* e.action, e.record */ });
```

### Design system

Components live in `ui/src/lib/components/ui` and are imported via the `$ui`
alias. They're **vendored, not an npm dependency** — edit them freely. Pull
framework improvements without losing your edits:

```sh
forge ui add table dialog toast   # copy more components in
forge ui update                    # refresh, skipping files you changed (hash-tracked)
```

### Development & build

```sh
forge dev     # runs the Go API and the Vite dev server together (proxying /api)
forge build   # compiles the UI and bakes it into the single Go binary
```

In production the compiled frontend is embedded and served from `/`, the admin
dashboard from `/_/`, and the API from `/api` — one binary, one port.
