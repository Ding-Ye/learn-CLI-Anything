package main

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// previewCLI builds the harness. Two subcommands:
//
//   preview run -- <cmd> [args...]   — execute (or replay) and print result
//   preview show <key>               — read a cached bundle by key
//
// The "--" separator after `run` is conventional: anything past it is
// the user's command, not our flags. That keeps `run` from needing its
// own --json or --upper-style flag parsing.
func previewCLI(store Store, cacheRoot string) *CLI {
	return &CLI{
		Name: "preview",
		Help: "Content-addressed cache for command outputs",
		Subcommands: map[string]*CLI{
			"run": {
				Name: "run",
				Help: "Execute a command (or replay its cached result)",
				Flags: []Flag{
					{Name: "--", Type: "string", Help: "separator before the command to run"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					cmd, err := stripDoubleDash(args)
					if err != nil {
						return nil, err
					}
					b, hit, err := Run(ctx, cmd, nil, store)
					if err != nil {
						return nil, err
					}
					return map[string]any{
						"cache_hit": hit,
						"key":       b.Key,
						"stdout":    b.Stdout,
						"stderr":    b.Stderr,
						"exit_code": b.ExitCode,
					}, nil
				},
			},
			"show": {
				Name: "show",
				Help: "Print a cached bundle by its sha256 key",
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) != 1 {
						return nil, errors.New("show: exactly one key argument required")
					}
					b, ok := store.Get(args[0])
					if !ok {
						return nil, fmt.Errorf("show: no bundle for key %q", args[0])
					}
					return b, nil
				},
			},
			"cache-dir": {
				Name: "cache-dir",
				Help: "Print where the on-disk cache lives",
				Run: func(ctx context.Context, args []string) (any, error) {
					return map[string]any{"cache_dir": cacheRoot}, nil
				},
			},
		},
	}
}

// stripDoubleDash removes everything up to and including "--" so the
// rest is the user's command. If "--" is missing we treat the whole
// argv as the command (forgiving mode; matches what `kubectl exec` etc.
// do).
func stripDoubleDash(args []string) ([]string, error) {
	for i, a := range args {
		if a == "--" {
			if i+1 >= len(args) {
				return nil, errors.New("run: nothing after --")
			}
			return args[i+1:], nil
		}
	}
	if len(args) == 0 {
		return nil, errors.New("run: nothing to run")
	}
	return args, nil
}

func parseCacheFlag(argv []string) (rest []string, cache string) {
	rest = make([]string, 0, len(argv))
	for i := 0; i < len(argv); i++ {
		if argv[i] == "-cache" && i+1 < len(argv) {
			cache = argv[i+1]
			i++
			continue
		}
		rest = append(rest, argv[i])
	}
	return rest, cache
}

func main() {
	argv, jsonMode := hasJSONFlag(os.Args[1:])
	argv, cacheOverride := parseCacheFlag(argv)
	store, err := NewDiskStore(cacheOverride)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}
	code := Dispatch(context.Background(), previewCLI(store, store.Root()), argv, jsonMode, os.Stdout, os.Stderr)
	os.Exit(code)
}
