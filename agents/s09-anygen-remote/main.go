// main.go wires the four primitives in client.go + poller.go into an
// s01-style CLI: `anygen submit | status | wait | result`. The shape
// matches the upstream `cli-anything-anygen task create | status | poll`
// — same lifecycle, just renamed to fit our minimal client surface.
//
// Configuration is read from env vars (ANYGEN_BASE_URL, ANYGEN_API_KEY)
// so the agent that calls this doesn't have to thread credentials through
// argv. A --json flag mirrors s01: when set, every command emits the
// Result envelope and exits with code 0/1.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"
)

const defaultBaseURL = "https://www.anygen.io"

// anygenRoot is the top-level CLI tree. We declare it as a function so
// tests (and the demo in the Makefile) can pass a custom *APIClient
// without rebuilding env-var resolution.
func anygenRoot(client *APIClient) *CLI {
	return &CLI{
		Name: "anygen",
		Help: "Wrap the AnyGen remote skill-generation API (HTTP, async)",
		Subcommands: map[string]*CLI{
			"submit": {
				Name: "submit",
				Help: "POST a prompt; print the new jobID",
				Flags: []Flag{
					{Name: "prompt", Type: "string", Required: true, Help: "Generation prompt (positional)"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("submit: prompt is required")
					}
					prompt := joinArgs(args)
					id, err := client.SubmitJob(ctx, prompt)
					if err != nil {
						return nil, err
					}
					return map[string]any{"job_id": id}, nil
				},
			},
			"status": {
				Name: "status",
				Help: "GET current status (queued | running | succeeded | failed)",
				Flags: []Flag{
					{Name: "id", Type: "string", Required: true, Help: "Job ID (positional)"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("status: jobID is required")
					}
					s, err := client.PollStatus(ctx, args[0])
					if err != nil {
						return nil, err
					}
					return map[string]any{"job_id": args[0], "status": string(s)}, nil
				},
			},
			"wait": {
				Name: "wait",
				Help: "Poll until terminal, then return the result. --interval=2s --timeout=20m",
				Flags: []Flag{
					{Name: "id", Type: "string", Required: true, Help: "Job ID (positional)"},
					{Name: "interval", Type: "string", Default: "2s", Help: "poll interval (Go duration)"},
					{Name: "timeout", Type: "string", Default: "20m", Help: "max wait (Go duration)"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					id, interval, timeout, err := parseWaitArgs(args)
					if err != nil {
						return nil, err
					}
					cctx, cancel := context.WithTimeout(ctx, timeout)
					defer cancel()
					rs, err := WaitForResult(cctx, client, id, interval)
					if err != nil {
						return nil, err
					}
					return map[string]any{
						"job_id":       rs.Result.JobID,
						"status":       string(rs.Status),
						"output":       rs.Result.Output,
						"content_type": rs.Result.ContentType,
					}, nil
				},
			},
			"result": {
				Name: "result",
				Help: "GET the artefact of a succeeded job (no polling)",
				Flags: []Flag{
					{Name: "id", Type: "string", Required: true, Help: "Job ID (positional)"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("result: jobID is required")
					}
					res, err := client.FetchResult(ctx, args[0])
					if err != nil {
						return nil, err
					}
					return map[string]any{
						"job_id":       res.JobID,
						"output":       res.Output,
						"content_type": res.ContentType,
					}, nil
				},
			},
		},
	}
}

// parseWaitArgs reads `<id> [--interval D] [--timeout D]`. Hand-rolled
// (not flag.Parse) so it matches s01-s05's style and stays zero-dep.
func parseWaitArgs(args []string) (id string, interval, timeout time.Duration, err error) {
	interval = 2 * time.Second
	timeout = 20 * time.Minute
	if len(args) == 0 {
		return "", 0, 0, fmt.Errorf("wait: jobID is required")
	}
	id = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--interval":
			if i+1 >= len(args) {
				return "", 0, 0, fmt.Errorf("--interval needs a value")
			}
			d, perr := time.ParseDuration(args[i+1])
			if perr != nil {
				return "", 0, 0, fmt.Errorf("--interval: %w", perr)
			}
			interval = d
			i++
		case "--timeout":
			if i+1 >= len(args) {
				return "", 0, 0, fmt.Errorf("--timeout needs a value")
			}
			d, perr := time.ParseDuration(args[i+1])
			if perr != nil {
				return "", 0, 0, fmt.Errorf("--timeout: %w", perr)
			}
			timeout = d
			i++
		default:
			return "", 0, 0, fmt.Errorf("unknown arg %q", args[i])
		}
	}
	return id, interval, timeout, nil
}

func joinArgs(args []string) string {
	out := ""
	for i, a := range args {
		if i > 0 {
			out += " "
		}
		out += a
	}
	return out
}

// envBaseURL / envAPIKey read from the process env with sensible defaults.
// We mirror the upstream's priority order from anygen_backend.py
// (CLI > env > config file) but drop the config-file tier — for the
// curriculum, env vars are plenty.
func envBaseURL() string {
	if v := os.Getenv("ANYGEN_BASE_URL"); v != "" {
		return v
	}
	return defaultBaseURL
}

func envAPIKey() string {
	return os.Getenv("ANYGEN_API_KEY")
}

func main() {
	argv := os.Args[1:]
	jsonMode := false
	rest := make([]string, 0, len(argv))
	for _, a := range argv {
		if a == "--json" {
			jsonMode = true
			continue
		}
		rest = append(rest, a)
	}

	client := NewAPIClient(envBaseURL(), envAPIKey())
	root := anygenRoot(client)

	code := Dispatch(context.Background(), root, rest, jsonMode, os.Stdout, os.Stderr)
	// Silence "unused" lints for helpers that exist for future expansion.
	_ = strconv.Itoa
	os.Exit(code)
}
