package main

import (
	"context"
	"os"
	"time"
)

// demo is a tiny harness with two subcommands. The point is to show what
// the CLI tree LOOKS like in Go; the real value comes when s03 introspects
// it and produces a SKILL.md from this same struct.
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

func main() {
	argv, jsonMode := hasJSONFlag(os.Args[1:])
	code := Dispatch(context.Background(), demo(), argv, jsonMode, os.Stdout, os.Stderr)
	os.Exit(code)
}
