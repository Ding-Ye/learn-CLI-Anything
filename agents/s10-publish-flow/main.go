// main.go is the s10 entrypoint. It exposes two subcommands:
//
//	publish run <src-dir> <out-dir>     run the full pipeline
//	publish status <out-dir>            print outDir/registry.json summary
//
// `run` is what CI calls. `status` is the read-only equivalent for
// inspecting an already-published out/ — equivalent to the upstream's
// `cli-hub list` against a local manifest file, but no HTTP fetch.
//
// We intentionally do not add flags. CI calls the publisher with two
// fixed paths from the workflow YAML; surface area minimal on purpose.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

func buildCLI() *CLI {
	return &CLI{
		Name: "publish",
		Help: "Publish CLI-Anything plugins from a src dir into an out dir",
		Subcommands: map[string]*CLI{
			"run": {
				Name: "run",
				Help: "Run the full scan/validate/bundle/sign/index pipeline",
				Flags: []Flag{
					{Name: "src-dir", Type: "string", Required: true, Help: "Directory containing plugin subdirs"},
					{Name: "out-dir", Type: "string", Required: true, Help: "Directory where artifacts and registry.json land"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 2 {
						return nil, errors.New("usage: publish run <src-dir> <out-dir>")
					}
					p := NewPipeline()
					rep, err := p.Run(ctx, args[0], args[1])
					if err != nil {
						return rep, err
					}
					if !rep.OK {
						// Surface the report shape but flag the failure
						// so the JSON envelope's "ok":false matches the
						// pipeline's OK.
						return rep, errors.New("pipeline failed; see steps[]")
					}
					return rep, nil
				},
			},
			"status": {
				Name: "status",
				Help: "Print the registry.json from a previously-run out-dir",
				Flags: []Flag{
					{Name: "out-dir", Type: "string", Required: true, Help: "Output directory from a prior `publish run`"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 1 {
						return nil, errors.New("usage: publish status <out-dir>")
					}
					rf, err := ReadStatus(args[0])
					if err != nil {
						return nil, err
					}
					return rf, nil
				},
			},
		},
	}
}

// parseFlags pulls the optional --json flag out of argv. We hand-roll
// rather than importing flag because the only knob is whether to print
// the Result envelope; matches s01's hand-rolled parsing style.
func parseFlags(argv []string) (jsonMode bool, rest []string) {
	rest = make([]string, 0, len(argv))
	for _, a := range argv {
		if a == "--json" {
			jsonMode = true
			continue
		}
		rest = append(rest, a)
	}
	return jsonMode, rest
}

func main() {
	jsonMode, args := parseFlags(os.Args[1:])
	root := buildCLI()
	code := Dispatch(context.Background(), root, args, jsonMode, os.Stdout, os.Stderr)
	if code != 0 {
		fmt.Fprintln(os.Stderr, "publish: exit code", code)
		os.Exit(code)
	}
}
