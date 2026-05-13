// cli.go — re-declared CLI/Flag/Result types so this session is a stand-alone
// Go module. The verify harness itself only exposes one user-facing command
// (`verify <plugin-dir>`), so we keep the surface tiny: a flat Dispatch that
// reuses the same JSON envelope every other chapter prints.
//
// Why this lives next to the verifier: the verifier IS a harness. It eats a
// plugin directory and emits either a human report or a Result envelope, so
// that an outer agent (or a CI step) can branch on .ok.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// CLI is the same recursive node every session uses. We don't need
// subcommands for s08 (verify is the only verb), but keeping the shape
// makes the file pattern-match s01..s05 at a glance.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag is the data-only flag declaration.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope. `Verify` returns a *Report which gets put
// into Data; CI scripts can grep `"ok":false` to fail the build.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch is a slim version of s01's: one root, walk subcommands, run the
// leaf. Widened to io.Writer so the tests can drive it from bytes.Buffer.
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
		fmt.Fprintf(out, "%s — %s\n", cur.Name, cur.Help)
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
