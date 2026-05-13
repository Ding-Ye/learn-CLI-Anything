// Package main re-declares the curriculum's shared CLI shapes so this module
// stays self-contained — no cross-session imports. The surface mirrors s01/s05
// just closely enough to host the installer subcommands; the dispatcher logic
// itself lives in installer.go.
//
// We keep the writer types as io.Writer (same widening trick as s05) so tests
// can drive Dispatch with a bytes.Buffer instead of os.Stdout.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// CLI is one node of the command tree. Leaves set Run; branches set Subcommands.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag describes a single option on a subcommand. Data-only by design — the
// dispatcher and any future skill generator read it via plain struct access.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope an agent (or --json caller) sees. OK is always
// present so the caller can branch without parsing the error string.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch walks the command tree from root and invokes the matching leaf.
// jsonMode toggles between human pretty-print and a Result envelope.
//
// We split out the writers (instead of holding them on a singleton) so the
// same code path works for os.Stdout in production and bytes.Buffer in tests.
func Dispatch(ctx context.Context, root *CLI, argv []string, jsonMode bool, out, errOut io.Writer) int {
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
	data, err := cur.Run(ctx, argv[i:])
	if jsonMode {
		env := Result{OK: err == nil, Data: data}
		if err != nil {
			env.Error = err.Error()
		}
		_ = json.NewEncoder(out).Encode(env)
		if err != nil {
			return 1
		}
		return 0
	}
	if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	}
	if data != nil {
		fmt.Fprintln(out, prettyPrint(data))
	}
	return 0
}

func printHelp(c *CLI, out io.Writer, jsonMode bool) {
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
			fmt.Fprintf(out, "  %-12s %s\n", name, c.Subcommands[name].Help)
		}
	}
}

func subcommandNames(c *CLI) []string {
	out := make([]string, 0, len(c.Subcommands))
	for k := range c.Subcommands {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
