# s10 — Publish flow

CI-driven publishing pipeline: walk a directory of plugins, validate
each one's `SKILL.md`, bundle each into a reproducible `.tar.gz`,
compute a `.sha256` sidecar, and emit a fresh `registry.json` that
points at the artifacts.

The Go port of the *shape* of the upstream's
[`publish-cli-hub.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/publish-cli-hub.yml)
and [`check-root-skills.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/check-root-skills.yml)
workflows, plus the `cli-hub/cli_hub/` packaging surface — boiled down
to what runs *before* anything gets pushed.

## Pipeline

```text
ScanPlugins(src)   walk src/ one level deep, find every SKILL.md-bearing dir
Validate           every plugin must have a non-empty SKILL.md
Bundle             tar.gz each plugin dir → out/<name>-<version>.tar.gz
Sign               sha256 each artifact → out/<artifact>.sha256
UpdateIndex        emit out/registry.json with one entry per plugin
```

Each step returns a `StepReport`; `Run(ctx, src, out)` aggregates them
into a `PipelineReport`. Tests drive each step in isolation so a CI
failure surfaces with a precise blame line.

## Run

```bash
make demo         # full pipeline against testdata/, human output
make demo-json    # same, but JSON-enveloped (what CI logs)
make status       # read out/registry.json from a prior demo
make test
```

Or by hand:

```bash
./s10-publish-flow run <src-dir> <out-dir>
./s10-publish-flow status <out-dir>
./s10-publish-flow --json run <src-dir> <out-dir>
```

## Properties

- **Idempotent.** Every tar header carries a fixed mtime, every digest
  is content-addressed. Re-running with the same input produces bit-
  identical bytes.
- **Hermetic.** No network. The publisher prepares a release directory;
  pushing it (rsync, gh-pages, PyPI) is left to whatever job follows.
- **Zero-dep.** `go.mod` has nothing but the stdlib.

## Files

- `cli.go` — re-declared `CLI` / `Flag` / `Result` + `Dispatch` (same shape as s05).
- `manifest.go` — re-declared `Manifest` + a minimal `readSkillFront` SKILL.md sniffer.
- `publish.go` — the `Pipeline` struct, five step methods, `Run`, `writeTarGz`, `sha256File`.
- `main.go` — entrypoint wiring `publish run` and `publish status`.
- `publish_test.go` — five tests using a `t.TempDir()` two-plugin fixture.
- `testdata/plugin-good/SKILL.md` — sample plugin `make demo` operates on.

## Upstream

See `docs/{zh,en}/s10-publish-flow.md` for the annotated walkthrough and
`upstream-readings/s10-publish-flow.yml` for the first 200 lines of the
upstream workflow files.
