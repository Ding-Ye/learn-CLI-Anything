// main.go — the verifier as a small harness in its own right. One command:
//
//	verify <plugin-dir> [--json]
//
// In production we shell out the harness smoke-tests through /bin/sh -c so a
// plugin that ships a shell-script entrypoint works without us caring about
// shebangs and exec permission edge cases. The tests wire in a FakeRunner
// instead so they don't depend on the host's /bin/sh.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// shellRunner is the production Runner. It joins args with shell-quoting,
// runs them through `/bin/sh -c`, and returns a coarse exit code. We don't
// surface signals separately — non-zero is non-zero, the report doesn't
// care if it was SIGTERM or exit 7.
type shellRunner struct{}

func (shellRunner) Exec(ctx context.Context, args []string, stdin []byte) (int, []byte, []byte, error) {
	if len(args) == 0 {
		return 1, nil, nil, fmt.Errorf("no command")
	}
	cmdline := shellJoin(args)
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", cmdline)
	if stdin != nil {
		cmd.Stdin = strings.NewReader(string(stdin))
	}
	var outBuf, errBuf strings.Builder
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	exitCode := 0
	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode = exitErr.ExitCode()
		err = nil
	}
	return exitCode, []byte(outBuf.String()), []byte(errBuf.String()), err
}

// shellJoin escapes each arg with single-quotes so a path with a space
// survives the /bin/sh -c trip. We don't try to support every shell metachar
// — the verifier only sends paths and bare flags.
func shellJoin(args []string) string {
	var b strings.Builder
	for i, a := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteByte('\'')
		b.WriteString(strings.ReplaceAll(a, "'", `'\''`))
		b.WriteByte('\'')
	}
	return b.String()
}

func buildCLI() *CLI {
	return &CLI{
		Name: "s08-verify-plugin",
		Help: "Verify a CLI-Anything plugin directory",
		Subcommands: map[string]*CLI{
			"verify": {
				Name: "verify",
				Help: "Run the full check suite on <plugin-dir>",
				Flags: []Flag{
					{Name: "json", Type: "bool", Default: false, Help: "emit JSON envelope"},
				},
				Run: func(ctx context.Context, args []string) (any, error) {
					if len(args) == 0 {
						return nil, fmt.Errorf("usage: verify <plugin-dir>")
					}
					rep, err := Verify(args[0], shellRunner{})
					if err != nil {
						return nil, err
					}
					return rep, nil
				},
			},
		},
	}
}

// runMain holds the body of main() so tests (if we wanted them) could drive
// it. main() is the os.Args/os.Stdout adapter.
func runMain(argv []string, out, errOut io.Writer) int {
	rest, jsonMode := stripJSON(argv)
	if len(rest) == 0 {
		fmt.Fprintln(errOut, "usage: s08-verify-plugin verify <plugin-dir> [--json]")
		return 2
	}
	rc := Dispatch(context.Background(), buildCLI(), rest, jsonMode, out, errOut)
	// When humans run it we additionally print the rendered report (Dispatch
	// only prints the pretty-print of *Report, which is fine but doesn't
	// give the upstream-style ✓/✗ output).
	return rc
}

// stripJSON pulls --json out of argv early so Dispatch sees just the verb.
// Mirrors hasJSONFlag in s02 — kept inline because it's three lines.
func stripJSON(argv []string) ([]string, bool) {
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

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}
