package cli

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
)

// Add installs backend modules into the current app.
func Add(args []string) error {
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	byID := CatalogByID()

	selected := args
	if len(selected) == 0 {
		// Interactive picker preselecting the current state.
		current := map[string]bool{}
		for _, id := range m.Modules {
			current[id] = true
		}
		options := []huh.Option[string]{}
		for _, def := range Catalog {
			if def.Hidden {
				continue
			}
			options = append(options,
				huh.NewOption(fmt.Sprintf("%-18s %s", def.Label, DimStyle.Render(def.Desc)), def.ID).
					Selected(current[def.ID]))
		}
		picked := m.Modules
		err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().Title("Modules").
				Description("space to toggle · enter to apply").
				Options(options...).
				Height(len(options) + 2).
				Value(&picked),
		)).Run()
		if err != nil {
			return err
		}
		return applyModules(m, dir, ResolveModules(picked))
	}

	for _, id := range selected {
		if _, ok := byID[id]; !ok {
			return fmt.Errorf("unknown module %q (see `forge modules`)", id)
		}
	}
	return applyModules(m, dir, ResolveModules(append(m.Modules, selected...)))
}

// Remove uninstalls modules (keeping requirements of the remaining set).
func Remove(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: forge remove <module...>")
	}
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	drop := map[string]bool{}
	for _, id := range args {
		drop[id] = true
	}
	var kept []string
	for _, id := range m.Modules {
		if !drop[id] {
			kept = append(kept, id)
		}
	}
	resolved := ResolveModules(kept)
	for _, id := range resolved {
		if drop[id] {
			return fmt.Errorf("module %q is required by another selected module", id)
		}
	}
	return applyModules(m, dir, resolved)
}

func applyModules(m *Manifest, dir string, modules []string) error {
	added, removed := diffModules(m.Modules, modules)
	m.Modules = modules
	if err := GenerateModulesFile(dir, modules); err != nil {
		return err
	}
	if err := m.Save(dir); err != nil {
		return err
	}
	step("Resolving Go dependencies")
	if out, err := run(dir, "go", "mod", "tidy"); err != nil {
		fmt.Println(DimStyle.Render(out))
		return fmt.Errorf("go mod tidy failed: %w", err)
	}
	if len(added) > 0 {
		fmt.Println(OkStyle.Render("✓ added: ") + strings.Join(added, ", "))
	}
	if len(removed) > 0 {
		fmt.Println(OkStyle.Render("✓ removed: ") + strings.Join(removed, ", "))
	}
	if len(added)+len(removed) == 0 {
		fmt.Println(DimStyle.Render("no changes"))
	}
	return nil
}

func diffModules(before, after []string) (added, removed []string) {
	b := map[string]bool{}
	for _, id := range before {
		b[id] = true
	}
	a := map[string]bool{}
	for _, id := range after {
		a[id] = true
		if !b[id] {
			added = append(added, id)
		}
	}
	for _, id := range before {
		if !a[id] {
			removed = append(removed, id)
		}
	}
	return
}

// ListModules prints the catalog with install markers.
func ListModules() error {
	installed := map[string]bool{}
	if m, _, err := LoadManifest("."); err == nil {
		for _, id := range m.Modules {
			installed[id] = true
		}
	}
	fmt.Println(TitleStyle.Render("Modules"))
	for _, def := range Catalog {
		marker := "  "
		if installed[def.ID] {
			marker = OkStyle.Render("✓ ")
		}
		suffix := ""
		if def.Hidden {
			suffix = DimStyle.Render(" (always on)")
		} else if len(def.Requires) > 0 {
			suffix = DimStyle.Render(" (requires " + strings.Join(def.Requires, ", ") + ")")
		}
		fmt.Printf("%s%-10s %s%s\n   %s\n", marker, def.ID, def.Label, suffix, DimStyle.Render(def.Desc))
	}
	return nil
}
