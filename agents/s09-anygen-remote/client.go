// client.go is the HTTP wrapper for a remote skill-generation service —
// modelled on `anygen/agent-harness/cli_anything/anygen/utils/anygen_backend.py`.
// The upstream uses Python's `requests`; we use net/http with a context
// on every call so cancellation/timeout work the way an agent expects.
//
// Three pieces an agent-facing remote harness needs:
//
//  1. SubmitJob — POST a prompt, get back a jobID. Synchronous.
//  2. PollStatus — GET status (queued/running/succeeded/failed). The
//     terminal states are the only ones a caller branches on.
//  3. FetchResult — GET the final artifact. Only valid after "succeeded".
//
// We deliberately keep the API surface small. The upstream backend has
// upload/prepare/download paths too; for the curriculum the lifecycle
// (submit → poll → fetch) is what matters because it's the part that
// has no GUI analogue.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Status is the four-valued state machine the remote service exposes.
// "queued" and "running" are non-terminal; "succeeded" and "failed" are.
// PollStatus and WaitForResult both treat these as opaque strings so a
// future "cancelled" state can be added upstream without breaking us.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// IsTerminal reports whether s is one of the states that stops polling.
// Centralising the predicate keeps the poller honest if a new state ever
// appears upstream.
func (s Status) IsTerminal() bool {
	return s == StatusSucceeded || s == StatusFailed
}

// APIClient is the minimal client surface a harness needs to wrap a
// remote skill-generation service. BaseURL is the service root (no
// trailing slash); APIKey is sent as a Bearer token; HTTPClient is
// caller-supplied so tests can inject httptest.Server.Client().
type APIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewAPIClient constructs a client with a sensible default timeout. Tests
// override HTTPClient after construction.
func NewAPIClient(baseURL, apiKey string) *APIClient {
	return &APIClient{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// JobResult is the body returned by /jobs/:id/result on a succeeded job.
// Output is whatever the service produces — we keep it as a plain string
// plus a content-type hint so the harness doesn't need to know about
// every artefact format the upstream supports (pptx, docx, drawio, …).
// The name is JobResult, not Result, because cli.go already owns the
// `Result` envelope (the s01-style JSON wrapper). Two `Result` types in
// one package would force one of them under an alias; cleaner to be
// explicit here.
type JobResult struct {
	JobID       string `json:"job_id"`
	Output      string `json:"output"`
	ContentType string `json:"content_type,omitempty"`
}

// jobCreateRequest is the body POST'd to /jobs. Kept private — callers
// pass a plain prompt string and SubmitJob does the framing.
type jobCreateRequest struct {
	Prompt string `json:"prompt"`
}

type jobCreateResponse struct {
	JobID string `json:"job_id"`
}

type jobStatusResponse struct {
	JobID  string `json:"job_id"`
	Status Status `json:"status"`
	Error  string `json:"error,omitempty"`
}

// SubmitJob POSTs the prompt to /jobs and returns the new jobID. Errors
// are wrapped with the HTTP status so a caller can tell auth/quota/5xx
// apart without re-doing the request.
func (c *APIClient) SubmitJob(ctx context.Context, prompt string) (string, error) {
	body, err := json.Marshal(jobCreateRequest{Prompt: prompt})
	if err != nil {
		return "", fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/jobs", newBodyReader(body))
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAuth(req)

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", httpError("submit", resp)
	}
	var out jobCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	if out.JobID == "" {
		return "", fmt.Errorf("submit: server returned empty job_id")
	}
	return out.JobID, nil
}

// PollStatus GETs /jobs/:id and returns its Status. "failed" is NOT an
// error from PollStatus's perspective — it's a perfectly valid terminal
// state. Callers that care turn it into an error themselves (see
// WaitForResult). This separation keeps the polling loop simple.
func (c *APIClient) PollStatus(ctx context.Context, jobID string) (Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/jobs/"+jobID, nil)
	if err != nil {
		return "", fmt.Errorf("new request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", httpError("status", resp)
	}
	var out jobStatusResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	return out.Status, nil
}

// FetchResult GETs /jobs/:id/result. Only valid after status = succeeded.
// The server is expected to reject other states; we don't pre-check so
// the harness stays a thin wrapper — the server is the source of truth.
func (c *APIClient) FetchResult(ctx context.Context, jobID string) (JobResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/jobs/"+jobID+"/result", nil)
	if err != nil {
		return JobResult{}, fmt.Errorf("new request: %w", err)
	}
	c.setAuth(req)

	resp, err := c.do(req)
	if err != nil {
		return JobResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return JobResult{}, httpError("result", resp)
	}
	var out JobResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return JobResult{}, fmt.Errorf("decode: %w", err)
	}
	if out.JobID == "" {
		out.JobID = jobID
	}
	return out, nil
}

func (c *APIClient) setAuth(req *http.Request) {
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
}

func (c *APIClient) do(req *http.Request) (*http.Response, error) {
	cli := c.HTTPClient
	if cli == nil {
		cli = http.DefaultClient
	}
	return cli.Do(req)
}

func httpError(op string, resp *http.Response) error {
	// Read at most 512B of the body so error messages stay readable.
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	return fmt.Errorf("%s: HTTP %d: %s", op, resp.StatusCode, string(body))
}

// newBodyReader exists so SubmitJob can pass []byte without pulling in
// bytes.NewReader at every call site — keeps the call site compact.
func newBodyReader(b []byte) *bodyReader {
	return &bodyReader{b: b}
}

type bodyReader struct {
	b   []byte
	pos int
}

func (r *bodyReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.pos:])
	r.pos += n
	return n, nil
}
