// Package sqlite compiles in the CGO-free SQLite driver (modernc.org/sqlite)
// and registers the sqlite dialect. Import it for effect:
//
//	import _ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
package sqlite

import (
	"database/sql"
	"net/url"
	"strings"

	"github.com/myfoxit/goforge/pkg/db"
	_ "modernc.org/sqlite"
)

func init() {
	db.Register("sqlite", open, func() db.Dialect { return db.SQLiteDialect{} })
}

// open normalizes a plain file path into a tuned DSN: WAL journal,
// busy timeout and NORMAL sync — the right defaults for a web app.
func open(dsn string) (*sql.DB, error) {
	if dsn == "" {
		dsn = "data.db"
	}
	if !strings.Contains(dsn, "?") && !strings.HasPrefix(dsn, "file:") {
		dsn = "file:" + dsn + "?" + url.Values{
			"_pragma": []string{
				"busy_timeout(10000)",
				"journal_mode(WAL)",
				"synchronous(NORMAL)",
				"foreign_keys(0)",
				"temp_store(MEMORY)",
			},
			"_time_format": []string{"sqlite"},
		}.Encode()
	}
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single writer avoids SQLITE_BUSY under concurrent writes.
	sdb.SetMaxOpenConns(4)
	sdb.SetMaxIdleConns(4)
	return sdb, nil
}
