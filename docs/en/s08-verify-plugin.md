---
title: "s08 · Plugin verification & test stub"
chapter: 08
slug: s08-verify-plugin
est_read_min: 7
---

# s08 · Plugin verification & test stub

> What this teaches: how to turn an ad-hoc bash "are all the right files here?" script into a structured, codified validation harness — with stable `Issue` codes, an injectable `Runner` for smoke-testing the wrapped harness, and a JSON envelope an agent (or CI) can branch on.

## Problem

Every CLI-Anything plugin author hits the same wall: their first push to the hub silently fails because (a) `SKILL.md` is missing a `description`, or (b) the harness blows up on `--json`, or (c) they forgot `README.md`. The upstream answer is `cli-anything-plugin/verify-plugin.sh` — a 56-line bash script that ticks off a list of required files and exits non-zero if anything's amiss. It works, but it has three limits we hit immediately when scaling past one plugin:

1. **Unstructured output.** Agents that consume the report can only string-match on "MISSING". There's no stable code to branch on.
2. **File-existence only.** Upstream verifies *shape*. It never asks the harness "do you actually run? do you actually emit a JSON envelope?". A plugin can pass `verify-plugin.sh` and still be unusable.
3. **Hard to unit-test.** Bash shells out directly; testing the verifier means dropping a real binary on disk. Our Go port wants a `Runner` interface so the test substitutes a fake.

## Solution

A flat list of `check_*` functions, each returning `[]Issue`, accumulated by `Verify(dir, runner)` into a `Report{Issues, Pass}`. Each Issue carries a `Severity` (`error` or `warn`) and a stable `Code` (`S001`..`S008`).

```go
type Issue struct {
    Severity string `json:"severity"` // "error" | "warn"
    Code     string `json:"code"`     // e.g. "S001"
    Message  string `json:"message"`
    Path     string `json:"path,omitempty"`
}

type Runner interface {
    Exec(ctx context.Context, args []string, stdin []byte) (exitCode int, stdout, stderr []byte, err error)
}

type Report struct {
    Plugin string  `json:"plugin"`
    Issues []Issue `json:"issues"`
    Pass   bool    `json:"pass"`
}
```

The checks split into two families: filesystem checks (S001, S002, S003, S005, S007, S008) and runtime smoke tests (S004, S006). Both run through the same orchestrator; the `Runner` interface is the only seam.

Three decisions worth calling out:

1. **No short-circuiting.** Every check that can run, runs. A plugin author sees all five problems in one pass instead of one per push — the same accumulator pattern the upstream `ERRORS=$((ERRORS+1))` enforces.
2. **`warn` ≠ `error`.** Missing `description` is annoying, not fatal. Missing `name` is fatal — every downstream consumer keys off it. `Pass` is "no error-severity issues"; warnings surface but don't fail.
3. **Runner is an interface, not `exec.Cmd`.** The shell-out lives in `shellRunner` in `main.go`; the tests wire a scripted `FakeRunner` instead. This is the same pattern as `httptest` for s06's hub fetcher.

## How It Works

```text
Verify(pluginDir, runner)
    │
    ├── stat(pluginDir)           ── error → S000
    │
    ├── check_skill_md_required_fields
    │       └── findSkillMD → ParseSkill → assert Name, warn on missing Description
    │
    ├── check_skill_md_triggers
    │       └── re-unmarshal frontmatter into yaml.Node, assert Sequence shape (S005)
    │
    ├── check_readme_exists
    │       └── stat(<dir>/README.md) (S008)
    │
    ├── check_harness_has_help
    │       └── runner.Exec("<harness> --help", ...) → assert exit 0 + non-empty (S006/S007)
    │
    └── check_harness_supports_json
            └── runner.Exec("<harness> --json", ...) → json.Unmarshal → assert "ok" key (S004)

    ↓
   sort issues by Code → Pass = (no error-severity issues)
```

The orchestrator from `verify.go`:

```go
func Verify(pluginDir string, runner Runner) (*Report, error) {
    rep := &Report{Plugin: pluginDir}
    // ... stat checks ...
    rep.Issues = append(rep.Issues, check_skill_md_required_fields(pluginDir)...)
    rep.Issues = append(rep.Issues, check_skill_md_triggers(pluginDir)...)
    rep.Issues = append(rep.Issues, check_readme_exists(pluginDir)...)
    rep.Issues = append(rep.Issues, check_harness_has_help(pluginDir, runner)...)
    rep.Issues = append(rep.Issues, check_harness_supports_json(pluginDir, runner)...)
    sort.SliceStable(rep.Issues, func(i, j int) bool { return rep.Issues[i].Code < rep.Issues[j].Code })
    rep.Pass = !hasErrors(rep.Issues)
    return rep, nil
}
```

