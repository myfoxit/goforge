package db

import (
	"strings"
	"testing"
)

func TestRebind(t *testing.T) {
	pg := &DB{Dialect: PostgresDialect{}}
	got := pg.Rebind("SELECT * FROM t WHERE a = ? AND b = ?")
	want := "SELECT * FROM t WHERE a = $1 AND b = $2"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	lite := &DB{Dialect: SQLiteDialect{}}
	if got := lite.Rebind("a = ?"); got != "a = ?" {
		t.Fatalf("sqlite rebind changed query: %q", got)
	}
}

func TestDialectDDL(t *testing.T) {
	cols := []ColumnDef{
		{Name: "id", Kind: ColID, PK: true},
		{Name: "title", Kind: ColText, NotNull: true, Default: "''"},
		{Name: "count", Kind: ColNumber},
		{Name: "done", Kind: ColBool},
		{Name: "meta", Kind: ColJSON},
		{Name: "created", Kind: ColDateTime},
	}
	cases := []struct {
		d        Dialect
		contains []string
	}{
		{SQLiteDialect{}, []string{`"id" TEXT PRIMARY KEY`, `"count" REAL`, `"done" BOOLEAN`, `"title" TEXT NOT NULL DEFAULT ''`}},
		{PostgresDialect{}, []string{`"id" TEXT PRIMARY KEY`, `"count" DOUBLE PRECISION`, `"done" BOOLEAN`}},
		{MySQLDialect{}, []string{"`id` VARCHAR(64) PRIMARY KEY", "`count` DOUBLE", "`done` TINYINT(1)", "ENGINE=InnoDB"}},
	}
	for _, c := range cases {
		sql := c.d.CreateTable("posts", cols)
		for _, want := range c.contains {
			if !strings.Contains(sql, want) {
				t.Errorf("%s: %q missing %q", c.d.Name(), sql, want)
			}
		}
	}
}

func TestMySQLIndexPrefix(t *testing.T) {
	d := MySQLDialect{}
	sql := d.CreateIndex(IndexDef{Name: "idx_posts_title", Table: "posts", Columns: []string{"title", "id"}, Unique: true},
		map[string]ColKind{"title": ColText, "id": ColID})
	if !strings.Contains(sql, "`title`(191)") {
		t.Fatalf("missing text prefix: %s", sql)
	}
	if strings.Contains(sql, "`id`(191)") {
		t.Fatalf("id should not get prefix: %s", sql)
	}
	if !strings.HasPrefix(sql, "CREATE UNIQUE INDEX") {
		t.Fatalf("not unique: %s", sql)
	}
}

func TestQuoteEscaping(t *testing.T) {
	if got := (SQLiteDialect{}).Quote(`we"ird`); got != `"we""ird"` {
		t.Fatalf("sqlite quote = %s", got)
	}
	if got := (MySQLDialect{}).Quote("we`ird"); got != "`we``ird`" {
		t.Fatalf("mysql quote = %s", got)
	}
}

func TestCoercions(t *testing.T) {
	if !ToBool(int64(1)) || ToBool("0") || !ToBool("1") || !ToBool(true) {
		t.Fatal("ToBool")
	}
	if ToFloat("3.5") != 3.5 || ToFloat(int64(2)) != 2 {
		t.Fatal("ToFloat")
	}
	if got := ToJSONList(`["a","b"]`); len(got) != 2 || got[0] != "a" {
		t.Fatalf("ToJSONList = %v", got)
	}
	if got := ToJSONList("solo"); len(got) != 1 || got[0] != "solo" {
		t.Fatalf("ToJSONList scalar = %v", got)
	}
	if got := ToJSONList(""); got != nil {
		t.Fatalf("ToJSONList empty = %v", got)
	}
}
