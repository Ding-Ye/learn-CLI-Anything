package main

import (
	"context"
	"runtime"
	"testing"
)

// TestFingerprintDeterministic — same inputs+args, called twice, must
// produce the same key. This is the contract the whole cache relies on.
func TestFingerprintDeterministic(t *testing.T) {
	inputs := map[string][]byte{
		"a.txt": []byte("hello"),
		"b.txt": []byte("world"),
	}
	args := []string{"echo", "hi"}
	k1 := Fingerprint(inputs, args)
	k2 := Fingerprint(inputs, args)
	if k1 != k2 {
		t.Fatalf("fingerprint not deterministic: %s vs %s", k1, k2)
	}
	// Re-build inputs in a different insertion order; map iteration is
	// random in Go, so this catches any accidental dependence on order.
	other := map[string][]byte{
		"b.txt": []byte("world"),
		"a.txt": []byte("hello"),
	}
	k3 := Fingerprint(other, args)
	if k1 != k3 {
		t.Fatalf("fingerprint depends on map insertion order: %s vs %s", k1, k3)
	}
}

// TestFingerprintDiffersOnInputChange — a single-byte change in any
// input must change the key.
func TestFingerprintDiffersOnInputChange(t *testing.T) {
	args := []string{"echo", "hi"}
	a := map[string][]byte{"x": []byte("hello")}
	b := map[string][]byte{"x": []byte("hellp")} // last byte flipped
	if Fingerprint(a, args) == Fingerprint(b, args) {
		t.Fatal("fingerprint did not change when input bytes changed")
	}

	// args change must also bust the key
	if Fingerprint(a, []string{"echo", "hi"}) == Fingerprint(a, []string{"echo", "bye"}) {
		t.Fatal("fingerprint did not change when args changed")
	}
}

// TestMemStoreRoundTrip — Put then Get yields the same bundle.
func TestMemStoreRoundTrip(t *testing.T) {
	store := NewMemStore(0)
	b := &Bundle{Key: "sha256:abc", Stdout: "hello", ExitCode: 0}
	if err := store.Put(b); err != nil {
		t.Fatalf("put: %v", err)
	}
	got, ok := store.Get("sha256:abc")
	if !ok {
		t.Fatal("get returned ok=false after put")
	}
	if got.Stdout != "hello" || got.ExitCode != 0 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

// TestRunCachesSecondCall — first Run executes, second Run returns
// cache_hit=true and skips the exec.
func TestRunCachesSecondCall(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses /bin/echo")
	}
	store := NewMemStore(0)
	ctx := context.Background()
	cmd := []string{"echo", "hello-from-test"}

	b1, hit1, err := Run(ctx, cmd, nil, store)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if hit1 {
		t.Fatal("first run reported cache hit; expected miss")
	}
	if b1.Stdout == "" {
		t.Fatalf("first run stdout empty: %+v", b1)
	}

	b2, hit2, err := Run(ctx, cmd, nil, store)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !hit2 {
		t.Fatal("second run reported cache miss; expected hit")
	}
	if b1.Key != b2.Key {
		t.Fatalf("keys differ across runs: %s vs %s", b1.Key, b2.Key)
	}
	if b1.Stdout != b2.Stdout {
		t.Fatalf("stdout differs across runs: %q vs %q", b1.Stdout, b2.Stdout)
	}
}

// TestMemStoreEviction — bonus: with cap=2, the third Put evicts the
// first. Verifies the bounded-memory mode an agent loop would want.
func TestMemStoreEviction(t *testing.T) {
	store := NewMemStore(2)
	if err := store.Put(&Bundle{Key: "k1"}); err != nil {
		t.Fatalf("put k1: %v", err)
	}
	if err := store.Put(&Bundle{Key: "k2"}); err != nil {
		t.Fatalf("put k2: %v", err)
	}
	if err := store.Put(&Bundle{Key: "k3"}); err != nil {
		t.Fatalf("put k3: %v", err)
	}
	if store.Len() != 2 {
		t.Fatalf("expected 2 entries after eviction, got %d", store.Len())
	}
	if _, ok := store.Get("k1"); ok {
		t.Fatal("k1 should have been evicted")
	}
	if _, ok := store.Get("k3"); !ok {
		t.Fatal("k3 should be the newest, still present")
	}
}
