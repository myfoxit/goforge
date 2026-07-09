// Command forge is the GoForge CLI: scaffold applications, pick modules
// shadcn-style, vendor UI components, run dev servers, build single-binary
// releases and publish self-update manifests.
package main

import (
	"fmt"
	"os"

	"github.com/myfoxit/goforge/internal/cli"
)

const usage = `forge — the GoForge application builder

Usage:
  forge init [dir]              Create a new application (interactive)
  forge add [module...]         Add backend modules (interactive without args)
  forge remove <module...>      Remove backend modules
  forge modules                 List available modules
  forge ui add [component...]   Vendor design-system components (interactive)
  forge ui update               Refresh vendored components (hash-aware)
  forge ui list                 List available components
  forge update [--check]        Update the framework dependency + components
  forge dev                     Run the app + frontend dev servers
  forge build                   Build the production binary (embeds the UI)
  forge release                 Cross-compile + write a self-update manifest
  forge release keygen          Generate an ed25519 release signing key
  forge module new <name>       Scaffold a custom Go module
  forge version                 Print version

Flags for non-interactive init:
  --name NAME --module PATH --db sqlite|postgres|mysql
  --modules a,b,c --ui=true|false --local PATH --yes
`

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		fmt.Print(usage)
		return
	}
	var err error
	switch args[0] {
	case "init":
		err = cli.Init(args[1:])
	case "add":
		err = cli.Add(args[1:])
	case "remove":
		err = cli.Remove(args[1:])
	case "modules":
		err = cli.ListModules()
	case "ui":
		err = cli.UI(args[1:])
	case "update":
		err = cli.Update(args[1:])
	case "dev":
		err = cli.Dev(args[1:])
	case "build":
		err = cli.Build(args[1:])
	case "release":
		err = cli.Release(args[1:])
	case "module":
		if len(args) >= 3 && args[1] == "new" {
			err = cli.ModuleNew(args[2])
		} else {
			err = fmt.Errorf("usage: forge module new <name>")
		}
	case "version", "--version", "-v":
		fmt.Println("forge", cli.Version)
	case "help", "--help", "-h":
		fmt.Print(usage)
	default:
		fmt.Print(usage)
		err = fmt.Errorf("unknown command %q", args[0])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, cli.ErrStyle.Render("✗ ")+err.Error())
		os.Exit(1)
	}
}
