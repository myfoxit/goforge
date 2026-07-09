// Package postgres compiles in the pgx driver and registers the postgres
// dialect. Import it for effect:
//
//	import _ "github.com/myfoxit/goforge/pkg/db/drivers/postgres"
//
// DSN example: postgres://user:pass@localhost:5432/myapp?sslmode=disable
package postgres

import (
	"database/sql"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/myfoxit/goforge/pkg/db"
)

func init() {
	db.Register("postgres", open, func() db.Dialect { return db.PostgresDialect{} })
}

func open(dsn string) (*sql.DB, error) {
	sdb, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(25)
	sdb.SetMaxIdleConns(5)
	return sdb, nil
}
