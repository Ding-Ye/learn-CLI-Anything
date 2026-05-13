package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// demo is the same shape as s01's demo harness — two subcommands with
// flags — so this chapter shows the generator working end-to-end on a
// realistic CLI. Re-declared instead of imported to keep the module
// self-contained.
func demo() *CLI {
	return &CLI{
		Name: "demo",
		Help: "Demo harness: time + echo subcommands. Used by s03 to show automatic SKILL.md generation.",
		Subcommands: map[string]*CLI{
			"time": {
				Name: "time",
				Help: "Print the current UTC time",
				Flags: []Flag{
					{Name: "format", Type: "string", Default: "rfc3339", Help: "rfc3339 | unix"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					return map[string]any{"rfc3339": time.Now().UTC().Format(time.RFC3339)}, nil
				},
			},
			"echo": {
				Name: "echo",
				Help: "Echo arguments back",
				Flags: []Flag{
					{Name: "upper", Type: "bool", Default: false, Help: "uppercase the output"},
					{Name: "text", Type: "string", Required: true, Help: "string to echo"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					return "", nil
				},
			},
		},
	}
}

func main() {
	args := os.Args[1:]
	if len(args) == 0 || args[0] != "demo" {
		fmt.Fprintln(os.Stderr, "usage: skill-gen demo")
		fmt.Fprintln(os.Stderr, "  Synthesizes a SKILL.md from the built-in demo harness and prints it to stdout.")
		os.Exit(2)
	}
	skill := GenerateSkill(demo())
	out, err := RenderSkill(skill)
	if err != nil {
		fmt.Fprintln(os.Stderr, "render:", err)
		os.Exit(1)
	}
	os.Stdout.Write(out)
}
