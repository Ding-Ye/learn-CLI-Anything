---
title: "s07 · Multi-backend installer"
chapter: 07
slug: s07-installer
est_read_min: 8
---

# s07 · Multi-backend installer

> What this teaches: how to turn a `Manifest` into a working install across four very different backends (`pip`, `npm`, `bundled`, `fake`) without coupling the dispatcher to any of them — by hiding the differences behind a one-method `Shell` interface and a single `BundledInstaller`, then keeping the result on disk as one JSON ledger.

## Problem

s06 gave us the registry — a list of `Manifest` entries describing which CLIs exist. But a manifest is just metadata; the agent still has to *install the thing* before it can call it. Upstream solves this with `cli-hub/cli_hub/installer.py`, a 373-line module that branches on `package_manager` and invokes `pip install`, `npm install -g`, `uv pip install`, or shells out to a curl/bash pipeline. The branching is the load-bearing part — and it's exactly what an LLM agent should not have to think about. The agent says "install anygen"; the installer figures out whether that means a Python wheel, an npm tarball, or a bundled archive.

Three concrete pains the agent hits without a dispatcher:

1. **Different argv shapes per backend.** `pip` wants `<name>==<version>`; `npm` wants `<name>@<version>`; `uv` slaps `pip` in the middle. Hard-coding any one of them per call site means every wrapped tool re-derives the same logic.
2. **Bundled archives need actual file I/O.** Some CLIs ship their own tarball (because they wrap a GUI app's plugin folder). The installer has to download, verify, extract — none of which a package manager does for you.
3. **State has to be queryable.** "What's installed?" is the question agents ask before every plan. Without a ledger we'd be probing PATH for every manifest, which is both slow and unreliable (a name on PATH doesn't mean *this* version is installed).

## Solution

One `Registry` struct, four backends behind two strategies, one JSON file.

```go
type Installer interface {
    Install(ctx context.Context, m Manifest) error
    Uninstall(ctx context.Context, name string) error
    List(ctx context.Context) ([]Manifest, error)
}
```

The `Registry` satisfies `Installer` and dispatches by `Manifest.Backend`:

