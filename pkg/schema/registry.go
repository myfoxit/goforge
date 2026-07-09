package schema

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/security"
)

// Registry loads collection definitions from _collections, keeps them cached
// and syncs definition changes to real tables (CREATE/ALTER/DROP).
type Registry struct {
	db  *db.DB
	log *slog.Logger

	mu     sync.RWMutex
	byName map[string]*Collection
	byID   map[string]*Collection

	// onChange subscribers are notified after any collection change
	// (used by the API layer to rebuild routes and by realtime).
	onChange []func(old, new *Collection)
}

func NewRegistry(d *db.DB, log *slog.Logger) *Registry {
	if log == nil {
		log = slog.Default()
	}
	return &Registry{
		db:     d,
		log:    log,
		byName: map[string]*Collection{},
		byID:   map[string]*Collection{},
	}
}

// OnChange registers a hook fired after create/update/delete of a collection.
// old is nil on create; new is nil on delete.
func (r *Registry) OnChange(fn func(old, new *Collection)) {
	r.onChange = append(r.onChange, fn)
}

func (r *Registry) emitChange(old, new *Collection) {
	for _, fn := range r.onChange {
		fn(old, new)
	}
}

// Init creates the _collections meta table and loads definitions.
func (r *Registry) Init(ctx context.Context) error {
	create := r.db.Dialect.CreateTable("_collections", []db.ColumnDef{
		{Name: "id", Kind: db.ColID, PK: true},
		{Name: "name", Kind: db.ColID, NotNull: true},
		{Name: "data", Kind: db.ColJSON, NotNull: true},
		{Name: "created", Kind: db.ColDateTime, NotNull: true},
		{Name: "updated", Kind: db.ColDateTime, NotNull: true},
	})
	if _, err := r.db.Exec(ctx, create); err != nil {
		return fmt.Errorf("schema: init _collections: %w", err)
	}
	idx := r.db.Dialect.CreateIndex(db.IndexDef{
		Name: "ux_collections_name", Table: "_collections", Columns: []string{"name"}, Unique: true,
	}, map[string]db.ColKind{"name": db.ColID})
	if _, err := r.db.Exec(ctx, idx); err != nil && !isDupIndexErr(err) {
		return fmt.Errorf("schema: index _collections: %w", err)
	}
	return r.Reload(ctx)
}

// Reload re-reads all collection definitions from the database.
func (r *Registry) Reload(ctx context.Context) error {
	rows, err := r.db.QueryMaps(ctx, "SELECT data FROM _collections")
	if err != nil {
		return err
	}
	byName := map[string]*Collection{}
	byID := map[string]*Collection{}
	for _, row := range rows {
		var c Collection
		if err := json.Unmarshal([]byte(db.ToString(row["data"])), &c); err != nil {
			return fmt.Errorf("schema: corrupt collection row: %w", err)
		}
		byName[c.Name] = &c
		byID[c.ID] = &c
	}
	r.mu.Lock()
	r.byName, r.byID = byName, byID
	r.mu.Unlock()
	return nil
}

// Get returns a collection by name or id (nil when absent).
func (r *Registry) Get(nameOrID string) *Collection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.byName[nameOrID]; ok {
		return c
	}
	return r.byID[nameOrID]
}

