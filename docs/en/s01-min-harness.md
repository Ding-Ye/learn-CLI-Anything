---
title: "s01 · Minimum harness: CLI + JSON output"
chapter: 01
slug: s01-min-harness
est_read_min: 6
---

# s01 · Minimum harness: CLI + JSON output

> What this teaches: the smallest Go program that satisfies CLI-Anything's HARNESS contract — a subcommand tree, a `--json` mode, and a `Result` envelope with `error` for non-zero exits.

## Problem

CLI-Anything's central claim is that LLM agents are best served by uniform CLI surfaces, not bespoke SDKs or GUIs. Before we can show how the framework *generates* those surfaces (s02-s05) or *distributes* them (s06-s07), we need to know what a conforming harness LOOKS like. The upstream `HARNESS.md` is a one-page spec; this chapter ships its smallest Go incarnation.

## Solution

A harness is a `CLI` struct: a name, a help string, optional flags, and either subcommands or a `Run` function. `Dispatch` walks argv against this tree. The `--json` flag is parsed at the top level (we never let subcommands see it) and switches the printer between human text and a `Result{OK, Data, Error}` envelope. Three design decisions matter:

1. **Data-first, not decorator-first.** The upstream uses Click's `@click.command` decorators which works in Python but means metadata is bound at import time and inspected via reflection. In Go we make the metadata a struct literal so s03's skill generator can introspect it without runtime tricks.
2. **JSON mode is purely the printer, not the handler.** A handler returns `(any, error)`. The dispatcher decides what to print based on `--json`. This keeps subcommand code free of presentation concerns.
3. **`Result.OK` exists even when there's no data.** Agents that parse JSON envelopes want a single field to switch on, not "infer success from absence of `error`."

## How It Works

```text
argv ──▶ hasJSONFlag ──▶ Dispatch(root, argv, jsonMode, out, err)
                            │
                            ▼
                       walk root.Subcommands until argv runs out or no match
                            │
                            ▼
                       cur.Run(ctx, remaining) ──▶ (any, error)
                            │
                            ▼
                  jsonMode ? Result{} envelope on stdout : pretty text
```

The core dispatcher (~40 LOC from `agents/s01-min-harness/cli.go`):

```go
func Dispatch(ctx context.Context, root *CLI, argv []string, jsonMode bool, out, errOut *os.File) int {
    cur := root
    i := 0
    for i < len(argv) {
        next, ok := cur.Subcommands[argv[i]]
        if !ok { break }
        cur = next
        i++
    }
    if cur.Run == nil {
        printHelp(cur, out, jsonMode)
        return 0
    }
    out1, err := cur.Run(ctx, argv[i:])
    if jsonMode {
        env := Result{OK: err == nil, Data: out1}
        if err != nil { env.Error = err.Error() }
        _ = json.NewEncoder(out).Encode(env)
    } else if err != nil {
        fmt.Fprintln(errOut, "error:", err)
        return 1
    } else {
        fmt.Fprintln(out, prettyPrint(out1))
    }
    if err != nil { return 1 }
    return 0
}
```

Three non-obvious points:

1. **No global flag library.** stdlib `flag` package would conflate parent and child flags; we hand-parse argv after dispatch and let each subcommand own its flag namespace. This is what every CLI-Anything harness ends up doing eventually anyway.
2. **`printHelp` honors `--json`.** An agent that runs `demo --json` (with no subcommand) needs a machine-readable description of the harness's surface — same shape as the data a tool/list returns in MCP. s03 will lean on this.
3. **Errors flow to stderr (human) OR `Result.Error` (JSON) — never both.** Mixing them is a common newbie mistake; an agent tailing stdout would treat a stderr error as a missing reply.

## What Changed (vs. (none))

This is the bootstrap chapter; there is no previous session. The reference point is upstream `cli-anything-plugin/HARNESS.md` — we built ~150 LOC of Go that implements its `--json` + subcommand + help contract.

## Try It

```bash
cd agents/s01-min-harness

# Human mode
make demo

# JSON envelope an agent sees
make demo-json
```

Expected from `make demo-json`:

```json
{"ok":true,"data":"hi"}
{"ok":true,"data":{"flags":null,"help":"...","name":"demo","subcommands":["echo","time"]}}
{"ok":true,"data":{"unix":...}}
```

## Upstream Source Reading

Read [`cli-anything-plugin/HARNESS.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/HARNESS.md) and contrast with `agents/s01-min-harness/cli.go`. The contract sections to focus on:

- **Subcommand tree** — what makes a "good" subcommand granularity (one verb per command).
- **Output modes** — JSON envelope shape (`ok`, `data`, `error`) is fixed; harnesses cannot invent fields.
- **Exit codes** — non-zero on error, 0 on success; JSON mode also emits `ok:false` so an agent can branch without parsing exit codes from a shell wrapper.

Local offline copy in [`upstream-readings/s01-min-harness.md`](../../upstream-readings/s01-min-harness.md).
