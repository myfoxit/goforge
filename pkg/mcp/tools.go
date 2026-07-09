package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/myfoxit/goforge/pkg/apis"
	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
)

// toolDef is an MCP tool descriptor.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

// visibleCollections returns collections the identity may see tools for.
func visibleCollections(app *core.App, auth *core.Auth) []*schema.Collection {
	var out []*schema.Collection
	for _, c := range app.Schema().All() {
		if c.System && !auth.IsSuperuser() {
			continue
		}
		if len(auth.Scopes) > 0 {
			readable := auth.HasScope(c.Name, "read") || auth.HasScope(c.Name, "create") ||
				auth.HasScope(c.Name, "update") || auth.HasScope(c.Name, "delete")
			if !readable {
				continue
			}
		}
		out = append(out, c)
	}
	return out
}

// buildTools produces the tool list for the identity.
func buildTools(app *core.App, auth *core.Auth) []toolDef {
	var tools []toolDef
	for _, c := range visibleCollections(app, auth) {
		tools = append(tools, collectionTools(c, auth)...)
	}
	if auth.IsSuperuser() {
		tools = append(tools, adminTools()...)
	}
	return tools
}

// fieldSchema converts a collection field to a JSON Schema property.
func fieldSchema(f *schema.Field) map[string]any {
	desc := f.Type
	prop := map[string]any{}
	switch f.Type {
	case schema.FieldNumber:
		prop["type"] = "number"
	case schema.FieldBool:
		prop["type"] = "boolean"
	case schema.FieldJSON:
		desc = "arbitrary JSON value"
	case schema.FieldSelect:
		values := []string{}
		if raw, ok := f.Options["values"].([]any); ok {
			for _, v := range raw {
				values = append(values, db.ToString(v))
			}
		}
		if f.IsMultiple() {
			prop["type"] = "array"
			prop["items"] = map[string]any{"type": "string", "enum": values}
			desc = fmt.Sprintf("up to %d of: %s", f.MaxSelect(), strings.Join(values, ", "))
		} else {
			prop["type"] = "string"
			prop["enum"] = values
			desc = "one of: " + strings.Join(values, ", ")
		}
	case schema.FieldRelation:
		if f.IsMultiple() {
			prop["type"] = "array"
			prop["items"] = map[string]any{"type": "string"}
			desc = fmt.Sprintf("record ids from collection %q (max %d)", f.RelationCollection(), f.MaxSelect())
		} else {
			prop["type"] = "string"
			desc = fmt.Sprintf("record id from collection %q", f.RelationCollection())
		}
	case schema.FieldDate:
		prop["type"] = "string"
		desc = "datetime (RFC3339 or 'YYYY-MM-DD HH:MM:SS')"
	case schema.FieldFile:
		prop["type"] = "array"
		prop["items"] = map[string]any{"type": "string"}
		desc = "stored filenames (uploads only via REST multipart)"
	default:
		prop["type"] = "string"
	}
	if f.Required {
		desc += " (required)"
	}
	prop["description"] = desc
	return prop
}

// dataSchema builds the JSON schema for a collection's writable payload.
func dataSchema(c *schema.Collection) map[string]any {
	props := map[string]any{}
	var required []string
	for _, f := range c.Fields {
		if f.Hidden && f.Type != schema.FieldPassword {
			continue
		}
		if f.Type == schema.FieldAutodate {
			continue
		}
		props[f.Name] = fieldSchema(f)
		if f.Required && f.Type != schema.FieldPassword {
			required = append(required, f.Name)
		}
	}
	if c.IsAuth() {
		props["password"] = map[string]any{"type": "string", "description": "login password (create only)"}
		props["passwordConfirm"] = map[string]any{"type": "string", "description": "must match password"}
	}
	return obj(props, required...)
}

