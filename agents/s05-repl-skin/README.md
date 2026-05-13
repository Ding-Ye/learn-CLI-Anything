# s05 — REPL skin

Interactive shell around any s01-style harness. The Go port of the
upstream `cli-anything-plugin/repl_skin.py`, distilled to the parts that
matter for an LLM agent: line-oriented stdin, `:meta` commands, and a
runtime `:json on|off` toggle that re-uses s01's `Result` envelope.

## Run

```bash
./s05-repl-skin
```

Then type:

```text
> :help
> :skills
> echo hi
> :json on
> echo hi
> time
> :history
> :quit
```

`-skill <path>` overrides the SKILL.md auto-detect; without it the REPL
walks up from CWD looking for one.

## Meta-commands

| command            | effect                                            |
|--------------------|---------------------------------------------------|
| `:help`            | list these meta-commands                          |
| `:skills`          | list subcommands the wrapped harness exposes      |
| `:json on \| off`  | toggle JSON envelope printing for following lines |
| `:history`         | dump the in-memory command history                |
| `:quit`            | exit (aliases: `:exit`, `:q`)                     |

## Files

- `cli.go` — re-declared `CLI` / `Flag` / `Result` + `Dispatch` widened to `io.Writer`.
- `skill.go` — minimal `SkillMeta` parser (no YAML lib).
- `repl.go` — the `REPL` struct + `Run()` loop + meta-commands.
- `main.go` — entrypoint that wires the s01 demo harness into a REPL.
- `repl_test.go` — 5 scripted-stdin tests.

## Upstream

See `docs/{zh,en}/s05-repl-skin.md` for the annotated walkthrough and
`upstream-readings/s05-repl-skin.py` for the first 250 lines of the
upstream `repl_skin.py`.
