package apis

import (
	"context"
	"fmt"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/rules"
	"github.com/myfoxit/goforge/pkg/schema"
)

const maxExpandDepth = 3

// expand resolves relation fields into nested records under "expand".
// Spec: comma-separated field paths, dots for nesting ("author,comments.user").
// Expanded records respect the target collection's view rule.
func (s *Records) expand(ctx context.Context, c *schema.Collection, items []map[string]any, spec string, req *Request) error {
	if len(items) == 0 {
		return nil
	}
	for _, path := range strings.Split(spec, ",") {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		parts := strings.Split(path, ".")
		if len(parts) > maxExpandDepth {
			return core.BadRequest(fmt.Sprintf("Expand %q exceeds max depth %d.", path, maxExpandDepth))
		}
		if err := s.expandPath(ctx, c, items, parts, req); err != nil {
			return err
		}
	}
	return nil
}

func (s *Records) expandPath(ctx context.Context, c *schema.Collection, items []map[string]any, parts []string, req *Request) error {
	fieldName := parts[0]
	f := c.Field(fieldName)
	if f == nil || f.Type != schema.FieldRelation {
		return core.BadRequest(fmt.Sprintf("Unknown relation field %q.", fieldName))
	}
	target := s.app.Schema().Get(f.RelationCollection())
	if target == nil {
		return core.BadRequest(fmt.Sprintf("Unknown relation collection %q.", f.RelationCollection()))
	}

	// Collect referenced ids.
	idSet := map[string]bool{}
	for _, item := range items {
		for _, id := range relationIDs(item[fieldName]) {
			idSet[id] = true
		}
	}
	if len(idSet) == 0 {
		return nil
	}
	ids := make([]any, 0, len(idSet))
	placeholders := make([]string, 0, len(idSet))
	for id := range idSet {
		ids = append(ids, id)
		placeholders = append(placeholders, "?")
	}

	// Visibility: superusers see everything, others need the view rule.
	where, args := "1=1", []any{}
	if !req.superuser() {
		rule := target.ViewRule
		if rule == nil {
			return nil // target locked → skip expansion silently
		}
		var err error
		where, args, err = rules.CompileRule(*rule, s.ruleContext(target, req))
		if err != nil {
			return nil
		}
	}

	q := s.app.DB().Dialect.Quote
	query := fmt.Sprintf("SELECT * FROM %s WHERE %s.%s IN (%s) AND (%s)",
		s.tableExpr(target), q(target.Name), q("id"), strings.Join(placeholders, ","), where)
	rows, err := s.app.DB().QueryMaps(ctx, query, append(ids, args...)...)
	if err != nil {
		return err
	}
	byID := map[string]map[string]any{}
	expanded := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		serialized := s.Serialize(target, row)
		byID[db.ToString(row["id"])] = serialized
		expanded = append(expanded, serialized)
	}

	// Recurse into nested path on the expanded records.
	if len(parts) > 1 && len(expanded) > 0 {
		if err := s.expandPath(ctx, target, expanded, parts[1:], req); err != nil {
			return err
		}
	}

	// Attach.
	for _, item := range items {
		refs := relationIDs(item[fieldName])
		if len(refs) == 0 {
			continue
		}
		exp, _ := item["expand"].(map[string]any)
		if exp == nil {
			exp = map[string]any{}
			item["expand"] = exp
		}
		if f.IsMultiple() {
			list := make([]map[string]any, 0, len(refs))
			for _, id := range refs {
				if rec, ok := byID[id]; ok {
					list = append(list, rec)
				}
			}
			exp[fieldName] = list
		} else if rec, ok := byID[refs[0]]; ok {
			exp[fieldName] = rec
		}
	}
	return nil
}

// relationIDs extracts ids from a serialized relation value.
func relationIDs(v any) []string {
	switch t := v.(type) {
	case string:
		if t == "" {
			return nil
		}
		return []string{t}
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := db.ToString(item); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}
