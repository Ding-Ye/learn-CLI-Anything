// generator.go — synthesize a SKILL.md from a CLI tree.
//
// The upstream Python version (`cli-anything-plugin/skill_generator.py`)
// is essentially a regex scraper over Click decorators: it reads
// `<software>_cli.py`, pattern-matches `@xxx.group(...)` / `@xxx.command(...)`
// blocks, and pulls docstrings to fill a SKILL template. That works
// because Python is duck-typed and metadata lives in source text.
//
// In Go we made a different bet in s01: the harness author writes a
// struct literal, which makes the metadata first-class. GenerateSkill
// is then a pure tree walk — no regex, no AST parsing, no reflection.
// That swap is the whole reason the s01 CLI struct exposes Flags as
// data instead of using a cobra-style closure registry.
package main

import (
	"fmt"
	"sort"
	"strings"
)

// GenerateSkill produces a Skill from a CLI tree. The result is always
// well-formed (RenderSkill never errors on its output) and stable
// (subcommands are sorted by name so two runs of the same input emit
// byte-identical SKILL.md).
func GenerateSkill(cli *CLI) Skill {
	meta := SkillMeta{
		Name:        cli.Name,
		Description: firstSentence(cli.Help),
		Triggers:    deriveTriggers(cli),
	}
	body := synthesizeBody(cli)
	return Skill{Meta: meta, Body: body}
}

// firstSentence pulls the first sentence (up to "." "!" or "?", or
// the first newline) of the help string. Mirrors the upstream
// `extract_intro_from_readme` heuristic: the agent that reads SKILL.md
// only needs a one-liner for triggering, the body has the detail.
func firstSentence(help string) string {
	help = strings.TrimSpace(help)
	if help == "" {
		return ""
	}
	// Cut at first newline first — multi-paragraph help is unusual but
	// when it happens we want only the lead paragraph.
	if i := strings.IndexByte(help, '\n'); i >= 0 {
		help = strings.TrimSpace(help[:i])
	}
	for i, r := range help {
		if r == '.' || r == '!' || r == '?' {
			return strings.TrimSpace(help[:i+1])
		}
	}
	return help
}

// deriveTriggers builds a stable list of natural-language trigger
// phrases an agent's SKILL matcher can grep. For each subcommand we
// emit "<verb> <root>" plus the bare subcommand name. Sorted +
// deduped for determinism.
func deriveTriggers(cli *CLI) []string {
	if cli == nil || len(cli.Subcommands) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(cli.Subcommands)*2)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, name := range sortedKeys(cli.Subcommands) {
		add(name)
		add(fmt.Sprintf("%s %s", name, cli.Name))
	}
	sort.Strings(out)
	return out
}

// synthesizeBody composes the SKILL.md markdown body: a short synopsis,
// a subcommands table (if any), a flags table (root flags), and a
// "Usage" block with one example per subcommand. Sections are skipped
// when empty, which matches what the upstream generator does for
// harnesses with no command groups.
func synthesizeBody(cli *CLI) string {
	if cli == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", cli.Name)
	if h := strings.TrimSpace(cli.Help); h != "" {
		fmt.Fprintf(&b, "%s\n\n", h)
	}
	fmt.Fprintf(&b, "## Synopsis\n\n")
	fmt.Fprintf(&b, "```text\n%s%s\n```\n\n", cli.Name, synopsisTail(cli))

	if subs := sortedKeys(cli.Subcommands); len(subs) > 0 {
		fmt.Fprintf(&b, "## Subcommands\n\n")
		fmt.Fprintf(&b, "| Command | Description |\n")
		fmt.Fprintf(&b, "| --- | --- |\n")
		for _, name := range subs {
			sub := cli.Subcommands[name]
			fmt.Fprintf(&b, "| `%s` | %s |\n", name, escapePipe(firstSentence(sub.Help)))
		}
		b.WriteString("\n")
	}

	if len(cli.Flags) > 0 {
		writeFlagsTable(&b, "Flags", cli.Flags)
	}

	// Per-subcommand flag tables + usage examples.
	if subs := sortedKeys(cli.Subcommands); len(subs) > 0 {
		fmt.Fprintf(&b, "## Usage\n\n")
		for _, name := range subs {
			sub := cli.Subcommands[name]
			fmt.Fprintf(&b, "### `%s %s`\n\n", cli.Name, name)
			if h := strings.TrimSpace(sub.Help); h != "" {
				fmt.Fprintf(&b, "%s\n\n", h)
			}
			if len(sub.Flags) > 0 {
				writeFlagsTable(&b, fmt.Sprintf("`%s` flags", name), sub.Flags)
			}
			fmt.Fprintf(&b, "```bash\n%s %s%s\n```\n\n", cli.Name, name, sampleArgs(sub))
		}
	}

	return b.String()
}

// writeFlagsTable renders a Markdown table for a flag list. We surface
// every declared field — Name, Type, Default, Required — because the
// agent's planner uses Required to know which arguments it cannot
// omit, and Type to coerce its own scratch variables.
func writeFlagsTable(b *strings.Builder, heading string, flags []Flag) {
	fmt.Fprintf(b, "## %s\n\n", heading)
	fmt.Fprintf(b, "| Flag | Type | Default | Required | Help |\n")
	fmt.Fprintf(b, "| --- | --- | --- | --- | --- |\n")
	sorted := make([]Flag, len(flags))
	copy(sorted, flags)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	for _, f := range sorted {
		def := "—"
		if f.Default != nil {
			def = fmt.Sprintf("`%v`", f.Default)
		}
		req := "no"
		if f.Required {
			req = "yes"
		}
		fmt.Fprintf(b, "| `--%s` | `%s` | %s | %s | %s |\n",
			f.Name, f.Type, def, req, escapePipe(f.Help))
	}
	b.WriteString("\n")
}

// synopsisTail prints the "[subcommand]" / "[flags]" hint after the
// program name in the synopsis block.
func synopsisTail(cli *CLI) string {
	var parts []string
	if len(cli.Subcommands) > 0 {
		parts = append(parts, " <subcommand>")
	}
	if len(cli.Flags) > 0 {
		parts = append(parts, " [flags]")
	}
	return strings.Join(parts, "")
}

// sampleArgs emits a plausible argv tail for a subcommand: required
// flags are shown explicitly with their type as the placeholder,
// optional ones are elided. This is what an agent would copy-paste.
func sampleArgs(sub *CLI) string {
	var b strings.Builder
	for _, f := range sub.Flags {
		if !f.Required {
			continue
		}
		fmt.Fprintf(&b, " --%s <%s>", f.Name, f.Type)
	}
	return b.String()
}

func sortedKeys(m map[string]*CLI) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// escapePipe keeps Markdown table cells from breaking when the help
// string itself contains a "|" character.
func escapePipe(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
