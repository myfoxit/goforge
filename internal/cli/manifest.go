package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest is forge.json — the app's module/component state that makes
// `forge add`, `forge ui update` and `forge update` deterministic.
type Manifest struct {
	Name     string   `json:"name"`
	Module   string   `json:"module"`
	GoForge  string   `json:"goforge"`
	DB       string   `json:"db"`
	Template string   `json:"template,omitempty"` // minimal | demo | saas
	Modules  []string `json:"modules"`
	UI       UIState  `json:"ui"`
}

// UIState tracks the frontend and its vendored design-system components.
type UIState struct {
	Path       string                    `json:"path,omitempty"`
	Components map[string]ComponentState `json:"components,omitempty"`
}

// ComponentState pins one copied component.
type ComponentState struct {
	Hash string `json:"hash"` // sha256 of the copied source files
}

const manifestName = "forge.json"

// LoadManifest reads forge.json from dir (or any parent).
func LoadManifest(dir string) (*Manifest, string, error) {
	current, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	for {
		path := filepath.Join(current, manifestName)
		raw, err := os.ReadFile(path)
		if err == nil {
			var m Manifest
			if err := json.Unmarshal(raw, &m); err != nil {
				return nil, "", fmt.Errorf("parse %s: %w", path, err)
			}
			if m.UI.Components == nil {
				m.UI.Components = map[string]ComponentState{}
			}
			return &m, current, nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil, "", fmt.Errorf("no %s found — run `forge init` first or cd into a GoForge app", manifestName)
		}
		current = parent
	}
}

// Save writes forge.json into dir.
func (m *Manifest) Save(dir string) error {
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, manifestName), append(raw, '\n'), 0o644)
}

// HasModule reports module membership.
func (m *Manifest) HasModule(id string) bool {
	for _, x := range m.Modules {
		if x == id {
			return true
		}
	}
	return false
}
