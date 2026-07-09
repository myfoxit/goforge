package update

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/myfoxit/goforge/pkg/cmd"
	"github.com/myfoxit/goforge/pkg/core"
)

// Module wires self-updates: settings, admin endpoints, the `update` CLI
// subcommand and the optional auto-update check ("update").
type Module struct{}

func (Module) ID() string { return "update" }

func (Module) Register(app *core.App) error {
	app.Settings().RegisterSection(core.SettingsSection{
		ID: "updates", Title: "Updates", Order: 90,
		Fields: []core.SettingsField{
			{Key: "updates.manifestURL", Label: "Manifest URL", Type: "text",
				Help: "Static JSON produced by `forge release` (leave empty to disable updates)."},
			{Key: "updates.channel", Label: "Channel", Type: "text", Default: "stable"},
			{Key: "updates.publicKey", Label: "Ed25519 public key", Type: "text",
				Help: "When set, releases must carry a valid signature."},
			{Key: "updates.autoCheck", Label: "Check daily and log available updates", Type: "bool", Default: true},
			{Key: "updates.autoApply", Label: "Apply updates automatically (restarts the app!)", Type: "bool", Default: false},
		},
	})

	cmd.Register(cmd.Command{
		Name:  "update",
		Usage: "Check for updates and install them (--check to only check)",
		Run: func(app *core.App, args []string) error {
			checkOnly := len(args) > 0 && (args[0] == "--check" || args[0] == "check")
			checker := checkerFromApp(app)
			release, err := checker.Check(context.Background())
			if err != nil {
				return err
			}
			if release == nil {
				fmt.Printf("already up to date (%s)\n", checker.Current)
				return nil
			}
			fmt.Printf("update available: %s → %s\n%s\n", checker.Current, release.Version, release.Notes)
			if checkOnly {
				return nil
			}
			fmt.Println("downloading and verifying...")
			if err := checker.Apply(context.Background(), release); err != nil {
				return err
			}
			fmt.Println("installed. restart the service to run the new version.")
			return nil
		},
	})

	// Admin endpoints.
	mux := app.Mux()
	mux.HandleFunc("GET /api/admin/update", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		checker := checkerFromApp(app)
		resp := map[string]any{"current": checker.Current, "channel": checker.Channel, "configured": checker.ManifestURL != ""}
		if checker.ManifestURL != "" {
			release, err := checker.Check(r.Context())
			if err != nil {
				resp["error"] = err.Error()
			} else if release != nil {
				resp["available"] = release.Version
				resp["notes"] = release.Notes
				resp["date"] = release.Date
			}
		}
		core.WriteJSON(w, 200, resp)
	}))

	mux.HandleFunc("POST /api/admin/update/apply", app.RequireSuperuser(func(w http.ResponseWriter, r *http.Request) {
		checker := checkerFromApp(app)
		release, err := checker.Check(r.Context())
		if err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		if release == nil {
			core.WriteError(w, app.Log(), core.BadRequest("Already up to date."))
			return
		}
		if err := checker.Apply(r.Context(), release); err != nil {
			core.WriteError(w, app.Log(), core.BadRequest(err.Error()))
			return
		}
		core.WriteJSON(w, 200, map[string]any{"installed": release.Version, "restarting": true})
		app.Log().Info("update installed, restarting", "version", release.Version)
		go func() {
			time.Sleep(500 * time.Millisecond) // let the response flush
			if err := Restart(); err != nil {
				app.Log().Error("restart failed — restart manually", "err", err)
			}
		}()
	}))

	// Daily background check.
	app.OnServe.Add(func(e *core.ServeEvent) error {
		go func() {
			for {
				time.Sleep(24 * time.Hour)
				if !app.Settings().Bool("updates.autoCheck") {
					continue
				}
				checker := checkerFromApp(app)
				if checker.ManifestURL == "" {
					continue
				}
				release, err := checker.Check(context.Background())
				if err != nil || release == nil {
					continue
				}
				if app.Settings().Bool("updates.autoApply") {
					app.Log().Info("auto-applying update", "version", release.Version)
					if err := checker.Apply(context.Background(), release); err != nil {
						app.Log().Error("auto-update failed", "err", err)
						continue
					}
					Restart()
				} else {
					app.Log().Info("update available — apply from the admin UI or `app update`",
						"current", checker.Current, "available", release.Version)
				}
			}
		}()
		return nil
	})
	return nil
}

func checkerFromApp(app *core.App) *Checker {
	return &Checker{
		ManifestURL: app.Settings().String("updates.manifestURL"),
		Channel:     app.Settings().String("updates.channel"),
		PublicKey:   app.Settings().String("updates.publicKey"),
		Current:     cmd.Version,
	}
}
