// Package main re-declares the s01 CLI types so this session stands on its
// own (no cross-session imports). The shapes match s01's contract:
//
//   - CLI is a recursive command-tree node (Name, Help, Flags, Subcommands, Run).
//   - Flag is data-only.
//   - Result is the JSON envelope the harness prints when --json is set.
//
// Dispatch keeps s01's *os.File signature here because s06 is back to a
// one-shot CLI; the REPL widening from s05 isn't needed.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// CLI is one node of the command tree. Leaves set Run; branches set Subcommands.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag describes one option. Data-only so a skill_generator (curriculum's
// s03) can introspect with plain struct access — no decorator magic.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope an agent sees in --json mode. OK is always
// present so callers can branch without parsing the error string.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch walks the command tree from root and invokes the matching leaf.
// jsonMode toggles between human pretty-print and a Result envelope.
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
	fmt.Fprintln(out, prettyPrint(data))
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
			fmt.Fprintf(out, "  %s  %s\n", name, c.Subcommands[name].Help)
		}
	}
}

func subcommandNames(c *CLI) []string {
	out := make([]string, 0, len(c.Subcommands))
	for k := range c.Subcommands {
		out = append(out, k)
	}
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
