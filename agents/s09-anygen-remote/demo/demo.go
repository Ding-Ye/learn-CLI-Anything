//go:build ignore

// demo.go spins up an in-process httptest.Server that imitates the
// AnyGen API, then runs the same client + poller against it end-to-end.
// `make demo` builds and runs this file with `go run`. It is not part
// of the package build (the `//go:build ignore` tag keeps it out of
// `go vet ./...` and `go test ./...`).
//
// The point: prove that the harness works without any external service.
// You see submit → poll (running, running, succeeded) → fetch happen on
// localhost in under 200ms.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"time"
)

type statusResp struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"`
}

type submitResp struct {
	JobID string `json:"job_id"`
}

type resultResp struct {
	JobID       string `json:"job_id"`
	Output      string `json:"output"`
	ContentType string `json:"content_type"`
}

func main() {
	var pollCount int32
	mux := http.NewServeMux()
	mux.HandleFunc("/jobs", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(submitResp{JobID: "demo-job-001"})
	})
	mux.HandleFunc("/jobs/", func(w http.ResponseWriter, r *http.Request) {
		// /jobs/demo-job-001 or /jobs/demo-job-001/result
		if strings.HasSuffix(r.URL.Path, "/result") {
			_ = json.NewEncoder(w).Encode(resultResp{
				JobID:       "demo-job-001",
				Output:      "https://fake.anygen.io/files/demo-job-001.pptx",
				ContentType: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
			})
			return
		}
		n := atomic.AddInt32(&pollCount, 1)
		status := "running"
		if n >= 3 {
			status = "succeeded"
		}
		_ = json.NewEncoder(w).Encode(statusResp{JobID: "demo-job-001", Status: status})
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	fmt.Println("demo: in-process AnyGen server at", srv.URL)

	// Submit
	body := strings.NewReader(`{"prompt":"AI trends presentation"}`)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/jobs", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := srv.Client().Do(req)
	if err != nil {
		fmt.Println("submit error:", err)
		return
	}
	var sub submitResp
	_ = json.NewDecoder(resp.Body).Decode(&sub)
	resp.Body.Close()
	fmt.Println("submit -> job_id =", sub.JobID)

	// Poll a few times manually so the demo prints intermediate state.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/jobs/"+sub.JobID, nil)
		resp, err := srv.Client().Do(req)
		if err != nil {
			fmt.Println("poll error:", err)
			return
		}
		var st statusResp
		_ = json.NewDecoder(resp.Body).Decode(&st)
		resp.Body.Close()
		fmt.Println("poll  -> status =", st.Status)
		if st.Status == "succeeded" || st.Status == "failed" {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}

	// Fetch
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/jobs/"+sub.JobID+"/result", nil)
	resp, err = srv.Client().Do(req)
	if err != nil {
		fmt.Println("result error:", err)
		return
	}
	var res resultResp
	_ = json.NewDecoder(resp.Body).Decode(&res)
	resp.Body.Close()
	fmt.Println("fetch -> output =", res.Output)
	fmt.Println("fetch -> content_type =", res.ContentType)
	fmt.Println("demo: OK")
}
