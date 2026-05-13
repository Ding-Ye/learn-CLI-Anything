// publish_test.go drives the pipeline against a temp src/ directory laid
// out as two fake plugins:
//
//	src/
//	  plugin-a/
//	    SKILL.md          (with valid front-matter, name: plugin-a, version: 1.0.0)
//	  plugin-b/
//	    SKILL.md          (with valid front-matter, name: plugin-b, version: 0.2.0)
//	    backend.txt       ("pip")
//
// One test then mutates the layout to remove plugin-b's SKILL.md so we
// can exercise the validation failure path.
//
// Every test builds a fresh srcDir + outDir under t.TempDir() so the
// suite is parallel-safe and idempotent.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture writes a two-plugin src tree under root and returns its path.
// The shape is the minimum the publisher needs to exercise every step.
func fixture(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join(root, "src")
	mustMkdir(t, filepath.Join(src, "plugin-a"))
	mustWrite(t, filepath.Join(src, "plugin-a", "SKILL.md"), pluginAFront)
	mustMkdir(t, filepath.Join(src, "plugin-b"))
	mustWrite(t, filepath.Join(src, "plugin-b", "SKILL.md"), pluginBFront)
	mustWrite(t, filepath.Join(src, "plugin-b", "backend.txt"), "pip\n")
	// also drop a non-plugin sibling to confirm scan skips it
	mustMkdir(t, filepath.Join(src, "docs"))
	mustWrite(t, filepath.Join(src, "docs", "README.md"), "not a plugin\n")
	return src
}

const pluginAFront = `---
name: plugin-a
version: 1.0.0
description: First fake plugin
---

# plugin-a

Body markdown.
`

const pluginBFront = `---
name: plugin-b
version: 0.2.0
description: Second fake plugin
---

# plugin-b

Body markdown.
`

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// TestScanPluginsDiscoversSkillDirs confirms ScanPlugins picks up both
// plugin subdirs and skips the non-plugin sibling. We assert on Name+
// Version because those come from the front-matter, not the directory
// name, so they prove readSkillFront ran.
func TestScanPluginsDiscoversSkillDirs(t *testing.T) {
	src := fixture(t, t.TempDir())
	p := NewPipeline()
	step, err := p.ScanPlugins(src)
	if err != nil {
		t.Fatalf("ScanPlugins: %v", err)
	}
	if !step.OK {
		t.Fatalf("scan reported not OK: %+v", step)
	}
	if len(p.Plugins) != 2 {
		t.Fatalf("want 2 plugins, got %d: %+v", len(p.Plugins), p.Plugins)
	}
	want := map[string]string{"plugin-a": "1.0.0", "plugin-b": "0.2.0"}
	for _, m := range p.Plugins {
		if want[m.Name] != m.Version {
			t.Fatalf("plugin %q: want version %q, got %q", m.Name, want[m.Name], m.Version)
		}
	}
	// backend.txt drives plugin-b's backend; plugin-a falls back to bundled.
	by := map[string]Manifest{}
	for _, m := range p.Plugins {
		by[m.Name] = m
	}
	if by["plugin-a"].Backend != "bundled" {
		t.Fatalf("plugin-a backend: want bundled, got %q", by["plugin-a"].Backend)
	}
	if by["plugin-b"].Backend != "pip" {
		t.Fatalf("plugin-b backend: want pip, got %q", by["plugin-b"].Backend)
	}
}

