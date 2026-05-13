// verify_test.go — five scenarios that exercise the Issue codes we care
// about. The pattern: build a temporary plugin layout under t.TempDir(),
// wire in a FakeRunner whose canned responses match what a real harness
// would emit, run Verify, then assert on Issues + Pass.
//
// The FakeRunner exists because the smoke-test checks (--help, --json)
// can't be exercised against a real binary in a unit test without a build
// step. By making Runner an interface, the test substitutes a fake whose
// behavior is scripted from a map[string]canned. This is the same pattern
// the upstream tests/ use for cli-hub HTTP fakes.
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// FakeRunner is keyed off the *last* path segment of the argv[0] plus the
// remaining args (so a different plugin's `harness --help` doesn't conflict
// with another test's). Each entry is a canned (exitCode, stdout) tuple.
type FakeRunner struct {
	// responses maps "harness --help" -> (0, "...help text...")
	responses map[string]struct {
		exitCode int
		stdout   string
	}
}

func newFakeRunner() *FakeRunner {
	return &FakeRunner{responses: map[string]struct {
		exitCode int
		stdout   string
	}{}}
}

func (f *FakeRunner) set(key string, exitCode int, stdout string) {
	f.responses[key] = struct {
		exitCode int
		stdout   string
	}{exitCode, stdout}
}

func (f *FakeRunner) Exec(ctx context.Context, args []string, stdin []byte) (int, []byte, []byte, error) {
	// Build key from "<harness-base> <args...>"
	base := filepath.Base(args[0])
	key := base
	if len(args) > 1 {
		key = base + " " + strings.Join(args[1:], " ")
	}
	r, ok := f.responses[key]
	if !ok {
		return 127, nil, []byte("command not found: " + key), nil
	}
	return r.exitCode, []byte(r.stdout), nil, nil
}

// writePlugin writes a minimal plugin layout into dir. The caller passes
// `skill` as the literal SKILL.md bytes (or empty to skip) and `harness`
// as "true" to also place an executable stub. README.md is always written
// unless `skipReadme` is true.
func writePlugin(t *testing.T, dir string, skill string, withHarness, skipReadme bool) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if skill != "" {
		if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(skill), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !skipReadme {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# plugin\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if withHarness {
		hp := filepath.Join(dir, "harness")
		if err := os.WriteFile(hp, []byte("#!/bin/sh\necho ok\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// findIssue returns the first Issue with the given code, or "" if absent.
// We assert on Code, not Message, so wording can change without breaking
// downstream agents that key off the code.
func findIssue(issues []Issue, code string) *Issue {
	for i := range issues {
		if issues[i].Code == code {
			return &issues[i]
		}
	}
	return nil
}

// Test 1: SKILL.md missing → S001 fires, Pass=false.
func TestVerifyMissingSkill(t *testing.T) {
	dir := t.TempDir()
	writePlugin(t, dir, "" /*no skill*/, true /*harness*/, false)
	rner := newFakeRunner()
	rner.set("harness --help", 0, "usage: harness [...]\n")
	rner.set("harness --json", 0, `{"ok":true}`)

	rep, err := Verify(dir, rner)
	if err != nil {
		t.Fatal(err)
	}
	if findIssue(rep.Issues, "S001") == nil {
		t.Fatalf("expected S001 (missing SKILL.md), got: %+v", rep.Issues)
	}
	if rep.Pass {
		t.Fatalf("expected Pass=false; report: %+v", rep)
	}
}

// Test 2: SKILL.md present but missing `name` → S002 fires.
// Description present so S003 (warn) does not.
func TestVerifySkillMissingName(t *testing.T) {
	dir := t.TempDir()
	skill := "---\ndescription: A demo plugin\n---\n# body\n"
	writePlugin(t, dir, skill, true, false)
	rner := newFakeRunner()
	rner.set("harness --help", 0, "usage\n")
	rner.set("harness --json", 0, `{"ok":true}`)

	rep, err := Verify(dir, rner)
	if err != nil {
		t.Fatal(err)
	}
	if findIssue(rep.Issues, "S002") == nil {
		t.Fatalf("expected S002 (missing name); issues=%+v", rep.Issues)
	}
	if rep.Pass {
		t.Fatalf("expected Pass=false; report=%+v", rep)
	}
}

// Test 3: harness --json prints a JSON object that has no `ok` key → S004.
func TestVerifyJSONEnvelopeMissingOK(t *testing.T) {
	dir := t.TempDir()
	skill := "---\nname: demo\ndescription: A demo plugin\n---\n# body\n"
	writePlugin(t, dir, skill, true, false)
	rner := newFakeRunner()
	rner.set("harness --help", 0, "usage\n")
	rner.set("harness --json", 0, `{"data":"hi"}`) // missing "ok"

	rep, err := Verify(dir, rner)
	if err != nil {
		t.Fatal(err)
	}
	if findIssue(rep.Issues, "S004") == nil {
		t.Fatalf("expected S004 (missing ok key); issues=%+v", rep.Issues)
	}
	if rep.Pass {
		t.Fatalf("expected Pass=false; report=%+v", rep)
	}
}

// Test 4: a fully well-formed plugin → no errors, Pass=true.
func TestVerifyAllGood(t *testing.T) {
	dir := t.TempDir()
	skill := "---\nname: demo\ndescription: A demo plugin\ntriggers:\n  - demo\n  - hello\n---\n# body\n"
	writePlugin(t, dir, skill, true, false)
	rner := newFakeRunner()
	rner.set("harness --help", 0, "usage: harness ...\n")
	rner.set("harness --json", 0, `{"ok":true,"data":null}`)

	rep, err := Verify(dir, rner)
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range rep.Issues {
		if x.Severity == "error" {
			t.Fatalf("unexpected error-issue: %+v", x)
		}
	}
	if !rep.Pass {
		t.Fatalf("expected Pass=true; issues=%+v", rep.Issues)
	}
}

// Test 5: warnings (missing description) don't fail Pass.
// Triggers must be a list (we set it correctly); name is present; only
// description is missing → only S003 fires, Pass stays true.
func TestVerifyWarningsDoNotFailPass(t *testing.T) {
	dir := t.TempDir()
	skill := "---\nname: demo\n---\n# body\n"
	writePlugin(t, dir, skill, true, false)
	rner := newFakeRunner()
	rner.set("harness --help", 0, "usage\n")
	rner.set("harness --json", 0, `{"ok":true}`)

	rep, err := Verify(dir, rner)
	if err != nil {
		t.Fatal(err)
	}
	if findIssue(rep.Issues, "S003") == nil {
		t.Fatalf("expected S003 (missing description warn); issues=%+v", rep.Issues)
	}
	if !rep.Pass {
		t.Fatalf("warnings should not fail Pass; report=%+v", rep)
	}
}
