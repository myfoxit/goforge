// Package apis exposes the GoForge HTTP API: record CRUD with rule
// enforcement, collection administration, settings, file serving, request
// logs, realtime SSE and health.
package apis

import (
	"context"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/files"
	"github.com/myfoxit/goforge/pkg/rules"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// Records is the rule-enforcing data service used by both the REST handlers
// and the MCP tools.
type Records struct {
	app *core.App
}

// NewRecords creates the records service.
func NewRecords(app *core.App) *Records { return &Records{app: app} }

// Request bundles the acting identity and request context for rule vars.
type Request struct {
	Auth  *core.Auth
	Query url.Values
	Data  map[string]any
	// HTTP is the originating request (nil for MCP/programmatic calls).
	HTTP *http.Request
	// Files holds multipart uploads keyed by field name.
	Files map[string][]*multipart.FileHeader
}

func (r *Request) vars() func(string) (any, bool) {
	if r == nil {
		return core.RuleVars(nil, nil, nil)
	}
	return core.RuleVars(r.Auth, r.Data, r.Query)
}

func (r *Request) superuser() bool { return r != nil && r.Auth.IsSuperuser() }

// ruleContext builds the compiler context for a collection.
func (s *Records) ruleContext(c *schema.Collection, req *Request) *rules.Context {
	return &rules.Context{
		Dialect:       s.app.DB().Dialect,
		Collection:    c,
		Vars:          req.vars(),
		Relations:     func(name string) *schema.Collection { return s.app.Schema().Get(name) },
		HiddenAllowed: req.superuser(),
	}
}

// compileRule resolves the effective access condition for an action.
// Returns core.Forbidden when the rule is locked and the caller is no
// superuser.
func (s *Records) compileRule(c *schema.Collection, action string, req *Request) (string, []any, error) {
	if req.superuser() {
		return "1=1", nil, nil
	}
	if req != nil && req.Auth != nil && len(req.Auth.Scopes) > 0 {
		scopeAction := action
		if action == "list" || action == "view" {
			scopeAction = "read"
		}
		if !req.Auth.HasScope(c.Name, scopeAction) {
			return "", nil, core.Forbidden("The API key does not grant " + scopeAction + " access to " + c.Name + ".")
		}
	}
	rule := c.Rule(action)
	if rule == nil {
		return "", nil, core.Forbidden("Only superusers can perform this action.")
	}
	return rules.CompileRule(*rule, s.ruleContext(c, req))
}

func (s *Records) tableExpr(c *schema.Collection) string {
	if c.IsView() {
		return schema.ViewQuery(c)
	}
	return s.app.DB().Dialect.Quote(c.Name)
}

// ---- List ----

// ListOptions control queries; all fields optional.
type ListOptions struct {
	Page      int
	PerPage   int
	Sort      string // "-created,name"
	Filter    string // rules expression
	Expand    string // "author,tags.owner" (single nested hop)
	SkipTotal bool
}

// ListResult is a page of records.
type ListResult struct {
	Page       int              `json:"page"`
	PerPage    int              `json:"perPage"`
	TotalItems int              `json:"totalItems"`
	TotalPages int              `json:"totalPages"`
	Items      []map[string]any `json:"items"`
}

// List returns records visible under the collection's list rule.
func (s *Records) List(ctx context.Context, collection string, req *Request, opts ListOptions) (*ListResult, error) {
	c := s.app.Schema().Get(collection)
	if c == nil {
		return nil, core.NotFound("Unknown collection.")
	}
	where, args, err := s.compileRule(c, "list", req)
	if err != nil {
		return nil, wrapRuleErr(err)
	}
	if opts.Filter != "" {
		fWhere, fArgs, err := rules.CompileRule(opts.Filter, s.ruleContext(c, req))
		if err != nil {
			return nil, core.BadRequest("Invalid filter: " + err.Error())
		}
		where = "(" + where + ") AND (" + fWhere + ")"
		args = append(args, fArgs...)
	}

	orderBy, err := parseSort(c, opts.Sort, s.app.DB().Dialect)
	if err != nil {
		return nil, err
	}

	page := max(1, opts.Page)
	perPage := opts.PerPage
	if perPage <= 0 {
		perPage = 30
	}
	perPage = min(perPage, 500)

	result := &ListResult{Page: page, PerPage: perPage, TotalItems: -1, TotalPages: -1}
	table := s.tableExpr(c)

	if !opts.SkipTotal {
		row, err := s.app.DB().QueryMap(ctx,
			fmt.Sprintf("SELECT COUNT(*) AS n FROM %s WHERE %s", table, where), args...)
		if err != nil {
			return nil, err
		}
		result.TotalItems = int(db.ToFloat(row["n"]))
		result.TotalPages = (result.TotalItems + perPage - 1) / perPage
	}

	query := fmt.Sprintf("SELECT * FROM %s WHERE %s ORDER BY %s LIMIT %d OFFSET %d",
		table, where, orderBy, perPage, (page-1)*perPage)
	rows, err := s.app.DB().QueryMaps(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		items = append(items, s.Serialize(c, row))
	}
	if opts.Expand != "" {
		if err := s.expand(ctx, c, items, opts.Expand, req); err != nil {
			return nil, err
		}
	}
	result.Items = items
	return result, nil
}

// parseSort validates a sort expression against collection columns.
func parseSort(c *schema.Collection, sort string, d db.Dialect) (string, error) {
	if strings.TrimSpace(sort) == "" {
		sort = "-created"
	}
	parts := strings.Split(sort, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		dir := "ASC"
		if strings.HasPrefix(p, "-") {
			dir = "DESC"
			p = p[1:]
		} else {
			p = strings.TrimPrefix(p, "+")
		}
		if p == "@random" {
			out = append(out, "RANDOM()")
			continue
		}
		if !c.HasColumn(p) {
			return "", core.BadRequest(fmt.Sprintf("Unknown sort field %q.", p))
		}
		if f := c.Field(p); f != nil && f.Hidden {
			return "", core.BadRequest(fmt.Sprintf("Unknown sort field %q.", p))
		}
		out = append(out, d.Quote(c.Name)+"."+d.Quote(p)+" "+dir)
	}
	if len(out) == 0 {
		out = []string{d.Quote(c.Name) + "." + d.Quote("created") + " DESC"}
	}
	return strings.Join(out, ", "), nil
}

// ---- View ----

// View returns one record if visible under the view rule.
func (s *Records) View(ctx context.Context, collection, id string, req *Request, expand string) (map[string]any, error) {
	c := s.app.Schema().Get(collection)
	if c == nil {
		return nil, core.NotFound("Unknown collection.")
	}
	where, args, err := s.compileRule(c, "view", req)
	if err != nil {
		return nil, wrapRuleErr(err)
	}
	q := s.app.DB().Dialect.Quote
	row, err := s.app.DB().QueryMap(ctx, fmt.Sprintf(
		"SELECT * FROM %s WHERE %s.%s = ? AND (%s) LIMIT 1",
		s.tableExpr(c), q(c.Name), q("id"), where),
		append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, core.NotFound("")
	}
	item := s.Serialize(c, row)
	if expand != "" {
		items := []map[string]any{item}
		if err := s.expand(ctx, c, items, expand, req); err != nil {
			return nil, err
		}
	}
	return item, nil
}

// ---- Create ----

// Create validates, stores and returns a new record.
func (s *Records) Create(ctx context.Context, collection string, req *Request) (map[string]any, error) {
	c := s.app.Schema().Get(collection)
	if c == nil {
		return nil, core.NotFound("Unknown collection.")
	}
	if c.IsView() {
		return nil, core.BadRequest("View collections are read-only.")
	}
	// Rule gate (create rule is checked against the *incoming* data below;
	// locked collections stop here).
	if !req.superuser() {
		if req.Auth != nil && len(req.Auth.Scopes) > 0 && !req.Auth.HasScope(c.Name, "create") {
			return nil, core.Forbidden("The API key does not grant create access to " + c.Name + ".")
		}
		if c.CreateRule == nil {
			return nil, core.Forbidden("Only superusers can create records here.")
		}
	}

	// Assign the id up front so file uploads land under the final key
	// (only superusers may choose their own ids).
	id := db.ToString(req.Data["id"])
	if id == "" || !req.superuser() {
		id = security.RandomID(15)
	}
	req.Data["id"] = id

	values, storedFiles, err := s.normalizePayload(ctx, c, req, nil)
	if err != nil {
		return nil, err
	}

	now := db.Now()
	values["id"] = id
	values["created"] = now
	values["updated"] = now

	if c.IsAuth() {
		if err := s.prepareAuthCreate(ctx, c, req, values); err != nil {
			s.cleanupFiles(ctx, storedFiles)
			return nil, err
		}
	}

	event := &core.RecordEvent{App: s.app, Action: "create", Collection: c, Record: values, Request: req.HTTP, Auth: req.Auth}
	if err := s.app.OnRecordBeforeCreate.Trigger(event); err != nil {
		s.cleanupFiles(ctx, storedFiles)
		return nil, err
	}

	cols := make([]string, 0, len(values))
	phs := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	q := s.app.DB().Dialect.Quote
	for k, v := range values {
		cols = append(cols, q(k))
		phs = append(phs, "?")
		args = append(args, v)
	}
	insert := fmt.Sprintf("INSERT INTO %s (%s) VALUES (%s)", q(c.Name), strings.Join(cols, ", "), strings.Join(phs, ", "))
	if _, err := s.app.DB().Exec(ctx, insert, args...); err != nil {
		s.cleanupFiles(ctx, storedFiles)
		if isUniqueViolation(err) {
			return nil, core.BadRequest("A record with this value already exists (unique constraint).")
		}
		return nil, err
	}

	// Post-insert create-rule check (PocketBase semantics): the stored row
	// must satisfy the rule, otherwise roll the insert back.
	if !req.superuser() && c.CreateRule != nil && *c.CreateRule != "" {
		where, ruleArgs, err := rules.CompileRule(*c.CreateRule, s.ruleContext(c, req))
		if err == nil {
			row, qerr := s.app.DB().QueryMap(ctx, fmt.Sprintf(
				"SELECT 1 AS ok FROM %s WHERE %s.%s = ? AND (%s)", q(c.Name), q(c.Name), q("id"), where),
				append([]any{id}, ruleArgs...)...)
			if qerr == nil && row == nil {
				err = fmt.Errorf("create rule not satisfied")
			} else {
				err = qerr
			}
		}
		if err != nil {
			s.app.DB().Exec(ctx, fmt.Sprintf("DELETE FROM %s WHERE %s = ?", q(c.Name), q("id")), id)
			s.cleanupFiles(ctx, storedFiles)
			return nil, core.BadRequest("Failed to create record.")
		}
	}

	record, err := s.app.FindRecordByID(ctx, c.Name, id)
	if err != nil || record == nil {
		return nil, fmt.Errorf("apis: reload created record: %w", err)
	}
	event.Record = record
	s.app.OnRecordAfterCreate.Trigger(event)
	return s.Serialize(c, record), nil
}

// prepareAuthCreate enforces auth collection invariants on insert values.
func (s *Records) prepareAuthCreate(ctx context.Context, c *schema.Collection, req *Request, values map[string]any) error {
	email := db.ToString(values["email"])
	if email == "" {
		return core.ValidationError("email", "Email is required.")
	}
	password := db.ToString(values["password"])
	opts := c.AuthOptions()
	if len(password) < opts.MinPasswordLength {
		return core.ValidationError("password", fmt.Sprintf("Password must be at least %d characters.", opts.MinPasswordLength))
	}
	if confirm, ok := req.Data["passwordConfirm"]; ok && db.ToString(confirm) != password {
		return core.ValidationError("passwordConfirm", "Passwords do not match.")
	}
	if existing, _ := s.app.FindFirstRecord(ctx, c.Name, "email", email); existing != nil {
		return core.ValidationError("email", "Email is already in use.")
	}
	hash, err := security.HashPassword(password)
	if err != nil {
		return err
	}
	values["password"] = hash
	values["tokenKey"] = security.RandomToken(24)
	if !req.superuser() {
		values["verified"] = false
	}
	return nil
}

// ---- Update ----

// Update patches a record if visible under the update rule.
func (s *Records) Update(ctx context.Context, collection, id string, req *Request) (map[string]any, error) {
	c := s.app.Schema().Get(collection)
	if c == nil {
		return nil, core.NotFound("Unknown collection.")
	}
	if c.IsView() {
		return nil, core.BadRequest("View collections are read-only.")
	}
	where, args, err := s.compileRule(c, "update", req)
	if err != nil {
		return nil, wrapRuleErr(err)
	}
	q := s.app.DB().Dialect.Quote
	existing, err := s.app.DB().QueryMap(ctx, fmt.Sprintf(
		"SELECT * FROM %s WHERE %s.%s = ? AND (%s) LIMIT 1", q(c.Name), q(c.Name), q("id"), where),
		append([]any{id}, args...)...)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, core.NotFound("")
	}

	values, storedFiles, err := s.normalizePayload(ctx, c, req, existing)
	if err != nil {
		return nil, err
	}
	if c.IsAuth() {
		if err := s.prepareAuthUpdate(ctx, c, req, existing, values); err != nil {
			s.cleanupFiles(ctx, storedFiles)
			return nil, err
		}
	}
	if len(values) == 0 {
		return s.Serialize(c, existing), nil
	}
	values["updated"] = db.Now()

	event := &core.RecordEvent{App: s.app, Action: "update", Collection: c, Record: values, Old: existing, Request: req.HTTP, Auth: req.Auth}
	if err := s.app.OnRecordBeforeUpdate.Trigger(event); err != nil {
		s.cleanupFiles(ctx, storedFiles)
		return nil, err
	}

	sets := make([]string, 0, len(values))
	updArgs := make([]any, 0, len(values)+1)
	for k, v := range values {
		sets = append(sets, q(k)+" = ?")
		updArgs = append(updArgs, v)
	}
	updArgs = append(updArgs, id)
	if _, err := s.app.DB().Exec(ctx, fmt.Sprintf(
		"UPDATE %s SET %s WHERE %s = ?", q(c.Name), strings.Join(sets, ", "), q("id")), updArgs...); err != nil {
		s.cleanupFiles(ctx, storedFiles)
		if isUniqueViolation(err) {
			return nil, core.BadRequest("A record with this value already exists (unique constraint).")
		}
		return nil, err
	}

	// Remove files that were replaced/dropped.
	s.deleteRemovedFiles(ctx, c, existing, values)

	record, err := s.app.FindRecordByID(ctx, c.Name, id)
	if err != nil || record == nil {
		return nil, fmt.Errorf("apis: reload updated record: %w", err)
	}
	event.Record = record
	s.app.OnRecordAfterUpdate.Trigger(event)
	return s.Serialize(c, record), nil
}

