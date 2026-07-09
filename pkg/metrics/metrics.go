// Package metrics exposes Prometheus text-format metrics at /metrics —
// request counts/latency by status class, process info — with no
// client-library dependency.
package metrics

import (
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
)

// Module serves /metrics ("metrics").
type Module struct{}

func (Module) ID() string { return "metrics" }

type counters struct {
	mu       sync.Mutex
	started  time.Time
	requests map[string]int64 // key: "method|class"
	durSum   float64          // seconds
	durCount int64
}

var c = &counters{started: time.Now(), requests: map[string]int64{}}

func (Module) Register(app *core.App) error {
	core.AddRequestLogFunc(func(r *http.Request, status int, dur time.Duration, _ *core.Auth) {
		class := fmt.Sprintf("%dxx", status/100)
		key := r.Method + "|" + class
		c.mu.Lock()
		c.requests[key]++
		c.durSum += dur.Seconds()
		c.durCount++
		c.mu.Unlock()
	})

	app.Mux().HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		// Optionally protect with a bearer token (metrics.token setting).
		if tok := app.Settings().String("metrics.token"); tok != "" {
			if r.Header.Get("Authorization") != "Bearer "+tok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")

		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		c.mu.Lock()
		defer c.mu.Unlock()

		fmt.Fprintf(w, "# HELP forge_uptime_seconds Process uptime.\n# TYPE forge_uptime_seconds gauge\nforge_uptime_seconds %f\n",
			time.Since(c.started).Seconds())
		fmt.Fprintf(w, "# HELP forge_goroutines Current goroutines.\n# TYPE forge_goroutines gauge\nforge_goroutines %d\n",
			runtime.NumGoroutine())
		fmt.Fprintf(w, "# HELP forge_memory_alloc_bytes Allocated heap bytes.\n# TYPE forge_memory_alloc_bytes gauge\nforge_memory_alloc_bytes %d\n",
			m.Alloc)

		fmt.Fprintf(w, "# HELP forge_http_requests_total Requests by method and status class.\n# TYPE forge_http_requests_total counter\n")
		keys := make([]string, 0, len(c.requests))
		for k := range c.requests {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			var method, class string
			fmt.Sscanf(k, "%s", &method) // key is "METHOD|class"
			for i := range k {
				if k[i] == '|' {
					method, class = k[:i], k[i+1:]
					break
				}
			}
			fmt.Fprintf(w, "forge_http_requests_total{method=%q,class=%q} %d\n", method, class, c.requests[k])
		}
		fmt.Fprintf(w, "# HELP forge_http_request_duration_seconds Aggregate request latency.\n# TYPE forge_http_request_duration_seconds summary\n")
		fmt.Fprintf(w, "forge_http_request_duration_seconds_sum %f\nforge_http_request_duration_seconds_count %d\n", c.durSum, c.durCount)
	})

	app.Settings().RegisterSection(core.SettingsSection{
		ID: "metrics", Title: "Metrics", Order: 91,
		Fields: []core.SettingsField{
			{Key: "metrics.token", Label: "/metrics bearer token", Type: "secret",
				Help: "Empty = endpoint is public. Set a token and configure your Prometheus scraper accordingly."},
		},
	})
	return nil
}
