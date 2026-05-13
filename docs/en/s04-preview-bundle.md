---
title: "s04 · Preview bundles & cache"
chapter: 04
slug: s04-preview-bundle
est_read_min: 7
---

# s04 · Preview bundles & cache

> What this teaches: how a content-addressed cache turns "agent re-runs the same expensive command 20 times in a row" into "agent runs it once and replays the result 19 times" — using nothing but a sha256 of (inputs, args).

## Problem

Agent loops are repetitive. When an agent is iterating on a Blender render, an audio transcription, a Pandoc conversion, or any deterministic-but-expensive command, it tends to re-issue the same call with the same inputs as it explores nearby parameters or just re-checks state. Every re-issue burns wall-clock, GPU time, or API quota. The upstream `cli-anything-plugin/preview_bundle.py` solves this with a *preview bundle*: hash the inputs + the command, store the result on disk, replay on a second hit. Same idea as Bazel's action cache or Nix's hash-of-inputs derivations, but trimmed to the single-bundle case an agent harness actually needs.

## Solution

A `Bundle` is `{Key, CreatedAt, CmdArgs, Files, Stdout, Stderr, ExitCode}`. The `Key` is the canonical-JSON sha256 of `(inputs, cmdArgs)`. Two `Store` implementations: `MemStore` (LRU-by-insertion, bounded capacity) for tests and short-lived processes, and `DiskStore` (one JSON file per bundle, written via tmp+rename for atomicity) for the persistent cache at `~/.cache/learn-cli-anything-s04/`. `Run(ctx, cmd, inputs, store)` is the single entry point: it computes the key, checks the store, and either replays or executes — returning a `(*Bundle, cacheHit bool, error)` triple so the caller can tell which path was taken.

Three design decisions:

1. **Hash the content, not the path.** Upstream's `fingerprint_file` hashes `{path, size, mtime_ns}` — fast, but it makes the cache invalidate on a no-op `touch` and miss after a copy. We hash the bytes. Slower for big files, but correct, and an agent that's iterating on a 4 KB SVG never notices.
2. **Sort map keys explicitly when canonicalizing.** Go's `encoding/json` happens to sort map keys (as of 1.12), but a content hash should not depend on stdlib internals. We sort, then build a struct, then marshal — three steps, but the hash is provably stable.
3. **Cache even non-zero exits.** A deterministic failure is still cacheable. If `convert: no decode delegate` fires once it'll fire again; spending another exec to re-confirm is waste. Upstream calls these `status: "partial"` or `"error"` manifests for the same reason.

## How It Works

```text
Run(ctx, cmd, inputs, store)
    │
    ├── key = Fingerprint(inputs, cmd)        ── sha256(canonical-JSON)
    │
    ├── store.Get(key) ──▶ hit?
    │       │
    │       ├── yes ──▶ return (bundle, true, nil)        ◀── cache hit
    │       │
    │       └── no  ──▶ mkdtemp + write inputs/
    │                       │
    │                       ├── exec.CommandContext(ctx, cmd)
    │                       │       ├── stdout → bundle.Stdout
    │                       │       ├── stderr → bundle.Stderr
    │                       │       └── exit  → bundle.ExitCode
    │                       │
    │                       └── store.Put(bundle)
    │                                │
    └─────────────────────────────── return (bundle, false, nil)
```

The fingerprint (~15 LOC from `agents/s04-preview-bundle/bundle.go`):

```go
func Fingerprint(inputs map[string][]byte, cmdArgs []string) string {
    names := make([]string, 0, len(inputs))
    for k := range inputs { names = append(names, k) }
    sort.Strings(names)
    type entry struct { Name, Hash string }
    entries := make([]entry, len(names))
    for i, n := range names {
        sum := sha256.Sum256(inputs[n])
        entries[i] = entry{Name: n, Hash: hex.EncodeToString(sum[:])}
    }
    canon := struct {
        Inputs []entry  `json:"inputs"`
        Cmd    []string `json:"cmd"`
    }{Inputs: entries, Cmd: cmdArgs}
    buf, _ := json.Marshal(canon)
    sum := sha256.Sum256(buf)
    return "sha256:" + hex.EncodeToString(sum[:])
}
```

Three non-obvious points:

1. **Tempdir + `LEARN_S04_INPUT_DIR`, not argv injection.** When `Run` materializes inputs to disk for the child process, the tempdir path goes through an env var. If we inserted it into `cmdArgs` instead, the cache key would change every run (new tempdir each time) and we'd cache-miss every time.
2. **Atomic write via tmp+rename.** `DiskStore.Put` writes `<key>.json.tmp` then renames over `<key>.json`. POSIX `rename` is atomic on the same filesystem — readers see either the old file or the new one, never a half-written blob. Upstream uses the same pattern in `write_json`.
3. **Defensive copy of inputs.** `copyInputs` clones the bytes before storing. A caller that mutates its `map[string][]byte` after `Run` cannot retroactively poison the cache.

## What Changed (vs. s01)

s01 was the bare CLI dispatch. s04 keeps the same `CLI` / `Result` envelope (re-declared in this module — every chapter is self-contained), and adds a side: the `Run` handlers don't *do* the work, they hand `(cmd, inputs)` to the bundle layer. From the agent's perspective the envelope is identical to s01's; the cache is invisible. That's the point: an agent that learned to read s01's JSON automatically benefits from s04's cache when an upstream harness opts into the bundle pattern.

## Try It

```bash
cd agents/s04-preview-bundle
make demo
```

Expected output (truncated):

```text
--- first run (expect cache_hit=false) ---
{"ok":true,"data":{"cache_hit":false,"exit_code":0,"key":"sha256:...","stderr":"","stdout":"hello\n"}}
--- second run (expect cache_hit=true) ---
{"ok":true,"data":{"cache_hit":true,"exit_code":0,"key":"sha256:...","stderr":"","stdout":"hello\n"}}
--- cache dir contents ---
<sha256-hex>.json
```

Two runs of `echo hello`, same args, same (empty) inputs → same key. The second run never spawns `echo`; it reads the JSON blob and returns the recorded stdout.

You can also inspect a cached bundle directly:

```bash
./s04-preview-bundle --json -cache /tmp/c1 show sha256:<hex>
```

## Upstream Source Reading

Read [`cli-anything-plugin/preview_bundle.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/preview_bundle.py) and contrast with `agents/s04-preview-bundle/bundle.go` + `exec.go`. The sections to focus on:

- **`hash_data` / `fingerprint_data`** — same canonical-JSON sha256. The upstream `_json_dumps` uses `sort_keys=True` which is the Python equivalent of what we do explicitly.
- **`build_cache_key`** — upstream's key includes `(software, recipe, bundle_kind, source_fingerprint, options, harness_version, protocol_version)`. We collapse to `(inputs, cmdArgs)` because the agent flow at the s04 level doesn't yet need versioning; s10 will revisit.
- **`prepare_bundle` / `find_cached_manifest`** — upstream walks a directory tree of `manifest.json` files. We keep one file per bundle keyed by hex-of-sha256: the directory IS the index. Trade-off: we lose per-bundle metadata files but gain O(1) lookups.

Local offline snapshot in [`upstream-readings/s04-preview-bundle.py`](../../upstream-readings/s04-preview-bundle.py).