// prepareAuthUpdate guards credential changes on auth collections.
func (s *Records) prepareAuthUpdate(ctx context.Context, c *schema.Collection, req *Request, existing, values map[string]any) error {
	if newPassword, ok := values["password"]; ok {
		pw := db.ToString(newPassword)
		opts := c.AuthOptions()
		if len(pw) < opts.MinPasswordLength {
			return core.ValidationError("password", fmt.Sprintf("Password must be at least %d characters.", opts.MinPasswordLength))
		}
		if confirm, ok := req.Data["passwordConfirm"]; !ok || db.ToString(confirm) != pw {
			return core.ValidationError("passwordConfirm", "Passwords do not match.")
		}
		if !req.superuser() {
			old := db.ToString(req.Data["oldPassword"])
			if !security.VerifyPassword(db.ToString(existing["password"]), old) {
				return core.ValidationError("oldPassword", "Invalid current password.")
			}
		}
		hash, err := security.HashPassword(pw)
		if err != nil {
			return err
		}
		values["password"] = hash
		values["tokenKey"] = security.RandomToken(24) // invalidate sessions
	}
	if newEmail, ok := values["email"]; ok {
		email := db.ToString(newEmail)
		if email != db.ToString(existing["email"]) {
			if other, _ := s.app.FindFirstRecord(ctx, c.Name, "email", email); other != nil {
				return core.ValidationError("email", "Email is already in use.")
			}
			if !req.superuser() {
				// direct email changes require the email-change flow
				return core.ValidationError("email", "Use the request-email-change flow to change the login email.")
			}
		}
	}
	if _, ok := values["verified"]; ok && !req.superuser() {
		delete(values, "verified")
	}
	return nil
}

