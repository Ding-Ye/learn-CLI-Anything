// client_test.go drives the harness against an in-process httptest.Server.
// No network, no fixtures — every test spins up a tiny handler that
// pretends to be /jobs, /jobs/:id, /jobs/:id/result. This is exactly the
// pattern the upstream uses for its `tests/test_full_e2e.py`, ported
// down to Go's stdlib httptest.
//
// The five tests cover the public contract:
//
//  1. SubmitJob returns the parsed job_id.
//  2. PollStatus returns the right status.
//  3. WaitForResult polls until "succeeded" then returns the result.
//  4. WaitForResult bails out cleanly when status flips to "failed".
//  5. WaitForResult respects context cancellation (timeout).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires an APIClient against the given handler. The
// returned cleanup func closes the server. We don't use t.Cleanup here
// because the tests are tiny and the explicit defer is easier to read.
func newTestClient(handler http.Handler) (*APIClient, func()) {
	srv := httptest.NewServer(handler)
	c := &APIClient{
		BaseURL:    srv.URL,
		APIKey:     "test-key",
		HTTPClient: srv.Client(),
	}
	return c, srv.Close
}

func TestSubmitJob_ReturnsJobID(t *testing.T) {
	c, stop := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/jobs" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("auth header = %q", got)
		}
		var body jobCreateRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body.Prompt != "hello world" {
			t.Fatalf("prompt = %q", body.Prompt)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jobCreateResponse{JobID: "job-42"})
	}))
	defer stop()

	id, err := c.SubmitJob(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("SubmitJob: %v", err)
	}
	if id != "job-42" {
		t.Fatalf("jobID = %q, want job-42", id)
	}
}

func TestPollStatus_ReturnsStatus(t *testing.T) {
	c, stop := newTestClient(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s", r.Method)
		}
		if !strings.HasPrefix(r.URL.Path, "/jobs/") {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(jobStatusResponse{
			JobID:  "job-7",
			Status: StatusRunning,
		})
	}))
	defer stop()

	s, err := c.PollStatus(context.Background(), "job-7")
	if err != nil {
		t.Fatalf("PollStatus: %v", err)
	}
	if s != StatusRunning {
		t.Fatalf("status = %q, want %q", s, StatusRunning)
	}
}

// TestWaitForResult_Succeeds drives the full poll loop: the first two
// /jobs/:id GETs return "running", the third returns "succeeded", and
// then /jobs/:id/result returns a payload. WaitForResult should return
// that payload with no error.
func TestWaitForResult_Succeeds(t *testing.T) {
	var statusHits int32
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/job-ok", func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&statusHits, 1)
		status := StatusRunning
		if n >= 3 {
			status = StatusSucceeded
		}
		_ = json.NewEncoder(w).Encode(jobStatusResponse{JobID: "job-ok", Status: status})
	})
	mux.HandleFunc("/jobs/job-ok/result", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(JobResult{
			JobID:       "job-ok",
			Output:      "slide-deck://job-ok",
			ContentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		})
	})
	c, stop := newTestClient(mux)
	defer stop()

	rs, err := WaitForResult(context.Background(), c, "job-ok", 5*time.Millisecond)
	if err != nil {
		t.Fatalf("WaitForResult: %v", err)
	}
	if rs.Status != StatusSucceeded {
		t.Fatalf("status = %q, want succeeded", rs.Status)
	}
	if rs.Result.Output != "slide-deck://job-ok" {
		t.Fatalf("output = %q", rs.Result.Output)
	}
	if got := atomic.LoadInt32(&statusHits); got < 3 {
		t.Fatalf("statusHits = %d, want >= 3", got)
	}
}

// TestWaitForResult_Fails proves Rule 1 of poller.go: a "failed" status
// short-circuits with a non-nil error and doesn't call /result.
func TestWaitForResult_Fails(t *testing.T) {
	var resultCalled int32
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/job-bad", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jobStatusResponse{
			JobID:  "job-bad",
			Status: StatusFailed,
			Error:  "out of quota",
		})
	})
	mux.HandleFunc("/jobs/job-bad/result", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&resultCalled, 1)
		w.WriteHeader(http.StatusInternalServerError)
	})
	c, stop := newTestClient(mux)
	defer stop()

	rs, err := WaitForResult(context.Background(), c, "job-bad", 5*time.Millisecond)
	if err == nil {
		t.Fatalf("expected error, got nil (status=%q)", rs.Status)
	}
	if rs.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", rs.Status)
	}
	if !strings.Contains(err.Error(), "failed") {
		t.Fatalf("error = %q, want it to mention failure", err.Error())
	}
	if n := atomic.LoadInt32(&resultCalled); n != 0 {
		t.Fatalf("result endpoint called %d times; expected 0 for failed job", n)
	}
}

// TestWaitForResult_RespectsTimeout proves Rule 2: a cancelled context
// preempts the polling loop. The server always says "running"; we give
// the caller 50ms and expect ctx.DeadlineExceeded back fast.
func TestWaitForResult_RespectsTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs/job-slow", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jobStatusResponse{
			JobID:  "job-slow",
			Status: StatusRunning,
		})
	})
	c, stop := newTestClient(mux)
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := WaitForResult(ctx, c, "job-slow", 20*time.Millisecond)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want context.DeadlineExceeded", err)
	}
	// Allow generous slack — CI machines can be slow. The point is we
	// didn't wait for the full default 20m timeout.
	if elapsed > time.Second {
		t.Fatalf("took %v, expected < 1s", elapsed)
	}
}
