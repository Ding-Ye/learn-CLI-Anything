// repl.go implements the REPL skin that wraps any s01-style harness.
//
// The upstream cli-anything-plugin/repl_skin.py is a 567-line ANSI-art
// rendering library bound to prompt_toolkit. We deliberately keep this Go
// port to the *interaction shape* that matters for agents:
//
//   - line-oriented input via bufio.Scanner — no readline/prompt_toolkit
//   - meta-commands prefixed with ":" so they can never collide with the
//     wrapped harness's own subcommands (the upstream uses bare words like
//     "help" and "quit", which causes name-clashes; we fix that with the
//     colon convention)
//   - :json on toggles the same Result envelope Dispatch already emits in
//     s01, so an agent driving the REPL gets bit-identical bytes to running
//     the one-shot CLI
//
// Banner text shows the auto-detected SKILL.md name + description so the
// agent driving the REPL knows which capabilities it just attached to.
package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// REPL is the interactive skin around a CLI harness. It is decoupled from
// stdio (the four io.* fields) so tests can drive it with strings.Reader
// and bytes.Buffer — see repl_test.go.
type REPL struct {
	Harness *CLI
	In      io.Reader
	Out     io.Writer
	Err     io.Writer

	// History keeps every non-blank line the user typed (meta-commands
	// included). :history dumps it. We store it in-memory only — the
	// upstream's FileHistory pulls in prompt_toolkit which we elide.
	History []string

	// JSONMode is the same flag s01's --json sets, but per-session. The
	// :json on / :json off meta-command flips it at runtime so the agent
	// can switch printers mid-conversation.
	JSONMode bool

	// Skill, if non-nil, is the auto-detected SKILL.md surfacing the
	// harness's name + description. The banner shows it on Run() startup.
	Skill *Skill
}

// NewREPL constructs a REPL with sensible stdio defaults. Tests construct
// the struct literally; only main() uses this helper.
func NewREPL(h *CLI) *REPL {
	return &REPL{
		Harness: h,
		In:      os.Stdin,
		Out:     os.Stdout,
		Err:     os.Stderr,
	}
}

// errQuit is the sentinel the :quit handler returns to break the loop.
// We use a sentinel (rather than a bool from every handler) because the
// dispatch table is uniform: every meta-command is "func(args) error".
var errQuit = errors.New("repl: quit")

// Run is the main loop. It prints the banner, reads "> " prompts line by
// line, splits each line into argv via simple whitespace splitting (the
// upstream uses shlex.split which we skip — the harnesses we care about
// never take args with embedded spaces), then dispatches to either a
// meta-command (":foo") or the wrapped harness.
//
// Run returns nil on EOF / :quit, or the underlying error if reading
// fails. Per-command errors are printed and the loop continues — quitting
// because echo failed would be hostile to interactive use.
func (r *REPL) Run(ctx context.Context) error {
	r.printBanner()
	sc := bufio.NewScanner(r.In)
	// SKILL bodies can produce long lines; lift the default 64 KiB cap.
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for {
		fmt.Fprint(r.Out, "> ")
		if !sc.Scan() {
			break // EOF
		}
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			// empty input is a no-op, not "show help"; matches the
			// upstream and avoids dumping screenfuls of help when the
			// user hits Enter twice
			continue
		}
		r.History = append(r.History, line)
		if strings.HasPrefix(line, ":") {
			if err := r.runMeta(line); err != nil {
				if errors.Is(err, errQuit) {
					return nil
				}
				fmt.Fprintln(r.Err, "error:", err)
			}
			continue
		}
		argv := strings.Fields(line)
		// Dispatch handles printing for both human and JSON modes; we
		// only need to forward our io.Writers.
		_ = Dispatch(ctx, r.Harness, argv, r.JSONMode, r.Out, r.Err)
	}
	return sc.Err()
}

// printBanner is the Go-port equivalent of repl_skin.print_banner(). The
// upstream paints a ◆-decorated box; we keep it plain ASCII so the same
// bytes serialize through pipes without ANSI noise.
func (r *REPL) printBanner() {
	name := r.Harness.Name
	desc := r.Harness.Help
	if r.Skill != nil {
		if r.Skill.Meta.Name != "" {
			name = r.Skill.Meta.Name
		}
		if r.Skill.Meta.Description != "" {
			desc = r.Skill.Meta.Description
		}
	}
	fmt.Fprintf(r.Out, "cli-anything · %s\n", name)
	if desc != "" {
		fmt.Fprintf(r.Out, "  %s\n", desc)
	}
	fmt.Fprintln(r.Out, "  Type :help for meta-commands, :quit to exit")
}

// runMeta dispatches a ":foo arg arg" line. Switching on the head keeps
// the table flat and easy to read; with five meta-commands a real map
// would be heavier than this switch.
func (r *REPL) runMeta(line string) error {
	parts := strings.Fields(line)
	head := parts[0]
	rest := parts[1:]
	switch head {
	case ":help":
		fmt.Fprintln(r.Out, "meta-commands:")
		fmt.Fprintln(r.Out, "  :help              show this message")
		fmt.Fprintln(r.Out, "  :skills            list harness subcommands")
		fmt.Fprintln(r.Out, "  :json on|off       toggle JSON envelope output")
		fmt.Fprintln(r.Out, "  :history           print line history")
		fmt.Fprintln(r.Out, "  :quit              exit the REPL")
		return nil
	case ":skills":
		// "skills" here means subcommands of the wrapped harness — the
		// vocabulary tracks the upstream's SKILL.md framing where the
		// CLI surface IS the skill. We list them in the same sorted
		// order Dispatch's help uses for consistency.
		for _, name := range subcommandNames(r.Harness) {
			sub := r.Harness.Subcommands[name]
			fmt.Fprintf(r.Out, "  %s  %s\n", name, sub.Help)
		}
		return nil
	case ":json":
		if len(rest) != 1 || (rest[0] != "on" && rest[0] != "off") {
			return errors.New(":json requires 'on' or 'off'")
		}
		r.JSONMode = rest[0] == "on"
		fmt.Fprintf(r.Out, "json mode: %s\n", rest[0])
		return nil
	case ":history":
		for i, h := range r.History {
			// don't include the :history command itself in its own
			// output — strip the trailing entry which is this line
			if i == len(r.History)-1 && h == line {
				continue
			}
			fmt.Fprintf(r.Out, "  %d  %s\n", i+1, h)
		}
		return nil
	case ":quit", ":exit", ":q":
		fmt.Fprintln(r.Out, "bye")
		return errQuit
	default:
		return fmt.Errorf("unknown meta-command %q (try :help)", head)
	}
}

// findSkill walks up from `start` looking for a SKILL.md. The upstream
// has a more elaborate search (skills/<id>/SKILL.md under repo root, then
// packaged path); we keep it minimal: a SKILL.md in the CWD or any
// ancestor is enough for the curriculum's purposes.
func findSkill(start string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", err
	}
	for {
		candidate := filepath.Join(dir, "SKILL.md")
		if fi, err := os.Stat(candidate); err == nil && !fi.IsDir() {
			return candidate, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}

// loadSkillFromPath opens and parses a SKILL.md. Used by main(); split out
// so tests can call it with a fixture path.
func loadSkillFromPath(path string) (*Skill, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseSkill(f)
}