// ---- Delete ----

// Delete removes a record if visible under the delete rule.
func (s *Records) Delete(ctx context.Context, collection, id string, req *Request) error {
	c := s.app.Schema().Get(collection)
	if c == nil {
		return core.NotFound("Unknown collection.")
	}
	if c.IsView() {
		return core.BadRequest("View collections are read-only.")
	}
	where, args, err := s.compileRule(c, "delete", req)
	if err != nil {
		return wrapRuleErr(err)
	}
	q := s.app.DB().Dialect.Quote
	existing, err := s.app.DB().QueryMap(ctx, fmt.Sprintf(
		"SELECT * FROM %s WHERE %s.%s = ? AND (%s) LIMIT 1", q(c.Name), q(c.Name), q("id"), where),
		append([]any{id}, args...)...)
	if err != nil {
		return err
	}
	if existing == nil {
		return core.NotFound("")
	}

	event := &core.RecordEvent{App: s.app, Action: "delete", Collection: c, Record: existing, Old: existing, Request: req.HTTP, Auth: req.Auth}
	if err := s.app.OnRecordBeforeDelete.Trigger(event); err != nil {
		return err
	}
	if _, err := s.app.DB().Exec(ctx, fmt.Sprintf(
		"DELETE FROM %s WHERE %s = ?", q(c.Name), q("id")), id); err != nil {
		return err
	}

	// Nullify single-relation references pointing at the deleted record.
	for _, other := range s.app.Schema().All() {
		for _, f := range other.Fields {
			if f.Type == schema.FieldRelation && !f.IsMultiple() &&
				(f.RelationCollection() == c.Name || f.RelationCollection() == c.ID) {
				s.app.DB().Exec(ctx, fmt.Sprintf(
					"UPDATE %s SET %s = '' WHERE %s = ?", q(other.Name), q(f.Name), q(f.Name)), id)
			}
		}
	}

	// Remove stored files.
	if st, err := StorageFromApp(s.app); err == nil {
		st.DeletePrefix(ctx, c.Name+"/"+id)
		st.DeletePrefix(ctx, ".thumbs/"+c.Name+"/"+id)
	}

	s.app.OnRecordAfterDelete.Trigger(event)
	return nil
}

