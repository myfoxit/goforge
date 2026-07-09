package core

import (
	"net/http"
	"sync"

	"github.com/myfoxit/goforge/pkg/schema"
)

// Hook is an ordered list of handlers for one event type. Handlers run in
// registration order; returning an error aborts the chain (and, for Before*
// hooks, the operation itself).
type Hook[T any] struct {
	mu       sync.RWMutex
	handlers []func(e T) error
}

// Add registers a handler.
func (h *Hook[T]) Add(fn func(e T) error) {
	h.mu.Lock()
	h.handlers = append(h.handlers, fn)
	h.mu.Unlock()
}

// Trigger runs all handlers, stopping at the first error.
func (h *Hook[T]) Trigger(e T) error {
	h.mu.RLock()
	handlers := h.handlers
	h.mu.RUnlock()
	for _, fn := range handlers {
		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// RecordEvent describes a record mutation. In Before* hooks Record holds the
// normalized values about to be written and may be mutated; in After* hooks
// it holds the stored row. Old is the previous row (update/delete only).
type RecordEvent struct {
	App        *App
	Action     string // create | update | delete
	Collection *schema.Collection
	Record     map[string]any
	Old        map[string]any
	// Request is the originating HTTP request, nil for programmatic calls.
	Request *http.Request
	// Auth is the acting identity, nil for unauthenticated/system calls.
	Auth *Auth
}

// AuthEvent fires on successful authentication.
type AuthEvent struct {
	App        *App
	Collection *schema.Collection
	Record     map[string]any
	Method     string // password | oauth2:<provider> | ldap | saml | token
	Request    *http.Request
}

// ServeEvent fires right before the HTTP server starts listening.
type ServeEvent struct {
	App *App
	Mux *http.ServeMux
}

// BootstrapEvent fires after the database, schema and settings are ready.
type BootstrapEvent struct {
	App *App
}

// TerminateEvent fires during graceful shutdown.
type TerminateEvent struct {
	App *App
}

// Hooks bundles all application hooks.
type Hooks struct {
	OnBootstrap Hook[*BootstrapEvent]
	OnServe     Hook[*ServeEvent]
	OnTerminate Hook[*TerminateEvent]

	OnRecordBeforeCreate Hook[*RecordEvent]
	OnRecordAfterCreate  Hook[*RecordEvent]
	OnRecordBeforeUpdate Hook[*RecordEvent]
	OnRecordAfterUpdate  Hook[*RecordEvent]
	OnRecordBeforeDelete Hook[*RecordEvent]
	OnRecordAfterDelete  Hook[*RecordEvent]

	OnAuth Hook[*AuthEvent]
}
