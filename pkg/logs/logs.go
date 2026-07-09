// Package logs persists request logs to the _logs system collection with
// async buffered writes and retention cleanup. Superusers browse them via
// the standard records API (and the admin UI Logs screen).
package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// Collection is the system logs collection.
const Collection = "_logs"

type entry struct {
	method   string
	path     string
	status   int
	duration time.Duration
	ip       string
	authID   string
	referer  string
}

// Module wires request logging ("logs").
type Module struct{}

func (Module) ID() string { return "logs" }

func (Module) Register(app *core.App) error {
	if !app.Config().Log.Requests {
		return nil
	}
	ch := make(chan entry, 512)

	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		if err := ensureCollection(e.App); err != nil {
			return err
		}
		go writer(e.App, ch)
		go cleaner(e.App)
		return nil
	})

	core.SetRequestLogFunc(func(r *http.Request, status int, dur time.Duration, auth *core.Auth) {
		// Skip noise: health checks, realtime stream holds, admin assets.
		p := r.URL.Path
		if p == "/api/health" || p == "/api/realtime" || len(p) < 5 || p[:5] != "/api/" {
			return
		}
		e := entry{
			method: r.Method, path: p, status: status, duration: dur,
			ip: app.ClientIP(r), authID: auth.ID(), referer: r.Referer(),
		}
		select {
		case ch <- e:
		default: // full buffer: drop rather than slow requests down
		}
	})
	return nil
}

func ensureCollection(app *core.App) error {
	if app.Schema().Get(Collection) != nil {
		return nil
	}
	return app.Schema().Save(context.Background(), &schema.Collection{
		Name: Collection, Type: schema.TypeBase, System: true,
		Fields: []*schema.Field{
			{Name: "method", Type: schema.FieldText, System: true},
			{Name: "path", Type: schema.FieldText, System: true},
			{Name: "status", Type: schema.FieldNumber, System: true},
			{Name: "durationMs", Type: schema.FieldNumber, System: true},
			{Name: "ip", Type: schema.FieldText, System: true},
			{Name: "auth", Type: schema.FieldText, System: true},
			{Name: "data", Type: schema.FieldJSON, System: true},
		},
		Indexes: []schema.Index{
			{Name: "ix_logs_created", Columns: []string{"created"}},
		},
	})
}

func writer(app *core.App, ch <-chan entry) {
	q := app.DB().Dialect.Quote
	insert := fmt.Sprintf(
		"INSERT INTO %s (id, created, updated, method, path, status, %s, ip, auth, data) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		q(Collection), q("durationMs"))
	for e := range ch {
		now := db.Now()
		data, _ := json.Marshal(map[string]any{"referer": e.referer})
		_, err := app.DB().Exec(context.Background(), insert,
			security.RandomID(15), now, now,
			e.method, e.path, float64(e.status), float64(e.duration.Milliseconds()),
			e.ip, e.authID, string(data))
		if err != nil {
			app.Log().Debug("request log write failed", "err", err)
		}
	}
}

// cleaner prunes logs beyond the retention window once an hour.
func cleaner(app *core.App) {
	days := app.Config().Log.RetentionDays
	if days <= 0 {
		return
	}
	q := app.DB().Dialect.Quote
	for {
		cutoff := time.Now().UTC().AddDate(0, 0, -days).Format(db.DateFormat)
		app.DB().Exec(context.Background(),
			fmt.Sprintf("DELETE FROM %s WHERE created < ?", q(Collection)), cutoff)
		time.Sleep(time.Hour)
	}
}
