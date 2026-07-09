package core

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
)

// SuperusersCollection is the system auth collection for admin users.
const SuperusersCollection = "_superusers"

// Auth is the resolved identity of a request.
type Auth struct {
	Record     map[string]any
	Collection *schema.Collection
	// Roles are resolved role names (from the record's "roles" relation).
	Roles []string
	// Superuser identities bypass collection rules.
	Superuser bool
	// Method: token | apikey | <custom>
	Method string
	// Scopes limits API-key identities to "collection:action" patterns
	// ("*", "posts:*", "posts:read"). nil/empty = unrestricted.
	Scopes []string
}

// ID returns the identity's record id ("" when nil).
func (a *Auth) ID() string {
	if a == nil || a.Record == nil {
		return ""
	}
	return db.ToString(a.Record["id"])
}

// IsSuperuser reports whether the identity bypasses rules.
func (a *Auth) IsSuperuser() bool { return a != nil && a.Superuser }

// HasScope checks an API-key scope like "posts:read". Read actions:
// list/view/read; write: create/update/delete.
func (a *Auth) HasScope(collection, action string) bool {
	if a == nil {
		return false
	}
	if len(a.Scopes) == 0 {
		return true
	}
	for _, s := range a.Scopes {
		parts := strings.SplitN(s, ":", 2)
		col := parts[0]
		act := "*"
		if len(parts) == 2 {
			act = parts[1]
		}
		if (col == "*" || col == collection) && (act == "*" || act == action) {
			return true
		}
	}
	return false
}

type authCtxKey struct{}

// WithAuthContext stores the identity on the request context.
func WithAuthContext(ctx context.Context, a *Auth) context.Context {
	return context.WithValue(ctx, authCtxKey{}, a)
}

// AuthFromContext returns the request identity (nil when unauthenticated).
func AuthFromContext(ctx context.Context) *Auth {
	a, _ := ctx.Value(authCtxKey{}).(*Auth)
	return a
}

// AuthResolver inspects a request and returns an identity, nil when the
// credentials are not its kind, or an error for definitively bad credentials.
type AuthResolver func(r *http.Request) (*Auth, error)

// RuleVars builds the @-placeholder resolver for rules compilation.
// data is the (parsed) request body for create/update requests.
func RuleVars(auth *Auth, data map[string]any, query url.Values) func(string) (any, bool) {
	return func(name string) (any, bool) {
		switch {
		case name == "@now":
			return db.Now(), true
		case name == "@request.auth.id":
			if auth.ID() == "" {
				return nil, true
			}
			return auth.ID(), true
		case name == "@request.auth.roles":
			if auth == nil {
				return nil, true
			}
			return toJSONArray(auth.Roles), true
		case strings.HasPrefix(name, "@request.auth."):
			if auth == nil || auth.Record == nil {
				return nil, true
			}
			field := strings.TrimPrefix(name, "@request.auth.")
			v, ok := auth.Record[field]
			if !ok {
				return nil, true
			}
			// never leak hidden values into comparisons for password fields
			if auth.Collection != nil {
				if f := auth.Collection.Field(field); f != nil && f.Hidden {
					return nil, true
				}
			}
			return v, true
		case strings.HasPrefix(name, "@request.data."):
			if data == nil {
				return nil, true
			}
			v, ok := data[strings.TrimPrefix(name, "@request.data.")]
			if !ok {
				return nil, true
			}
			return normalizeVar(v), true
		case strings.HasPrefix(name, "@request.query."):
			if query == nil {
				return nil, true
			}
			v := query.Get(strings.TrimPrefix(name, "@request.query."))
			if v == "" {
				return nil, true
			}
			return v, true
		}
		return nil, false
	}
}

func toJSONArray(items []string) string {
	if len(items) == 0 {
		return "[]"
	}
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range items {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		b.WriteString(strings.ReplaceAll(s, `"`, `\"`))
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}

func normalizeVar(v any) any {
	switch t := v.(type) {
	case string, float64, bool, nil:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	default:
		return db.ToString(v)
	}
}
