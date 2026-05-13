// skill.go — the minimum SKILL.md parser the verifier needs. We re-declare
// (rather than import) s02's parser so this module stays self-contained.
//
// Difference from s02: we don't require `name` at the parser level. The
// verifier *itself* reports missing-name as S002 with a structured Issue;
// crashing on it in the parser would short-circuit every other check and
// hide downstream problems like "description missing AND no harness".
//
// We do keep yaml.v3 here (unlike s05) because the verifier needs to be
// strict about whether `triggers` is a YAML list — that's the kind of bug
// hand-rolled scalar splitting can't catch.
package main

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SkillMeta mirrors the YAML front-matter of a SKILL.md file.
// Triggers is `[]string` so YAML *scalars* in the field (e.g. `triggers: foo`)
// fail to unmarshal — exactly the bug check_skill_md_triggers wants to surface.
type SkillMeta struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers,omitempty"`
}

// Skill is a parsed SKILL.md.
type Skill struct {
	Meta SkillMeta
	Body []byte
}

var (
	frontDelim       = []byte("---\n")
	errNoFrontMatter = errors.New("skill: missing front-matter delimiter '---'")
)

// ParseSkill reads bytes of a SKILL.md and returns the parsed Skill. Unlike
// s02 it does NOT enforce required fields — the verifier handles that and
// emits structured Issues so a single SKILL.md can collect multiple errors
// in one pass.
func ParseSkill(data []byte) (*Skill, error) {
	if !bytes.HasPrefix(data, frontDelim) {
		return nil, errNoFrontMatter
	}
	rest := data[len(frontDelim):]
	end := bytes.Index(rest, frontDelim)
	if end < 0 {
		return nil, errNoFrontMatter
	}
	frontRaw := rest[:end]
	body := rest[end+len(frontDelim):]
	var meta SkillMeta
	if err := yaml.Unmarshal(frontRaw, &meta); err != nil {
		return nil, fmt.Errorf("skill: yaml: %w", err)
	}
	return &Skill{Meta: meta, Body: body}, nil
}

// frontMatterBytes returns just the YAML front-matter bytes from data, with
// a flag telling the caller whether the delimiters were even there. The
// triggers-shape check needs raw YAML (so it can re-unmarshal into a probe
// type) without re-running the whole ParseSkill pipeline.
func frontMatterBytes(data []byte) ([]byte, bool) {
	if !bytes.HasPrefix(data, frontDelim) {
		return nil, false
	}
	rest := data[len(frontDelim):]
	end := bytes.Index(rest, frontDelim)
	if end < 0 {
		return nil, false
	}
	return rest[:end], true
}
