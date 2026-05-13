// verify.go — orchestration. Verify() walks every check in a fixed order
// and collects their Issues into one Report. The order is documentation as
// much as logic: structural checks fire first (does the file exist?), then
// content checks (is its YAML valid?), then runtime smoke (does the
// harness actually run?). A reader of the output gets a story from "shape"
// to "behavior".
//
// We do NOT short-circuit on errors. Every check that can run, does. This
// matches the upstream's `ERRORS=$((ERRORS+1))` accumulator pattern, and
// produces a more useful report for the plugin author — they fix five
// things in one round-trip instead of one per push.
package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// Report is what Verify hands back. Pass = "no error-severity Issues".
// Warnings can fire freely without affecting Pass; the CI script chooses
// whether to fail on warnings by inspecting Issues itself.
type Report struct {
	Plugin string  `json:"plugin"`
	Issues []Issue `json:"issues"`
	Pass   bool    `json:"pass"`
}

// Verify is the top-level entrypoint. It returns a Report even on a
// "plugin directory doesn't exist" error so the caller can render that
// uniformly with the rest of the issues; the error return is reserved for
// truly unrecoverable cases (e.g. permission errors stat'ing the dir).
//
// Determinism note: Issues are sorted by Code for stable output. We don't
// want test flakes from check_*'s discovery order changing.
func Verify(pluginDir string, runner Runner) (*Report, error) {
	rep := &Report{Plugin: pluginDir}

	st, err := os.Stat(pluginDir)
	if err != nil {
		return nil, fmt.Errorf("verify: stat plugin dir: %w", err)
	}
	if !st.IsDir() {
		rep.Issues = append(rep.Issues, Issue{
			Severity: "error",
			Code:     "S000",
			Message:  "plugin path is not a directory",
			Path:     pluginDir,
		})
		rep.Pass = false
		return rep, nil
	}

	// The order matters: a missing SKILL.md (S001) causes most others to
	// short-circuit internally with "skip if S001 fired". Running them in
	// order keeps the report readable.
	rep.Issues = append(rep.Issues, check_skill_md_required_fields(pluginDir)...)
	rep.Issues = append(rep.Issues, check_skill_md_triggers(pluginDir)...)
	rep.Issues = append(rep.Issues, check_readme_exists(pluginDir)...)
	rep.Issues = append(rep.Issues, check_harness_has_help(pluginDir, runner)...)
	rep.Issues = append(rep.Issues, check_harness_supports_json(pluginDir, runner)...)

	sort.SliceStable(rep.Issues, func(i, j int) bool {
		return rep.Issues[i].Code < rep.Issues[j].Code
	})

	rep.Pass = !hasErrors(rep.Issues)
	return rep, nil
}

// hasErrors is the Pass predicate.
func hasErrors(issues []Issue) bool {
	for _, x := range issues {
		if x.Severity == "error" {
			return true
		}
	}
	return false
}

// RenderHuman formats a Report for a terminal. We mirror the upstream
// `verify-plugin.sh` shape (✓/✗ lines) on purpose — readers who know that
// script will recognize the output. JSON callers should use the Result
// envelope from Dispatch instead.
func RenderHuman(rep *Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Verifying plugin: %s\n\n", rep.Plugin)
	if len(rep.Issues) == 0 {
		fmt.Fprintln(&b, "All checks passed.")
		return b.String()
	}
	for _, x := range rep.Issues {
		mark := "x"
		if x.Severity == "warn" {
			mark = "!"
		}
		fmt.Fprintf(&b, "  [%s] %s %s — %s", mark, x.Code, x.Severity, x.Message)
		if x.Path != "" {
			fmt.Fprintf(&b, " (%s)", x.Path)
		}
		b.WriteByte('\n')
	}
	fmt.Fprintln(&b)
	if rep.Pass {
		fmt.Fprintln(&b, "Pass (warnings only)")
	} else {
		fmt.Fprintln(&b, "FAIL")
	}
	return b.String()
}
