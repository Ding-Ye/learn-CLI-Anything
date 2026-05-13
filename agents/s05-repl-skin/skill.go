// skill.go re-declares the minimum subset of s02's SKILL.md parser the REPL
// needs: the YAML-front-matter SkillMeta struct plus a small Parse that pulls
// out `name:` and `description:` lines. We don't pull in gopkg.in/yaml.v3
// here on purpose — the REPL only needs to render two fields in the banner,
// and the s05 module must stay zero-dep.
//
// The real parser lives in s02; this is a deliberate minimal cousin.
package main

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// SkillMeta is the YAML front-matter of a SKILL.md file. The full set in s02
// also has Triggers []string; we drop it here because the REPL banner doesn't
// surface triggers. Re-declaring the subset keeps the parser tiny.
type SkillMeta struct {
	Name        string
	Description string
}

// Skill is a parsed SKILL.md: meta + raw markdown body.
type Skill struct {
	Meta SkillMeta
	Body string
}

// ParseSkill reads SKILL.md from r. It expects (and tolerates) the YAML
// front-matter shape:
//
//	---
//	name: cli-anything-demo
//	description: A short one-liner.
//	---
//
//	# Body markdown...
//
// We hand-roll the parse instead of pulling in a YAML lib because the REPL
// only ever reads two scalar fields. If a body has no front-matter we return
// an empty Meta and treat everything as Body — handy for ad-hoc SKILL files.
func ParseSkill(r io.Reader) (*Skill, error) {
	sc := bufio.NewScanner(r)
	// 1 MiB max line — SKILL bodies can have long URL lines.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)

	var (
		inFront bool
		seenEnd bool
		meta    SkillMeta
		body    strings.Builder
	)
	firstLine := true
	for sc.Scan() {
		line := sc.Text()
		if firstLine {
			firstLine = false
			if strings.TrimSpace(line) == "---" {
				inFront = true
				continue
			}
			// no front-matter — first line is body
			body.WriteString(line)
			body.WriteByte('\n')
			continue
		}
		if inFront && !seenEnd {
			if strings.TrimSpace(line) == "---" {
				seenEnd = true
				continue
			}
			// only name: and description: are honored
			if k, v, ok := splitFrontLine(line); ok {
				switch k {
				case "name":
					meta.Name = v
				case "description":
					meta.Description = v
				}
			}
			continue
		}
		body.WriteString(line)
		body.WriteByte('\n')
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if inFront && !seenEnd {
		return nil, errors.New("unterminated front-matter")
	}
	return &Skill{Meta: meta, Body: body.String()}, nil
}

// splitFrontLine returns (key, value, true) for a "key: value" YAML scalar.
// It strips surrounding quotes from value. Lines without ":" or starting
// with "#" are treated as not-a-key.
func splitFrontLine(line string) (string, string, bool) {
	s := strings.TrimSpace(line)
	if s == "" || strings.HasPrefix(s, "#") {
		return "", "", false
	}
	idx := strings.Index(s, ":")
	if idx <= 0 {
		return "", "", false
	}
	k := strings.TrimSpace(s[:idx])
	v := strings.TrimSpace(s[idx+1:])
	v = strings.Trim(v, "\"'")
	return k, v, true
}