- **`bundled` → BundledInstaller**: downloads `manifest.url`, extracts the gzip tarball into `InstallDir/<name>/`, rejects path-traversal entries.
- **`pip` / `npm` / `uv` → ShellInstaller**: derives the argv via `installArgs(m)` and hands it to the injected `Shell`.
- **`fake` → ledger-only**: useful for demos, tests, and any backend whose install step happens elsewhere (e.g. the upstream's `bundled` strategy for tools that ship inside a GUI app).
- **anything else → typed error**: the dispatcher fails loudly instead of silently doing nothing.

Three design decisions worth calling out:

1. **`Shell` is an interface, not a function.** The production wiring is `RealShell{}` which delegates to `os/exec`; tests use `FakeShell` that records every call. The dispatcher itself never imports `os/exec`. This is the single biggest deviation from the upstream — Python's `subprocess.run` is called inline everywhere, which is fine for a script but makes unit tests effectively impossible.
2. **`pip` / `npm` / `uv` share one strategy, not three.** Upstream has nine near-identical `_pip_install` / `_npm_install` / `_uv_install` functions. They all amount to "compute argv, call subprocess.run, return (ok, msg)". We collapse them into `installShell` parameterised by `installArgs`, which is a switch on `Backend`. Same surface area, one-third the code.
3. **The ledger is the single source of truth.** Upstream's `installed.json` lives at `~/.cli-hub/`; we sit at `~/.cache/learn-cli-anything-s07/` (via `os.UserCacheDir` for cross-platform layout). The shape is `[]Manifest`, written via `MarshalIndent` so the file is grep-able. `List` reads it; `Install` replaces-or-appends; `Uninstall` removes-by-name.

## How It Works

```text
Install(ctx, m)
    │
    ├─ m.Backend == "bundled"  ──▶ installBundled
    │                                  │
    │                                  ├─ HTTP GET m.URL
    │                                  ├─ gzip + tar extract → InstallDir/<name>/
    │                                  └─ reject "../"/abs paths
    │
    ├─ m.Backend in {pip,npm,uv} ─▶ installShell
    │                                  │
    │                                  ├─ installArgs(m) → ["pip","install","foo==1.2"]
    │                                  └─ Shell.Run(ctx, args[0], args[1:]...)
    │
    ├─ m.Backend == "fake"     ──▶ (no-op)
    │
    └─ else                    ──▶ "unknown backend %q"

    │
    └─▶ appendLedger(m)   (replace-by-name, then MarshalIndent → installed.json)
```

The dispatch core (from `installer.go`) is 20 lines:

```go
func (r *Registry) Install(ctx context.Context, m Manifest) error {
    if m.Name == "" {
        return errors.New("install: manifest.name is required")
    }
    switch m.Backend {
    case "bundled":
        if err := r.installBundled(ctx, m); err != nil {
            return err
        }
    case "pip", "npm", "uv":
        if err := r.installShell(ctx, m); err != nil {
            return err
        }
    case "fake":
        // ledger-only
    default:
        return fmt.Errorf("install: unknown backend %q (want one of: pip, npm, uv, bundled, fake)", m.Backend)
    }
    return r.appendLedger(m)
}
```

Three non-obvious points:

1. **Path traversal is rejected at extraction time.** `extractTarGz` cleans every header name and rejects entries that escape the destination dir (`..` or absolute). A malicious tarball is a real attack vector for any "download and extract" installer — the upstream's `_bundled_install` skips this because it doesn't actually extract anything (it just runs `detect_cmd`). Since we *do* extract, the check is mandatory.
2. **`installArgs` is a pure function.** Both `installShell` and the unit test call it. The test asserts on the same argv the production code emits, so the only thing the FakeShell verifies is "the dispatcher *did* call Shell.Run with these arguments." No string-compare on a stringified command line.
3. **Re-install is idempotent.** `appendLedger` replaces by name, so `install foo` followed by a second `install foo` updates the version field in place rather than producing a duplicate row. Upstream does the same (`installed[cli['name']] = ...` is a dict-set).

## What Changed (vs. s06)

s06 ended at "the registry can tell you what exists." s07 adds "and now you can put it on disk." Three concrete deltas:

- **Re-declared `Manifest`.** Same struct as s06 — Name, Version, Backend, Entry, Skill, Requires — plus an optional `URL` field that only the bundled backend reads. The no-cross-imports rule means we duplicate the type rather than import s06.
- **A new `Shell` interface.** s06 was pure I/O on a JSON file plus an HTTP fetch; nothing it did needed shelling out. s07 is the first chapter where the harness invokes external processes, and the `Shell` injection is what keeps the tests hermetic.
- **The ledger is mutated by `Install`/`Uninstall`, not just read.** s06's TTL cache is invalidated by time; s07's ledger is invalidated by the user's explicit action. The two co-exist in the upstream — s06 caches *what could be installed*; s07 tracks *what is installed*.

## Try It

```bash
cd agents/s07-installer
make build
make test
```

To see the full flow with a real HTTP fetch:

```bash
make demo
```

That spins up `python3 -m http.server` against `testdata/anygen-0.1.tar.gz`, writes a manifest pointing at it, then runs `install` → `list` → `uninstall`. The five unit tests cover the same paths without needing python:

```text
=== RUN   TestInstallBundled
=== RUN   TestInstallPipRecordsShellCall
=== RUN   TestListAfterTwoInstalls
=== RUN   TestUninstallBundledRemovesFromDiskAndLedger
=== RUN   TestUnknownBackendErrors
ok      learn-cli-anything/s07
```

## Upstream Source Reading

Read [`cli-hub/cli_hub/installer.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/installer.py) (373 lines) alongside our `installer.go` (≈300 lines). Focus areas:

- **`_install_strategy`** (Python ~85-99) — the rule table that maps `package_manager` → strategy. Our Go version inlines this in the `switch` because we already have a typed `Backend` field on the manifest (the upstream's manifests can omit it and fall back to heuristics).
- **`_pip_install` / `_npm_install` / `_uv_install`** (Python ~166-329) — three near-identical functions; we collapse them into `installShell` + `installArgs` driven off a single switch.
- **`_run_command`** (Python ~58-77) — auto-detects shell metacharacters and switches between `subprocess.run(shell=True)` and `shlex.split`. We don't need this because the manifest's argv is structured (a `name` + `version` pair, not a free-text command); the upstream supports `curl … | bash`-style install commands for completeness.
- **`_perform_action`** (Python ~289-301) — the strategy → action dispatch table. The pattern is the same as our `switch m.Backend`; the Python dict-of-dicts is slightly more declarative but does the same job.
- **`install_cli` / `uninstall_cli` / `update_cli`** (Python ~316-373) — the top-level entrypoints that wrap `_perform_action` and update `installed.json`. Our `Registry.Install` / `Uninstall` mirror this; we skip `update` (it's just `uninstall` + `install` with `--force-reinstall`, which the curriculum doesn't need to demonstrate twice).

Local offline copy of the first 200 lines in [`upstream-readings/s07-installer.py`](../../upstream-readings/s07-installer.py).
