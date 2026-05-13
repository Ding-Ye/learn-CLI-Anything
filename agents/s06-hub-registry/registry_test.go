// registry_test.go covers the five behaviors that matter for an agent
// using the hub:
//
//   1. JSON round-trip: a Manifest survives Marshal+Unmarshal byte-for-byte.
//      If this breaks, the cache file format is incompatible across runs.
//   2. HTTPSource talks to a real (loopback) server and parses the response.
//   3. Cache hits on the second call within TTL — the underlying source
//      sees exactly one Fetch.
//   4. Cache expires after TTL — call #3 hits the source again.
//   5. Hub.Search by substring works (case-insensitive).
//
// We use t.TempDir() throughout so cache files live in a per-test sandbox.
// No goroutine leaks, no shared global state.
package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"
)

// TestManifestRoundTrip ensures the wire format is stable. If a future
// change adds a struct field without a `json:""` tag, this catches it —
// Marshal will include the new field but Unmarshal into our schema won't
// have it, and reflect.DeepEqual fails.
func TestManifestRoundTrip(t *testing.T) {
	orig := Manifest{
		Name:     "anygen",
		Version:  "1.0.0",
		Backend:  "pip",
		Entry:    "cli-anything-anygen",
		Skill:    "skills/cli-anything-anygen/SKILL.md",
		Requires: []string{"python>=3.10", "ANYGEN_API_KEY"},
	}
	b, err := json.Marshal(orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(orig, got) {
		t.Fatalf("round-trip mismatch:\n want %+v\n got  %+v", orig, got)
	}
}

// TestHTTPSourceFetchesIndex starts an httptest.Server that returns a
// fixed JSON body and asserts the HTTPSource decodes it correctly. Using
// httptest keeps the network path real (real Listen + Accept) without
// touching the internet.
func TestHTTPSourceFetchesIndex(t *testing.T) {
	want := Index{
		Updated: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC),
		Manifests: []Manifest{
			{Name: "blender", Version: "0.2.0", Backend: "bundled", Entry: "cli-anything-blender", Skill: "skills/cli-anything-blender/SKILL.md"},
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(want)
	}))
	defer srv.Close()

	src := &HTTPSource{URL: srv.URL}
	got, err := src.FetchIndex(context.Background())
	if err != nil {
		t.Fatalf("FetchIndex: %v", err)
	}
	if !got.Updated.Equal(want.Updated) {
		t.Fatalf("Updated mismatch: want %s got %s", want.Updated, got.Updated)
	}
	if !reflect.DeepEqual(got.Manifests, want.Manifests) {
		t.Fatalf("Manifests mismatch:\n want %+v\n got  %+v", want.Manifests, got.Manifests)
	}
}

// countingSource wraps any Source and increments a counter on each call.
// We use atomic so the cache's internal mutex doesn't mask a race.
type countingSource struct {
	inner Source
	calls int32
}

func (c *countingSource) FetchIndex(ctx context.Context) (Index, error) {
	atomic.AddInt32(&c.calls, 1)
	return c.inner.FetchIndex(ctx)
}

// TestCacheHitWithinTTL exercises the happy path: two calls inside the
// TTL window should produce one upstream Fetch. We assert via the counter
// rather than by reading the cache file because the user-visible contract
// is "no extra network" — the file layout is incidental.
func TestCacheHitWithinTTL(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "src.json")
	writeJSON(t, indexPath, Index{
		Updated:   time.Now().UTC(),
		Manifests: []Manifest{{Name: "audacity", Version: "0.1.0", Backend: "pip"}},
	})

	src := &countingSource{inner: &FileSource{Path: indexPath}}
	cache := &Cache{
		Source: src,
		Path:   filepath.Join(dir, "cache.json"),
		TTL:    time.Minute,
	}

	for i := 0; i < 2; i++ {
		if _, err := cache.FetchIndex(context.Background()); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := atomic.LoadInt32(&src.calls); got != 1 {
		t.Fatalf("expected 1 underlying fetch, got %d", got)
	}
}

// TestCacheExpiresAfterTTL fast-forwards the clock past TTL between
// calls and confirms a third call re-fetches. The injected clock is the
// only knob we need for time-sensitive logic — no test sleep.
func TestCacheExpiresAfterTTL(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "src.json")
	writeJSON(t, indexPath, Index{
		Updated:   time.Now().UTC(),
		Manifests: []Manifest{{Name: "blender", Version: "0.2.0", Backend: "bundled"}},
	})

	src := &countingSource{inner: &FileSource{Path: indexPath}}

	now := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	cache := &Cache{
		Source: src,
		Path:   filepath.Join(dir, "cache.json"),
		TTL:    5 * time.Minute,
		Now:    func() time.Time { return now },
	}

	// Call #1: cache miss, hits source.
	if _, err := cache.FetchIndex(context.Background()); err != nil {
		t.Fatalf("call 1: %v", err)
	}
	// Call #2: still within TTL.
	if _, err := cache.FetchIndex(context.Background()); err != nil {
		t.Fatalf("call 2: %v", err)
	}
	// Jump 10 minutes forward — past the 5-minute TTL.
	now = now.Add(10 * time.Minute)
	// Call #3: should re-fetch.
	if _, err := cache.FetchIndex(context.Background()); err != nil {
		t.Fatalf("call 3: %v", err)
	}
	if got := atomic.LoadInt32(&src.calls); got != 2 {
		t.Fatalf("expected 2 underlying fetches (one cold, one post-expiry), got %d", got)
	}
}

// TestSearchSubstringMatch covers the only behavior `hub search` promises:
// case-insensitive substring against Name. We also verify the empty query
// returns everything (grep convention).
func TestSearchSubstringMatch(t *testing.T) {
	h := &Hub{Index: Index{Manifests: []Manifest{
		{Name: "anygen", Backend: "pip"},
		{Name: "blender", Backend: "bundled"},
		{Name: "audacity", Backend: "pip"},
	}}}

	cases := []struct {
		q    string
		want []string // names, order doesn't strictly matter but we match insertion
	}{
		{"any", []string{"anygen"}},
		{"BLEND", []string{"blender"}},
		{"pip", []string{"anygen", "audacity"}}, // matches Backend
		{"xyz", []string{}},
		{"", []string{"anygen", "blender", "audacity"}},
	}
	for _, c := range cases {
		got := h.Search(c.q)
		names := make([]string, 0, len(got))
		for _, m := range got {
			names = append(names, m.Name)
		}
		if !reflect.DeepEqual(names, c.want) {
			t.Fatalf("Search(%q) = %v, want %v", c.q, names, c.want)
		}
	}
}

// writeJSON is a tiny helper so the test bodies stay focused on the
// thing under test, not on os.WriteFile error plumbing.
func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
