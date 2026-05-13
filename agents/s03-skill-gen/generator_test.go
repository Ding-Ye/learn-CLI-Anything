package main

import (
	"context"
	"strings"
	"testing"
)

// helper: a tiny one-subcommand CLI used by several tests
func oneSubCLI() *CLI {
	return &CLI{
		Name: "tool",
		Help: "Tool harness. A tiny example.",
		Subcommands: map[string]*CLI{
			"build": {
				Name: "build",
				Help: "Build the project",
				Flags: []Flag{
					{Name: "out", Type: "string", Default: "out/", Help: "output dir"},
					{Name: "verbose", Type: "bool", Default: false, Help: "log every step"},
				},
				Run: func(ctx context.Context, args []string) (any, error) { return nil, nil },
			},
		},
	}
}

// 1. End-to-end: GenerateSkill -> RenderSkill -> ParseSkill round-trip
// produces the same meta we synthesized and a non-empty body.
func TestGenerateSkill_RoundTripsThroughParse(t *testing.T) {
	cli := oneSubCLI()
	s := GenerateSkill(cli)
	rendered, err := RenderSkill(s)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.HasPrefix(string(rendered), "---\n") {
		t.Fatalf("rendered output missing frontmatter delimiter:\n%s", rendered)
	}
	parsed, err := ParseSkill(rendered)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if parsed.Meta.Name != "tool" {
		t.Fatalf("meta.Name=%q want %q", parsed.Meta.Name, "tool")
	}
	if parsed.Meta.Description != "Tool harness." {
		t.Fatalf("meta.Description=%q want first-sentence trim", parsed.Meta.Description)
	}
	if !strings.Contains(parsed.Body, "# tool") {
		t.Fatalf("body missing H1; got:\n%s", parsed.Body)
	}
}

// 2. Triggers include subcommand names AND their "<sub> <root>" form.
func TestGenerateSkill_TriggersIncludeSubcommands(t *testing.T) {
	cli := oneSubCLI()
	// add a second sub to ensure deduping + multiple entries
	cli.Subcommands["test"] = &CLI{Name: "test", Help: "Run tests"}
	s := GenerateSkill(cli)
	want := []string{"build", "build tool", "test", "test tool"}
	got := strings.Join(s.Meta.Triggers, ",")
	for _, w := range want {
		if !contains(s.Meta.Triggers, w) {
			t.Fatalf("triggers missing %q; got %s", w, got)
		}
	}
}

// 3. Flags table contains every declared flag.
func TestGenerateSkill_FlagsTableHasAllFlags(t *testing.T) {
	cli := oneSubCLI()
	s := GenerateSkill(cli)
	body := s.Body
	for _, want := range []string{"`--out`", "`--verbose`", "`string`", "`bool`"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q; body:\n%s", want, body)
		}
	}
	// Required flag should be marked "yes"
	cli2 := &CLI{
		Name: "tool",
		Help: "Tool.",
		Subcommands: map[string]*CLI{
			"x": {Name: "x", Help: "x", Flags: []Flag{{Name: "in", Type: "string", Required: true, Help: "input"}}},
		},
	}
	s2 := GenerateSkill(cli2)
	if !strings.Contains(s2.Body, "| yes |") {
		t.Fatalf("required flag not marked yes; body:\n%s", s2.Body)
	}
}

// 4. Empty CLI (zero subcommands, zero flags) still renders cleanly.
func TestGenerateSkill_EmptyCLI(t *testing.T) {
	cli := &CLI{Name: "void", Help: "Nothing here."}
	s := GenerateSkill(cli)
	if s.Meta.Name != "void" {
		t.Fatalf("meta.Name=%q", s.Meta.Name)
	}
	if len(s.Meta.Triggers) != 0 {
		t.Fatalf("expected no triggers for empty CLI; got %v", s.Meta.Triggers)
	}
	rendered, err := RenderSkill(s)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// Should NOT have a Subcommands or Usage section.
	body := string(rendered)
	if strings.Contains(body, "## Subcommands") {
		t.Fatalf("unexpected Subcommands section in empty CLI:\n%s", body)
	}
	if strings.Contains(body, "## Usage") {
		t.Fatalf("unexpected Usage section in empty CLI:\n%s", body)
	}
}

// 5. Determinism — generating twice from a CLI whose Subcommands map
// has insertion-order ambiguity must produce byte-identical output.
// We build the input fresh each iteration to force map randomization.
func TestGenerateSkill_DeterministicOutput(t *testing.T) {
	mk := func() *CLI {
		return &CLI{
			Name: "tool",
			Help: "Tool.",
			Subcommands: map[string]*CLI{
				"zebra": {Name: "zebra", Help: "z"},
				"alpha": {Name: "alpha", Help: "a"},
				"mike":  {Name: "mike", Help: "m"},
			},
		}
	}
	var prev []byte
	for i := 0; i < 5; i++ {
		out, err := RenderSkill(GenerateSkill(mk()))
		if err != nil {
			t.Fatalf("render: %v", err)
		}
		if i > 0 && string(out) != string(prev) {
			t.Fatalf("non-deterministic output across runs:\n--- prev ---\n%s\n--- now ---\n%s", prev, out)
		}
		prev = out
	}
	// Bonus: subcommand order in the table must be alphabetical
	body := string(prev)
	i := strings.Index(body, "## Subcommands")
	if i < 0 {
		t.Fatalf("no subcommands section")
	}
	tail := body[i:]
	ialpha := strings.Index(tail, "alpha")
	imike := strings.Index(tail, "mike")
	izebra := strings.Index(tail, "zebra")
	if !(ialpha < imike && imike < izebra) {
		t.Fatalf("subcommands not alphabetical: alpha@%d mike@%d zebra@%d", ialpha, imike, izebra)
	}
}

func contains(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}
