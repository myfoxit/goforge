package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var moduleNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*$`)

// ModuleNew scaffolds a custom Go module inside the current app, wiring it
// into the app so it participates in migrations, settings, routes and hooks.
func ModuleNew(name string) error {
	if !moduleNameRe.MatchString(name) {
		return fmt.Errorf("module name must be lowercase letters/digits, starting with a letter")
	}
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	pkgDir := filepath.Join(dir, "modules", name)
	if _, err := os.Stat(pkgDir); err == nil {
		return fmt.Errorf("module %q already exists", name)
	}
	if err := os.MkdirAll(pkgDir, 0o755); err != nil {
		return err
	}

	title := strings.ToUpper(name[:1]) + name[1:]
	src := fmt.Sprintf(`// Package %[1]s is a custom GoForge module for %[2]s.
package %[1]s

import (
	"net/http"

	"github.com/myfoxit/goforge/pkg/core"
)

// Module implements a custom feature. Register wires migrations, settings,
// routes and hooks into the app.
type Module struct{}

func (Module) ID() string { return %[1]q }

func (Module) Register(app *core.App) error {
	// 1. Settings (rendered generically in the admin UI):
	// app.Settings().RegisterSection(core.SettingsSection{
	// 	ID: %[1]q, Title: %[3]q, Order: 50,
	// 	Fields: []core.SettingsField{{Key: "%[1]s.example", Label: "Example", Type: "text"}},
	// })

	// 2. Custom routes:
	app.Mux().HandleFunc("GET /api/%[1]s/hello", func(w http.ResponseWriter, r *http.Request) {
		core.WriteJSON(w, http.StatusOK, map[string]string{"module": %[1]q, "status": "ok"})
	})

	// 3. React to record events:
	// app.OnRecordAfterCreate.Add(func(e *core.RecordEvent) error { return nil })

	return nil
}
`, name, m.Name, title)

	if err := os.WriteFile(filepath.Join(pkgDir, name+".go"), []byte(src), 0o644); err != nil {
		return err
	}

	fmt.Println(OkStyle.Render("✓ created ") + filepath.Join("modules", name))
	fmt.Println()
	fmt.Println("Wire it into your app in main.go:")
	fmt.Printf("  %s\n", DimStyle.Render(fmt.Sprintf(`import "%s/modules/%s"`, m.Module, name)))
	fmt.Printf("  %s\n", DimStyle.Render(fmt.Sprintf(`app.Use(%s.Module{})`, name)))
	return nil
}
