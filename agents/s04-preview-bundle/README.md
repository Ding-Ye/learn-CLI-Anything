# s04 — preview bundle: fingerprint + cache

Content-addressed cache for command outputs. Same inputs + same args =>
same sha256 key => we return the previous result instead of re-running
the command. Mirrors the upstream `cli-anything-plugin/preview_bundle.py`
pattern, trimmed to what an agent harness actually uses.

## Why this matters

Re-rendering a Blender frame, transcribing an audio file, or running any
deterministic-but-expensive command is the bottleneck in an agent loop.
The whole framework is built around "the agent's CLI surface is a pure
function of its inputs" — so a content hash is enough to cache.

## Run

```bash
make demo
```

First call to `preview run -- echo hello` runs the command for real;
second call returns the cached envelope with `cache_hit: true` and
exit code 0 without spawning `echo` again.

## Files

- `cli.go` — `CLI` / `Flag` / `Result` types re-declared from s01.
- `bundle.go` — `Fingerprint`, `Bundle`, `Store` interface, `MemStore`, `DiskStore`.
- `exec.go` — `Run(ctx, cmd, inputs, store) → (bundle, cacheHit, err)`.
- `main.go` — `preview run`, `preview show`, `preview cache-dir` subcommands.
- `bundle_test.go` — 5 tests covering determinism, change detection, round-trip, cache-hit, eviction.

## Try it

```bash
# First run executes echo.
./s04-preview-bundle --json -cache /tmp/c1 run -- echo hello
# {"ok":true,"data":{"cache_hit":false,"exit_code":0,"key":"sha256:...","stderr":"","stdout":"hello\n"}}

# Second run replays from cache. Same key. Same stdout. No echo spawned.
./s04-preview-bundle --json -cache /tmp/c1 run -- echo hello
# {"ok":true,"data":{"cache_hit":true,...}}

# Inspect a cached bundle directly.
./s04-preview-bundle --json -cache /tmp/c1 show sha256:...
```

## Upstream

See `docs/{zh,en}/s04-preview-bundle.md` for the annotated walkthrough
against upstream `cli-anything-plugin/preview_bundle.py`. A 200-line
snapshot of that file lives at `upstream-readings/s04-preview-bundle.py`.
