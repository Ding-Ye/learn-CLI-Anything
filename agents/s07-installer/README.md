# s07 — Multi-backend installer

Dispatcher that turns a `Manifest` (from s06) into a real install: tarball
extraction for `bundled`, `pip install <name>==<version>` for `pip`, and the
parallel `npm`/`uv` argv shapes. The Go port of the upstream
`cli-hub/cli_hub/installer.py`, distilled down to the parts an LLM agent
actually needs.

## Run

```bash
make build
./s07-installer install path/to/manifest.json
./s07-installer list
./s07-installer uninstall anygen
```

`--json` before any subcommand wraps output in s01's `Result` envelope.

## Backends

| backend   | install action                                            |
|-----------|-----------------------------------------------------------|
| `pip`     | `pip install <name>==<version>`                           |
| `npm`     | `npm install -g <name>@<version>`                         |
| `uv`      | `uv pip install <name>==<version>`                        |
| `bundled` | download `manifest.url`, extract the .tar.gz into install dir |
| `fake`    | ledger-only — for demos and tests                         |

The `Shell` interface is injectable, so tests use a `FakeShell` that records
every argv tuple instead of actually forking pip. See `installer_test.go`.

## Files

- `cli.go` — re-declared `CLI` / `Flag` / `Result` + `Dispatch`.
- `manifest.go` — re-declared `Manifest` (matches s06).
- `installer.go` — `Registry` + `Installer` interface + shell/bundled strategies.
- `main.go` — entrypoint: `install` / `uninstall` / `list`.
- `installer_test.go` — five tests (bundled via `httptest`, pip via `FakeShell`, list, uninstall, unknown-backend).
- `testdata/anygen-0.1.tar.gz` — fixture tarball used by `make demo`.

## Demo

`make demo` spins up `python3 -m http.server`, points a manifest at it,
installs anygen, lists it, and uninstalls it. Requires `python3` on PATH —
all production code paths are exercised by `make test` which has no system
dependencies.

## Upstream

See `docs/{zh,en}/s07-installer.md` for the annotated walkthrough and
`upstream-readings/s07-installer.py` for the first 200 lines of the upstream
`installer.py`.
