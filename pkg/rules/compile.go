package rules

import (
	"fmt"
	"strings"

	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
)

// Context provides everything the compiler needs to resolve identifiers.
type Context struct {
	Dialect    db.Dialect
	Collection *schema.Collection
	// Vars resolves @-placeholders (@request.auth.id, @request.data.x, @now).
	// Returning ok=false yields a nil (SQL NULL / unauthenticated) value.
	Vars func(name string) (any, bool)
	// Relations resolves a collection by name for relation traversal.
	Relations func(name string) *schema.Collection
	// HiddenAllowed permits referencing hidden fields (admin filters).
	HiddenAllowed bool
}

// Compile turns a parsed rule into a parameterized SQL condition scoped to
// ctx.Collection's table. A nil expr compiles to "1=1" (matches all).
func Compile(expr Expr, ctx *Context) (string, []any, error) {
	if expr == nil {
		return "1=1", nil, nil
	}
	c := &compiler{ctx: ctx}
	sql, err := c.compile(expr)
	if err != nil {
		return "", nil, err
	}
	return sql, c.args, nil
}

// CompileRule parses and compiles in one step.
func CompileRule(rule string, ctx *Context) (string, []any, error) {
	expr, err := Parse(rule)
	if err != nil {
		return "", nil, err
	}
	return Compile(expr, ctx)
}

type compiler struct {
	ctx  *Context
	args []any
}

// term is a compiled operand: either an SQL expression or a Go value.
type term struct {
	sql   string
	val   any
	isVal bool
	multi bool // JSON-array column or list value (affects ~ semantics)
}

func (c *compiler) compile(e Expr) (string, error) {
	switch t := e.(type) {
	case Logic:
		l, err := c.compile(t.L)
		if err != nil {
			return "", err
		}
		r, err := c.compile(t.R)
		if err != nil {
			return "", err
		}
		op := "AND"
		if t.Op == "||" {
			op = "OR"
		}
		return "(" + l + " " + op + " " + r + ")", nil
	case Cmp:
		return c.compileCmp(t)
	default:
		return "", fmt.Errorf("rules: expression must be a comparison")
	}
}

func (c *compiler) compileCmp(cmp Cmp) (string, error) {
	l, err := c.operand(cmp.L)
	if err != nil {
		return "", err
	}
	r, err := c.operand(cmp.R)
	if err != nil {
		return "", err
	}

	// Both sides are plain values → fold at compile time.
	if l.isVal && r.isVal {
		if evalCmp(cmp.Op, l.val, r.val) {
			return "1=1", nil
		}
		return "1=0", nil
	}

	switch cmp.Op {
	case "~", "!~":
		return c.compileLike(cmp.Op, l, r)
	case "=", "!=":
		return c.compileEq(cmp.Op, l, r)
	default:
		return c.binary(cmp.Op, l, r), nil
	}
}

// compileEq handles NULL-safe equality: empty/nil values match NULL or ”.
func (c *compiler) compileEq(op string, l, r term) (string, error) {
	// Normalize: column on the left when one side is a value.
	if l.isVal && !r.isVal {
		l, r = r, l
	}
	if !l.isVal && r.isVal {
		if r.val == nil || r.val == "" {
			// col = '' → matches NULL or ''
			cond := fmt.Sprintf("(%s IS NULL OR %s = '')", l.sql, l.sql)
			if op == "!=" {
				cond = fmt.Sprintf("(%s IS NOT NULL AND %s <> '')", l.sql, l.sql)
			}
			return cond, nil
		}
		ph := c.param(r.val)
		if op == "=" {
			return fmt.Sprintf("%s = %s", l.sql, ph), nil
		}
		// col != v → include NULL rows (SQL three-valued logic surprise)
		return fmt.Sprintf("(%s IS NULL OR %s <> %s)", l.sql, l.sql, ph), nil
	}
	// column vs column
	sqlOp := "="
	if op == "!=" {
		sqlOp = "<>"
	}
	return fmt.Sprintf("%s %s %s", l.sql, sqlOp, r.sql), nil
}

// compileLike implements `~` (contains, case-insensitive) and `!~`.
func (c *compiler) compileLike(op string, l, r term) (string, error) {
	not := ""
	if op == "!~" {
		not = "NOT "
	}
	like := c.ctx.Dialect.LikeOperator()
	esc := c.ctx.Dialect.LikeEscape()

	switch {
	case !l.isVal && r.isVal:
		pattern := "%" + escapeLike(db.ToString(r.val)) + "%"
		return fmt.Sprintf("%s%s %s %s%s", not, l.sql, like, c.param(pattern), esc), nil
	case l.isVal && !r.isVal:
		// value ~ column → does the value contain the column's text?
		return fmt.Sprintf("%s%s %s %s%s", not, c.param(db.ToString(l.val)), like,
			c.ctx.Dialect.Concat("'%'", r.sql, "'%'"), esc), nil
	default: // column ~ column
		return fmt.Sprintf("%s%s %s %s%s", not, l.sql, like,
			c.ctx.Dialect.Concat("'%'", r.sql, "'%'"), esc), nil
	}
}

