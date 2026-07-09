# Databases

GoForge runs on **SQLite, PostgreSQL or MySQL/MariaDB** behind one dialect
abstraction. The same collection definitions, rules and queries work on all
three; only the DSN and the blank-imported driver change.

## Choosing a driver

The driver is compiled in via a blank import in your app's `main.go` (the CLI
adds the right one for you):

```go
import _ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"   // or postgres, mysql
```

Then set it in `config.yaml` (or `FORGE_DB_DRIVER` / `FORGE_DB_DSN`):

```yaml
db:
  driver: sqlite
  dsn: ""            # sqlite: defaults to <dataDir>/data.db
```

```yaml
db:
  driver: postgres
  dsn: "postgres://user:pass@localhost:5432/myapp?sslmode=disable"
```

```yaml
db:
  driver: mysql
  dsn: "user:pass@tcp(localhost:3306)/myapp"
```

## SQLite (default)

Uses the **CGO-free** `modernc.org/sqlite` driver, so cross-compilation stays a
plain `GOOS=… GOARCH=… go build`. GoForge tunes the connection for a web app: WAL
journal, busy-timeout, `synchronous=NORMAL`, and a small writer pool to avoid
`SQLITE_BUSY`. Perfect for single-node deployments and the vast majority of SaaS
instances.

## PostgreSQL

Uses `pgx` (via `database/sql`). Placeholders are rebound from `?` to `$n`
automatically. Recommended when you need multiple app nodes against one database.

## MySQL / MariaDB

Uses `go-sql-driver/mysql`. `utf8mb4` and `parseTime` are enabled by default. Text
columns get index prefix lengths automatically where required.

## How portability works

The dynamic schema maps every field type to one of six storage kinds, and the
dialect renders engine-specific DDL and SQL:

| Concern | SQLite | PostgreSQL | MySQL |
|---|---|---|---|
| number | `REAL` | `DOUBLE PRECISION` | `DOUBLE` |
| id / text | `TEXT` | `TEXT` | `VARCHAR`/`LONGTEXT` |
| placeholder | `?` | `$n` | `?` |
| case-insensitive `~` | `LIKE` | `ILIKE` | `LIKE` (ci collation) |
| JSON extract | `json_extract` | `::jsonb ->>` | `JSON_EXTRACT` |
| concat | `||` | `||` | `CONCAT` |

Timestamps are stored as sortable UTC text (`YYYY-MM-DD HH:MM:SS.sssZ`) so
ordering and range filters behave identically across engines.

## Migrations

The schema engine handles collection DDL automatically. For seed data and custom
tables, use code migrations (forward-only, applied once each, ordered by id):

```go
// migrations/migrations.go in your app
migrations.Register(migrations.Migration{
    ID: "0002_seed_roles",
    Up: func(ctx context.Context, d *db.DB) error {
        _, err := d.Exec(ctx, "INSERT INTO _roles (id, created, updated, name) VALUES (?, ?, ?, ?)",
            security.RandomID(15), db.Now(), db.Now(), "editor")
        return err
    },
})
```

Run them explicitly with `app migrate`, or automatically at `app serve` startup.

## Backups

The `backups` module snapshots SQLite (via `VACUUM INTO`) plus uploaded files as a
`tar.gz`. For Postgres/MySQL, use the engine's native dump tooling for the
database and the module for file storage. See [updates.md](updates.md) for the
fleet story.
