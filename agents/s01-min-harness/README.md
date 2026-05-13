# s01 — minimum harness

Smallest Go program satisfying CLI-Anything's HARNESS contract: a CLI with
subcommands, `--json` mode for machine output, and a JSON envelope that
includes an `error` field on failure.

## Run

```bash
make demo
```

Human mode prints `hello world` and the current time. `make demo-json`
shows the structured envelope.

## Files

- `cli.go` — the `CLI`, `Flag`, `Result` types + `Dispatch`.
- `main.go` — a small demo harness with `echo` and `time` subcommands.
- `cli_test.go` — 5 unit tests.

## Upstream source reading

See `docs/{zh,en}/s01-min-harness.md` for the annotated walkthrough.
