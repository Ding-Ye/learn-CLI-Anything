---
title: "s06 · CLI-Hub registry"
chapter: 06
slug: s06-hub-registry
est_read_min: 7
---

# s06 · CLI-Hub registry

> What this teaches: how to model a remote registry as JSON, fetch it over HTTP, stash it on disk with a TTL, and expose `hub search/list/info` over the cached `Index` — the bones of the upstream `cli-hub/cli_hub/registry.py` in ~250 lines of Go.

## Problem

An agent that wants to install a wrapped CLI needs to discover what exists: names, versions, install backends. The upstream answers this with a single canonical `registry.json` hosted on GitHub Pages — every `cli-hub install <name>` first reads that file. But you can't hit the network on every invocation (latency, offline use, GitHub rate-limit), and you can't ship a stale snapshot in the binary (the registry grows). What you need is a fetch-once-cache-locally pattern with a TTL window: fresh enough for daily use, fast enough for back-to-back `hub list && hub info <x>`.

## Solution

Three composable pieces:

1. **`Source` interface** — anything that produces an `Index`. Concrete implementations: `HTTPSource{URL}` and `FileSource{Path}`. The demo uses `FileSource` so it works offline; the test suite uses `httptest.Server` against `HTTPSource`.
2. **`Cache` wrapper** — also implements `Source`, so it stacks: `Cache{Source: HTTPSource{...}, Path: ..., TTL: 1h}`. On `FetchIndex`, it reads the on-disk envelope, and if `time.Since(cachedAt) < TTL` returns that data without touching the inner Source. On expiry (or first run), it calls the inner Source and rewrites the cache file. On network error with a cache present, it returns stale-but-readable data — that's deliberate, matching the upstream's `try/except → cached_data` fallback.
3. **`Hub` facade** — wraps a decoded `Index` and exposes `Search(query) []Manifest`, `List() []Manifest`, `Info(name) (*Manifest, error)`. Free-function shape was tempting but the facade lets us add `Reload()` later without touching call sites.

Three design decisions worth calling out:

1. **`Source` is an interface, not a function pointer.** The upstream collapses fetch-from-URL and fetch-from-file into one function parameterized by URL. That's fine in Python but loses type-safety in Go. An interface lets `Cache` wrap *anything* Source-shaped; the test suite's `countingSource` is one such wrapper.
2. **`Cache` returns stale data on network failure.** Same as the upstream, and the right tradeoff for `hub list`: an offline agent should still see *what was last available*, not a hard error. New users get an error only on the first run before any cache exists.
3. **`Manifest` mirrors the upstream JSON shape, not a Go-idiomatic struct.** Field names like `Backend` (one of `pip | npm | bundled | uv`) feed directly into s07's installer dispatch. The wire format stays byte-compatible with the upstream's `registry.json` so a future migration can read either side.

## How It Works

```text
buildSource ──▶ ┌────────────┐         ┌───────────────┐
                │ HTTPSource │ or      │  FileSource   │
                └────────────┘         └───────────────┘
                       │                     │
                       └─────────┬───────────┘
                                 ▼
                          ┌────────────┐
                          │   Cache    │  (only wrap HTTP)
                          │  TTL=1h    │
                          └────────────┘
                                 │
                            FetchIndex ──▶ Index{Updated, Manifests[]}
                                 │
                                 ▼
                          ┌────────────┐
                          │    Hub     │
                          └────────────┘
                              │  │  │
                  Search ─────┘  │  └───── Info(name) → *Manifest
                                List() → []Manifest
                                 │
                              Dispatch ──▶ stdout (JSON or pretty)
```

The 30-line core of `Cache.FetchIndex` from `agents/s06-hub-registry/registry.go`:

```go
func (c *Cache) FetchIndex(ctx context.Context) (Index, error) {
    c.mu.Lock()
    defer c.mu.Unlock()

    cached, hasCached, _ := c.readCache()
    if hasCached && c.TTL > 0 {
        if c.now().Sub(cached.CachedAt) < c.TTL {
            return cached.Data, nil
        }
    }

    idx, err := c.Source.FetchIndex(ctx)
    if err != nil {
        if hasCached {
            return cached.Data, nil // stale beats hard-fail
        }
        return Index{}, err
    }
    _ = c.writeCache(idx)
    return idx, nil
}
```

Three non-obvious points:

1. **The injected clock (`c.Now`) is what makes the TTL test fast.** The expiry test jumps `now` forward 10 minutes between calls instead of sleeping. The same field is `nil` in production, defaulting to `time.Now`.
2. **A malformed cache file is treated as missing.** `readCache` swallows the JSON decode error and reports `hasCached=false`. The user can't fix a corrupted cache without a refetch; surfacing the parse error would just block `hub list` for no upside.
3. **The cache wraps only HTTP, not File.** In `main.go` we conditionally inject `Cache` only when `--url` is set. Wrapping a local file with a disk cache would add IO with no benefit — the file *is* the cache.

## What Changed (vs. s05)

s01-s05 all dealt with one logical artifact: a harness, a SKILL.md, a preview bundle. s06 is the first chapter where the harness needs to *discover* artifacts it doesn't have a path to yet. Two concrete deltas:

- **External I/O surface.** Previous chapters touched only `os.Stdout`/`os.Stderr` and (in s04) the on-disk cache. s06 introduces `net/http` — the first chapter where flaky networks matter. The stale-fallback in `Cache.FetchIndex` is the load-bearing concession to that reality.
- **Multiple `Source` implementations.** The interface is the smallest abstraction that makes both real fetches and test fakes pluggable. Counting calls (as `countingSource` in the test) is how we prove the cache is doing its job without inspecting file mtime.

## Try It

```bash
cd agents/s06-hub-registry
make demo
```

Sample output (truncated):

```text
==> hub list
[
  {"name": "anygen", "version": "1.0.0", "backend": "pip", ...},
  {"name": "blender", "version": "0.2.0", "backend": "bundled", ...},
  {"name": "audacity", "version": "0.1.0", "backend": "pip", ...}
]

==> hub search blend
[{"name": "blender", ...}]

==> hub info anygen
{"name": "anygen", "version": "1.0.0", "backend": "pip", ...}
```

`make test` runs the five tests: JSON round-trip, HTTP fetch via `httptest`, cache-hit-within-TTL, cache-expiry-after-TTL, search substring.

## Upstream Source Reading

Read [`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py) (115 lines) alongside our Go `registry.go`. Focus areas:

- **`_fetch_json`** (Python ~33-56) — the cache+fetch core. Our Go `Cache.FetchIndex` is a structural transcription: read cache → check TTL → fall through to fetch → write cache → return.
- **`fetch_all_clis`** (Python ~75-90) — merges the harness registry and the public registry, tagging each entry with `_source`. We don't model that split in s06 (one Source per call); the equivalent in Go would be a `MultiSource` wrapping two `Source`s and concatenating their `Index.Manifests`.
- **`search_clis` and `get_cli`** (Python ~93-114) — case-insensitive substring match across name/description/category. Our `Hub.Search` matches Name and Backend (the curriculum's Manifest schema dropped Description); the search surface is otherwise identical.
- **`CACHE_TTL = 3600`** (Python line 13) — the literal one-hour default. We default to the same value in `parseTTL`, and surface it as a CLI flag (`--ttl 1h`) so callers can tune for batch jobs.

Local offline copy in [`upstream-readings/s06-hub-registry.py`](../../upstream-readings/s06-hub-registry.py).
