---
title: "s03 · Skill generator from a CLI"
chapter: 03
slug: s03-skill-gen
est_read_min: 8
---

# s03 · Skill generator from a CLI

> What this teaches: how to walk a CLI tree (s01's struct) and synthesize a SKILL.md (s02's format) — frontmatter, synopsis, subcommand table, flag tables, usage examples — all from data the harness author already wrote down.

## Problem

s02 gave us a `Skill` (frontmatter + body) we can parse and render. But who writes the SKILL.md in the first place? In the upstream CLI-Anything, every wrapped GUI tool ships a hand-edited skill file, and they all look 90% the same — same H1, same subcommands table, same triggers list. That's busywork the harness author shouldn't have to do, and worse, the hand-edited file drifts the moment someone adds a subcommand.

The upstream solves this with `cli-anything-plugin/skill_generator.py`: scrape `@click.group(...)` / `@click.command(...)` decorators out of the CLI source, extract docstrings via regex, and stamp a SKILL.md template. It works because Python carries metadata as source-level decorators. In Go we made a deliberately different bet in s01 — `CLI` is a struct literal, not a decorator chain — so the generator becomes a pure tree walk with zero AST work.

## Solution

`GenerateSkill(cli *CLI) Skill` does three things:

1. **Build frontmatter from leaves of the tree.** `meta.Name = cli.Name`, `meta.Description = firstSentence(cli.Help)`, and `meta.Triggers` is a sorted/deduped list of `<sub>` and `<sub> <root>` phrases. The dual form matters: agents whose skill matcher does fuzzy keyword scoring need both "echo" and "echo demo" to match different prompt shapes.
2. **Synthesize a Markdown body.** H1 = name, then Synopsis, Subcommands table, root Flags table (if any), Usage section with one `### subcommand` block per child — each block has the child's help, its own flags table (sorted by flag name), and a copy-pasteable `bash` example with required flags filled in by type placeholder.
3. **Sort everything.** Subcommand iteration goes through `sortedKeys`, flag tables get a stable `sort.Slice`, triggers go through `sort.Strings` after deduping. Map iteration in Go is randomized, so without these sorts two runs of the generator produce different bytes — fatal for diffing or for caching the rendered SKILL.md.

The hand-off to s02 is then trivial: `RenderSkill(GenerateSkill(cli))` is a complete pipeline.

## How It Works

```text
*CLI ──▶ GenerateSkill
            │
            ├─▶ firstSentence(Help) ──▶ Meta.Description
            ├─▶ deriveTriggers       ──▶ Meta.Triggers (sorted, deduped)
            └─▶ synthesizeBody       ──▶ Body
                    │
                    ├─ # name / help
                    ├─ ## Synopsis
                    ├─ ## Subcommands (sorted table)
                    ├─ ## Flags (root, sorted)
                    └─ ## Usage
                           └─ per subcommand:
                                ### name sub / help / flags table / bash sample
            │
            ▼
        Skill{Meta, Body}
```

Three non-obvious points:

1. **`firstSentence` is the trigger-line heuristic.** A SKILL.md's `description` field is what the agent reads to decide whether to load the skill at all — it must fit on one line. We cut at the first `.`, `!`, `?`, or newline. The upstream Python uses the same idea by slicing the first 100 chars of the README intro; we cut on sentence boundary instead because Go strings are UTF-8 and a fixed byte cut can split a rune.
2. **Required-flag placeholders mirror the type.** For a `Required: true` flag of type `string`, the bash example renders `--name <string>`. An agent's planner sees that and knows it can't omit the flag — combined with the Required column it has two redundant signals, which is intentional.
3. **No section appears unless it has content.** An empty CLI (no subcommands, no flags) renders only the H1 + help + Synopsis. The empty-CLI test pins this down; the upstream behavior is the same — `command_groups=[]` skips the entire Commands section.

The core of `GenerateSkill` is small enough to quote:

```go
func GenerateSkill(cli *CLI) Skill {
    meta := SkillMeta{
        Name:        cli.Name,
        Description: firstSentence(cli.Help),
        Triggers:    deriveTriggers(cli),
    }
    body := synthesizeBody(cli)
    return Skill{Meta: meta, Body: body}
}
```

Everything else is rendering details. The body builder is ~80 LOC of `fmt.Fprintf` against a `strings.Builder` — the kind of code that wants no abstraction, just sorted iteration and stable formatting.

## What Changed (vs. s02)

s02 gave us `ParseSkill`/`RenderSkill` and the `Skill` model — but a `Skill` value still had to come from somewhere. s03 makes that "somewhere" automatic for any harness already written in the s01 shape. We add ~200 LOC of `generator.go`, re-declare s01's `CLI`/`Flag` and s02's `Skill`/`SkillMeta` (no cross-module imports — house rule), and end up with a one-call pipeline: `RenderSkill(GenerateSkill(cli))`.

The trade-off vs. upstream: the Python generator can scrape ANY Click-based CLI, even ones whose author never thought about generation. Our generator needs the harness to expose its metadata as a `CLI` struct. In exchange we get zero reflection, byte-stable output, and a `go vet`-clean compile-time check that the metadata even exists.

## Try It

```bash
cd agents/s03-skill-gen

make test    # 5 tests: round-trip, triggers, flags table, empty CLI, determinism
make demo    # prints synthesized SKILL.md for the built-in demo harness
```

Expected `make demo` output begins with:

```yaml
---
name: demo
description: 'Demo harness: time + echo subcommands.'
triggers:
  - echo
  - echo demo
  - time
  - time demo
---
# demo
...
```

Pipe `make demo > SKILL.md` and you have a file the s02 parser will accept verbatim.

## Upstream Source Reading

Read [`cli-anything-plugin/skill_generator.py:1-200`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/skill_generator.py) and compare with `agents/s03-skill-gen/generator.go`. The bits that map cleanly:

- `extract_cli_metadata` ↔ `GenerateSkill` — same role: harness → `SkillMetadata` / `Skill`.
- `extract_intro_from_readme` ↔ `firstSentence` — both trim help text down to a triggering one-liner.
- `extract_commands_from_cli` (regex over `@click.group` / `@click.command`) ↔ `synthesizeBody`'s `sortedKeys` walk over `cli.Subcommands`. The Python version has to handle decorator stacking and multi-line docstrings; the Go version sidesteps that entirely because the metadata is already structured.
- `CommandGroup` / `CommandInfo` dataclasses ↔ Go's anonymous structs inside the `*CLI` tree.

Local offline excerpt in [`upstream-readings/s03-skill-gen.py`](../../upstream-readings/s03-skill-gen.py).
