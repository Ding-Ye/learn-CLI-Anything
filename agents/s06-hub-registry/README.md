# s06 — CLI-Hub registry

Tiny `hub` CLI that lists, searches, and inspects manifests from a JSON
index. Go port of the upstream `cli-hub/cli_hub/registry.py`, distilled to
the three jobs that matter for an agent: fetch JSON, cache it on disk for
a TTL, fall back to the cache on network failure.

## Run

```bash
make demo
```

That builds the binary and runs:

```bash
./s06-hub-registry --file testdata/registry.json list
./s06-hub-registry --file testdata/registry.json search blend
./s06-hub-registry --file testdata/registry.json info anygen
```

Switch to a real HTTP source with `--url`:

```bash
./s06-hub-registry --url https://example.com/registry.json --ttl 1h list
```

The cache lands in `~/.cache/learn-cli-anything-s06/index.json` unless
overridden via `--cache <path>`.

## Layout

- `cli.go` — re-declared `CLI` / `Flag` / `Result` + `Dispatch`.
- `registry.go` — `Manifest`, `Index`, the `Source` interface, `HTTPSource`,
  `FileSource`, and the `Cache` wrapper.
- `commands.go` — `Hub.Search` / `Hub.List` / `Hub.Info`.
- `main.go` — argv parsing + wiring (`--file` | `--url`, `--cache`, `--ttl`).
- `registry_test.go` — five tests: round-trip, HTTP, cache-hit, cache-expiry, search.
- `testdata/registry.json` — three-entry fake registry (anygen / blender / audacity).

## Upstream

See `docs/{zh,en}/s06-hub-registry.md` for the annotated walkthrough and
`upstream-readings/s06-hub-registry.py` for the first 200 lines of
`registry.py`.
