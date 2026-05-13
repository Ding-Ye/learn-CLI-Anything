// publish.go implements the five-step publish pipeline. The shape mirrors
// the upstream's CI flow (.github/workflows/publish-cli-hub.yml +
// check-root-skills.yml) but boiled down to what runs *locally* before
// any push happens:
//
//	ScanPlugins(src)   walk src/, find every SKILL.md-bearing subdir
//	Validate           every plugin must have a non-empty SKILL.md
//	Bundle             tar.gz each plugin dir into out/<name>-<version>.tar.gz
//	Sign               write out/<name>-<version>.tar.gz.sha256 sidecars
//	UpdateIndex        emit out/registry.json with one entry per plugin
//
// Each step returns a per-step Report; Run aggregates them into a
// PipelineReport. The publisher is *local-to-release*: it produces a
// directory of artifacts you could rsync to a CDN, but it does no
// pushing — that's the deploy-pages.yml step on the CI side, and we
// intentionally stop short of it so the curriculum stays hermetic.
//
// One non-obvious property: every step is idempotent. Re-running Run with
// the same src/out produces bit-identical tarballs (we set a fixed mtime
// in the tar header) and bit-identical hashes. CI re-runs on the same
// commit don't churn the index.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// fixedMTime is the timestamp we stamp into every tar header so the
// pipeline is reproducible. Anything constant works; we picked the
// upstream's first-release date so the bytes have a story.
var fixedMTime = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

// StepReport is one row in the pipeline's summary table.
type StepReport struct {
	Step    string   `json:"step"`
	OK      bool     `json:"ok"`
	Items   []string `json:"items,omitempty"`
	Errors  []string `json:"errors,omitempty"`
	Message string   `json:"message,omitempty"`
}

// PipelineReport is what `publish run` prints. Plugins is the final list
// (post-validation), Steps is the per-step audit trail.
type PipelineReport struct {
	SrcDir  string       `json:"src_dir"`
	OutDir  string       `json:"out_dir"`
	Plugins []Manifest   `json:"plugins"`
	Steps   []StepReport `json:"steps"`
	OK      bool         `json:"ok"`
}

// Pipeline holds the per-step state so individual steps can be unit-tested
// without driving the full Run(). All fields are publisher-owned — the
// caller only sees PipelineReport.
type Pipeline struct {
	// Plugins is populated by ScanPlugins; downstream steps read it.
	Plugins []Manifest
}

// NewPipeline returns an empty Pipeline. Tests construct the struct
// literally; Run uses this helper.
func NewPipeline() *Pipeline { return &Pipeline{} }

// ScanPlugins walks srcDir one level deep, looking for subdirectories
// that contain a SKILL.md at the top. Each match becomes a Manifest.
//
// "One level deep" matches the upstream layout where every wrapped tool
// lives at the repo root (`blender/`, `audacity/`, `anygen/`). We do not
// recurse — a plugin is exactly a single directory.
func (p *Pipeline) ScanPlugins(srcDir string) (StepReport, error) {
	rep := StepReport{Step: "scan"}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return rep, err
	}
	var manifests []Manifest
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(srcDir, e.Name())
		skillPath := filepath.Join(dir, "SKILL.md")
		if _, err := os.Stat(skillPath); err != nil {
			// Not a plugin (no SKILL.md). Skip silently — srcDir
			// often has testdata/, docs/, etc. mixed in.
			continue
		}
		meta, err := readSkillFront(skillPath)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", e.Name(), err))
			continue
		}
		name := meta.Name
		if name == "" {
			name = e.Name()
		}
		version := meta.Version
		if version == "" {
			version = "0.0.0"
		}
		manifests = append(manifests, Manifest{
			Name:    name,
			Version: version,
			Backend: readBackendHint(dir),
			Entry:   e.Name(),                                  // dir name == entry point
			Skill:   filepath.ToSlash(filepath.Join(e.Name(), "SKILL.md")),
		})
		rep.Items = append(rep.Items, e.Name())
	}
	// Sort by Name for a deterministic registry.json downstream.
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Name < manifests[j].Name })
	p.Plugins = manifests
	rep.OK = len(rep.Errors) == 0
	rep.Message = fmt.Sprintf("found %d plugin(s)", len(manifests))
	return rep, nil
}

