// registry.go defines the Manifest schema and the Source/Cache layering
// that backs `hub search/list/info`. The upstream's registry.py boils down
// to three jobs:
//
//   1. Fetch a JSON document from a URL (the harness registry).
//   2. Stash it on disk with a timestamp so subsequent calls don't re-hit
//      the network within a TTL window.
//   3. Fall back to the on-disk copy when the network is unavailable.
//
// We mirror that shape in Go but factor it into composable pieces:
//
//   - Source is an interface (FetchIndex), so the same Cache wrapper can
//     stack on top of an HTTPSource (prod) or a FileSource (tests + the
//     demo). The upstream collapses these two cases into one function
//     parameterized by URL; that's fine in Python but loses type-safety
//     when you want to inject a fake in a test.
//   - Cache *wraps* a Source, instead of being a third path inside the
//     fetch function. That isolates the on-disk format from the network
//     code: swap the storage and the source code stays untouched.
//
// Everything below is zero-dep (only stdlib) — the curriculum constraint.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Manifest is one CLI entry in the registry. Names match the s07 installer's
// Backend dispatch keys (pip | npm | bundled | uv) so we can hand a Manifest
// straight to that session without translation.
//
// Skill is a path relative to the install root (e.g. "skills/cli-anything-anygen/SKILL.md")
// because the upstream registry.json uses relative paths and we want to stay
// byte-compatible with that schema.
type Manifest struct {
	Name     string   `json:"name"`
	Version  string   `json:"version"`
	Backend  string   `json:"backend"`
	Entry    string   `json:"entry"`
	Skill    string   `json:"skill"`
	Requires []string `json:"requires,omitempty"`
}

// Index is the wire format: a top-level object with an Updated timestamp
// and a Manifests list. The upstream calls the list "clis"; we stick with
// "manifests" since that's what the curriculum's shared types in plan.md
// settled on.
type Index struct {
	Updated   time.Time  `json:"updated"`
	Manifests []Manifest `json:"manifests"`
}

// Source is anything that can produce an Index. Stacking a Cache on a
// Source preserves the interface, so callers don't know (or care) whether
// the bytes came from the network, a local file, or an in-memory fake.
type Source interface {
	FetchIndex(ctx context.Context) (Index, error)
}

// HTTPSource fetches the registry JSON from an arbitrary URL. We deliberately
// expose URL as a field (not a constructor argument) so tests can build
// one with the httptest.Server URL after the server starts — there's no
// reason to hide it.
type HTTPSource struct {
	URL    string
	Client *http.Client // nil → http.DefaultClient with a 15s timeout
}

// FetchIndex performs the GET and decodes the body. The 15s timeout matches
// the upstream's requests.get timeout — same network-friendliness budget.
func (h *HTTPSource) FetchIndex(ctx context.Context) (Index, error) {
	if h.URL == "" {
		return Index{}, errors.New("HTTPSource: URL is empty")
	}
	client := h.Client
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.URL, nil)
	if err != nil {
		return Index{}, fmt.Errorf("HTTPSource: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return Index{}, fmt.Errorf("HTTPSource: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Index{}, fmt.Errorf("HTTPSource: status %d from %s", resp.StatusCode, h.URL)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Index{}, fmt.Errorf("HTTPSource: read body: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(body, &idx); err != nil {
		return Index{}, fmt.Errorf("HTTPSource: decode: %w", err)
	}
	return idx, nil
}

// FileSource reads the registry from a local JSON file — the demo binary
// uses this so it works offline, and tests reach for it when they don't
// want to stand up an httptest server.
type FileSource struct {
	Path string
}

// FetchIndex opens the file and decodes it. We don't ctx-cancel a file
// read (it would race with os.Open) — local disk IO completes fast enough
// that the context wouldn't fire usefully.
func (f *FileSource) FetchIndex(_ context.Context) (Index, error) {
	if f.Path == "" {
		return Index{}, errors.New("FileSource: path is empty")
	}
	b, err := os.ReadFile(f.Path)
	if err != nil {
		return Index{}, fmt.Errorf("FileSource: %w", err)
	}
	var idx Index
	if err := json.Unmarshal(b, &idx); err != nil {
		return Index{}, fmt.Errorf("FileSource: decode: %w", err)
	}
	return idx, nil
}

// cachePayload is the on-disk envelope. We keep CachedAt out of Index so
// the Index type stays the wire format — a downstream user that re-publishes
// the cache file as a registry shouldn't accidentally leak our timestamp.
type cachePayload struct {
	CachedAt time.Time `json:"cached_at"`
	Data     Index     `json:"data"`
}

// Cache wraps a Source with a TTL-bounded disk cache. The shape mirrors
// the upstream's _fetch_json:
//
//   - If the cache file exists and is younger than TTL, return its Data.
//   - Otherwise, call the underlying Source.
//   - On Source error, fall back to the cache file *even if expired* —
//     stale data beats a hard failure for `hub list`.
//
// The Now field lets tests jump forward in time without sleeping. In
// production it stays nil and we use time.Now.
type Cache struct {
	Source Source
	Path   string        // e.g. ~/.cache/learn-cli-anything-s06/index.json
	TTL    time.Duration // 0 → never refresh from Source once the cache exists
	Now    func() time.Time

	mu sync.Mutex
}

// now returns the injected clock or time.Now. Centralizing here means the
// TTL test only has to override one field.
func (c *Cache) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// FetchIndex is the only public method. It's stateful (it touches disk)
// but idempotent: calling it twice within TTL returns the same Index.
func (c *Cache) FetchIndex(ctx context.Context) (Index, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Try the on-disk cache first. A malformed cache file is treated the
	// same as a missing one — we refetch instead of erroring, because the
	// user can't fix a corrupted cache without a refetch anyway.
	cached, hasCached, _ := c.readCache()
	if hasCached && c.TTL > 0 {
		if c.now().Sub(cached.CachedAt) < c.TTL {
			return cached.Data, nil
		}
	}

	// Cache missing or expired — hit the source.
	idx, err := c.Source.FetchIndex(ctx)
	if err != nil {
		// Stale-but-present beats a hard error for offline use.
		if hasCached {
			return cached.Data, nil
		}
		return Index{}, err
	}

	if writeErr := c.writeCache(idx); writeErr != nil {
		// We got the data; surface a warning by way of an Index field?
		// No — the upstream silently swallows write errors and returns
		// the in-memory Index. We do the same to match behavior.
		_ = writeErr
	}
	return idx, nil
}

func (c *Cache) readCache() (cachePayload, bool, error) {
	if c.Path == "" {
		return cachePayload{}, false, nil
	}
	b, err := os.ReadFile(c.Path)
	if err != nil {
		return cachePayload{}, false, err
	}
	var p cachePayload
	if err := json.Unmarshal(b, &p); err != nil {
		return cachePayload{}, false, err
	}
	return p, true, nil
}

func (c *Cache) writeCache(idx Index) error {
	if c.Path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o755); err != nil {
		return err
	}
	p := cachePayload{CachedAt: c.now(), Data: idx}
	b, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(c.Path, b, 0o644)
}

// DefaultCachePath returns ~/.cache/learn-cli-anything-s06/index.json or a
// fallback under os.TempDir() if $HOME is unset. We intentionally don't
// share a path with the real upstream (~/.cli-hub/) — this is a learning
// build and shouldn't clobber a user's real cache.
func DefaultCachePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "learn-cli-anything-s06", "index.json")
	}
	return filepath.Join(home, ".cache", "learn-cli-anything-s06", "index.json")
}
