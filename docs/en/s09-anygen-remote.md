---
title: "s09 · anygen — remote-API harness"
chapter: 09
slug: s09-anygen-remote
est_read_min: 8
---

# s09 · anygen — remote-API harness

> What this teaches: a CLI-Anything harness doesn't have to wrap a local GUI. `anygen` wraps an HTTP API — the work happens server-side, the CLI just submits prompts, polls for completion, and fetches the result. The interesting piece is the polling loop, because GUI harnesses don't have one.

## Problem

s01..s05 all assume the wrapped thing lives on the same machine: launch a subprocess, read stdout, exit. That model breaks for **AnyGen**, which is a cloud service. There's no binary to fork, no PID to signal, no stdout to drain. The CLI has to:

1. POST a prompt to `https://www.anygen.io/v1/openapi/tasks`.
2. Get back a `task_id` immediately — the work *hasn't started yet*.
3. Poll `GET /v1/openapi/tasks/:id` on an interval until status flips to `completed` or `failed`.
4. Download the artefact from a signed URL the server returns.

The upstream's `anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py` (Python, ~430 LOC) does all of this with `requests`. We need a Go port that captures the **shape** of a remote-API harness without porting the entire OpenAPI surface — just the lifecycle that distinguishes it from a local one.

## Solution

Three primitives in `client.go`, one loop in `poller.go`, four subcommands in `main.go`:

```
APIClient.SubmitJob   (ctx, prompt)  -> jobID
APIClient.PollStatus  (ctx, jobID)   -> Status      // queued | running | succeeded | failed
APIClient.FetchResult (ctx, jobID)   -> JobResult
WaitForResult         (ctx, client, jobID, interval) -> ResultStatus
```

Four design decisions worth calling out:

1. **`Status` is a string, not an `int` enum.** The upstream service is the source of truth; if it adds `cancelled` next quarter, the harness shouldn't have to recompile. `Status.IsTerminal()` is the only predicate the poller depends on.
2. **`PollStatus` does not treat `failed` as an error.** Returning `(StatusFailed, nil)` keeps the polling layer pure: a single network round-trip, no policy. The policy ("treat failed as fatal") lives one level up, in `WaitForResult`.
3. **`WaitForResult` polls before sleeping.** A job that finishes instantly (cached, no-op) shouldn't pay a full interval of latency. The loop is `poll → branch on terminal → sleep → repeat`, not `sleep → poll`.
4. **Context cancellation preempts both the sleep and the next HTTP call.** `select { case <-ctx.Done(): ... case <-time.After(interval): }` handles the sleep; `http.NewRequestWithContext` handles the in-flight call. An agent that hits its timeout has to be able to actually bail.

## How It Works

```text
anygen submit "AI trends presentation"
  ├─ POST /jobs       { "prompt": "AI trends presentation" }
  └─ ◀  { "job_id": "job-xyz" }

anygen status job-xyz
  ├─ GET  /jobs/job-xyz
  └─ ◀  { "job_id": "job-xyz", "status": "running" }

anygen wait job-xyz --interval 3s --timeout 20m
  │
  │   ┌─────────────────────────────────────────┐
  │   │ for {                                   │
  │   │   status := PollStatus(ctx, jobID)      │
  │   │   if terminal:                          │
  │   │     if succeeded: return FetchResult()  │
  │   │     if failed:    return error          │
  │   │   select <-ctx.Done() | <-time.After()  │
  │   │ }                                       │
  │   └─────────────────────────────────────────┘
  └─ ◀  { "status": "succeeded", "output": "...", "content_type": "..." }
```

The core of `poller.go`:

```go
func WaitForResult(ctx context.Context, c *APIClient, jobID string, interval time.Duration) (ResultStatus, error) {
    if interval <= 0 {
        interval = time.Second
    }
    for {
        status, err := c.PollStatus(ctx, jobID)
        if err != nil {
            if ctx.Err() != nil {
                return ResultStatus{}, ctx.Err()
            }
            return ResultStatus{}, fmt.Errorf("poll: %w", err)
        }
        switch status {
        case StatusSucceeded:
            res, err := c.FetchResult(ctx, jobID)
            if err != nil {
                return ResultStatus{Status: status}, fmt.Errorf("fetch: %w", err)
            }
            return ResultStatus{Status: status, Result: res}, nil
        case StatusFailed:
            return ResultStatus{Status: status}, fmt.Errorf("job %s failed", jobID)
        }
        select {
        case <-ctx.Done():
            return ResultStatus{}, ctx.Err()
        case <-time.After(interval):
        }
    }
}
```

