package schema

import (
	"context"
	"testing"

	"github.com/myfoxit/goforge/pkg/db"
	_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
)

func newTestRegistry(t *testing.T) (*Registry, *db.DB) {
	t.Helper()
	d, err := db.Open("sqlite", "file:"+t.Name()+"?mode=memory&cache=shared")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	r := NewRegistry(d, nil)
	if err := r.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	return r, d
}

func str(s string) *string { return &s }

func TestCollectionLifecycle(t *testing.T) {
	r, d := newTestRegistry(t)
	ctx := context.Background()

	posts := &Collection{
		Name: "posts",
		Type: TypeBase,
		Fields: []*Field{
			{Name: "title", Type: FieldText, Required: true},
			{Name: "views", Type: FieldNumber},
			{Name: "slug", Type: FieldText, Unique: true},
		},
		ListRule: str(""),
	}
	if err := r.Save(ctx, posts); err != nil {
		t.Fatal(err)
	}
	if posts.ID == "" || posts.Field("title").ID == "" {
		t.Fatal("ids not assigned")
	}

	// Table usable?
	if _, err := d.Exec(ctx, `INSERT INTO posts (id, created, updated, title, views, slug) VALUES ('p1', ?, ?, 'Hello', 3, 'hello')`, db.Now(), db.Now()); err != nil {
		t.Fatal(err)
	}

	// Unique index enforced?
	if _, err := d.Exec(ctx, `INSERT INTO posts (id, created, updated, title, views, slug) VALUES ('p2', ?, ?, 'Other', 0, 'hello')`, db.Now(), db.Now()); err == nil {
		t.Fatal("unique slug violated without error")
	}

	// Add + rename + drop fields.
	upd := r.Get("posts").Clone()
	upd.Fields = append(upd.Fields, &Field{Name: "body", Type: FieldEditor})
	title := upd.Field("title")
	title.Name = "headline"
	var kept []*Field
	for _, f := range upd.Fields {
		if f.Name != "views" {
			kept = append(kept, f)
		}
	}
	upd.Fields = kept
	if err := r.Save(ctx, upd); err != nil {
		t.Fatal(err)
	}

	row, err := d.QueryMap(ctx, "SELECT headline, body FROM posts WHERE id = 'p1'")
	if err != nil {
		t.Fatalf("renamed column unusable: %v", err)
	}
	if db.ToString(row["headline"]) != "Hello" {
		t.Fatalf("rename lost data: %v", row)
	}
	if _, err := d.QueryMap(ctx, "SELECT views FROM posts LIMIT 1"); err == nil {
		t.Fatal("dropped column still present")
	}

	// Reload from persistence.
	r2 := NewRegistry(d, nil)
	if err := r2.Init(ctx); err != nil {
		t.Fatal(err)
	}
	got := r2.Get("posts")
	if got == nil || got.Field("headline") == nil || got.Field("views") != nil {
		t.Fatalf("persisted definition wrong: %+v", got)
	}

	// Delete.
	if err := r.Delete(ctx, "posts"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.QueryMap(ctx, "SELECT 1 FROM posts LIMIT 1"); err == nil {
		t.Fatal("table not dropped")
	}
}

func TestRelationGuards(t *testing.T) {
	r, _ := newTestRegistry(t)
	ctx := context.Background()

	if err := r.Save(ctx, &Collection{Name: "authors", Type: TypeBase,
		Fields: []*Field{{Name: "name", Type: FieldText}}}); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, &Collection{Name: "books", Type: TypeBase,
		Fields: []*Field{{Name: "author", Type: FieldRelation, Options: map[string]any{"collection": "authors"}}}}); err != nil {
		t.Fatal(err)
	}

	// Unknown relation target rejected.
	err := r.Save(ctx, &Collection{Name: "bad", Type: TypeBase,
		Fields: []*Field{{Name: "x", Type: FieldRelation, Options: map[string]any{"collection": "ghost"}}}})
	if err == nil {
		t.Fatal("unknown relation accepted")
	}

	// Referenced collection cannot be deleted.
	if err := r.Delete(ctx, "authors"); err == nil {
		t.Fatal("referenced collection deleted")
	}
	if err := r.Delete(ctx, "books"); err != nil {
		t.Fatal(err)
	}
	if err := r.Delete(ctx, "authors"); err != nil {
		t.Fatal(err)
	}
}

