# s08 — verify-plugin

A validation harness for CLI-Anything plugins. Go port of the upstream
`cli-anything-plugin/verify-plugin.sh`, expanded with:

- structured `Issue` codes (`S001`..`S008`) instead of an `ERRORS=$((ERRORS+1))` accumulator,
- a `Runner` indirection so `--help` / `--json` smoke tests are fakeable in unit tests,
- a JSON envelope output mode an agent can branch on (`{"ok":...,"data":{...report...}}`).

## Run

```bash
make demo           # human-readable report on testdata/plugin-good
make demo-json      # JSON envelope an agent sees
make test           # five FakeRunner-driven scenarios
```

## Check matrix

| code | severity | check                                             |
|------|----------|---------------------------------------------------|
| S000 | error    | plugin path is not a directory                    |
| S001 | error    | SKILL.md missing                                  |
| S002 | error    | SKILL.md front-matter missing required `name`     |
| S003 | warn     | SKILL.md missing recommended `description`        |
| S004 | error    | `harness --json` output lacks an `ok` envelope key|
| S005 | error    | SKILL.md `triggers` must be a YAML list of strings|
| S006 | error    | `harness --help` failed or printed nothing        |
| S007 | error    | harness binary not found                          |
| S008 | error    | README.md missing alongside the harness           |

`Pass` = no error-severity issues. Warnings surface in the report but don't fail it.

## Files

- `cli.go` — re-declared `CLI` / `Flag` / `Result` (no cross-session imports).
- `skill.go` — minimal SKILL.md parser (yaml.v3-based, no required-field check).
- `checks.go` — the `check_*` functions, one per Issue code.
- `verify.go` — `Verify(dir, runner)` orchestrator + human renderer.
- `main.go` — `verify <plugin-dir>` CLI; real `shellRunner` shells out via `/bin/sh -c`.
- `verify_test.go` — five FakeRunner-driven scenarios.
- `testdata/plugin-good/` — happy-path fixture (`SKILL.md` + `harness` stub + `README.md`).
- `testdata/plugin-broken/` — counter-fixture (no name; triggers wrong shape).

## Upstream

See `docs/{zh,en}/s08-verify-plugin.md` for the annotated walkthrough and
`upstream-readings/s08-verify-plugin.md` for the verbatim upstream
`verify-plugin.sh` (it's 56 lines — fits on a screen).
