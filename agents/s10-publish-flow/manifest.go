// manifest.go re-declares the canonical Manifest shape from the plan, plus
// a small SkillMeta sniffer the publisher uses during scan/validate.
//
// The publisher reads two pieces from each plugin subdirectory:
//
//  1. SKILL.md — required. The file's existence is the validation gate;
//     the YAML front-matter's `name:` and (optional) `version:` populate
//     the Manifest. If front-matter is missing we fall back to the
//     directory name and "0.0.0".
//  2. backend (optional) — a one-line file named `backend.txt` containing
//     "pip", "npm", "bundled", or "uv". Missing → "bundled".
//
// We keep this format deliberately minimal: the upstream registry.json
// has a richer schema (display_name, install_cmd, source_url, etc.) but
// for the curriculum a 4-field Manifest is enough to demonstrate the
// scan -> validate -> bundle -> sign -> index pipeline shape.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Manifest is the per-CLI registry entry. The JSON tags match the plan's
// canonical signature so the emitted registry.json is shape-compatible
// with what s06 would consume.
type Manifest struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Backend  string   `json:"backend"` // pip | npm | bundled | uv
	Entry    string   `json:"entry"`
	Skill    string   `json:"skill"` // path to SKILL.md (relative to src root)
	Requires []string `json:"requires,omitempty"`
}

// SkillMeta is the subset of SKILL.md front-matter the publisher reads.
// Re-declared minimal cousin of s02's parser — s10 only needs name and
// version, so we avoid pulling gopkg.in/yaml.v3 and keep go.mod zero-dep.
type SkillMeta struct {
	Name    string
	Version string
}

// readSkillFront opens path and returns the first YAML-front-matter block
// parsed into a SkillMeta. Lines that aren't "key: value" are ignored.
// A SKILL.md without front-matter returns an empty SkillMeta and nil err —
// the publisher fills the holes from the directory name.
func readSkillFront(path string) (SkillMeta, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return SkillMeta{}, err
	}
	text := string(b)
	if !strings.HasPrefix(text, "---\n") {
		return SkillMeta{}, nil
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		// also tolerate "---\n" at exact EOF without trailing newline
		end = strings.Index(rest, "\n---")
		if end < 0 {
			return SkillMeta{}, errors.New("unterminated front-matter")
		}
	}
	front := rest[:end]

	var meta SkillMeta
	for _, line := range strings.Split(front, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.Index(line, ":")
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		v = strings.Trim(v, "\"'")
		switch k {
		case "name":
			meta.Name = v
		case "version":
			meta.Version = v
		}
	}
	return meta, nil
}

// readBackendHint returns the backend declared in <dir>/backend.txt, or
// "bundled" if the file is missing. Whitespace-only files also fall
// through to "bundled" — we never invent a backend the publisher would
// not be able to install for.
func readBackendHint(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "backend.txt"))
	if err != nil {
		return "bundled"
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "bundled"
	}
	return s
}
