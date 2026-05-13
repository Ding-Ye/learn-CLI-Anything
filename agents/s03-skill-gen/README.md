# s03 — skill generator from a CLI

Walks an s01-shaped `CLI` struct and emits a SKILL.md (frontmatter +
markdown body) ready for an agent's skill loader. No reflection, no
runtime tricks — the harness author already declared everything we
need as data.

## Run

```bash
make demo
```

Prints the synthesized SKILL.md for a two-subcommand demo harness
(same shape as s01's `demo`).

## Files

- `cli.go` — `CLI`/`Flag`/`Result` types re-declared from s01.
- `skill.go` — `SkillMeta`/`Skill` types + `ParseSkill`/`RenderSkill`
  re-declared from s02; we round-trip our own output through `ParseSkill`
  in the tests.
- `generator.go` — `GenerateSkill(cli *CLI) Skill`. Tree walk →
  frontmatter + Markdown body. Sorted output, no randomness.
- `main.go` — `skill-gen demo` prints the synthesized SKILL.md.
- `generator_test.go` — 5 tests (round-trip, triggers, flags table,
  empty CLI, determinism).

## Upstream source reading

See `docs/{zh,en}/s03-skill-gen.md` for the annotated walkthrough.
Local offline copy of the upstream Python: `upstream-readings/s03-skill-gen.py`.
