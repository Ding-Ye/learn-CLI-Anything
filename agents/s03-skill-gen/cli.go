// Package main — s03 skill generator.
//
// CLI / Flag / Result are re-declared here (instead of imported from s01)
// because every session in this curriculum is its own Go module with no
// cross-imports. The struct shape MUST stay byte-compatible with s01's
// declaration: that is the whole reason s03 can introspect any harness
// that targets the HARNESS contract.
package main

import (
	"context"
)

// CLI is the recursive command-tree node — same shape as s01.
type CLI struct {
	Name        string
	Help        string
	Flags       []Flag
	Subcommands map[string]*CLI
	Run         func(ctx context.Context, args []string) (any, error)
}

// Flag is a per-subcommand flag declaration.
type Flag struct {
	Name     string
	Type     string // "string" | "int" | "bool"
	Default  any
	Required bool
	Help     string
}

// Result is the JSON envelope an agent sees.
type Result struct {
	OK    bool   `json:"ok"`
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}
