// skill.go — re-declared SKILL.md model from s02.
//
// The wire format is unchanged across sessions: YAML frontmatter delimited
// by "---\n" lines, followed by the markdown body verbatim. This file
// re-implements Parse/Render so s03 stays self-contained (no cross-module
// import) — and so the generator's output can be round-tripped through
// the same Parse a downstream tool would use.
package main

import (
	"bytes"
	"errors"
	"strings"

	"gopkg.in/yaml.v3"
)

// SkillMeta is the YAML frontmatter.
type SkillMeta struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Triggers    []string `yaml:"triggers,omitempty"`
}

// Skill = frontmatter + raw markdown body.
type Skill struct {
	Meta SkillMeta
	Body string
}

const (
	skillDelim = "---"
	skillNL    = "\n"
)

// ParseSkill reads a SKILL.md byte stream and returns the parsed Skill.
// We are deliberately lenient: a missing frontmatter delimiter at the top
// is treated as a body-only document (Meta zero value, Body = input).
func ParseSkill(data []byte) (Skill, error) {
	s := string(data)
	// Normalize CRLF so we don't break on Windows-checked-out files.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	if !strings.HasPrefix(s, skillDelim+skillNL) {
		return Skill{Body: s}, nil
	}
	rest := s[len(skillDelim)+1:]
	end := strings.Index(rest, skillNL+skillDelim+skillNL)
	if end < 0 {
		// also accept a trailing "---" with no newline
		if strings.HasSuffix(rest, skillNL+skillDelim) {
			end = len(rest) - len(skillDelim) - 1
		} else {
			return Skill{}, errors.New("skill: unterminated frontmatter")
		}
	}
	frontmatter := rest[:end]
	body := ""
	bodyStart := end + len(skillNL+skillDelim+skillNL)
	if bodyStart <= len(rest) {
		body = rest[bodyStart:]
	}
	var meta SkillMeta
	if err := yaml.Unmarshal([]byte(frontmatter), &meta); err != nil {
		return Skill{}, err
	}
	return Skill{Meta: meta, Body: body}, nil
}

// RenderSkill emits the canonical text form of a Skill. yaml.v3 writes
// keys in struct-field order with a trailing newline, which gives us a
// stable round-trip target.
func RenderSkill(s Skill) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteString(skillDelim)
	buf.WriteString(skillNL)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(s.Meta); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	buf.WriteString(skillDelim)
	buf.WriteString(skillNL)
	buf.WriteString(s.Body)
	return buf.Bytes(), nil
}
