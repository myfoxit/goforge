package schema

import (
	"encoding/json"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/myfoxit/goforge/pkg/db"
)

// Field types.
const (
	FieldText     = "text"
	FieldEditor   = "editor" // rich text (html)
	FieldNumber   = "number"
	FieldBool     = "bool"
	FieldEmail    = "email"
	FieldURL      = "url"
	FieldDate     = "date"
	FieldSelect   = "select"
	FieldJSON     = "json"
	FieldRelation = "relation"
	FieldFile     = "file"
	FieldPassword = "password" // write-only, hashed by the auth layer
	FieldAutodate = "autodate" // system-managed timestamps
)

// FieldTypes lists all available field types.
func FieldTypes() []string {
	return []string{FieldText, FieldEditor, FieldNumber, FieldBool, FieldEmail,
		FieldURL, FieldDate, FieldSelect, FieldJSON, FieldRelation, FieldFile,
		FieldPassword, FieldAutodate}
}

// Field describes one column of a collection.
type Field struct {
	// ID is a stable random identifier — renames are detected by matching IDs.
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Type     string         `json:"type"`
	Required bool           `json:"required"`
	Unique   bool           `json:"unique"`
	System   bool           `json:"system"` // not removable/renamable
	Hidden   bool           `json:"hidden"` // excluded from API responses
	Options  map[string]any `json:"options,omitempty"`
}

// Opt reads a typed option with a fallback.
func optInt(o map[string]any, key string, def int) int {
	if v, ok := o[key]; ok {
		switch t := v.(type) {
		case float64:
			return int(t)
		case int:
			return t
		case string:
			if n, err := strconv.Atoi(t); err == nil {
				return n
			}
		}
	}
	return def
}

func optFloat(o map[string]any, key string) (float64, bool) {
	if v, ok := o[key]; ok {
		switch t := v.(type) {
		case float64:
			return t, true
		case int:
			return float64(t), true
		}
	}
	return 0, false
}

func optString(o map[string]any, key string) string {
	if v, ok := o[key].(string); ok {
		return v
	}
	return ""
}

