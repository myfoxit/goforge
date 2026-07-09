package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/myfoxit/goforge/ui/registry"
)

// RegistryComponent mirrors an entry in ui/registry/registry.json.
type RegistryComponent struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	Files        []string `json:"files"`
	Dependencies []string `json:"dependencies"` // other components
	Default      bool     `json:"default"`
}

// registryDoc is the registry.json envelope.
type registryDoc struct {
	Target     string              `json:"target"`     // install path under the UI, e.g. src/lib/components/ui
	TokensFile string              `json:"tokensFile"` // e.g. src/app.css
	Components []RegistryComponent `json:"components"`
}

// embedComponentDir is the directory inside the embedded registry that holds
// component source files (destination path is doc.Target).
const embedComponentDir = "components"

func loadRegistry() (*registryDoc, error) {
	raw, err := registry.FS.ReadFile("registry.json")
	if err != nil {
		return nil, err
	}
	var doc registryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return &doc, nil
}

func registryIndex(doc *registryDoc) map[string]RegistryComponent {
	out := make(map[string]RegistryComponent, len(doc.Components))
	for _, c := range doc.Components {
		out[c.Name] = c
	}
	return out
}

func defaultComponents() []string {
	doc, err := loadRegistry()
	if err != nil {
		return nil
	}
	var out []string
	for _, c := range doc.Components {
		if c.Default {
			out = append(out, c.Name)
		}
	}
	return out
}

// UI dispatches `forge ui <sub>`.
func UI(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: forge ui add|update|list")
	}
	switch args[0] {
	case "add":
		return uiAdd(args[1:])
	case "update":
		return uiUpdate()
	case "list":
		return uiList()
	}
	return fmt.Errorf("unknown ui subcommand %q", args[0])
}

func uiList() error {
	doc, err := loadRegistry()
	if err != nil {
		return err
	}
	installed := map[string]bool{}
	if m, _, err := LoadManifest("."); err == nil {
		for name := range m.UI.Components {
			installed[name] = true
		}
	}
	fmt.Println(TitleStyle.Render("Design system components"))
	for _, c := range doc.Components {
		marker := "  "
		if installed[c.Name] {
			marker = OkStyle.Render("✓ ")
		}
		fmt.Printf("%s%-16s %s\n", marker, c.Name, DimStyle.Render(c.Description))
	}
	return nil
}

func uiAdd(names []string) error {
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	if m.UI.Path == "" {
		return fmt.Errorf("this app has no frontend (init with --ui)")
	}
	doc, err := loadRegistry()
	if err != nil {
		return err
	}

	if len(names) == 0 {
		installed := map[string]bool{}
		for name := range m.UI.Components {
			installed[name] = true
		}
		options := make([]huh.Option[string], 0, len(doc.Components))
		for _, c := range doc.Components {
			options = append(options,
				huh.NewOption(fmt.Sprintf("%-16s %s", c.Name, DimStyle.Render(c.Description)), c.Name).
					Selected(installed[c.Name]))
		}
		picked := []string{}
		for name := range installed {
			picked = append(picked, name)
		}
		if err := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().Title("Components").
				Description("space to toggle · enter to install").
				Options(options...).Height(len(options) + 2).Value(&picked),
		)).Run(); err != nil {
			return err
		}
		names = picked
	}

	added, err := copyComponents(dir, m, names, false)
	if err != nil {
		return err
	}
	if err := m.Save(dir); err != nil {
		return err
	}
	if len(added) == 0 {
		fmt.Println(DimStyle.Render("nothing to add"))
	} else {
		fmt.Println(OkStyle.Render("✓ installed: ") + strings.Join(added, ", "))
	}
	return nil
}

