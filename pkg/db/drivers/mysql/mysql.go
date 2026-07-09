// Package mysql compiles in the MySQL/MariaDB driver and registers the
// mysql dialect. Import it for effect:
//
//	import _ "github.com/myfoxit/goforge/pkg/db/drivers/mysql"
//
// DSN example: user:pass@tcp(localhost:3306)/myapp?parseTime=true
package mysql

import (
	"database/sql"
	"strings"

	_ "github.com/go-sql-driver/mysql"
	"github.com/myfoxit/goforge/pkg/db"
)

func init() {
	db.Register("mysql", open, func() db.Dialect { return db.MySQLDialect{} })
}

func open(dsn string) (*sql.DB, error) {
	// Ensure sane charset + parseTime defaults without overriding user choices.
	if !strings.Contains(dsn, "charset=") {
		dsn = appendParam(dsn, "charset=utf8mb4")
	}
	if !strings.Contains(dsn, "parseTime=") {
		dsn = appendParam(dsn, "parseTime=true")
	}
	sdb, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(25)
	sdb.SetMaxIdleConns(5)
	return sdb, nil
}

func appendParam(dsn, param string) string {
	if strings.Contains(dsn, "?") {
		return dsn + "&" + param
	}
	return dsn + "?" + param
}