func collectionTools(c *schema.Collection, auth *core.Auth) []toolDef {
	name := c.Name
	can := func(action string) bool {
		return len(auth.Scopes) == 0 || auth.HasScope(name, action)
	}
	var tools []toolDef
	if can("read") {
		tools = append(tools,
			toolDef{
				Name:        name + "_list",
				Description: fmt.Sprintf("List records from the %q collection with optional filtering, sorting and pagination.", name),
				InputSchema: obj(map[string]any{
					"filter":  map[string]any{"type": "string", "description": "rules expression, e.g. status = 'active' && count > 3"},
					"sort":    map[string]any{"type": "string", "description": "comma-separated fields, - prefix for desc, e.g. -created,name"},
					"page":    map[string]any{"type": "number", "description": "1-based page (default 1)"},
					"perPage": map[string]any{"type": "number", "description": "items per page (default 30, max 500)"},
					"expand":  map[string]any{"type": "string", "description": "relation fields to expand, e.g. author,tags"},
				}),
			},
			toolDef{
				Name:        name + "_get",
				Description: fmt.Sprintf("Fetch one record from %q by id.", name),
				InputSchema: obj(map[string]any{
					"id":     map[string]any{"type": "string"},
					"expand": map[string]any{"type": "string", "description": "relation fields to expand"},
				}, "id"),
			},
		)
	}
	if c.IsView() {
		return tools
	}
	if can("create") {
		tools = append(tools, toolDef{
			Name:        name + "_create",
			Description: fmt.Sprintf("Create a record in %q.", name),
			InputSchema: obj(map[string]any{"data": dataSchema(c)}, "data"),
		})
	}
	if can("update") {
		tools = append(tools, toolDef{
			Name:        name + "_update",
			Description: fmt.Sprintf("Update fields of a record in %q.", name),
			InputSchema: obj(map[string]any{
				"id":   map[string]any{"type": "string"},
				"data": dataSchema(c),
			}, "id", "data"),
		})
	}
	if can("delete") {
		tools = append(tools, toolDef{
			Name:        name + "_delete",
			Description: fmt.Sprintf("Delete a record from %q by id.", name),
			InputSchema: obj(map[string]any{"id": map[string]any{"type": "string"}}, "id"),
		})
	}
	return tools
}

func adminTools() []toolDef {
	fieldProps := obj(map[string]any{
		"name":     map[string]any{"type": "string"},
		"type":     map[string]any{"type": "string", "enum": schema.FieldTypes()},
		"required": map[string]any{"type": "boolean"},
		"unique":   map[string]any{"type": "boolean"},
		"hidden":   map[string]any{"type": "boolean"},
		"options": map[string]any{"type": "object", "description": "type options: select {values, maxSelect}; relation {collection, maxSelect}; " +
			"text {min, max, pattern}; number {min, max, noDecimals}; file {maxSelect, maxSize}"},
	}, "name", "type")

	return []toolDef{
		{
			Name:        "collections_list",
			Description: "List all collections with their fields and access rules (the application schema).",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name: "collections_save",
			Description: "Create or update a collection: fields (columns), indexes and API access rules. " +
				"Rules: null = superusers only, \"\" = public, or an expression like \"owner = @request.auth.id\". " +
				"Existing fields are matched by name; omitting an existing field DELETES its column.",
			InputSchema: obj(map[string]any{
				"name":       map[string]any{"type": "string"},
				"type":       map[string]any{"type": "string", "enum": []string{"base", "auth", "view"}, "description": "default base"},
				"fields":     map[string]any{"type": "array", "items": fieldProps},
				"listRule":   map[string]any{"type": []string{"string", "null"}},
				"viewRule":   map[string]any{"type": []string{"string", "null"}},
				"createRule": map[string]any{"type": []string{"string", "null"}},
				"updateRule": map[string]any{"type": []string{"string", "null"}},
				"deleteRule": map[string]any{"type": []string{"string", "null"}},
				"options":    map[string]any{"type": "object", "description": "view: {query: \"SELECT ...\"}"},
			}, "name"),
		},
		{
			Name:        "collections_delete",
			Description: "Delete a collection and its data. Irreversible.",
			InputSchema: obj(map[string]any{"name": map[string]any{"type": "string"}}, "name"),
		},
		{
			Name:        "settings_get",
			Description: "Read the application settings sections and current values (secrets masked).",
			InputSchema: obj(map[string]any{}),
		},
		{
			Name:        "settings_set",
			Description: "Update application settings by key, e.g. {\"app.name\": \"My SaaS\", \"mail.adapter\": \"smtp\"}.",
			InputSchema: obj(map[string]any{
				"values": map[string]any{"type": "object"},
			}, "values"),
		},
	}
}

// ---- tool dispatch ----

type toolResult struct {
	Content []map[string]any `json:"content"`
	IsError bool             `json:"isError,omitempty"`
}

func textResult(v any) toolResult {
	var text string
	switch t := v.(type) {
	case string:
		text = t
	default:
		raw, _ := json.MarshalIndent(v, "", "  ")
		text = string(raw)
	}
	return toolResult{Content: []map[string]any{{"type": "text", "text": text}}}
}

func errResult(err error) toolResult {
	r := textResult("Error: " + err.Error())
	r.IsError = true
	return r
}

