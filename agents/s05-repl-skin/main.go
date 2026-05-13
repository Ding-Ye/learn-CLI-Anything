// main.go is the s05 entrypoint: it builds the same demo harness s01 ships
// (echo + time) and hands it to a REPL. A -skill <path> flag and a CWD
// auto-detect surface a SKILL.md in the banner. We don't pull in stdlib
// `flag` to stay consistent with s01's hand-rolled arg parsing — the
// REPL only takes two optional knobs.
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// demo is the same s01 demo harness, re-declared. We want this module to
// stand alone (no cross-session imports) and the surface is tiny.
func demo() *CLI {
	return &CLI{
		Name: "demo",
		Help: "Demo harness: time + echo subcommands",
		Subcommands: map[string]*CLI{
			"time": {
				Name: "time",
				Help: "Print the current UTC time",
				Flags: []Flag{
					{Name: "format", Type: "string", Default: "rfc3339", Help: "rfc3339 | unix"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					format := "rfc3339"
					for i := 0; i < len(args)-1; i++ {
						if args[i] == "--format" {
							format = args[i+1]
						}
					}
					now := time.Now().UTC()
					if format == "unix" {
						return map[string]any{"unix": now.Unix()}, nil
					}
					return map[string]any{"rfc3339": now.Format(time.RFC3339)}, nil
				},
			},
			"echo": {
				Name: "echo",
				Help: "Echo arguments back",
				Flags: []Flag{
					{Name: "upper", Type: "bool", Default: false, Help: "uppercase the output"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					upper := false
					rest := make([]string, 0, len(args))
					for _, a := range args {
						if a == "--upper" {
							upper = true
							continue
						}
						rest = append(rest, a)
					}
					out := joinStrings(rest, " ")
					if upper {
						out = toUpper(out)
					}
					return out, nil
				},
			},
		},
	}
}

func joinStrings(s []string, sep string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}

func toUpper(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'a' && c <= 'z' {
			b[i] = c - 32
		}
	}
	return string(b)
}

// parseArgs walks os.Args looking for -skill <path>. Anything else is
// reported on stderr and ignored — the REPL has no positional args.
func parseArgs(argv []string) (skillPath string, err error) {
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "-skill", "--skill":
			if i+1 >= len(argv) {
				return "", fmt.Errorf("%s requires a path", argv[i])
			}
			skillPath = argv[i+1]
			i++
		case "-h", "--help":
			fmt.Println("usage: s05-repl-skin [-skill <path>]")
			os.Exit(0)
		default:
			return "", fmt.Errorf("unknown arg %q", argv[i])
		}
	}
	return skillPath, nil
}

func main() {
	skillPath, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	r := NewREPL(demo())

	// Resolve SKILL.md: -skill wins, else walk up from CWD. Both are
	// best-effort — a missing SKILL.md is not an error, the REPL just
	// falls back to the harness's own Name/Help in the banner.
	if skillPath == "" {
		cwd, _ := os.Getwd()
		if p, err := findSkill(cwd); err == nil {
			skillPath = p
		}
	}
	if skillPath != "" {
		if s, err := loadSkillFromPath(skillPath); err == nil {
			r.Skill = s
		} else {
			fmt.Fprintln(os.Stderr, "warning: could not parse skill:", err)
		}
	}

	if err := r.Run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
