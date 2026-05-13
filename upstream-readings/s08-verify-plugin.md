# Upstream reading — `cli-anything-plugin/verify-plugin.sh`

Source: [HKUDS/CLI-Anything@c5a4b2d](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/verify-plugin.sh)

A 56-line bash script. The whole thing fits on one screen, which is the point:
verification has to be cheap enough that every `git push` runs it.

```bash
#!/usr/bin/env bash
# Verify cli-anything plugin structure

echo "Verifying cli-anything plugin structure..."
echo ""

ERRORS=0

# Check required files
check_file() {
    if [ -f "$1" ]; then
        echo "✓ $1"
    else
        echo "✗ $1 (MISSING)"
        ERRORS=$((ERRORS + 1))
    fi
}

echo "Required files:"
check_file ".claude-plugin/plugin.json"
check_file "README.md"
check_file "LICENSE"
check_file "PUBLISHING.md"
check_file "commands/cli-anything.md"
check_file "commands/refine.md"
check_file "commands/test.md"
check_file "commands/validate.md"
check_file "scripts/setup-cli-anything.sh"

echo ""
echo "Checking plugin.json validity..."
if python3 -c "import json; json.load(open('.claude-plugin/plugin.json'))" 2>/dev/null; then
    echo "✓ plugin.json is valid JSON"
else
    echo "✗ plugin.json is invalid JSON"
    ERRORS=$((ERRORS + 1))
fi

echo ""
echo "Checking script permissions..."
if [ -x "scripts/setup-cli-anything.sh" ]; then
    echo "✓ setup-cli-anything.sh is executable"
else
    echo "✗ setup-cli-anything.sh is not executable"
    ERRORS=$((ERRORS + 1))
fi

echo ""
if [ $ERRORS -eq 0 ]; then
    echo "✓ All checks passed! Plugin is ready."
    exit 0
else
    echo "✗ $ERRORS error(s) found. Please fix before publishing."
    exit 1
fi
```

## What our Go port keeps

- The accumulator pattern (`ERRORS=$((ERRORS+1))` → `[]Issue` append). Verification
  runs every check, then totals at the end — no short-circuit. A plugin author
  fixes five things per round-trip instead of one.
- The exit-code contract: 0 for clean, non-zero for any error. Our `Pass` field
  drives the exit code in `main.go`.
- The "required files" mental model: a flat list of paths, each yielding one
  ✓/✗ line. Our `S001`..`S008` codes map 1:1 to upstream's individual
  `check_file` calls.

## What our Go port changes

- **Structured Issue codes.** Bash prints unstructured strings; we attach a
  stable `Code` field (`S001` etc.) so an outer agent can branch without
  matching message wording.
- **Severity split.** Bash has one notion: error. We add `warn` so missing
  `description` (annoying but tolerable) doesn't fail the build the same way
  a missing `name` does.
- **Smoke tests, not just file presence.** Upstream only verifies that files
  exist + plugin.json parses. We extend with `--help` and `--json` runtime
  checks because "the harness builds" and "the harness behaves" are two
  different bugs in practice.
- **Runner indirection.** Upstream shells out directly. We accept a `Runner`
  interface so the unit tests substitute a `FakeRunner` and we don't have
  to compile a real harness during `go test`.

## What this skips deliberately

- The `python3 -c "import json"` validity check. We don't have plugin.json
  yet in the curriculum; SKILL.md serves the same "structured metadata"
  role. We replicate the pattern (parse front-matter → emit S002 if shape
  is wrong) rather than the exact file.
- The `setup-cli-anything.sh` executable-bit check. Our convention requires
  the binary at `./harness` or `./bin/harness` — if it's not executable, the
  Runner's `Exec` returns a non-zero from `127`/permission-denied and S006
  fires for free.

## Where to read next

- `tests/` directory in upstream — `cli-anything-plugin/tests/` shows the
  pytest fixtures that drive multi-plugin verification runs. Our
  `verify_test.go` adopts that "one happy-path + one counter-example per
  rule" pattern.
- `cli-hub/cli_hub/publish.py` (read in s10) — the publish flow calls
  `verify-plugin.sh` before pushing a manifest update. The same coupling
  applies here: s10 will invoke s08's `Verify()` before regenerating the
  hub index.
