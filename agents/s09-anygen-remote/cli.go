// Package main re-declares the minimal s01 CLI surface so s09 stays a
// stand-alone Go module (no cross-session imports). The shapes match the
// curriculum's canonical signatures from plan.md, trimmed to what a
// remote-API harness actually exercises.
//
// The point of s09 is to show a CLI-Anything harness that does NOT wrap a
// local GUI. AnyGen lives on a server — the harness just speaks HTTP and
// polls. cli.go is identical in spirit to s01/s05; the interesting parts
// are in client.go (HTTP wrapping) and poller.go (the async loop GUI
// harnesses don't have).
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// CLI is one node of the command tree. Leaves set Run; branches set
// Subcommands. The s09 demo wires four leaves (submit/status/wait/result)
// under a single "anygen" root.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag describes one option on a subcommand. Data-only — same as s01.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope an agent sees. OK is always present so
// callers branch without parsing the error string.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch walks the command tree from root and invokes the matching leaf.
// jsonMode toggles between human pretty-print and a Result envelope. The
// out/errOut writers are io.Writer (not *os.File) so the demo's
// httptest-backed integration can drive Dispatch from a bytes.Buffer.
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
