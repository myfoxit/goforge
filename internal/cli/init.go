package cli

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Version of the forge CLI (stamped at build time).
var Version = "0.1.0-dev"

// Styles.
var (
	TitleStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	DimStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	OkStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	ErrStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	BoxStyle   = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("212")).Padding(1, 2)
)

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Init scaffolds a new application.
func Init(args []string) error {
	// Allow the target dir before the flags: `forge init mydir --db sqlite`.
	// Go's flag package stops at the first positional, so pull a leading
	// non-flag argument out first.
	leadingDir := ""
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		leadingDir = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	name := fs.String("name", "", "application name (lowercase, dashes)")
	modulePath := fs.String("module", "", "Go module path (e.g. github.com/you/app)")
	dbDriver := fs.String("db", "", "database: sqlite|postgres|mysql")
	moduleList := fs.String("modules", "", "comma-separated module ids")
	template := fs.String("template", "", "frontend template: minimal|demo|saas")
	withUI := fs.Bool("ui", true, "scaffold a frontend (--ui=false ⇒ minimal, API-only)")
	local := fs.String("local", "", "path to a local goforge checkout (adds replace directive)")
	yes := fs.Bool("yes", false, "accept defaults, no prompts")
	gitInit := fs.Bool("git", true, "initialize a git repository")
	if err := fs.Parse(args); err != nil {
		return err
	}

	targetDir := leadingDir
	if targetDir == "" {
		targetDir = fs.Arg(0)
	}
	interactive := !*yes && *name == ""

	// Defaults.
	selected := defaultModuleIDs()
	if *moduleList != "" {
		selected = splitList(*moduleList)
	}
	if *dbDriver == "" {
		*dbDriver = "sqlite"
	}

	// Resolve the scaffold template. --ui=false is shorthand for the API-only
	// "minimal" flavor; an explicit --template always wins.
	templateID := *template
	if templateID == "" && !*withUI {
		templateID = "minimal"
	}

	if interactive {
		if templateID == "" {
			templateID = "saas" // highlight the full starter by default
		}
		fmt.Println(TitleStyle.Render("⚒  GoForge") + DimStyle.Render("  — let's build a SaaS"))
		if err := runInitForm(name, modulePath, dbDriver, &templateID, &selected); err != nil {
			return err
		}
	}
	if templateID == "" {
		templateID = "demo" // non-interactive default preserves prior behavior
	}
	tpl, ok := TemplateByID(templateID)
	if !ok {
		return fmt.Errorf("unknown template %q (want minimal|demo|saas)", templateID)
	}
	// A template pulls in the backend modules it depends on.
	selected = append(selected, tpl.Modules...)
	if *name == "" {
		return fmt.Errorf("missing --name")
	}
	if !nameRe.MatchString(*name) {
		return fmt.Errorf("name must be lowercase letters/digits/dashes, starting with a letter")
	}
	if *modulePath == "" {
		*modulePath = "example.com/" + *name
	}
	if targetDir == "" {
		targetDir = *name
	}
	valid := false
	for _, d := range DBDrivers {
		if d.ID == *dbDriver {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("unknown db driver %q", *dbDriver)
	}

	abs, err := filepath.Abs(targetDir)
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(abs); err == nil && len(entries) > 0 {
		return fmt.Errorf("directory %s is not empty", abs)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return err
	}

	modules := ResolveModules(selected)
	localPath := *local
	if localPath != "" {
		localPath, err = filepath.Abs(localPath)
		if err != nil {
			return err
		}
		if rel, err := filepath.Rel(abs, localPath); err == nil {
			localPath = rel
		}
	}

	data := ScaffoldData{
		Name:           *name,
		Module:         *modulePath,
		DB:             *dbDriver,
		Modules:        modules,
		GoForgeVersion: frameworkVersion(),
		LocalFramework: localPath,
		UI:             tpl.UI,
		Template:       tpl.ID,
	}
	step("Scaffolding application")
	if err := RenderScaffold(abs, data); err != nil {
		return err
	}
	if err := GenerateModulesFile(abs, modules); err != nil {
		return err
	}

	manifest := &Manifest{
		Name:     *name,
		Module:   *modulePath,
		GoForge:  data.GoForgeVersion,
		DB:       *dbDriver,
		Template: tpl.ID,
		Modules:  modules,
		UI:       UIState{Components: map[string]ComponentState{}},
	}
	if tpl.UI {
		manifest.UI.Path = "ui"
		step("Vendoring design system components")
		if _, err := copyComponents(abs, manifest, allComponents(), false); err != nil {
			return err
		}
	}
	if err := manifest.Save(abs); err != nil {
		return err
	}

	step("Resolving Go dependencies")
	if out, err := run(abs, "go", "mod", "tidy"); err != nil {
		fmt.Println(DimStyle.Render(out))
		return fmt.Errorf("go mod tidy failed: %w (is the framework path reachable?)", err)
	}
	if *gitInit {
		if _, err := run(abs, "git", "init", "-q"); err == nil {
			run(abs, "git", "add", "-A")
		}
	}

	next := []string{
		"cd " + targetDir,
		"go run . superuser create you@example.com <password>",
		"go run . serve",
		"open http://localhost:8090/_/   # admin",
	}
	if tpl.UI {
		next = append(next, "forge dev                       # api + frontend hot reload")
	}
	fmt.Println()
	fmt.Println(BoxStyle.Render(
		OkStyle.Render("✓ "+*name+" is ready") + "\n\n" +
			DimStyle.Render("modules: ") + strings.Join(modules, ", ") + "\n\n" +
			strings.Join(next, "\n")))
	return nil
}

func runInitForm(name, modulePath, dbDriver, template *string, selected *[]string) error {
	dbOptions := make([]huh.Option[string], len(DBDrivers))
	for i, d := range DBDrivers {
		dbOptions[i] = huh.NewOption(fmt.Sprintf("%s — %s", d.Label, d.Desc), d.ID)
	}
	tplOptions := make([]huh.Option[string], len(Templates))
	for i, t := range Templates {
		tplOptions[i] = huh.NewOption(fmt.Sprintf("%-18s %s", t.Label, DimStyle.Render(t.Desc)), t.ID)
	}
	moduleOptions := []huh.Option[string]{}
	defaults := map[string]bool{}
	for _, id := range *selected {
		defaults[id] = true
	}
	for _, m := range Catalog {
		if m.Hidden {
			continue
		}
		moduleOptions = append(moduleOptions,
			huh.NewOption(fmt.Sprintf("%-18s %s", m.Label, DimStyle.Render(m.Desc)), m.ID).
				Selected(defaults[m.ID]))
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Application name").
				Description("lowercase, e.g. northplane").
				Placeholder("myapp").
				Validate(func(s string) error {
					if !nameRe.MatchString(s) {
						return fmt.Errorf("lowercase letters, digits and dashes only")
					}
					return nil
				}).
				Value(name),
			huh.NewInput().Title("Go module path").
				Description("e.g. github.com/acme/northplane (enter to derive)").
				Value(modulePath),
			huh.NewSelect[string]().Title("Database").
				Options(dbOptions...).
				Value(dbDriver),
		),
		huh.NewGroup(
			huh.NewSelect[string]().Title("Template").
				Description("what to scaffold — 'Full SaaS starter' gives a ready base app").
				Options(tplOptions...).
				Value(template),
			huh.NewMultiSelect[string]().Title("Modules").
				Description("space to toggle · enter to confirm — the SaaS starter adds its own").
				Options(moduleOptions...).
				Height(len(moduleOptions)+2).
				Value(selected),
		),
	)
	return form.Run()
}

func defaultModuleIDs() []string {
	var out []string
	for _, m := range Catalog {
		if m.Default && !m.Hidden {
			out = append(out, m.ID)
		}
	}
	return out
}

// frameworkVersion returns the goforge version to pin in generated apps.
func frameworkVersion() string {
	if Version != "" && Version != "0.1.0-dev" {
		return "v" + strings.TrimPrefix(Version, "v")
	}
	return "v0.1.0"
}

func step(msg string) {
	fmt.Println(DimStyle.Render("•"), msg)
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func run(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}