func callTool(app *core.App, svc *apis.Records, r *http.Request, auth *core.Auth, name string, args map[string]any) toolResult {
	if args == nil {
		args = map[string]any{}
	}
	ctx := r.Context()

	// Admin tools.
	switch name {
	case "collections_list":
		if !auth.IsSuperuser() {
			return errResult(core.Forbidden(""))
		}
		return textResult(app.Schema().All())
	case "collections_save":
		if !auth.IsSuperuser() {
			return errResult(core.Forbidden(""))
		}
		return saveCollectionTool(ctx, app, args)
	case "collections_delete":
		if !auth.IsSuperuser() {
			return errResult(core.Forbidden(""))
		}
		colName := db.ToString(args["name"])
		if err := app.Schema().Delete(ctx, colName); err != nil {
			return errResult(err)
		}
		return textResult("Collection " + colName + " deleted.")
	case "settings_get":
		if !auth.IsSuperuser() {
			return errResult(core.Forbidden(""))
		}
		return textResult(app.Settings().Export())
	case "settings_set":
		if !auth.IsSuperuser() {
			return errResult(core.Forbidden(""))
		}
		values, _ := args["values"].(map[string]any)
		if len(values) == 0 {
			return errResult(fmt.Errorf("values must be a non-empty object"))
		}
		if err := app.Settings().SetMany(ctx, values); err != nil {
			return errResult(err)
		}
		return textResult("Settings updated.")
	}

	// Collection tools: <collection>_<action>.
	idx := strings.LastIndexByte(name, '_')
	if idx <= 0 {
		return errResult(fmt.Errorf("unknown tool %q", name))
	}
	colName, action := name[:idx], name[idx+1:]
	if app.Schema().Get(colName) == nil {
		return errResult(fmt.Errorf("unknown tool %q", name))
	}

	req := &apis.Request{Auth: auth, Data: map[string]any{}, HTTP: r}

	switch action {
	case "list":
		result, err := svc.List(ctx, colName, req, apis.ListOptions{
			Page:    int(db.ToFloat(args["page"])),
			PerPage: int(db.ToFloat(args["perPage"])),
			Sort:    db.ToString(args["sort"]),
			Filter:  db.ToString(args["filter"]),
			Expand:  db.ToString(args["expand"]),
		})
		if err != nil {
			return errResult(err)
		}
		return textResult(result)
	case "get":
		record, err := svc.View(ctx, colName, db.ToString(args["id"]), req, db.ToString(args["expand"]))
		if err != nil {
			return errResult(err)
		}
		return textResult(record)
	case "create":
		data, _ := args["data"].(map[string]any)
		if data == nil {
			return errResult(fmt.Errorf("data must be an object"))
		}
		req.Data = data
		record, err := svc.Create(ctx, colName, req)
		if err != nil {
			return errResult(err)
		}
		return textResult(record)
	case "update":
		data, _ := args["data"].(map[string]any)
		if data == nil {
			return errResult(fmt.Errorf("data must be an object"))
		}
		req.Data = data
		record, err := svc.Update(ctx, colName, db.ToString(args["id"]), req)
		if err != nil {
			return errResult(err)
		}
		return textResult(record)
	case "delete":
		if err := svc.Delete(ctx, colName, db.ToString(args["id"]), req); err != nil {
			return errResult(err)
		}
		return textResult("Record deleted.")
	}
	return errResult(fmt.Errorf("unknown tool %q", name))
}

// saveCollectionTool upserts a collection from MCP args.
func saveCollectionTool(ctx context.Context, app *core.App, args map[string]any) toolResult {
	raw, err := json.Marshal(args)
	if err != nil {
		return errResult(err)
	}
	var input schema.Collection
	if err := json.Unmarshal(raw, &input); err != nil {
		return errResult(err)
	}
	existing := app.Schema().Get(input.Name)
	target := &input
	if existing != nil {
		// Merge: keep stable field IDs by name so renames/drops behave.
		merged := existing.Clone()
		merged.Indexes = input.Indexes
		if input.Options != nil {
			merged.Options = input.Options
		}
		// Rules: only override the ones present in args (JSON null vs absent).
		applyRule := func(key string, dst **string, src *string) {
			if _, present := args[key]; present {
				*dst = src
			}
		}
		applyRule("listRule", &merged.ListRule, input.ListRule)
		applyRule("viewRule", &merged.ViewRule, input.ViewRule)
		applyRule("createRule", &merged.CreateRule, input.CreateRule)
		applyRule("updateRule", &merged.UpdateRule, input.UpdateRule)
		applyRule("deleteRule", &merged.DeleteRule, input.DeleteRule)
		if _, present := args["fields"]; present {
			var fields []*schema.Field
			for _, f := range input.Fields {
				if prev := existing.Field(f.Name); prev != nil {
					f.ID = prev.ID
					f.System = prev.System
				}
				fields = append(fields, f)
			}
			// System fields must survive.
			for _, f := range existing.Fields {
				if f.System && input.Field(f.Name) == nil {
					fields = append(fields, f)
				}
			}
			merged.Fields = fields
		}
		target = merged
	} else if target.IsAuth() {
		target.Fields = append(schema.BaseAuthFields(), target.Fields...)
	}

	if err := app.Schema().Save(ctx, target); err != nil {
		return errResult(err)
	}
	return textResult(app.Schema().Get(target.Name))
}
