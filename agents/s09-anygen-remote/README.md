# s09 — anygen: a remote-API harness

A CLI-Anything harness doesn't have to wrap a local GUI. **anygen** wraps
an HTTP API — the upstream service runs server-side and the CLI just
submits prompts, polls for completion, and fetches the result. This is
the Go port of the lifecycle in
`anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py`,
trimmed to the parts that highlight what's different from a local
harness.

## What's different from s01..s05

| Aspect          | Local GUI harness (s01..s05)         | Remote-API harness (s09)             |
|-----------------|--------------------------------------|--------------------------------------|
| Process model   | Fork subprocess, read stdout/stderr  | HTTP request, decode JSON            |
| Done signal     | Subprocess exits                     | Status flips to `succeeded`/`failed` |
| Idle behaviour  | Block on read                        | Poll on an interval                  |
| Cancellation    | Send signal to PID                   | Cancel context, server keeps running |
| Failure mode    | Non-zero exit code                   | Status = `failed` + error body       |

The poller in `poller.go` is the load-bearing piece — that loop is what
GUI harnesses never need.

## Run

```bash
make build
ANYGEN_BASE_URL=https://www.anygen.io \
ANYGEN_API_KEY=sk-xxx \
./s09-anygen-remote submit "Quarterly business review presentation"
./s09-anygen-remote status <job-id>
./s09-anygen-remote wait   <job-id> --interval 3s --timeout 20m
./s09-anygen-remote result <job-id>
```

`--json` toggles the s01-style envelope on any subcommand.

For an offline end-to-end run against an in-process httptest.Server:

```bash
make demo
```

## Commands

| command                  | what it does                                                |
|--------------------------|-------------------------------------------------------------|
| `anygen submit <prompt>` | POST /jobs, print the new jobID                             |
| `anygen status <id>`     | GET /jobs/:id, print one of queued/running/succeeded/failed |
| `anygen wait <id>`       | Poll until terminal, then fetch + print the result          |
| `anygen result <id>`     | GET /jobs/:id/result without polling                        |

`wait` accepts `--interval <Go duration>` (default `2s`) and
`--timeout <Go duration>` (default `20m`).

## Files

- `cli.go` — re-declared `CLI` / `Flag` / `Result` envelope + `Dispatch`.
- `client.go` — `APIClient` + `SubmitJob` / `PollStatus` / `FetchResult`. Plain `net/http`, zero deps.
- `poller.go` — `WaitForResult`: poll-until-terminal with context cancellation.
- `main.go` — the four `anygen` subcommands wired into the s01 Dispatch.
- `client_test.go` — five tests against `httptest.Server`.
- `demo/demo.go` — `make demo` target; spins up a fake server and runs the lifecycle.

## Auth & config

Reads (in priority order, matching upstream's `get_api_key`):

1. `ANYGEN_API_KEY` env var
2. (upstream also reads `~/.config/anygen/config.json`; we elide the
   config-file tier — env vars are plenty for the curriculum.)

`ANYGEN_BASE_URL` overrides the default `https://www.anygen.io` (set it
to a local mock during development).

## Upstream

See `docs/{zh,en}/s09-anygen-remote.md` for the annotated walkthrough
and `upstream-readings/s09-anygen-remote.md` for the first 200 lines of
the upstream `ANYGEN.md`.
