---
title: "Appendix B · Upstream source-reading map"
slug: appendix-b-upstream-map
---

# Appendix B · Upstream source-reading map

A pointer guide to the 257K-LOC upstream so a learner can navigate without getting lost. Where to start, where the load-bearing code lives, and what to skip on the first pass.

## Reading order

1. `README.md` — the agent-native thesis, the 5-minute quickstart, the demo gallery.
2. `cli-anything-plugin/HARNESS.md` — the contract every harness satisfies. Short, dense, the canonical spec.
3. `cli-anything-plugin/QUICKSTART.md` — how to ship a new harness yourself.
4. `cli-anything-plugin/skill_generator.py` — the SKILL.md generator. See how the contract turns into Markdown.
5. `cli-hub/cli_hub/cli.py` → `cli-hub/cli_hub/registry.py` → `cli-hub/cli_hub/installer.py` — the hub's CLI, the registry layer, the install dispatcher. In that order.
6. `anygen/agent-harness/ANYGEN.md` — the recursive case study (anygen wraps the API that generates harnesses).
7. Pick one wrapped CLI by interest (`blender/`, `audacity/`, `gimp/`, etc.) and read its `SKILL.md` + Python entry. The pattern is the same; the domain detail is where the value lives.

## Code reading guide (per chapter)

| Chapter | Upstream files | What to look at |
|---------|----------------|-----------------|
| s01 min-harness | `cli-anything-plugin/HARNESS.md`, `cli-anything-plugin/templates/cli.py.j2` | The contract sections (subcommand tree, --json envelope, exit codes) |
| s02 skill-md | `cli-anything-plugin/skill_generator.py` (top 200 lines) | The YAML frontmatter parser + Markdown body assembler |
| s03 skill-gen | `cli-anything-plugin/skill_generator.py` (Click introspection section) | How decorators are scraped at runtime |
| s04 preview-bundle | `cli-anything-plugin/preview_bundle.py` | The fingerprint function + on-disk cache layout |
| s05 repl-skin | `cli-anything-plugin/repl_skin.py` | The REPL loop + meta-commands + ancestor SKILL.md walk |
| s06 hub-registry | `cli-hub/cli_hub/registry.py` | The HTTP fetch + TTL cache + manifest schema |
| s07 installer | `cli-hub/cli_hub/installer.py` | The backend dispatcher (pip/npm/uv/bundled) |
| s08 verify-plugin | `cli-anything-plugin/verify-plugin.sh`, `cli-anything-plugin/tests/` | The validation checks + the test harness pattern |
| s09 anygen-remote | `anygen/agent-harness/ANYGEN.md`, `anygen/agent-harness/anygen_backend.py` | The submit/poll/result HTTP client |
| s10 publish-flow | `.github/workflows/publish-cli-hub.yml`, `.github/workflows/check-root-skills.yml` | The CI pipeline that regenerates the registry |

## What to skip on the first pass

- All the wrapped-CLI subdirectories (`audacity/`, `blender/`, `chromadb/`, ...). They follow the same pattern as `anygen/`; one example is enough.
- `assets/` — videos and screenshots, useful but not load-bearing.
- The `QGIS/` directory — it's a port-in-progress and the patterns are still settling.
- The `.pi-extension/` directory — it's Pi's deployment integration, downstream from the core.

## Suggested extension exercises

1. **Real Sandboxing for s07.** Right now the installer extracts tarballs straight into the install directory. Wrap the extraction in a `firejail` or `bwrap` invocation that limits filesystem access to just the install dir.
2. **Lockfile concurrency for s07.** Add a `golang.org/x/sys/unix.Flock` around the ledger writes so two parallel `hub install` invocations don't corrupt the file.
3. **SSE for s09.** anygen's upstream supports both poll and SSE. Add a `WaitForResultSSE` that subscribes to a server-sent-event stream instead of polling.
4. **Sign + verify in s10.** Today the pipeline only writes `.sha256` sidecars. Add `cosign`-style signature creation and a verify step that runs at install time in s07.
5. **Fingerprint by file metadata, not content, in s04.** The upstream fingerprints by `(path, size, mtime_ns)` for speed; the Go version hashes content. Swap to the upstream's mode behind a `--fast` flag and measure on a 1 GB input.

## License notes

Upstream is Apache-2.0. The slices under `upstream-readings/` retain Apache-2.0; the Go re-implementations are MIT (see LICENSE). When cherry-picking code from this repo into your own project, the Go files are MIT, the literal upstream copies are Apache-2.0.