// ---- payload normalization + files ----

// normalizePayload validates writable fields from the request. existing is
// nil on create. Returns storage keys written for rollback cleanup.
func (s *Records) normalizePayload(ctx context.Context, c *schema.Collection, req *Request, existing map[string]any) (map[string]any, []string, error) {
	values := map[string]any{}
	var stored []string
	recordID := ""
	if existing != nil {
		recordID = db.ToString(existing["id"])
	}

	for _, f := range c.Fields {
		if f.Type == schema.FieldAutodate {
			continue
		}
		// File fields: merge kept filenames + new uploads.
		if f.Type == schema.FieldFile {
			newVal, wrote, err := s.handleFileField(ctx, c, f, req, existing, recordID)
			if err != nil {
				s.cleanupFiles(ctx, stored)
				return nil, nil, err
			}
			stored = append(stored, wrote...)
			if newVal != nil {
				values[f.Name] = newVal
			}
			continue
		}

		raw, provided := req.Data[f.Name]
		if !provided {
			if existing == nil && f.Required && f.Type != schema.FieldPassword {
				// required fields must be present on create (password is
				// validated by prepareAuthCreate for auth collections)
				if c.IsAuth() && (f.Name == "email" || f.Name == "password") {
					continue
				}
				return nil, nil, core.ValidationError(f.Name, "Field is required.")
			}
			continue
		}
		norm, err := f.NormalizeValue(raw)
		if err != nil {
			s.cleanupFiles(ctx, stored)
			return nil, nil, core.ValidationError(f.Name, err.Error())
		}
		// Relation targets must exist.
		if f.Type == schema.FieldRelation {
			if err := s.checkRelationTargets(ctx, f, norm); err != nil {
				s.cleanupFiles(ctx, stored)
				return nil, nil, err
			}
		}
		values[f.Name] = norm
	}

	// Auth collection: allow email/password through even though they're
	// system-hidden-ish (handled by prepareAuth*).
	if c.IsAuth() {
		if v, ok := req.Data["email"]; ok {
			f := c.Field("email")
			norm, err := f.NormalizeValue(v)
			if err != nil {
				return nil, nil, core.ValidationError("email", err.Error())
			}
			values["email"] = norm
		}
		if v, ok := req.Data["password"]; ok {
			values["password"] = db.ToString(v)
		}
	}
	return values, stored, nil
}

