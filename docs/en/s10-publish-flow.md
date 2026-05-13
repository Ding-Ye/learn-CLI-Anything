---
title: "s10 · Publish flow: CI + registry"
chapter: 10
slug: s10-publish-flow
est_read_min: 8
---

# s10 · Publish flow: CI + registry

> What this teaches: how the CI side of CLI-Anything turns a directory of plugin subdirs into a signed, indexed release. We port the *shape* of `.github/workflows/publish-cli-hub.yml` + `check-root-skills.yml` + the packaging surface of `cli-hub/cli_hub/` into a five-step pipeline that runs locally and produces bit-identical bytes on every re-run.

## Problem

s01-s09 built the **runtime** side of the harness: parsers, REPLs, registries, installers, an anygen case study. None of those answer the question "how does a plugin actually get into the registry the installer reads from?" The upstream answers it in CI: when `cli-hub/**` changes on `main`, a workflow rebuilds the package, checks PyPI for the version, and publishes; a parallel workflow validates every harness's SKILL.md against the repo-root mirror in `skills/`. That's two scripts and two workflow YAMLs cooperating across a push. We need a single local Go binary that captures that flow — without the actual push — so the curriculum stays hermetic and reproducible.

## Solution

A `Pipeline` struct with five methods, run in order, each returning a `StepReport`:

```text
ScanPlugins(src)   walk src/ one level deep; collect every SKILL.md-bearing dir
Validate           SKILL.md must exist and be non-empty
Bundle             tar.gz each plugin dir → out/<name>-<version>.tar.gz
Sign               sha256 each artifact → out/<artifact>.sha256
UpdateIndex        emit out/registry.json with one entry per plugin
```

`Run(ctx, src, out)` chains them and aggregates per-step reports into a `PipelineReport`. Three design decisions worth pointing at:

1. **Reproducible bytes.** Every `tar` header gets `fixedMTime = 2024-01-01 UTC`, uid/gid zeroed, uname/gname stripped. The upstream achieves the same via `SOURCE_DATE_EPOCH` inside `python -m build`; we get it with one constant. Re-running `publish run` on the same input produces byte-identical tarballs, which means identical sha256s, which means CI re-runs don't churn the index.
2. **`Sign` is a digest, not a signature.** Real signing (Sigstore, PyPI trusted-publishing) is out of scope; the point is to show *where* signing slots into the pipeline. The sidecar format matches GNU `sha256sum` so off-the-shelf tooling can verify.
3. **Hermetic.** No HTTP. No `git push`. The publisher writes a directory you could rsync to a CDN or `gh-pages` branch, and stops. The `pypa/gh-action-pypi-publish` step in the upstream workflow is exactly the part we *don't* port — it would couple the curriculum to network state.

## How It Works

```text
Pipeline.Run(ctx, src, out)
   │
   ├─ ScanPlugins(src)        for each subdir with SKILL.md:
   │                            readSkillFront → Manifest{Name, Version, Backend, Entry, Skill}
   │                            (sorted by Name for deterministic order)
   │
   ├─ Validate(src)           stat every Manifest.Skill; fail on missing/empty
   │
   ├─ Bundle(src, out)        for each Manifest:
   │                            writeTarGz(src/Entry → out/<name>-<version>.tar.gz)
   │                            (fixed mtime, zero uid/gid)
   │
   ├─ Sign(out)               for each artifact:
   │                            sha256File → out/<artifact>.sha256
   │
   └─ UpdateIndex(out)        read every sidecar, marshal registry.json {meta, clis[]}
```

The five-step body of `Run` (~30 LOC from `agents/s10-publish-flow/publish.go`):

