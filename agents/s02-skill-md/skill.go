// Package main: SKILL.md is the front door an agent sees for any
// CLI-Anything harness. Upstream `cli-anything-plugin/skill_generator.py`
// renders it; agents (Claude Code, Pi, Codex…) parse it. To play in either
// role we need a round-trippable parser+renderer that doesn't drop bytes
// the agent or the generator cares about.
//
// Format (see also `anygen/agent-harness/cli_anything/anygen/skills/SKILL.md`):
//
//	---
//	name: cli-anything-anygen
//	description: >-
//	  Command-line interface for Anygen — ...
//	triggers:
//	  - anygen
//	  - slides
//	---
//
//	# cli-anything-anygen
//
//	...markdown body...
//
// The contract that matters: anything in the frontmatter is structured
// (the agent's discovery layer reads it); everything below the closing
// `---` is opaque markdown the renderer must preserve byte-for-byte so
// `Parse` ∘ `Render` = identity for any file we emit.
package main

import (
	"bytes"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// SkillMeta mirrors the YAML frontmatter every SKILL.md begins with.
// `triggers` is the optional list of activation keywords; upstream uses it
// to let agents fuzzy-match user intent against installed skills.
type SkillMeta struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description"`
	Triggers    []string `yaml:"triggers,omitempty" json:"triggers,omitempty"`
}

// Skill is the parsed pair (frontmatter, body). `raw` stores the exact
// frontmatter bytes the parser saw, so Render can put them back verbatim
// instead of letting yaml.v3 re-emit and break round-tripping (e.g.
// folded-scalar `>-` becoming a plain string).
type Skill struct {
	Meta SkillMeta
	Body []byte

	// raw is the literal frontmatter bytes between the two `---` lines,
	// including the trailing newline. Empty when constructed in-memory.
	raw []byte
}

// delim is the YAML front-matter fence. CLI-Anything always uses three
// hyphens followed by a newline — never `...` and never indented.
var delim = []byte("---\n")

// errMissingDelim and errMissingName are the two failure modes worth
// surfacing as typed errors. Everything else collapses to fmt.Errorf.
var (
	errMissingDelim = errors.New("skill: missing front-matter delimiter '---'")
	errMissingName  = errors.New("skill: front-matter is missing required field 'name'")
)

// Parse decodes a SKILL.md byte stream. It is strict about the delimiter
// (must be `---\n` at offset 0 and again later) but permissive about what
// the body contains — including a trailing newline or lack thereof.
//
// The function does TWO things that matter for round-tripping:
//
//  1. It snapshots the literal frontmatter bytes into Skill.raw so Render
//     can emit them unchanged. This is the only way to guarantee byte
//     equality, since yaml.v3 normalizes block-scalar styles.
//  2. It validates that `name` is present, because every downstream
//     consumer (skill_generator, repl_skin, the agent itself) keys off
//     it; an empty `name` is the #1 cause of "skill not discovered".
func Parse(data []byte) (Skill, error) {
	if !bytes.HasPrefix(data, delim) {
		return Skill{}, errMissingDelim
	}
	rest := data[len(delim):]
	end := bytes.Index(rest, delim)
	if end < 0 {
		return Skill{}, errMissingDelim
	}
	frontRaw := rest[:end]
	body := rest[end+len(delim):]

	var meta SkillMeta
	if err := yaml.Unmarshal(frontRaw, &meta); err != nil {
		return Skill{}, fmt.Errorf("skill: yaml: %w", err)
	}
	if meta.Name == "" {
		return Skill{}, errMissingName
	}
	return Skill{
		Meta: meta,
		Body: body,
		raw:  frontRaw,
	}, nil
}

// Render emits a SKILL.md byte stream. If `raw` is populated (i.e. the
// Skill came from Parse) we reuse it so the round-trip is exact.
// Otherwise we serialize Meta via yaml.v3, which is fine for freshly
// constructed skills (e.g. those s03 will produce).
func Render(s Skill) []byte {
	var buf bytes.Buffer
	buf.Write(delim)
	if len(s.raw) > 0 {
		buf.Write(s.raw)
	} else {
		// yaml.v3 emits a trailing newline already.
		out, _ := yaml.Marshal(s.Meta)
		buf.Write(out)
	}
	buf.Write(delim)
	buf.Write(s.Body)
	return buf.Bytes()
}
