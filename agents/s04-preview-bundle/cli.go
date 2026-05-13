// Package main is the s04 chapter: a preview-bundle cache. This file holds
// the CLI dispatch surface re-declared from s01 (every chapter is its own
// Go module; no cross-imports allowed).
//
// What's new vs. s01:
//   - The `Run` handlers in s04 don't actually do the work themselves; they
//     hand the (cmd, inputs) pair to the bundle layer in bundle.go +
//     exec.go, which decides whether to execute or replay from cache.
//   - We keep the same Result envelope so an agent that already knows how
//     to read s01's JSON can read s04's too — the cache is transparent.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// CLI re-declared from s01. Same shape on purpose: the s03 skill generator
// would happily introspect this struct, and an agent that learned the s01
// HARNESS contract doesn't have to re-learn anything here.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag — same as s01. Data-first metadata, not decorator-bound.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result envelope — see s01 for the rationale. Repeated here so an agent
// only has to learn one shape across the whole curriculum.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// Dispatch walks the subcommand tree, then prints either pretty output
// or a JSON envelope. Identical semantics to s01.
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
	} else if err != nil {
		fmt.Fprintln(errOut, "error:", err)
		return 1
	} else {
		fmt.Fprintln(out, prettyPrint(data))
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
	case nil:
		return ""
	default:
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b)
	}
}

// hasJSONFlag strips --json from argv and reports it. Same as s01.
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
