// checks.go — the actual verification rules. Each `check_*` function takes a
// plugin directory + a Runner and returns the list of Issues it found. The
// design lets us swap in a FakeRunner in tests so we never have to compile
// a real harness just to exercise the harness-execution checks.
//
// Issue codes follow a flat S### namespace so a CI grep on (e.g.) "S004"
// gives a deterministic match. The codes deliberately mirror the upstream
// `verify-plugin.sh` structure: required-files first, then content validity,
// then runtime smoke. Severity is two-valued ("error" fails the report;
// "warn" surfaces in output but Pass stays true).
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Issue is a single finding. Path is informational — the report prints it
// so a human can jump to the file. Code lets agents branch on a specific
// finding without string-matching Message.
type Issue struct {
	Severity string `json:"severity"` // "error" | "warn"
	Code     string `json:"code"`     // e.g. "S001"
	Message  string `json:"message"`
	Path     string `json:"path,omitempty"`
}

// Runner is the indirection that lets tests substitute a FakeRunner. In
// production main.go wires in a /bin/sh -c runner; in tests we wire in a
// programmable one that returns canned stdout for canned argv.
//
// The signature is deliberately string-arg-flat (not exec.Cmd) so the fake
// can be trivially scripted from a map[string][]byte.
type Runner interface {
	Exec(ctx context.Context, args []string, stdin []byte) (exitCode int, stdout, stderr []byte, err error)
}

// harnessTimeout caps each smoke-test invocation. A misbehaving harness that
// blocks forever shouldn't hang the verifier; 10s is plenty for --help.
const harnessTimeout = 10 * time.Second

// findSkillMD locates the SKILL.md inside a plugin directory. The upstream
// layout uses `<plugin>/skills/<name>/SKILL.md`; some smaller plugins put it
// at `<plugin>/SKILL.md`. We accept either, preferring the deeper match
// (it's the more "production" shape).
func findSkillMD(pluginDir string) string {
	// Prefer skills/<*>/SKILL.md
	entries, err := os.ReadDir(filepath.Join(pluginDir, "skills"))
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			candidate := filepath.Join(pluginDir, "skills", e.Name(), "SKILL.md")
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}
	// Fallback: plugin-root SKILL.md
	candidate := filepath.Join(pluginDir, "SKILL.md")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// findHarness returns the relative path of the harness binary/script the
// plugin advertises. For the curriculum we use a simple convention: a file
// named `harness` (executable) or `bin/harness` next to the plugin root. A
// real verifier would read this from plugin.json — we keep it convention-based
// so the test data stays tiny.
func findHarness(pluginDir string) string {
	for _, rel := range []string{"harness", "bin/harness"} {
		p := filepath.Join(pluginDir, rel)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// check_skill_md_required_fields enforces the two fields every downstream
// consumer reads: `name` (S002 = error) and `description` (S003 = warn).
// Missing-SKILL.md collapses to S001. We deliberately separate "missing file"
// from "present-but-broken" so a CI dashboard can count them differently.
func check_skill_md_required_fields(pluginDir string) []Issue {
	skillPath := findSkillMD(pluginDir)
	if skillPath == "" {
		return []Issue{{
			Severity: "error",
			Code:     "S001",
			Message:  "SKILL.md missing (looked in <plugin>/SKILL.md and <plugin>/skills/*/SKILL.md)",
			Path:     pluginDir,
		}}
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S001",
			Message:  "SKILL.md unreadable: " + err.Error(),
			Path:     skillPath,
		}}
	}
	skill, err := ParseSkill(data)
	if err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S002",
			Message:  "SKILL.md front-matter parse failed: " + err.Error(),
			Path:     skillPath,
		}}
	}
	var issues []Issue
	if skill.Meta.Name == "" {
		issues = append(issues, Issue{
			Severity: "error",
			Code:     "S002",
			Message:  "SKILL.md missing required field: name",
			Path:     skillPath,
		})
	}
	if skill.Meta.Description == "" {
		issues = append(issues, Issue{
			Severity: "warn",
			Code:     "S003",
			Message:  "SKILL.md missing recommended field: description",
			Path:     skillPath,
		})
	}
	return issues
}

