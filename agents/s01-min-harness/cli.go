// Package main implements the minimum "harness" satisfying CLI-Anything's
// HARNESS contract: a CLI that exposes its capabilities through subcommands
// and can emit machine-readable JSON output when an agent needs it.
//
// HARNESS contract (upstream cli-anything-plugin/HARNESS.md):
//   - Every subcommand MUST have a help string.
//   - --json flag toggles between human-pretty output and structured JSON.
//   - Exit code is 0 on success, non-zero on failure; JSON failures still
//     emit a valid JSON envelope with an "error" field.
//   - Output to stdout, errors to stderr (machine readers tail stdout only).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// CLI is the recursive command-tree node. Subcommands compose via the
// Subcommands map. Each leaf (Run != nil) does the actual work.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag is the per-subcommand flag declaration. We expose it as data
// (rather than the cobra/click decorator pattern) so the SKILL.md
// generator in s03 can introspect it without runtime tricks.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope an agent sees.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch walks the command tree from root and invokes the matching leaf.
// `jsonMode` switches the printer between human text and a Result envelope.
func Dispatch(ctx context.Context, root *CLI, argv []string, jsonMode bool, out, errOut *os.File) int {
	cur := root
	i := 0
	for i < len(argv) {
		next, ok := cur.Subcommands[argv[i]]
		if !ok {
			break
		}
		cur = next
		i++
	}
	if cur.Run == nil {
		printHelp(cur, out, jsonMode)
		return 0
	}
	out1, err := cur.Run(ctx, argv[i:])
	if jsonMode {
		env := Result{OK: err == nil, Data: out1}
		if err != nil {
			env.Error = err.Error()
		}
		_ = json.NewEncoder(out).Encode(env)
	} else if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	} else {
		fmt.Fprintln(out, prettyPrint(out1))
	}
	if err != nil {
		return 1
	}
	return 0
}

func printHelp(c *CLI, out *os.File, jsonMode bool) {
	if jsonMode {
		_ = json.NewEncoder(out).Encode(Result{OK: true, Data: map[string]any{
			"name":        c.Name,
			"help":        c.Help,
			"subcommands": subcommandNames(c),
			"flags":       c.Flags,
		}})
		return
	}
	fmt.Fprintf(out, "%s — %s\n", c.Name, c.Help)
	if len(c.Subcommands) > 0 {
		fmt.Fprintln(out, "subcommands:")
		for _, name := range subcommandNames(c) {
			fmt.Fprintf(out, "  %s  %s\n", name, c.Subcommands[name].Help)
		}
	}
}

func subcommandNames(c *CLI) []string {
	out := make([]string, 0, len(c.Subcommands))
	for k := range c.Subcommands {
		out = append(out, k)
	}
	// stable order helps both human reading and the s03 generator
	sortStrings(out)
	return out
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

func prettyPrint(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case nil:
		return ""
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

// hasJSONFlag is a tiny argv preprocessor: it strips --json and reports it.
func hasJSONFlag(argv []string) ([]string, bool) {
	out := make([]string, 0, len(argv))
	jsonMode := false
	for _, a := range argv {
		if a == "--json" {
			jsonMode = true
			continue
		}
		out = append(out, a)
	}
	return out, jsonMode
}

var errNotImplemented = errors.New("not implemented")
