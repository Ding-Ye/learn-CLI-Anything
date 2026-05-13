package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

func TestParseMinimal(t *testing.T) {
	in := []byte("---\nname: demo\ndescription: a demo skill\n---\nbody text\n")
	s, err := Parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Meta.Name != "demo" {
		t.Fatalf("name=%q", s.Meta.Name)
	}
	if s.Meta.Description != "a demo skill" {
		t.Fatalf("desc=%q", s.Meta.Description)
	}
	if got, want := string(s.Body), "body text\n"; got != want {
		t.Fatalf("body=%q want %q", got, want)
	}
}

func TestParseWithTriggers(t *testing.T) {
	in := []byte("---\n" +
		"name: cli-anything-anygen\n" +
		"description: AnyGen CLI\n" +
		"triggers:\n" +
		"  - anygen\n" +
		"  - slides\n" +
		"  - presentation\n" +
		"---\n" +
		"# anygen\n\nbody\n")
	s, err := Parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"anygen", "slides", "presentation"}
	if len(s.Meta.Triggers) != len(want) {
		t.Fatalf("triggers=%v", s.Meta.Triggers)
	}
	for i, w := range want {
		if s.Meta.Triggers[i] != w {
			t.Fatalf("triggers[%d]=%q want %q", i, s.Meta.Triggers[i], w)
		}
	}
}

func TestRoundTripPreservesBytes(t *testing.T) {
	// Includes folded-scalar `>-` style that yaml.v3 would otherwise
	// re-emit differently — proves raw is being reused.
	in := []byte("---\n" +
		"name: >-\n" +
		"  cli-anything-anygen\n" +
		"description: >-\n" +
		"  Command-line interface for Anygen - generate slides...\n" +
		"---\n" +
		"# cli-anything-anygen\n\nbody body body\n")
	s, err := Parse(in)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	out := Render(s)
	if !bytes.Equal(in, out) {
		t.Fatalf("round-trip mismatch:\nIN:  %q\nOUT: %q", in, out)
	}
}

func TestParseMissingDelimiter(t *testing.T) {
	cases := map[string][]byte{
		"no_open":  []byte("name: x\n---\nbody\n"),
		"no_close": []byte("---\nname: x\nbody-without-close\n"),
	}
	for label, in := range cases {
		_, err := Parse(in)
		if !errors.Is(err, errMissingDelim) {
			t.Fatalf("%s: want errMissingDelim, got %v", label, err)
		}
	}
}

func TestParseMissingName(t *testing.T) {
	in := []byte("---\ndescription: missing name field\n---\nbody\n")
	_, err := Parse(in)
	if !errors.Is(err, errMissingName) {
		t.Fatalf("want errMissingName, got %v", err)
	}
	// also verify the error message helps a human debugging
	if err != nil && !strings.Contains(err.Error(), "name") {
		t.Fatalf("error %q should mention 'name'", err)
	}
}
