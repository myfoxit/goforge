// Package cmd implements the command line of a built GoForge application
// binary: serve, migrate, superuser management, version — plus commands
// registered by modules (update, backup, jobs, ...).
package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"

	"github.com/myfoxit/goforge/pkg/core"
	"github.com/myfoxit/goforge/pkg/db"
	"github.com/myfoxit/goforge/pkg/schema"
	"github.com/myfoxit/goforge/pkg/security"
)

// Command is one app subcommand.
type Command struct {
	Name  string
	Usage string
	Run   func(app *core.App, args []string) error
}

var extra []Command

// Register adds a module-provided subcommand (call from module Register).
func Register(c Command) {
	for _, existing := range extra {
		if existing.Name == c.Name {
			return
		}
	}
	extra = append(extra, c)
}

// Run dispatches os.Args for the app binary. No args = serve.
func Run(app *core.App) {
	args := os.Args[1:]
	name := "serve"
	if len(args) > 0 {
		name = args[0]
		args = args[1:]
	}
	if err := run(app, name, args); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func run(app *core.App, name string, args []string) error {
	ctx := context.Background()
	switch name {
	case "serve":
		return app.Serve(ctx)
	case "migrate":
		if err := app.Bootstrap(ctx); err != nil {
			return err
		}
		fmt.Println("migrations applied")
		return nil
	case "superuser":
		return superuserCmd(ctx, app, args)
	case "version", "--version", "-v":
		fmt.Printf("%s (goforge %s)\n", appVersion(), core.Version)
		return nil
	case "help", "--help", "-h":
		printHelp()
		return nil
	}
	for _, c := range extra {
		if c.Name == name {
			if err := app.Bootstrap(ctx); err != nil {
				return err
			}
			return c.Run(app, args)
		}
	}
	printHelp()
	return fmt.Errorf("unknown command %q", name)
}

// AppVersion is the application's own version, stamped at build time via
// -ldflags "-X github.com/myfoxit/goforge/pkg/cmd.Version=v1.2.3".
var Version = "dev"

func appVersion() string { return Version }

func printHelp() {
	fmt.Println(`Usage: app [command]

Commands:
  serve                       Start the HTTP server (default)
  migrate                     Apply pending migrations and exit
  superuser create EMAIL PW   Create or update a superuser
  superuser list              List superusers
  superuser delete EMAIL      Delete a superuser
  version                     Print version`)
	if len(extra) > 0 {
		sorted := append([]Command{}, extra...)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
		for _, c := range sorted {
			fmt.Printf("  %-27s %s\n", c.Name, c.Usage)
		}
	}
}

func superuserCmd(ctx context.Context, app *core.App, args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: superuser create|list|delete")
	}
	if err := app.Bootstrap(ctx); err != nil {
		return err
	}
	q := app.DB().Dialect.Quote
	switch args[0] {
	case "create":
		if len(args) < 3 {
			return fmt.Errorf("usage: superuser create EMAIL PASSWORD")
		}
		email, password := args[1], args[2]
		f := schema.Field{Name: "email", Type: schema.FieldEmail}
		normalized, err := f.NormalizeValue(email)
		if err != nil {
			return err
		}
		email = db.ToString(normalized)
		if len(password) < 10 {
			return fmt.Errorf("password must be at least 10 characters")
		}
		hash, err := security.HashPassword(password)
		if err != nil {
			return err
		}
		existing, err := app.FindFirstRecord(ctx, core.SuperusersCollection, "email", email)
		if err != nil {
			return err
		}
		if existing != nil {
			_, err = app.DB().Exec(ctx,
				fmt.Sprintf("UPDATE %s SET password = ?, tokenKey = ?, updated = ? WHERE id = ?", q(core.SuperusersCollection)),
				hash, security.RandomToken(24), db.Now(), existing["id"])
			if err == nil {
				fmt.Println("superuser password updated:", email)
			}
			return err
		}
		_, err = app.DB().Exec(ctx,
			fmt.Sprintf("INSERT INTO %s (id, created, updated, email, password, tokenKey, verified, name) VALUES (?, ?, ?, ?, ?, ?, ?, ?)", q(core.SuperusersCollection)),
			security.RandomID(15), db.Now(), db.Now(), email, hash, security.RandomToken(24), true, "")
		if err == nil {
			fmt.Println("superuser created:", email)
		}
		return err
	case "list":
		rows, err := app.DB().QueryMaps(ctx,
			fmt.Sprintf("SELECT id, email, created FROM %s ORDER BY created", q(core.SuperusersCollection)))
		if err != nil {
			return err
		}
		for _, row := range rows {
			fmt.Printf("%s  %s  (created %s)\n", row["id"], row["email"], row["created"])
		}
		if len(rows) == 0 {
			fmt.Println("no superusers — create one with: superuser create EMAIL PASSWORD")
		}
		return nil
	case "delete":
		if len(args) < 2 {
			return fmt.Errorf("usage: superuser delete EMAIL")
		}
		res, err := app.DB().Exec(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE email = ?", q(core.SuperusersCollection)), args[1])
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return fmt.Errorf("superuser %q not found", args[1])
		}
		fmt.Println("superuser deleted:", args[1])
		return nil
	}
	return fmt.Errorf("unknown superuser subcommand %q", args[0])
}
