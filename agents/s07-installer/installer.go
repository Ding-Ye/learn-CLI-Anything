// installer.go implements the multi-backend installer dispatcher. The shape
// mirrors upstream's cli-hub/cli_hub/installer.py: a strategy-per-backend
// table with a shared on-disk ledger (installed.json).
//
// Key differences from the Python original:
//
//  1. The Shell interface is injectable. The upstream calls subprocess.run
//     directly; we wrap it behind a tiny interface so tests can supply a
//     FakeShell that just records the (cmd, args) tuples instead of forking
//     pip. The production wiring uses RealShell which delegates to os/exec.
//
//  2. The "command" / "uv" / "npm" strategies from the upstream collapse into
//     one ShellInstaller. They all amount to "run a vendor binary against the
//     manifest name@version"; differing only in the argv prefix. Keeping them
//     as parameters of one type (instead of N near-identical structs) makes
//     the dispatch table trivially extensible.
//
//  3. BundledInstaller is what the upstream documents but doesn't actually do
//     — for the curriculum it's the most pedagogically interesting case: a
//     tarball downloaded over HTTP, extracted into the install dir. We use
//     httptest.Server in tests to avoid touching the network.
//
//  4. The ledger is a single JSON file under ~/.cache/. Upstream uses
//     ~/.cli-hub/; we sit in the OS cache dir so a `make clean` style nuke
//     by the user is the only state that matters.
package main

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Run executes name+args, streaming stdout/stderr to the parent. The parent
// is os.Stdout/os.Stderr in production; tests don't reach this code path
// because FakeShell intercepts Run first.
func (RealShell) Run(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Installer is the interface every backend (and the Registry that fans out to
// them) satisfies. Three methods, all context-aware so downloads and shell
// spawns can be cancelled.
type Installer interface {
	Install(ctx context.Context, m Manifest) error
	Uninstall(ctx context.Context, name string) error
	List(ctx context.Context) ([]Manifest, error)
}

// Shell abstracts "run a vendor binary". The only operation is Run — we don't
// care about stdout/stderr in the happy path; the manifest tells us what to
// expect. Errors are returned verbatim so callers can wrap them.
type Shell interface {
	Run(ctx context.Context, name string, args ...string) error
}

// RealShell is the production Shell. Not exercised in tests (those use
// FakeShell). We keep it small and deferred to os/exec so the bulk of the
// dispatcher stays unit-testable without any subprocess machinery.
//
// Note: we deliberately do not capture output. The upstream's pip/npm calls
// stream output to the user's terminal, which is the right ergonomics for a
// human; we mirror that. Tests use FakeShell so this never runs there.
type RealShell struct{}

// FakeShell records every invocation. Tests use it to assert the dispatcher
// emitted the right argv for each backend. Threaded into the dispatcher via
// NewRegistry's Shell field.
type FakeShell struct {
	Calls []ShellCall
	// Err is returned by every Run call if non-nil — useful for testing the
	// "pip failed" branch without rigging a real subprocess.
	Err error
}

// ShellCall is one recorded invocation. The test asserts on these tuples.
type ShellCall struct {
	Name string
	Args []string
}

// Run records the call and (optionally) returns a canned error. The actual
// exec.Cmd is never built — that's the whole point of the fake.
func (f *FakeShell) Run(ctx context.Context, name string, args ...string) error {
	f.Calls = append(f.Calls, ShellCall{Name: name, Args: append([]string{}, args...)})
	return f.Err
}

// Registry is the dispatcher. It picks a backend by Manifest.Backend, runs
// the corresponding strategy, and on success appends/removes entries in the
// on-disk ledger.
//
// Construction goes through NewRegistry so we can default Shell to a
// FakeShell-friendly nil (production wires RealShell), default InstallDir to
// the OS cache, and accept overrides per test.
type Registry struct {
	// LedgerPath is the JSON file that records what's installed. Defaults to
	// ~/.cache/learn-cli-anything-s07/installed.json.
	LedgerPath string

	// InstallDir is where the BundledInstaller extracts tarballs. Each
	// manifest lands under InstallDir/<name>/.
	InstallDir string

	// Shell is the injected runner. Production: &RealShell{}. Tests:
	// &FakeShell{}.
	Shell Shell

	// HTTPClient is used by BundledInstaller. Tests inject one wired to
	// httptest.Server.
	HTTPClient *http.Client
}

// NewRegistry returns a Registry rooted at the given cacheDir (typically
// ~/.cache/learn-cli-anything-s07). All fields are overridable post-construction
// — the constructor's job is just to fill in defensible defaults.
func NewRegistry(cacheDir string) *Registry {
	return &Registry{
		LedgerPath: filepath.Join(cacheDir, "installed.json"),
		InstallDir: filepath.Join(cacheDir, "pkgs"),
		Shell:      &RealShell{},
		HTTPClient: http.DefaultClient,
	}
}

// Install dispatches by Backend. Unknown backends return a clear error so the
// CLI surface can fail loudly instead of silently no-op'ing.
func (r *Registry) Install(ctx context.Context, m Manifest) error {
	if m.Name == "" {
		return errors.New("install: manifest.name is required")
	}
	switch m.Backend {
	case "bundled":
		if err := r.installBundled(ctx, m); err != nil {
			return err
		}
	case "pip", "npm", "uv":
		if err := r.installShell(ctx, m); err != nil {
			return err
		}
	case "fake":
		// "fake" is a no-op backend used by tests and the demo. We still
		// record it in the ledger so List can return it. The point is to
		// keep the dispatch table exercising both branches (file-side and
		// shell-side) without depending on tarballs or a real subprocess.
	default:
		return fmt.Errorf("install: unknown backend %q (want one of: pip, npm, uv, bundled, fake)", m.Backend)
	}
	return r.appendLedger(m)
}

// Uninstall removes the manifest from the ledger; for bundled entries it also
// deletes the on-disk install dir. The upstream additionally calls
// `pip uninstall -y`/`npm uninstall -g`; we'd do the same in RealShell-driven
// production, but only the ledger side needs to be testable.
func (r *Registry) Uninstall(ctx context.Context, name string) error {
	all, err := r.List(ctx)
	if err != nil {
		return err
	}
	idx := -1
	for i, m := range all {
		if m.Name == name {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("uninstall: %q is not installed", name)
	}
	m := all[idx]
	switch m.Backend {
	case "bundled":
		dir := filepath.Join(r.InstallDir, m.Name)
		// Best-effort: a missing dir is fine (someone may have nuked the
		// cache by hand). A real removal failure is surfaced.
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("uninstall: remove %s: %w", dir, err)
		}
	case "pip", "npm", "uv":
		// We mirror upstream's argv shape. The FakeShell records this; the
		// RealShell would actually run it.
		args := uninstallArgs(m)
		if err := r.Shell.Run(ctx, args[0], args[1:]...); err != nil {
			return fmt.Errorf("uninstall %s via %s: %w", m.Name, m.Backend, err)
		}
	case "fake":
		// ledger-only — nothing to undo
	}
	// Drop the entry and rewrite the ledger.
	all = append(all[:idx], all[idx+1:]...)
	return r.writeLedger(all)
}

// List returns the current ledger, sorted by Name for determinism.
func (r *Registry) List(_ context.Context) ([]Manifest, error) {
	b, err := os.ReadFile(r.LedgerPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read ledger %s: %w", r.LedgerPath, err)
	}
	var all []Manifest
	if len(b) == 0 {
		return nil, nil
	}
	if err := json.Unmarshal(b, &all); err != nil {
		return nil, fmt.Errorf("parse ledger %s: %w", r.LedgerPath, err)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })
	return all, nil
}

// installShell handles pip/npm/uv. The argv shape comes from installArgs —
// kept as a free function so the test can re-derive the expected calls.
func (r *Registry) installShell(ctx context.Context, m Manifest) error {
	args := installArgs(m)
	if err := r.Shell.Run(ctx, args[0], args[1:]...); err != nil {
		return fmt.Errorf("install %s via %s: %w", m.Name, m.Backend, err)
	}
	return nil
}

// installArgs renders the manifest into a shell invocation. Returned as a
// flat []string with argv[0] being the program name. Centralising this lets
// the test assert on the exact same shape as the production code emits.
//
//	pip:  pip install <name>==<version>
//	npm:  npm install -g <name>@<version>
//	uv:   uv pip install <name>==<version>
func installArgs(m Manifest) []string {
	switch m.Backend {
	case "pip":
		spec := m.Name
		if m.Version != "" {
			spec = m.Name + "==" + m.Version
		}
		return []string{"pip", "install", spec}
	case "npm":
		spec := m.Name
		if m.Version != "" {
			spec = m.Name + "@" + m.Version
		}
		return []string{"npm", "install", "-g", spec}
	case "uv":
		spec := m.Name
		if m.Version != "" {
			spec = m.Name + "==" + m.Version
		}
		return []string{"uv", "pip", "install", spec}
	}
	return nil
}

// uninstallArgs mirrors installArgs for the reverse path. The version is
// dropped — pip/npm uninstall don't need it.
func uninstallArgs(m Manifest) []string {
	switch m.Backend {
	case "pip":
		return []string{"pip", "uninstall", "-y", m.Name}
	case "npm":
		return []string{"npm", "uninstall", "-g", m.Name}
	case "uv":
		return []string{"uv", "pip", "uninstall", m.Name}
	}
	return nil
}

// installBundled downloads the URL into a temporary file, extracts the gzip
// tarball into InstallDir/<name>/, and trusts the manifest. The upstream
// version also runs a detect_cmd to short-circuit if the bundled binary is
// already on PATH; we skip that — the ledger answers the same question.
func (r *Registry) installBundled(ctx context.Context, m Manifest) error {
	if m.URL == "" {
		return fmt.Errorf("install %s: bundled backend requires manifest.url", m.Name)
	}
	dest := filepath.Join(r.InstallDir, m.Name)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dest, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.URL, nil)
	if err != nil {
		return fmt.Errorf("build request for %s: %w", m.URL, err)
	}
	resp, err := r.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("download %s: %w", m.URL, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("download %s: status %d", m.URL, resp.StatusCode)
	}
	return extractTarGz(resp.Body, dest)
}