// TestValidateFlagsMissingSkill removes plugin-b's SKILL.md after the
// scan and runs Validate. The point is to confirm Validate alone (not
// Scan) catches the corruption — useful because a CI run might fail
// mid-flight between the two steps.
func TestValidateFlagsMissingSkill(t *testing.T) {
	root := t.TempDir()
	src := fixture(t, root)
	p := NewPipeline()
	if _, err := p.ScanPlugins(src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	// Sabotage plugin-b's SKILL.md *after* the scan so Plugins still
	// includes it but Validate has to flag the missing file.
	if err := os.Remove(filepath.Join(src, "plugin-b", "SKILL.md")); err != nil {
		t.Fatalf("remove SKILL.md: %v", err)
	}
	step, err := p.Validate(src)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if step.OK {
		t.Fatalf("expected validate to fail, got OK=true: %+v", step)
	}
	joined := strings.Join(step.Errors, "\n")
	if !strings.Contains(joined, "plugin-b") {
		t.Fatalf("expected error mentioning plugin-b, got: %s", joined)
	}
}

// TestBundleProducesTarGz runs scan + bundle and confirms the tar.gz
// for plugin-a actually opens and contains SKILL.md at the expected
// in-archive path ("plugin-a/SKILL.md").
func TestBundleProducesTarGz(t *testing.T) {
	root := t.TempDir()
	src := fixture(t, root)
	out := filepath.Join(root, "out")
	p := NewPipeline()
	if _, err := p.ScanPlugins(src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	step, err := p.Bundle(src, out)
	if err != nil {
		t.Fatalf("bundle: %v", err)
	}
	if !step.OK {
		t.Fatalf("bundle reported not OK: %+v", step)
	}
	tarball := filepath.Join(out, "plugin-a-1.0.0.tar.gz")
	if _, err := os.Stat(tarball); err != nil {
		t.Fatalf("expected %s to exist: %v", tarball, err)
	}
	// Confirm the tarball actually contains plugin-a/SKILL.md.
	found := tarballContains(t, tarball, "plugin-a/SKILL.md")
	if !found {
		t.Fatalf("expected plugin-a/SKILL.md inside %s", tarball)
	}
}

// TestSignProducesSidecar runs scan+bundle+sign and confirms the .sha256
// sidecar exists, is 64-char hex followed by "  <artifact>", and that
// the hex matches a re-computation on the same file.
func TestSignProducesSidecar(t *testing.T) {
	root := t.TempDir()
	src := fixture(t, root)
	out := filepath.Join(root, "out")
	p := NewPipeline()
	if _, err := p.ScanPlugins(src); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if _, err := p.Bundle(src, out); err != nil {
		t.Fatalf("bundle: %v", err)
	}
	step, err := p.Sign(out)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !step.OK {
		t.Fatalf("sign reported not OK: %+v", step)
	}
	sidecar := filepath.Join(out, "plugin-a-1.0.0.tar.gz.sha256")
	b, err := os.ReadFile(sidecar)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	parts := strings.Fields(string(b))
	if len(parts) != 2 {
		t.Fatalf("sidecar format: want 2 fields, got %d: %q", len(parts), string(b))
	}
	if len(parts[0]) != 64 {
		t.Fatalf("expected 64-char hex digest, got %d chars: %q", len(parts[0]), parts[0])
	}
	if parts[1] != "plugin-a-1.0.0.tar.gz" {
		t.Fatalf("sidecar filename: want plugin-a-1.0.0.tar.gz, got %q", parts[1])
	}
	// Re-compute to be sure the sidecar is honest.
	got, err := sha256File(filepath.Join(out, "plugin-a-1.0.0.tar.gz"))
	if err != nil {
		t.Fatalf("recompute sha256: %v", err)
	}
	if got != parts[0] {
		t.Fatalf("sidecar hash mismatch: file=%s, sidecar=%s", got, parts[0])
	}
}

// TestUpdateIndexEmitsValidRegistry runs the full pipeline and parses
// the resulting registry.json. We check shape (meta + clis), plugin
// count, sorted-by-name order, and that each entry has a non-empty
// sha256 — i.e. UpdateIndex actually read the sidecars Sign wrote.
func TestUpdateIndexEmitsValidRegistry(t *testing.T) {
	root := t.TempDir()
	src := fixture(t, root)
	out := filepath.Join(root, "out")
	p := NewPipeline()
	rep, err := p.Run(context.Background(), src, out)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !rep.OK {
		t.Fatalf("pipeline not OK: %+v", rep.Steps)
	}
	b, err := os.ReadFile(filepath.Join(out, "registry.json"))
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}
	var rf registryFile
	if err := json.Unmarshal(b, &rf); err != nil {
		t.Fatalf("parse registry: %v", err)
	}
	if got, want := rf.Meta["plugin_count"], float64(2); got != want {
		t.Fatalf("plugin_count: want %v, got %v", want, got)
	}
	if len(rf.CLIs) != 2 {
		t.Fatalf("clis length: want 2, got %d", len(rf.CLIs))
	}
	if rf.CLIs[0].Name != "plugin-a" || rf.CLIs[1].Name != "plugin-b" {
		t.Fatalf("clis order: want plugin-a then plugin-b, got %q and %q", rf.CLIs[0].Name, rf.CLIs[1].Name)
	}
	for _, e := range rf.CLIs {
		if len(e.SHA256) != 64 {
			t.Fatalf("entry %s: sha256 not 64 chars: %q", e.Name, e.SHA256)
		}
		if e.Artifact == "" {
			t.Fatalf("entry %s: artifact empty", e.Name)
		}
	}
}

// tarballContains opens a .tar.gz and walks its headers looking for a
// path matching `target`. We inline the read here (instead of in a
// helpers.go) because only one test cares about archive contents.
func tarballContains(t *testing.T, path, target string) bool {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open tarball: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		if hdr.Name == target {
			return true
		}
	}
	return false
}
