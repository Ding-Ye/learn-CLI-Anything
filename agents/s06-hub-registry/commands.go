// commands.go is the query layer that sits on top of an Index. The three
// public functions (Search / List / Info) match the upstream registry's
// trio of search_clis / fetch_all_clis / get_cli — same names, same shape,
// minus the Python-isms.
//
// Why these are functions and not methods on Index: an agent wants to
// pipe `hub list | hub info <name>` style queries from a single Index it
// already fetched. Free functions make that composition obvious; methods
// would invite a builder pattern we don't need.
package main

import (
	"fmt"
	"strings"
)

// Hub is a thin facade that lets the CLI layer hold one object instead of
// passing an Index around. It also lets us add caching/refresh semantics
// later (e.g. a Reload() method) without touching call sites.
type Hub struct {
	Index Index
}

// Search returns every Manifest whose Name contains query (case-insensitive
// substring). The upstream also matches against Description and Category;
// our Manifest struct doesn't carry those fields (the curriculum kept the
// schema minimal), so we match Name and Backend.
//
// An empty query returns *all* manifests — same convention as `grep ""`.
// Callers who want "no results for empty query" should pre-check.
func (h *Hub) Search(query string) []Manifest {
	q := strings.ToLower(strings.TrimSpace(query))
	out := make([]Manifest, 0, len(h.Index.Manifests))
	for _, m := range h.Index.Manifests {
		if q == "" {
			out = append(out, m)
			continue
		}
		if strings.Contains(strings.ToLower(m.Name), q) ||
			strings.Contains(strings.ToLower(m.Backend), q) {
			out = append(out, m)
		}
	}
	return out
}

// List is "give me everything." We return a copy of the slice header so
// callers can't sort/append into our backing array unexpectedly. The
// underlying Manifest values are shared (no deep copy) — they're
// effectively immutable once decoded from JSON.
func (h *Hub) List() []Manifest {
	out := make([]Manifest, len(h.Index.Manifests))
	copy(out, h.Index.Manifests)
	return out
}

// Info looks up a single Manifest by Name (case-insensitive). Returns a
// pointer so callers can distinguish "not found" (nil) from "found with
// zero-value fields" — relevant once Requires gets compared to empty slice.
//
// We return *Manifest, not Manifest, because that's the idiomatic Go way
// to express "may be absent." An (*Manifest, error) signature would let
// us distinguish reasons-for-absence, but right now the only reason is
// "not in index" — overkill to ship as an error type.
func (h *Hub) Info(name string) (*Manifest, error) {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" {
		return nil, fmt.Errorf("name is empty")
	}
	for i := range h.Index.Manifests {
		if strings.ToLower(h.Index.Manifests[i].Name) == n {
			return &h.Index.Manifests[i], nil
		}
	}
	return nil, fmt.Errorf("manifest not found: %s", name)
}
