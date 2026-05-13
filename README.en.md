# learn-CLI-Anything

> Re-grow the core harness pattern of [HKUDS/CLI-Anything](https://github.com/HKUDS/CLI-Anything) from scratch in Go — one mechanism per chapter, each ending with an annotated upstream-source reading. Pedagogy inspired by [shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code).

中文版本: [README.md](./README.md).

## What

**CLI-Anything** is HKUDS's "make all software agent-native" framework — it wraps GUI/SDK tooling into a uniform CLI + SKILL.md surface that LLM agents can invoke like function calls. The upstream is 257K LOC, but 95% of that lives in 60+ wrapped-CLI subdirectories (`blender/`, `audacity/`, `anygen/`, etc.) that all share the same harness pattern. The actual core framework (`cli-anything-plugin/` + `cli-hub/`) is only ~6K LOC.

This repo: **re-grow that 6K-LOC core harness pattern in Go**, chapter by chapter — HARNESS contract, SKILL.md parser, skill generator, preview bundles, REPL skin, CLI-Hub registry, installer, validation, a remote-API case study (`anygen`), and the publish flow.

Each chapter is ≤ 1000 lines of Go and is its own Go module (`agents/sNN-*/`, no cross-imports).

## Curriculum

| #     | Chapter                                       | Status |
|-------|-----------------------------------------------|--------|
| s01   | Minimum harness: CLI + JSON output            | ✅     |
| s02   | SKILL.md parser & renderer                    | ✅     |
| s03   | Skill generator from a CLI                    | ✅     |
| s04   | Preview bundles & cache                       | ✅     |
| s05   | REPL skin: interactive harness                | ✅     |
| s06   | CLI-Hub registry                              | ⏳     |
| s07   | Multi-backend installer                       | ⏳     |
| s08   | Plugin verification & test stub               | ⏳     |
| s09   | anygen — remote-API harness case study        | ⏳     |
| s10   | Publish flow: CI + registry sync              | ⏳     |
| s_full| End-to-end integration trace                  | ⏳     |
| App A | Appendix A · Why CLIs are right for agents    | ⏳     |
| App B | Appendix B · Upstream source-reading map      | ⏳     |

## Quickstart

```bash
git clone https://github.com/Ding-Ye/learn-CLI-Anything
cd learn-CLI-Anything
go work sync

cd agents/s01-min-harness
make demo        # human output
make demo-json   # JSON envelope an agent sees
```

Requires Go 1.22+.

## Acknowledgements

- Upstream: [HKUDS/CLI-Anything](https://github.com/HKUDS/CLI-Anything) (Apache-2.0).
- Pedagogy: [shareAI-lab/learn-claude-code](https://github.com/shareAI-lab/learn-claude-code).
- Generator: Anthropic's `learn-repo-generator` skill.

## License

MIT — see [LICENSE](./LICENSE).