```go
func (p *Pipeline) Run(ctx context.Context, srcDir, outDir string) (PipelineReport, error) {
    rep := PipelineReport{SrcDir: srcDir, OutDir: outDir}
    step, err := p.ScanPlugins(srcDir)
    rep.Steps = append(rep.Steps, step)
    if err != nil { rep.OK = false; return rep, err }
    step, _ = p.Validate(srcDir)
    rep.Steps = append(rep.Steps, step)
    if !step.OK { rep.Plugins = p.Plugins; rep.OK = false; return rep, nil }
    step, err = p.Bundle(srcDir, outDir)
    rep.Steps = append(rep.Steps, step)
    if err != nil { rep.Plugins = p.Plugins; rep.OK = false; return rep, err }
    step, _ = p.Sign(outDir)
    rep.Steps = append(rep.Steps, step)
    if !step.OK { rep.Plugins = p.Plugins; rep.OK = false; return rep, nil }
    step, _ = p.UpdateIndex(outDir)
    rep.Steps = append(rep.Steps, step)
    rep.Plugins = p.Plugins
    rep.OK = allOK(rep.Steps)
    return rep, nil
}
```

Three non-obvious points:

1. **Validate-then-bundle, not bundle-then-validate.** If a SKILL.md is corrupt, bundling it would produce a tarball that fails the index step anyway — but with a worse blame line ("sha256 mismatch") than the real cause ("empty SKILL.md"). Short-circuiting on Validate keeps the failure mode legible.
2. **The error return is reserved for I/O.** `Run` returns a non-nil error only when something the caller can't recover from happens (out-dir mkdir failed, registry.json couldn't be written). Per-plugin failures go into `StepReport.Errors` and the run continues so the operator sees the full damage report.
3. **`ScanPlugins` is one level deep, on purpose.** A plugin is a directory at the root of `src/`. The upstream layout (`blender/`, `audacity/`, …) matches that exactly. Recursing would let `testdata/plugin/sub/SKILL.md` masquerade as a plugin — that's a bug, not a feature.

## What Changed (vs. s09)

s09 was a single harness wrapping a remote API; s10 is the *meta* layer that ships any harness. Two concrete deltas:

- **The `Manifest` shape from the plan finally gets used.** s06 and s07 hinted at it via their consumer side; s10 is the producer. The JSON tags match the canonical signature in the curriculum plan, so a downstream s06-style registry reader can consume s10's output without translation.
- **A reproducible `tar.gz` writer.** s01-s09 wrote files at whatever mtime `os.WriteFile` defaulted to. Bundling for release demands byte-stability, so `writeTarGz` zeroes out every header field that varies between runs.

## Try It

```bash
cd agents/s10-publish-flow
make demo         # full pipeline against testdata/, JSON envelope
make status       # cat the registry.json from the prior run
make test         # five tests on a two-plugin t.TempDir() fixture
```

After `make demo`, `out/` contains:

```text
plugin-good-0.1.0.tar.gz
plugin-good-0.1.0.tar.gz.sha256
registry.json
```

`tar tzf out/plugin-good-0.1.0.tar.gz` lists `plugin-good/SKILL.md` — the tarball stores the plugin at its directory name, exactly the layout an installer wants.

## Upstream Source Reading

Read these alongside our 350-line `publish.go`:

- [`.github/workflows/publish-cli-hub.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/publish-cli-hub.yml) — the 49-line workflow that runs `python -m build` and `pypa/gh-action-pypi-publish` on push to `main`. The "check if version already published" curl into `pypi.org/pypi/<pkg>/<v>/json` is the upstream's idempotency check; our equivalent is the fixed-mtime tar + sha256 — same property, different mechanism.
- [`.github/workflows/check-root-skills.yml`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/.github/workflows/check-root-skills.yml) — the PR-time validation gate that calls `validate_root_skills.py`. We collapse the discover+validate into our `ScanPlugins` + `Validate` pair.
- [`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py) — the *consumer* of what `UpdateIndex` emits. Reading that file makes the schema choice in our `indexEntry` legible: `name`, `version`, `backend`, `entry`, `skill` cover what `fetch_all_clis` keys off, plus `artifact` + `sha256` for the install/verify step that an s07-style installer would do.
- [`cli-hub/setup.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/setup.py) — the source of truth for the package version the publish workflow reads. Our `readSkillFront` plays the same role at the plugin level: front-matter `version:` is the contract.

Local offline copy of the first 200 lines of the two workflow YAMLs in [`upstream-readings/s10-publish-flow.yml`](../../upstream-readings/s10-publish-flow.yml).