func (c *compiler) binary(op string, l, r term) string {
	ls, rs := l.sql, r.sql
	if l.isVal {
		ls = c.param(l.val)
	}
	if r.isVal {
		rs = c.param(r.val)
	}
	return fmt.Sprintf("%s %s %s", ls, op, rs)
}

func (c *compiler) param(v any) string {
	c.args = append(c.args, v)
	return "?"
}

// operand resolves an AST leaf into a term.
func (c *compiler) operand(e Expr) (term, error) {
	switch t := e.(type) {
	case Lit:
		return term{val: t.Val, isVal: true}, nil
	case Ident:
		if strings.HasPrefix(t.Name, "@") {
			return c.resolveVar(t.Name)
		}
		return c.resolveField(t.Name)
	case Cmp, Logic:
		return term{}, fmt.Errorf("rules: nested expression not allowed as operand")
	}
	return term{}, fmt.Errorf("rules: invalid operand")
}

func (c *compiler) resolveVar(name string) (term, error) {
	if c.ctx.Vars == nil {
		return term{val: nil, isVal: true}, nil
	}
	v, ok := c.ctx.Vars(name)
	if !ok {
		return term{val: nil, isVal: true}, nil
	}
	return term{val: v, isVal: true}, nil
}

// resolveField maps an identifier to a column expression, following at most
// one relation hop (author.name).
func (c *compiler) resolveField(name string) (term, error) {
	col := c.ctx.Collection
	d := c.ctx.Dialect
	parts := strings.Split(name, ".")

	base := parts[0]
	if !col.HasColumn(base) {
		return term{}, fmt.Errorf("rules: unknown field %q", base)
	}
	f := col.Field(base)
	if f != nil && f.Hidden && !c.ctx.HiddenAllowed {
		return term{}, fmt.Errorf("rules: field %q is not accessible", base)
	}
	qualified := d.Quote(col.Name) + "." + d.Quote(base)

	if len(parts) == 1 {
		multi := f != nil && f.IsMultiple()
		return term{sql: qualified, multi: multi}, nil
	}
	if len(parts) > 2 {
		return term{}, fmt.Errorf("rules: %q — only single-hop relations are supported", name)
	}
	if f == nil || f.Type != schema.FieldRelation {
		return term{}, fmt.Errorf("rules: %q is not a relation field", base)
	}
	if c.ctx.Relations == nil {
		return term{}, fmt.Errorf("rules: relation resolver unavailable")
	}
	target := c.ctx.Relations(f.RelationCollection())
	if target == nil {
		return term{}, fmt.Errorf("rules: unknown relation collection %q", f.RelationCollection())
	}
	sub := parts[1]
	if !target.HasColumn(sub) {
		return term{}, fmt.Errorf("rules: unknown field %q on %q", sub, target.Name)
	}
	if tf := target.Field(sub); tf != nil && tf.Hidden && !c.ctx.HiddenAllowed {
		return term{}, fmt.Errorf("rules: field %q is not accessible", name)
	}

	tq := d.Quote(target.Name)
	if f.IsMultiple() {
		// JSON array containment via LIKE on '"<id>"'.
		return term{}, fmt.Errorf("rules: %q — dotted access through multi-relations is not supported yet; use %s ~ 'recordId'", name, base)
	}
	subquery := fmt.Sprintf("(SELECT %s.%s FROM %s WHERE %s.%s = %s LIMIT 1)",
		tq, d.Quote(sub), tq, tq, d.Quote("id"), qualified)
	return term{sql: subquery}, nil
}

// escapeLike escapes LIKE wildcards in a pattern chunk.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// evalCmp folds value-vs-value comparisons at compile time.
func evalCmp(op string, l, r any) bool {
	switch op {
	case "=":
		return looseEq(l, r)
	case "!=":
		return !looseEq(l, r)
	case "~":
		return looseContains(l, r)
	case "!~":
		return !looseContains(l, r)
	case ">", ">=", "<", "<=":
		lf, rf := db.ToFloat(l), db.ToFloat(r)
		ls, rs := db.ToString(l), db.ToString(r)
		_, lIsNum := l.(float64)
		_, rIsNum := r.(float64)
		if lIsNum || rIsNum {
			switch op {
			case ">":
				return lf > rf
			case ">=":
				return lf >= rf
			case "<":
				return lf < rf
			case "<=":
				return lf <= rf
			}
		}
		switch op {
		case ">":
			return ls > rs
		case ">=":
			return ls >= rs
		case "<":
			return ls < rs
		case "<=":
			return ls <= rs
		}
	}
	return false
}

func looseEq(l, r any) bool {
	if l == nil && r == nil {
		return true
	}
	if l == nil {
		return db.ToString(r) == ""
	}
	if r == nil {
		return db.ToString(l) == ""
	}
	return db.ToString(l) == db.ToString(r)
}

// looseContains: list contains element, or substring match (ci).
func looseContains(l, r any) bool {
	needle := strings.ToLower(db.ToString(r))
	if list := db.ToJSONList(l); len(list) > 1 || strings.HasPrefix(db.ToString(l), "[") {
		for _, item := range list {
			if strings.ToLower(item) == needle {
				return true
			}
		}
		return false
	}
	return strings.Contains(strings.ToLower(db.ToString(l)), needle)
}