func optStrings(o map[string]any, key string) []string {
	switch v := o[key].(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func optBool(o map[string]any, key string) bool {
	v, _ := o[key].(bool)
	return v
}

// MaxSelect returns the effective max selections for select/relation/file
// fields (default 1 → stored as scalar; >1 → stored as JSON array).
func (f *Field) MaxSelect() int {
	n := optInt(f.Options, "maxSelect", 1)
	if n < 1 {
		n = 1
	}
	return n
}

// IsMultiple reports whether the field stores a JSON array.
func (f *Field) IsMultiple() bool {
	switch f.Type {
	case FieldSelect, FieldRelation, FieldFile:
		return f.MaxSelect() > 1
	}
	return false
}

// RelationCollection returns the target collection name of a relation field.
func (f *Field) RelationCollection() string { return optString(f.Options, "collection") }

// ColKind maps the field type to its storage kind.
func (f *Field) ColKind() db.ColKind {
	switch f.Type {
	case FieldNumber:
		return db.ColNumber
	case FieldBool:
		return db.ColBool
	case FieldDate, FieldAutodate:
		return db.ColDateTime
	case FieldJSON:
		return db.ColJSON
	case FieldRelation:
		if f.IsMultiple() {
			return db.ColJSON
		}
		return db.ColID
	case FieldSelect, FieldFile:
		if f.IsMultiple() {
			return db.ColJSON
		}
		return db.ColText
	default:
		return db.ColText
	}
}

// Validate checks the field definition itself.
func (f *Field) Validate() error {
	if !nameRe.MatchString(f.Name) {
		return fmt.Errorf("invalid field name %q", f.Name)
	}
	valid := false
	for _, t := range FieldTypes() {
		if f.Type == t {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("field %q: unknown type %q", f.Name, f.Type)
	}
	if f.Type == FieldSelect && len(optStrings(f.Options, "values")) == 0 {
		return fmt.Errorf("field %q: select requires options.values", f.Name)
	}
	if f.Type == FieldRelation && f.RelationCollection() == "" {
		return fmt.Errorf("field %q: relation requires options.collection", f.Name)
	}
	if p := optString(f.Options, "pattern"); p != "" {
		if _, err := regexp.Compile(p); err != nil {
			return fmt.Errorf("field %q: invalid pattern: %w", f.Name, err)
		}
	}
	return nil
}

// dateFormats accepted as input.
var dateFormats = []string{
	db.DateFormat,
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02 15:04:05",
	"2006-01-02T15:04",
	"2006-01-02",
}

// NormalizeValue validates a client-supplied value against the field
// definition and returns the storage representation
// (string | float64 | bool | nil).
func (f *Field) NormalizeValue(v any) (any, error) {
	// Empty handling first: nil / "" / [] are the "zero" for every type.
	if isZero(v) {
		if f.Required {
			return nil, fmt.Errorf("field %q is required", f.Name)
		}
		return f.zeroValue(), nil
	}

	switch f.Type {
	case FieldText, FieldEditor:
		s, ok := stringValue(v)
		if !ok {
			return nil, typeErr(f, v)
		}
		if min := optInt(f.Options, "min", 0); min > 0 && len(s) < min {
			return nil, fmt.Errorf("field %q must be at least %d characters", f.Name, min)
		}
		if max := optInt(f.Options, "max", 0); max > 0 && len(s) > max {
			return nil, fmt.Errorf("field %q must be at most %d characters", f.Name, max)
		}
		if p := optString(f.Options, "pattern"); p != "" {
			if re, err := regexp.Compile(p); err == nil && !re.MatchString(s) {
				return nil, fmt.Errorf("field %q does not match pattern", f.Name)
			}
		}
		return s, nil

	case FieldPassword:
		s, ok := stringValue(v)
		if !ok {
			return nil, typeErr(f, v)
		}
		return s, nil

	case FieldNumber:
		var n float64
		switch t := v.(type) {
		case float64:
			n = t
		case int:
			n = float64(t)
		case int64:
			n = float64(t)
		case json.Number:
			f64, err := t.Float64()
			if err != nil {
				return nil, typeErr(f, v)
			}
			n = f64
		case string:
			f64, err := strconv.ParseFloat(t, 64)
			if err != nil {
				return nil, typeErr(f, v)
			}
			n = f64
		default:
			return nil, typeErr(f, v)
		}
		if min, ok := optFloat(f.Options, "min"); ok && n < min {
			return nil, fmt.Errorf("field %q must be >= %v", f.Name, min)
		}
		if max, ok := optFloat(f.Options, "max"); ok && n > max {
			return nil, fmt.Errorf("field %q must be <= %v", f.Name, max)
		}
		if optBool(f.Options, "noDecimals") && n != float64(int64(n)) {
			return nil, fmt.Errorf("field %q must be an integer", f.Name)
		}
		return n, nil

	case FieldBool:
		switch t := v.(type) {
		case bool:
			return t, nil
		case string:
			b, err := strconv.ParseBool(t)
			if err != nil {
				return nil, typeErr(f, v)
			}
			return b, nil
		default:
			return nil, typeErr(f, v)
		}

	case FieldEmail:
		s, ok := stringValue(v)
		if !ok {
			return nil, typeErr(f, v)
		}
		addr, err := mail.ParseAddress(s)
		if err != nil || addr.Address != s || !strings.Contains(s, ".") {
			return nil, fmt.Errorf("field %q is not a valid email", f.Name)
		}
		return strings.ToLower(s), nil

	case FieldURL:
		s, ok := stringValue(v)
		if !ok {
			return nil, typeErr(f, v)
		}
		u, err := url.Parse(s)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("field %q is not a valid http(s) url", f.Name)
		}
		return s, nil

	case FieldDate:
		s, ok := stringValue(v)
		if !ok {
			return nil, typeErr(f, v)
		}
		for _, layout := range dateFormats {
			if t, err := time.Parse(layout, s); err == nil {
				return t.UTC().Format(db.DateFormat), nil
			}
		}
		return nil, fmt.Errorf("field %q is not a valid date", f.Name)

	case FieldAutodate:
		// System-managed; incoming values are ignored by the records service.
		return db.Now(), nil

	case FieldSelect:
		items, err := listValue(v)
		if err != nil {
			return nil, typeErr(f, v)
		}
		allowed := map[string]bool{}
		for _, opt := range optStrings(f.Options, "values") {
			allowed[opt] = true
		}
		for _, item := range items {
			if !allowed[item] {
				return nil, fmt.Errorf("field %q: %q is not an allowed option", f.Name, item)
			}
		}
		return f.marshalList(items)

	case FieldRelation, FieldFile:
		items, err := listValue(v)
		if err != nil {
			return nil, typeErr(f, v)
		}
		return f.marshalList(items)

	case FieldJSON:
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, typeErr(f, v)
		}
		if max := optInt(f.Options, "maxSize", 0); max > 0 && len(raw) > max {
			return nil, fmt.Errorf("field %q json exceeds %d bytes", f.Name, max)
		}
		return string(raw), nil
	}
	return nil, fmt.Errorf("field %q: unsupported type %q", f.Name, f.Type)
}

// marshalList enforces maxSelect and returns scalar or JSON array storage form.
func (f *Field) marshalList(items []string) (any, error) {
	max := f.MaxSelect()
	if len(items) > max {
		return nil, fmt.Errorf("field %q allows at most %d value(s)", f.Name, max)
	}
	if !f.IsMultiple() {
		if len(items) == 0 {
			return "", nil
		}
		return items[0], nil
	}
	raw, _ := json.Marshal(items)
	return string(raw), nil
}

// zeroValue is the storage representation of "empty" per type.
func (f *Field) zeroValue() any {
	switch f.Type {
	case FieldNumber:
		return float64(0)
	case FieldBool:
		return false
	case FieldJSON:
		return "null"
	default:
		if f.IsMultiple() {
			return "[]"
		}
		return ""
	}
}

// APIValue converts a stored value into its JSON API representation.
func (f *Field) APIValue(v any) any {
	switch f.Type {
	case FieldBool:
		return db.ToBool(v)
	case FieldNumber:
		return db.ToFloat(v)
	case FieldJSON:
		s := db.ToString(v)
		if s == "" {
			return nil
		}
		var out any
		if err := json.Unmarshal([]byte(s), &out); err != nil {
			return s
		}
		return out
	default:
		if f.IsMultiple() {
			list := db.ToJSONList(v)
			if list == nil {
				return []string{}
			}
			return list
		}
		return db.ToString(v)
	}
}

func isZero(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case string:
		return t == ""
	case []any:
		return len(t) == 0
	case []string:
		return len(t) == 0
	}
	return false
}

func stringValue(v any) (string, bool) {
	s, ok := v.(string)
	return s, ok
}

// listValue accepts "a", ["a","b"] or []string.
func listValue(v any) ([]string, error) {
	switch t := v.(type) {
	case string:
		return []string{t}, nil
	case []string:
		return t, nil
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string list")
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("expected string or string list")
}

func typeErr(f *Field, v any) error {
	return fmt.Errorf("field %q: invalid value of type %T", f.Name, v)
}
