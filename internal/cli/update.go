package cli

import (
	"flag"
	"fmt"
	"strings"
)

// Update bumps the framework dependency and refreshes vendored UI components.
func Update(args []string) error {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	check := fs.Bool("check", false, "only report what would change")
	version := fs.String("to", "latest", "framework version to update to")
	if err := fs.Parse(args); err != nil {
		return err
	}
	m, dir, err := LoadManifest(".")
	if err != nil {
		return err
	}

	if *check {
		out, err := run(dir, "go", "list", "-m", "-u", "github.com/myfoxit/goforge")
		if err != nil {
			return err
		}
		fmt.Println(strings.TrimSpace(out))
		fmt.Println(DimStyle.Render("run `forge update` to apply, then `forge ui update` for components"))
		return nil
	}

	step("Updating framework dependency")
	target := "github.com/myfoxit/goforge@" + *version
	if out, err := run(dir, "go", "get", target); err != nil {
		fmt.Println(DimStyle.Render(out))
		return fmt.Errorf("go get failed: %w", err)
	}
	if _, err := run(dir, "go", "mod", "tidy"); err != nil {
		return err
	}

	// Refresh vendored UI components (skips locally modified files).
	if m.UI.Path != "" {
		step("Refreshing design system components")
		if err := uiUpdate(); err != nil {
			return err
		}
	}
	fmt.Println(OkStyle.Render("✓ update complete"))
	fmt.Println(DimStyle.Render("review the diff, run your tests, then commit"))
	return nil
}