func (s *Records) checkRelationTargets(ctx context.Context, f *schema.Field, norm any) error {
	target := s.app.Schema().Get(f.RelationCollection())
	if target == nil {
		return core.ValidationError(f.Name, "Unknown relation collection.")
	}
	ids := db.ToJSONList(norm)
	q := s.app.DB().Dialect.Quote
	for _, rid := range ids {
		row, err := s.app.DB().QueryMap(ctx, fmt.Sprintf(
			"SELECT 1 AS ok FROM %s WHERE %s = ? LIMIT 1", q(target.Name), q("id")), rid)
		if err != nil {
			return err
		}
		if row == nil {
			return core.ValidationError(f.Name, fmt.Sprintf("Related record %q not found.", rid))
		}
	}
	return nil
}

// handleFileField merges kept + newly uploaded files for one field.
// Returns nil newVal when the field is untouched.
func (s *Records) handleFileField(ctx context.Context, c *schema.Collection, f *schema.Field, req *Request, existing map[string]any, recordID string) (any, []string, error) {
	uploads := req.Files[f.Name]
	raw, provided := req.Data[f.Name]
	if len(uploads) == 0 && !provided {
		return nil, nil, nil
	}

	var existingNames []string
	if existing != nil {
		existingNames = db.ToJSONList(existing[f.Name])
	}
	existingSet := map[string]bool{}
	for _, n := range existingNames {
		existingSet[n] = true
	}

	// Kept files: client sends the filenames it wants to keep.
	var kept []string
	if provided {
		for _, n := range db.ToJSONList(normalizeAny(raw)) {
			if existingSet[n] {
				kept = append(kept, n)
			}
		}
	} else {
		kept = existingNames
	}

	if recordID == "" {
		recordID = db.ToString(req.Data["id"])
	}
	var wrote []string
	if len(uploads) > 0 {
		st, err := StorageFromApp(s.app)
		if err != nil {
			return nil, nil, err
		}
		if recordID == "" {
			// pre-assign id for create so file keys are stable
			recordID = security.RandomID(15)
			req.Data["id"] = recordID
		}
		for _, fh := range uploads {
			if fh.Size > maxUploadSize(f) {
				return nil, wrote, core.ValidationError(f.Name, "File too large.")
			}
			name := files.SanitizeFilename(fh.Filename)
			src, err := fh.Open()
			if err != nil {
				return nil, wrote, err
			}
			key := files.Key(c.Name, recordID, name)
			err = st.Put(ctx, key, src, fh.Size, files.ContentTypeByName(name))
			src.Close()
			if err != nil {
				return nil, wrote, err
			}
			wrote = append(wrote, key)
			kept = append(kept, name)
		}
	}

	norm, err := f.NormalizeValue(kept)
	if err != nil {
		return nil, wrote, core.ValidationError(f.Name, err.Error())
	}
	return norm, wrote, nil
}

