// main.go wires the installer into a tiny CLI surface: install, uninstall,
// list. The dispatcher itself is in cli.go; here we only build the command
// tree and inject a Registry into each leaf's closure.
//
// We deliberately don't expose --json on every subcommand the way s01 does.
// install/uninstall print "ok"/"err"; list emits a JSON array regardless.
// That matches what an agent actually needs from a package manager (a
// success bit + the resulting state) without the ceremony of a per-call mode.
package main

import (
	"context"
	"fmt"
	"os"
)

func newCLI(reg *Registry) *CLI {
	return &CLI{
		Name: "s07-installer",
		Help: "Multi-backend installer (pip / npm / bundled / fake)",
		Subcommands: map[string]*CLI{
			"install": {
				Name: "install",
				Help: "Install a manifest by path: install <manifest.json>",
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 1 {
						return nil, fmt.Errorf("install: need a manifest path")
					}
					m, err := LoadManifest(args[0])
					if err != nil {
						return nil, err
					}
					if err := reg.Install(ctx, m); err != nil {
						return nil, err
					}
					return map[string]any{"installed": m.Name, "version": m.Version, "backend": m.Backend}, nil
				},
			},
			"uninstall": {
				Name: "uninstall",
				Help: "Uninstall by name: uninstall <name>",
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) < 1 {
						return nil, fmt.Errorf("uninstall: need a name")
					}
					if err := reg.Uninstall(ctx, args[0]); err != nil {
						return nil, err
					}
					return map[string]any{"uninstalled": args[0]}, nil
				},
			},
			"list": {
				Name: "list",
				Help: "List installed manifests",
				Run: func(ctx context.Context, _ []string) (any, error) {
					all, err := reg.List(ctx)
					if err != nil {
						return nil, err
					}
					if all == nil {
						all = []Manifest{}
					}
					return all, nil
				},
			},
		},
	}
}

func main() {
	cacheDir, err := DefaultCacheDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: cache dir:", err)
		os.Exit(1)
	}
	reg := NewRegistry(cacheDir)

	// `--json` toggle is global, parsed off the front. Everything else is
	// passed as the subcommand argv.
	jsonMode := false
	argv := os.Args[1:]
	if len(argv) > 0 && (argv[0] == "--json" || argv[0] == "-json") {
		jsonMode = true
		argv = argv[1:]
	}
	if len(argv) == 0 {
		printHelp(newCLI(reg), os.Stdout, jsonMode)
		return
	}
	os.Exit(Dispatch(context.Background(), newCLI(reg), argv, jsonMode, os.Stdout, os.Stderr))
}