// extractTarGz reads a gzipped tarball stream and writes each regular file
// under dest, preserving relative paths. Directory entries are created on
// demand. Symlinks and other special files are skipped — we don't need them
// for the curriculum and they're a security footgun (path traversal via
// `..`/absolute targets).
func extractTarGz(r io.Reader, dest string) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		// Reject path traversal. A header named "../../etc/passwd" is the
		// canonical malicious tarball. We require the cleaned path to stay
		// rooted at dest.
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("tar: unsafe path %q", hdr.Name)
		}
		target := filepath.Join(dest, clean)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return fmt.Errorf("create %s: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write %s: %w", target, err)
			}
			f.Close()
		default:
			// skip symlinks, devices, etc.
		}
	}
}

// appendLedger reads the ledger, replaces or appends m, then writes back.
// Replace-on-name lets `install foo` followed by a re-install update the
// version field in place instead of duplicating rows.
func (r *Registry) appendLedger(m Manifest) error {
	all, err := r.List(context.Background())
	if err != nil {
		return err
	}
	found := false
	for i := range all {
		if all[i].Name == m.Name {
			all[i] = m
			found = true
			break
		}
	}
	if !found {
		all = append(all, m)
	}
	return r.writeLedger(all)
}

// writeLedger serialises the slice to disk. The dir is created on demand so
// the first install bootstraps the cache without an extra MkdirAll in main.
func (r *Registry) writeLedger(all []Manifest) error {
	if err := os.MkdirAll(filepath.Dir(r.LedgerPath), 0o755); err != nil {
		return fmt.Errorf("mkdir ledger dir: %w", err)
	}
	b, err := json.MarshalIndent(all, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal ledger: %w", err)
	}
	if err := os.WriteFile(r.LedgerPath, b, 0o644); err != nil {
		return fmt.Errorf("write ledger %s: %w", r.LedgerPath, err)
	}
	return nil
}

// DefaultCacheDir returns ~/.cache/learn-cli-anything-s07. The Go stdlib's
// os.UserCacheDir handles cross-platform layouts (XDG on Linux,
// Library/Caches on macOS, %LocalAppData% on Windows).
func DefaultCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "learn-cli-anything-s07"), nil
}