func TestViewCollections(t *testing.T) {
	r, d := newTestRegistry(t)
	ctx := context.Background()

	if err := r.Save(ctx, &Collection{Name: "nums", Type: TypeBase,
		Fields: []*Field{{Name: "n", Type: FieldNumber}}}); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"a", "b", "c"} {
		if _, err := d.Exec(ctx, "INSERT INTO nums (id, created, updated, n) VALUES (?, ?, ?, ?)", id, db.Now(), db.Now(), i); err != nil {
			t.Fatal(err)
		}
	}

	view := &Collection{Name: "stats", Type: TypeView,
		Options: map[string]any{"query": "SELECT id, n * 2 AS doubled FROM nums"}}
	if err := r.Save(ctx, view); err != nil {
		t.Fatal(err)
	}
	if view.Field("doubled") == nil {
		t.Fatalf("view fields not introspected: %+v", view.Fields)
	}

	// Non-SELECT rejected.
	if err := r.Save(ctx, &Collection{Name: "evil", Type: TypeView,
		Options: map[string]any{"query": "DELETE FROM nums"}}); err == nil {
		t.Fatal("non-select view accepted")
	}
}

func TestFieldNormalization(t *testing.T) {
	cases := []struct {
		f       Field
		in      any
		want    any
		wantErr bool
	}{
		{Field{Name: "t", Type: FieldText, Options: map[string]any{"max": float64(3)}}, "abcd", nil, true},
		{Field{Name: "t", Type: FieldText}, "ok", "ok", false},
		{Field{Name: "t", Type: FieldText, Required: true}, "", nil, true},
		{Field{Name: "n", Type: FieldNumber}, "12.5", 12.5, false},
		{Field{Name: "n", Type: FieldNumber, Options: map[string]any{"min": float64(5)}}, float64(3), nil, true},
		{Field{Name: "b", Type: FieldBool}, true, true, false},
		{Field{Name: "e", Type: FieldEmail}, "Foo@Bar.com", "foo@bar.com", false},
		{Field{Name: "e", Type: FieldEmail}, "not-an-email", nil, true},
		{Field{Name: "u", Type: FieldURL}, "https://x.dev/a", "https://x.dev/a", false},
		{Field{Name: "u", Type: FieldURL}, "ftp://x.dev", nil, true},
		{Field{Name: "d", Type: FieldDate}, "2025-06-01", "2025-06-01 00:00:00.000Z", false},
		{Field{Name: "s", Type: FieldSelect, Options: map[string]any{"values": []any{"a", "b"}}}, "a", "a", false},
		{Field{Name: "s", Type: FieldSelect, Options: map[string]any{"values": []any{"a", "b"}}}, "z", nil, true},
		{Field{Name: "s", Type: FieldSelect, Options: map[string]any{"values": []any{"a", "b"}, "maxSelect": float64(2)}}, []any{"a", "b"}, `["a","b"]`, false},
		{Field{Name: "r", Type: FieldRelation, Options: map[string]any{"collection": "x"}}, "rec1", "rec1", false},
		{Field{Name: "j", Type: FieldJSON}, map[string]any{"k": "v"}, `{"k":"v"}`, false},
	}
	for i, tc := range cases {
		got, err := tc.f.NormalizeValue(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("case %d (%s=%v): expected error, got %v", i, tc.f.Type, tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("case %d (%s=%v): %v", i, tc.f.Type, tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("case %d (%s): got %v (%T), want %v", i, tc.f.Type, got, got, tc.want)
		}
	}
}

func TestAPIValue(t *testing.T) {
	multi := Field{Name: "s", Type: FieldSelect, Options: map[string]any{"values": []any{"a"}, "maxSelect": float64(2)}}
	got := multi.APIValue(`["a","b"]`)
	if list, ok := got.([]string); !ok || len(list) != 2 {
		t.Fatalf("multi APIValue = %v", got)
	}
	b := Field{Name: "b", Type: FieldBool}
	if b.APIValue(int64(1)) != true {
		t.Fatal("bool APIValue")
	}
	j := Field{Name: "j", Type: FieldJSON}
	if m, ok := j.APIValue(`{"x":1}`).(map[string]any); !ok || m["x"] != float64(1) {
		t.Fatal("json APIValue")
	}
}