Three non-obvious points:

1. **`if ctx.Err() != nil` unwraps before fmt.Errorf wraps.** A cancelled HTTP request surfaces as a `*url.Error` with a noisy URL string. We special-case it so the caller sees `context.DeadlineExceeded` (testable with `errors.Is`) instead of a stringly-typed mess.
2. **`StatusFailed` short-circuits without calling `/result`.** The upstream convention is that the failure reason rides on the status response itself; `/result` on a failed job is undefined behaviour. Cheaper and clearer to bail at the status step.
3. **No exponential backoff.** A constant interval matches the upstream's `POLL_INTERVAL = 3` and is what the AnyGen team's docs recommend. If you want backoff, add it at the call site — `WaitForResult` is small enough to wrap.

## What Changed (vs. s01..s05)

Two structural deltas from the local-harness chapters:

- **No subprocess at all.** s01's `exec.Command` is replaced by `http.NewRequestWithContext`. The harness is a thin glue layer over `net/http`; there's no PID to wait on, no stdin/stdout pipes, no signal handling.
- **`Result` is renamed `JobResult` to avoid clashing with the CLI envelope.** s01's `Result` (the `{ok, data, error}` envelope) is unchanged; the AnyGen artefact gets its own type. Two `Result`s in one package would force one of them under an alias — explicit naming is cheaper.

One pattern from earlier sessions is reused unchanged: the `CLI` / `Flag` / `Dispatch` trio in `cli.go` is the same shape as s05. A remote harness is still **a CLI** to the agent calling it; only the *body* of the `Run` functions changes.

## Try It

```bash
cd agents/s09-anygen-remote
make build
make demo            # in-process httptest.Server — no network, no API key
```

`make demo` prints the full lifecycle against a fake server:

```text
demo: in-process AnyGen server at http://127.0.0.1:xxxxx
submit -> job_id = demo-job-001
poll  -> status = running
poll  -> status = running
poll  -> status = succeeded
fetch -> output = https://fake.anygen.io/files/demo-job-001.pptx
fetch -> content_type = application/vnd.openxmlformats-officedocument.presentationml.presentation
demo: OK
```

For the real service:

```bash
export ANYGEN_API_KEY=sk-xxx
./s09-anygen-remote submit "Quarterly business review"
./s09-anygen-remote wait <jobID> --interval 3s --timeout 20m
```

Run `make test` for the five `httptest.Server`-driven tests:

- `SubmitJob_ReturnsJobID` — POST body + auth header + parsed jobID.
- `PollStatus_ReturnsStatus` — status round-trip.
- `WaitForResult_Succeeds` — running → running → succeeded → fetch.
- `WaitForResult_Fails` — terminal failure short-circuits without calling `/result`.
- `WaitForResult_RespectsTimeout` — context deadline preempts the loop in < 1 s.

## Upstream Source Reading

Read [`anygen/agent-harness/ANYGEN.md`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/ANYGEN.md) (the architecture brief) alongside [`anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py`](https://github.com/HKUDS/CLI-Anything/blob/c5a4b2d456a4a9baeb2c524d71705a6bdb7dad69/anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py) (the Python implementation). Focus areas:

- **`ANYGEN.md` "CLI Strategy: HTTP API Client"** — the three-line summary of why AnyGen needs polling. Our Go port preserves that strategy verbatim.
- **`anygen_backend.py::poll_task`** (Python ~291-328) — the upstream's polling loop. Note the `on_progress` callback that ours omits: agents don't need a progress bar, and any UI that does can wrap `WaitForResult` itself.
- **`anygen_backend.py::create_task`** (Python ~195-268) — the create-task body. We strip almost every parameter (operation, language, slide_count, …) because the curriculum's lesson is *the lifecycle*, not the schema. A real anygen Go port would re-add them, but each is a simple field.
- **`anygen_backend.py::get_api_key`** (Python ~63-70) — three-tier auth resolution (CLI > env > config file). We keep tiers 1-2 and drop the config file; a real harness would persist `~/.config/anygen/config.json` the same way.

Local offline copy of the first 200 lines in [`upstream-readings/s09-anygen-remote.md`](../../upstream-readings/s09-anygen-remote.md).
