// Bundle layer: content-addressed cache of "the result of running CMD on
// these INPUTS". Mirrors the upstream `cli-anything-plugin/preview_bundle.py`
// pattern (sha256 of canonical-JSON of inputs+args), trimmed to what an
// agent harness actually needs.
//
// Upstream's preview_bundle.py also stores manifest.json + summary.json
// sidecars and supports multi-bundle workspaces; we keep one bundle = one
// JSON blob on disk because the agent flow only cares "did the inputs
// change since we last ran this?" — a single key per result is enough.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Bundle is what a cache stores. The Key is sha256(inputs+cmdArgs) and is
// the only identity that matters — CreatedAt is informational, the rest
// is what an agent wants to replay.
type Bundle struct {
	Key       string            `json:"key"`
	CreatedAt time.Time         `json:"created_at"`
	CmdArgs   []string          `json:"cmd_args"`
	Files     map[string][]byte `json:"files"` // input file basename → content (small inputs only)
	Stdout    string            `json:"stdout"`
	Stderr    string            `json:"stderr"`
	ExitCode  int               `json:"exit_code"`
}

// Store is the cache abstraction. Two implementations: MemStore (tests +
// short-lived processes) and DiskStore (persistent, under
// ~/.cache/learn-cli-anything-s04/). Both honor the same contract:
// Get returns (bundle, true) on hit; Put never overwrites a hit with a
// stale entry (the key IS the content, so any replay is by definition
// equivalent).
type Store interface {
	Get(key string) (*Bundle, bool)
	Put(b *Bundle) error
}

// Fingerprint computes the cache key from (inputs, cmdArgs).
//
// Canonical-JSON encoding is critical: maps must serialize in a
// deterministic key order, or `Run(cmd, {"a":1,"b":2})` and
// `Run(cmd, {"b":2,"a":1})` would hash differently — they MUST hash the
// same, because the file content is identical. We sort keys explicitly
// instead of relying on encoding/json's iteration order (which is
// already sorted for maps as of Go 1.12, but we don't want to depend on
// implementation detail for a content hash).
func Fingerprint(inputs map[string][]byte, cmdArgs []string) string {
	// Sort input names for deterministic order.
	names := make([]string, 0, len(inputs))
	for k := range inputs {
		names = append(names, k)
	}
	sort.Strings(names)

	// Build a stable shape: [{name, sha256_of_content}], plus cmdArgs.
	// We hash content rather than embedding it — keeps the fingerprint
	// short even for big inputs, and the cache value still has the raw
	// bytes if you need to inspect them.
	type entry struct {
		Name string `json:"name"`
		Hash string `json:"hash"`
	}
	entries := make([]entry, len(names))
	for i, n := range names {
		sum := sha256.Sum256(inputs[n])
		entries[i] = entry{Name: n, Hash: hex.EncodeToString(sum[:])}
	}

	canon := struct {
		Inputs []entry  `json:"inputs"`
		Cmd    []string `json:"cmd"`
	}{Inputs: entries, Cmd: cmdArgs}

	buf, _ := json.Marshal(canon) // structs serialize in field order — deterministic by definition
	sum := sha256.Sum256(buf)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// MemStore is an in-memory Store with optional LRU-by-insertion eviction.
// Useful for tests and for the cache miss flag in exec.go to be
// observable without touching the disk.
type MemStore struct {
	mu      sync.Mutex
	cap     int                // 0 = unbounded
	order   []string           // insertion order, oldest first
	entries map[string]*Bundle // key → bundle
}

// NewMemStore returns a memstore with capacity (0 = unbounded). When cap
// is positive and adding a new entry would exceed it, the oldest entry is
// evicted. Matches what an agent loop with bounded memory needs.
func NewMemStore(cap int) *MemStore {
	return &MemStore{cap: cap, entries: map[string]*Bundle{}}
}

func (m *MemStore) Get(key string) (*Bundle, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.entries[key]
	return b, ok
}

func (m *MemStore) Put(b *Bundle) error {
	if b == nil || b.Key == "" {
		return errors.New("bundle must have a key")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.entries[b.Key]; exists {
		// re-insert is a no-op; the key IS the content
		return nil
	}
	m.entries[b.Key] = b
	m.order = append(m.order, b.Key)
	if m.cap > 0 && len(m.order) > m.cap {
		// evict oldest
		old := m.order[0]
		m.order = m.order[1:]
		delete(m.entries, old)
	}
	return nil
}

// Len reports how many entries are live. Test-only helper.
func (m *MemStore) Len() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// DiskStore is an on-disk Store. One file per bundle, keyed by the
// hex-of-sha256 portion of Key (we drop the "sha256:" prefix to keep
// filenames short). Encoding is JSON for readability + portability —
// gob would be smaller but you couldn't `cat` a cache entry.
type DiskStore struct {
	mu   sync.Mutex
	root string
}

// NewDiskStore returns a DiskStore rooted at the given directory (it is
// created on demand). Pass "" to use ~/.cache/learn-cli-anything-s04/.
func NewDiskStore(root string) (*DiskStore, error) {
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("home dir: %w", err)
		}
		root = filepath.Join(home, ".cache", "learn-cli-anything-s04")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", root, err)
	}
	return &DiskStore{root: root}, nil
}

// Root reports where the disk store lives. Useful for the `preview show`
// command which needs to pretty-print the path.
func (d *DiskStore) Root() string { return d.root }

func (d *DiskStore) path(key string) string {
	// Strip the "sha256:" tag if present so the filename is just the hex.
	tag := key
	if len(key) > 7 && key[:7] == "sha256:" {
		tag = key[7:]
	}
	return filepath.Join(d.root, tag+".json")
}

func (d *DiskStore) Get(key string) (*Bundle, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	buf, err := os.ReadFile(d.path(key))
	if err != nil {
		return nil, false
	}
	var b Bundle
	if err := json.Unmarshal(buf, &b); err != nil {
		// A corrupt file is treated as a miss, not an error: the next
		// Put will overwrite it. Matches upstream's "skip unreadable
		// manifest" behavior.
		return nil, false
	}
	return &b, true
}

func (d *DiskStore) Put(b *Bundle) error {
	if b == nil || b.Key == "" {
		return errors.New("bundle must have a key")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	buf, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal bundle: %w", err)
	}
	// Write to a temp file then rename, so a partial write never produces
	// a corrupt cache entry. POSIX rename is atomic on the same FS.
	p := d.path(b.Key)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}
