# s02 — SKILL.md parser & renderer

Round-trippable parser+renderer for the `SKILL.md` format CLI-Anything uses
to advertise harnesses to agents. YAML frontmatter (`name`, `description`,
optional `triggers[]`) + opaque Markdown body.

## Run

```bash
make demo
```

## Files

- `skill.go` — `SkillMeta`, `Skill`, `Parse`, `Render`.
- `cli.go` — Dispatch + CLI types, re-declared from s01.
- `main.go` — `skill-md parse <file>` / `skill-md render <file.json>`.
- `skill_test.go` — 5 unit tests (round-trip + the two typed errors).

## Why bytes-equal matters

`Parse(b) → Render → b` only holds if we keep the literal frontmatter
bytes. `yaml.v3` normalizes block-scalar styles (`>-` becomes plain),
which would break generators that re-emit a stored skill.

## Upstream source reading

See `docs/{zh,en}/s02-skill-md.md` for the annotated walkthrough.
