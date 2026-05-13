package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// skillMD is the CLI surface for s02. Two subcommands mirror the
// parser/renderer pair:
//
//	skill-md parse  <file.md>    →  JSON dump of {meta, body}
//	skill-md render <file.json>  →  emit a SKILL.md to stdout
//
// `render` takes JSON because we want a CLI-friendly inverse — a human
// (or a test in CI) hands us structured data, we print markdown.
func skillMD() *CLI {
	return &CLI{
		Name: "skill-md",
		Help: "SKILL.md parser & renderer",
		Subcommands: map[string]*CLI{
			"parse": {
				Name: "parse",
				Help: "Parse a SKILL.md file and print its structured form",
				Flags: []Flag{
					{Name: "<file>", Type: "string", Required: true, Help: "path to SKILL.md"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 1 {
						return nil, errors.New("usage: skill-md parse <file>")
					}
					data, err := os.ReadFile(args[0])
					if err != nil {
						return nil, fmt.Errorf("read: %w", err)
					}
					s, err := Parse(data)
					if err != nil {
						return nil, err
					}
					return map[string]any{
						"meta": s.Meta,
						"body": string(s.Body),
					}, nil
				},
			},
			"render": {
				Name: "render",
				Help: "Render a SKILL.md from a JSON {meta, body} document",
				Flags: []Flag{
					{Name: "<file>", Type: "string", Required: true, Help: "path to JSON input"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 1 {
						return nil, errors.New("usage: skill-md render <file.json>")
					}
					data, err := os.ReadFile(args[0])
					if err != nil {
						return nil, fmt.Errorf("read: %w", err)
					}
					var doc struct {
						Meta SkillMeta `json:"meta"`
						Body string    `json:"body"`
					}
					if err := json.Unmarshal(data, &doc); err != nil {
						return nil, fmt.Errorf("json: %w", err)
					}
					if doc.Meta.Name == "" {
						return nil, errMissingName
					}
					out := Render(Skill{Meta: doc.Meta, Body: []byte(doc.Body)})
					return string(out), nil
				},
			},
		},
	}
}

func main() {
	argv, jsonMode := hasJSONFlag(os.Args[1:])
	code := Dispatch(context.Background(), skillMD(), argv, jsonMode, os.Stdout, os.Stderr)
	os.Exit(code)
}
