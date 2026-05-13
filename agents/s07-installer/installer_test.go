// installer_test.go — five focused tests that cover every branch in the
// dispatcher without touching the network or running real subprocesses.
//
// The pattern: each test builds a Registry rooted in t.TempDir(), swaps the
// Shell for a FakeShell, and (for the bundled tests) wires HTTPClient to an
// httptest.Server that serves a fixture tarball from testdata/. The ledger
// path is per-temp-dir so tests run in parallel without colliding.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newTestRegistry returns a Registry whose ledger + install dir live inside
// the test's temp dir, with Shell already wired to a FakeShell that tests can
// read back. Concentrating this here keeps each test focused on the assertion.
func newTestRegistry(t *testing.T) (*Registry, *FakeShell) {
	t.Helper()
	tmp := t.TempDir()
	fs := &FakeShell{}
	r := &Registry{
		LedgerPath: filepath.Join(tmp, "installed.json"),
		InstallDir: filepath.Join(tmp, "pkgs"),
		Shell:      fs,
		HTTPClient: http.DefaultClient,
	}
	return r, fs
}

// buildTarGz produces an in-memory gzipped tarball with one file. Used by the
// bundled test so we don't need to ship a binary fixture for the unit test
// path (testdata/anygen-0.1.tar.gz exists for the `make demo` flow).
func buildTarGz(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(body)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header: %v", err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("tar body: %v", err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	return buf.Bytes()
}

// TestInstallBundled exercises the BundledInstaller end to end: an
// httptest.Server hosts a tarball with one file, the registry downloads it,
// and we verify the file landed under InstallDir/<name>/.
func TestInstallBundled(t *testing.T) {
	reg, _ := newTestRegistry(t)

	tarBytes := buildTarGz(t, map[string]string{
		"README.txt": "hello from the bundled tarball\n",
	})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(tarBytes)
	}))
	defer srv.Close()

	m := Manifest{
		Name:    "anygen",
		Version: "0.1",
		Backend: "bundled",
		URL:     srv.URL + "/anygen-0.1.tar.gz",
	}
	if err := reg.Install(context.Background(), m); err != nil {
		t.Fatalf("install: %v", err)
	}

	readme := filepath.Join(reg.InstallDir, "anygen", "README.txt")
	got, err := os.ReadFile(readme)
	if err != nil {
		t.Fatalf("expected %s to exist: %v", readme, err)
	}
	if !strings.Contains(string(got), "hello from the bundled tarball") {
		t.Fatalf("unexpected README contents: %q", got)
	}
}

// TestInstallPipRecordsShellCall asserts the dispatcher emits the exact argv
// upstream does: `pip install <name>==<version>`. We use the FakeShell to
// capture the call instead of forking a real pip.
func TestInstallPipRecordsShellCall(t *testing.T) {
	reg, fs := newTestRegistry(t)

	m := Manifest{
		Name:    "cli-anything-blender",
		Version: "1.2.3",
		Backend: "pip",
		Entry:   "blender-cli",
	}
	if err := reg.Install(context.Background(), m); err != nil {
		t.Fatalf("install: %v", err)
	}
	if len(fs.Calls) != 1 {
		t.Fatalf("want 1 shell call, got %d: %+v", len(fs.Calls), fs.Calls)
	}
	got := fs.Calls[0]
	if got.Name != "pip" {
		t.Fatalf("want pip, got %q", got.Name)
	}
	wantArgs := []string{"install", "cli-anything-blender==1.2.3"}
	if len(got.Args) != len(wantArgs) {
		t.Fatalf("args: want %v got %v", wantArgs, got.Args)
	}
	for i := range wantArgs {
		if got.Args[i] != wantArgs[i] {
			t.Fatalf("args[%d]: want %q got %q", i, wantArgs[i], got.Args[i])
		}
	}
}

// TestListAfterTwoInstalls covers the ledger round-trip: two installs of
// different backends should show up as two manifests, sorted by Name.
func TestListAfterTwoInstalls(t *testing.T) {
	reg, _ := newTestRegistry(t)

	a := Manifest{Name: "alpha", Version: "1.0", Backend: "fake"}
	b := Manifest{Name: "beta", Version: "2.0", Backend: "fake"}
	if err := reg.Install(context.Background(), b); err != nil {
		t.Fatalf("install beta: %v", err)
	}
	if err := reg.Install(context.Background(), a); err != nil {
		t.Fatalf("install alpha: %v", err)
	}

	got, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 entries, got %d: %+v", len(got), got)
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Fatalf("want sorted [alpha, beta], got [%s, %s]", got[0].Name, got[1].Name)
	}
}

// TestUninstallBundledRemovesFromDiskAndLedger installs a bundled manifest,
// confirms the file is there, calls Uninstall, and asserts both the install
// dir and the ledger entry are gone.
func TestUninstallBundledRemovesFromDiskAndLedger(t *testing.T) {
	reg, _ := newTestRegistry(t)

	tarBytes := buildTarGz(t, map[string]string{"README.txt": "hi\n"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(tarBytes)
	}))
	defer srv.Close()

	m := Manifest{Name: "anygen", Version: "0.1", Backend: "bundled", URL: srv.URL}
	if err := reg.Install(context.Background(), m); err != nil {
		t.Fatalf("install: %v", err)
	}
	installed := filepath.Join(reg.InstallDir, "anygen", "README.txt")
	if _, err := os.Stat(installed); err != nil {
		t.Fatalf("pre-uninstall file missing: %v", err)
	}

	if err := reg.Uninstall(context.Background(), "anygen"); err != nil {
		t.Fatalf("uninstall: %v", err)
	}

	if _, err := os.Stat(installed); !os.IsNotExist(err) {
		t.Fatalf("expected %s to be gone, got err=%v", installed, err)
	}
	all, err := reg.List(context.Background())
	if err != nil {
		t.Fatalf("list after uninstall: %v", err)
	}
	if len(all) != 0 {
		t.Fatalf("expected empty ledger, got %+v", all)
	}
}

// TestUnknownBackendErrors is the trivial dispatcher guard. A manifest with
// a Backend the switch doesn't know about must produce a clear error so the
// CLI surfaces "unknown backend" to the agent instead of silently no-op'ing.
func TestUnknownBackendErrors(t *testing.T) {
	reg, _ := newTestRegistry(t)

	m := Manifest{Name: "ghost", Version: "0.0", Backend: "carrier-pigeon"}
	err := reg.Install(context.Background(), m)
	if err == nil {
		t.Fatal("want error for unknown backend, got nil")
	}
	if !strings.Contains(err.Error(), "unknown backend") {
		t.Fatalf("want 'unknown backend' in error, got %q", err.Error())
	}
}
