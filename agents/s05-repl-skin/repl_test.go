// repl_test.go exercises the REPL by piping scripted input through a
// strings.Reader and capturing output to a bytes.Buffer. Because the REPL
// loop reads via bufio.Scanner, line-buffered scripted input is exactly
// what a user typing into a terminal would produce — modulo timing.
//
// We pin the harness to the same s01 demo so the assertions can pattern
// on "echo hi" and the time subcommand without re-deriving them.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// newTestREPL builds a REPL with the demo harness wired up to in-memory
// streams. We expose stdout and stderr separately because a couple of the
// assertions inspect only one of them (errors go to stderr, normal
// output to stdout — same contract as s01's Dispatch).
func newTestREPL(t *testing.T, script string) (*REPL, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	r := &REPL{
		Harness: demo(),
		In:      strings.NewReader(script),
		Out:     outBuf,
		Err:     errBuf,
	}
	return r, outBuf, errBuf
}

// TestEchoCommand drives the wrapped harness through a single
// "echo hi" line. The expected stdout contains both the prompt and the
// echoed payload; we assert on the payload because the prompt is plain
// scaffolding.
func TestEchoCommand(t *testing.T) {
	r, out, _ := newTestREPL(t, "echo hi\n")
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(out.String(), "hi") {
		t.Fatalf("expected 'hi' in output, got: %q", out.String())
	}
}

// TestJSONModeEnvelope flips :json on and then runs echo. The output line
// after the toggle should be a JSON envelope identical to what s01
// produces in --json mode. We decode it to assert structure rather than
// match raw bytes (Go's encoder may shift field order if Result ever
// grows new fields).
func TestJSONModeEnvelope(t *testing.T) {
	r, out, _ := newTestREPL(t, ":json on\necho hi\n")
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Find a JSON envelope anywhere in the stream. We scan for the first
	// '{' on each line (because the "> " prompt is written on the same
	// physical line as the response when streams are unbuffered).
	var env Result
	found := false
	for _, line := range strings.Split(out.String(), "\n") {
		brace := strings.Index(line, "{")
		if brace < 0 {
			continue
		}
		if err := json.Unmarshal([]byte(line[brace:]), &env); err == nil {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("no JSON envelope in output: %q", out.String())
	}
	if !env.OK || env.Data != "hi" {
		t.Fatalf("envelope wrong: %+v", env)
	}
}

// TestSkillsListsSubcommands runs :skills and asserts every demo
// subcommand appears. We don't pin order — subcommandNames sorts, but the
// agent-facing behavior is "all names appear", not "in this exact order".
func TestSkillsListsSubcommands(t *testing.T) {
	r, out, _ := newTestREPL(t, ":skills\n")
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, name := range []string{"echo", "time"} {
		if !strings.Contains(out.String(), name) {
			t.Fatalf(":skills missing %q in: %s", name, out.String())
		}
	}
}

// TestQuitExits feeds :quit and a *second* command that should never run
// (the loop must terminate before reading it). We assert the second
// command's marker text is absent from the output as proof.
func TestQuitExits(t *testing.T) {
	r, out, _ := newTestREPL(t, ":quit\necho should-not-appear\n")
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if strings.Contains(out.String(), "should-not-appear") {
		t.Fatalf("REPL did not stop on :quit; got: %q", out.String())
	}
	if !strings.Contains(out.String(), "bye") {
		t.Fatalf("missing goodbye, got: %q", out.String())
	}
}

// TestEmptyLineIgnored sends two blank lines then a real command. The
// empty lines must not produce help dumps or errors — only the real
// command's output should be present besides the prompt scaffolding.
func TestEmptyLineIgnored(t *testing.T) {
	r, out, errBuf := newTestREPL(t, "\n\necho hi\n")
	if err := r.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if errBuf.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", errBuf.String())
	}
	if !strings.Contains(out.String(), "hi") {
		t.Fatalf("real command did not run; got: %q", out.String())
	}
}