func maxUploadSize(f *schema.Field) int64 {
	if v, ok := f.Options["maxSize"]; ok {
		if n := int64(db.ToFloat(v)); n > 0 {
			return n
		}
	}
	return 16 << 20 // 16 MiB default
}

func normalizeAny(v any) any { return v }

func (s *Records) cleanupFiles(ctx context.Context, keys []string) {
	if len(keys) == 0 {
		return
	}
	st, err := StorageFromApp(s.app)
	if err != nil {
		return
	}
	for _, k := range keys {
		st.Delete(ctx, k)
	}
}

// deleteRemovedFiles removes files dropped by an update.
func (s *Records) deleteRemovedFiles(ctx context.Context, c *schema.Collection, existing, values map[string]any) {
	st, err := StorageFromApp(s.app)
	if err != nil {
		return
	}
	recordID := db.ToString(existing["id"])
	for _, f := range c.Fields {
		if f.Type != schema.FieldFile {
			continue
		}
		newVal, ok := values[f.Name]
		if !ok {
			continue
		}
		keep := map[string]bool{}
		for _, n := range db.ToJSONList(newVal) {
			keep[n] = true
		}
		for _, old := range db.ToJSONList(existing[f.Name]) {
			if !keep[old] {
				st.Delete(ctx, files.Key(c.Name, recordID, old))
			}
		}
	}
}

// ---- serialization ----

// Serialize converts a raw row into its public API shape.
func (s *Records) Serialize(c *schema.Collection, row map[string]any) map[string]any {
	out := map[string]any{
		"id":             db.ToString(row["id"]),
		"collectionName": c.Name,
	}
	if v, ok := row["created"]; ok {
		out["created"] = db.ToString(v)
	}
	if v, ok := row["updated"]; ok {
		out["updated"] = db.ToString(v)
	}
	for _, f := range c.Fields {
		if f.Hidden {
			continue
		}
		out[f.Name] = f.APIValue(row[f.Name])
	}
	return out
}

func wrapRuleErr(err error) error {
	if _, ok := err.(*core.APIError); ok {
		return err
	}
	return core.BadRequest(err.Error())
}

func isUniqueViolation(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique") || strings.Contains(msg, "duplicate")
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