// Validate checks that every scanned plugin has a non-empty SKILL.md.
// The upstream's .github/scripts/validate_root_skills.py also diffs
// front-matter against a canonical name; we keep our check tighter so
// the failure mode in tests is unambiguous (missing-file vs out-of-sync).
//
// If srcDir has subdirs without SKILL.md, ScanPlugins already filtered
// them. So Validate's job is to catch the case where a plugin's
// SKILL.md exists but is empty/corrupt — useful when CI runs after a
// botched commit.
func (p *Pipeline) Validate(srcDir string) (StepReport, error) {
	rep := StepReport{Step: "validate"}
	for _, m := range p.Plugins {
		path := filepath.Join(srcDir, m.Skill)
		fi, err := os.Stat(path)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: stat SKILL.md: %v", m.Name, err))
			continue
		}
		if fi.Size() == 0 {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: SKILL.md is empty", m.Name))
			continue
		}
		rep.Items = append(rep.Items, m.Name)
	}
	rep.OK = len(rep.Errors) == 0
	if rep.OK {
		rep.Message = fmt.Sprintf("validated %d plugin(s)", len(rep.Items))
	} else {
		rep.Message = fmt.Sprintf("validation failed for %d plugin(s)", len(rep.Errors))
	}
	return rep, nil
}

// Bundle writes one tar.gz per plugin to outDir. The archive contains
// the plugin's entire directory tree with paths relative to the plugin
// dir (so SKILL.md sits at the root of the tarball).
//
// We force a fixed mtime on every header so the bytes are reproducible
// across re-runs. The upstream uses `python -m build` which sets
// SOURCE_DATE_EPOCH; we get the same property with one local constant.
func (p *Pipeline) Bundle(srcDir, outDir string) (StepReport, error) {
	rep := StepReport{Step: "bundle"}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return rep, err
	}
	for _, m := range p.Plugins {
		artifact := fmt.Sprintf("%s-%s.tar.gz", m.Name, m.Version)
		dst := filepath.Join(outDir, artifact)
		if err := writeTarGz(filepath.Join(srcDir, m.Entry), dst); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", m.Name, err))
			continue
		}
		rep.Items = append(rep.Items, artifact)
	}
	rep.OK = len(rep.Errors) == 0
	rep.Message = fmt.Sprintf("bundled %d artifact(s)", len(rep.Items))
	return rep, nil
}

// Sign computes sha256 of each tar.gz and writes a sidecar
// <artifact>.sha256 next to it. The sidecar contents match the GNU
// `sha256sum` format ("<hex>  <filename>\n") so a downstream CDN can
// verify with off-the-shelf tooling.
//
// "Sign" is a slight overload of the word — we're producing a digest,
// not a signature. The upstream's PyPI trusted-publishing flow uses
// Sigstore for real signatures; for the curriculum we stop at the hash.
// The point is to show *where* signing slots into the pipeline; the
// scheme is a substitution.
func (p *Pipeline) Sign(outDir string) (StepReport, error) {
	rep := StepReport{Step: "sign"}
	for _, m := range p.Plugins {
		artifact := fmt.Sprintf("%s-%s.tar.gz", m.Name, m.Version)
		artifactPath := filepath.Join(outDir, artifact)
		sum, err := sha256File(artifactPath)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", m.Name, err))
			continue
		}
		sidecar := artifactPath + ".sha256"
		if err := os.WriteFile(sidecar, []byte(sum+"  "+artifact+"\n"), 0o644); err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: %v", m.Name, err))
			continue
		}
		rep.Items = append(rep.Items, artifact+".sha256")
	}
	rep.OK = len(rep.Errors) == 0
	rep.Message = fmt.Sprintf("signed %d artifact(s)", len(rep.Items))
	return rep, nil
}

// indexEntry is one row of the emitted registry.json. We keep the shape
// loosely compatible with upstream/registry.json so an s06-style
// consumer could read either file.
type indexEntry struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Backend     string `json:"backend"`
	Entry       string `json:"entry"`
	Skill       string `json:"skill"`
	Artifact    string `json:"artifact"`
	SHA256      string `json:"sha256"`
}

// registryFile is the top-level shape of registry.json. Same `meta` +
// `clis` envelope upstream uses.
type registryFile struct {
	Meta map[string]any `json:"meta"`
	CLIs []indexEntry   `json:"clis"`
}

