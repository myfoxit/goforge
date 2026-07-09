// Package mcp exposes a GoForge app as a Model Context Protocol server:
// every collection becomes a set of typed tools (list/get/create/update/
// delete) and admin keys additionally get schema-building tools — so an LLM
// can both use and build the application.
//
// The server implements MCP streamable HTTP (JSON responses, stateless) at
// POST /api/mcp. Authentication: GoForge API keys (Bearer forge_... or
// X-Api-Key) or a superuser JWT.
package mcp

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/myfoxit/goforge/pkg/apis"
	"github.com/myfoxit/goforge/pkg/core"
)

// protocolVersions supported, newest first.
var protocolVersions = []string{"2025-06-18", "2025-03-26", "2024-11-05"}

// Module mounts the MCP endpoint and API key management ("mcp").
type Module struct{}

func (Module) ID() string { return "mcp" }

func (Module) Register(app *core.App) error {
	app.OnBootstrap.Add(func(e *core.BootstrapEvent) error {
		return ensureAPIKeysCollection(e.App)
	})
	app.AddAuthResolver(apiKeyResolver(app))
	registerAPIKeyRoutes(app)

	svc := apis.NewRecords(app)
	mux := app.Mux()

	mux.HandleFunc("POST /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		serveMCP(app, svc, w, r)
	})
	mux.HandleFunc("GET /api/mcp", func(w http.ResponseWriter, r *http.Request) {
		// Stateless server: no SSE stream to resume.
		w.WriteHeader(http.StatusMethodNotAllowed)
	})

	// Connection info for the admin UI "Connect to AI" screen.
	mux.HandleFunc("GET /api/mcp/info", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		endpoint := app.BaseURL() + "/api/mcp"
		core.WriteJSON(w, 200, map[string]any{
			"endpoint": endpoint,
			"snippets": map[string]string{
				"claudeCode": fmt.Sprintf(`claude mcp add --transport http %s %s --header "Authorization: Bearer YOUR_API_KEY"`,
					sanitizeName(app.AppName()), endpoint),
				"mcpJson": fmt.Sprintf(`{
  "mcpServers": {
    "%s": {
      "type": "http",
      "url": "%s",
      "headers": { "Authorization": "Bearer YOUR_API_KEY" }
    }
  }
}`, sanitizeName(app.AppName()), endpoint),
			},
		})
	}))
	return nil
}

// ---- JSON-RPC 2.0 ----

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

func writeRPC(w http.ResponseWriter, id json.RawMessage, result any, rpcErr *rpcError) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: id, Result: result, Error: rpcErr})
}

func serveMCP(app *core.App, svc *apis.Records, w http.ResponseWriter, r *http.Request) {
	auth := core.AuthFromContext(r.Context())
	if auth == nil {
		w.Header().Set("WWW-Authenticate", `Bearer realm="mcp"`)
		core.WriteJSON(w, http.StatusUnauthorized, map[string]string{
			"error": "Authentication required: pass an API key as 'Authorization: Bearer forge_...'.",
		})
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		writeRPC(w, nil, nil, &rpcError{Code: codeParse, Message: "Parse error: " + err.Error()})
		return
	}
	if req.JSONRPC != "2.0" {
		writeRPC(w, req.ID, nil, &rpcError{Code: codeInvalidRequest, Message: "jsonrpc must be \"2.0\""})
		return
	}

	// Notifications get a 202 with no body.
	if len(req.ID) == 0 || string(req.ID) == "null" {
		w.WriteHeader(http.StatusAccepted)
		return
	}

	switch req.Method {
	case "initialize":
		var params struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		json.Unmarshal(req.Params, &params)
		version := protocolVersions[0]
		for _, v := range protocolVersions {
			if v == params.ProtocolVersion {
				version = v
				break
			}
		}
		writeRPC(w, req.ID, map[string]any{
			"protocolVersion": version,
			"capabilities": map[string]any{
				"tools": map[string]any{"listChanged": false},
			},
			"serverInfo": map[string]any{
				"name":    sanitizeName(app.AppName()),
				"title":   app.AppName(),
				"version": core.Version,
			},
			"instructions": serverInstructions(app, auth),
		}, nil)

	case "ping":
		writeRPC(w, req.ID, map[string]any{}, nil)

	case "tools/list":
		writeRPC(w, req.ID, map[string]any{"tools": buildTools(app, auth)}, nil)

	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &params); err != nil {
			writeRPC(w, req.ID, nil, &rpcError{Code: codeInvalidParams, Message: err.Error()})
			return
		}
		result := callTool(app, svc, r, auth, params.Name, params.Arguments)
		writeRPC(w, req.ID, result, nil)

	default:
		writeRPC(w, req.ID, nil, &rpcError{Code: codeMethodNotFound, Message: "Method not found: " + req.Method})
	}
}

func serverInstructions(app *core.App, auth *core.Auth) string {
	base := fmt.Sprintf("This is the API of %q, a GoForge application. "+
		"Every collection has list/get/create/update/delete tools. "+
		"List tools accept `filter` expressions like: status = 'active' && created >= '2025-01-01' "+
		"(operators: = != > >= < <= ~ !~ && ||; ~ means contains).",
		app.AppName())
	if auth.IsSuperuser() {
		base += " You also have schema tools (collections_*) to create and evolve collections, fields and access rules — you can build the application itself."
	}
	return base
}

func sanitizeName(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-':
			out = append(out, r)
		case r == ' ':
			out = append(out, '-')
		}
	}
	if len(out) == 0 {
		return "goforge"
	}
	return string(out)
}
