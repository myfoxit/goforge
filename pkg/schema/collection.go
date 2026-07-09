// Package schema implements GoForge's dynamic collection engine: collections
// (tables) and fields (columns) are defined at runtime — from the admin UI,
// the API or MCP tools — and synced to real database tables through the
// db.Dialect DDL abstraction.
package schema

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/myfoxit/goforge/pkg/db"
)

// Collection types.
const (
	TypeBase = "base" // regular data table
	TypeAuth = "auth" // auth-enabled table (email/password/verified/...)
	TypeView = "view" // read-only stored query
)

// Collection describes one dynamic table plus its API access rules.
type Collection struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	System bool   `json:"system"` // system collections can't be deleted/renamed

	Fields  []*Field `json:"fields"`
	Indexes []Index  `json:"indexes"`

	// API rules. nil = admin only ("locked"), empty string = public,
	// otherwise a rules expression (see pkg/rules).
	ListRule   *string `json:"listRule"`
	ViewRule   *string `json:"viewRule"`
	CreateRule *string `json:"createRule"`
	UpdateRule *string `json:"updateRule"`
	DeleteRule *string `json:"deleteRule"`

	// Options holds type-specific settings:
	//   auth: identityFields, minPasswordLength, requireVerification, onlyVerified
	//   view: query (read-only SQL SELECT)
	Options map[string]any `json:"options,omitempty"`
}

// Index describes a (composite) index on collection columns.
type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Unique  bool     `json:"unique"`
}

var nameRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// reservedFieldNames cannot be used for custom fields.
var reservedFieldNames = map[string]bool{
	"id": true, "created": true, "updated": true,
	"expand": true, "collectionName": true,
}

// IsAuth reports whether the collection is an auth collection.
func (c *Collection) IsAuth() bool { return c.Type == TypeAuth }

// IsView reports whether the collection is a read-only view.
func (c *Collection) IsView() bool { return c.Type == TypeView }

// Field returns the field with the given name, or nil.
func (c *Collection) Field(name string) *Field {
	for _, f := range c.Fields {
		if f.Name == name {
			return f
		}
	}
	return nil
}

// FieldByID returns the field with the given stable id, or nil.
func (c *Collection) FieldByID(id string) *Field {
	for _, f := range c.Fields {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// ColumnNames returns id/created/updated plus all field column names.
func (c *Collection) ColumnNames() []string {
	out := []string{"id", "created", "updated"}
	for _, f := range c.Fields {
		out = append(out, f.Name)
	}
	return out
}

// HasColumn reports whether name is a queryable column of the collection.
func (c *Collection) HasColumn(name string) bool {
	if name == "id" || name == "created" || name == "updated" {
		return true
	}
	return c.Field(name) != nil
}

// ColKinds maps every column to its storage kind (used for index DDL).
func (c *Collection) ColKinds() map[string]db.ColKind {
	m := map[string]db.ColKind{"id": db.ColID, "created": db.ColDateTime, "updated": db.ColDateTime}
	for _, f := range c.Fields {
		m[f.Name] = f.ColKind()
	}
	return m
}

// Rule returns the rule pointer for an action: list|view|create|update|delete.
func (c *Collection) Rule(action string) *string {
	switch action {
	case "list":
		return c.ListRule
	case "view":
		return c.ViewRule
	case "create":
		return c.CreateRule
	case "update":
		return c.UpdateRule
	case "delete":
		return c.DeleteRule
	}
	return nil
}

// Validate checks structural correctness of the collection definition.
func (c *Collection) Validate() error {
	if !nameRe.MatchString(c.Name) {
		return fmt.Errorf("schema: invalid collection name %q", c.Name)
	}
	if strings.HasPrefix(c.Name, "sqlite_") {
		return fmt.Errorf("schema: reserved collection name %q", c.Name)
	}
	switch c.Type {
	case TypeBase, TypeAuth, TypeView:
	default:
		return fmt.Errorf("schema: invalid collection type %q", c.Type)
	}
	if c.IsView() {
		if q, _ := c.Options["query"].(string); strings.TrimSpace(q) == "" {
			return fmt.Errorf("schema: view collection %q requires options.query", c.Name)
		}
	}
	seen := map[string]bool{}
	for _, f := range c.Fields {
		if err := f.Validate(); err != nil {
			return fmt.Errorf("schema: collection %q: %w", c.Name, err)
		}
		if reservedFieldNames[f.Name] {
			return fmt.Errorf("schema: field name %q is reserved", f.Name)
		}
		if seen[f.Name] {
			return fmt.Errorf("schema: duplicate field %q", f.Name)
		}
		seen[f.Name] = true
	}
	for _, idx := range c.Indexes {
		if !nameRe.MatchString(idx.Name) {
			return fmt.Errorf("schema: invalid index name %q", idx.Name)
		}
		if len(idx.Columns) == 0 {
			return fmt.Errorf("schema: index %q has no columns", idx.Name)
		}
		for _, col := range idx.Columns {
			if !c.HasColumn(col) {
				return fmt.Errorf("schema: index %q references unknown column %q", idx.Name, col)
			}
		}
	}
	return nil
}

// Clone returns a deep copy of the collection.
func (c *Collection) Clone() *Collection {
	raw, _ := json.Marshal(c)
	var out Collection
	json.Unmarshal(raw, &out)
	return &out
}

// AuthOptions returns typed auth collection options with defaults applied.
func (c *Collection) AuthOptions() AuthOptions {
	opts := AuthOptions{
		IdentityFields:    []string{"email"},
		MinPasswordLength: 10,
	}
	if c.Options == nil {
		return opts
	}
	if v, ok := c.Options["identityFields"].([]any); ok && len(v) > 0 {
		opts.IdentityFields = nil
		for _, s := range v {
			if str, ok := s.(string); ok {
				opts.IdentityFields = append(opts.IdentityFields, str)
			}
		}
	}
	if v, ok := c.Options["minPasswordLength"].(float64); ok && v >= 6 {
		opts.MinPasswordLength = int(v)
	}
	if v, ok := c.Options["onlyVerified"].(bool); ok {
		opts.OnlyVerified = v
	}
	return opts
}

// AuthOptions configures auth collections.
type AuthOptions struct {
	IdentityFields    []string // fields accepted as login identity (email, username, ...)
	MinPasswordLength int
	OnlyVerified      bool // reject login before email verification
}