// All returns all collections sorted by name (system first).
func (r *Registry) All() []*Collection {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Collection, 0, len(r.byName))
	for _, c := range r.byName {
		out = append(out, c)
	}
	// stable order: system collections first, then alphabetical
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && less(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func less(a, b *Collection) bool {
	if a.System != b.System {
		return a.System
	}
	return a.Name < b.Name
}

// Save creates or updates a collection: validates, assigns ids, diffs against
// the stored definition and applies DDL, then persists the definition.
func (r *Registry) Save(ctx context.Context, c *Collection) error {
	if c.Type == "" {
		c.Type = TypeBase
	}
	if err := c.Validate(); err != nil {
		return err
	}
	for _, f := range c.Fields {
		if f.ID == "" {
			f.ID = security.RandomID(10)
		}
	}
	// Relation targets must exist.
	for _, f := range c.Fields {
		if f.Type == FieldRelation {
			if target := r.Get(f.RelationCollection()); target == nil {
				return fmt.Errorf("schema: relation field %q references unknown collection %q", f.Name, f.RelationCollection())
			}
		}
	}

	var old *Collection
	if c.ID != "" {
		old = r.Get(c.ID)
	}
	if old == nil {
		old = r.Get(c.Name)
		if old != nil && c.ID != "" && old.ID != c.ID {
			return fmt.Errorf("schema: collection name %q already in use", c.Name)
		}
	}

	if old == nil {
		return r.create(ctx, c)
	}
	return r.update(ctx, old, c)
}

func (r *Registry) create(ctx context.Context, c *Collection) error {
	c.ID = security.RandomID(15)
	if c.IsView() {
		if err := r.validateViewQuery(ctx, c); err != nil {
			return err
		}
	} else {
		cols := systemColumns()
		for _, f := range c.Fields {
			cols = append(cols, db.ColumnDef{Name: f.Name, Kind: f.ColKind()})
		}
		if _, err := r.db.Exec(ctx, r.db.Dialect.CreateTable(c.Name, cols)); err != nil {
			return fmt.Errorf("schema: create table %q: %w", c.Name, err)
		}
		if err := r.syncIndexes(ctx, nil, c); err != nil {
			return err
		}
	}
	if err := r.persist(ctx, c, true); err != nil {
		return err
	}
	r.cache(c, "")
	r.log.Info("collection created", "name", c.Name, "type", c.Type)
	r.emitChange(nil, c)
	return nil
}

func (r *Registry) update(ctx context.Context, old, c *Collection) error {
	c.ID = old.ID
	c.System = old.System
	if old.Type != c.Type && c.Type != "" {
		return fmt.Errorf("schema: collection type cannot change")
	}
	if old.System && old.Name != c.Name {
		return fmt.Errorf("schema: system collection %q cannot be renamed", old.Name)
	}
	// Ensure system fields survive.
	for _, f := range old.Fields {
		if f.System && c.FieldByID(f.ID) == nil {
			return fmt.Errorf("schema: system field %q cannot be removed", f.Name)
		}
	}

	if c.IsView() {
		if err := r.validateViewQuery(ctx, c); err != nil {
			return err
		}
	} else {
		if old.Name != c.Name {
			if _, err := r.db.Exec(ctx, r.db.Dialect.RenameTable(old.Name, c.Name)); err != nil {
				return fmt.Errorf("schema: rename table: %w", err)
			}
		}
		if err := r.syncFields(ctx, old, c); err != nil {
			return err
		}
		if err := r.syncIndexes(ctx, old, c); err != nil {
			return err
		}
	}
	if err := r.persist(ctx, c, false); err != nil {
		return err
	}
	r.cache(c, old.Name)
	r.log.Info("collection updated", "name", c.Name)
	r.emitChange(old, c)
	return nil
}

// syncFields diffs fields by stable ID and applies column DDL.
func (r *Registry) syncFields(ctx context.Context, old, c *Collection) error {
	d := r.db.Dialect
	oldByID := map[string]*Field{}
	for _, f := range old.Fields {
		oldByID[f.ID] = f
	}
	newByID := map[string]*Field{}
	for _, f := range c.Fields {
		newByID[f.ID] = f
	}

	// Dropped fields.
	for _, f := range old.Fields {
		if newByID[f.ID] == nil {
			if _, err := r.db.Exec(ctx, d.DropColumn(c.Name, f.Name)); err != nil {
				return fmt.Errorf("schema: drop column %q: %w", f.Name, err)
			}
		}
	}
	// Added / renamed / retyped fields.
	for _, f := range c.Fields {
		prev := oldByID[f.ID]
		switch {
		case prev == nil:
			if _, err := r.db.Exec(ctx, d.AddColumn(c.Name, db.ColumnDef{Name: f.Name, Kind: f.ColKind()})); err != nil {
				return fmt.Errorf("schema: add column %q: %w", f.Name, err)
			}
		case prev.Name != f.Name && prev.ColKind() == f.ColKind():
			if _, err := r.db.Exec(ctx, d.RenameColumn(c.Name, prev.Name, f.Name)); err != nil {
				return fmt.Errorf("schema: rename column %q: %w", prev.Name, err)
			}
		case prev.ColKind() != f.ColKind():
			// Storage kind changed: drop + recreate (destructive, admin confirms).
			if _, err := r.db.Exec(ctx, d.DropColumn(c.Name, prev.Name)); err != nil {
				return fmt.Errorf("schema: retype column %q (drop): %w", prev.Name, err)
			}
			if _, err := r.db.Exec(ctx, d.AddColumn(c.Name, db.ColumnDef{Name: f.Name, Kind: f.ColKind()})); err != nil {
				return fmt.Errorf("schema: retype column %q (add): %w", f.Name, err)
			}
		}
	}
	return nil
}

// syncIndexes drops removed and creates added indexes. Unique fields get
// implicit ux_<collection>_<field> indexes.
func (r *Registry) syncIndexes(ctx context.Context, old, c *Collection) error {
	d := r.db.Dialect
	want := effectiveIndexes(c)
	var have []Index
	if old != nil {
		have = effectiveIndexes(old)
	}
	haveByName := map[string]Index{}
	for _, idx := range have {
		haveByName[idx.Name] = idx
	}
	wantByName := map[string]Index{}
	for _, idx := range want {
		wantByName[idx.Name] = idx
	}
	for name, idx := range haveByName {
		if w, ok := wantByName[name]; !ok || !sameIndex(idx, w) {
			if _, err := r.db.Exec(ctx, d.DropIndex(name, tableNameFor(old, c))); err != nil {
				r.log.Warn("drop index failed", "index", name, "err", err)
			}
		}
	}
	kinds := c.ColKinds()
	for name, idx := range wantByName {
		if h, ok := haveByName[name]; ok && sameIndex(h, idx) && old != nil && old.Name == c.Name {
			continue
		}
		def := db.IndexDef{Name: idx.Name, Table: c.Name, Columns: idx.Columns, Unique: idx.Unique}
		if _, err := r.db.Exec(ctx, d.CreateIndex(def, kinds)); err != nil && !isDupIndexErr(err) {
			return fmt.Errorf("schema: create index %q: %w", name, err)
		}
	}
	return nil
}

func tableNameFor(old, c *Collection) string {
	if old != nil {
		return c.Name // rename already applied
	}
	return c.Name
}

func effectiveIndexes(c *Collection) []Index {
	out := append([]Index{}, c.Indexes...)
	for _, f := range c.Fields {
		if f.Unique {
			out = append(out, Index{
				Name:    fmt.Sprintf("ux_%s_%s", c.Name, f.Name),
				Columns: []string{f.Name},
				Unique:  true,
			})
		}
	}
	return out
}

func sameIndex(a, b Index) bool {
	if a.Unique != b.Unique || len(a.Columns) != len(b.Columns) {
		return false
	}
	for i := range a.Columns {
		if a.Columns[i] != b.Columns[i] {
			return false
		}
	}
	return true
}

// Delete removes a collection definition and drops its table.
func (r *Registry) Delete(ctx context.Context, nameOrID string) error {
	c := r.Get(nameOrID)
	if c == nil {
		return fmt.Errorf("schema: collection %q not found", nameOrID)
	}
	if c.System {
		return fmt.Errorf("schema: system collection %q cannot be deleted", c.Name)
	}
	// Refuse when other collections point at it.
	for _, other := range r.All() {
		if other.Name == c.Name {
			continue
		}
		for _, f := range other.Fields {
			if f.Type == FieldRelation && (f.RelationCollection() == c.Name || f.RelationCollection() == c.ID) {
				return fmt.Errorf("schema: collection %q is referenced by %s.%s", c.Name, other.Name, f.Name)
			}
		}
	}
	if !c.IsView() {
		if _, err := r.db.Exec(ctx, r.db.Dialect.DropTable(c.Name)); err != nil {
			return fmt.Errorf("schema: drop table: %w", err)
		}
	}
	if _, err := r.db.Exec(ctx, "DELETE FROM _collections WHERE id = ?", c.ID); err != nil {
		return err
	}
	r.mu.Lock()
	delete(r.byName, c.Name)
	delete(r.byID, c.ID)
	r.mu.Unlock()
	r.log.Info("collection deleted", "name", c.Name)
	r.emitChange(c, nil)
	return nil
}

// validateViewQuery ensures the stored query is a executable SELECT and
// captures its output columns as pseudo-fields.
func (r *Registry) validateViewQuery(ctx context.Context, c *Collection) error {
	q, _ := c.Options["query"].(string)
	q = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(q), ";"))
	upper := strings.ToUpper(q)
	if !strings.HasPrefix(upper, "SELECT") && !strings.HasPrefix(upper, "WITH") {
		return fmt.Errorf("schema: view query must be a SELECT")
	}
	c.Options["query"] = q
	rows, err := r.db.Query(ctx, wrapViewQuery(q, 0))
	if err != nil {
		return fmt.Errorf("schema: invalid view query: %w", err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	fields := make([]*Field, 0, len(cols))
	for _, col := range cols {
		if col == "id" || col == "created" || col == "updated" {
			continue
		}
		f := &Field{Name: col, Type: FieldJSON}
		if prev := c.Field(col); prev != nil {
			f.ID = prev.ID
		}
		fields = append(fields, f)
	}
	c.Fields = fields
	return nil
}

// wrapViewQuery embeds a view query as a subselect with a limit.
func wrapViewQuery(q string, limit int) string {
	return fmt.Sprintf("SELECT * FROM (%s) AS _view LIMIT %d", q, limit)
}

// ViewQuery returns the wrapped, paginated SQL for a view collection.
func ViewQuery(c *Collection) string {
	q, _ := c.Options["query"].(string)
	return "(" + q + ") AS " + c.Name
}

func (r *Registry) persist(ctx context.Context, c *Collection, isNew bool) error {
	raw, err := json.Marshal(c)
	if err != nil {
		return err
	}
	now := db.Now()
	if isNew {
		_, err = r.db.Exec(ctx,
			"INSERT INTO _collections (id, name, data, created, updated) VALUES (?, ?, ?, ?, ?)",
			c.ID, c.Name, string(raw), now, now)
	} else {
		_, err = r.db.Exec(ctx,
			"UPDATE _collections SET name = ?, data = ?, updated = ? WHERE id = ?",
			c.Name, string(raw), now, c.ID)
	}
	return err
}

func (r *Registry) cache(c *Collection, oldName string) {
	r.mu.Lock()
	if oldName != "" && oldName != c.Name {
		delete(r.byName, oldName)
	}
	r.byName[c.Name] = c
	r.byID[c.ID] = c
	r.mu.Unlock()
}

func isDupIndexErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate key name")
}

func systemColumns() []db.ColumnDef {
	return []db.ColumnDef{
		{Name: "id", Kind: db.ColID, PK: true},
		{Name: "created", Kind: db.ColDateTime, NotNull: true},
		{Name: "updated", Kind: db.ColDateTime, NotNull: true},
	}
}

// BaseAuthFields returns the system fields every auth collection carries.
func BaseAuthFields() []*Field {
	return []*Field{
		{ID: "sys_email", Name: "email", Type: FieldEmail, Required: true, Unique: true, System: true},
		{ID: "sys_password", Name: "password", Type: FieldPassword, Required: true, Hidden: true, System: true,
			Options: map[string]any{"min": 10}},
		{ID: "sys_tokenkey", Name: "tokenKey", Type: FieldText, Hidden: true, System: true},
		{ID: "sys_verified", Name: "verified", Type: FieldBool, System: true},
		{ID: "sys_name", Name: "name", Type: FieldText, System: true},
	}
}
