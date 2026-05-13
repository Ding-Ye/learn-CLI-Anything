// manifest.go re-declares the Manifest shape from s06's hub-registry. We don't
// import s06 (the curriculum forbids cross-session imports), so the struct is
// duplicated verbatim. The JSON tags match upstream's installed.json layout
// closely enough that a Go-written ledger interoperates with the Python tools.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest is the per-CLI registry entry, identical to s06's. The Backend
// field is what the installer dispatches on.
type Manifest struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Backend  string   `json:"backend"` // pip | npm | bundled | fake
	Entry    string   `json:"entry"`
	Skill    string   `json:"skill"`
	Requires []string `json:"requires,omitempty"`

	// URL is only consulted by the bundled backend. We keep it optional so a
	// pip/npm manifest can omit it without tripping the unmarshaller.
	URL string `json:"url,omitempty"`
}

// LoadManifest reads a manifest from disk and returns it. Lightweight wrapper
// — kept here so main.go doesn't need to know about os/json directly.
func LoadManifest(path string) (Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("parse manifest %s: %w", path, err)
	}
	if m.Name == "" {
		return Manifest{}, fmt.Errorf("manifest %s: name is required", path)
	}
	if m.Backend == "" {
		return Manifest{}, fmt.Errorf("manifest %s: backend is required", path)
	}
	return m, nil
}