Three non-obvious points:

1. **The `Runner` is taken by interface, not type.** This is what makes `verify_test.go` cheap: a 30-line `FakeRunner` with a `map[string]canned` lets us cover all five scenarios without compiling a real plugin. Production `shellRunner` is 25 lines of `exec.CommandContext("/bin/sh", "-c", ...)`; tests never touch it.
2. **`check_skill_md_triggers` re-parses the front-matter.** Our `ParseSkill` returns a `SkillMeta` whose `Triggers` field is `[]string`, so YAML's coercion would silently turn `triggers: foo` into `[]string{}` and lose the bug. We unmarshal again into `map[string]yaml.Node` and assert `Kind == SequenceNode` — exactly the kind of structural check the upstream's `python3 -c "import json"` validity hop performs for plugin.json.
3. **Issues are sorted by Code before returning.** Discovery order isn't deterministic when files come back from `os.ReadDir` — sorting keeps test assertions stable and the human renderer's output diff-able across runs.

## What Changed (vs. s07)

s07 was about *getting* a plugin onto disk (installer dispatch). s08 is about *validating* the result. Three concrete deltas from the rest of the curriculum:

- **New top-level type: `Report`.** s01..s07 returned domain data or errors. s08 returns a *structured set of findings* — closer to a linter than a CLI. The `Issue` shape is what an outer agent reads to decide "publish" vs "block".
- **`Runner` interface, new for s08.** Earlier chapters either ran code in-process (`Dispatch`) or shelled out without abstraction (s07's installer). The verifier is the first time we need *both* — production shells out, tests use a fake — so the seam earns its keep.
- **`SKILL.md` parser re-declared with weaker invariants.** s02's `Parse` errors on missing `name`; s08's `ParseSkill` returns the struct anyway so `check_skill_md_required_fields` can emit a structured `S002` instead of a Go error. Same input, different consumer.

## Try It

```bash
cd agents/s08-verify-plugin
make demo            # human-readable report on testdata/plugin-good
make demo-json       # JSON envelope an agent sees
make test            # 5 FakeRunner-driven scenarios
```

Expected demo output:

```text
{
  "plugin": "./testdata/plugin-good",
  "issues": null,
  "pass": true
}
```

And the JSON envelope:

```json
{"ok":true,"data":{"plugin":"./testdata/plugin-good","issues":null,"pass":true}}
```

Try corrupting the fixture — delete `testdata/plugin-good/README.md`, re-run, and watch `S008` fire while everything else stays green.

## Upstream Source Reading

Read [`cli-anything-plugin/verify-plugin.sh`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/verify-plugin.sh) (56 lines) alongside our `verify.go` + `checks.go` (~300 lines combined). Focus areas:

- **`check_file()` (bash lines 10-17)** — the helper that emits `✓` / `✗` and bumps `ERRORS`. Our `Issue{Severity:"error", Code:"S00X"}` is the typed equivalent; same accumulator, more structure.
- **`Required files:` block (bash lines 19-29)** — a hard-coded list of paths. Our `check_skill_md_required_fields` + `check_readme_exists` cover the curriculum-relevant subset; a production verifier would add `LICENSE`, `PUBLISHING.md`, and the `commands/*.md` paths.
- **`Checking plugin.json validity` block (bash lines 31-37)** — shells out to `python3 -c` for JSON-parse validation. We do the analogous thing for YAML front-matter (yaml.v3) and triggers shape (re-unmarshal into `yaml.Node`).
- **`Checking script permissions` block (bash lines 39-46)** — the executable-bit check on `setup-cli-anything.sh`. We don't replicate it explicitly; if `harness` isn't executable, the `Runner` call returns `127`/error and `S006` catches the failure for free.

A close offline copy is in [`upstream-readings/s08-verify-plugin.md`](../../upstream-readings/s08-verify-plugin.md) — both the verbatim source and a side-by-side of what we kept, changed, and skipped.

The upstream also has a `cli-anything-plugin/tests/` directory whose pytest fixtures drive multi-plugin verification runs. Our `verify_test.go` adopts the same "one happy-path + one counter-example per rule" pattern, just in Go.
