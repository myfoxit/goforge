// Package goforge is the root facade of the GoForge framework — a modular,
// self-hostable SaaS backend: dynamic collections with instant REST + MCP
// APIs, pluggable auth (password/OAuth/OIDC/LDAP/SAML/MFA), a rules-based
// permission system, adapter-based mail and storage, realtime subscriptions,
// an embedded admin UI and Caddy-style self-updates — compiled into a single
// Go binary on SQLite, PostgreSQL or MySQL.
//
// Minimal app:
//
//	package main
//
//	import (
//		forge "github.com/myfoxit/goforge"
//		_ "github.com/myfoxit/goforge/pkg/db/drivers/sqlite"
//	)
//
//	func main() {
//		app := forge.New()
//		forge.Run(app) // parses CLI subcommands (serve, migrate, superuser, ...)
//	}
package goforge

import (
	"github.com/myfoxit/goforge/pkg/cmd"
	"github.com/myfoxit/goforge/pkg/config"
	"github.com/myfoxit/goforge/pkg/core"
)

// App re-exports the application container.
type App = core.App

// Module re-exports the module interface.
type Module = core.Module

// Config re-exports boot configuration.
type Config = config.Config

// New creates an app with configuration loaded from ./config.yaml (when
// present) and FORGE_* environment variables.
func New() *App {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		panic(err)
	}
	return core.New(cfg)
}

// NewWithConfig creates an app from explicit configuration.
func NewWithConfig(cfg *Config) *App {
	return core.New(cfg)
}

// Run executes the app binary's command line (serve, migrate, superuser,
// update, version). It is the standard main() body of a GoForge app.
func Run(app *App) {
	cmd.Run(app)
}
