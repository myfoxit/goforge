package migrations

import (
	"context"
	"testing"

	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
)

func TestRunner(t *testing.T) {
	d, err := db.Open("sqlite", "file:test?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	ctx := context.Background()
	var order []string
	r := NewRunner(d, nil)
	r.Register(
		Migration{ID: "0002_second", Up: func(ctx context.Context, d *db.DB) error {
			order = append(order, "0002")
			return nil
		}},
		Migration{ID: "0001_first", Up: func(ctx context.Context, d *db.DB) error {
			order = append(order, "0001")
			_, err := d.Exec(ctx, "CREATE TABLE demo (id TEXT)")
			return err
		}},
	)
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "0001" || order[1] != "0002" {
		t.Fatalf("order = %v", order)
	}

	// Second run applies nothing.
	order = nil
	r2 := NewRunner(d, nil)
	r2.Register(Migration{ID: "0001_first", Up: func(ctx context.Context, d *db.DB) error {
		order = append(order, "again")
		return nil
	}})
	if err := r2.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if len(order) != 0 {
		t.Fatalf("reapplied: %v", order)
	}

	// Duplicate registration is an error.
	r3 := NewRunner(d, nil)
	r3.Register(
		Migration{ID: "dup", Up: func(context.Context, *db.DB) error { return nil }},
		Migration{ID: "dup", Up: func(context.Context, *db.DB) error { return nil }},
	)
	if err := r3.Run(ctx); err == nil {
		t.Fatal("duplicate ids accepted")
	}
}
