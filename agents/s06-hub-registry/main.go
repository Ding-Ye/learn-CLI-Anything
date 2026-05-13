// main.go wires registry/Cache/Hub into a tiny CLI: `hub search/list/info`.
// The demo defaults to a FileSource pointed at testdata/registry.json so
// `make demo` runs offline; pass -url <addr> to swap in an HTTPSource and
// -cache <path> to override the cache file.
//
// We deliberately keep arg parsing hand-rolled (no flag package) for the
// same reason s01 does: it makes the introspection path one struct walk,
// and we only have three knobs.
package main

import (
	"context"
	"fmt"
	"os"
	"time"
)

// buildRoot returns the `hub` command tree. The subcommands close over
// the Hub value rather than re-fetching per call — `hub list` followed by
// `hub info <name>` is one logical session.
func buildRoot(h *Hub) *CLI {
	return &CLI{
		Name: "hub",
		Help: "Query the CLI-Anything registry (search/list/info).",
		Subcommands: map[string]*CLI{
			"search": {
				Name: "search",
				Help: "Search manifests by name substring (case-insensitive).",
				Flags: []Flag{
					{Name: "query", Type: "string", Required: true, Help: "substring to match against Name and Backend"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("usage: hub search <query>")
					}
					return h.Search(args[0]), nil
				},
			},
			"list": {
				Name: "list",
				Help: "List every manifest in the index.",
				Run: func(ctx context.Context, args []string) (any, error) {
					return h.List(), nil
				},
			},
			"info": {
				Name: "info",
				Help: "Show one manifest by name.",
				Flags: []Flag{
					{Name: "name", Type: "string", Required: true, Help: "manifest name (case-insensitive)"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("usage: hub info <name>")
					}
					return h.Info(args[0])
				},
			},
		},
	}
}

// parsedArgs holds the small set of pre-Dispatch knobs.
type parsedArgs struct {
	source   string // "file" | "http"
	path     string // FileSource path
	url      string // HTTPSource URL
	cache    string // cache file path, "" → DefaultCachePath()
	ttl      string // human duration (e.g. "1h"); empty → 1h
	jsonMode bool
	rest     []string
}

// parseArgs walks os.Args looking for our knobs and stops at the first
// non-flag token, which we treat as the start of the `hub <verb> ...`
// portion. This mirrors `kubectl --context=foo get pods` shape.
func parseArgs(argv []string) (parsedArgs, error) {
	p := parsedArgs{source: "file"}
	i := 0
	for i < len(argv) {
		a := argv[i]
		switch a {
		case "--json":
			p.jsonMode = true
			i++
		case "-file", "--file":
			if i+1 >= len(argv) {
				return p, fmt.Errorf("%s requires a path", a)
			}
			p.source = "file"
			p.path = argv[i+1]
			i += 2
		case "-url", "--url":
			if i+1 >= len(argv) {
				return p, fmt.Errorf("%s requires a URL", a)
			}
			p.source = "http"
			p.url = argv[i+1]
			i += 2
		case "-cache", "--cache":
			if i+1 >= len(argv) {
				return p, fmt.Errorf("%s requires a path", a)
			}
			p.cache = argv[i+1]
			i += 2
		case "-ttl", "--ttl":
			if i+1 >= len(argv) {
				return p, fmt.Errorf("%s requires a duration", a)
			}
			p.ttl = argv[i+1]
			i += 2
		case "-h", "--help":
			fmt.Println("usage: s06-hub-registry [--file <path> | --url <addr>] [--cache <path>] [--ttl 1h] [--json] <verb> [args]")
			os.Exit(0)
		default:
			p.rest = append(p.rest, argv[i])
			i++
		}
	}
	return p, nil
}

func buildSource(p parsedArgs) Source {
	if p.source == "http" {
		return &HTTPSource{URL: p.url}
	}
	path := p.path
	if path == "" {
		path = "testdata/registry.json"
	}
	return &FileSource{Path: path}
}

func main() {
	p, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(2)
	}

	src := buildSource(p)

	// Wrap the source in a Cache only when the user gave us an HTTP URL.
	// Hitting the cache for a local file would just add disk IO with no
	// upside; the file *is* the cache.
	var hubSource Source = src
	if p.source == "http" {
		ttl := parseTTL(p.ttl)
		cachePath := p.cache
		if cachePath == "" {
			cachePath = DefaultCachePath()
		}
		hubSource = &Cache{Source: src, Path: cachePath, TTL: ttl}
	}

	idx, err := hubSource.FetchIndex(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	hub := &Hub{Index: idx}

	code := Dispatch(context.Background(), buildRoot(hub), p.rest, p.jsonMode, os.Stdout, os.Stderr)
	os.Exit(code)
}

// parseTTL is a tiny shim around time.ParseDuration that defaults to 1h
// when the input is empty. We don't bubble the parse error up — a bad TTL
// shouldn't kill `hub list`, it should fall back to the default.
func parseTTL(s string) time.Duration {
	const defaultTTL = time.Hour
	if s == "" {
		return defaultTTL
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return defaultTTL
	}
	return parsed
}