// check_skill_md_triggers validates that if `triggers` is present in the
// front-matter it parses as a YAML list of strings. The s02 parser would
// silently coerce a scalar into [], so we re-unmarshal into a probe type
// that explicitly distinguishes "missing" from "present-with-wrong-shape".
func check_skill_md_triggers(pluginDir string) []Issue {
	skillPath := findSkillMD(pluginDir)
	if skillPath == "" {
		return nil // S001 will already have fired
	}
	data, err := os.ReadFile(skillPath)
	if err != nil {
		return nil
	}
	front, ok := frontMatterBytes(data)
	if !ok {
		return nil
	}
	var probe map[string]yaml.Node
	if err := yaml.Unmarshal(front, &probe); err != nil {
		return nil
	}
	node, present := probe["triggers"]
	if !present {
		return nil
	}
	if node.Kind != yaml.SequenceNode {
		return []Issue{{
			Severity: "error",
			Code:     "S005",
			Message:  "SKILL.md: triggers must be a YAML list of strings",
			Path:     skillPath,
		}}
	}
	for _, child := range node.Content {
		if child.Kind != yaml.ScalarNode {
			return []Issue{{
				Severity: "error",
				Code:     "S005",
				Message:  "SKILL.md: every trigger must be a string",
				Path:     skillPath,
			}}
		}
	}
	return nil
}

// check_harness_has_help runs `<harness> --help` and asserts (a) exit 0 and
// (b) non-empty stdout. The S006 code covers both modes — a harness that
// exits 0 with no help is just as broken as one that exits nonzero.
func check_harness_has_help(pluginDir string, runner Runner) []Issue {
	h := findHarness(pluginDir)
	if h == "" {
		return []Issue{{
			Severity: "error",
			Code:     "S007",
			Message:  "harness binary not found (expected ./harness or ./bin/harness)",
			Path:     pluginDir,
		}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	code, stdout, _, err := runner.Exec(ctx, []string{h, "--help"}, nil)
	if err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S006",
			Message:  "harness --help failed to start: " + err.Error(),
			Path:     h,
		}}
	}
	if code != 0 {
		return []Issue{{
			Severity: "error",
			Code:     "S006",
			Message:  "harness --help exited non-zero",
			Path:     h,
		}}
	}
	if len(stdout) == 0 {
		return []Issue{{
			Severity: "error",
			Code:     "S006",
			Message:  "harness --help produced empty stdout",
			Path:     h,
		}}
	}
	return nil
}

// check_harness_supports_json runs `<harness> --json` (with no other args, so
// it should print help-as-JSON) and asserts the stdout parses into a Result
// envelope that contains an `ok` key. This is THE invariant other chapters
// rely on; if it breaks the plugin can't be driven by an agent.
func check_harness_supports_json(pluginDir string, runner Runner) []Issue {
	h := findHarness(pluginDir)
	if h == "" {
		return nil // S007 fired in the help check
	}
	ctx, cancel := context.WithTimeout(context.Background(), harnessTimeout)
	defer cancel()
	_, stdout, _, err := runner.Exec(ctx, []string{h, "--json"}, nil)
	if err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S004",
			Message:  "harness --json failed to start: " + err.Error(),
			Path:     h,
		}}
	}
	// We accept any object that has an "ok" key — Data/Error are optional.
	// json.Unmarshal into map[string]any is more permissive than the
	// Result struct (which would silently drop unknown fields) and lets us
	// detect the "ok" key explicitly.
	var env map[string]any
	if err := json.Unmarshal(stdout, &env); err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S004",
			Message:  "harness --json output is not valid JSON: " + err.Error(),
			Path:     h,
		}}
	}
	if _, ok := env["ok"]; !ok {
		return []Issue{{
			Severity: "error",
			Code:     "S004",
			Message:  "harness --json output is missing \"ok\" envelope key",
			Path:     h,
		}}
	}
	return nil
}

// check_readme_exists is the cheapest check we have but catches real bugs:
// many published plugins forget the README that hub-search renders as the
// description card. The upstream verify-plugin.sh treats this as required.
func check_readme_exists(pluginDir string) []Issue {
	p := filepath.Join(pluginDir, "README.md")
	if _, err := os.Stat(p); err != nil {
		return []Issue{{
			Severity: "error",
			Code:     "S008",
			Message:  "README.md missing alongside the harness",
			Path:     pluginDir,
		}}
	}
	return nil
}
