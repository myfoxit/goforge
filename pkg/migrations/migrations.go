// Package migrations runs code-based, forward-only migrations. The dynamic
// schema engine handles collection DDL automatically; migrations exist for
// module setup, data backfills and custom app tables.
package migrations

import (
	"context"
	"fmt"
	"log/slog"
	"sort"

	"github.com/myfoxit/goforge/pkg/db"
)

// Migration is a single named migration. IDs must be unique and are applied
// in lexicographic order — use "0001_modulename_description".
type Migration struct {
	ID string
	Up func(ctx context.Context, d *db.DB) error
}

// Runner applies registered migrations exactly once each.
type Runner struct {
	db         *db.DB
	log        *slog.Logger
	migrations []Migration
}

func NewRunner(d *db.DB, log *slog.Logger) *Runner {
	if log == nil {
		log = slog.Default()
	}
	return &Runner{db: d, log: log}
}

// Register queues migrations. Safe to call multiple times before Run.
func (r *Runner) Register(ms ...Migration) {
	r.migrations = append(r.migrations, ms...)
}

// Run applies all pending migrations in ID order.
func (r *Runner) Run(ctx context.Context) error {
	d := r.db.Dialect
	create := d.CreateTable("_migrations", []db.ColumnDef{
		{Name: "id", Kind: db.ColID, PK: true},
		{Name: "applied", Kind: db.ColDateTime, NotNull: true},
	})
	if _, err := r.db.Exec(ctx, create); err != nil {
		return fmt.Errorf("migrations: init table: %w", err)
	}

	applied := map[string]bool{}
	rows, err := r.db.QueryMaps(ctx, "SELECT id FROM _migrations")
	if err != nil {
		return err
	}
	for _, row := range rows {
		applied[db.ToString(row["id"])] = true
	}

	pending := make([]Migration, 0)
	seen := map[string]bool{}
	for _, m := range r.migrations {
		if seen[m.ID] {
			return fmt.Errorf("migrations: duplicate id %q", m.ID)
		}
		seen[m.ID] = true
		if !applied[m.ID] {
			pending = append(pending, m)
		}
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].ID < pending[j].ID })

	for _, m := range pending {
		r.log.Info("applying migration", "id", m.ID)
		if err := m.Up(ctx, r.db); err != nil {
			return fmt.Errorf("migrations: %s: %w", m.ID, err)
		}
		if _, err := r.db.Exec(ctx,
			"INSERT INTO _migrations (id, applied) VALUES (?, ?)", m.ID, db.Now()); err != nil {
			return fmt.Errorf("migrations: record %s: %w", m.ID, err)
		}
	}
	return nil
}
