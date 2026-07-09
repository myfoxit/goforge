package rules

import (
	"strings"
	"testing"

	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
)

func testCollection() *schema.Collection {
	return &schema.Collection{
		ID: "col1", Name: "posts", Type: schema.TypeBase,
		Fields: []*schema.Field{
			{ID: "f1", Name: "title", Type: schema.FieldText},
			{ID: "f2", Name: "views", Type: schema.FieldNumber},
			{ID: "f3", Name: "published", Type: schema.FieldBool},
			{ID: "f4", Name: "owner", Type: schema.FieldRelation, Options: map[string]any{"collection": "users"}},
			{ID: "f5", Name: "tags", Type: schema.FieldSelect, Options: map[string]any{"values": []any{"a", "b"}, "maxSelect": float64(3)}},
			{ID: "f6", Name: "secret", Type: schema.FieldText, Hidden: true},
		},
	}
}

func usersCollection() *schema.Collection {
	return &schema.Collection{
		ID: "col2", Name: "users", Type: schema.TypeAuth,
		Fields: []*schema.Field{
			{ID: "u1", Name: "email", Type: schema.FieldEmail},
			{ID: "u2", Name: "plan", Type: schema.FieldText},
			{ID: "u3", Name: "password", Type: schema.FieldPassword, Hidden: true},
		},
	}
}

func ctx(vars map[string]any) *Context {
	return &Context{
		Dialect:    db.SQLiteDialect{},
		Collection: testCollection(),
		Vars: func(name string) (any, bool) {
			v, ok := vars[name]
			return v, ok
		},
		Relations: func(name string) *schema.Collection {
			if name == "users" {
				return usersCollection()
			}
			return nil
		},
	}
}

func TestParseErrors(t *testing.T) {
	bad := []string{
		"title =",
		"= 'x'",
		"title = 'x' &&",
		"title == 'x' extra",
		"(title = 'x'",
		"title ! 'x'",
		"title = 'unterminated",
		"title & 'x'",
	}
	for _, in := range bad {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) should fail", in)
		}
	}
	if expr, err := Parse("   "); err != nil || expr != nil {
		t.Errorf("blank rule should parse to nil")
	}
}

func TestCompileBasics(t *testing.T) {
	cases := []struct {
		rule     string
		vars     map[string]any
		wantSQL  string
		wantArgs []any
	}{
		{
			rule:     "title = 'hello'",
			wantSQL:  `"posts"."title" = ?`,
			wantArgs: []any{"hello"},
		},
		{
			rule:     "views > 100",
			wantSQL:  `"posts"."views" > ?`,
			wantArgs: []any{float64(100)},
		},
		{
			rule:     "published = true",
			wantSQL:  `"posts"."published" = ?`,
			wantArgs: []any{true},
		},
		{
			rule:    "title != ''",
			wantSQL: `("posts"."title" IS NOT NULL AND "posts"."title" <> '')`,
		},
		{
			rule:    "owner = null",
			wantSQL: `("posts"."owner" IS NULL OR "posts"."owner" = '')`,
		},
		{
			rule:     "owner = @request.auth.id",
			vars:     map[string]any{"@request.auth.id": "user123"},
			wantSQL:  `"posts"."owner" = ?`,
			wantArgs: []any{"user123"},
		},
		{
			rule:    "owner = @request.auth.id",
			vars:    map[string]any{}, // unauthenticated → nil → IS NULL
			wantSQL: `("posts"."owner" IS NULL OR "posts"."owner" = '')`,
		},
		{
			rule:     "title ~ 'go%_'",
			wantSQL:  `"posts"."title" LIKE ? ESCAPE '\'`,
			wantArgs: []any{`%go\%\_%`},
		},
		{
			rule:     "tags ~ 'a'",
			wantSQL:  `"posts"."tags" LIKE ? ESCAPE '\'`,
			wantArgs: []any{"%a%"},
		},
		{
			rule:     "title = 'a' && views >= 5 || published = false",
			wantSQL:  `(("posts"."title" = ? AND "posts"."views" >= ?) OR "posts"."published" = ?)`,
			wantArgs: []any{"a", float64(5), false},
		},
		{
			rule:    "owner.plan = 'pro'",
			wantSQL: `(SELECT "users"."plan" FROM "users" WHERE "users"."id" = "posts"."owner" LIMIT 1) = ?`,
		},
	}
	for _, tc := range cases {
		sql, args, err := CompileRule(tc.rule, ctx(tc.vars))
		if err != nil {
			t.Errorf("CompileRule(%q): %v", tc.rule, err)
			continue
		}
		if sql != tc.wantSQL {
			t.Errorf("CompileRule(%q)\n got %s\nwant %s", tc.rule, sql, tc.wantSQL)
		}
		if tc.wantArgs != nil {
			if len(args) != len(tc.wantArgs) {
				t.Errorf("CompileRule(%q) args = %v, want %v", tc.rule, args, tc.wantArgs)
				continue
			}
			for i := range args {
				if args[i] != tc.wantArgs[i] {
					t.Errorf("CompileRule(%q) arg[%d] = %v, want %v", tc.rule, i, args[i], tc.wantArgs[i])
				}
			}
		}
	}
}

func TestCompileFolding(t *testing.T) {
	// value-vs-value comparisons fold at compile time
	cases := map[string]string{
		"@request.auth.id != ''":        "1=0", // unauthenticated
		"1 = 1":                         "1=1",
		"'a' = 'b'":                     "1=0",
		"@request.auth.roles ~ 'admin'": "1=1",
		"@request.auth.roles ~ 'nope'":  "1=0",
		"@request.data.status = 'ok'":   "1=1",
	}
	vars := map[string]any{
		"@request.auth.roles":  `["admin","editor"]`,
		"@request.data.status": "ok",
	}
	for rule, want := range cases {
		sql, _, err := CompileRule(rule, ctx(vars))
		if err != nil {
			t.Errorf("CompileRule(%q): %v", rule, err)
			continue
		}
		if sql != want {
			t.Errorf("CompileRule(%q) = %s, want %s", rule, sql, want)
		}
	}

	// authenticated variant
	sql, _, _ := CompileRule("@request.auth.id != ''", ctx(map[string]any{"@request.auth.id": "u1"}))
	if sql != "1=1" {
		t.Errorf("authenticated fold = %s", sql)
	}
}

func TestCompileErrors(t *testing.T) {
	bad := []string{
		"unknown = 'x'",
		"secret = 'x'",          // hidden field
		"owner.password = 'x'",  // hidden field via relation
		"owner.nope = 'x'",      // unknown relation field
		"title.sub = 'x'",       // not a relation
		"owner.plan.deep = 'x'", // two hops
	}
	for _, rule := range bad {
		if _, _, err := CompileRule(rule, ctx(nil)); err == nil {
			t.Errorf("CompileRule(%q) should fail", rule)
		}
	}
	// hidden allowed for admin contexts
	c := ctx(nil)
	c.HiddenAllowed = true
	if _, _, err := CompileRule("secret = 'x'", c); err != nil {
		t.Errorf("hidden with HiddenAllowed: %v", err)
	}
}

func TestMySQLConcatLike(t *testing.T) {
	c := ctx(nil)
	c.Dialect = db.MySQLDialect{}
	sql, _, err := CompileRule("'needle' ~ title", c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "CONCAT('%'") {
		t.Errorf("mysql concat missing: %s", sql)
	}
}
