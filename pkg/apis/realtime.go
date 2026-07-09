package apis

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/rules"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// realtime implements PocketBase-style SSE subscriptions:
//
//	GET  /api/realtime            → SSE stream; first event carries clientId
//	POST /api/realtime            → {clientId, subscriptions: ["posts", "posts/id"]}
//
// Subscription visibility re-checks the collection rules for every event with
// the auth captured at subscribe time.
type rtClient struct {
	id     string
	ch     chan rtMessage
	auth   *core.Auth
	topics map[string]bool
}

type rtMessage struct {
	event string
	data  []byte
}

type rtHub struct {
	mu      sync.RWMutex
	clients map[string]*rtClient
}

var hub = &rtHub{clients: map[string]*rtClient{}}

func (h *rtHub) add(c *rtClient) {
	h.mu.Lock()
	h.clients[c.id] = c
	h.mu.Unlock()
}

func (h *rtHub) remove(id string) {
	h.mu.Lock()
	if c, ok := h.clients[id]; ok {
		delete(h.clients, id)
		close(c.ch)
	}
	h.mu.Unlock()
}

func (h *rtHub) get(id string) *rtClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.clients[id]
}

func (h *rtHub) snapshot() []*rtClient {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]*rtClient, 0, len(h.clients))
	for _, c := range h.clients {
		out = append(out, c)
	}
	return out
}

func registerRealtime(app *core.App, svc *Records) {
	mux := app.Mux()

	mux.HandleFunc("GET /api/realtime", func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			core.WriteError(w, app.Log(), core.BadRequest("Streaming unsupported."))
			return
		}
		client := &rtClient{
			id:     security.RandomID(20),
			ch:     make(chan rtMessage, 64),
			auth:   core.AuthFromContext(r.Context()),
			topics: map[string]bool{},
		}
		hub.add(client)
		defer hub.remove(client.id)

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-store")
		h.Set("Connection", "keep-alive")
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(200)

		fmt.Fprintf(w, "id:%s\nevent:GF_CONNECT\ndata:%s\n\n", client.id, mustJSON(map[string]string{"clientId": client.id}))
		flusher.Flush()

		keepalive := time.NewTicker(30 * time.Second)
		defer keepalive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-keepalive.C:
				fmt.Fprint(w, ":keepalive\n\n")
				flusher.Flush()
			case msg, open := <-client.ch:
				if !open {
					return
				}
				fmt.Fprintf(w, "event:%s\ndata:%s\n\n", msg.event, msg.data)
				flusher.Flush()
			}
		}
	})

	mux.HandleFunc("POST /api/realtime", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ClientID      string   `json:"clientId"`
			Subscriptions []string `json:"subscriptions"`
		}
		if err := core.ReadJSON(r, &body); err != nil {
			core.WriteError(w, app.Log(), err)
			return
		}
		client := hub.get(body.ClientID)
		if client == nil {
			core.WriteError(w, app.Log(), core.NotFound("Unknown realtime client."))
			return
		}
		// (Re)bind the auth of the subscribing request.
		client.auth = core.AuthFromContext(r.Context())
		topics := map[string]bool{}
		for _, t := range body.Subscriptions {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			colName := t
			if i := strings.IndexByte(t, '/'); i > 0 {
				colName = t[:i]
			}
			if app.Schema().Get(colName) == nil {
				core.WriteError(w, app.Log(), core.BadRequest("Unknown collection in topic: "+t))
				return
			}
			topics[t] = true
		}
		hub.mu.Lock()
		client.topics = topics
		hub.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})

	// Fan out record events.
	broadcast := func(action string) func(*core.RecordEvent) error {
		return func(e *core.RecordEvent) error {
			go fanout(app, svc, action, e.Collection, e.Record)
			return nil
		}
	}
	app.OnRecordAfterCreate.Add(broadcast("create"))
	app.OnRecordAfterUpdate.Add(broadcast("update"))
	app.OnRecordAfterDelete.Add(broadcast("delete"))
}

// fanout delivers one record event to all authorized subscribers.
func fanout(app *core.App, svc *Records, action string, c *schema.Collection, record map[string]any) {
	if c == nil || record == nil {
		return
	}
	recordID := db.ToString(record["id"])
	collectionTopic := c.Name
	recordTopic := c.Name + "/" + recordID

	clients := hub.snapshot()
	if len(clients) == 0 {
		return
	}
	serialized := svc.Serialize(c, record)
	payload := mustJSON(map[string]any{"action": action, "record": serialized})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Visibility cache per rule signature (auth id + rule) for this event.
	type verdict struct{ allowed bool }
	cache := map[string]verdict{}

	for _, client := range clients {
		hub.mu.RLock()
		wantsCol := client.topics[collectionTopic]
		wantsRec := client.topics[recordTopic]
		auth := client.auth
		hub.mu.RUnlock()
		if !wantsCol && !wantsRec {
			continue
		}

		allowed := false
		if auth.IsSuperuser() {
			allowed = true
		} else {
			rule := c.ListRule
			if wantsRec && !wantsCol {
				rule = c.ViewRule
			}
			if rule != nil {
				cacheKey := auth.ID() + "|" + *rule
				if v, ok := cache[cacheKey]; ok {
					allowed = v.allowed
				} else {
					allowed = checkEventVisibility(ctx, app, svc, c, auth, *rule, recordID, action, record)
					cache[cacheKey] = verdict{allowed}
				}
			}
		}
		if !allowed {
			continue
		}

		topic := collectionTopic
		if wantsRec {
			topic = recordTopic
		}
		select {
		case client.ch <- rtMessage{event: topic, data: payload}:
		default: // slow client: drop event rather than block the hub
		}
	}
}

// checkEventVisibility evaluates a rule for a single record. Deleted records
// no longer exist in the table, so the rule is checked with a synthetic
// single-row query built from the in-memory record for deletes.
func checkEventVisibility(ctx context.Context, app *core.App, svc *Records, c *schema.Collection, auth *core.Auth, rule, recordID, action string, record map[string]any) bool {
	if rule == "" {
		return true
	}
	req := &Request{Auth: auth, Data: map[string]any{}}
	where, args, err := rules.CompileRule(rule, svc.ruleContext(c, req))
	if err != nil {
		return false
	}
	q := app.DB().Dialect.Quote
	if action == "delete" {
		// Best effort: allow when the rule references only auth vars
		// (compiled to constant), otherwise deliver to collection topic
		// subscribers who could previously see it — conservative: constant
		// rules only.
		return where == "1=1"
	}
	row, err := app.DB().QueryMap(ctx, fmt.Sprintf(
		"SELECT 1 AS ok FROM %s WHERE %s.%s = ? AND (%s) LIMIT 1",
		svc.tableExpr(c), q(c.Name), q("id"), where),
		append([]any{recordID}, args...)...)
	return err == nil && row != nil
}

func mustJSON(v any) []byte {
	raw, _ := json.Marshal(v)
	return raw
}
