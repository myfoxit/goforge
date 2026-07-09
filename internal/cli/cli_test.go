package cli

import (
	"encoding/json"
	"io/fs"
	"os"
	"strings"
	"testing"

	"github.com/myfoxit/goforge/ui/registry"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestResolveModules(t *testing.T) {
	// perm requires auth; orgs requires auth+mail; apis is hidden/always-on.
	got := ResolveModules([]string{"perm"})
	if !contains(got, "auth") || !contains(got, "perm") || !contains(got, "apis") {
		t.Fatalf("perm should pull auth + apis: %v", got)
	}
	got = ResolveModules([]string{"orgs"})
	for _, want := range []string{"auth", "mail", "orgs", "apis"} {
		if !contains(got, want) {
			t.Fatalf("orgs missing %q: %v", want, got)
		}
	}
	// Order is stable (catalog order): apis before auth before perm.
	if idx(got, "apis") > idx(got, "auth") {
		t.Fatalf("unstable order: %v", got)
	}
	// Dedup.
	got = ResolveModules([]string{"auth", "auth", "perm"})
	seen := map[string]int{}
	for _, m := range got {
		seen[m]++
		if seen[m] > 1 {
			t.Fatalf("duplicate %q: %v", m, got)
		}
	}
}

func TestGenerateModulesFileContent(t *testing.T) {
	dir := t.TempDir()
	if err := GenerateModulesFile(dir, []string{"apis", "auth", "mcp"}); err != nil {
		t.Fatal(err)
	}
	raw := readFile(t, dir+"/modules_gen.go")
	for _, want := range []string{
		"package main",
		`"github.com/myfoxit/goforge/pkg/apis"`,
		`"github.com/myfoxit/goforge/pkg/auth"`,
		`"github.com/myfoxit/goforge/pkg/mcp"`,
		"apis.Module{}",
		"auth.Module{}",
		"mcp.Module{}",
		"func forgeModules() []forge.Module",
	} {
		if !strings.Contains(raw, want) {
			t.Errorf("modules_gen.go missing %q", want)
		}
	}
}

func TestManifestRoundtrip(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{
		Name: "demo", Module: "example.com/demo", DB: "sqlite",
		Modules: []string{"apis", "auth"},
		UI:      UIState{Path: "ui", Components: map[string]ComponentState{"button": {Hash: "abc"}}},
	}
	if err := m.Save(dir); err != nil {
		t.Fatal(err)
	}
	loaded, root, err := LoadManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if root != dir || loaded.Name != "demo" || !loaded.HasModule("auth") {
		t.Fatalf("loaded = %+v", loaded)
	}
	if loaded.UI.Components["button"].Hash != "abc" {
		t.Fatal("component state lost")
	}
	// Finds manifest from a subdirectory.
	sub := dir + "/nested/deep"
	if err := mkdirAll(sub); err != nil {
		t.Fatal(err)
	}
	if _, foundRoot, err := LoadManifest(sub); err != nil || foundRoot != dir {
		t.Fatalf("upward search failed: %v %s", err, foundRoot)
	}
}

// TestRegistryIntegrity ensures every registry.json entry references files
// that exist in the embed and that dependencies resolve.
func TestRegistryIntegrity(t *testing.T) {
	raw, err := registry.FS.ReadFile("registry.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc registryDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Components) < 15 {
		t.Fatalf("expected the full component set, got %d", len(doc.Components))
	}
	index := registryIndex(&doc)
	for _, c := range doc.Components {
		for _, f := range c.Files {
			if _, err := registry.FS.ReadFile(embedComponentDir + "/" + f); err != nil {
				t.Errorf("component %q references missing file %q", c.Name, f)
			}
		}
		for _, dep := range c.Dependencies {
			if _, ok := index[dep]; !ok {
				t.Errorf("component %q depends on unknown %q", c.Name, dep)
			}
		}
	}
	// Tokens file must exist in the embed.
	if _, err := registry.FS.ReadFile("tokens/tokens.css"); err != nil {
		t.Errorf("tokens file missing: %v", err)
	}
	// Every .svelte in the embed should be registered.
	fs.WalkDir(registry.FS, embedComponentDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || strings.HasSuffix(path, "index.ts") {
			return nil
		}
		base := strings.TrimPrefix(path, embedComponentDir+"/")
		for _, c := range doc.Components {
			for _, f := range c.Files {
				if f == base {
					return nil
				}
			}
		}
		t.Errorf("embedded file %q is not in registry.json", base)
		return nil
	})
}

func TestHashComponentDeterministic(t *testing.T) {
	doc, err := loadRegistry()
	if err != nil {
		t.Fatal(err)
	}
	comp := registryIndex(doc)["button"]
	h1 := hashComponent(comp)
	h2 := hashComponent(comp)
	if h1 == "" || h1 != h2 {
		t.Fatalf("hash not deterministic: %q vs %q", h1, h2)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func idx(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
