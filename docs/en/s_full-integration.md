---
title: "s_full · End-to-end integration trace"
chapter: full
slug: s_full-integration
est_read_min: 9
---

# s_full · End-to-end integration trace

> What this teaches: how the ten chapters compose into a single user flow — agent asks the hub to install a harness, runs it, and gets a JSON result it can parse.

## Problem

We've shipped ten standalone Go modules — each is its own world. The teaching value of the curriculum doesn't crystallize until you can trace a single user request through every layer and see why each chapter exists. Without that trace, s04's preview cache and s08's verify pass feel like loose ends; with it, they're load-bearing.

## Solution

Pick one canonical use case and walk it: *"An agent uses the hub to install `anygen`, then calls it to generate a SKILL.md from a prompt."* Five chapters touch this path (s06 install registry lookup, s07 install, s09 anygen client, s02 SKILL.md parser, s01 dispatch). The remaining five (s03 skill-gen, s04 preview, s05 REPL, s08 verify, s10 publish) sit one hop off the path; we'll annotate where they would plug in.

The diagram, then the 16 steps.

```text
┌────────┐  hub install   ┌────────┐ download  ┌────────┐
│ agent  │ ─────────────► │  s06   │ ────────► │  s07   │
│        │                │ regist │           │ instal │
│        │ ◄───────────── │  ry    │ ◄──────── │  ler   │
└───┬────┘  manifest      └────────┘  receipt  └───┬────┘
    │                                              │
    │  anygen submit "make a SKILL.md for X"       │
    │   ─────────────► ┌────────┐ HTTP POST  ┌────┴───┐
    │                  │  s01   │ ────────► │  s09   │
    │                  │ dispat │           │ anygen │
    │  ◄────────────── │  ch    │ ◄──────── │ client │
    │   {"ok":true,    └────────┘           └────────┘
    │    "data":{...}}                          │
    │                                           │  result
    │  parse SKILL.md ─────────────────► ┌──────┴─────┐
    │                                    │   s02      │
    │  ◄──────────────────────────────── │ skill-md   │
    │   Skill{Meta, Body}                └────────────┘
```

## The 16-step execution trace

| Step | Action | Chapter | File / function |
|-----:|--------|---------|-----------------|
|  1 | Agent reads its plan; decides it needs an `anygen` skill | (off-path) | — |
|  2 | Agent runs `hub install anygen` | s06 + s07 | `s06/main.go`, `s07/main.go` |
|  3 | Hub HTTP-fetches the index (cache miss → real GET) | s06 | `s06/registry.go: HTTPSource.FetchIndex` |
|  4 | Cache stores the new index with TTL=24h | s06 | `s06/registry.go: Cache.FetchIndex` |
|  5 | Hub looks up `anygen` → returns Manifest{Backend:"bundled", URL:...} | s06 | `s06/commands.go: Hub.Info` |
|  6 | Installer dispatches Backend=bundled → BundledInstaller | s07 | `s07/installer.go: Registry.Install` |
|  7 | BundledInstaller GETs the tarball, extracts to install dir | s07 | `s07/installer.go: extractTarGz` |
|  8 | Installer appends manifest to `installed.json` ledger | s07 | `s07/installer.go: appendLedger` |
|  9 | Agent invokes `anygen submit "make a SKILL.md for X"` | s01 + s09 | `s01/cli.go: Dispatch` |
| 10 | Dispatch walks argv → finds `submit` subcommand | s01 | `s01/cli.go: Dispatch` |
| 11 | submit handler calls APIClient.SubmitJob (POST /jobs) | s09 | `s09/client.go: SubmitJob` |
| 12 | Remote server queues the job; returns `{"jobID":"abc"}` | s09 | (HTTP) |
| 13 | Agent calls `anygen wait abc` → poll loop | s09 | `s09/poller.go: WaitForResult` |
| 14 | After N polls, server returns Status:"succeeded" + Result | s09 | `s09/client.go: FetchResult` |
| 15 | Dispatch wraps `JobResult` in `Result{OK:true, Data:...}` | s01 | `s01/cli.go: Dispatch` (JSON branch) |
| 16 | Agent parses `Result.Data` → loads SKILL.md → next plan step | s02 | `s02/skill.go: Parse` |

Where the off-path chapters plug in:

- **s03 skill-gen** comes into play if the agent itself wants to ship a new harness. After step 16 it can call `skill-gen` on its own `CLI` struct to emit a SKILL.md for installation.
- **s04 preview-bundle** would wrap step 9-15 with a content-addressed cache; the second time the agent submits the same prompt, the bundle replays without re-hitting the remote API.
- **s05 repl-skin** is the human-facing skin around step 9-15. Same dispatch, same SKILL.md, but driven from a `> ` prompt.
- **s08 verify-plugin** is the gatekeeper between step 7 and step 8: the installer validates the bundle's structure before recording it in the ledger.
- **s10 publish-flow** is what runs upstream of step 3 — the registry index is regenerated by s10's pipeline every time a maintainer ships a new plugin.

## Deliberate omissions

- **Auth.** No bearer tokens, signed manifests, or capability-scoped install rights. Upstream is also loose here; tightening would be a real-world exercise.
- **Concurrent installs.** s07's ledger isn't fcntl-locked. The upstream uses `fcntl.flock`; a Go port would use `golang.org/x/sys/unix.Flock` or `os.OpenFile(... O_EXCL)`. Out of scope.
- **Real backends.** s07 stubs `pip install` via the FakeShell; we never invoke pip for real. Upstream actually shells out — that's a 10-line diff in `RealShell.Run`.
- **Telemetry.** No usage counter, no version-update push. Upstream's hub frontend has these.
- **Multi-tenant cache.** s04's preview cache is single-user. Upstream isolates by harness version; we collapsed that to the args-only key.
- **SSE / streaming.** s09's anygen is poll-based. Upstream supports both poll and SSE; we only wired poll.

## Try It

Each module ships its own demo; the trace below stitches three of them by hand:

```bash
# Step 2-8: install
cd agents/s06-hub-registry && make demo
cd agents/s07-installer    && make demo

# Step 9-15: submit + wait
cd agents/s09-anygen-remote && make demo

# Step 16: parse the resulting SKILL.md
cd agents/s02-skill-md && make demo
```

For a true end-to-end Go test that asserts every step in one process, see `agents/s_full-integration/trace_test.go` (left as a follow-up exercise — the building blocks are all in place; what's missing is a fixture registry JSON + an httptest server that fakes anygen).

## Upstream Source Reading

The trace mirrors the upstream's own end-to-end flow with one substitution at every step:

- Hub registry: [`cli-hub/cli_hub/registry.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/registry.py)
- Installer: [`cli-hub/cli_hub/installer.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-hub/cli_hub/installer.py)
- anygen entry: [`anygen/agent-harness/ANYGEN.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/ANYGEN.md)
- SKILL.md parser: [`cli-anything-plugin/skill_generator.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/skill_generator.py)
