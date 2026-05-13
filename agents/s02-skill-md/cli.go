// cli.go — re-declared from s01. Each session is a self-contained Go
// module (no cross-imports), so the CLI/Flag/Result/Dispatch trio
// reappears here in nearly identical form. The point: every chapter
// reads as a standalone snippet.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// CLI is the recursive command-tree node — same shape as s01.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag is the per-subcommand flag declaration.
type Flag struct {
	Name     string
	Type     string
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

// Dispatch walks the command tree and runs the matching leaf.
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
	case []byte:
		return string(x)
	case nil:
		return ""
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

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