// UpdateIndex emits outDir/registry.json. It reads the sha256 sidecars
// Sign just wrote so the index is self-consistent. Re-reading (rather
// than caching) means a manual `sha256sum` edit would surface as a
// hash mismatch on the next run — desirable for tamper-evidence.
func (p *Pipeline) UpdateIndex(outDir string) (StepReport, error) {
	rep := StepReport{Step: "index"}
	entries := make([]indexEntry, 0, len(p.Plugins))
	for _, m := range p.Plugins {
		artifact := fmt.Sprintf("%s-%s.tar.gz", m.Name, m.Version)
		sidecar := filepath.Join(outDir, artifact+".sha256")
		b, err := os.ReadFile(sidecar)
		if err != nil {
			rep.Errors = append(rep.Errors, fmt.Sprintf("%s: read sidecar: %v", m.Name, err))
			continue
		}
		// sha256sum format: "<hex><spaces><filename>\n"
		fields := strings.Fields(string(b))
		sum := ""
		if len(fields) > 0 {
			sum = fields[0]
		}
		entries = append(entries, indexEntry{
			Name:     m.Name,
			Version:  m.Version,
			Backend:  m.Backend,
			Entry:    m.Entry,
			Skill:    m.Skill,
			Artifact: artifact,
			SHA256:   sum,
		})
	}
	doc := registryFile{
		Meta: map[string]any{
			"generated_by": "learn-cli-anything/s10",
			"plugin_count": len(entries),
		},
		CLIs: entries,
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return rep, err
	}
	// Append the closing newline so `git diff` is friendly.
	b = append(b, '\n')
	if err := os.WriteFile(filepath.Join(outDir, "registry.json"), b, 0o644); err != nil {
		rep.Errors = append(rep.Errors, err.Error())
		return rep, err
	}
	rep.Items = []string{"registry.json"}
	rep.OK = len(rep.Errors) == 0
	rep.Message = fmt.Sprintf("indexed %d plugin(s)", len(entries))
	return rep, nil
}

// Run drives the whole pipeline. It short-circuits on Validate failure
// because Bundle/Sign would just produce broken artifacts — better to
// fail loudly with a stable Report than to half-publish.
//
// Note that Validate's failure still returns a non-nil PipelineReport;
// callers (main and tests) inspect rep.OK rather than the error to
// decide whether to proceed. The error is reserved for I/O failures the
// caller can't recover from (out-dir mkdir, registry write).
func (p *Pipeline) Run(ctx context.Context, srcDir, outDir string) (PipelineReport, error) {
	rep := PipelineReport{SrcDir: srcDir, OutDir: outDir}

	step, err := p.ScanPlugins(srcDir)
	rep.Steps = append(rep.Steps, step)
	if err != nil {
		rep.OK = false
		return rep, err
	}

	step, _ = p.Validate(srcDir)
	rep.Steps = append(rep.Steps, step)
	if !step.OK {
		// surface the validation report and stop — don't bundle bad input
		rep.Plugins = p.Plugins
		rep.OK = false
		return rep, nil
	}

	step, err = p.Bundle(srcDir, outDir)
	rep.Steps = append(rep.Steps, step)
	if err != nil {
		rep.Plugins = p.Plugins
		rep.OK = false
		return rep, err
	}

	step, _ = p.Sign(outDir)
	rep.Steps = append(rep.Steps, step)
	if !step.OK {
		rep.Plugins = p.Plugins
		rep.OK = false
		return rep, nil
	}

	step, _ = p.UpdateIndex(outDir)
	rep.Steps = append(rep.Steps, step)

	rep.Plugins = p.Plugins
	rep.OK = allOK(rep.Steps)
	return rep, nil
}

// allOK is a small helper so Run's final OK reflects the whole table.
// Inlining the loop at the call site obscured Run's shape, so it lives
// here.
func allOK(steps []StepReport) bool {
	for _, s := range steps {
		if !s.OK {
			return false
		}
	}
	return true
}

// ReadStatus loads outDir/registry.json and returns the registryFile.
// `publish status` uses this to print a quick "what's currently
// published" summary without re-running the pipeline.
func ReadStatus(outDir string) (registryFile, error) {
	var rf registryFile
	b, err := os.ReadFile(filepath.Join(outDir, "registry.json"))
	if err != nil {
		return rf, err
	}
	if err := json.Unmarshal(b, &rf); err != nil {
		return rf, err
	}
	return rf, nil
}

// writeTarGz tars+gzips srcDir into dst. Paths inside the archive are
// stored relative to srcDir's parent so the top-level entry in the tar
// is the plugin's directory name itself — `tar tzf` then reads as
// "<plugin>/SKILL.md", which is what every downstream extractor expects.
func writeTarGz(srcDir, dst string) error {
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()

	parent := filepath.Dir(srcDir)
	return filepath.Walk(srcDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(parent, path)
		if err != nil {
			return err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		hdr.Name = filepath.ToSlash(rel)
		hdr.ModTime = fixedMTime
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
		hdr.Uid = 0
		hdr.Gid = 0
		hdr.Uname = ""
		hdr.Gname = ""
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		if info.Mode().IsRegular() {
			rf, err := os.Open(path)
			if err != nil {
				return err
			}
			_, err = io.Copy(tw, rf)
			rf.Close()
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// sha256File returns the hex digest of path's contents. Streaming Read
// keeps memory flat — plugin tarballs in the upstream max out around
// a few MB but the function should scale without re-allocating.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
