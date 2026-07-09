package core

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"

	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/security"
)

// SettingsField describes one runtime-configurable value. The admin UI
// renders settings forms generically from these descriptors, so modules get
// a settings page for free.
type SettingsField struct {
	Key         string   `json:"key"` // full key, e.g. "mail.adapter"
	Label       string   `json:"label"`
	Type        string   `json:"type"` // text | number | bool | select | secret | textarea | json
	Options     []string `json:"options,omitempty"`
	Default     any      `json:"default,omitempty"`
	Help        string   `json:"help,omitempty"`
	Placeholder string   `json:"placeholder,omitempty"`
}

// SettingsSection groups fields in the admin UI.
type SettingsSection struct {
	ID     string          `json:"id"`
	Title  string          `json:"title"`
	Fields []SettingsField `json:"fields"`
	Order  int             `json:"order"`
}

// Settings is the db-backed runtime configuration store (_params table).
// Secret fields are encrypted at rest with the app secret.
type Settings struct {
	db     *db.DB
	secret string

	mu       sync.RWMutex
	values   map[string]any
	sections map[string]*SettingsSection
	onChange []func(keys []string)
}

func newSettings(d *db.DB, secret string) *Settings {
	return &Settings{
		db:       d,
		secret:   secret,
		values:   map[string]any{},
		sections: map[string]*SettingsSection{},
	}
}

// RegisterSection declares a settings section (idempotent by ID).
// Modules call this from Register().
func (s *Settings) RegisterSection(sec SettingsSection) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sections[sec.ID] = &sec
}

// Sections returns all registered sections ordered for display.
func (s *Settings) Sections() []*SettingsSection {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SettingsSection, 0, len(s.sections))
	for _, sec := range s.sections {
		out = append(out, sec)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Order != out[j].Order {
			return out[i].Order < out[j].Order
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// field looks up the descriptor for a key.
func (s *Settings) field(key string) *SettingsField {
	for _, sec := range s.sections {
		for i := range sec.Fields {
			if sec.Fields[i].Key == key {
				return &sec.Fields[i]
			}
		}
	}
	return nil
}

// OnChange registers a listener fired after any settings write.
func (s *Settings) OnChange(fn func(keys []string)) {
	s.mu.Lock()
	s.onChange = append(s.onChange, fn)
	s.mu.Unlock()
}

// init creates the table and loads all values into the cache.
func (s *Settings) init(ctx context.Context) error {
	create := s.db.Dialect.CreateTable("_params", []db.ColumnDef{
		{Name: "key", Kind: db.ColID, PK: true},
		{Name: "value", Kind: db.ColJSON},
		{Name: "encrypted", Kind: db.ColBool},
		{Name: "updated", Kind: db.ColDateTime},
	})
	if _, err := s.db.Exec(ctx, create); err != nil {
		return fmt.Errorf("settings: init: %w", err)
	}
	rows, err := s.db.QueryMaps(ctx, "SELECT * FROM _params")
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, row := range rows {
		key := db.ToString(row["key"])
		raw := db.ToString(row["value"])
		if db.ToBool(row["encrypted"]) {
			dec, err := security.Decrypt(raw, s.secret)
			if err != nil {
				return fmt.Errorf("settings: decrypt %q (secret changed?): %w", key, err)
			}
			raw = dec
		}
		var v any
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			continue
		}
		s.values[key] = v
	}
	return nil
}

// Get returns the raw value for key, falling back to the field default.
func (s *Settings) Get(key string) any {
	s.mu.RLock()
	v, ok := s.values[key]
	s.mu.RUnlock()
	if ok {
		return v
	}
	if f := s.field(key); f != nil {
		return f.Default
	}
	return nil
}

func (s *Settings) String(key string) string {
	return db.ToString(s.Get(key))
}

func (s *Settings) Bool(key string) bool {
	return db.ToBool(s.Get(key))
}

func (s *Settings) Int(key string) int {
	switch v := s.Get(key).(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// Set writes a single key (see SetMany).
func (s *Settings) Set(ctx context.Context, key string, value any) error {
	return s.SetMany(ctx, map[string]any{key: value})
}

// SetMany persists multiple settings atomically-ish and updates the cache.
// Secret-typed fields are encrypted; writing the mask "••••••" is a no-op so
// admin forms can round-trip masked secrets safely.
func (s *Settings) SetMany(ctx context.Context, kv map[string]any) error {
	keys := make([]string, 0, len(kv))
	for key, value := range kv {
		f := s.field(key)
		if f != nil && f.Type == "secret" {
			if str, _ := value.(string); str == SecretMask {
				continue
			}
		}
		raw, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("settings: marshal %q: %w", key, err)
		}
		stored := string(raw)
		encrypted := false
		if f != nil && f.Type == "secret" {
			enc, err := security.Encrypt(stored, s.secret)
			if err != nil {
				return err
			}
			stored, encrypted = enc, true
		}
		if err := s.upsert(ctx, key, stored, encrypted); err != nil {
			return err
		}
		s.mu.Lock()
		s.values[key] = value
		s.mu.Unlock()
		keys = append(keys, key)
	}
	if len(keys) > 0 {
		s.mu.RLock()
		listeners := s.onChange
		s.mu.RUnlock()
		for _, fn := range listeners {
			fn(keys)
		}
	}
	return nil
}

func (s *Settings) upsert(ctx context.Context, key, value string, encrypted bool) error {
	kcol := s.db.Dialect.Quote("key")
	res, err := s.db.Exec(ctx,
		"UPDATE _params SET value = ?, encrypted = ?, updated = ? WHERE "+kcol+" = ?",
		value, encrypted, db.Now(), key)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		return nil
	}
	_, err = s.db.Exec(ctx,
		"INSERT INTO _params ("+kcol+", value, encrypted, updated) VALUES (?, ?, ?, ?)",
		key, value, encrypted, db.Now())
	return err
}

// SecretMask is what secret values render as in the admin API.
const SecretMask = "••••••"

// Export returns all sections with their current values for the admin UI,
// masking secrets.
func (s *Settings) Export() []map[string]any {
	sections := s.Sections()
	out := make([]map[string]any, 0, len(sections))
	for _, sec := range sections {
		fields := make([]map[string]any, 0, len(sec.Fields))
		for _, f := range sec.Fields {
			v := s.Get(f.Key)
			if f.Type == "secret" {
				if str := db.ToString(v); str != "" {
					v = SecretMask
				}
			}
			fields = append(fields, map[string]any{
				"key": f.Key, "label": f.Label, "type": f.Type,
				"options": f.Options, "help": f.Help, "placeholder": f.Placeholder,
				"value": v,
			})
		}
		out = append(out, map[string]any{"id": sec.ID, "title": sec.Title, "fields": fields})
	}
	return out
}
