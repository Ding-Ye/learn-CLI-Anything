// poller.go is the piece a GUI harness doesn't have.
//
// A local-GUI harness (s01..s05 style) returns when the subprocess exits.
// A remote-API harness doesn't: the server is doing the work, and the
// only way to know it's finished is to ask. WaitForResult is that ask —
// it polls /jobs/:id on a fixed interval until the status is terminal
// (succeeded or failed) or the context is cancelled.
//
// Three rules the poller follows:
//
//  1. Fail fast on terminal "failed". Don't keep polling — the server's
//     answer won't change, and an agent that retries failed jobs in a
//     loop is the worst possible UX.
//  2. Respect context cancellation. ctx.Done() preempts the sleep AND
//     the next HTTP round-trip; an agent that times out has to be able
//     to actually bail.
//  3. Poll once *before* sleeping. If the job is already done by the
//     time we start (cached state, instant job), we don't waste the
//     first interval.
package main

import (
	"context"
	"fmt"
	"time"
)

// ResultStatus bundles a JobResult with the terminal Status that produced
// it. For "succeeded" the JobResult is the payload; for "failed" the
// JobResult is zero and the caller reads Status (and the returned error).
type ResultStatus struct {
	Status Status
	Result JobResult
}

// WaitForResult polls /jobs/:id every interval until terminal.
//
// Returns:
//   - (ResultStatus{StatusSucceeded, result}, nil) on success
//   - (ResultStatus{StatusFailed, _}, error)       on failure
//   - (zero, ctx.Err())                            on cancellation / timeout
//
// The error path distinguishes "the job failed" from "we gave up waiting"
// — both are valid outcomes a CLI surfaces differently. interval=0 is
// treated as 1 second so callers can pass zero-value structs in tests
// without accidentally busy-looping.
func WaitForResult(ctx context.Context, c *APIClient, jobID string, interval time.Duration) (ResultStatus, error) {
	if interval <= 0 {
		interval = time.Second
	}
	// Rule 3: poll once before the first sleep.
	for {
		status, err := c.PollStatus(ctx, jobID)
		if err != nil {
			// Context cancellation surfaces here as a wrapped
			// net/http error; unwrap so the caller sees ctx.Err()
			// directly rather than a noisy URL string.
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
			// Rule 1: terminal failure short-circuits. We could
			// fetch /result for an error blob, but the upstream
			// convention is that /jobs/:id already carries the
			// failure reason in its body. Surface a generic error
			// — callers that want details can call PollStatus
			// themselves and inspect the raw response.
			return ResultStatus{Status: status}, fmt.Errorf("job %s failed", jobID)
		}

		// Non-terminal: sleep then retry. Rule 2: ctx.Done() preempts.
		select {
		case <-ctx.Done():
			return ResultStatus{}, ctx.Err()
		case <-time.After(interval):
		}
	}
}
