package db

import (
	"fmt"
	"strings"
)

// ColKind is the small set of storage kinds the dynamic schema maps onto.
// Higher-level field types (email, url, select, relation, file, ...) all
// reduce to one of these.
type ColKind int

const (
	// ColID is a short indexed text primary/foreign key column.
	ColID ColKind = iota
	// ColText is arbitrary-length text.
	ColText
	// ColNumber is a double-precision float.
	ColNumber
	// ColBool is a boolean.
	ColBool
	// ColJSON is JSON stored as text (portable across all three engines).
	ColJSON
	// ColDateTime is a UTC timestamp stored as sortable text (DateFormat).
	ColDateTime
)

// ColumnDef describes one column for DDL generation.
type ColumnDef struct {
	Name    string
	Kind    ColKind
	PK      bool
	NotNull bool
	Default string // raw SQL literal, already escaped (used for simple defaults)
}

// IndexDef describes an index for DDL generation.
type IndexDef struct {
	Name    string
	Table   string
	Columns []string
	Unique  bool
}

// Dialect abstracts the SQL differences between engines.
type Dialect interface {
	Name() string
	// Quote quotes an identifier (table/column name).
	Quote(ident string) string
	// Placeholder returns the n-th (1-based) parameter placeholder.
	Placeholder(n int) string
	// ColumnType maps a ColKind to the engine column type.
	ColumnType(kind ColKind) string
	// CreateTable renders CREATE TABLE IF NOT EXISTS.
	CreateTable(table string, cols []ColumnDef) string
	// AddColumn renders ALTER TABLE ... ADD COLUMN.
	AddColumn(table string, col ColumnDef) string
	// DropColumn renders ALTER TABLE ... DROP COLUMN.
	DropColumn(table, col string) string
	// RenameColumn renders a column rename.
	RenameColumn(table, oldName, newName string) string
	// RenameTable renders a table rename.
	RenameTable(oldName, newName string) string
	// CreateIndex renders CREATE [UNIQUE] INDEX IF NOT EXISTS. The dialect
	// handles engine quirks (MySQL text prefix lengths).
	CreateIndex(idx IndexDef, kinds map[string]ColKind) string
	// DropIndex renders DROP INDEX.
	DropIndex(name, table string) string
	// DropTable renders DROP TABLE IF EXISTS.
	DropTable(table string) string
	// TableExistsQuery returns a query with one `?` param (table name)
	// that yields at least one row when the table exists.
	TableExistsQuery() string
	// JSONExtract renders an expression extracting a JSON object key as text
	// from a ColJSON column.
	JSONExtract(col, key string) string
	// LikeOperator returns the case-insensitive LIKE operator.
	LikeOperator() string
	// LikeEscape returns the ESCAPE clause (SQL literal for backslash).
	LikeEscape() string
	// Concat renders string concatenation of the given SQL expressions.
	Concat(parts ...string) string
}

// renderColumn is shared DDL column rendering.
func renderColumn(d Dialect, c ColumnDef) string {
	var b strings.Builder
	b.WriteString(d.Quote(c.Name))
	b.WriteByte(' ')
	b.WriteString(d.ColumnType(c.Kind))
	if c.PK {
		b.WriteString(" PRIMARY KEY")
	}
	if c.NotNull {
		b.WriteString(" NOT NULL")
	}
	if c.Default != "" {
		b.WriteString(" DEFAULT ")
		b.WriteString(c.Default)
	}
	return b.String()
}

func renderCreateTable(d Dialect, table string, cols []ColumnDef, suffix string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = renderColumn(d, c)
	}
	return fmt.Sprintf("CREATE TABLE IF NOT EXISTS %s (%s)%s", d.Quote(table), strings.Join(parts, ", "), suffix)
}

// ---- SQLite ----

type SQLiteDialect struct{}

func (SQLiteDialect) Name() string           { return "sqlite" }
func (SQLiteDialect) Quote(s string) string  { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func (SQLiteDialect) Placeholder(int) string { return "?" }
func (SQLiteDialect) LikeOperator() string   { return "LIKE" } // sqlite LIKE is case-insensitive for ASCII
func (SQLiteDialect) ColumnType(k ColKind) string {
	switch k {
	case ColNumber:
		return "REAL"
	case ColBool:
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}
func (d SQLiteDialect) CreateTable(t string, cols []ColumnDef) string {
	return renderCreateTable(d, t, cols, "")
}
func (d SQLiteDialect) AddColumn(t string, c ColumnDef) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", d.Quote(t), renderColumn(d, c))
}
func (d SQLiteDialect) DropColumn(t, c string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", d.Quote(t), d.Quote(c))
}
func (d SQLiteDialect) RenameColumn(t, o, n string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(o), d.Quote(n))
}
func (d SQLiteDialect) RenameTable(o, n string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.Quote(o), d.Quote(n))
}
func (d SQLiteDialect) CreateIndex(idx IndexDef, _ map[string]ColKind) string {
	return renderCreateIndex(d, idx, nil)
}
func (d SQLiteDialect) DropIndex(name, _ string) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", d.Quote(name))
}
func (d SQLiteDialect) DropTable(t string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", d.Quote(t))
}
func (SQLiteDialect) TableExistsQuery() string {
	return "SELECT 1 FROM sqlite_master WHERE type IN ('table','view') AND name = ?"
}
func (d SQLiteDialect) JSONExtract(col, key string) string {
	return fmt.Sprintf("json_extract(%s, '$.%s')", d.Quote(col), key)
}
func (SQLiteDialect) LikeEscape() string { return ` ESCAPE '\'` }
func (SQLiteDialect) Concat(parts ...string) string {
	return "(" + strings.Join(parts, " || ") + ")"
}

