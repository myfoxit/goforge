// Package db wraps database/sql with a small dialect abstraction so the
// dynamic schema engine and query builders work identically on SQLite,
// PostgreSQL and MySQL/MariaDB.
//
// GoForge writes SQL with `?` placeholders; DB rebinds them for drivers
// that use positional parameters (PostgreSQL).
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DateFormat is the canonical timestamp representation stored in all
// databases: UTC, sortable, millisecond precision.
const DateFormat = "2006-01-02 15:04:05.000Z"

// Now returns the current UTC time formatted with DateFormat.
func Now() string { return time.Now().UTC().Format(DateFormat) }

// DB is a *sql.DB plus its dialect.
type DB struct {
	*sql.DB
	Dialect Dialect
}

// OpenFunc opens a ready *sql.DB for a driver-specific DSN.
// Driver subpackages (pkg/db/drivers/...) register themselves.
type OpenFunc func(dsn string) (*sql.DB, error)

var (
	openers  = map[string]OpenFunc{}
	dialects = map[string]func() Dialect{}
)

// Register wires a driver name to its opener and dialect. Called from the
// init() of driver subpackages.
func Register(name string, open OpenFunc, dialect func() Dialect) {
	openers[name] = open
	dialects[name] = dialect
}

// Drivers lists the compiled-in driver names.
func Drivers() []string {
	out := make([]string, 0, len(openers))
	for k := range openers {
		out = append(out, k)
	}
	return out
}

// Open connects using a registered driver ("sqlite", "postgres", "mysql").
func Open(driver, dsn string) (*DB, error) {
	open, ok := openers[driver]
	if !ok {
		return nil, fmt.Errorf(`db: driver %q not compiled in — import _ "github.com/myfoxit/goforge/pkg/db/drivers/%s" (available: %s)`,
			driver, driver, strings.Join(Drivers(), ", "))
	}
	sdb, err := open(dsn)
	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", driver, err)
	}
	if err := sdb.Ping(); err != nil {
		sdb.Close()
		return nil, fmt.Errorf("db: ping %s: %w", driver, err)
	}
	return &DB{DB: sdb, Dialect: dialects[driver]()}, nil
}

// Rebind converts `?` placeholders to the dialect's positional form.
// Generated SQL never contains a literal `?` outside placeholders
// (all values are parameterized), so a simple scan is safe.
func (d *DB) Rebind(query string) string {
	if d.Dialect.Placeholder(1) == "?" {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for i := 0; i < len(query); i++ {
		if query[i] == '?' {
			n++
			b.WriteString(d.Dialect.Placeholder(n))
		} else {
			b.WriteByte(query[i])
		}
	}
	return b.String()
}

// Exec runs a statement after rebinding placeholders.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.DB.ExecContext(ctx, d.Rebind(query), args...)
}

// Query runs a query after rebinding placeholders.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(ctx, d.Rebind(query), args...)
}

// QueryRow runs a single-row query after rebinding placeholders.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.DB.QueryRowContext(ctx, d.Rebind(query), args...)
}

// Tx runs fn inside a transaction, committing on nil error.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// RowMap scans the current row of rows into a map keyed by column name.
// Values are normalized: []byte→string, int64 stays int64, nil stays nil.
func RowMap(rows *sql.Rows) (map[string]any, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	m := make(map[string]any, len(cols))
	for i, c := range cols {
		m[c] = normalize(vals[i])
	}
	return m, nil
}

// QueryMaps runs a query and scans every row into a map.
func (d *DB) QueryMaps(ctx context.Context, query string, args ...any) ([]map[string]any, error) {
	rows, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		m, err := RowMap(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// QueryMap runs a query expected to return at most one row.
// Returns (nil, nil) when no row matches.
func (d *DB) QueryMap(ctx context.Context, query string, args ...any) (map[string]any, error) {
	rows, err := d.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, rows.Err()
	}
	return RowMap(rows)
}

func normalize(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case time.Time:
		return t.UTC().Format(DateFormat)
	default:
		return v
	}
}

// ToBool coerces driver-specific boolean representations.
func ToBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case float64:
		return t != 0
	case string:
		b, _ := strconv.ParseBool(t)
		return b || t == "1"
	default:
		return false
	}
}

// ToFloat coerces numeric driver values.
func ToFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	default:
		return 0
	}
}

// ToString coerces a driver value to its string form.
func ToString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprint(t)
	}
}

// ToJSONList parses a JSON array column value into []string.
// Accepts nil, "", single scalar strings and JSON arrays.
func ToJSONList(v any) []string {
	s := ToString(v)
	if s == "" || s == "null" {
		return nil
	}
	if strings.HasPrefix(s, "[") {
		var out []string
		if err := json.Unmarshal([]byte(s), &out); err == nil {
			return out
		}
		var anyOut []any
		if err := json.Unmarshal([]byte(s), &anyOut); err == nil {
			strs := make([]string, 0, len(anyOut))
			for _, item := range anyOut {
				strs = append(strs, ToString(item))
			}
			return strs
		}
		return nil
	}
	return []string{s}
}
