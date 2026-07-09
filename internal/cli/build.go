package cli

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
)

// Dev runs the Go API and (if present) the SvelteKit dev server together.
func Dev(args []string) error {
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(bgContext(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var wg sync.WaitGroup
	launch := func(name, workdir string, argv ...string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
			cmd.Dir = workdir
			cmd.Stdout, cmd.Stderr, cmd.Stdin = os.Stdout, os.Stderr, os.Stdin
			fmt.Println(DimStyle.Render("▶ " + name + ": " + join(argv)))
			cmd.Run()
		}()
	}

	launch("api", dir, "go", "run", ".", "serve")
	if m.UI.Path != "" {
		uiDir := filepath.Join(dir, m.UI.Path)
		if _, err := os.Stat(filepath.Join(uiDir, "node_modules")); os.IsNotExist(err) {
			fmt.Println(DimStyle.Render("• installing frontend dependencies (first run)..."))
			install := exec.Command("npm", "install")
			install.Dir = uiDir
			install.Stdout, install.Stderr = os.Stdout, os.Stderr
			install.Run()
		}
		launch("web", uiDir, "npm", "run", "dev")
	}
	fmt.Println(OkStyle.Render("✓ dev servers running") + DimStyle.Render("  (ctrl-c to stop)"))
	<-ctx.Done()
	wg.Wait()
	return nil
}

// Build compiles the production binary (embedding the UI when present).
func Build(args []string) error {
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}
	if m.UI.Path != "" {
		uiDir := filepath.Join(dir, m.UI.Path)
		step("Building frontend")
		if err := stream(uiDir, "npm", "install"); err != nil {
			return err
		}
		if err := stream(uiDir, "npm", "run", "build"); err != nil {
			return err
		}
	}
	step("Compiling binary")
	out := filepath.Join(dir, "dist", m.Name)
	ldflags := "-s -w -X github.com/myfoxit/goforge/pkg/cmd.Version=" + gitVersion(dir)
	if err := stream(dir, "go", "build", "-ldflags", ldflags, "-o", out, "."); err != nil {
		return err
	}
	fmt.Println(OkStyle.Render("✓ built ") + out)
	return nil
}

func stream(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func gitVersion(dir string) string {
	cmd := exec.Command("git", "describe", "--tags", "--always")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	return trimSpace(string(out))
}

func join(argv []string) string {
	s := ""
	for i, a := range argv {
		if i > 0 {
			s += " "
		}
		s += a
	}
	return s
}