// ---- PostgreSQL ----

type PostgresDialect struct{}

func (PostgresDialect) Name() string             { return "postgres" }
func (PostgresDialect) Quote(s string) string    { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func (PostgresDialect) Placeholder(n int) string { return fmt.Sprintf("$%d", n) }
func (PostgresDialect) LikeOperator() string     { return "ILIKE" }
func (PostgresDialect) ColumnType(k ColKind) string {
	switch k {
	case ColNumber:
		return "DOUBLE PRECISION"
	case ColBool:
		return "BOOLEAN"
	default:
		return "TEXT"
	}
}
func (d PostgresDialect) CreateTable(t string, cols []ColumnDef) string {
	return renderCreateTable(d, t, cols, "")
}
func (d PostgresDialect) AddColumn(t string, c ColumnDef) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN IF NOT EXISTS %s", d.Quote(t), renderColumn(d, c))
}
func (d PostgresDialect) DropColumn(t, c string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN IF EXISTS %s", d.Quote(t), d.Quote(c))
}
func (d PostgresDialect) RenameColumn(t, o, n string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(o), d.Quote(n))
}
func (d PostgresDialect) RenameTable(o, n string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME TO %s", d.Quote(o), d.Quote(n))
}
func (d PostgresDialect) CreateIndex(idx IndexDef, _ map[string]ColKind) string {
	return renderCreateIndex(d, idx, nil)
}
func (d PostgresDialect) DropIndex(name, _ string) string {
	return fmt.Sprintf("DROP INDEX IF EXISTS %s", d.Quote(name))
}
func (d PostgresDialect) DropTable(t string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", d.Quote(t))
}
func (PostgresDialect) TableExistsQuery() string {
	return "SELECT 1 FROM information_schema.tables WHERE table_schema = current_schema() AND table_name = ?"
}
func (d PostgresDialect) JSONExtract(col, key string) string {
	return fmt.Sprintf("(%s::jsonb ->> '%s')", d.Quote(col), key)
}
func (PostgresDialect) LikeEscape() string { return ` ESCAPE '\'` }
func (PostgresDialect) Concat(parts ...string) string {
	return "(" + strings.Join(parts, " || ") + ")"
}

// ---- MySQL / MariaDB ----

type MySQLDialect struct{}

func (MySQLDialect) Name() string           { return "mysql" }
func (MySQLDialect) Quote(s string) string  { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }
func (MySQLDialect) Placeholder(int) string { return "?" }
func (MySQLDialect) LikeOperator() string   { return "LIKE" } // ci by default collation
func (MySQLDialect) ColumnType(k ColKind) string {
	switch k {
	case ColID:
		return "VARCHAR(64)"
	case ColNumber:
		return "DOUBLE"
	case ColBool:
		return "TINYINT(1)"
	case ColDateTime:
		return "VARCHAR(32)"
	default:
		return "LONGTEXT"
	}
}
func (d MySQLDialect) CreateTable(t string, cols []ColumnDef) string {
	return renderCreateTable(d, t, cols, " ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci")
}
func (d MySQLDialect) AddColumn(t string, c ColumnDef) string {
	return fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s", d.Quote(t), renderColumn(d, c))
}
func (d MySQLDialect) DropColumn(t, c string) string {
	return fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", d.Quote(t), d.Quote(c))
}
func (d MySQLDialect) RenameColumn(t, o, n string) string {
	return fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO %s", d.Quote(t), d.Quote(o), d.Quote(n))
}
func (d MySQLDialect) RenameTable(o, n string) string {
	return fmt.Sprintf("RENAME TABLE %s TO %s", d.Quote(o), d.Quote(n))
}
func (d MySQLDialect) CreateIndex(idx IndexDef, kinds map[string]ColKind) string {
	// LONGTEXT columns need a prefix length in MySQL indexes.
	prefix := func(col string) string {
		if k, ok := kinds[col]; ok {
			if k == ColText || k == ColJSON {
				return "(191)"
			}
		}
		return ""
	}
	return renderCreateIndex(d, idx, prefix)
}
func (d MySQLDialect) DropIndex(name, table string) string {
	return fmt.Sprintf("DROP INDEX %s ON %s", d.Quote(name), d.Quote(table))
}
func (d MySQLDialect) DropTable(t string) string {
	return fmt.Sprintf("DROP TABLE IF EXISTS %s", d.Quote(t))
}
func (MySQLDialect) TableExistsQuery() string {
	return "SELECT 1 FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = ?"
}
func (d MySQLDialect) JSONExtract(col, key string) string {
	return fmt.Sprintf("JSON_UNQUOTE(JSON_EXTRACT(%s, '$.%s'))", d.Quote(col), key)
}
func (MySQLDialect) LikeEscape() string { return ` ESCAPE '\\'` }
func (MySQLDialect) Concat(parts ...string) string {
	return "CONCAT(" + strings.Join(parts, ", ") + ")"
}

func renderCreateIndex(d Dialect, idx IndexDef, prefix func(string) string) string {
	cols := make([]string, len(idx.Columns))
	for i, c := range idx.Columns {
		cols[i] = d.Quote(c)
		if prefix != nil {
			cols[i] += prefix(c)
		}
	}
	unique := ""
	if idx.Unique {
		unique = "UNIQUE "
	}
	ifNotExists := "IF NOT EXISTS "
	if d.Name() == "mysql" {
		ifNotExists = "" // MariaDB supports it, MySQL 8 does not; callers tolerate dup errors
	}
	return fmt.Sprintf("CREATE %sINDEX %s%s ON %s (%s)",
		unique, ifNotExists, d.Quote(idx.Name), d.Quote(idx.Table), strings.Join(cols, ", "))
}