// uiUpdate re-copies components whose registry hash changed, unless the user
// locally modified the file (hash differs from the recorded one).
func uiUpdate() error {
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	doc, err := loadRegistry()
	if err != nil {
		return err
	}
	index := registryIndex(doc)

	var names []string
	for name := range m.UI.Components {
		names = append(names, name)
	}
	sort.Strings(names)

	updated, skipped := 0, 0
	for _, name := range names {
		comp, ok := index[name]
		if !ok {
			continue
		}
		registryHash := hashComponent(comp)
		recorded := m.UI.Components[name].Hash
		if registryHash == recorded {
			continue // already current
		}
		// Local modification check.
		onDisk := hashOnDisk(doc, dir, m.UI.Path, comp)
		if onDisk != "" && onDisk != recorded {
			fmt.Println(ErrStyle.Render("! ") + name + DimStyle.Render(" — locally modified, skipping (use --force to overwrite)"))
			skipped++
			continue
		}
		if _, err := copyComponents(dir, m, []string{name}, true); err != nil {
			return err
		}
		updated++
		fmt.Println(OkStyle.Render("✓ updated ") + name)
	}
	if err := m.Save(dir); err != nil {
		return err
	}
	fmt.Printf("%s %d updated, %d skipped\n", DimStyle.Render("done:"), updated, skipped)
	return nil
}

// copyComponents vendors components (and their deps + tokens) into the app UI.
func copyComponents(appDir string, m *Manifest, names []string, force bool) ([]string, error) {
	doc, err := loadRegistry()
	if err != nil {
		return nil, err
	}
	index := registryIndex(doc)
	uiRoot := filepath.Join(appDir, m.UI.Path)

	// Always ensure tokens are present.
	if doc.TokensFile != "" {
		if err := copyRegistryFile(uiRoot, doc.TokensFile, "tokens/"+filepath.Base(doc.TokensFile)); err != nil {
			// tokens are optional if the scaffold already shipped them
			_ = err
		}
	}

	want := map[string]bool{}
	var order []string
	var visit func(name string) error
	visit = func(name string) error {
		if want[name] {
			return nil
		}
		comp, ok := index[name]
		if !ok {
			return fmt.Errorf("unknown component %q", name)
		}
		want[name] = true
		for _, dep := range comp.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		order = append(order, name)
		return nil
	}
	for _, n := range names {
		if err := visit(n); err != nil {
			return nil, err
		}
	}

	var installed []string
	for _, name := range order {
		comp := index[name]
		if _, exists := m.UI.Components[name]; exists && !force {
			// present already; keep unless caller forces
		}
		for _, f := range comp.Files {
			if err := copyRegistryFile(uiRoot, doc.Target+"/"+f, embedComponentDir+"/"+f); err != nil {
				return nil, err
			}
		}
		m.UI.Components[name] = ComponentState{Hash: hashComponent(comp)}
		installed = append(installed, name)
	}
	return installed, nil
}

// copyRegistryFile copies srcRel (inside the embedded registry) to
// <uiRoot>/<dstRel>.
func copyRegistryFile(uiRoot, dstRel, srcRel string) error {
	raw, err := registry.FS.ReadFile(srcRel)
	if err != nil {
		return err
	}
	dst := filepath.Join(uiRoot, filepath.FromSlash(dstRel))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, raw, 0o644)
}

func hashComponent(comp RegistryComponent) string {
	h := sha256.New()
	for _, f := range comp.Files {
		raw, _ := registry.FS.ReadFile(embedComponentDir + "/" + f)
		h.Write(raw)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

func hashOnDisk(doc *registryDoc, appDir, uiPath string, comp RegistryComponent) string {
	h := sha256.New()
	found := false
	for _, f := range comp.Files {
		raw, err := os.ReadFile(filepath.Join(appDir, uiPath, filepath.FromSlash(doc.Target), filepath.FromSlash(f)))
		if err != nil {
			continue
		}
		found = true
		h.Write(raw)
	}
	if !found {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// listRegistryFiles is a helper for tests/tools.
func listRegistryFiles() ([]string, error) {
	var out []string
	err := fs.WalkDir(registry.FS, "components", func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			out = append(out, path)
		}
		return err
	})
	return out, err
}
