---
title: "s05 · REPL skin: interactive harness"
chapter: 05
slug: s05-repl-skin
est_read_min: 7
---

# s05 · REPL skin: interactive harness

> What this teaches: how to wrap any s01-style `CLI` harness in an interactive REPL — line input, meta-commands prefixed with `:`, a runtime `:json on|off` toggle that re-uses the same `Result` envelope, and SKILL.md auto-detection for the banner.

## Problem

s01 gave us a one-shot CLI an agent invokes per call. That works for stateless probes (`echo`, `time`) but breaks down the moment the agent wants to *keep state* across turns — open a project, set a flag, run a sequence of edits, save. Re-launching the process every line is wasteful (cold start) and forces you to externalize all state to disk. The upstream's answer is `cli-anything-plugin/repl_skin.py`: a shared REPL skin every wrapped tool reuses. We need a Go port that captures the *shape*, not the ANSI-art.

## Solution

A `REPL` struct holds the harness, the four `io.*` streams, an in-memory `History`, a `JSONMode` flag, and an optional `Skill`. `Run(ctx)` is a `bufio.Scanner` loop that:

1. Prints `> ` and reads a line.
2. If empty, continues — empty input is a no-op, not "show help" (matches the upstream and avoids screenful dumps when the user hits Enter twice).
3. If the line starts with `:`, dispatches to a meta-command (`:help`, `:skills`, `:json on|off`, `:history`, `:quit`).
4. Otherwise, splits on whitespace and forwards to `Dispatch(ctx, r.Harness, argv, r.JSONMode, r.Out, r.Err)` — the same s01 function, unchanged.

Three design decisions to call out:

1. **Meta-commands use a `:` prefix.** The upstream uses bare words (`help`, `quit`) which clash with any wrapped tool that happens to define those subcommands. We sidestep the conflict by making the prefix mechanical.
2. **`Dispatch` was widened to `io.Writer`.** s01 took `*os.File`. The REPL has to drive Dispatch from a `bytes.Buffer` in tests, so we relaxed the type. Production behavior is identical.
3. **No prompt_toolkit, no readline.** `bufio.Scanner` is enough for an agent. The upstream's history-search and ANSI palette are human-ergonomics that an LLM doesn't need; we keep `History []string` in memory just for `:history` introspection.

## How It Works

```text
NewREPL(harness) ──▶ r.Run(ctx)
                       │
                       ├─ printBanner()              (reads r.Skill or harness Name/Help)
                       │
                       ▼
                  for sc.Scan():
                       │
                       ├─ line == ""        ──▶ continue
                       ├─ line[0] == ':'    ──▶ runMeta(line)
                       │                          ├─ :help    print table
                       │                          ├─ :skills  list subcommandNames(harness)
                       │                          ├─ :json    flip r.JSONMode
                       │                          ├─ :history dump r.History
                       │                          └─ :quit    return errQuit
                       │
                       └─ otherwise        ──▶ Dispatch(ctx, harness, argv, r.JSONMode, r.Out, r.Err)
```

The full loop (~30 LOC from `agents/s05-repl-skin/repl.go`):

```go
func (r *REPL) Run(ctx context.Context) error {
    r.printBanner()
    sc := bufio.NewScanner(r.In)
    sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
    for {
        fmt.Fprint(r.Out, "> ")
        if !sc.Scan() {
            break
        }
        line := strings.TrimSpace(sc.Text())
        if line == "" {
            continue
        }
        r.History = append(r.History, line)
        if strings.HasPrefix(line, ":") {
            if err := r.runMeta(line); err != nil {
                if errors.Is(err, errQuit) {
                    return nil
                }
                fmt.Fprintln(r.Err, "error:", err)
            }
            continue
        }
        argv := strings.Fields(line)
        _ = Dispatch(ctx, r.Harness, argv, r.JSONMode, r.Out, r.Err)
    }
    return sc.Err()
}
```

Three non-obvious points:

1. **`errQuit` is a sentinel, not a special return.** Meta-commands all share `func(args) error`. `:quit` returns `errQuit`; the loop catches it via `errors.Is` and returns `nil`. The other handlers return real errors, which the loop prints and continues — exiting because `echo` blew up would be hostile.
2. **SKILL.md detection is best-effort.** `findSkill` walks up from CWD looking for `SKILL.md`. Missing-file is not an error — the banner falls back to the harness's `Name`/`Help`. The upstream has an elaborate search (repo-root `skills/<id>/SKILL.md` then packaged path); for the curriculum's purposes one ancestor walk is enough.
3. **`:json on` mid-session matches `--json` at startup.** The same `Dispatch` writes the same `Result` envelope. An agent can flip modes mid-conversation without re-attaching to a new process — that's the whole reason a REPL beats a one-shot CLI for stateful workflows.

## What Changed (vs. s04)

s01-s04 all assume a one-shot lifecycle: argv in, bytes out, process exits. s05 introduces a *session* — the harness stays resident, state lives in memory, and the agent talks to it line by line. Two concrete deltas:

- **`Dispatch`'s writer type widened** from `*os.File` to `io.Writer` so the REPL can drive it from a `bytes.Buffer` in tests. The s01 behavior is unchanged for `os.Stdout` callers.
- **A `Skill` parser was re-declared** (a minimal cousin of s02's). The REPL needs only `Name` and `Description` for the banner, so we hand-roll a two-field YAML reader instead of importing `gopkg.in/yaml.v3`. The `go.mod` stays zero-dep.

## Try It

```bash
cd agents/s05-repl-skin
make build
./s05-repl-skin
```

Inside the REPL:

```text
> :help
> :skills
> echo hi
hi
> :json on
json mode: on
> echo hi
{"ok":true,"data":"hi"}
> :history
> :quit
bye
```

Run `make test` for the five scripted-stdin tests (`strings.Reader` in, `bytes.Buffer` out).

## Upstream Source Reading

Read [`cli-anything-plugin/repl_skin.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/cli-anything-plugin/repl_skin.py) (567 lines) alongside our 200-line `repl.go`. Focus areas:

- **`print_banner`** (Python ~167-218) — ANSI-art box with skill install hint. Our Go port keeps the banner plain ASCII so it survives pipes without escape-code noise.
- **`prompt`** + **`prompt_tokens`** (Python ~220-310) — generates prompt_toolkit token streams. We elide both: an agent doesn't need a colored prompt, just `> `.
- **`success`/`error`/`warning`/`info`** (Python ~342-358) — the message-level helpers a real interactive REPL exposes. We don't need them because Dispatch already prints; if you build a `cli-anything-<thing>` for human use, port these.
- **`create_prompt_session`** (Python ~485-510) — the prompt_toolkit `PromptSession` factory. The Go equivalent would be a `bufio.Reader` with completer hooks; we leave that for users to add per-tool.

Local offline copy of the first 250 lines in [`upstream-readings/s05-repl-skin.py`](../../upstream-readings/s05-repl-skin.py).
