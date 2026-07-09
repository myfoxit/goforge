// Package jobs runs scheduled background work (cron expressions) inside the
// app process — pollers, cleanups, report generation, alerting.
//
//	j := jobs.New()
//	j.Add("*/5 * * * *", "poll-agents", func(ctx context.Context, app *core.App) error { ... })
//	app.Use(j)
package jobs

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/robfig/cron/v3"
)

// Func is one job body.
type Func func(ctx context.Context, app *core.App) error

type job struct {
	Spec string
	Name string
	Fn   Func

	mu      sync.Mutex
	lastRun time.Time
	lastErr string
	runs    int
}

// Module is the cron scheduler ("jobs").
type Module struct {
	jobs []*job
	cron *cron.Cron
}

// New creates the jobs module.
func New() *Module { return &Module{} }

func (*Module) ID() string { return "jobs" }

// Add schedules fn with a standard 5-field cron spec (supports @every 5m,
// @hourly, @daily aliases too).
func (m *Module) Add(spec, name string, fn Func) *Module {
	m.jobs = append(m.jobs, &job{Spec: spec, Name: name, Fn: fn})
	return m
}

func (m *Module) Register(app *core.App) error {
	app.OnServe.Add(func(e *core.ServeEvent) error {
		m.cron = cron.New()
		for _, j := range m.jobs {
			j := j
			_, err := m.cron.AddFunc(j.Spec, func() { m.run(app, j) })
			if err != nil {
				return err
			}
		}
		m.cron.Start()
		app.Log().Info("jobs scheduler started", "jobs", len(m.jobs))
		return nil
	})
	app.OnTerminate.Add(func(e *core.TerminateEvent) error {
		if m.cron != nil {
			m.cron.Stop()
		}
		return nil
	})

	// Admin visibility + manual trigger.
	app.Mux().HandleFunc("GET /api/admin/jobs", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		out := make([]map[string]any, 0, len(m.jobs))
		for _, j := range m.jobs {
			j.mu.Lock()
			item := map[string]any{
				"name": j.Name, "spec": j.Spec, "runs": j.runs, "lastError": j.lastErr,
			}
			if !j.lastRun.IsZero() {
				item["lastRun"] = j.lastRun.UTC().Format(time.RFC3339)
			}
			j.mu.Unlock()
			out = append(out, item)
		}
		core.WriteJSON(w, 200, map[string]any{"items": out})
	}))
	app.Mux().HandleFunc("POST /api/admin/jobs/{name}/run", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		for _, j := range m.jobs {
			if j.Name == r.PathValue("name") {
				go m.run(app, j)
				w.WriteHeader(http.StatusAccepted)
				return
			}
		}
		core.WriteError(w, app.Log(), core.NotFound("Unknown job."))
	}))
	return nil
}

func (m *Module) run(app *core.App, j *job) {
	defer func() {
		if rec := recover(); rec != nil {
			app.Log().Error("job panicked", "job", j.Name, "err", rec)
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	err := j.Fn(ctx, app)

	j.mu.Lock()
	j.lastRun = time.Now()
	j.runs++
	j.lastErr = ""
	if err != nil {
		j.lastErr = err.Error()
	}
	j.mu.Unlock()

	if err != nil {
		app.Log().Error("job failed", "job", j.Name, "err", err)
	} else {
		app.Log().Debug("job completed", "job", j.Name)
	}
}
