// Package webhooks delivers record events to external HTTP endpoints with
// HMAC-signed payloads and retries. Hooks are data: rows in the _webhooks
// system collection, manageable from the admin UI.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
)

// Collection stores webhook definitions.
const Collection = "_webhooks"

// Module wires outgoing webhooks ("webhooks").
type Module struct{}

func (Module) ID() string { return "webhooks" }

func (Module) Register(app *core.App) error {
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		return ensureCollection(e.App)
	})

	deliver := func(action string) func(*core.RecordEvent) error {
		return func(e *core.RecordEvent) error {
			if e.Collection.Name == Collection { // don't hook the hooks
				return nil
			}
			go dispatch(e.App, action, e)
			return nil
		}
	}
	app.OnRecordAfterCreate.Add(deliver("create"))
	app.OnRecordAfterUpdate.Add(deliver("update"))
	app.OnRecordAfterDelete.Add(deliver("delete"))
	return nil
}

func ensureCollection(app *core.App) error {
	if app.Schema().Get(Collection) != nil {
		return nil
	}
	return app.Schema().Save(context.Background(), &schema.Collection{
		Name: Collection, Type: schema.TypeBase, System: true,
		Fields: []*schema.Field{
			{Name: "name", Type: schema.FieldText, Required: true, System: true},
			{Name: "url", Type: schema.FieldURL, Required: true, System: true},
			{Name: "collections", Type: schema.FieldJSON, System: true,
				Options: map[string]any{"maxSize": float64(4096)}}, // ["posts","*"]
			{Name: "actions", Type: schema.FieldJSON, System: true}, // ["create","update","delete"]
			{Name: "secret", Type: schema.FieldText, Hidden: true, System: true},
			{Name: "enabled", Type: schema.FieldBool, System: true},
			{Name: "lastStatus", Type: schema.FieldText, System: true},
		},
	})
}

var client = &http.Client{Timeout: 20 * time.Second}

func dispatch(app *core.App, action string, e *core.RecordEvent) {
	hooks, err := app.DB().QueryMaps(context.Background(),
		"SELECT * FROM "+app.DB().Dialect.Quote(Collection)+" WHERE enabled = ?", true)
	if err != nil {
		return
	}
	for _, hook := range hooks {
		if !matches(db.ToJSONList(hook["collections"]), e.Collection.Name) {
			continue
		}
		if !matches(db.ToJSONList(hook["actions"]), action) {
			continue
		}
		go send(app, hook, action, e)
	}
}

func matches(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return true
	}
	for _, p := range patterns {
		if p == "*" || p == value {
			return true
		}
	}
	return false
}

func send(app *core.App, hook map[string]any, action string, e *core.RecordEvent) {
	payload, _ := json.Marshal(map[string]any{
		"action":     action,
		"collection": e.Collection.Name,
		"record":     publicShape(e.Collection, e.Record),
		"timestamp":  db.Now(),
	})
	url := db.ToString(hook["url"])
	secret := db.ToString(hook["secret"])

	status := ""
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("POST", url, bytes.NewReader(payload))
		if err != nil {
			status = err.Error()
			break
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "GoForge-Webhooks/1.0")
		req.Header.Set("X-Forge-Event", action)
		if secret != "" {
			mac := hmac.New(sha256.New, []byte(secret))
			mac.Write(payload)
			req.Header.Set("X-Forge-Signature", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			status = fmt.Sprintf("%d @ %s", resp.StatusCode, db.Now())
			if resp.StatusCode < 300 {
				break
			}
		} else {
			status = err.Error()
		}
		time.Sleep(time.Duration(attempt*attempt) * time.Second)
	}

	q := app.DB().Dialect.Quote
	app.DB().Exec(context.Background(), fmt.Sprintf(
		"UPDATE %s SET %s = ? WHERE id = ?", q(Collection), q("lastStatus")),
		status, hook["id"])
}

// publicShape strips hidden fields without depending on the apis package.
func publicShape(c *schema.Collection, record map[string]any) map[string]any {
	out := map[string]any{
		"id":      db.ToString(record["id"]),
		"created": db.ToString(record["created"]),
		"updated": db.ToString(record["updated"]),
	}
	for _, f := range c.Fields {
		if f.Hidden {
			continue
		}
		if v, ok := record[f.Name]; ok {
			out[f.Name] = f.APIValue(v)
		}
	}
	return out
}
