// Exec layer: given (cmd, inputs, store), either replay from cache or
// run the command for real, capture its outputs into a Bundle, and Put
// it back. The boolean we return is "cache hit" — false means we
// executed fresh, true means the store handed us a recorded result.
//
// This is the bit that an agent actually calls: "run preview command X
// against input set Y; if you've seen this exact pair before, skip the
// work." Same idea as Bazel's action cache or Nix's hash-of-inputs path,
// but trimmed to one bundle = one command invocation.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Run is the canonical entry point. It computes the fingerprint, asks
// the store, and either replays or executes.
//
// On cache miss, inputs are materialized into a fresh temp directory
// before exec — the convention is that the command can reference them
// via the LEARN_S04_INPUT_DIR environment variable. We do NOT inject
// the inputs into the command line ourselves: that would change the
// fingerprint (cmdArgs would carry temp paths) and defeat the cache.
func Run(ctx context.Context, cmd []string, inputs map[string][]byte, store Store) (*Bundle, bool, error) {
	if len(cmd) == 0 {
		return nil, false, errors.New("Run: empty command")
	}
	if store == nil {
		return nil, false, errors.New("Run: nil store")
	}

	key := Fingerprint(inputs, cmd)
	if hit, ok := store.Get(key); ok {
		return hit, true, nil // cache hit — no exec
	}

	// Materialize inputs into a tempdir so a real command (think:
	// `ffmpeg -i $LEARN_S04_INPUT_DIR/clip.mp4 ...`) can find them. We
	// clean up after ourselves; the cache holds the bytes, the tempdir
	// is disposable.
	dir, err := os.MkdirTemp("", "s04-run-")
	if err != nil {
		return nil, false, fmt.Errorf("mkdtemp: %w", err)
	}
	defer os.RemoveAll(dir)
	for name, body := range inputs {
		// reject names that try to escape the tempdir; this is a content
		// hash, not a sandbox, but a path like "../etc/passwd" would be
		// a footgun
		clean := filepath.Base(name)
		if err := os.WriteFile(filepath.Join(dir, clean), body, 0o644); err != nil {
			return nil, false, fmt.Errorf("write input %s: %w", clean, err)
		}
	}

	// Execute. We capture stdout + stderr separately because both are
	// part of what an agent might want to replay (think: a tool that
	// prints a warning on stderr — the cached replay should still show
	// that warning).
	var stdout, stderr bytes.Buffer
	c := exec.CommandContext(ctx, cmd[0], cmd[1:]...)
	c.Stdout = &stdout
	c.Stderr = &stderr
	c.Env = append(os.Environ(), "LEARN_S04_INPUT_DIR="+dir)

	runErr := c.Run()
	exitCode := 0
	if runErr != nil {
		// If the command itself exited non-zero, ExitError carries the
		// code; any other error (couldn't find binary, killed by ctx)
		// is a different category and we surface it as a Go error.
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			return nil, false, fmt.Errorf("exec %s: %w", cmd[0], runErr)
		}
	}

	// Materialize a Bundle and persist it. We persist even non-zero
	// exit codes: a deterministic failure is still a cacheable result.
	// (Upstream's preview_bundle.py also stores "partial" / "error"
	// manifests for the same reason.)
	b := &Bundle{
		Key:       key,
		CreatedAt: time.Now().UTC(),
		CmdArgs:   append([]string(nil), cmd...),
		Files:     copyInputs(inputs),
		Stdout:    stdout.String(),
		Stderr:    stderr.String(),
		ExitCode:  exitCode,
	}
	if err := store.Put(b); err != nil {
		return nil, false, fmt.Errorf("store put: %w", err)
	}
	return b, false, nil
}

// copyInputs takes a defensive copy so a caller mutating its map after
// Run can't tamper with what we cached.
func copyInputs(in map[string][]byte) map[string][]byte {
	out := make(map[string][]byte, len(in))
	for k, v := range in {
		buf := make([]byte, len(v))
		copy(buf, v)
		out[k] = buf
	}
	return out
}
